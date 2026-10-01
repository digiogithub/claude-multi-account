//go:build linux

package core

import (
	"errors"
	"os"
	"path/filepath"
)

func resolved(path string) (string, error) {
	p, e := filepath.Abs(path)
	if e != nil {
		return "", errors.New("Invalid local path.")
	}
	p, e = filepath.EvalSymlinks(p)
	if e != nil {
		return "", errors.New("Local directory is missing or inaccessible.")
	}
	return p, nil
}
func ProjectRoot(cwd string) (string, error) {
	cwd, e := resolved(cwd)
	if e != nil {
		return "", e
	}
	i, e := os.Stat(cwd)
	if e != nil || !i.IsDir() {
		return "", errors.New("Project working directory does not exist.")
	}
	h, _ := resolved(home())
	dirs := []string{}
	for d := cwd; ; d = filepath.Dir(d) {
		dirs = append(dirs, d)
		if d == h || d == filepath.Dir(d) {
			break
		}
	}
	for _, d := range dirs {
		if _, e = os.Stat(filepath.Join(d, ".git")); e == nil {
			return d, nil
		}
	}
	for _, d := range dirs {
		if exists(filepath.Join(d, ".claude", "accounts.local.json")) {
			return d, nil
		}
	}
	return cwd, nil
}
func DefaultProject() Project {
	return Project{Version: 1, Memory: Memory{Mode: "shared"}, Switching: Switching{ConfirmContextTransfer: true}}
}
func validateProject(p Project) error {
	if p.Version != 1 {
		return errors.New("Unsupported project configuration version.")
	}
	if p.Profile != nil {
		if e := ValidateProfileID(*p.Profile); e != nil {
			return e
		}
	}
	if p.Memory.Mode != "shared" && p.Memory.Mode != "per-profile" {
		return errors.New("Invalid project memory policy.")
	}
	seen := map[string]bool{}
	for _, id := range p.AllowedProfiles {
		if e := ValidateProfileID(id); e != nil {
			return e
		}
		if seen[id] {
			return errors.New("allowedProfiles must not contain duplicates.")
		}
		seen[id] = true
	}
	return nil
}
func CheckAllowed(p Project, id string) error {
	if e := ValidateProfileID(id); e != nil {
		return e
	}
	if p.AllowedProfiles != nil {
		for _, allowed := range p.AllowedProfiles {
			if allowed == id {
				return nil
			}
		}
		return errors.New("Profile is not permitted by this project's allowedProfiles policy.")
	}
	return nil
}
func LoadProject(cwd string) (string, Project, error) {
	root, e := ProjectRoot(cwd)
	p := DefaultProject()
	if e != nil {
		return "", p, e
	}
	path := filepath.Join(root, ".claude", "accounts.local.json")
	if !exists(path) {
		return root, p, nil
	}
	if e = ReadJSON(path, &p, false); e != nil {
		return root, p, e
	}
	if p.Profile != nil {
		if _, e = GetProfile(*p.Profile); e != nil {
			return root, p, e
		}
		if e = CheckAllowed(p, *p.Profile); e != nil {
			return root, p, e
		}
	}
	return root, p, nil
}
func SaveProject(root string, p Project) error {
	root, e := resolved(root)
	if e != nil {
		return e
	}
	if e = validateProject(p); e != nil {
		return e
	}
	if p.Profile != nil {
		if _, e = GetProfile(*p.Profile); e != nil {
			return e
		}
		if e = CheckAllowed(p, *p.Profile); e != nil {
			return e
		}
	}
	dir := filepath.Join(root, ".claude")
	if e = noLinks(dir); e != nil {
		return e
	}
	if e = os.Mkdir(dir, 0700); e != nil && !os.IsExist(e) {
		return errors.New("Cannot create project configuration directory.")
	}
	return AtomicWriteJSON(filepath.Join(dir, "accounts.local.json"), p)
}
