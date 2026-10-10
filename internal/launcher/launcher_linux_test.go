//go:build linux

package launcher

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"claude-multi-account/internal/core"
)

const sessionID = "11111111-1111-1111-1111-111111111111"

func isolated(t *testing.T) (string, core.Project) {
	t.Helper()
	base := t.TempDir()
	for _, key := range authEnv {
		t.Setenv(key, "")
	}
	for _, key := range []string{"CMA_MANAGED", "CMA_RUN_DIR", "CMA_ACTIVE_PROFILE", "CMA_LAUNCH_ID", "CMA_PROJECT_ROOT", "CLAUDE_CONFIG_DIR", "CMA_PROFILE", "XDG_CONFIG_HOME", "XDG_STATE_HOME"} {
		t.Setenv(key, "")
	}
	t.Setenv("HOME", base)
	t.Setenv("CMA_STATE_HOME", filepath.Join(base, "state"))
	t.Setenv("CMA_CONFIG_HOME", filepath.Join(base, "config"))
	binary := filepath.Join(base, "claude")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CMA_CLAUDE_BINARY", binary)
	root := filepath.Join(base, "project")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	profile := "one"
	project := core.DefaultProject()
	project.Profile = &profile
	// The protocol reader deliberately uses real core validation, not launcher seams.
	for _, id := range []string{"one", "two"} {
		_, err := core.AddProfile(core.Profile{ID: id, Label: id, Email: "test@example.invalid", Organization: "test", OrganizationID: sessionID}, "synthetic-"+id, "file")
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("CMA_ACCOUNTS_HOME", filepath.Join(base, "accounts"))
	t.Setenv("CMA_SHARED_HOME", "")
	oldLoad, oldRegistry, oldProfile, oldToken, oldSave, oldWait, oldTTY := loadProject, loadRegistry, getProfile, getToken, saveProject, wait, stdinTTY
	oldEnsure, oldMgmt := ensureProfileDir, managementProject
	t.Cleanup(func() {
		loadProject, loadRegistry, getProfile, getToken, saveProject, wait, stdinTTY = oldLoad, oldRegistry, oldProfile, oldToken, oldSave, oldWait, oldTTY
		ensureProfileDir, managementProject = oldEnsure, oldMgmt
	})
	managementProject = func(string) (string, core.Project, error) { return root, project, nil }
	loadProject = func(string) (string, core.Project, error) { return root, project, nil }
	loadRegistry = func() (core.Registry, error) {
		return core.Registry{Version: 1, Profiles: map[string]core.Profile{"one": {ID: "one"}, "two": {ID: "two"}}}, nil
	}
	getProfile = func(id string) (core.Profile, error) { return core.Profile{ID: id}, nil }
	getToken = func(id string) (string, error) { return "synthetic-" + id, nil }
	saveProject = func(string, core.Project) error { return nil }
	wait = waitProcess
	stdinTTY = func() bool { return false }
	return root, project
}
func envMap(env []string) map[string]string {
	out := map[string]string{}
	for _, item := range env {
		k, v, _ := strings.Cut(item, "=")
		out[k] = v
	}
	return out
}
func writeRequest(t *testing.T, env []string, update func(*core.SwitchRequest)) {
	t.Helper()
	e := envMap(env)
	dir := e["CMA_RUN_DIR"]
	session := core.Session{LaunchID: e["CMA_LAUNCH_ID"], SessionID: sessionID, Cwd: e["CMA_PROJECT_ROOT"], ProjectRoot: e["CMA_PROJECT_ROOT"]}
	request := core.SwitchRequest{LaunchID: session.LaunchID, SessionID: sessionID, Profile: "two", Scope: "session", ProjectRoot: session.ProjectRoot, Confirmed: true}
	if update != nil {
		update(&request)
	}
	if err := core.AtomicWriteJSON(filepath.Join(dir, "session.json"), session); err != nil {
		t.Fatal(err)
	}
	if err := core.AtomicWriteJSON(filepath.Join(dir, "switch.json"), request); err != nil {
		t.Fatal(err)
	}
}
func TestResumeArgs(t *testing.T) {
	cases := []struct{ in, want []string }{
		{[]string{"--resume", sessionID, "prompt"}, []string{"prompt"}},
		{[]string{"--resume", "session-name"}, []string{}},
		{[]string{"--resume", "session-name", "prompt"}, []string{"prompt"}},
		{[]string{"--resume=" + sessionID, "--continue", "prompt"}, []string{"prompt"}},
		{[]string{"--session-id", sessionID, "--system-prompt", "--continue", "prompt"}, []string{"--system-prompt", "--continue", "prompt"}},
		{[]string{"--", "--resume", "prompt"}, []string{"--", "--resume", "prompt"}},
		{[]string{"prompt", "--continue", "--model", "sonnet"}, []string{"prompt", "--model", "sonnet"}},
		{[]string{"--fork-session", "-r", "session-name"}, []string{}},
	}
	for _, c := range cases {
		got, err := ResumeArgs(c.in, sessionID)
		want := append([]string{"--resume", sessionID}, c.want...)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%q => %q, %v; want %q", c.in, got, err, want)
		}
	}
	if _, err := ResumeArgs([]string{"--model"}, sessionID); err == nil {
		t.Fatal("missing option value accepted")
	}
}
func TestNormalProcessPrivateStateAndIsolation(t *testing.T) {
	isolated(t)
	for i := 0; i < 2; i++ {
		status, err := Run([]string{"hello"}, "")
		if err != nil || status != 0 {
			t.Fatalf("%d %v", status, err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(core.StateHome(), "runs"))
	if err != nil || len(entries) != 2 {
		t.Fatalf("run isolation: %v %v", entries, err)
	}
	for _, e := range entries {
		path := filepath.Join(core.StateHome(), "runs", e.Name())
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0700 {
			t.Fatal("run directory mode")
		}
		info, _ = os.Stat(filepath.Join(path, "run.json"))
		if info.Mode().Perm() != 0600 {
			t.Fatal("run file mode")
		}
		data, _ := os.ReadFile(filepath.Join(path, "run.json"))
		if strings.Contains(string(data), "synthetic-") {
			t.Fatal("token in run state")
		}
	}
	if err := os.Chmod(core.StateHome(), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(nil, ""); err == nil {
		t.Fatal("unsafe state accepted")
	}
}
func TestSwitchResumePolicyAndProtocol(t *testing.T) {
	root, project := isolated(t)
	count := 0
	persisted := false
	saveProject = func(string, core.Project) error { persisted = true; return nil }
	wait = func(command, env []string) (int, error) {
		count++
		e := envMap(env)
		if count == 1 {
			writeRequest(t, env, nil)
		} else {
			if e["CMA_ACTIVE_PROFILE"] != "two" || e["CLAUDE_CODE_OAUTH_TOKEN"] != "synthetic-two" {
				t.Fatal("switch profile not retained")
			}
			want := []string{"--resume", sessionID, "prompt", "--model", "sonnet", "--", "--continue"}
			if !reflect.DeepEqual(command[1:], want) {
				t.Fatalf("resume: %q", command)
			}
		}
		return 0, nil
	}
	loadProject = func(string) (string, core.Project, error) { return root, project, nil }
	status, err := Run([]string{"prompt", "--continue", "--model", "sonnet", "--", "--continue"}, "")
	if err != nil || status != 0 || count != 2 || persisted {
		t.Fatalf("%d %v count=%d persisted=%v", status, err, count, persisted)
	}
}
func TestNonzeroNeverRestarts(t *testing.T) {
	isolated(t)
	count := 0
	wait = func(command, env []string) (int, error) { count++; writeRequest(t, env, nil); return 7, nil }
	status, err := Run(nil, "")
	if err != nil || status != 7 || count != 1 {
		t.Fatalf("%d %v %d", status, err, count)
	}
}
func TestTamperedRequestsRejected(t *testing.T) {
	updates := []func(*core.SwitchRequest){func(r *core.SwitchRequest) { r.Confirmed = false }, func(r *core.SwitchRequest) { r.SessionID = "other" }, func(r *core.SwitchRequest) { r.Scope = "global" }, func(r *core.SwitchRequest) { r.ProjectRoot = "/elsewhere" }, func(r *core.SwitchRequest) { r.LaunchID = "wrong" }}
	for i, update := range updates {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			isolated(t)
			wait = func(command, env []string) (int, error) { writeRequest(t, env, update); return 0, nil }
			if _, err := Run(nil, ""); err == nil {
				t.Fatal("tampered request accepted")
			}
		})
	}
	for _, kind := range []string{"missing-launch", "changed-profile", "symlink-switch"} {
		t.Run(kind, func(t *testing.T) {
			isolated(t)
			wait = func(command, env []string) (int, error) {
				writeRequest(t, env, nil)
				e := envMap(env)
				name := "switch.json"
				if kind == "changed-profile" {
					name = "run.json"
				}
				path := filepath.Join(e["CMA_RUN_DIR"], name)
				data, _ := os.ReadFile(path)
				var obj map[string]any
				_ = json.Unmarshal(data, &obj)
				if kind == "missing-launch" {
					delete(obj, "launch_id")
				} else if kind == "changed-profile" {
					obj["profile"] = "two"
				}
				if kind == "symlink-switch" {
					victim := filepath.Join(t.TempDir(), "victim")
					_ = os.WriteFile(victim, data, 0600)
					_ = os.Remove(path)
					_ = os.Symlink(victim, path)
				} else {
					if err := core.AtomicWriteJSON(path, obj); err != nil {
						t.Fatal(err)
					}
				}
				return 0, nil
			}
			if _, err := Run(nil, ""); err == nil {
				t.Fatal("tampered state accepted")
			}
		})
	}
}
func TestAuthConflictsBeforeToken(t *testing.T) {
	for _, key := range authEnv {
		t.Run(key, func(t *testing.T) {
			isolated(t)
			t.Setenv(key, "enabled")
			getToken = func(string) (string, error) { t.Fatal("token accessed"); return "", nil }
			if _, err := Run(nil, ""); err == nil {
				t.Fatal("env conflict accepted")
			}
		})
	}
	for _, settings := range []string{`{"apiKeyHelper":"helper"}`, `{"env":{"ANTHROPIC_API_KEY":"fake"}}`, `{"env":null}`, `{"forceLoginMethod":"console"}`, `{"forceLoginOrgUUID":"x"}`} {
		t.Run(settings, func(t *testing.T) {
			isolated(t)
			getToken = func(string) (string, error) { t.Fatal("token accessed"); return "", nil }
			if _, err := Run([]string{"prompt", "--settings", settings}, ""); err == nil {
				t.Fatal("settings conflict accepted")
			}
		})
	}
}
func TestStartingSubdirectorySettingsChecked(t *testing.T) {
	root, project := isolated(t)
	nested := filepath.Join(root, "nested")
	if err := os.MkdirAll(filepath.Join(nested, ".claude"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, ".claude", "settings.json"), []byte(`{"env":{"ANTHROPIC_BASE_URL":"https://invalid.example"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(nested); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Error(err)
		}
	})
	loadProject = func(string) (string, core.Project, error) { return root, project, nil }
	if _, err := Run(nil, ""); err == nil {
		t.Fatal("cwd settings not checked")
	}
}
func TestExpiredAndAllowedProfiles(t *testing.T) {
	root, project := isolated(t)
	getToken = func(string) (string, error) { t.Fatal("token accessed"); return "", nil }
	expiry := "2000-01-01T00:00:00Z"
	getProfile = func(id string) (core.Profile, error) { return core.Profile{ID: id, ExpiresAt: &expiry}, nil }
	if _, err := Run(nil, ""); err == nil {
		t.Fatal("expired profile accepted")
	}
	getProfile = func(id string) (core.Profile, error) { return core.Profile{ID: id}, nil }
	project.AllowedProfiles = []string{"two"}
	loadProject = func(string) (string, core.Project, error) { return root, project, nil }
	if _, err := Run(nil, ""); err == nil {
		t.Fatal("disallowed profile accepted")
	}
}
func TestMemoryOverlayPreservesExplicitSettings(t *testing.T) {
	root, project := isolated(t)
	project.Memory.Mode = "per-profile"
	loadProject = func(string) (string, core.Project, error) { return root, project, nil }
	dirs := []string{}
	wait = func(command, env []string) (int, error) {
		if len(command) < 3 || command[1] != "--settings" {
			t.Fatal(command)
		}
		var obj map[string]any
		data, _ := os.ReadFile(command[2])
		if err := json.Unmarshal(data, &obj); err != nil {
			t.Fatal(err)
		}
		if obj["model"] != "chosen" {
			t.Fatal("model lost")
		}
		perms := obj["permissions"].(map[string]any)
		if _, ok := perms["allow"]; !ok {
			t.Fatal("allow lost")
		}
		if _, ok := perms["deny"]; !ok {
			t.Fatal("deny lost")
		}
		dirs = append(dirs, obj["autoMemoryDirectory"].(string))
		info, _ := os.Stat(command[2])
		if info.Mode().Perm() != 0600 {
			t.Fatal("overlay mode")
		}
		if len(dirs) == 1 {
			writeRequest(t, env, nil)
		}
		return 0, nil
	}
	status, err := Run([]string{"--settings", `{"model":"chosen","permissions":{"allow":["Read"]}}`, "prompt", "--settings", `{"permissions":{"deny":["Write"]}}`}, "")
	if err != nil || status != 0 {
		t.Fatalf("%d %v", status, err)
	}
	if len(dirs) != 2 || dirs[0] == dirs[1] || filepath.Base(dirs[1]) != "two" {
		t.Fatal(dirs)
	}
	getToken = func(string) (string, error) { t.Fatal("token accessed on memory conflict"); return "", nil }
	if _, err := Run([]string{"--settings", `{"autoMemoryDirectory":"/existing"}`}, ""); err == nil {
		t.Fatal("memory conflict accepted")
	}
}
func TestUnsafeModesAndNestedAndHeadless(t *testing.T) {
	root, project := isolated(t)
	getToken = func(string) (string, error) { t.Fatal("token accessed"); return "", nil }
	for _, arg := range []string{"--bare", "--bg", "--background", "--cloud"} {
		if _, err := Run([]string{"prompt", arg}, ""); err == nil {
			t.Fatal(arg)
		}
	}
	project.Profile = nil
	loadProject = func(string) (string, core.Project, error) { return root, project, nil }
	if _, err := Run(nil, ""); err == nil {
		t.Fatal("headless selection accepted")
	}
	stdinTTY = func() bool { return true }
	if _, err := Run([]string{"-p", "hello"}, ""); err == nil {
		t.Fatal("print selection accepted")
	}
	t.Setenv("CMA_MANAGED", "1")
	if _, err := Run(nil, ""); err == nil {
		t.Fatal("nested launch accepted")
	}
}
func TestAdministrationBypassesRegistry(t *testing.T) {
	isolated(t)
	loadProject = func(string) (string, core.Project, error) {
		t.Fatal("administration accessed project")
		return "", core.Project{}, nil
	}
	getToken = func(string) (string, error) { t.Fatal("administration accessed token"); return "", nil }
	wait = func(command, env []string) (int, error) {
		if _, ok := envMap(env)["CMA_MANAGED"]; ok && envMap(env)["CMA_MANAGED"] != "" {
			t.Fatal("managed passthrough")
		}
		return 0, nil
	}
	for _, args := range [][]string{{"setup-token"}, {"plugin", "install", "accounts@example"}, {"auth", "status"}, {"--help"}, {"--version"}} {
		if status, err := Run(args, ""); err != nil || status != 0 {
			t.Fatalf("%d %v", status, err)
		}
	}
}
func TestResolverAndCredentialsNeverRead(t *testing.T) {
	isolated(t)
	binary := os.Getenv("CMA_CLAUDE_BINARY")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n# CMA_MANAGED_SHIM\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveBinary(); err == nil {
		t.Fatal("renamed shim accepted")
	}
	t.Setenv("CMA_CLAUDE_BINARY", "relative")
	if _, err := ResolveBinary(); err == nil {
		t.Fatal("relative override accepted")
	}
	path := filepath.Join(t.TempDir(), ".credentials.json")
	if _, err := readSettings(path); err == nil || !strings.Contains(err.Error(), "Credential files") {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "settings.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("never read"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readSettings(link); err == nil || !strings.Contains(err.Error(), "Credential files") {
		t.Fatal(err)
	}
}
func TestRealFakeExecutableHandoff(t *testing.T) {
	isolated(t)
	record := filepath.Join(t.TempDir(), "record")
	t.Setenv("FAKE_RECORD", record)
	script := `#!/bin/sh
set -eu
printf '%s\n' "$CMA_ACTIVE_PROFILE" >> "$FAKE_RECORD"
if [ "$CMA_ACTIVE_PROFILE" = one ]; then
 umask 077
 printf '{"launch_id":"%s","session_id":"11111111-1111-1111-1111-111111111111","cwd":"%s","project_root":"%s"}' "$CMA_LAUNCH_ID" "$CMA_PROJECT_ROOT" "$CMA_PROJECT_ROOT" > "$CMA_RUN_DIR/session.json"
 printf '{"launch_id":"%s","session_id":"11111111-1111-1111-1111-111111111111","profile":"two","scope":"session","confirmed":true,"project_root":"%s"}' "$CMA_LAUNCH_ID" "$CMA_PROJECT_ROOT" > "$CMA_RUN_DIR/switch.json"
else
 [ "$1" = --resume ] && [ "$2" = 11111111-1111-1111-1111-111111111111 ] && [ "$3" = prompt ]
fi
`
	if err := os.WriteFile(os.Getenv("CMA_CLAUDE_BINARY"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	status, err := Run([]string{"prompt", "--continue"}, "")
	if err != nil || status != 0 {
		t.Fatalf("%d %v", status, err)
	}
	data, _ := os.ReadFile(record)
	if string(data) != "one\ntwo\n" {
		t.Fatalf("record %q", data)
	}
}
func TestSignalExitNormalized(t *testing.T) {
	isolated(t)
	if err := os.WriteFile(os.Getenv("CMA_CLAUDE_BINARY"), []byte("#!/bin/sh\nkill -TERM $$\n"), 0755); err != nil {
		t.Fatal(err)
	}
	status, err := Run(nil, "")
	if err != nil || status != 143 {
		t.Fatalf("%d %v", status, err)
	}
}
func TestSignalForwardingHelper(t *testing.T) {
	if os.Getenv("CMA_SIGNAL_HELPER") != "1" {
		return
	}
	prior := make(chan os.Signal, 2)
	signal.Notify(prior, syscall.SIGTERM)
	defer signal.Stop(prior)
	status, err := waitProcess([]string{"/bin/sh", "-c", `printf ready > "$READY_FILE"; exec /bin/sleep 30`}, os.Environ())
	if err != nil || status != 143 {
		t.Fatalf("forward: %d %v", status, err)
	}
	select {
	case <-prior:
	case <-time.After(time.Second):
		t.Fatal("prior subscriber lost")
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case <-prior:
	case <-time.After(time.Second):
		t.Fatal("prior subscriber not restored")
	}
}
func TestSignalsForwardedPriorSubscribersPreserved(t *testing.T) {
	ready := filepath.Join(t.TempDir(), "ready")
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=^TestSignalForwardingHelper$")
	cmd.Env = append(os.Environ(), "CMA_SIGNAL_HELPER=1", "READY_FILE="+ready)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			t.Fatal("helper did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
}
