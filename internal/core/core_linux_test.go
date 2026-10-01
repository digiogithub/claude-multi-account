//go:build linux

package core

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func setup(t *testing.T) string {
	t.Helper()
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("CMA_CONFIG_HOME", filepath.Join(h, "config"))
	t.Setenv("CMA_STATE_HOME", filepath.Join(h, "state"))
	for _, k := range []string{"CMA_MANAGED", "CMA_RUN_DIR", "CMA_LAUNCH_ID", "CMA_ACTIVE_PROFILE", "CMA_PROJECT_ROOT"} {
		t.Setenv(k, "")
	}
	p := filepath.Join(h, "project")
	must(t, os.MkdirAll(filepath.Join(p, ".git"), 0700))
	return p
}
func must(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func reject(t *testing.T, e error) {
	t.Helper()
	if e == nil {
		t.Fatal("expected safe rejection")
	}
}
func metadata(id string) Profile {
	return Profile{ID: id, Label: "Example", Email: "same@example.invalid", Organization: "Example Org", OrganizationID: "00000000-0000-0000-0000-000000000001"}
}
func add(t *testing.T, id string) Profile {
	t.Helper()
	p, e := AddProfile(metadata(id), "synthetic-"+id, "file")
	must(t, e)
	return p
}
func setProject(t *testing.T, root, id string) Project {
	t.Helper()
	p := DefaultProject()
	p.Profile = &id
	must(t, SaveProject(root, p))
	return p
}
func runState(t *testing.T, root, launch string) (string, Run) {
	t.Helper()
	must(t, SecureDirectory(StateHome()))
	must(t, SecureDirectory(filepath.Join(StateHome(), "runs")))
	dir := filepath.Join(StateHome(), "runs", launch)
	must(t, SecureDirectory(dir))
	r := Run{Version: 1, LaunchID: launch, Profile: "one", ProjectRoot: root}
	must(t, AtomicWriteJSON(filepath.Join(dir, "run.json"), r))
	t.Setenv("CMA_MANAGED", "1")
	t.Setenv("CMA_RUN_DIR", dir)
	t.Setenv("CMA_LAUNCH_ID", launch)
	t.Setenv("CMA_ACTIVE_PROFILE", "one")
	t.Setenv("CMA_PROJECT_ROOT", root)
	return dir, r
}
func chdir(t *testing.T, p string) {
	t.Helper()
	old, e := os.Getwd()
	must(t, e)
	must(t, os.Chdir(p))
	t.Cleanup(func() { _ = os.Chdir(old) })
}
func TestFileLifecycle(t *testing.T) {
	setup(t)
	r, e := LoadRegistry()
	must(t, e)
	if r.Version != 1 || len(r.Profiles) != 0 {
		t.Fatal(r)
	}
	one := add(t, "one")
	add(t, "two")
	token, e := GetToken("one")
	must(t, e)
	if token != "synthetic-one" {
		t.Fatal("wrong token")
	}
	r, e = LoadRegistry()
	must(t, e)
	b, _ := json.Marshal(r)
	if strings.Contains(string(b), "synthetic") {
		t.Fatal("registry leaked token")
	}
	p, e := RenewProfile("one", "synthetic-renewed")
	must(t, e)
	if p.ExpiresAt != nil || p.SecretRef == one.SecretRef {
		t.Fatal(p)
	}
	if exists(filepath.Join(ConfigHome(), "secrets", one.SecretRef+".json")) {
		t.Fatal("old secret retained")
	}
	must(t, RemoveProfile("one"))
	r, e = LoadRegistry()
	must(t, e)
	if len(r.Profiles) != 1 {
		t.Fatal(r)
	}
	for _, path := range []string{ConfigHome(), filepath.Join(ConfigHome(), "secrets")} {
		i, e := os.Stat(path)
		must(t, e)
		if i.Mode().Perm() != 0700 {
			t.Fatal("directory permissions")
		}
	}
	i, e := os.Stat(filepath.Join(ConfigHome(), "profiles.json"))
	must(t, e)
	if i.Mode().Perm() != 0600 {
		t.Fatal("file permissions")
	}
}
func TestExpiryStatusAndRenewal(t *testing.T) {
	p := setup(t)
	meta := metadata("one")
	expiry := "2000-01-01T00:00:00Z"
	meta.ExpiresAt = &expiry
	_, e := AddProfile(meta, "synthetic-one", "file")
	must(t, e)
	_, e = GetToken("one")
	reject(t, e)
	s, e := Status(p)
	must(t, e)
	if s["expiry_status"] != "unknown" {
		t.Fatal(s)
	}
	setProject(t, p, "one")
	s, e = Status(p)
	must(t, e)
	if s["expiry_status"] != "expired" {
		t.Fatal(s)
	}
	renewed, e := RenewProfile("one", "synthetic-new")
	must(t, e)
	if renewed.ExpiresAt != nil {
		t.Fatal("stale expiry")
	}
	_, e = GetToken("one")
	must(t, e)
}
func TestInvalidProfilesTokensAndAuto(t *testing.T) {
	setup(t)
	for _, id := range []string{"../x", "", "-option", "bad id", strings.Repeat("x", 65)} {
		_, e := AddProfile(metadata(id), "synthetic", "file")
		reject(t, e)
	}
	for _, token := range []string{"", "a\nb", "a b", "a\x00b", "a\u00a0b", strings.Repeat("x", 16385)} {
		_, e := AddProfile(metadata("one"), token, "file")
		reject(t, e)
	}
	meta := metadata("one")
	bad := "2030-01-01"
	meta.ExpiresAt = &bad
	_, e := AddProfile(meta, "synthetic", "file")
	reject(t, e)
	meta.ExpiresAt = nil
	meta.OrganizationID = "not-uuid"
	_, e = AddProfile(meta, "synthetic", "file")
	reject(t, e)
	add(t, "one")
	_, e = AddProfile(metadata("one"), "synthetic", "file")
	reject(t, e)
	_, e = GetProfile("missing")
	reject(t, e)
	t.Setenv("PATH", t.TempDir())
	_, e = AddProfile(metadata("auto"), "synthetic", "auto")
	reject(t, e)
	_, e = AddProfile(metadata("mac"), "synthetic", "keychain")
	reject(t, e)
}
func TestStrictJSONSchemas(t *testing.T) {
	setup(t)
	add(t, "one")
	path := filepath.Join(ConfigHome(), "profiles.json")
	original, e := os.ReadFile(path)
	must(t, e)
	for _, data := range []string{`[]`, `{"version":true,"profiles":{}}`, `{"version":1,"profiles":null}`, `{"version":1,"profiles":{},"extra":0}`, `{"version":1,"version":1,"profiles":{}}`, `{"Version":1,"profiles":{}}`, string(original) + `{}`, strings.Replace(string(original), `"secret_backend": "file"`, `"secret_backend": []`, 1), strings.Replace(string(original), `"label": "Example"`, `"label": null`, 1), strings.Replace(string(original), `"expires_at": null,`, "", 1)} {
		must(t, os.WriteFile(path, []byte(data), 0600))
		_, e = LoadRegistry()
		reject(t, e)
	}
	must(t, os.WriteFile(path, original, 0600))
	secret := filepath.Join(ConfigHome(), "secrets")
	r, e := LoadRegistry()
	must(t, e)
	must(t, AtomicWriteJSON(filepath.Join(secret, r.Profiles["one"].SecretRef+".json"), map[string]any{"token": "synthetic", "extra": 1}))
	_, e = GetToken("one")
	reject(t, e)
}
func TestPrivateSymlinkPermissionsAndSize(t *testing.T) {
	setup(t)
	must(t, SecureDirectory(ConfigHome()))
	target := filepath.Join(home(), "target")
	must(t, os.WriteFile(target, []byte("original"), 0600))
	path := filepath.Join(ConfigHome(), "profiles.json")
	must(t, os.Symlink(target, path))
	reject(t, AtomicWriteJSON(path, map[string]any{}))
	_, e := LoadRegistry()
	reject(t, e)
	b, e := os.ReadFile(target)
	must(t, e)
	if string(b) != "original" {
		t.Fatal("symlink target changed")
	}
	must(t, os.Remove(path))
	add(t, "one")
	must(t, os.Chmod(path, 0644))
	_, e = LoadRegistry()
	reject(t, e)
	must(t, os.Chmod(path, 0600))
	must(t, os.WriteFile(path, []byte(strings.Repeat("x", 1024*1024+1)), 0600))
	var out map[string]any
	reject(t, ReadJSON(path, &out, true))
	redirect := filepath.Join(home(), "redirect")
	must(t, os.Symlink(home(), redirect))
	t.Setenv("CMA_CONFIG_HOME", filepath.Join(redirect, "nested", "config"))
	_, e = AddProfile(metadata("other"), "synthetic", "file")
	reject(t, e)
	if exists(filepath.Join(home(), "nested")) {
		t.Fatal("created through ancestor symlink")
	}
	reject(t, AtomicWriteJSON(filepath.Join(redirect, "payload.json"), map[string]any{}))
}
func TestProjectDiscoveryAndPolicies(t *testing.T) {
	root := setup(t)
	add(t, "one")
	nested := filepath.Join(root, "nested", "deeper")
	must(t, os.MkdirAll(nested, 0700))
	setProject(t, root, "one")
	found, p, e := LoadProject(nested)
	must(t, e)
	if found != root || p.Profile == nil || *p.Profile != "one" {
		t.Fatal(found, p)
	}
	work := filepath.Join(root, "worktree")
	must(t, os.Mkdir(work, 0700))
	must(t, os.WriteFile(filepath.Join(work, ".git"), []byte("gitdir: /fake"), 0600))
	found, p, e = LoadProject(work)
	must(t, e)
	if found != work || p.Profile != nil {
		t.Fatal(found, p)
	}
	other := filepath.Join(home(), "other")
	must(t, os.MkdirAll(filepath.Join(other, "nested"), 0700))
	must(t, SaveProject(other, DefaultProject()))
	must(t, SaveProject(home(), DefaultProject()))
	found, e = ProjectRoot(filepath.Join(other, "nested"))
	must(t, e)
	if found != other {
		t.Fatal(found)
	}
	path := filepath.Join(root, ".claude", "accounts.local.json")
	for _, data := range []string{`{"unknown":true}`, `{"version":2}`, `{"version":true}`, `{"memory":{"mode":"bad"}}`, `{"memory":{"mode":[]}}`, `{"memory":{}}`, `{"switching":[]}`, `{"switching":{"confirmContextTransfer":"yes"}}`, `{"switching":{}}`, `{"allowedProfiles":"one"}`, `{"allowedProfiles":null}`, `{"allowedProfiles":[[]]}`, `{"allowedProfiles":["one","one"]}`, `{"profile":"missing"}`, `{"profile":"one","allowedProfiles":[]}`} {
		must(t, os.WriteFile(path, []byte(data), 0600))
		_, _, e = LoadProject(root)
		reject(t, e)
	}
	must(t, os.WriteFile(path, []byte(`{"allowedProfiles":[],"switching":{"confirmContextTransfer":false}}`), 0600))
	_, p, e = LoadProject(root)
	must(t, e)
	if p.AllowedProfiles == nil || p.Switching.ConfirmContextTransfer {
		t.Fatal("explicit defaults lost")
	}
	reject(t, CheckAllowed(p, "one"))
	must(t, SaveProject(root, p))
	_, p, e = LoadProject(root)
	must(t, e)
	reject(t, CheckAllowed(p, "one"))
	must(t, os.WriteFile(path, []byte(`{}`), 0600))
	_, p, e = LoadProject(root)
	must(t, e)
	if p.AllowedProfiles != nil || !p.Switching.ConfirmContextTransfer || p.Memory.Mode != "shared" {
		t.Fatal(p)
	}
}
func TestSessionsSwitchesAndIsolation(t *testing.T) {
	root := setup(t)
	chdir(t, root)
	add(t, "one")
	add(t, "two")
	setProject(t, root, "one")
	reject(t, RequestSwitch("two", "session", true))
	dir, r := runState(t, root, strings.Repeat("a", 32))
	req, e := ReadSwitchRequest(dir, r)
	must(t, e)
	if req != nil {
		t.Fatal(req)
	}
	reject(t, RequestSwitch("two", "session", true))
	for _, s := range []Session{{SessionID: "../bad", Cwd: root}, {SessionID: "good", Cwd: "relative"}, {SessionID: "good", Cwd: home()}} {
		reject(t, RegisterSession(s))
	}
	must(t, RegisterSession(Session{SessionID: "exact-123", Cwd: root}))
	reject(t, RequestSwitch("two", "session", false))
	must(t, RequestSwitch("two", "session", true))
	req, e = ReadSwitchRequest(dir, r)
	must(t, e)
	if req.SessionID != "exact-123" {
		t.Fatal(req)
	}
	_, p, e := LoadProject(root)
	must(t, e)
	if *p.Profile != "one" {
		t.Fatal("session switch persisted")
	}
	must(t, RequestSwitch("two", "project", true))
	_, p, e = LoadProject(root)
	must(t, e)
	if *p.Profile != "two" {
		t.Fatal("project switch not persisted")
	}
	s, e := Status(root)
	must(t, e)
	if s["active_profile"] != "one" || s["session_id"] != "exact-123" {
		t.Fatal(s)
	}
	info, e := SessionInfo()
	must(t, e)
	if info.SessionID != "exact-123" {
		t.Fatal(info)
	}
	original := *req
	for _, field := range []string{"launch", "session", "confirmed", "scope", "root", "profile"} {
		bad := original
		switch field {
		case "launch":
			bad.LaunchID = strings.Repeat("b", 32)
		case "session":
			bad.SessionID = "wrong"
		case "confirmed":
			bad.Confirmed = false
		case "scope":
			bad.Scope = "bad"
		case "root":
			bad.ProjectRoot = home()
		case "profile":
			bad.Profile = "missing"
		}
		must(t, AtomicWriteJSON(filepath.Join(dir, "switch.json"), bad))
		_, e = ReadSwitchRequest(dir, r)
		reject(t, e)
	}
	must(t, AtomicWriteJSON(filepath.Join(dir, "switch.json"), original))
	second, secondRun := runState(t, root, strings.Repeat("b", 32))
	must(t, RegisterSession(Session{SessionID: "second", Cwd: root}))
	req, e = ReadSwitchRequest(second, secondRun)
	must(t, e)
	if req != nil {
		t.Fatal("cross-run request")
	}
	req, e = ReadSwitchRequest(dir, r)
	must(t, e)
	if req.SessionID != "exact-123" {
		t.Fatal(req)
	}
	must(t, os.Chmod(second, 0755))
	_, _, e = ValidateRun()
	reject(t, e)
}
func TestSwitchCwdAllowedAndEnvironment(t *testing.T) {
	root := setup(t)
	chdir(t, root)
	add(t, "one")
	add(t, "two")
	dir, _ := runState(t, root, strings.Repeat("a", 32))
	must(t, RegisterSession(Session{SessionID: "session", Cwd: root}))
	p := DefaultProject()
	id := "one"
	p.Profile = &id
	p.AllowedProfiles = []string{"one"}
	must(t, SaveProject(root, p))
	reject(t, RequestSwitch("two", "session", true))
	must(t, os.Chdir(home()))
	reject(t, RequestSwitch("one", "session", true))
	t.Setenv("CMA_ACTIVE_PROFILE", "two")
	_, _, e := ValidateRun()
	reject(t, e)
	t.Setenv("CMA_ACTIVE_PROFILE", "one")
	t.Setenv("CMA_LAUNCH_ID", "")
	_, _, e = ValidateRun()
	reject(t, e)
	t.Setenv("CMA_LAUNCH_ID", strings.Repeat("a", 32))
	must(t, AtomicWriteJSON(filepath.Join(dir, "session.json"), map[string]any{"launch_id": strings.Repeat("a", 32), "session_id": "good", "cwd": root, "project_root": root, "extra": true}))
	_, e = SessionInfo()
	reject(t, e)
}

// A copy of the current test executable acts as secret-tool, with no shell/Python dependencies.
func TestBackendHelper(t *testing.T) {
	if os.Getenv("CMA_TEST_BACKEND_HELPER") != "1" {
		return
	}
	args := os.Args[1:]
	if len(args) < 5 {
		os.Exit(2)
	}
	for _, arg := range args {
		if strings.Contains(arg, "synthetic") {
			os.Exit(3)
		}
	}
	action := args[0]
	ref := args[len(args)-1]
	if args[len(args)-4] != "service" || args[len(args)-3] != service || args[len(args)-2] != "reference" {
		os.Exit(4)
	}
	path := filepath.Join(os.Getenv("CMA_FAKE_STORE"), ref)
	if os.Getenv("CMA_FAKE_FAIL") == "1" {
		fmt.Fprint(os.Stdout, "synthetic-leak")
		fmt.Fprint(os.Stderr, "synthetic-leak")
		os.Exit(1)
	}
	switch action {
	case "store":
		if args[1] != "--label="+service {
			os.Exit(5)
		}
		data, e := os.ReadFile("/dev/stdin")
		if e != nil {
			os.Exit(6)
		}
		if os.WriteFile(path, data, 0600) != nil {
			os.Exit(7)
		}
		// Force an actual registry commit failure after successful secret storage.
		if os.Getenv("CMA_FAKE_POISON_REGISTRY") == "1" {
			registry := filepath.Join(os.Getenv("CMA_CONFIG_HOME"), "profiles.json")
			if os.Rename(registry, registry+".backup") != nil {
				os.Exit(11)
			}
			if os.Symlink(filepath.Join(os.Getenv("HOME"), "missing-target"), registry) != nil {
				os.Exit(12)
			}
		}
	case "lookup":
		data, e := os.ReadFile(path)
		if e != nil {
			os.Exit(8)
		}
		fmt.Println(string(data))
	case "clear":
		if os.Remove(path) != nil {
			os.Exit(9)
		}
	default:
		os.Exit(10)
	}
	os.Exit(0)
}
func TestRealFakeSecretTool(t *testing.T) {
	setup(t)
	bin := t.TempDir()
	exe, e := os.Executable()
	must(t, e)
	data, e := os.ReadFile(exe)
	must(t, e)
	must(t, os.WriteFile(filepath.Join(bin, "secret-tool"), data, 0700))
	t.Setenv("PATH", bin)
	t.Setenv("CMA_TEST_BACKEND_HELPER", "1")
	t.Setenv("CMA_FAKE_STORE", t.TempDir())
	_, e = AddProfile(metadata("one"), "synthetic-one", "auto")
	must(t, e)
	token, e := GetToken("one")
	must(t, e)
	if token != "synthetic-one" {
		t.Fatal("backend read mismatch")
	}
	_, e = RenewProfile("one", "synthetic-new")
	must(t, e)
	token, e = GetToken("one")
	must(t, e)
	if token != "synthetic-new" {
		t.Fatal("backend renewal mismatch")
	}
	files, e := os.ReadDir(os.Getenv("CMA_FAKE_STORE"))
	must(t, e)
	if len(files) != 1 {
		t.Fatal("orphan secret")
	}
	t.Setenv("CMA_FAKE_FAIL", "1")
	_, e = GetToken("one")
	reject(t, e)
	if strings.Contains(e.Error(), "synthetic") {
		t.Fatal("backend error leaked output")
	}
	t.Setenv("CMA_FAKE_FAIL", "")
	must(t, RemoveProfile("one"))
	files, e = os.ReadDir(os.Getenv("CMA_FAKE_STORE"))
	must(t, e)
	if len(files) != 0 {
		t.Fatal("backend secret not cleared")
	}
}
func TestConcurrentProcessHelper(t *testing.T) {
	id := os.Getenv("CMA_TEST_WRITER")
	if id == "" {
		return
	}
	_, e := AddProfile(metadata(id), "synthetic-child", "file")
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	os.Exit(0)
}
func TestConcurrentWriters(t *testing.T) {
	setup(t)
	exe, e := os.Executable()
	must(t, e)
	commands := []*exec.Cmd{}
	for i := 0; i < 8; i++ {
		cmd := exec.Command(exe, "-test.run=^TestConcurrentProcessHelper$")
		cmd.Env = append(os.Environ(), fmt.Sprintf("CMA_TEST_WRITER=p%d", i))
		commands = append(commands, cmd)
		must(t, cmd.Start())
	}
	for _, cmd := range commands {
		must(t, cmd.Wait())
	}
	r, e := LoadRegistry()
	must(t, e)
	if len(r.Profiles) != 8 {
		t.Fatal("lost concurrent registrations")
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, e := RenewProfile(fmt.Sprintf("p%d", i), "synthetic-renewed")
			errs <- e
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		must(t, e)
	}
	r, e = LoadRegistry()
	must(t, e)
	if len(r.Profiles) != 8 {
		t.Fatal("lost concurrent renewals")
	}
}
