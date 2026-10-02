//go:build linux

package launcher

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"claude-multi-account/internal/core"
)

// cdSetup registers config-dir profiles cd1 and cd2 in the temp registry, uses the real
// profile lookup (token profiles: one, two), and selects project as the project profile.
func cdSetup(t *testing.T, project string) (root string) {
	t.Helper()
	root, p := isolated(t)
	for _, id := range []string{"cd1", "cd2"} {
		if _, _, err := core.AddConfigDirProfile(core.Profile{ID: id, Label: id}); err != nil {
			t.Fatal(err)
		}
	}
	getProfile = core.GetProfile
	p.Profile = &project
	loadProject = func(string) (string, core.Project, error) { return root, p, nil }
	managementProject = loadProject
	getToken = func(string) (string, error) { t.Fatal("token accessed"); return "", nil }
	return root
}

func profileDir(t *testing.T, id string) string {
	t.Helper()
	dir, err := core.ProfileDir(id)
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	done := make(chan string)
	go func() { b, _ := io.ReadAll(r); done <- string(b) }()
	func() {
		defer func() { os.Stderr = old; w.Close() }()
		fn()
	}()
	return <-done
}

func neverStart(t *testing.T) {
	t.Helper()
	wait = func([]string, []string) (int, error) { t.Fatal("process started"); return 0, nil }
}

func TestConfigDirLaunchEnvironment(t *testing.T) {
	cdSetup(t, "cd1")
	var env map[string]string
	wait = func(command, e []string) (int, error) { env = envMap(e); return 0, nil }
	if status, err := Run([]string{"hello"}, ""); err != nil || status != 0 {
		t.Fatalf("%d %v", status, err)
	}
	dir := profileDir(t, "cd1")
	if env["CLAUDE_CONFIG_DIR"] != dir {
		t.Fatalf("config dir %q", env["CLAUDE_CONFIG_DIR"])
	}
	if _, ok := env["CLAUDE_CODE_OAUTH_TOKEN"]; ok && env["CLAUDE_CODE_OAUTH_TOKEN"] != "" {
		t.Fatal("token in config-dir child")
	}
	for _, k := range []string{"CMA_LAUNCH_ID", "CMA_ACTIVE_PROFILE", "CMA_PROJECT_ROOT", "CMA_MANAGED", "CMA_RUN_DIR"} {
		if env[k] == "" {
			t.Fatalf("missing %s", k)
		}
	}
	if env["CMA_ACTIVE_PROFILE"] != "cd1" {
		t.Fatal("active profile")
	}
	if _, err := os.Lstat(filepath.Join(dir, "projects")); err != nil {
		t.Fatalf("shared links missing: %v", err)
	}
}

func TestConfigDirAmbientOverriddenTokenPreserved(t *testing.T) {
	cdSetup(t, "cd1")
	ambient := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", ambient)
	var env map[string]string
	wait = func(command, e []string) (int, error) { env = envMap(e); return 0, nil }
	if _, err := Run(nil, ""); err != nil || env["CLAUDE_CONFIG_DIR"] != profileDir(t, "cd1") {
		t.Fatalf("%v %q", err, env["CLAUDE_CONFIG_DIR"])
	}
	getToken = func(id string) (string, error) { return "synthetic-" + id, nil }
	if _, err := Run(nil, "one"); err != nil || env["CLAUDE_CONFIG_DIR"] != ambient || env["CLAUDE_CODE_OAUTH_TOKEN"] != "synthetic-one" {
		t.Fatalf("%v %q", err, env["CLAUDE_CONFIG_DIR"])
	}
}

func writeShared(t *testing.T, content string) {
	t.Helper()
	// Create the shared settings through the real link machinery first.
	if _, _, err := core.EnsureProfileDir("cd1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(core.SharedHome(), "settings.json"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestConfigDirSettingsFromProfileDirectory(t *testing.T) {
	cdSetup(t, "cd1")
	neverStart(t)
	writeShared(t, `{"apiKeyHelper":"helper"}`)
	if _, err := Run(nil, ""); err == nil || !strings.Contains(err.Error(), "apiKeyHelper") {
		t.Fatalf("apiKeyHelper accepted: %v", err)
	}
	// Ambient settings must not matter in config-dir mode.
	writeShared(t, `{"forceLoginMethod":"console"}`)
	started := false
	wait = func([]string, []string) (int, error) { started = true; return 0, nil }
	if _, err := Run(nil, ""); err != nil || !started {
		t.Fatalf("forceLoginMethod rejected in config-dir mode: %v", err)
	}
	// Token mode still rejects it through the explicit settings.
	getToken = func(string) (string, error) { t.Fatal("token accessed"); return "", nil }
	if _, err := Run([]string{"--settings", `{"forceLoginMethod":"console"}`}, "one"); err == nil {
		t.Fatal("forceLoginMethod accepted in token mode")
	}
	// Token mode reads ambient settings, not the profile directory.
	home := os.Getenv("HOME")
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte(`{"forceLoginMethod":"console"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(nil, "one"); err == nil {
		t.Fatal("ambient forceLoginMethod accepted in token mode")
	}
}

func TestConfigDirAuthEnvConflicts(t *testing.T) {
	cdSetup(t, "cd1")
	neverStart(t)
	t.Setenv("ANTHROPIC_API_KEY", "fake")
	if _, err := Run(nil, ""); err == nil {
		t.Fatal("env conflict accepted")
	}
	t.Setenv("ANTHROPIC_API_KEY", "")
	if _, err := Run([]string{"--settings", `{"env":{"ANTHROPIC_AUTH_TOKEN":"x"}}`}, ""); err == nil {
		t.Fatal("settings env conflict accepted")
	}
}

func TestUnsupportedOptionsByMode(t *testing.T) {
	cdSetup(t, "cd1")
	var args []string
	wait = func(command, e []string) (int, error) { args = command[1:]; return 0, nil }
	for _, flag := range []string{"--bg", "--background", "--cloud"} {
		args = nil
		if _, err := Run([]string{"prompt", flag}, ""); err != nil || !reflect.DeepEqual(args, []string{"prompt", flag}) {
			t.Fatalf("%s: %v %q", flag, err, args)
		}
		neverToken := func(string) (string, error) { t.Fatal("token accessed"); return "", nil }
		getToken = neverToken
		saved := wait
		neverStart(t)
		if _, err := Run([]string{"prompt", flag}, "one"); err == nil {
			t.Fatalf("%s accepted in token mode", flag)
		}
		wait = saved
	}
	neverStart(t)
	for _, id := range []string{"cd1", "one"} {
		if _, err := Run([]string{"--bare"}, id); err == nil {
			t.Fatalf("--bare accepted for %s", id)
		}
	}
}

func TestConfigDirLinkConflictWarning(t *testing.T) {
	cdSetup(t, "cd1")
	dir := profileDir(t, "cd1")
	if err := os.Remove(filepath.Join(dir, "projects")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "projects"), 0700); err != nil {
		t.Fatal(err)
	}
	started := false
	wait = func([]string, []string) (int, error) { started = true; return 0, nil }
	var err error
	out := captureStderr(t, func() { _, err = Run(nil, "") })
	if err != nil || !started {
		t.Fatalf("launch blocked: %v", err)
	}
	want := "cma: shared link conflict in profile cd1: projects (left untouched)"
	if !strings.Contains(out, want) {
		t.Fatalf("stderr %q", out)
	}
}

func TestManagementConfigDir(t *testing.T) {
	cdSetup(t, "one")
	var env []string
	var command []string
	wait = func(c, e []string) (int, error) { command, env = c, e; return 0, nil }
	check := func(label string) {
		t.Helper()
		m := envMap(env)
		if m["CLAUDE_CONFIG_DIR"] != profileDir(t, "cd1") || m["CMA_MANAGED"] != "" || m["CMA_RUN_DIR"] != "" || m["CMA_ACTIVE_PROFILE"] != "" {
			t.Fatalf("%s: env %q", label, m["CLAUDE_CONFIG_DIR"])
		}
		if !reflect.DeepEqual(command[1:], []string{"auth", "login"}) {
			t.Fatalf("%s: %q", label, command)
		}
	}
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	if _, err := Run([]string{"auth", "login"}, "cd1"); err != nil {
		t.Fatal(err)
	}
	check("explicit")
	if entries, _ := os.ReadDir(filepath.Join(core.StateHome(), "runs")); len(entries) != 0 {
		t.Fatal("run directory created")
	}
	// Project profile, no override.
	p := core.DefaultProject()
	id := "cd1"
	p.Profile = &id
	managementProject = func(string) (string, core.Project, error) { return "", p, nil }
	command, env = nil, nil
	if _, err := Run([]string{"auth", "login"}, ""); err != nil {
		t.Fatal(err)
	}
	check("project")
}

func TestManagementFallbacks(t *testing.T) {
	cdSetup(t, "one")
	ambient := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", ambient)
	var env map[string]string
	started := 0
	wait = func(c, e []string) (int, error) { env = envMap(e); started++; return 0, nil }
	// Unknown explicit profile: error, nothing started.
	if _, err := Run([]string{"auth", "login"}, "missing"); err == nil || started != 0 {
		t.Fatalf("unknown override: %v started=%d", err, started)
	}
	// Token profile explicit: untouched.
	if _, err := Run([]string{"auth", "status"}, "one"); err != nil || env["CLAUDE_CONFIG_DIR"] != ambient || started != 1 {
		t.Fatalf("token override: %v", err)
	}
	// Project load failure without override: untouched pass-through.
	managementProject = func(string) (string, core.Project, error) { return "", core.Project{}, os.ErrNotExist }
	if _, err := Run([]string{"auth", "status"}, ""); err != nil || env["CLAUDE_CONFIG_DIR"] != ambient || started != 2 {
		t.Fatalf("project failure: %v", err)
	}
	// Project profile unknown: untouched pass-through.
	p := core.DefaultProject()
	id := "ghost"
	p.Profile = &id
	managementProject = func(string) (string, core.Project, error) { return "", p, nil }
	if _, err := Run([]string{"--version"}, ""); err != nil || env["CLAUDE_CONFIG_DIR"] != ambient || started != 3 {
		t.Fatalf("unknown project profile: %v", err)
	}
	// Token project profile: untouched.
	id = "one"
	if _, err := Run([]string{"--version"}, ""); err != nil || env["CLAUDE_CONFIG_DIR"] != ambient || started != 4 {
		t.Fatalf("token project profile: %v", err)
	}
}

func TestSwitchBetweenConfigDirProfiles(t *testing.T) {
	cdSetup(t, "cd1")
	count := 0
	wait = func(command, env []string) (int, error) {
		count++
		e := envMap(env)
		if count == 1 {
			if e["CLAUDE_CONFIG_DIR"] != profileDir(t, "cd1") {
				t.Fatal("first dir")
			}
			writeRequest(t, env, func(r *core.SwitchRequest) { r.Profile = "cd2" })
			return 0, nil
		}
		if e["CLAUDE_CONFIG_DIR"] != profileDir(t, "cd2") || e["CMA_ACTIVE_PROFILE"] != "cd2" || e["CLAUDE_CODE_OAUTH_TOKEN"] != "" {
			t.Fatalf("second env %q", e["CLAUDE_CONFIG_DIR"])
		}
		if !reflect.DeepEqual(command[1:], []string{"--resume", sessionID, "prompt"}) {
			t.Fatalf("resume %q", command)
		}
		return 0, nil
	}
	if status, err := Run([]string{"prompt"}, ""); err != nil || status != 0 || count != 2 {
		t.Fatalf("%d %v %d", status, err, count)
	}
}

func TestSwitchMixedModes(t *testing.T) {
	for _, tc := range []struct{ from, to string }{{"cd1", "one"}, {"one", "cd1"}} {
		t.Run(tc.from+">"+tc.to, func(t *testing.T) {
			cdSetup(t, tc.from)
			ambient := t.TempDir()
			t.Setenv("CLAUDE_CONFIG_DIR", ambient)
			tokenFor := map[string]bool{"one": true}
			getToken = func(id string) (string, error) {
				if !tokenFor[id] {
					t.Fatalf("token accessed for %s", id)
				}
				return "synthetic-" + id, nil
			}
			count := 0
			wait = func(command, env []string) (int, error) {
				count++
				e := envMap(env)
				id := tc.from
				if count == 2 {
					id = tc.to
				}
				if tokenFor[id] {
					if e["CLAUDE_CODE_OAUTH_TOKEN"] != "synthetic-"+id || e["CLAUDE_CONFIG_DIR"] != ambient {
						t.Fatalf("token iteration %d: dir %q", count, e["CLAUDE_CONFIG_DIR"])
					}
				} else if e["CLAUDE_CODE_OAUTH_TOKEN"] != "" || e["CLAUDE_CONFIG_DIR"] != profileDir(t, id) {
					t.Fatalf("config-dir iteration %d: dir %q", count, e["CLAUDE_CONFIG_DIR"])
				}
				if count == 1 {
					writeRequest(t, env, func(r *core.SwitchRequest) { r.Profile = tc.to })
				}
				return 0, nil
			}
			if status, err := Run([]string{"prompt"}, ""); err != nil || status != 0 || count != 2 {
				t.Fatalf("%d %v %d", status, err, count)
			}
		})
	}
}

func TestManagementProjectProfilePrepareFailureIsReturned(t *testing.T) {
	cdSetup(t, "cd1")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	neverStart(t)
	ensureProfileDir = func(string) (string, core.LinkReport, error) {
		return "", core.LinkReport{}, errors.New("prepare failed")
	}
	if _, err := Run([]string{"auth", "logout"}, ""); err == nil || err.Error() != "prepare failed" {
		t.Fatalf("error swallowed: %v", err)
	}
}

func TestManagementInsideManagedSession(t *testing.T) {
	cdSetup(t, "cd1")
	t.Setenv("CMA_MANAGED", "1")
	session := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", session)
	managementProject = func(string) (string, core.Project, error) {
		t.Fatal("project re-resolved inside managed session")
		return "", core.Project{}, nil
	}
	var env map[string]string
	var command []string
	wait = func(c, e []string) (int, error) { command, env = c, envMap(e); return 0, nil }
	for _, args := range [][]string{{"auth", "status"}, {"plugin", "list"}, {"--help"}, {"--version"}} {
		command, env = nil, nil
		if status, err := Run(args, ""); err != nil || status != 0 || command == nil {
			t.Fatalf("%v: %d %v", args, status, err)
		}
		if env["CLAUDE_CONFIG_DIR"] != session || env["CMA_MANAGED"] != "1" {
			t.Fatalf("%v: inherited environment changed", args)
		}
	}
	// An explicit override still applies its own directory.
	if _, err := Run([]string{"auth", "login"}, "cd2"); err != nil || env["CLAUDE_CONFIG_DIR"] != profileDir(t, "cd2") {
		t.Fatalf("override: %v %q", err, env["CLAUDE_CONFIG_DIR"])
	}
	// Managed conversations stay refused when nested.
	neverStart(t)
	for _, args := range [][]string{nil, {"hello"}, {"-p", "x"}} {
		if _, err := Run(args, ""); err == nil || !strings.Contains(err.Error(), "Nested") {
			t.Fatalf("%v: nested conversation accepted: %v", args, err)
		}
	}
}

func TestManagementExplicitProfileIgnoresAllowedProfiles(t *testing.T) {
	root := cdSetup(t, "cd2")
	p := core.DefaultProject()
	id := "cd2"
	p.Profile = &id
	p.AllowedProfiles = []string{"cd2"}
	loadProject = func(string) (string, core.Project, error) { return root, p, nil }
	managementProject = loadProject
	var env map[string]string
	wait = func(c, e []string) (int, error) { env = envMap(e); return 0, nil }
	if _, err := Run([]string{"auth", "login"}, "cd1"); err != nil || env["CLAUDE_CONFIG_DIR"] != profileDir(t, "cd1") {
		t.Fatalf("login refused by project policy: %v", err)
	}
	if _, err := Run([]string{"auth", "login"}, "../bad"); err == nil {
		t.Fatal("invalid ID accepted")
	}
	if _, err := Run([]string{"auth", "login"}, "missing"); err == nil {
		t.Fatal("unknown profile accepted")
	}
	neverStart(t)
	if _, err := Run([]string{"hello"}, "cd1"); err == nil || !strings.Contains(err.Error(), "allowedProfiles") {
		t.Fatalf("managed launch not refused: %v", err)
	}
}

func TestBareRejectedBeforeSelector(t *testing.T) {
	root, project := isolated(t)
	project.Profile = nil
	loadProject = func(string) (string, core.Project, error) { return root, project, nil }
	stdinTTY = func() bool { return true }
	loadRegistry = func() (core.Registry, error) { t.Fatal("selector reached"); return core.Registry{}, nil }
	saveProject = func(string, core.Project) error { t.Fatal("project saved"); return nil }
	neverStart(t)
	if _, err := Run([]string{"--bare"}, ""); err == nil || !strings.Contains(err.Error(), "--bare") {
		t.Fatalf("bare accepted: %v", err)
	}
}
