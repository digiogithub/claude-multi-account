//go:build linux

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"claude-multi-account/internal/core"
)

const testToken = "synthetic-token"

func isolated(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "CMA_") || strings.HasPrefix(name, "CLAUDE_") || strings.HasPrefix(name, "ANTHROPIC_") || strings.HasPrefix(name, "AWS_") || strings.HasPrefix(name, "GOOGLE_") || strings.HasPrefix(name, "AZURE_") {
			t.Setenv(name, "")
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("CMA_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("CMA_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("CMA_ACCOUNTS_HOME", filepath.Join(home, "accounts"))
	t.Setenv("CMA_SHARED_HOME", "")
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "claude"))
	project := filepath.Join(home, "project")
	if err := os.MkdirAll(filepath.Join(project, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
	return project
}

func call(t *testing.T, args []string, input string) (int, string, string) {
	t.Helper()
	var out, stderr bytes.Buffer
	code := execute(args, strings.NewReader(input), &out, &stderr)
	if strings.Contains(out.String()+stderr.String(), testToken) {
		t.Fatal("Secret exposed in CLI output")
	}
	return code, out.String(), stderr.String()
}
func addArgs(id string) []string {
	return []string{"profiles", "add", id, "--label", "Example", "--email", "user@example.invalid", "--organization", "Example", "--organization-id", "00000000-0000-0000-0000-000000000001", "--mode", "token", "--backend", "file", "--token-stdin"}
}
func add(t *testing.T, id string) {
	t.Helper()
	if code, _, err := call(t, addArgs(id), testToken+"\n"); code != 0 {
		t.Fatalf("add failed %d: %s", code, err)
	}
}

func TestProfileCRUDRedacted(t *testing.T) {
	isolated(t)
	add(t, "one")
	code, out, err := call(t, []string{"profiles", "list", "--json"}, "")
	if code != 0 {
		t.Fatal(err)
	}
	var items []core.Profile
	if json.Unmarshal([]byte(out), &items) != nil || len(items) != 1 || items[0].ID != "one" {
		t.Fatal("Invalid profiles JSON")
	}
	if code, _, err := call(t, []string{"profiles", "renew", "one", "--token-stdin"}, testToken+"-renewed\r\n"); code != 0 {
		t.Fatal(err)
	}
	token, e := core.GetToken("one")
	if e != nil || token != testToken+"-renewed" {
		t.Fatal("Renew did not persist supplied token")
	}
	if code, _, err := call(t, []string{"profiles", "remove", "one"}, ""); code != 0 {
		t.Fatal(err)
	}
	if code, out, err := call(t, []string{"profiles", "list", "--json"}, ""); code != 0 || strings.TrimSpace(out) != "[]" {
		t.Fatalf("Empty list: %d %s %s", code, out, err)
	}
}

func TestAddWithOptionalAccountMetadata(t *testing.T) {
	for _, test := range []struct {
		name    string
		options []string
	}{
		{"unknown", nil},
		{"email", []string{"--email", "user@example.invalid"}},
		{"organization", []string{"--organization", "Example"}},
		{"organization-id", []string{"--organization-id", "00000000-0000-0000-0000-000000000001"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			isolated(t)
			args := append([]string{"profiles", "add", "one", "--label", "Personal", "--mode", "token", "--backend", "file", "--token-stdin"}, test.options...)
			if code, _, err := call(t, args, testToken+"\n"); code != 0 {
				t.Fatal(err)
			}
			profile, err := core.GetProfile("one")
			if err != nil || profile.Label != "Personal" || profile.Verification != "unverified" {
				t.Fatal("Invalid minimal profile", err)
			}
			if len(test.options) > 0 {
				value := map[string]string{"email": profile.Email, "organization": profile.Organization, "organization-id": profile.OrganizationID}[test.name]
				if value != test.options[1] {
					t.Fatal("Supplied optional metadata was lost")
				}
			}
			code, out, stderr := call(t, []string{"profiles", "list", "--json"}, "")
			var items []core.Profile
			if code != 0 || json.Unmarshal([]byte(out), &items) != nil || len(items) != 1 || !reflect.DeepEqual(items[0], profile) {
				t.Fatal("Optional metadata did not round-trip", stderr)
			}
			if test.name == "unknown" {
				if profile.Email != "" || profile.Organization != "" || profile.OrganizationID != "" {
					t.Fatal("Account metadata was invented")
				}
				if code, out, err := call(t, []string{"profiles", "list"}, ""); code != 0 || !strings.Contains(out, "\tunknown\tunknown\t") {
					t.Fatal("Unknown account metadata not displayed", err)
				}
			}
			if code, _, err := call(t, []string{"profiles", "renew", "one", "--token-stdin"}, testToken+"-renewed\n"); code != 0 {
				t.Fatal(err)
			}
			renewed, err := core.GetProfile("one")
			if err != nil || renewed.Email != profile.Email || renewed.Organization != profile.Organization || renewed.OrganizationID != profile.OrganizationID {
				t.Fatal("Renewal changed account metadata", err)
			}
			if code, _, err := call(t, []string{"profiles", "remove", "one"}, ""); code != 0 {
				t.Fatal(err)
			}
		})
	}
}

func TestAddStillRequiresLabelAndValidMetadata(t *testing.T) {
	isolated(t)
	if code, _, _ := call(t, []string{"profiles", "add", "one", "--mode", "token", "--backend", "file", "--token-stdin"}, testToken+"\n"); code != 2 {
		t.Fatal("Missing label accepted")
	}
	for _, option := range []string{"--label", "--email", "--organization", "--organization-id"} {
		args := []string{"profiles", "add", "one", "--label", "Personal", "--mode", "token", "--backend", "file", "--token-stdin", option, "invalid\ntext"}
		if code, _, _ := call(t, args, testToken+"\n"); code != 1 {
			t.Fatal("Invalid supplied metadata accepted")
		}
	}
}

func TestSecretInputFailsClosed(t *testing.T) {
	isolated(t)
	add(t, "one")
	if code, _, err := call(t, []string{"profiles", "renew", "one"}, testToken); code != 1 || !strings.Contains(err, "explicit --token-stdin") {
		t.Fatalf("Implicit stdin accepted: %d %s", code, err)
	}
	for _, input := range []string{"", "first\nsecond\n", strings.Repeat("x", 16385), "hello world", string([]byte{0xff})} {
		if code, _, _ := call(t, []string{"profiles", "renew", "one", "--token-stdin"}, input); code != 1 {
			t.Fatal("Invalid token accepted")
		}
	}
	token, err := core.GetToken("one")
	if err != nil || token != testToken {
		t.Fatal("Failed input changed token")
	}
}

func TestArgumentErrorsNeverEchoValues(t *testing.T) {
	isolated(t)
	for _, args := range [][]string{
		{"profiles", "renew", "one", "--token=" + testToken},
		{"profiles", "add", testToken, "--backend", testToken},
		{"switch", "one", "--scope", testToken},
		{"project", "memory", testToken},
		{testToken},
		{"launch", "--token", testToken},
	} {
		if code, _, _ := call(t, args, ""); code != 2 {
			t.Fatalf("Expected usage failure for command %s", args[0])
		}
	}
}

func TestProjectAndStatus(t *testing.T) {
	isolated(t)
	add(t, "one")
	add(t, "two")
	for _, args := range [][]string{{"project", "init", "--profile", "one"}, {"project", "use", "two"}, {"project", "memory", "per-profile"}} {
		if code, _, err := call(t, args, ""); code != 0 {
			t.Fatal(err)
		}
	}
	if code, _, _ := call(t, []string{"project", "init"}, ""); code != 1 {
		t.Fatal("Duplicate init accepted")
	}
	code, out, err := call(t, []string{"status", "--json"}, "")
	if code != 0 {
		t.Fatal(err)
	}
	var value struct {
		Project core.Project `json:"project"`
		Expiry  string       `json:"expiry_status"`
	}
	if e := json.Unmarshal([]byte(out), &value); e != nil {
		t.Fatal(e)
	}
	if value.Project.Profile == nil || *value.Project.Profile != "two" || value.Project.Memory.Mode != "per-profile" || value.Expiry != "unknown" {
		t.Fatal("Incorrect project status")
	}
	if code, out, err := call(t, []string{"status"}, ""); code != 0 || !strings.Contains(out, "Configured profile: two") {
		t.Fatalf("Status: %d %s %s", code, out, err)
	}
}

func TestLaunchArgumentBoundary(t *testing.T) {
	for _, test := range []struct {
		args      []string
		profile   string
		forwarded []string
	}{
		{[]string{"--profile", "one", "--", "--resume", "exact", "--model", "sonnet"}, "one", []string{"--resume", "exact", "--model", "sonnet"}},
		{[]string{"--", "--profile", "claude-profile", "prompt"}, "", []string{"--profile", "claude-profile", "prompt"}},
		{[]string{"--profile=one", "prompt", "--model", "sonnet"}, "one", []string{"prompt", "--model", "sonnet"}},
	} {
		profile, args, err := launchArgs(test.args)
		if err != nil || profile != test.profile || !reflect.DeepEqual(args, test.forwarded) {
			t.Fatal("Launch boundary changed Claude args")
		}
	}
}

func TestSessionPayloadStrictBoundedHookFields(t *testing.T) {
	session, err := readSession(strings.NewReader(`{"session_id":"exact-id","cwd":"/tmp","hook_event_name":"SessionStart","transcript_path":"unused","source":"startup"}`))
	if err != nil || session.SessionID != "exact-id" || session.Cwd != "/tmp" {
		t.Fatal("Normal Claude hook rejected")
	}
	for _, raw := range []string{`null`, `[]`, `{}`, `{"session_id":12,"cwd":"/tmp"}`, `{"session_id":"exact-id","cwd":null}`, `{"session_id":"exact-id","cwd":"relative"}`, `{"session_id":"exact-id","cwd":"/tmp"} {}`, `{"session_id":"first","session_id":"second","cwd":"/tmp"}`, `{"session_id":"exact-id","cwd":"/tmp","extra":{"duplicate":1,"duplicate":2}}`, strings.Repeat("x", 1024*1024+1)} {
		if _, err := readSession(strings.NewReader(raw)); err == nil {
			t.Fatal("Invalid hook payload accepted")
		}
	}
}

func TestInternalRegistrationAndSwitch(t *testing.T) {
	root := isolated(t)
	if code, _, err := call(t, []string{"internal", "session-start"}, "not JSON"); code != 0 || !strings.Contains(err, "unmanaged") {
		t.Fatal("Unmanaged hook contract")
	}
	add(t, "one")
	add(t, "two")
	launch := strings.Repeat("a", 32)
	runDir := filepath.Join(core.StateHome(), "runs", launch)
	if err := core.SecureDirectory(runDir); err != nil {
		t.Fatal(err)
	}
	run := core.Run{Version: 1, LaunchID: launch, Profile: "one", ProjectRoot: root}
	if err := core.AtomicWriteJSON(filepath.Join(runDir, "run.json"), run); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CMA_MANAGED", "1")
	t.Setenv("CMA_RUN_DIR", runDir)
	t.Setenv("CMA_LAUNCH_ID", launch)
	t.Setenv("CMA_ACTIVE_PROFILE", "one")
	t.Setenv("CMA_PROJECT_ROOT", root)
	if code, _, _ := call(t, []string{"internal", "session-start"}, "not JSON"); code != 1 {
		t.Fatal("Malformed hook accepted")
	}
	raw, _ := json.Marshal(map[string]any{"session_id": "exact-id", "cwd": root, "source": "startup"})
	if code, _, err := call(t, []string{"internal", "session-start"}, string(raw)); code != 0 {
		t.Fatal(err)
	}
	if code, out, err := call(t, []string{"internal", "session-info"}, ""); code != 0 || !strings.Contains(out, "exact-id") {
		t.Fatalf("Session info: %d %s %s", code, out, err)
	}
	if code, _, _ := call(t, []string{"switch", "two"}, ""); code != 1 {
		t.Fatal("Consent not required")
	}
	if code, out, err := call(t, []string{"switch", "two", "--scope", "project", "--confirm-context-transfer"}, ""); code != 0 || !strings.Contains(out, "/exit") {
		t.Fatalf("Switch: %d %s %s", code, out, err)
	}
	request, err := core.ReadSwitchRequest(runDir, run)
	if err != nil || request == nil || request.SessionID != "exact-id" {
		t.Fatal("Switch lost exact session")
	}
}

func TestCompiledCLIPipeAndFakeRuntime(t *testing.T) {
	// Build only repository code. The child receives an isolated environment and fake Claude.
	_, source, _, _ := runtime.Caller(0)
	repository := filepath.Clean(filepath.Join(filepath.Dir(source), "../.."))
	binary := filepath.Join(t.TempDir(), "cma")
	build := exec.Command("go", "build", "-o", binary, "./cmd/cma")
	build.Dir = repository
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("Build: %v %s", err, output)
	}
	root := isolated(t)
	run := func(args []string, input string) (int, string) {
		t.Helper()
		cmd := exec.Command(binary, args...)
		cmd.Dir = root
		cmd.Stdin = strings.NewReader(input)
		cmd.Env = os.Environ()
		output, err := cmd.CombinedOutput()
		if strings.Contains(string(output), testToken) {
			t.Fatal("Compiled CLI leaked token")
		}
		code := 0
		if err != nil {
			if exit, ok := err.(*exec.ExitError); ok {
				code = exit.ExitCode()
			} else {
				t.Fatal(err)
			}
		}
		return code, string(output)
	}
	if code, out := run([]string{"profiles", "add", "one", "--label", "Personal", "--mode", "token", "--backend", "file", "--token-stdin"}, testToken+"\n"); code != 0 {
		t.Fatalf("Import: %d %s", code, out)
	}
	if code, out := run([]string{"profiles", "renew", "one"}, testToken+"\n"); code != 1 || !strings.Contains(out, "explicit --token-stdin") {
		t.Fatalf("Implicit pipe accepted: %d %s", code, out)
	}
	capture := filepath.Join(t.TempDir(), "args")
	fake := filepath.Join(t.TempDir(), "claude")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$CMA_TEST_CAPTURE\"\nexit 27\n"
	if err := os.WriteFile(fake, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CMA_CLAUDE_BINARY", fake)
	t.Setenv("CMA_TEST_CAPTURE", capture)
	if code, out := run([]string{"launch", "--profile", "one", "--", "--model", "sonnet", "prompt with spaces"}, ""); code != 27 {
		t.Fatalf("Fake launch: %d %s", code, out)
	}
	raw, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "--model\nsonnet\nprompt with spaces\n") {
		t.Fatal("Compiled launch changed argv")
	}
	// Exercise an actual fake Claude hook calling the compiled internal endpoint.
	t.Setenv("CMA_TEST_CLI", binary)
	script = "#!/bin/sh\nprintf '{\"session_id\":\"compiled-exact\",\"cwd\":\"%s\",\"source\":\"startup\"}\\n' \"$PWD\" | \"$CMA_TEST_CLI\" internal session-start || exit 91\n\"$CMA_TEST_CLI\" internal session-info > \"$CMA_TEST_CAPTURE\" || exit 92\nexit 0\n"
	if err := os.WriteFile(fake, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if code, out := run([]string{"launch", "--profile", "one", "--", "prompt"}, ""); code != 0 {
		t.Fatalf("Compiled hook: %d %s", code, out)
	}
	raw, err = os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	var session core.Session
	if err := json.Unmarshal(raw, &session); err != nil || session.SessionID != "compiled-exact" || session.ProjectRoot != root {
		t.Fatal("Compiled hook lost exact session identity")
	}
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nkill -TERM $$\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if code, out := run([]string{"launch", "--", "--version"}, ""); code != 143 {
		t.Fatalf("Signal normalization: %d %s", code, out)
	}
}
