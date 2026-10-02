//go:build linux

package core

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func setupAccounts(t *testing.T) (project, accounts string) {
	t.Helper()
	project = setup(t)
	accounts = filepath.Join(filepath.Dir(project), "accounts")
	t.Setenv("CMA_ACCOUNTS_HOME", accounts)
	t.Setenv("CMA_SHARED_HOME", "")
	return
}
func addCD(t *testing.T, id string) LinkReport {
	t.Helper()
	p, r, e := AddConfigDirProfile(metadata(id))
	must(t, e)
	if p.SecretBackend != ConfigDirBackend || p.ExpiresAt != nil || p.Verification != "unverified" || !launchRE.MatchString(p.SecretRef) {
		t.Fatalf("bad record %+v", p)
	}
	return r
}
func mode(t *testing.T, path string) os.FileMode {
	t.Helper()
	i, e := os.Lstat(path)
	must(t, e)
	return i.Mode()
}

func TestAccountsLayoutAndPermissions(t *testing.T) {
	_, acc := setupAccounts(t)
	if AccountsHome() != acc || SharedHome() != filepath.Join(acc, "shared") {
		t.Fatal("unexpected homes")
	}
	r := addCD(t, "one")
	dir, e := ProfileDir("one")
	must(t, e)
	for _, d := range []string{acc, filepath.Join(acc, "shared"), filepath.Join(acc, "profiles"), dir} {
		if m := mode(t, d); !m.IsDir() || m.Perm() != 0700 {
			t.Fatalf("%s mode %v", d, m)
		}
	}
	var want []string
	for _, s := range SharedEntries {
		if !s.Optional {
			want = append(want, s.Name)
		}
		tg := filepath.Join(SharedHome(), s.Name)
		if s.Optional {
			continue
		}
		m := mode(t, tg)
		if m.IsDir() != s.Dir || (s.Dir && m.Perm() != 0700) || (!s.Dir && m.Perm() != 0600) {
			t.Fatalf("shared %s mode %v", s.Name, m)
		}
		l, e := os.Readlink(filepath.Join(dir, s.Name))
		must(t, e)
		if l != tg || !filepath.IsAbs(l) {
			t.Fatalf("link %s -> %s", s.Name, l)
		}
	}
	if !reflect.DeepEqual(r.Linked, want) || !reflect.DeepEqual(r.Skipped, []string{"keybindings.json"}) || len(r.Present)+len(r.Conflicts) != 0 {
		t.Fatalf("report %+v", r)
	}
	data, _ := os.ReadFile(filepath.Join(SharedHome(), "settings.json"))
	if string(data) != "{}\n" {
		t.Fatalf("settings %q", data)
	}
	raw, _ := json.Marshal(newLinkReport())
	if string(raw) != `{"linked":[],"present":[],"conflicts":[],"skipped":[]}` {
		t.Fatal(string(raw))
	}
	if _, _, e = EnsureProfileDir("../x"); e == nil {
		t.Fatal("bad id accepted")
	}
}

func TestEnsureIdempotentSharedAndConflicts(t *testing.T) {
	_, _ = setupAccounts(t)
	addCD(t, "one")
	addCD(t, "two")
	d1, _ := ProfileDir("one")
	d2, _ := ProfileDir("two")
	must(t, os.MkdirAll(filepath.Join(d1, "projects", "x"), 0700))
	_ = os.Remove(filepath.Join(d1, "projects"))
	_, r, e := EnsureProfileDir("one")
	must(t, e)
	if len(r.Linked) != 1 || len(r.Present) != len(SharedEntries)-2 {
		t.Fatalf("heal %+v", r)
	}
	must(t, os.WriteFile(filepath.Join(d1, "projects", "s.jsonl"), []byte("hi"), 0600))
	got, e := os.ReadFile(filepath.Join(d2, "projects", "s.jsonl"))
	must(t, e)
	if string(got) != "hi" {
		t.Fatal("projects not shared")
	}
	_, r, e = EnsureProfileDir("two")
	must(t, e)
	if len(r.Linked) != 0 || len(r.Conflicts) != 0 || len(r.Present) != len(SharedEntries)-1 || !reflect.DeepEqual(r.Skipped, []string{"keybindings.json"}) {
		t.Fatalf("idempotent %+v", r)
	}
	// optional becomes linked once shared has it
	must(t, os.WriteFile(filepath.Join(SharedHome(), "keybindings.json"), []byte("{}"), 0600))
	_, r, _ = EnsureProfileDir("two")
	if !reflect.DeepEqual(r.Linked, []string{"keybindings.json"}) || len(r.Skipped) != 0 {
		t.Fatalf("optional %+v", r)
	}
	// a real file replacing a link is a conflict and is left untouched
	h := filepath.Join(d2, "history.jsonl")
	must(t, os.Remove(h))
	must(t, os.WriteFile(h, []byte("mine"), 0600))
	// a link elsewhere is a conflict too
	must(t, os.Remove(filepath.Join(d2, "CLAUDE.md")))
	must(t, os.Symlink(filepath.Join(d2, ".claude.json"), filepath.Join(d2, "CLAUDE.md")))
	_, r, e = EnsureProfileDir("two")
	must(t, e)
	if !reflect.DeepEqual(r.Conflicts, []string{"CLAUDE.md", "history.jsonl"}) {
		t.Fatalf("conflicts %+v", r)
	}
	if b, _ := os.ReadFile(h); string(b) != "mine" {
		t.Fatal("conflict overwritten")
	}
	if m := mode(t, h); m&os.ModeSymlink != 0 {
		t.Fatal("conflict replaced")
	}
}

func TestRegistryRecordV1(t *testing.T) {
	_, _ = setupAccounts(t)
	addCD(t, "cd")
	add(t, "tok")
	data, e := os.ReadFile(filepath.Join(ConfigHome(), "profiles.json"))
	must(t, e)
	var raw struct {
		Version  int                       `json:"version"`
		Profiles map[string]map[string]any `json:"profiles"`
	}
	must(t, json.Unmarshal(data, &raw))
	var keys []string
	for k := range raw.Profiles["cd"] {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	want := []string{"email", "expires_at", "id", "label", "organization", "organization_id", "secret_backend", "secret_ref", "verification"}
	if raw.Version != 1 || !reflect.DeepEqual(keys, want) || raw.Profiles["cd"]["secret_backend"] != "config-dir" || raw.Profiles["cd"]["expires_at"] != nil {
		t.Fatalf("record %v", raw.Profiles["cd"])
	}
	r, e := LoadRegistry()
	must(t, e)
	if !IsConfigDir(r.Profiles["cd"]) || IsConfigDir(r.Profiles["tok"]) || r.Profiles["tok"].SecretBackend != "file" {
		t.Fatal("modes")
	}
	if tok, e := GetToken("tok"); e != nil || tok != "synthetic-tok" {
		t.Fatal("token mode broken")
	}
	_, _, e = AddConfigDirProfile(metadata("cd"))
	reject(t, e)
	_, _, e = AddConfigDirProfile(metadata("bad/id"))
	reject(t, e)
}

func TestConfigDirTokenOperationsAndRemove(t *testing.T) {
	_, _ = setupAccounts(t)
	addCD(t, "cd")
	_, e := GetToken("cd")
	if e == nil || e.Error() != "Profile uses a configuration directory; it has no token." {
		t.Fatalf("GetToken %v", e)
	}
	_, e = RenewProfile("cd", "synthetic-new")
	if e == nil || !strings.Contains(e.Error(), "cma profiles login cd") {
		t.Fatalf("Renew %v", e)
	}
	dir, _ := ProfileDir("cd")
	must(t, RemoveProfile("cd"))
	if _, e = GetProfile("cd"); e == nil {
		t.Fatal("still registered")
	}
	if !exists(dir) {
		t.Fatal("directory removed without purge")
	}
	// re-adding reuses the kept directory
	r := addCD(t, "cd")
	if len(r.Present) != len(SharedEntries)-1 {
		t.Fatalf("%+v", r)
	}
}

func TestPurgeKeepsSharedData(t *testing.T) {
	_, _ = setupAccounts(t)
	addCD(t, "one")
	addCD(t, "two")
	d, _ := ProfileDir("one")
	must(t, os.WriteFile(filepath.Join(SharedHome(), "projects", "keep"), []byte("x"), 0600))
	must(t, os.WriteFile(filepath.Join(d, ".credentials.json"), []byte(`{"synthetic":true}`), 0600))
	must(t, os.MkdirAll(filepath.Join(d, "cache", "sub"), 0700))
	must(t, os.WriteFile(filepath.Join(d, "cache", "sub", "f"), []byte("x"), 0600))
	// a symlink to a directory elsewhere must be unlinked, not traversed
	outside := filepath.Join(filepath.Dir(d), "outside")
	must(t, os.Mkdir(outside, 0700))
	must(t, os.WriteFile(filepath.Join(outside, "o"), []byte("x"), 0600))
	must(t, os.Symlink(outside, filepath.Join(d, "extra")))
	must(t, PurgeProfileDir("one"))
	if exists(d) {
		t.Fatal("profile dir remains")
	}
	for _, p := range []string{filepath.Join(SharedHome(), "projects", "keep"), filepath.Join(outside, "o")} {
		if !exists(p) {
			t.Fatalf("%s destroyed", p)
		}
	}
	d2, _ := ProfileDir("two")
	if b, e := os.ReadFile(filepath.Join(d2, "projects", "keep")); e != nil || string(b) != "x" {
		t.Fatal("other profile lost shared data")
	}
	must(t, PurgeProfileDir("one"))
	reject(t, PurgeProfileDir("../one"))
}

func TestLoginState(t *testing.T) {
	_, _ = setupAccounts(t)
	addCD(t, "one")
	if LoginState("one") != "absent" || LoginState("bad/id") != "absent" {
		t.Fatal("expected absent")
	}
	d, _ := ProfileDir("one")
	// unreadable synthetic file proves content is never opened
	must(t, os.WriteFile(filepath.Join(d, ".credentials.json"), []byte("synthetic"), 0))
	if LoginState("one") != "present" {
		t.Fatal("expected present")
	}
}

func TestImportShared(t *testing.T) {
	_, acc := setupAccounts(t)
	src := filepath.Join(filepath.Dir(acc), "claude-src")
	must(t, os.MkdirAll(filepath.Join(src, "projects", "p1", "deep"), 0755))
	must(t, os.WriteFile(filepath.Join(src, "projects", "p1", "deep", "a.jsonl"), []byte("a"), 0644))
	must(t, os.WriteFile(filepath.Join(src, "projects", ".credentials.json"), []byte("synthetic"), 0600))
	must(t, os.WriteFile(filepath.Join(src, "projects", "p1", ".claude.json"), []byte("synthetic"), 0600))
	must(t, os.Symlink("../p1", filepath.Join(src, "projects", "link")))
	must(t, os.WriteFile(filepath.Join(src, "settings.json"), []byte(`{"theme":"dark"}`), 0644))
	must(t, os.WriteFile(filepath.Join(src, "CLAUDE.md"), []byte("memo"), 0600))
	must(t, os.WriteFile(filepath.Join(src, ".credentials.json"), []byte("synthetic"), 0600))
	must(t, os.WriteFile(filepath.Join(src, ".claude.json"), []byte("synthetic"), 0600))
	must(t, os.WriteFile(filepath.Join(src, "skills"), []byte("file not dir"), 0600)) // kind mismatch
	snapshot := func() string {
		var b strings.Builder
		_ = filepath.Walk(src, func(p string, i os.FileInfo, e error) error {
			b.WriteString(p + i.Mode().String() + "\n")
			if i.Mode().IsRegular() {
				d, _ := os.ReadFile(p)
				b.Write(d)
			}
			return nil
		})
		return b.String()
	}
	before := snapshot()
	copied, skipped, e := ImportShared(src)
	must(t, e)
	if !reflect.DeepEqual(copied, []string{"CLAUDE.md", "projects", "settings.json"}) {
		t.Fatalf("copied %v", copied)
	}
	if !contains(skipped, "skills") || !contains(skipped, "agents") || contains(skipped, "projects") {
		t.Fatalf("skipped %v", skipped)
	}
	if !sort.StringsAreSorted(copied) || !sort.StringsAreSorted(skipped) {
		t.Fatal("unsorted")
	}
	sh := SharedHome()
	if b, _ := os.ReadFile(filepath.Join(sh, "projects", "p1", "deep", "a.jsonl")); string(b) != "a" {
		t.Fatal("projects not copied")
	}
	if m := mode(t, filepath.Join(sh, "projects", "p1", "deep", "a.jsonl")); m.Perm() != 0600 {
		t.Fatalf("mode %v", m)
	}
	if l, e := os.Readlink(filepath.Join(sh, "projects", "link")); e != nil || l != "../p1" {
		t.Fatal("symlink not recreated")
	}
	for _, p := range []string{".credentials.json", ".claude.json", "projects/.credentials.json", "projects/p1/.claude.json"} {
		if exists(filepath.Join(sh, p)) {
			t.Fatalf("%s copied", p)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(sh, "settings.json")); string(b) != `{"theme":"dark"}` {
		t.Fatal("placeholder settings not replaced")
	}
	if snapshot() != before {
		t.Fatal("source modified")
	}
	// non-placeholder shared content is not overwritten by a second import
	must(t, os.WriteFile(filepath.Join(src, "CLAUDE.md"), []byte("other"), 0600))
	copied, _, e = ImportShared(src)
	must(t, e)
	if len(copied) != 0 {
		t.Fatalf("second import copied %v", copied)
	}
	if b, _ := os.ReadFile(filepath.Join(sh, "CLAUDE.md")); string(b) != "memo" {
		t.Fatal("overwritten")
	}
}
func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func TestImportRefusals(t *testing.T) {
	_, acc := setupAccounts(t)
	addCD(t, "one")
	for _, p := range []string{acc, SharedHome(), filepath.Join(acc, "profiles", "one"), filepath.Join(SharedHome(), "projects"), filepath.Join(acc, "nope"), filepath.Join(SharedHome(), "settings.json")} {
		if _, _, e := ImportShared(p); e == nil {
			t.Fatalf("accepted %s", p)
		}
	}
	// a symlink pointing into the accounts home resolves and is refused too
	l := filepath.Join(filepath.Dir(acc), "alias")
	must(t, os.Symlink(SharedHome(), l))
	if _, _, e := ImportShared(l); e == nil {
		t.Fatal("alias accepted")
	}
	// empty-placeholder detection: whitespace-padded {} counts, real content does not
	src := filepath.Join(filepath.Dir(acc), "src")
	must(t, os.Mkdir(src, 0700))
	must(t, os.WriteFile(filepath.Join(src, "settings.json"), []byte(`{"a":1}`), 0600))
	must(t, os.WriteFile(filepath.Join(SharedHome(), "settings.json"), []byte(" {}\n\n"), 0600))
	c, _, e := ImportShared(src)
	must(t, e)
	if !reflect.DeepEqual(c, []string{"settings.json"}) {
		t.Fatalf("copied %v", c)
	}
	must(t, os.WriteFile(filepath.Join(src, "settings.json"), []byte(`{"a":2}`), 0600))
	c, _, _ = ImportShared(src)
	if len(c) != 0 {
		t.Fatal("overwrote user settings")
	}
}

func TestAccountsSymlinkedAncestorRefused(t *testing.T) {
	project, _ := setupAccounts(t)
	base := filepath.Dir(project)
	real := filepath.Join(base, "real")
	must(t, os.Mkdir(real, 0700))
	must(t, os.Symlink(real, filepath.Join(base, "link")))
	t.Setenv("CMA_ACCOUNTS_HOME", filepath.Join(base, "link", "acc"))
	_, _, e := AddConfigDirProfile(metadata("one"))
	reject(t, e)
	if exists(filepath.Join(real, "acc")) {
		t.Fatal("created through symlink")
	}
	if _, e = GetProfile("one"); e == nil {
		t.Fatal("registered despite failure")
	}
	reject(t, PurgeProfileDir("one"))
	_, _, e = EnsureProfileDir("one")
	reject(t, e)
}

func TestStatusModes(t *testing.T) {
	root, _ := setupAccounts(t)
	chdir(t, root)
	st, e := Status(root)
	must(t, e)
	if st["mode"] != nil || st["config_dir"] != nil || st["login"] != nil {
		t.Fatalf("no profile: %v", st)
	}
	add(t, "tok")
	addCD(t, "cd")
	setProject(t, root, "tok")
	st, e = Status(root)
	must(t, e)
	if st["mode"] != "token" || st["config_dir"] != nil || st["login"] != nil {
		t.Fatalf("token: %v", st)
	}
	setProject(t, root, "cd")
	st, e = Status(root)
	must(t, e)
	dir, _ := ProfileDir("cd")
	if st["mode"] != "config-dir" || st["config_dir"] != dir || st["login"] != "absent" {
		t.Fatalf("config-dir: %v", st)
	}
	must(t, os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte("synthetic"), 0600))
	st, _ = Status(root)
	if st["login"] != "present" {
		t.Fatal("login not present")
	}
}

func TestImportFollowsTopLevelSymlinks(t *testing.T) {
	_, acc := setupAccounts(t)
	root := filepath.Dir(acc)
	src := filepath.Join(root, "claude-src")
	store := filepath.Join(root, "dotfiles")
	must(t, os.MkdirAll(src, 0700))
	must(t, os.MkdirAll(filepath.Join(store, "skills", "s1"), 0700))
	must(t, os.WriteFile(filepath.Join(store, "skills", "s1", "SKILL.md"), []byte("skill"), 0644))
	must(t, os.Symlink("../p1", filepath.Join(store, "skills", "nested")))
	must(t, os.WriteFile(filepath.Join(store, "CLAUDE.md"), []byte("memo"), 0644))
	must(t, os.Symlink(filepath.Join(store, "skills"), filepath.Join(src, "skills")))
	must(t, os.Symlink(filepath.Join(store, "CLAUDE.md"), filepath.Join(src, "CLAUDE.md")))
	// links into the accounts/shared homes, kind mismatches and dangling links are skipped
	must(t, os.MkdirAll(SharedHome(), 0700))
	must(t, os.MkdirAll(filepath.Join(acc, "other"), 0700))
	must(t, os.Symlink(filepath.Join(acc, "other"), filepath.Join(src, "agents")))
	must(t, os.Symlink(SharedHome(), filepath.Join(src, "commands")))
	must(t, os.Symlink(filepath.Join(store, "CLAUDE.md"), filepath.Join(src, "plugins"))) // file for a dir entry
	must(t, os.Symlink(filepath.Join(store, "missing"), filepath.Join(src, "todos")))
	copied, skipped, e := ImportShared(src)
	must(t, e)
	if !reflect.DeepEqual(copied, []string{"CLAUDE.md", "skills"}) {
		t.Fatalf("copied %v skipped %v", copied, skipped)
	}
	for _, n := range []string{"agents", "commands", "plugins", "todos"} {
		if !contains(skipped, n) {
			t.Fatalf("%s not skipped: %v", n, skipped)
		}
	}
	sh := SharedHome()
	if m := mode(t, filepath.Join(sh, "skills")); m&os.ModeSymlink != 0 || !m.IsDir() {
		t.Fatal("top-level link was not materialised as a directory")
	}
	if b, _ := os.ReadFile(filepath.Join(sh, "skills", "s1", "SKILL.md")); string(b) != "skill" {
		t.Fatal("linked directory not copied")
	}
	if l, e := os.Readlink(filepath.Join(sh, "skills", "nested")); e != nil || l != "../p1" {
		t.Fatal("nested symlink not recreated as symlink")
	}
	if b, _ := os.ReadFile(filepath.Join(sh, "CLAUDE.md")); string(b) != "memo" || mode(t, filepath.Join(sh, "CLAUDE.md")).IsRegular() == false {
		t.Fatal("linked file not copied")
	}
}

func TestImportFailureLeavesNoPartialEntry(t *testing.T) {
	_, acc := setupAccounts(t)
	src := filepath.Join(filepath.Dir(acc), "claude-src")
	must(t, os.MkdirAll(filepath.Join(src, "projects", "p1"), 0700))
	must(t, os.WriteFile(filepath.Join(src, "projects", "p1", "a.jsonl"), []byte("a"), 0600))
	must(t, os.WriteFile(filepath.Join(src, "CLAUDE.md"), []byte("memo"), 0600))
	real := importCopy
	importCopy = func(from, to string) error {
		if filepath.Base(from) == "projects" {
			must(t, os.MkdirAll(filepath.Join(to, "p1"), 0700))
			must(t, os.WriteFile(filepath.Join(to, "p1", "partial"), []byte("x"), 0600))
			return errors.New("injected")
		}
		return real(from, to)
	}
	t.Cleanup(func() { importCopy = real })
	if _, _, e := ImportShared(src); e == nil {
		t.Fatal("failure not reported")
	}
	sh := SharedHome()
	if ents, _ := os.ReadDir(filepath.Join(sh, "projects")); len(ents) != 0 {
		t.Fatalf("partial entry left: %v", ents)
	}
	ents, _ := os.ReadDir(sh)
	for _, ent := range ents {
		if strings.Contains(ent.Name(), ".cma-import") {
			t.Fatalf("temporary entry left: %s", ent.Name())
		}
	}
	importCopy = real
	copied, _, e := ImportShared(src)
	must(t, e)
	if !contains(copied, "projects") {
		t.Fatalf("not importable afterwards: %v", copied)
	}
	if b, _ := os.ReadFile(filepath.Join(sh, "projects", "p1", "a.jsonl")); string(b) != "a" {
		t.Fatal("retry did not copy")
	}
}
