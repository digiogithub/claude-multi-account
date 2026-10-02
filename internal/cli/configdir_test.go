//go:build linux

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"claude-multi-account/internal/core"
)

func addConfigDir(t *testing.T, id string) string {
	t.Helper()
	code, out, err := call(t, []string{"profiles", "add", id, "--label", "Work"}, "")
	if code != 0 {
		t.Fatalf("add failed %d: %s", code, err)
	}
	dir, e := core.ProfileDir(id)
	if e != nil || !strings.Contains(out, "Profile directory: "+dir+"\n") {
		t.Fatalf("directory not reported: %s", out)
	}
	return dir
}

type launcherCall struct {
	args    []string
	profile string
	calls   int
}

func fakeLauncher(t *testing.T, code int) *launcherCall {
	t.Helper()
	c := &launcherCall{}
	previous := runLauncher
	runLauncher = func(args []string, profile string) (int, error) {
		c.calls++
		c.args, c.profile = args, profile
		return code, nil
	}
	t.Cleanup(func() { runLauncher = previous })
	return c
}

func TestDefaultAddIsConfigDirWithoutStdin(t *testing.T) {
	isolated(t)
	code, out, stderr := call(t, []string{"profiles", "add", "work", "--label", "Work"}, testToken+"\n")
	if code != 0 || !strings.Contains(out, "Next step: cma profiles login work\n") {
		t.Fatalf("add: %d %s %s", code, out, stderr)
	}
	p, err := core.GetProfile("work")
	if err != nil || !core.IsConfigDir(p) {
		t.Fatal("not a config-dir profile", err)
	}
	dir, _ := core.ProfileDir("work")
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatal("profile directory missing")
	}
	if code, _, _ := call(t, []string{"profiles", "add", "work", "--label", "Work", "--mode", "config-dir"}, ""); code != 1 {
		t.Fatal("duplicate accepted")
	}
}

func TestAddModeValidation(t *testing.T) {
	isolated(t)
	for _, args := range [][]string{
		{"profiles", "add", "one", "--label", "L", "--token-stdin"},
		{"profiles", "add", "one", "--label", "L", "--backend", testToken},
		{"profiles", "add", "one", "--label", "L", "--mode", "config-dir", "--backend", "file"},
		{"profiles", "add", "one", "--label", "L", "--expires-at", testToken},
		{"profiles", "add", "one", "--label", "L", "--mode", testToken},
		{"profiles", "add", "one", "--label", "L", "--mode", "TOKEN"},
	} {
		code, out, stderr := call(t, args, testToken+"\n")
		if code != 2 || strings.Contains(out+stderr, testToken) {
			t.Fatalf("expected argument error for %v: %d", args[3:], code)
		}
	}
	if _, err := core.GetProfile("one"); err == nil {
		t.Fatal("profile created despite error")
	}
}

func TestListTextAndJSONBothModes(t *testing.T) {
	isolated(t)
	add(t, "tok")
	dir := addConfigDir(t, "cfg")
	code, out, stderr := call(t, []string{"profiles", "list"}, "")
	if code != 0 {
		t.Fatal(stderr)
	}
	want := []string{
		"cfg\tWork\tunknown\tunknown\tlogin: absent\tverification: unverified\tmode: config-dir",
		"tok\tExample\tuser@example.invalid\tExample\texpiry: unknown\tverification: unverified\tmode: token",
	}
	if lines := strings.Split(strings.TrimSpace(out), "\n"); !reflect.DeepEqual(lines, want) {
		t.Fatalf("list lines:\n%s", out)
	}
	if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, out, _ := call(t, []string{"profiles", "list"}, ""); !strings.Contains(out, "cfg\tWork\tunknown\tunknown\tlogin: present\t") {
		t.Fatalf("login state did not flip: %s", out)
	}
	_, out, _ = call(t, []string{"profiles", "list", "--json"}, "")
	var items []core.Profile
	if json.Unmarshal([]byte(out), &items) != nil || len(items) != 2 || items[0].SecretBackend != core.ConfigDirBackend || items[1].SecretBackend == core.ConfigDirBackend {
		t.Fatalf("json: %s", out)
	}
}

func TestProfilesLoginUsesLauncher(t *testing.T) {
	isolated(t)
	add(t, "tok")
	addConfigDir(t, "cfg")
	c := fakeLauncher(t, 7)
	if code, _, _ := call(t, []string{"profiles", "login", "cfg"}, ""); code != 7 {
		t.Fatalf("exit code not propagated: %d", code)
	}
	if c.calls != 1 || c.profile != "cfg" || !reflect.DeepEqual(c.args, []string{"auth", "login"}) {
		t.Fatalf("launcher call: %+v", c)
	}
	c.calls = 0
	code, _, stderr := call(t, []string{"profiles", "login", "tok"}, "")
	if code != 1 || c.calls != 0 || !strings.Contains(stderr, "profiles renew tok") {
		t.Fatalf("token login: %d %s", code, stderr)
	}
	if code, _, _ := call(t, []string{"profiles", "login", "missing"}, ""); code != 1 || c.calls != 0 {
		t.Fatal("missing profile reached launcher")
	}
	if code, _, _ := call(t, []string{"profiles", "login"}, ""); code != 2 {
		t.Fatal("missing ID accepted")
	}
	fakeLauncher(t, -15)
	if code, _, _ := call(t, []string{"profiles", "login", "cfg"}, ""); code != 143 {
		t.Fatalf("signal normalisation: %d", code)
	}
}

func TestRemoveKeepsOrPurgesDirectory(t *testing.T) {
	isolated(t)
	keep := addConfigDir(t, "keep")
	purge := addConfigDir(t, "purge")
	for _, dir := range []string{keep, purge} {
		if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	sharedFile := filepath.Join(core.SharedHome(), "settings.json")
	if err := os.WriteFile(sharedFile, []byte(`{"x":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	code, out, stderr := call(t, []string{"profiles", "remove", "keep"}, "")
	if code != 0 || !strings.Contains(out, "directory was kept: "+keep) || !strings.Contains(out, "manually") {
		t.Fatalf("remove: %d %s %s", code, out, stderr)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatal("directory not kept")
	}
	if _, err := core.GetProfile("keep"); err == nil {
		t.Fatal("registry entry kept")
	}
	code, out, stderr = call(t, []string{"profiles", "remove", "purge", "--purge"}, "")
	if code != 0 || !strings.Contains(out, "directory and login were deleted: "+purge) {
		t.Fatalf("purge: %d %s %s", code, out, stderr)
	}
	if _, err := os.Lstat(purge); !os.IsNotExist(err) {
		t.Fatal("directory not deleted")
	}
	if raw, err := os.ReadFile(sharedFile); err != nil || string(raw) != `{"x":1}` {
		t.Fatal("shared content did not survive purge")
	}
}

func TestPurgeRefusedForTokenProfile(t *testing.T) {
	isolated(t)
	add(t, "tok")
	if code, _, stderr := call(t, []string{"profiles", "remove", "tok", "--purge"}, ""); code != 1 || !strings.Contains(stderr, "config-dir") {
		t.Fatalf("purge on token profile: %d %s", code, stderr)
	}
	if _, err := core.GetProfile("tok"); err != nil {
		t.Fatal("token profile removed")
	}
	if code, _, _ := call(t, []string{"profiles", "remove", "tok"}, ""); code != 0 {
		t.Fatal("plain token remove failed")
	}
}

func TestSharedCommands(t *testing.T) {
	isolated(t)
	if code, out, _ := call(t, []string{"shared", "path"}, ""); code != 0 || strings.TrimSpace(out) != core.SharedHome() {
		t.Fatalf("shared path: %d %s", code, out)
	}
	source := filepath.Join(t.TempDir(), "claude-src")
	for name, content := range map[string]string{"projects/x/y.jsonl": "{}\n", "settings.json": `{"theme":"dark"}`, ".credentials.json": "{}"} {
		path := filepath.Join(source, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	code, out, stderr := call(t, []string{"shared", "import", "--from", source}, "")
	if code != 0 || !strings.Contains(out, "projects") || !strings.Contains(out, "settings.json") || !strings.Contains(out, "source directory was not modified") {
		t.Fatalf("import: %d %s %s", code, out, stderr)
	}
	if _, err := os.Stat(filepath.Join(core.SharedHome(), "projects/x/y.jsonl")); err != nil {
		t.Fatal("projects not imported")
	}
	if raw, err := os.ReadFile(filepath.Join(core.SharedHome(), "settings.json")); err != nil || string(raw) != `{"theme":"dark"}` {
		t.Fatal("settings not imported")
	}
	if _, err := os.Lstat(filepath.Join(core.SharedHome(), ".credentials.json")); !os.IsNotExist(err) {
		t.Fatal("credentials copied")
	}
	if _, err := os.Stat(filepath.Join(source, ".credentials.json")); err != nil {
		t.Fatal("source modified")
	}
	// default source is ~/.claude
	if code, _, _ := call(t, []string{"shared", "import"}, ""); code != 1 {
		t.Fatal("missing default source accepted")
	}
	if code, _, _ := call(t, []string{"shared", "import", "extra"}, ""); code != 2 {
		t.Fatal("positional accepted")
	}

	if code, out, _ := call(t, []string{"shared", "sync"}, ""); code != 0 || !strings.Contains(out, "No config-dir profiles") {
		t.Fatalf("empty sync: %s", out)
	}
	dir := addConfigDir(t, "b")
	addConfigDir(t, "a")
	add(t, "tok")
	// a regular file in the way is a conflict, never overwritten
	if err := os.Remove(filepath.Join(dir, "settings.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	code, out, stderr = call(t, []string{"shared", "sync"}, "")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if code != 0 || len(lines) != 2 || !strings.HasPrefix(lines[0], "a\t") || !strings.HasPrefix(lines[1], "b\t") || !strings.Contains(lines[1], "conflicts: settings.json") || !strings.Contains(stderr, "b: settings.json") {
		t.Fatalf("sync: %d %q %q", code, out, stderr)
	}
	code, out, _ = call(t, []string{"shared", "sync", "--json"}, "")
	var reports map[string]core.LinkReport
	if code != 0 || json.Unmarshal([]byte(out), &reports) != nil || len(reports) != 2 || len(reports["b"].Conflicts) != 1 || len(reports["a"].Conflicts) != 0 {
		t.Fatalf("sync json: %s", out)
	}
}

func TestStatusConfigDirAndTokenModes(t *testing.T) {
	isolated(t)
	add(t, "tok")
	dir := addConfigDir(t, "cfg")
	if code, _, err := call(t, []string{"project", "init", "--profile", "cfg"}, ""); code != 0 {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	code, out, stderr := call(t, []string{"status"}, "")
	for _, want := range []string{"Configured profile: cfg\n", "Mode: config-dir\n", "Config directory: " + dir + "\n", "Login: present\n", "Token expiry: not applicable\n"} {
		if code != 0 || !strings.Contains(out, want) {
			t.Fatalf("status missing %q: %s %s", want, out, stderr)
		}
	}
	_, out, _ = call(t, []string{"status", "--json"}, "")
	var value map[string]any
	if json.Unmarshal([]byte(out), &value) != nil || value["mode"] != "config-dir" || value["config_dir"] != dir || value["login"] != "present" {
		t.Fatalf("status json: %s", out)
	}
	if code, _, err := call(t, []string{"project", "use", "tok"}, ""); code != 0 {
		t.Fatal(err)
	}
	_, out, _ = call(t, []string{"status"}, "")
	for _, want := range []string{"Mode: token\n", "Config directory: not applicable\n", "Login: not applicable\n", "Token expiry: unknown\n"} {
		if !strings.Contains(out, want) {
			t.Fatalf("token status missing %q: %s", want, out)
		}
	}
}

func TestHelpForConfigDirCommands(t *testing.T) {
	for args, want := range map[string]string{
		"profiles add":    "--mode {config-dir,token}",
		"profiles login":  "cma profiles login ID",
		"profiles remove": "[--purge]",
		"shared":          "{path,import,sync}",
		"shared path":     "cma shared path",
		"shared import":   "[--from DIR]",
		"shared sync":     "[--json]",
		"profiles":        "login",
		"":                "shared",
	} {
		argv := append(strings.Fields(args), "--help")
		code, out, _ := call(t, argv, "")
		if code != 0 || !strings.HasPrefix(out, "usage: cma") || !strings.Contains(out, want) {
			t.Fatalf("help %q: %d %s", args, code, out)
		}
	}
}

func TestPurgeFailureKeepsRegistryEntry(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("permissions are not enforced for root")
	}
	isolated(t)
	dir := addConfigDir(t, "stuck")
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "file"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sub, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sub, 0700) })
	code, _, stderr := call(t, []string{"profiles", "remove", "stuck", "--purge"}, "")
	if code != 1 || stderr == "" {
		t.Fatalf("failed purge not reported: %d %s", code, stderr)
	}
	if _, out, _ := call(t, []string{"profiles", "list"}, ""); !strings.Contains(out, "stuck\t") {
		t.Fatalf("profile no longer listed: %s", out)
	}
	if err := os.Chmod(sub, 0700); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := call(t, []string{"profiles", "remove", "stuck", "--purge"}, ""); code != 0 {
		t.Fatalf("retry failed: %s", stderr)
	}
}

func TestDuplicateAddHintsByExistingMode(t *testing.T) {
	isolated(t)
	add(t, "tok")
	addConfigDir(t, "cfg")
	for _, c := range []struct {
		id   string
		args []string
		want string
	}{
		{"tok", []string{"--mode", "config-dir"}, "Profile already exists. To replace its token use `cma profiles renew tok`."},
		{"cfg", []string{"--mode", "token", "--token-stdin", "--backend", "file"}, "Profile already exists. To replace its login use `cma profiles login cfg`."},
		{"cfg", nil, "Profile already exists. To replace its login use `cma profiles login cfg`."},
	} {
		args := append([]string{"profiles", "add", c.id, "--label", "L"}, c.args...)
		code, _, stderr := call(t, args, testToken+"\n")
		if code != 1 || !strings.Contains(stderr, c.want) || strings.Contains(stderr, testToken) {
			t.Fatalf("%v: %d %s", args, code, stderr)
		}
	}
}

func TestAddReportsReusedLogin(t *testing.T) {
	isolated(t)
	dir, _ := core.ProfileDir("old")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	code, out, stderr := call(t, []string{"profiles", "add", "old", "--label", "Old"}, "")
	if code != 0 || !strings.Contains(out, "already holds a login; it is being reused") || !strings.Contains(out, "cma profiles login old") || strings.Contains(out, "Next step") {
		t.Fatalf("add: %d %s %s", code, out, stderr)
	}
}
