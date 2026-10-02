//go:build linux

package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunAndSessionSchemaAndSymlinks(t *testing.T) {
	root := setup(t)
	chdir(t, root)
	add(t, "one")
	add(t, "two")
	dir, r := runState(t, root, strings.Repeat("a", 32))
	runPath := filepath.Join(dir, "run.json")
	raw, e := json.Marshal(r)
	must(t, e)
	for _, data := range []string{`{"version":true,"launch_id":"` + r.LaunchID + `","profile":"one","project_root":"` + root + `"}`, strings.Replace(string(raw), `"profile":"one"`, `"profile":null`, 1), strings.Replace(string(raw), `"version":1,`, "", 1), strings.Replace(string(raw), `"version":1`, `"version":2`, 1), strings.Replace(string(raw), `"profile":"one"`, `"profile":"../bad"`, 1), strings.Replace(string(raw), root, root+"/../project", 1)} {
		must(t, os.WriteFile(runPath, []byte(data), 0600))
		_, _, e = ValidateRun()
		reject(t, e)
	}
	must(t, AtomicWriteJSON(runPath, r))
	must(t, RegisterSession(Session{SessionID: "exact", Cwd: root}))
	must(t, RequestSwitch("two", "session", true))
	badExpected := r
	badExpected.Profile = "two"
	_, e = ReadSwitchRequest(dir, badExpected)
	reject(t, e)
	for _, name := range []string{"switch.json", "session.json", "run.json"} {
		path := filepath.Join(dir, name)
		data, e := os.ReadFile(path)
		must(t, e)
		target := filepath.Join(home(), "target-"+name)
		must(t, os.WriteFile(target, data, 0600))
		must(t, os.Remove(path))
		must(t, os.Symlink(target, path))
		_, e = ReadSwitchRequest(dir, r)
		reject(t, e)
		must(t, os.Remove(path))
		must(t, AtomicWriteJSON(path, json.RawMessage(data)))
	}
	must(t, os.Chmod(filepath.Join(dir, "session.json"), 0644))
	_, e = SessionInfo()
	reject(t, e)
}
func TestProjectSymlinksAndSharedPermissions(t *testing.T) {
	root := setup(t)
	p := DefaultProject()
	p.Memory.Mode = "per-profile"
	p.Switching.ConfirmContextTransfer = false
	must(t, SaveProject(root, p))
	dir := filepath.Join(root, ".claude")
	must(t, os.Chmod(dir, 0755))
	_, got, e := LoadProject(root)
	must(t, e)
	if got.Memory.Mode != "per-profile" || got.Switching.ConfirmContextTransfer {
		t.Fatal(got)
	}
	path := filepath.Join(dir, "accounts.local.json")
	must(t, os.Chmod(path, 0644))
	_, _, e = LoadProject(root)
	must(t, e)
	target := filepath.Join(home(), "config-target.json")
	must(t, os.WriteFile(target, []byte(`{}`), 0600))
	must(t, os.Remove(path))
	must(t, os.Symlink(target, path))
	_, _, e = LoadProject(root)
	reject(t, e)
	reject(t, SaveProject(root, p))
	must(t, os.Remove(path))
	must(t, os.Remove(dir))
	actual := filepath.Join(home(), "actual-claude")
	must(t, os.Mkdir(actual, 0700))
	must(t, os.Symlink(actual, dir))
	reject(t, SaveProject(root, p))
	if exists(filepath.Join(actual, "accounts.local.json")) {
		t.Fatal("wrote redirected project")
	}
}
func TestBackendFailureDoesNotChangeRegistry(t *testing.T) {
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
	original, e := AddProfile(metadata("one"), "synthetic-original", "secret-service")
	must(t, e)
	t.Setenv("CMA_FAKE_FAIL", "1")
	_, e = RenewProfile("one", "synthetic-new")
	reject(t, e)
	_, e = AddProfile(metadata("two"), "synthetic-two", "secret-service")
	reject(t, e)
	r, e := LoadRegistry()
	must(t, e)
	if len(r.Profiles) != 1 || r.Profiles["one"].SecretRef != original.SecretRef {
		t.Fatal("failed backend changed metadata")
	}
	t.Setenv("CMA_FAKE_FAIL", "")
	token, e := GetToken("one")
	must(t, e)
	if token != "synthetic-original" {
		t.Fatal("failed renewal lost original secret")
	}
	// The fake backend redirects the registry only after storing the new secret.
	// A failed atomic commit must clean the new reference and preserve the old.
	for _, operation := range []string{"renew", "add"} {
		t.Setenv("CMA_FAKE_POISON_REGISTRY", "1")
		if operation == "renew" {
			_, e = RenewProfile("one", "synthetic-new")
		} else {
			_, e = AddProfile(metadata("two"), "synthetic-two", "secret-service")
		}
		reject(t, e)
		t.Setenv("CMA_FAKE_POISON_REGISTRY", "")
		path := filepath.Join(ConfigHome(), "profiles.json")
		must(t, os.Remove(path))
		must(t, os.Rename(path+".backup", path))
		token, e = GetToken("one")
		must(t, e)
		if token != "synthetic-original" {
			t.Fatal("registry failure lost original secret")
		}
		files, e := os.ReadDir(os.Getenv("CMA_FAKE_STORE"))
		must(t, e)
		if len(files) != 1 {
			t.Fatal("failed commit left new backend secret")
		}
	}
}
func TestExpiryFormatsAndMetadataValidation(t *testing.T) {
	setup(t)
	for _, v := range []string{"2030-01-01T00:00:00Z", "2030-01-01T00:00:00+01:00", "2030-01-01 00:00:00+0000", "2030-01-01T00:00:00.123456+00:00"} {
		p := metadata("one")
		p.ExpiresAt = &v
		p.SecretBackend = "file"
		p.SecretRef = strings.Repeat("a", 32)
		p.Verification = "unverified"
		must(t, validateMetadata(p.ID, p))
		must(t, EnsureNotExpired(p))
	}
	for _, v := range []string{"", "bad", "2030-01-01T00:00:00"} {
		_, e := expiry(&v)
		reject(t, e)
	}
	for _, p := range []Profile{{ID: "one", Label: ""}, {ID: "one", Label: "bad\ntext"}, {ID: "one", Label: strings.Repeat("x", 513)}} {
		_, e := AddProfile(p, "synthetic", "file")
		reject(t, e)
	}
}

func TestOptionalAccountMetadataValidation(t *testing.T) {
	setup(t)
	p, e := AddProfile(Profile{ID: "one", Label: "Personal"}, "synthetic", "file")
	must(t, e)
	got, e := GetProfile("one")
	must(t, e)
	if got != p || got.Email != "" || got.Organization != "" || got.OrganizationID != "" {
		t.Fatal("Unknown metadata did not survive registry reload")
	}
	for _, field := range []string{"email", "organization", "organization_id"} {
		for _, value := range []string{" ", "bad\ntext", strings.Repeat("x", 513), string([]byte{0xff})} {
			invalid := p
			switch field {
			case "email":
				invalid.Email = value
			case "organization":
				invalid.Organization = value
			case "organization_id":
				invalid.OrganizationID = value
			}
			reject(t, validateMetadata(invalid.ID, invalid))
		}
	}
	p.OrganizationID = "not-a-uuid"
	reject(t, validateMetadata(p.ID, p))
}
