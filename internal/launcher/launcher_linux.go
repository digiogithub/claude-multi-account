//go:build linux

// Package launcher supervises opt-in Claude processes without writing credentials.
package launcher

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"claude-multi-account/internal/core"
)

var authEnv = strings.Fields(`ANTHROPIC_API_KEY ANTHROPIC_AUTH_TOKEN ANTHROPIC_BASE_URL CLAUDE_CODE_USE_BEDROCK CLAUDE_CODE_USE_VERTEX CLAUDE_CODE_USE_FOUNDRY ANTHROPIC_BEDROCK_BASE_URL ANTHROPIC_VERTEX_BASE_URL ANTHROPIC_FOUNDRY_BASE_URL CLAUDE_CODE_ORGANIZATION_UUID ANTHROPIC_CUSTOM_HEADERS AWS_BEARER_TOKEN_BEDROCK CLAUDE_CODE_OAUTH_TOKEN AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN AWS_PROFILE GOOGLE_APPLICATION_CREDENTIALS ANTHROPIC_VERTEX_PROJECT_ID ANTHROPIC_FOUNDRY_API_KEY ANTHROPIC_FOUNDRY_RESOURCE ANTHROPIC_FOUNDRY_AUTH_TOKEN CLAUDE_CODE_USE_GATEWAY CLAUDE_CODE_USE_ANTHROPIC_AWS ANTHROPIC_AWS_BASE_URL ANTHROPIC_AWS_API_KEY CLAUDE_CODE_PROVIDER_MANAGED_BY_HOST CLAUDE_CODE_HOST_AUTH_ENV_VAR ANTHROPIC_UNIX_SOCKET CLAUDE_CODE_OAUTH_TOKEN_FILE_DESCRIPTOR CLAUDE_CODE_HOST_CREDS_FILE CLAUDE_CODE_OAUTH_REFRESH_TOKEN CLAUDE_CODE_OAUTH_SCOPES CLAUDE_CODE_SIMPLE`)
var valueOptions = stringSet(strings.Fields(`--settings --model --permission-mode --allowedTools --allowed-tools --disallowedTools --disallowed-tools --tools --system-prompt --append-system-prompt --system-prompt-file --append-system-prompt-file --output-format --input-format --max-turns --max-budget-usd --mcp-config --agents --agent --setting-sources --plugin-dir --add-dir --permission-prompt-tool --betas --fallback-model --debug-file --mcp-debug --json-schema --effort --name --session-id`))
var management = stringSet(strings.Fields(`auth setup-token plugin update install doctor --help -h --version -v`))

// Function seams keep supervisor tests isolated from real credentials and processes.
var loadProject = core.LoadProject
var loadRegistry = core.LoadRegistry
var getProfile = core.GetProfile
var getToken = core.GetToken
var saveProject = core.SaveProject
var ensureProfileDir = core.EnsureProfileDir

// managementProject is the best-effort project lookup for management commands. It is
// separate from loadProject so administration never depends on launch seams.
var managementProject = core.LoadProject
var wait = waitProcess
var stdinTTY = func() bool {
	var term syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, os.Stdin.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&term)))
	return errno == 0
}

func stringSet(values []string) map[string]bool {
	out := make(map[string]bool)
	for _, s := range values {
		out[s] = true
	}
	return out
}

type option struct {
	start, end  int
	name, value string
}

func options(args []string) ([]option, error) {
	out := []option{}
	for i := 0; i < len(args); {
		arg := args[i]
		if arg == "--" {
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			i++
			continue
		}
		name, value, equals := strings.Cut(arg, "=")
		end := i + 1
		if !equals && valueOptions[name] {
			if end >= len(args) {
				return nil, fmt.Errorf("Missing value for %s", name)
			}
			value = args[end]
			end++
		} else if !equals && (name == "--resume" || name == "-r") && end < len(args) && !strings.HasPrefix(args[end], "-") {
			value = args[end]
			end++
		}
		out = append(out, option{i, end, name, value})
		i = end
	}
	return out, nil
}
func removeSpans(args []string, spans []option) []string {
	removed := make(map[int]bool)
	for _, s := range spans {
		for i := s.start; i < s.end; i++ {
			removed[i] = true
		}
	}
	out := []string{}
	for i, a := range args {
		if !removed[i] {
			out = append(out, a)
		}
	}
	return out
}

// ResumeArgs replaces session selectors, including names and flags after prompts,
// without interpreting option values or anything following the -- terminator.
func ResumeArgs(args []string, sessionID string) ([]string, error) {
	opts, err := options(args)
	if err != nil {
		return nil, err
	}
	spans := []option{}
	for _, o := range opts {
		switch o.name {
		case "--resume", "-r", "--continue", "-c", "--session-id", "--fork-session":
			spans = append(spans, o)
		}
	}
	return append([]string{"--resume", sessionID}, removeSpans(args, spans)...), nil
}

func randomID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", errors.New("Cannot generate launch identity")
	}
	return hex.EncodeToString(b[:]), nil
}
func homePath(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, strings.TrimPrefix(path, "~/"))
	}
	return path
}
func canonical(path string) (string, error) {
	p, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(p)
}

// ResolveBinary resolves official Claude, excluding our executable and copied shims.
func ResolveBinary() (string, error) {
	override := os.Getenv("CMA_CLAUDE_BINARY")
	candidates := []string{}
	if override != "" {
		if !filepath.IsAbs(override) {
			return "", errors.New("CMA_CLAUDE_BINARY must be absolute")
		}
		candidates = append(candidates, override)
	} else {
		for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
			candidates = append(candidates, filepath.Join(dir, "claude"))
		}
	}
	own, _ := os.Executable()
	ownInfo, _ := os.Stat(own)
	for _, path := range candidates {
		resolved, err := canonical(path)
		if err != nil {
			continue
		}
		info, err := os.Stat(resolved)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
			continue
		}
		if filepath.Base(resolved) == "cma" || filepath.Base(resolved) == "claude-cma" || (ownInfo != nil && os.SameFile(info, ownInfo)) {
			continue
		}
		// Renamed copies of this compiled binary are wrappers too, not Claude.
		if ownInfo != nil && info.Size() == ownInfo.Size() {
			candidateHash, candidateErr := binaryDigest(resolved)
			ownHash, ownErr := binaryDigest(own)
			if candidateErr != nil || ownErr != nil || candidateHash == ownHash {
				continue
			}
		}
		f, err := os.Open(resolved)
		if err != nil {
			continue
		}
		prefix, err := io.ReadAll(io.LimitReader(f, 8192))
		f.Close()
		if err != nil {
			continue
		}
		invalid := false
		for _, marker := range []string{"cma.launcher", "cma.cli", "CMA_MANAGED_SHIM"} {
			if strings.Contains(string(prefix), marker) {
				invalid = true
			}
		}
		if !invalid {
			return resolved, nil
		}
	}
	return "", errors.New("Cannot find the official Claude binary; set CMA_CLAUDE_BINARY to its absolute path")
}

func binaryDigest(path string) ([32]byte, error) {
	var result [32]byte
	file, err := os.Open(path)
	if err != nil {
		return result, err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return result, err
	}
	copy(result[:], hash.Sum(nil))
	return result, nil
}

func decodeSettings(data []byte) (map[string]any, error) {
	var obj map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	// Preserve large integers in the explicit settings copied into memory overlays.
	decoder.UseNumber()
	if err := decoder.Decode(&obj); err != nil || obj == nil {
		return nil, errors.New("Settings must be JSON objects")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, errors.New("Settings must contain exactly one JSON object")
	}
	return obj, nil
}
func readSettings(path string) (map[string]any, error) {
	// Never open credentials, even when the supplied filename is a symlink.
	resolved, err := canonical(homePath(path))
	if filepath.Base(path) == ".credentials.json" || (err == nil && filepath.Base(resolved) == ".credentials.json") {
		return nil, errors.New("Credential files cannot be used as settings")
	}
	if err != nil {
		return nil, errors.New("Cannot read settings")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1024*1024 {
		return nil, errors.New("Settings must be regular files smaller than 1 MiB")
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return nil, errors.New("Cannot read settings")
	}
	return decodeSettings(data)
}
func explicitSettings(value string) (map[string]any, error) {
	if strings.HasPrefix(strings.TrimSpace(value), "{") {
		return decodeSettings([]byte(value))
	}
	return readSettings(value)
}
func settings(args []string, root, configDir string) ([]map[string]any, []option, error) {
	config := configDir
	if config == "" {
		home, _ := os.UserHomeDir()
		config = os.Getenv("CLAUDE_CONFIG_DIR")
		if config == "" {
			config = filepath.Join(home, ".claude")
		}
	}
	paths := []string{filepath.Join(homePath(config), "settings.json"), "/etc/claude-code/managed-settings.json"}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, nil, err
	}
	// Read root, starting directory, and intervening ancestors: all can supply policy.
	for p := cwd; ; p = filepath.Dir(p) {
		paths = append(paths, filepath.Join(p, ".claude", "settings.json"), filepath.Join(p, ".claude", "settings.local.json"))
		if p == root || filepath.Dir(p) == p {
			break
		}
	}
	if root != cwd {
		paths = append(paths, filepath.Join(root, ".claude", "settings.json"), filepath.Join(root, ".claude", "settings.local.json"))
	}
	values := []map[string]any{}
	seen := make(map[string]bool)
	for _, path := range paths {
		if seen[path] {
			continue
		}
		seen[path] = true
		_, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, nil, errors.New("Cannot inspect settings")
		}
		obj, err := readSettings(path)
		if err != nil {
			return nil, nil, err
		}
		values = append(values, obj)
	}
	opts, err := options(args)
	if err != nil {
		return nil, nil, err
	}
	explicit := []option{}
	for _, o := range opts {
		if o.name == "--settings" {
			obj, err := explicitSettings(o.value)
			if err != nil {
				return nil, nil, err
			}
			values = append(values, obj)
			explicit = append(explicit, o)
		}
	}
	return values, explicit, nil
}
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case string:
		return x != ""
	case bool:
		return x
	case float64:
		return x != 0
	case json.Number:
		v, err := x.Float64()
		return err != nil || v != 0
	case map[string]any:
		return len(x) > 0
	case []any:
		return len(x) > 0
	default:
		return true
	}
}
func authConflicts(values []map[string]any, tokenMode bool) error {
	for _, key := range authEnv {
		if os.Getenv(key) != "" {
			return fmt.Errorf("Authentication/routing conflict: %s", key)
		}
	}
	for _, obj := range values {
		if truthy(obj["apiKeyHelper"]) {
			return errors.New("Authentication conflict: apiKeyHelper")
		}
		if tokenMode && (truthy(obj["forceLoginMethod"]) || truthy(obj["forceLoginOrgUUID"])) {
			return errors.New("Managed login policy conflicts with token-based profile selection")
		}
		env := map[string]any{}
		if v, ok := obj["env"]; ok {
			var valid bool
			env, valid = v.(map[string]any)
			if !valid {
				return errors.New("Settings env must be an object")
			}
		}
		for _, key := range authEnv {
			if truthy(env[key]) {
				return fmt.Errorf("Settings authentication/routing conflict: %s", key)
			}
		}
	}
	return nil
}
func validateProfile(id string, project core.Project) error {
	_, err := resolveProfile(id, project)
	return err
}

// resolveProfile validates the profile and returns its record, which decides the mode.
func resolveProfile(id string, project core.Project) (core.Profile, error) {
	if err := core.ValidateProfileID(id); err != nil {
		return core.Profile{}, err
	}
	if err := core.CheckAllowed(project, id); err != nil {
		return core.Profile{}, err
	}
	return lookupProfile(id)
}

// lookupProfile validates ID, existence and expiry without any project policy.
func lookupProfile(id string) (core.Profile, error) {
	if err := core.ValidateProfileID(id); err != nil {
		return core.Profile{}, err
	}
	profile, err := getProfile(id)
	if err != nil {
		return core.Profile{}, err
	}
	if err := core.EnsureNotExpired(profile); err != nil {
		return core.Profile{}, err
	}
	return profile, nil
}

// prepareDir ensures a config-dir profile directory, optionally reporting conflicts.
func prepareDir(id string, warn bool) (string, error) {
	dir, report, err := ensureProfileDir(id)
	if err != nil {
		return "", err
	}
	if warn {
		for _, name := range report.Conflicts {
			fmt.Fprintf(os.Stderr, "cma: shared link conflict in profile %s: %s (left untouched)\n", id, name)
		}
	}
	return dir, nil
}

func unsupported(opts []option, configDir bool) error {
	for _, o := range opts {
		switch o.name {
		case "--bare":
			if configDir {
				return errors.New("--bare is not supported: it ignores the profile configuration directory login")
			}
			return errors.New("Bare, background and cloud sessions are not supported by this terminal wrapper")
		case "--bg", "--background", "--cloud":
			if !configDir {
				return errors.New("Bare, background and cloud sessions are not supported by this terminal wrapper")
			}
		}
	}
	return nil
}
func merge(left, right map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range left {
		out[k] = v
	}
	for k, v := range right {
		l, lok := out[k].(map[string]any)
		r, rok := v.(map[string]any)
		if lok && rok {
			out[k] = merge(l, r)
		} else {
			out[k] = v
		}
	}
	return out
}
func memoryArgs(args []string, project core.Project, root, profile, runDir string) ([]string, error) {
	return memoryArgsIn(args, project, root, profile, runDir, "")
}
func memoryArgsIn(args []string, project core.Project, root, profile, runDir, configDir string) ([]string, error) {
	values, spans, err := settings(args, root, configDir)
	if err != nil {
		return nil, err
	}
	if project.Memory.Mode != "per-profile" {
		return append([]string{}, args...), nil
	}
	for _, obj := range values {
		if _, ok := obj["autoMemoryDirectory"]; ok {
			return nil, errors.New("autoMemoryDirectory already configured; use shared memory or resolve the conflict explicitly")
		}
	}
	hash := sha256.Sum256([]byte(root))
	directory := filepath.Join(core.StateHome(), "memory", hex.EncodeToString(hash[:]), profile)
	for _, p := range []string{filepath.Join(core.StateHome(), "memory"), filepath.Dir(directory), directory} {
		if err := core.SecureDirectory(p); err != nil {
			return nil, err
		}
	}
	overlay := map[string]any{}
	for _, s := range spans {
		obj, err := explicitSettings(s.value)
		if err != nil {
			return nil, err
		}
		overlay = merge(overlay, obj)
	}
	overlay["autoMemoryDirectory"] = directory
	id, err := randomID()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(runDir, "memory-"+id+".json")
	if err := core.AtomicWriteJSON(path, overlay); err != nil {
		return nil, err
	}
	return append([]string{"--settings", path}, removeSpans(args, spans)...), nil
}
func processEnv(updates map[string]string) []string {
	env := []string{}
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if _, ok := updates[key]; !ok {
			env = append(env, kv)
		}
	}
	for k, v := range updates {
		env = append(env, k+"="+v)
	}
	return env
}

func waitProcess(command []string, env []string) (int, error) {
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	signals := make(chan os.Signal, 8)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals) // Removes only our subscription; prior Go subscribers survive.
	if err := cmd.Start(); err != nil {
		return 1, errors.New("Cannot start Claude process")
	}
	done := make(chan struct{})
	forwarded := make(chan struct{})
	go func() {
		defer close(forwarded)
		for {
			select {
			case sig := <-signals:
				_ = cmd.Process.Signal(sig)
			case <-done:
				return
			}
		}
	}()
	err := cmd.Wait()
	close(done)
	<-forwarded
	if err == nil {
		return 0, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if status, ok := exit.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			return 128 + int(status.Signal()), nil
		}
		return exit.ExitCode(), nil
	}
	return 1, errors.New("Cannot wait for Claude process")
}

// Run preserves Claude argv and stdio, validates each run's consent/identity through
// core.ReadSwitchRequest, and returns shell-normalized signal statuses.
func Run(args []string, profileOverride string) (int, error) {
	if len(args) > 0 && management[args[0]] {
		return runManagement(args, profileOverride)
	}
	if os.Getenv("CMA_MANAGED") == "1" {
		return 1, errors.New("Nested managed launch refused; exit the current Claude session first")
	}
	opts, err := options(args)
	if err != nil {
		return 1, err
	}
	// --bare ignores any profile login in every mode: reject before any prompt.
	for _, o := range opts {
		if o.name == "--bare" {
			return 1, errors.New("--bare is not supported: it ignores the profile login")
		}
	}
	printing := false
	for _, o := range opts {
		switch o.name {
		case "-p", "--print":
			printing = true
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return 1, err
	}
	root, project, err := loadProject(cwd)
	if err != nil {
		return 1, err
	}
	root, err = canonical(root)
	if err != nil {
		return 1, err
	}
	profile := profileOverride
	if profile == "" && project.Profile != nil {
		profile = *project.Profile
	}
	if profile == "" {
		if !stdinTTY() || printing {
			return 1, errors.New("No profile selected; configure a project or pass --profile (interactive selection requires a TTY)")
		}
		registry, err := loadRegistry()
		if err != nil {
			return 1, err
		}
		choices := []string{}
		for key := range registry.Profiles {
			choices = append(choices, key)
		}
		sort.Strings(choices)
		fmt.Fprintln(os.Stderr, "0: Continue with normal Claude login")
		for i, key := range choices {
			fmt.Fprintf(os.Stderr, "%d: %s\n", i+1, key)
		}
		reader := bufio.NewReader(os.Stdin)
		fmt.Fprint(os.Stderr, "Profile: ")
		line, err := reader.ReadString('\n')
		if err != nil {
			return 1, errors.New("Invalid profile selection")
		}
		selection, err := strconv.Atoi(strings.TrimSpace(line))
		if err != nil || selection < 0 || selection > len(choices) {
			return 1, errors.New("Invalid profile selection")
		}
		if selection == 0 {
			binary, err := ResolveBinary()
			if err != nil {
				return 1, err
			}
			return wait(append([]string{binary}, args...), os.Environ())
		}
		profile = choices[selection-1]
		if err := validateProfile(profile, project); err != nil {
			return 1, err
		}
		fmt.Fprint(os.Stderr, "Save this profile for the project? [y/N]: ")
		line, _ = reader.ReadString('\n')
		if stringSet([]string{"y", "yes", "s", "si", "sí"})[strings.ToLower(strings.TrimSpace(line))] {
			project.Profile = &profile
			if err := saveProject(root, project); err != nil {
				return 1, err
			}
			fmt.Fprintln(os.Stderr, "Keep .claude/accounts.local.json out of Git (use .git/info/exclude).")
		}
	}
	record, err := resolveProfile(profile, project)
	if err != nil {
		return 1, err
	}
	configDir := core.IsConfigDir(record)
	// Mode-dependent option checks run before any token retrieval or process start.
	if err := unsupported(opts, configDir); err != nil {
		return 1, err
	}
	binary, err := ResolveBinary()
	if err != nil {
		return 1, err
	}
	dir := ""
	if configDir {
		if dir, err = prepareDir(profile, false); err != nil {
			return 1, err
		}
	}
	values, _, err := settings(args, root, dir)
	if err != nil {
		return 1, err
	}
	if err := authConflicts(values, !configDir); err != nil {
		return 1, err
	}
	launchID, err := randomID()
	if err != nil {
		return 1, err
	}
	runs := filepath.Join(core.StateHome(), "runs")
	if err := core.SecureDirectory(core.StateHome()); err != nil {
		return 1, err
	}
	if err := core.SecureDirectory(runs); err != nil {
		return 1, err
	}
	runDir := filepath.Join(runs, launchID)
	if err := os.Mkdir(runDir, 0700); err != nil {
		return 1, errors.New("Cannot create private run directory")
	}
	current := append([]string{}, args...)
	for {
		record, err := resolveProfile(profile, project)
		if err != nil {
			return 1, err
		}
		configDir := core.IsConfigDir(record)
		curOpts, err := options(current)
		if err != nil {
			return 1, err
		}
		if err := unsupported(curOpts, configDir); err != nil {
			return 1, err
		}
		dir := ""
		if configDir {
			if dir, err = prepareDir(profile, true); err != nil {
				return 1, err
			}
		}
		values, _, err = settings(current, root, dir)
		if err != nil {
			return 1, err
		}
		if err := authConflicts(values, !configDir); err != nil {
			return 1, err
		}
		// Construct overlays before fetching secrets; conflicts must never retrieve a token.
		childArgs, err := memoryArgsIn(current, project, root, profile, runDir, dir)
		if err != nil {
			return 1, err
		}
		updates := map[string]string{"CMA_LAUNCH_ID": launchID, "CMA_ACTIVE_PROFILE": profile, "CMA_PROJECT_ROOT": root, "CMA_MANAGED": "1", "CMA_RUN_DIR": runDir}
		if configDir {
			updates["CLAUDE_CONFIG_DIR"] = dir
		} else {
			token, err := getToken(profile)
			if err != nil {
				return 1, err
			}
			if strings.TrimSpace(token) == "" {
				return 1, errors.New("Profile has no usable token")
			}
			updates["CLAUDE_CODE_OAUTH_TOKEN"] = token
		}
		env := processEnv(updates)
		expected := core.Run{Version: 1, LaunchID: launchID, Profile: profile, ProjectRoot: root}
		if err := core.AtomicWriteJSON(filepath.Join(runDir, "run.json"), expected); err != nil {
			return 1, err
		}
		status, err := wait(append([]string{binary}, childArgs...), env)
		if err != nil {
			return status, err
		}
		if status != 0 {
			return status, nil
		}
		request, err := core.ReadSwitchRequest(runDir, expected)
		if err != nil {
			return 1, err
		}
		if request == nil {
			return 0, nil
		}
		for _, name := range []string{"switch.json", "session.json"} {
			if err := os.Remove(filepath.Join(runDir, name)); err != nil {
				return 1, errors.New("Cannot consume switch request")
			}
		}
		profile = request.Profile
		newRoot, newProject, err := loadProject(root)
		if err != nil {
			return 1, err
		}
		newRoot, err = canonical(newRoot)
		if err != nil || newRoot != root {
			return 1, errors.New("Project identity changed; restart explicitly")
		}
		project = newProject
		current, err = ResumeArgs(current, request.SessionID)
		if err != nil {
			return 1, err
		}
	}
}

// runManagement runs official Claude management commands. A config-dir profile (explicit
// or best-effort from the project) supplies its CLAUDE_CONFIG_DIR; everything else is an
// untouched pass-through.
func runManagement(args []string, profileOverride string) (int, error) {
	var record core.Profile
	var id string
	if profileOverride != "" {
		// Account administration (login) is not subject to the current project's allowedProfiles.
		var err error
		id = profileOverride
		if record, err = lookupProfile(id); err != nil {
			return 1, err
		}
	} else if os.Getenv("CMA_MANAGED") == "1" {
		// Inside a managed session the inherited environment already carries its configuration.
		return passthrough(args, os.Environ())
	} else if cwd, err := os.Getwd(); err == nil {
		if _, p, err := managementProject(cwd); err == nil && p.Profile != nil {
			if rec, err := resolveProfile(*p.Profile, p); err == nil {
				id, record = *p.Profile, rec
			}
		}
	}
	env := os.Environ()
	if id != "" && core.IsConfigDir(record) {
		// A failure must not fall back to the default account.
		dir, err := prepareDir(id, true)
		if err != nil {
			return 1, err
		}
		env = processEnv(map[string]string{"CLAUDE_CONFIG_DIR": dir})
	}
	return passthrough(args, env)
}

func passthrough(args, env []string) (int, error) {
	binary, err := ResolveBinary()
	if err != nil {
		return 1, err
	}
	return wait(append([]string{binary}, args...), env)
}
