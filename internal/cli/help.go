//go:build linux

package cli

import (
	"fmt"
	"strings"
)

// helpEntry documents one command path. Help text never interpolates argv.
type helpEntry struct {
	path      string
	usage     string
	summary   string
	details   []string
	arguments [][2]string
	options   [][2]string
	examples  []string
}

var helpEntries = []helpEntry{
	{
		path:    "profiles list",
		usage:   "cma profiles list [--json]",
		summary: "List registered profiles with their mode, login or expiry state.",
		options: [][2]string{
			{"--json", "Print the profiles as a JSON array instead of tab-separated lines."},
		},
	},
	{
		path:    "profiles add",
		usage:   "cma profiles add ID --label LABEL [--mode {config-dir,token}] [--email EMAIL] [--organization ORGANIZATION] [--organization-id ID] [token mode only: --expires-at DATE --backend {auto,secret-service,file} --token-stdin]",
		summary: "Register a new profile.",
		details: []string{
			"config-dir (default) creates the profile's own Claude directory and links the",
			"shared entries into it; log in afterwards with cma profiles login ID.",
			"token stores a claude setup-token token, read from a hidden prompt (inference-only:",
			"no remote control or other login-dependent features).",
			"Email and organization values are declared metadata; they are never verified.",
		},
		arguments: [][2]string{
			{"ID", "Profile identifier: 1-64 letters, digits, '_' or '-', starting with a letter or digit."},
		},
		options: [][2]string{
			{"--label LABEL", "Required. Human-readable name shown in listings."},
			{"--mode MODE", "config-dir (default) or token."},
			{"--email EMAIL", "Optional declared account email."},
			{"--organization NAME", "Optional declared organization name."},
			{"--organization-id ID", "Optional declared organization UUID."},
			{"--expires-at DATE", "Token mode only. Known ISO expiration date; otherwise shown as unknown."},
			{"--backend BACKEND", "Token mode only. auto (default, Secret Service), secret-service, or file (plaintext, 0600)."},
			{"--token-stdin", "Token mode only. Read the token from stdin instead of a hidden terminal prompt."},
		},
		examples: []string{
			`cma profiles add work --label "Work" --email me@example.com`,
			`cma profiles add ci --label "CI" --mode token --backend file`,
		},
	},
	{
		path:    "profiles login",
		usage:   "cma profiles login ID",
		summary: "Run the official Claude login inside a config-dir profile's directory.",
		arguments: [][2]string{
			{"ID", "Existing config-dir profile. Token profiles use cma profiles renew."},
		},
	},
	{
		path:    "profiles renew",
		usage:   "cma profiles renew ID [--token-stdin]",
		summary: "Replace the stored token of a token-mode profile.",
		arguments: [][2]string{
			{"ID", "Existing token-mode profile."},
		},
		options: [][2]string{
			{"--token-stdin", "Read the token from stdin instead of a hidden terminal prompt."},
		},
	},
	{
		path:    "profiles remove",
		usage:   "cma profiles remove ID [--purge]",
		summary: "Unregister a profile. A config-dir profile's directory is kept unless --purge is given.",
		arguments: [][2]string{
			{"ID", "Existing profile."},
		},
		options: [][2]string{
			{"--purge", "config-dir only. Also delete the profile directory, including its login."},
		},
	},
	{
		path:    "shared path",
		usage:   "cma shared path",
		summary: "Print the shared directory linked into every config-dir profile.",
	},
	{
		path:    "shared import",
		usage:   "cma shared import [--from DIR]",
		summary: "Copy skills, settings, history and other shareable entries into the shared directory.",
		details: []string{"Existing shared entries are skipped; the source directory is never modified."},
		options: [][2]string{
			{"--from DIR", "Source Claude directory (default: ~/.claude)."},
		},
	},
	{
		path:    "shared sync",
		usage:   "cma shared sync [--json]",
		summary: "Re-create missing shared links in every config-dir profile and report conflicts.",
		options: [][2]string{
			{"--json", "Print the per-profile link reports as JSON."},
		},
	},
	{
		path:    "project init",
		usage:   "cma project init [--profile ID]",
		summary: "Create .claude/accounts.local.json for the current project.",
		options: [][2]string{
			{"--profile ID", "Default profile for this project; without it the launcher asks."},
		},
	},
	{
		path:    "project use",
		usage:   "cma project use ID",
		summary: "Set the project's default profile for future launches.",
		arguments: [][2]string{
			{"ID", "Existing profile, permitted by the project's allowedProfiles policy if any."},
		},
	},
	{
		path:    "project memory",
		usage:   "cma project memory {shared,per-profile}",
		summary: "Choose whether automatic memory is shared by all profiles or kept per profile.",
		arguments: [][2]string{
			{"shared", "All profiles use the same project memory (default)."},
			{"per-profile", "Each profile gets its own memory folder; existing memory is not migrated."},
		},
	},
	{
		path:    "status",
		usage:   "cma status [--json]",
		summary: "Show the current project, its profile, the active managed session and login state.",
		options: [][2]string{
			{"--json", "Print the status as JSON."},
		},
	},
	{
		path:    "switch",
		usage:   "cma switch ID [--scope {session,project}] [--confirm-context-transfer]",
		summary: "Queue a switch of the running managed session to another profile.",
		details: []string{
			"Run inside a managed session; exit Claude with /exit and the wrapper resumes the",
			"same conversation under the new profile. No process is killed.",
		},
		arguments: [][2]string{
			{"ID", "Destination profile."},
		},
		options: [][2]string{
			{"--scope SCOPE", "session (default): only this session. project: also make it the project default."},
			{"--confirm-context-transfer", "Confirm that the conversation context may be sent under the destination profile."},
		},
	},
	{
		path:    "launch",
		usage:   "cma launch [--profile ID] [-- CLAUDE_ARGS...]",
		summary: "Start the official Claude with the project's profile (this is what claude-cma runs).",
		details: []string{"Everything after -- is passed to Claude unchanged."},
		options: [][2]string{
			{"--profile ID", "Use this profile for this launch instead of the project's default."},
		},
		examples: []string{
			"cma launch --profile work -- --continue",
			"claude-cma -p \"summarize this repo\"",
		},
	},
	{
		path:    "claude",
		usage:   "cma claude [--profile ID] [--] CLAUDE_ARGS...",
		summary: "Run the official claude with the project's profile, forwarding every argument verbatim.",
		details: []string{
			"Subcommands such as mcp, plugin, agents or auth act on the profile's own configuration",
			"directory instead of the default account; other arguments start a managed session as launch does.",
			"--help after claude is passed to Claude; use cma help claude for this text.",
		},
		options: [][2]string{
			{"--profile ID", "Must come first. Use this profile instead of the project's default."},
		},
		examples: []string{
			"cma claude mcp add --transport http docs https://example.com/mcp",
			"cma claude mcp list",
			"cma claude --profile work plugin install NAME",
			`cma claude -p "summarize this repo"`,
		},
	},
	{
		path:    "install",
		usage:   "cma install [--target PATH] [--shell]",
		summary: "Copy this binary to a stable location and create the claude-cma shim. Prints a JSON report.",
		options: [][2]string{
			{"--target PATH", "Installation root (default: ~/.local/share/claude-multi-account)."},
			{"--shell", "Also add a managed claude function to .bashrc or .zshrc."},
		},
	},
	{
		path:    "uninstall",
		usage:   "cma uninstall [--target PATH]",
		summary: "Remove the installed binary, shims and managed shell block. Profiles are kept.",
		options: [][2]string{
			{"--target PATH", "Installation root (default: ~/.local/share/claude-multi-account)."},
		},
	},
	{
		path:    "internal session-start",
		usage:   "cma internal session-start",
		summary: "Plugin hook: register the managed session from a SessionStart JSON payload on stdin.",
	},
	{
		path:    "internal session-info",
		usage:   "cma internal session-info",
		summary: "Plugin helper: print the registered managed session as JSON.",
	},
}

var helpGroups = []struct{ path, title, usage, summary string }{
	{"profiles", "Profiles", "cma profiles {list,add,login,renew,remove} ...", "Manage accounts (config-dir or token profiles)."},
	{"shared", "Shared directory", "cma shared {path,import,sync} ...", "Manage the directory shared by config-dir profiles."},
	{"project", "Project", "cma project {init,use,memory} ...", "Configure the profile and memory of the current project."},
	{"internal", "Internal", "cma internal {session-start,session-info}", "Commands used by the accounts plugin; not for direct use."},
}

const helpEnvironment = `Environment:
  CMA_ACCOUNTS_HOME   Accounts root holding shared/ and profiles/<id> (default: ~/.claude-accounts).
  CMA_SHARED_HOME     Shared directory (default: $CMA_ACCOUNTS_HOME/shared).
  CMA_CONFIG_HOME     Registry directory (default: $XDG_CONFIG_HOME/claude-multi-account).
  CMA_STATE_HOME      Session state directory (default: $XDG_STATE_HOME/claude-multi-account).
  CMA_CLAUDE_BINARY   Absolute path to the official claude binary (default: found in PATH).`

func findHelpEntry(path string) (helpEntry, bool) {
	for _, entry := range helpEntries {
		if entry.path == path {
			return entry, true
		}
	}
	return helpEntry{}, false
}

func helpUsage(path string) (string, bool) {
	if path == "" {
		return "cma {profiles,shared,project,status,switch,launch,claude,install,uninstall} ...", true
	}
	for _, group := range helpGroups {
		if group.path == path {
			return group.usage, true
		}
	}
	if entry, ok := findHelpEntry(path); ok {
		return entry.usage, true
	}
	return "", false
}

func writeRows(b *strings.Builder, title string, rows [][2]string, indent string) {
	if len(rows) == 0 {
		return
	}
	width := 0
	for _, row := range rows {
		width = max(width, len(row[0]))
	}
	fmt.Fprintf(b, "%s%s:\n", indent, title)
	for _, row := range rows {
		fmt.Fprintf(b, "%s  %-*s  %s\n", indent, width, row[0], row[1])
	}
}

// writeEntry renders a command; compact output omits details and examples.
func writeEntry(b *strings.Builder, entry helpEntry, compact bool) {
	if compact {
		fmt.Fprintf(b, "  %s\n      %s\n", entry.usage, entry.summary)
		writeRows(b, "Arguments", entry.arguments, "      ")
		writeRows(b, "Options", entry.options, "      ")
		return
	}
	fmt.Fprintf(b, "\n%s\n", entry.summary)
	for _, line := range entry.details {
		fmt.Fprintf(b, "%s\n", line)
	}
	if len(entry.arguments) > 0 {
		b.WriteString("\n")
		writeRows(b, "Arguments", entry.arguments, "")
	}
	if len(entry.options) > 0 {
		b.WriteString("\n")
		writeRows(b, "Options", entry.options, "")
	}
	if len(entry.examples) > 0 {
		b.WriteString("\nExamples:\n")
		for _, example := range entry.examples {
			fmt.Fprintf(b, "  %s\n", example)
		}
	}
}

// helpText renders the help for a known command path, or the full reference for "".
func helpText(path string) (string, bool) {
	usage, ok := helpUsage(path)
	if !ok {
		return "", false
	}
	var b strings.Builder
	b.WriteString("usage: " + usage + "\n")
	if entry, ok := findHelpEntry(path); ok {
		writeEntry(&b, entry, false)
		return strings.TrimRight(b.String(), "\n"), true
	}
	if path == "" {
		b.WriteString("\nManage several Claude Code accounts and pick one per project.\n")
	}
	section := func(prefix, title string) {
		fmt.Fprintf(&b, "\n%s:\n", title)
		for _, entry := range helpEntries {
			if strings.HasPrefix(entry.path, prefix) && (prefix != "" || !strings.Contains(entry.path, " ")) {
				writeEntry(&b, entry, true)
			}
		}
	}
	for _, group := range helpGroups {
		if path == group.path {
			b.WriteString("\n" + group.summary + "\n")
			section(group.path+" ", "Commands")
		} else if path == "" && group.path != "internal" {
			section(group.path+" ", group.title)
		}
	}
	if path == "" {
		section("", "Session and installation")
		b.WriteString("\n" + helpEnvironment + "\n")
		b.WriteString("\nRun cma COMMAND --help (or cma help COMMAND) for details and examples.\n")
		b.WriteString("Internal plugin commands: cma internal --help.\n")
	}
	return strings.TrimRight(b.String(), "\n"), true
}

// commandPath returns the longest known command path at the start of args.
func commandPath(args []string) string {
	path := ""
	for i := range args {
		candidate := strings.Join(args[:i+1], " ")
		if _, ok := helpUsage(candidate); !ok {
			break
		}
		path = candidate
	}
	return path
}

func help(args []string) (string, bool) {
	// cma claude forwards --help to the official Claude like any other argument.
	if len(args) > 0 && args[0] == "claude" {
		return "", false
	}
	if len(args) > 0 && args[0] == "help" {
		path := strings.Join(args[1:], " ")
		if _, ok := helpUsage(path); !ok {
			return "", false
		}
		return helpText(path)
	}
	for i, arg := range args {
		if arg == "--" {
			break
		}
		if arg != "--help" && arg != "-h" {
			continue
		}
		// Only command prefixes are help requests, never Claude's own remainder.
		return helpText(strings.Join(args[:i], " "))
	}
	return "", false
}
