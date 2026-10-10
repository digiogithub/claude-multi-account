//go:build linux

package cli

import (
	"strings"
	"testing"
)

func TestFullHelpDocumentsEveryOption(t *testing.T) {
	isolated(t)
	code, out, _ := call(t, []string{"--help"}, "")
	if code != 0 || !strings.HasPrefix(out, "usage: cma") {
		t.Fatalf("help: %d %s", code, out)
	}
	for _, entry := range helpEntries {
		if strings.HasPrefix(entry.path, "internal ") {
			continue
		}
		if !strings.Contains(out, entry.usage) || !strings.Contains(out, entry.summary) {
			t.Fatalf("full help misses %q", entry.path)
		}
		for _, option := range entry.options {
			if !strings.Contains(out, option[0]) || !strings.Contains(out, option[1]) {
				t.Fatalf("full help misses option %q of %q", option[0], entry.path)
			}
		}
	}
	for _, want := range []string{"CMA_ACCOUNTS_HOME", "CMA_CLAUDE_BINARY"} {
		if !strings.Contains(out, want) {
			t.Fatalf("full help misses %s", want)
		}
	}
}

func TestHelpCommandAndGroupHelp(t *testing.T) {
	isolated(t)
	_, flagged, _ := call(t, []string{"profiles", "add", "--help"}, "")
	code, out, _ := call(t, []string{"help", "profiles", "add"}, "")
	if code != 0 || out != flagged || !strings.Contains(out, "Examples:") || !strings.Contains(out, "--backend BACKEND") {
		t.Fatalf("help profiles add: %d %s", code, out)
	}
	code, out, _ = call(t, []string{"shared", "-h"}, "")
	for _, want := range []string{"cma shared import [--from DIR]", "--from DIR", "--json"} {
		if code != 0 || !strings.Contains(out, want) {
			t.Fatalf("shared help misses %q: %s", want, out)
		}
	}
	if code, _, _ := call(t, []string{"help", "nonexistent"}, ""); code != 2 {
		t.Fatalf("unknown help topic: %d", code)
	}
}

func TestNoArgumentsPrintsHelp(t *testing.T) {
	isolated(t)
	code, out, stderr := call(t, nil, "")
	if code != 2 || out != "" || !strings.HasPrefix(stderr, "usage: cma") || !strings.Contains(stderr, "cma profiles add") {
		t.Fatalf("no arguments: %d %q %q", code, out, stderr)
	}
}

func TestArgumentErrorsPointToCommandHelp(t *testing.T) {
	isolated(t)
	code, _, stderr := call(t, []string{"switch", "one", "--scope", testToken}, "")
	if code != 2 || !strings.Contains(stderr, "usage: cma switch ID") || !strings.Contains(stderr, "Run cma help switch for details") {
		t.Fatalf("switch error: %d %s", code, stderr)
	}
	code, _, stderr = call(t, []string{testToken}, "")
	if code != 2 || !strings.Contains(stderr, "Run cma help for details") {
		t.Fatalf("unknown command error: %d %s", code, stderr)
	}
}

func TestClaudeForwardsArgumentsVerbatim(t *testing.T) {
	isolated(t)
	var gotArgs []string
	var gotProfile string
	old := runLauncher
	t.Cleanup(func() { runLauncher = old })
	runLauncher = func(args []string, profile string) (int, error) { gotArgs, gotProfile = args, profile; return 0, nil }
	for _, c := range []struct {
		argv    []string
		profile string
		want    []string
	}{
		{[]string{"claude", "mcp", "add", "--transport", "http", "docs", "https://example.invalid"}, "", []string{"mcp", "add", "--transport", "http", "docs", "https://example.invalid"}},
		{[]string{"claude", "--profile", "work", "mcp", "list"}, "work", []string{"mcp", "list"}},
		{[]string{"claude", "--profile=work", "--", "--profile", "x"}, "work", []string{"--profile", "x"}},
		{[]string{"claude", "mcp", "--help"}, "", []string{"mcp", "--help"}},
		{[]string{"claude", "-p", "hi"}, "", []string{"-p", "hi"}},
		{[]string{"claude"}, "", []string{}},
	} {
		gotArgs, gotProfile = nil, "unset"
		if code, _, stderr := call(t, c.argv, ""); code != 0 || gotProfile != c.profile || len(gotArgs) != len(c.want) || strings.Join(gotArgs, "\x00") != strings.Join(c.want, "\x00") {
			t.Fatalf("%q: %d %q %q %s", c.argv, code, gotProfile, gotArgs, stderr)
		}
	}
	if code, _, stderr := call(t, []string{"claude", "--profile"}, ""); code != 2 || !strings.Contains(stderr, "Run cma help claude") {
		t.Fatalf("missing profile: %d %s", code, stderr)
	}
	code, out, _ := call(t, []string{"help", "claude"}, "")
	if code != 0 || !strings.Contains(out, "cma claude mcp add") {
		t.Fatalf("help claude: %d %s", code, out)
	}
}
