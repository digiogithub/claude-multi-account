//go:build linux

package core

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

// ConfigDirBackend marks a profile whose login lives in its own Claude configuration directory.
const ConfigDirBackend = "config-dir"

// AccountsHome is the root of all config-dir profiles and the shared directory.
func AccountsHome() string {
	if p := os.Getenv("CMA_ACCOUNTS_HOME"); p != "" {
		return absolute(p)
	}
	return absolute(filepath.Join(home(), ".claude-accounts"))
}

// SharedHome holds the real files that every config-dir profile links to.
func SharedHome() string {
	if p := os.Getenv("CMA_SHARED_HOME"); p != "" {
		return absolute(p)
	}
	return filepath.Join(AccountsHome(), "shared")
}

// ProfileDir is the CLAUDE_CONFIG_DIR of a config-dir profile.
func ProfileDir(id string) (string, error) {
	if e := ValidateProfileID(id); e != nil {
		return "", e
	}
	return filepath.Join(AccountsHome(), "profiles", id), nil
}

// duplicateProfileError hints at the command that fits the existing profile's mode.
func duplicateProfileError(existing Profile) error {
	if IsConfigDir(existing) {
		return errors.New("Profile already exists. To replace its login use `cma profiles login " + existing.ID + "`.")
	}
	return errors.New("Profile already exists. To replace its token use `cma profiles renew " + existing.ID + "`.")
}

func IsConfigDir(p Profile) bool { return p.SecretBackend == ConfigDirBackend }

type SharedEntry struct {
	Name          string
	Dir, Optional bool
}

// SharedEntries is the fixed list of names linked from every profile into shared.
var SharedEntries = []SharedEntry{
	{Name: "projects", Dir: true},
	{Name: "skills", Dir: true},
	{Name: "agents", Dir: true},
	{Name: "commands", Dir: true},
	{Name: "plugins", Dir: true},
	{Name: "todos", Dir: true},
	{Name: "plans", Dir: true},
	{Name: "file-history", Dir: true},
	{Name: "settings.json"},
	{Name: "CLAUDE.md"},
	{Name: "history.jsonl"},
	{Name: "keybindings.json", Optional: true},
}

type LinkReport struct {
	Linked    []string `json:"linked"`
	Present   []string `json:"present"`
	Conflicts []string `json:"conflicts"`
	Skipped   []string `json:"skipped"`
}

func newLinkReport() LinkReport {
	return LinkReport{[]string{}, []string{}, []string{}, []string{}}
}

// ensureShared creates missing shared targets; existing entries are never touched.
func ensureShared() error {
	shared := SharedHome()
	if e := SecureDirectory(shared); e != nil {
		return e
	}
	for _, s := range SharedEntries {
		if s.Optional {
			continue
		}
		path := filepath.Join(shared, s.Name)
		if exists(path) {
			continue
		}
		if s.Dir {
			if e := os.Mkdir(path, 0700); e != nil && !os.IsExist(e) {
				return errors.New("Cannot create shared directory.")
			}
			continue
		}
		content := ""
		if s.Name == "settings.json" {
			content = "{}\n"
		}
		f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0600)
		if e != nil {
			if os.IsExist(e) {
				continue
			}
			return errors.New("Cannot create shared file.")
		}
		_, e = f.WriteString(content)
		if c := f.Close(); e == nil {
			e = c
		}
		if e != nil {
			return errors.New("Cannot create shared file.")
		}
	}
	return nil
}

func linksTo(path, target string) bool {
	if l, e := os.Readlink(path); e == nil && l == target {
		return true
	}
	a, e1 := filepath.EvalSymlinks(path)
	b, e2 := filepath.EvalSymlinks(target)
	return e1 == nil && e2 == nil && a == b
}

// EnsureProfileDir creates the private profile directory and links shared entries; it never overwrites.
func EnsureProfileDir(id string) (string, LinkReport, error) {
	r := newLinkReport()
	dir, e := ProfileDir(id)
	if e != nil {
		return "", r, e
	}
	for _, d := range []string{AccountsHome(), filepath.Join(AccountsHome(), "profiles"), dir} {
		if e = SecureDirectory(d); e != nil {
			return "", r, e
		}
	}
	if e = ensureShared(); e != nil {
		return "", r, e
	}
	for _, s := range SharedEntries {
		target := filepath.Join(SharedHome(), s.Name)
		link := filepath.Join(dir, s.Name)
		_, le := os.Lstat(link)
		switch {
		case le == nil:
			if i, _ := os.Lstat(link); i.Mode()&os.ModeSymlink != 0 && linksTo(link, target) {
				r.Present = append(r.Present, s.Name)
			} else {
				r.Conflicts = append(r.Conflicts, s.Name)
			}
		case !os.IsNotExist(le):
			return "", r, errors.New("Profile directory is inaccessible.")
		case s.Optional && !exists(target):
			r.Skipped = append(r.Skipped, s.Name)
		default:
			if e = os.Symlink(target, link); e != nil {
				return "", r, errors.New("Cannot link shared entry " + s.Name + ".")
			}
			r.Linked = append(r.Linked, s.Name)
		}
	}
	return dir, r, nil
}

// LoginState never opens the credentials file; it only checks that it exists.
func LoginState(id string) string {
	dir, e := ProfileDir(id)
	if e != nil {
		return "absent"
	}
	if _, e = os.Lstat(filepath.Join(dir, ".credentials.json")); e != nil {
		return "absent"
	}
	return "present"
}

func AddConfigDirProfile(p Profile) (Profile, LinkReport, error) {
	r := newLinkReport()
	if e := ValidateProfileID(p.ID); e != nil {
		return Profile{}, r, e
	}
	var e error
	p.SecretBackend = ConfigDirBackend
	if p.SecretRef, e = newRef(); e != nil {
		return Profile{}, r, e
	}
	p.ExpiresAt = nil
	p.Verification = "unverified"
	if e = validateMetadata(p.ID, p); e != nil {
		return Profile{}, r, e
	}
	unlock, e := registryLock()
	if e != nil {
		return Profile{}, r, e
	}
	defer unlock()
	reg, e := LoadRegistry()
	if e != nil {
		return Profile{}, r, e
	}
	if old, ok := reg.Profiles[p.ID]; ok {
		return Profile{}, r, duplicateProfileError(old)
	}
	if _, r, e = EnsureProfileDir(p.ID); e != nil {
		return Profile{}, r, e
	}
	reg.Profiles[p.ID] = p
	if e = AtomicWriteJSON(filepath.Join(ConfigHome(), "profiles.json"), reg); e != nil {
		return Profile{}, r, e
	}
	return p, r, nil
}

// PurgeProfileDir deletes a profile directory; symlinks are unlinked, never followed.
func PurgeProfileDir(id string) error {
	dir, e := ProfileDir(id)
	if e != nil {
		return e
	}
	if e = noLinks(dir); e != nil {
		return e
	}
	i, e := os.Lstat(dir)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil || !i.IsDir() {
		return errors.New("Profile directory is inaccessible.")
	}
	entries, e := os.ReadDir(dir)
	if e != nil {
		return errors.New("Cannot read profile directory.")
	}
	for _, ent := range entries {
		path := filepath.Join(dir, ent.Name())
		fi, e := os.Lstat(path)
		if e != nil {
			return errors.New("Cannot inspect profile entry.")
		}
		if fi.IsDir() && fi.Mode()&os.ModeSymlink == 0 {
			e = os.RemoveAll(path)
		} else {
			e = os.Remove(path)
		}
		if e != nil {
			return errors.New("Cannot remove profile entry.")
		}
	}
	if e = os.Remove(dir); e != nil {
		return errors.New("Cannot remove profile directory.")
	}
	return nil
}

// resolveLoose resolves symlinks in the longest existing prefix of path.
func resolveLoose(path string) string {
	path = absolute(path)
	rest := ""
	for {
		if r, e := filepath.EvalSymlinks(path); e == nil {
			return filepath.Join(r, rest)
		}
		next := filepath.Dir(path)
		if next == path {
			return filepath.Join(path, rest)
		}
		rest = filepath.Join(filepath.Base(path), rest)
		path = next
	}
}

func inside(p, base string) bool {
	r, e := filepath.Rel(base, p)
	return e == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator))
}

func importable(path string, dir bool) bool {
	i, e := os.Lstat(path)
	if os.IsNotExist(e) {
		return true
	}
	if e != nil {
		return false
	}
	if dir {
		if !i.IsDir() {
			return false
		}
		ents, e := os.ReadDir(path)
		return e == nil && len(ents) == 0
	}
	if !i.Mode().IsRegular() {
		return false
	}
	if i.Size() == 0 {
		return true
	}
	if filepath.Base(path) != "settings.json" || i.Size() > 64 {
		return false
	}
	data, e := os.ReadFile(path)
	return e == nil && strings.TrimSpace(string(data)) == "{}"
}

func neverCopy(name string) bool { return name == ".credentials.json" || name == ".claude.json" }

func copyTree(src, dst string) error {
	i, e := os.Lstat(src)
	if e != nil {
		return errors.New("Cannot read import source.")
	}
	switch m := i.Mode(); {
	case m&os.ModeSymlink != 0:
		l, e := os.Readlink(src)
		if e == nil {
			e = os.Symlink(l, dst)
		}
		if e != nil {
			return errors.New("Cannot copy symbolic link.")
		}
	case m.IsDir():
		if e = os.Mkdir(dst, 0700); e != nil && !os.IsExist(e) {
			return errors.New("Cannot create shared directory.")
		}
		ents, e := os.ReadDir(src)
		if e != nil {
			return errors.New("Cannot read import source.")
		}
		for _, ent := range ents {
			if neverCopy(ent.Name()) {
				continue
			}
			if e = copyTree(filepath.Join(src, ent.Name()), filepath.Join(dst, ent.Name())); e != nil {
				return e
			}
		}
	case m.IsRegular():
		in, e := os.OpenFile(src, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if e != nil {
			return errors.New("Cannot read import source.")
		}
		defer in.Close()
		out, e := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0600)
		if e != nil {
			return errors.New("Cannot create shared file.")
		}
		_, e = io.Copy(out, in)
		if e == nil {
			e = out.Chmod(m.Perm() & 0700)
		}
		if c := out.Close(); e == nil {
			e = c
		}
		if e != nil {
			return errors.New("Cannot copy file.")
		}
	}
	return nil
}

// importCopy is a seam so tests can inject a failure midway through a copy.
var importCopy = copyTree

// ImportShared copies existing Claude data into shared where shared is missing or still a placeholder.
func ImportShared(from string) (copied, skipped []string, err error) {
	copied, skipped = []string{}, []string{}
	src, e := filepath.EvalSymlinks(absolute(from))
	if e != nil {
		return copied, skipped, errors.New("Import source does not exist.")
	}
	if i, e := os.Stat(src); e != nil || !i.IsDir() {
		return copied, skipped, errors.New("Import source must be a directory.")
	}
	for _, base := range []string{AccountsHome(), SharedHome()} {
		if inside(src, resolveLoose(base)) {
			return copied, skipped, errors.New("Refusing to import from the accounts or shared directory.")
		}
	}
	if e = SecureDirectory(SharedHome()); e != nil {
		return copied, skipped, e
	}
	bases := []string{resolveLoose(AccountsHome()), resolveLoose(SharedHome())}
	for _, s := range SharedEntries {
		from, to := filepath.Join(src, s.Name), filepath.Join(SharedHome(), s.Name)
		i, e := os.Lstat(from)
		if e == nil && i.Mode()&os.ModeSymlink != 0 {
			// a top-level link (dotfile managers) is followed once, unless it leads into our own homes
			resolved, e2 := filepath.EvalSymlinks(from)
			if e2 != nil || inside(resolved, bases[0]) || inside(resolved, bases[1]) {
				skipped = append(skipped, s.Name)
				continue
			}
			from = resolved
			i, e = os.Lstat(from)
		}
		if e != nil || i.IsDir() != s.Dir || (!i.IsDir() && !i.Mode().IsRegular()) || !importable(to, s.Dir) {
			skipped = append(skipped, s.Name)
			continue
		}
		// copy beside the target, then replace the empty or missing target only on success
		var tmp string
		if s.Dir {
			tmp, e = os.MkdirTemp(SharedHome(), s.Name+".cma-import-")
			if e != nil {
				return copied, skipped, errors.New("Cannot create shared directory.")
			}
		} else {
			tmp = to + ".cma-import"
			_ = os.Remove(tmp)
		}
		if e = importCopy(from, tmp); e == nil {
			e = os.Rename(tmp, to)
		}
		if e != nil {
			_ = os.RemoveAll(tmp)
			return copied, skipped, e
		}
		copied = append(copied, s.Name)
	}
	sort.Strings(copied)
	sort.Strings(skipped)
	return copied, skipped, nil
}
