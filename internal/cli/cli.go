//go:build linux

// Package cli implements the Linux cma command line without accepting secrets in argv.
package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"claude-multi-account/internal/core"
	"claude-multi-account/internal/install"
	"claude-multi-account/internal/launcher"
)

var errArguments = errors.New("invalid command or options.")
var errCancelled = errors.New("operation cancelled.")

// runLauncher is a seam so tests never start the official Claude binary.
var runLauncher = launcher.Run

func runAndNormalize(args []string, profile string) (int, error) {
	code, err := runLauncher(args, profile)
	if code < 0 {
		code = 128 - code
	}
	return code, err
}

// Main executes argv without the program name and returns a shell exit status.
func Main(args []string) int { return execute(args, os.Stdin, os.Stdout, os.Stderr) }

type options struct {
	values     map[string]string
	flags      map[string]bool
	positional []string
}

// Parsing diagnostics deliberately never interpolate argv, which may contain secrets.
func parse(args []string, values, flags []string, count int) (options, error) {
	o := options{values: map[string]string{}, flags: map[string]bool{}}
	valueNames, flagNames := map[string]bool{}, map[string]bool{}
	for _, name := range values {
		valueNames[name] = true
	}
	for _, name := range flags {
		flagNames[name] = true
	}
	positionalOnly := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" && !positionalOnly {
			positionalOnly = true
			continue
		}
		if !positionalOnly && strings.HasPrefix(arg, "-") {
			name, value, equals := strings.Cut(arg, "=")
			if valueNames[name] {
				if !equals {
					i++
					if i >= len(args) || strings.HasPrefix(args[i], "--") {
						return o, errArguments
					}
					value = args[i]
				}
				o.values[name] = value
			} else if flagNames[name] && !equals {
				o.flags[name] = true
			} else {
				return o, errArguments
			}
		} else {
			o.positional = append(o.positional, arg)
		}
	}
	if len(o.positional) != count {
		return o, errArguments
	}
	return o, nil
}

func execute(args []string, in io.Reader, out, stderr io.Writer) int {
	if usage, ok := help(args); ok {
		fmt.Fprintln(out, usage)
		return 0
	}
	if len(args) == 0 {
		usage, _ := helpText("")
		fmt.Fprintln(stderr, usage)
		return 2
	}
	code, err := dispatch(args, in, out, stderr)
	if err != nil {
		if errors.Is(err, errArguments) {
			// Only a known command path is named, never other argv values.
			path := commandPath(args)
			usage, _ := helpUsage(path)
			fmt.Fprintln(stderr, "cma:", errArguments)
			fmt.Fprintln(stderr, "usage: "+usage)
			fmt.Fprintln(stderr, strings.TrimSpace("Run cma help "+path)+" for details.")
			return 2
		}
		if errors.Is(err, errCancelled) {
			fmt.Fprintln(stderr, "cma:", errCancelled)
			return 130
		}
		var pathErr *os.PathError
		var syscallErr *os.SyscallError
		if errors.As(err, &pathErr) || errors.As(err, &syscallErr) {
			err = errors.New("local file or process operation failed.")
		}
		fmt.Fprintln(stderr, "cma:", err)
		return 1
	}
	return code
}

func printJSON(out io.Writer, value any) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

func dispatch(args []string, in io.Reader, out, stderr io.Writer) (int, error) {
	if len(args) == 0 {
		return 0, errArguments
	}
	switch args[0] {
	case "profiles":
		return profiles(args[1:], in, out, stderr)
	case "shared":
		return 0, shared(args[1:], out, stderr)
	case "project":
		return 0, project(args[1:], out, stderr)
	case "status":
		o, err := parse(args[1:], nil, []string{"--json"}, 0)
		if err != nil {
			return 0, err
		}
		cwd, err := os.Getwd()
		if err != nil {
			return 0, err
		}
		value, err := core.Status(cwd)
		if err != nil {
			return 0, err
		}
		if o.flags["--json"] {
			return 0, printJSON(out, value)
		}
		// Status uses JSON-compatible values; decode the project independently of its concrete representation.
		raw, err := json.Marshal(value["project"])
		if err != nil {
			return 0, errors.New("Invalid project status.")
		}
		var p core.Project
		if json.Unmarshal(raw, &p) != nil {
			return 0, errors.New("Invalid project status.")
		}
		profile := "none"
		if p.Profile != nil {
			profile = *p.Profile
		}
		text := func(key, fallback string) string {
			if s, ok := value[key].(string); ok && s != "" {
				return s
			}
			return fallback
		}
		fmt.Fprintln(out, "Project: "+text("project_root", ""))
		fmt.Fprintln(out, "Configured profile: "+profile)
		fmt.Fprintln(out, "Active managed profile: "+text("active_profile", "none"))
		fmt.Fprintln(out, "Session: "+text("session_id", "not registered"))
		fmt.Fprintln(out, "Memory: "+p.Memory.Mode)
		fmt.Fprintln(out, "Mode: "+text("mode", "none"))
		fmt.Fprintln(out, "Config directory: "+text("config_dir", "not applicable"))
		fmt.Fprintln(out, "Login: "+text("login", "not applicable"))
		expiry := text("expiry_status", "unknown")
		if value["mode"] == core.ConfigDirBackend {
			expiry = "not applicable"
		}
		fmt.Fprintln(out, "Token expiry: "+expiry)
		fmt.Fprintln(out, "Profile metadata verification: unverified")
	case "switch":
		o, err := parse(args[1:], []string{"--scope"}, []string{"--confirm-context-transfer"}, 1)
		if err != nil {
			return 0, err
		}
		scope := o.values["--scope"]
		if scope == "" {
			scope = "session"
		}
		if scope != "session" && scope != "project" {
			return 0, errArguments
		}
		if err := core.RequestSwitch(o.positional[0], scope, o.flags["--confirm-context-transfer"]); err != nil {
			return 0, err
		}
		fmt.Fprintln(out, "Switch queued. Exit Claude normally with /exit; the wrapper will resume the exact registered session. No process was killed.")
	case "launch":
		profile, forwarded, err := launchArgs(args[1:])
		if err != nil {
			return 0, err
		}
		return runAndNormalize(forwarded, profile)
	case "claude":
		profile, forwarded, err := claudeArgs(args[1:])
		if err != nil {
			return 0, err
		}
		return runAndNormalize(forwarded, profile)
	case "install", "uninstall":
		flags := []string{}
		if args[0] == "install" {
			flags = []string{"--shell"}
		}
		o, err := parse(args[1:], []string{"--target"}, flags, 0)
		if err != nil {
			return 0, err
		}
		target, set := o.values["--target"]
		if !set {
			home, err := os.UserHomeDir()
			if err != nil {
				return 0, err
			}
			target = filepath.Join(home, ".local/share/claude-multi-account")
		}
		var result map[string]any
		if args[0] == "install" {
			result, err = install.Install(target, o.flags["--shell"])
		} else {
			result, err = install.Uninstall(target)
		}
		if err != nil {
			return 0, err
		}
		return 0, printJSON(out, result)
	case "internal":
		if len(args) != 2 {
			return 0, errArguments
		}
		switch args[1] {
		case "session-start":
			if os.Getenv("CMA_MANAGED") != "1" {
				fmt.Fprintln(stderr, "accounts: unmanaged session; use claude-cma for session switching.")
				return 0, nil
			}
			session, err := readSession(in)
			if err != nil {
				return 0, err
			}
			return 0, core.RegisterSession(session)
		case "session-info":
			session, err := core.SessionInfo()
			if err != nil {
				return 0, err
			}
			return 0, printJSON(out, session)
		default:
			return 0, errArguments
		}
	default:
		return 0, errArguments
	}
	return 0, nil
}

func launchArgs(args []string) (string, []string, error) {
	profile := ""
	for len(args) > 0 {
		if args[0] == "--" {
			return profile, args[1:], nil
		}
		if args[0] == "--profile" {
			if len(args) < 2 || strings.HasPrefix(args[1], "--") {
				return "", nil, errArguments
			}
			profile, args = args[1], args[2:]
			continue
		}
		if strings.HasPrefix(args[0], "--profile=") {
			profile, args = strings.TrimPrefix(args[0], "--profile="), args[1:]
			continue
		}
		if strings.HasPrefix(args[0], "-") {
			return "", nil, errArguments
		}
		return profile, args, nil
	}
	return profile, []string{}, nil
}

// claudeArgs forwards everything verbatim except a leading --profile and an optional "--".
func claudeArgs(args []string) (string, []string, error) {
	profile := ""
	if len(args) > 0 && args[0] == "--profile" {
		if len(args) < 2 || strings.HasPrefix(args[1], "-") {
			return "", nil, errArguments
		}
		profile, args = args[1], args[2:]
	} else if len(args) > 0 && strings.HasPrefix(args[0], "--profile=") {
		profile, args = strings.TrimPrefix(args[0], "--profile="), args[1:]
	}
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	return profile, append([]string{}, args...), nil
}

func profiles(args []string, in io.Reader, out, stderr io.Writer) (int, error) {
	if len(args) == 0 {
		return 0, errArguments
	}
	switch args[0] {
	case "login":
		o, err := parse(args[1:], nil, nil, 1)
		if err != nil {
			return 0, err
		}
		id := o.positional[0]
		p, err := core.GetProfile(id)
		if err != nil {
			return 0, err
		}
		if !core.IsConfigDir(p) {
			return 0, errors.New("Profile uses a token; login applies to config-dir profiles. Use cma profiles renew " + id + ".")
		}
		return runAndNormalize([]string{"auth", "login"}, id)
	case "add":
		return 0, profilesAdd(args[1:], in, out, stderr)
	case "remove":
		return 0, profilesRemove(args[1:], out)
	case "list":
		return 0, profilesList(args[1:], out)
	case "renew":
		return 0, profilesRenew(args[1:], in, stderr, out)
	default:
		return 0, errArguments
	}
}

func profilesList(args []string, out io.Writer) error {
	o, err := parse(args, nil, []string{"--json"}, 0)
	if err != nil {
		return err
	}
	registry, err := core.LoadRegistry()
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(registry.Profiles))
	for id := range registry.Profiles {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	items := make([]core.Profile, 0, len(ids))
	for _, id := range ids {
		items = append(items, registry.Profiles[id])
	}
	if o.flags["--json"] {
		return printJSON(out, items)
	}
	for _, p := range items {
		email, organization := p.Email, p.Organization
		if email == "" {
			email = "unknown"
		}
		if organization == "" {
			organization = "unknown"
		}
		mode, column := "token", "expiry: unknown"
		if p.ExpiresAt != nil {
			column = "expiry: " + *p.ExpiresAt
		}
		if core.IsConfigDir(p) {
			mode, column = core.ConfigDirBackend, "login: "+core.LoginState(p.ID)
		}
		fmt.Fprintf(out, "%s\t%s\t%s\t%s\t%s\tverification: unverified\tmode: %s\n", p.ID, p.Label, email, organization, column, mode)
	}
	if len(items) == 0 {
		fmt.Fprintln(out, "No profiles configured.")
	}
	return nil
}

func profilesRenew(args []string, in io.Reader, stderr, out io.Writer) error {
	o, err := parse(args, nil, []string{"--token-stdin"}, 1)
	if err != nil {
		return err
	}
	id := o.positional[0]
	if _, err := core.GetProfile(id); err != nil {
		return err
	}
	token, err := readToken(in, stderr, o.flags["--token-stdin"])
	if err != nil {
		return err
	}
	if _, err := core.RenewProfile(id, token); err != nil {
		return err
	}
	fmt.Fprintln(out, "Profile renewed; expiry is now unknown.")
	return nil
}

func profilesAdd(args []string, in io.Reader, out, stderr io.Writer) error {
	o, err := parse(args, []string{"--label", "--email", "--organization", "--organization-id", "--expires-at", "--backend", "--mode"}, []string{"--token-stdin"}, 1)
	if err != nil {
		return err
	}
	if _, ok := o.values["--label"]; !ok {
		return errArguments
	}
	mode := o.values["--mode"]
	if mode == "" {
		mode = core.ConfigDirBackend
	}
	if mode != core.ConfigDirBackend && mode != "token" {
		return errArguments
	}
	_, expires := o.values["--expires-at"]
	_, backendSet := o.values["--backend"]
	id := o.positional[0]
	if mode == core.ConfigDirBackend {
		if expires || backendSet || o.flags["--token-stdin"] {
			return errArguments
		}
		p := core.Profile{ID: id, Label: o.values["--label"], Email: o.values["--email"], Organization: o.values["--organization"], OrganizationID: o.values["--organization-id"]}
		_, report, err := core.AddConfigDirProfile(p)
		if err != nil {
			return err
		}
		dir, err := core.ProfileDir(id)
		if err != nil {
			return err
		}
		fmt.Fprintln(out, "Profile added (config-dir); optional organization/email metadata is unverified and is not inferred.")
		fmt.Fprintln(out, "Profile directory: "+dir)
		if len(report.Conflicts) > 0 {
			fmt.Fprintln(out, "Shared link conflicts (existing entries left untouched): "+strings.Join(report.Conflicts, ", "))
		}
		if core.LoginState(id) == "present" {
			fmt.Fprintln(out, "This directory already holds a login; it is being reused. To replace it, run: cma profiles login "+id)
			return nil
		}
		fmt.Fprintln(out, "Next step: cma profiles login "+id)
		return nil
	}
	backend := o.values["--backend"]
	if backend == "" {
		backend = "auto"
	}
	if backend != "auto" && backend != "file" && backend != "secret-service" {
		return errArguments
	}
	if err := core.ValidateProfileID(id); err != nil {
		return err
	}
	token, err := readToken(in, stderr, o.flags["--token-stdin"])
	if err != nil {
		return err
	}
	p := core.Profile{ID: id, Label: o.values["--label"], Email: o.values["--email"], Organization: o.values["--organization"], OrganizationID: o.values["--organization-id"]}
	if expires, ok := o.values["--expires-at"]; ok {
		p.ExpiresAt = &expires
	}
	if _, err := core.AddProfile(p, token, backend); err != nil {
		return err
	}
	fmt.Fprintln(out, "Profile added; optional organization/email metadata is unverified and is not inferred from the token.")
	return nil
}

func profilesRemove(args []string, out io.Writer) error {
	o, err := parse(args, nil, []string{"--purge"}, 1)
	if err != nil {
		return err
	}
	id := o.positional[0]
	p, err := core.GetProfile(id)
	if err != nil {
		return err
	}
	configDir := core.IsConfigDir(p)
	if o.flags["--purge"] && !configDir {
		return errors.New("--purge applies only to config-dir profiles; nothing was removed.")
	}
	if configDir && o.flags["--purge"] {
		// purge first: a failed purge must leave the registry entry so it can be retried
		if err := core.PurgeProfileDir(id); err != nil {
			return err
		}
	}
	if err := core.RemoveProfile(id); err != nil {
		return err
	}
	if !configDir {
		fmt.Fprintln(out, "Profile removed.")
		return nil
	}
	dir, err := core.ProfileDir(id)
	if err != nil {
		return err
	}
	if o.flags["--purge"] {
		fmt.Fprintln(out, "Profile removed; its directory and login were deleted: "+dir)
		return nil
	}
	fmt.Fprintln(out, "Profile removed; its directory was kept: "+dir)
	fmt.Fprintln(out, "It contains the login of this profile. Delete the directory manually if no longer needed (--purge is not possible after removal).")
	return nil
}

func shared(args []string, out, stderr io.Writer) error {
	if len(args) == 0 {
		return errArguments
	}
	switch args[0] {
	case "path":
		if _, err := parse(args[1:], nil, nil, 0); err != nil {
			return err
		}
		fmt.Fprintln(out, core.SharedHome())
	case "import":
		o, err := parse(args[1:], []string{"--from"}, nil, 0)
		if err != nil {
			return err
		}
		from, set := o.values["--from"]
		if !set {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			from = filepath.Join(home, ".claude")
		}
		copied, skipped, err := core.ImportShared(from)
		if err != nil {
			return err
		}
		fmt.Fprintln(out, "Copied: "+listOrNone(copied))
		fmt.Fprintln(out, "Skipped: "+listOrNone(skipped))
		fmt.Fprintln(out, "The source directory was not modified.")
	case "sync":
		o, err := parse(args[1:], nil, []string{"--json"}, 0)
		if err != nil {
			return err
		}
		registry, err := core.LoadRegistry()
		if err != nil {
			return err
		}
		ids := []string{}
		for id, p := range registry.Profiles {
			if core.IsConfigDir(p) {
				ids = append(ids, id)
			}
		}
		sort.Strings(ids)
		reports := map[string]core.LinkReport{}
		conflicts := []string{}
		for _, id := range ids {
			_, report, err := core.EnsureProfileDir(id)
			if err != nil {
				return err
			}
			reports[id] = report
			if len(report.Conflicts) > 0 {
				conflicts = append(conflicts, id+": "+strings.Join(report.Conflicts, ", "))
			}
		}
		if o.flags["--json"] {
			if err := printJSON(out, reports); err != nil {
				return err
			}
		} else {
			for _, id := range ids {
				r := reports[id]
				fmt.Fprintf(out, "%s\tlinked: %s\tpresent: %s\tconflicts: %s\tskipped: %s\n", id, listOrNone(r.Linked), listOrNone(r.Present), listOrNone(r.Conflicts), listOrNone(r.Skipped))
			}
			if len(ids) == 0 {
				fmt.Fprintln(out, "No config-dir profiles configured.")
			}
		}
		if len(conflicts) > 0 {
			fmt.Fprintln(stderr, "cma: shared link conflicts (existing entries left untouched): "+strings.Join(conflicts, "; "))
		}
	default:
		return errArguments
	}
	return nil
}

func listOrNone(items []string) string {
	if len(items) == 0 {
		return "none"
	}
	return strings.Join(items, ", ")
}

func project(args []string, out, stderr io.Writer) error {
	if len(args) == 0 {
		return errArguments
	}
	var o options
	var err error
	switch args[0] {
	case "init":
		o, err = parse(args[1:], []string{"--profile"}, nil, 0)
	case "use", "memory":
		o, err = parse(args[1:], nil, nil, 1)
	default:
		return errArguments
	}
	if err != nil {
		return err
	}
	if args[0] == "memory" && o.positional[0] != "shared" && o.positional[0] != "per-profile" {
		return errArguments
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	root, config, err := core.LoadProject(cwd)
	if err != nil {
		return err
	}
	switch args[0] {
	case "init":
		if _, err := os.Lstat(filepath.Join(root, ".claude/accounts.local.json")); err == nil {
			return errors.New("Project is already initialized; use project use or project memory.")
		} else if !os.IsNotExist(err) {
			return err
		}
		config.Profile = nil
		if id, ok := o.values["--profile"]; ok {
			config.Profile = &id
		}
	case "use":
		id := o.positional[0]
		if _, err := core.GetProfile(id); err != nil {
			return err
		}
		if err := core.CheckAllowed(config, id); err != nil {
			return err
		}
		config.Profile = &id
	case "memory":
		config.Memory.Mode = o.positional[0]
	}
	if err := core.SaveProject(root, config); err != nil {
		return err
	}
	fmt.Fprintln(out, "Project configuration saved.")
	fmt.Fprintln(stderr, "Keep .claude/accounts.local.json local: add it to .git/info/exclude (worktrees: use git rev-parse --git-path info/exclude).")
	if args[0] == "use" {
		fmt.Fprintln(out, "Applies to future launches; an active managed session requires cma switch.")
	}
	if args[0] == "memory" {
		fmt.Fprintln(out, "Applies to future launches; existing memory is not copied or migrated.")
	}
	return nil
}

func readToken(in io.Reader, stderr io.Writer, explicit bool) (string, error) {
	var raw []byte
	var err error
	if explicit {
		raw, err = io.ReadAll(io.LimitReader(in, 16387))
	} else {
		raw, err = hiddenToken(in, stderr)
	}
	if err != nil {
		return "", err
	}
	token := strings.TrimSuffix(strings.TrimSuffix(string(raw), "\n"), "\r")
	if len(token) == 0 || len(token) > 16384 || !utf8.ValidString(token) {
		return "", errors.New("Token must be a nonempty single value without whitespace.")
	}
	for _, r := range token {
		if unicode.IsSpace(r) || r < 32 {
			return "", errors.New("Token must be a nonempty single value without whitespace.")
		}
	}
	return token, nil
}

func readSession(in io.Reader) (core.Session, error) {
	var session core.Session
	raw, err := io.ReadAll(io.LimitReader(in, 1024*1024+1))
	if err != nil {
		return session, errors.New("SessionStart requires a JSON object on stdin.")
	}
	if len(raw) > 1024*1024 {
		return session, errors.New("SessionStart payload is too large.")
	}
	var fields map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if !utf8.Valid(raw) || strictValue(decoder, 0) != nil || json.Unmarshal(raw, &fields) != nil || fields == nil {
		return session, errors.New("SessionStart requires a JSON object on stdin.")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return session, errors.New("SessionStart requires a JSON object on stdin.")
	}
	// Hook payloads contain additional Claude fields. Only these two belong to the registration API.
	if json.Unmarshal(fields["session_id"], &session.SessionID) != nil || session.SessionID == "" {
		return session, errors.New("SessionStart requires a valid exact session_id.")
	}
	if json.Unmarshal(fields["cwd"], &session.Cwd) != nil || !filepath.IsAbs(session.Cwd) {
		return session, errors.New("SessionStart requires an absolute cwd.")
	}
	return session, nil
}

// Reject duplicate keys and pathological nesting, including inside ignored hook fields.
func strictValue(decoder *json.Decoder, depth int) error {
	if depth > 128 {
		return errors.New("Invalid JSON.")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, composite := token.(json.Delim)
	if !composite {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return errors.New("Invalid JSON.")
			}
			seen[name] = true
			if err := strictValue(decoder, depth+1); err != nil {
				return err
			}
		}
		if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
			return errors.New("Invalid JSON.")
		}
	case '[':
		for decoder.More() {
			if err := strictValue(decoder, depth+1); err != nil {
				return err
			}
		}
		if token, err := decoder.Token(); err != nil || token != json.Delim(']') {
			return errors.New("Invalid JSON.")
		}
	default:
		return errors.New("Invalid JSON.")
	}
	return nil
}
