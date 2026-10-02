//go:build linux

// Package install provides explicit, tracked user-local binary installation.
package install

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

const Marker = ".cma-install.json"

type shellMarker struct {
	RC    string `json:"rc"`
	Block string `json:"block"`
}
type manifest struct {
	Version int               `json:"version"`
	Target  string            `json:"target"`
	Files   map[string]string `json:"files"`
	Shell   *shellMarker      `json:"shell,omitempty"`
}

var executable = os.Executable

func digest(data []byte) string { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }
func quote(s string) string     { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

// noSymlinks checks every ancestor, including missing final components. Operations
// fail closed for static symlinks; this is not a sandbox against malicious same-UID races.
func noSymlinks(path string) error {
	for p := path; ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return errors.New("Managed paths cannot traverse symlinks")
			}
			st, ok := info.Sys().(*syscall.Stat_t)
			if !ok {
				return errors.New("Cannot inspect path ownership")
			}
			if st.Uid != uint32(os.Getuid()) && st.Uid != 0 {
				return errors.New("Managed paths have unsafe ownership")
			}
		} else if !os.IsNotExist(err) {
			return errors.New("Cannot inspect managed path")
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	return nil
}
func ownedRegular(path string) (os.FileInfo, error) {
	if err := noSymlinks(path); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != uint32(os.Getuid()) || !info.Mode().IsRegular() {
		return nil, errors.New("Managed path must be an owned regular file")
	}
	return info, nil
}
func normalizeTarget(target string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if target == "~" {
		target = home
	} else if strings.HasPrefix(target, "~/") {
		target = filepath.Join(home, target[2:])
	}
	if target == "" {
		return "", errors.New("Install target must be a dedicated directory")
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return "", err
	}
	target = filepath.Clean(target)
	if target == "/" || target == filepath.Clean(home) {
		return "", errors.New("Install target must be a dedicated directory")
	}
	if err := noSymlinks(target); err != nil {
		return "", err
	}
	if info, err := os.Lstat(target); err == nil {
		st := info.Sys().(*syscall.Stat_t)
		if !info.IsDir() || st.Uid != uint32(os.Getuid()) {
			return "", errors.New("Install target must be an owned directory")
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	return target, nil
}
func validName(name string) bool { return name == "bin/cma" || name == "bin/claude-cma" }
func readManifest(target string) (*manifest, error) {
	path := filepath.Join(target, Marker)
	if _, err := ownedRegular(path); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("Cannot read install marker")
	}
	var m manifest
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&m); err != nil {
		return nil, errors.New("Invalid install marker")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, errors.New("Invalid install marker")
	}
	if m.Version != 1 || m.Target != target || m.Files == nil || len(m.Files) != 2 {
		return nil, errors.New("Install marker does not match target")
	}
	for name, hash := range m.Files {
		b, err := hex.DecodeString(hash)
		if !validName(name) || err != nil || len(b) != 32 || hash != strings.ToLower(hash) {
			return nil, errors.New("Unsafe install file list")
		}
	}
	if m.Shell != nil {
		home, _ := os.UserHomeDir()
		if m.Shell.RC != filepath.Join(home, ".bashrc") && m.Shell.RC != filepath.Join(home, ".zshrc") {
			return nil, errors.New("Unsafe shell marker")
		}
		if m.Shell.Block != shellBlock(target) {
			return nil, errors.New("Unsafe or modified shell marker")
		}
		if err := noSymlinks(m.Shell.RC); err != nil {
			return nil, err
		}
		if _, err := ownedRegular(m.Shell.RC); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	return &m, nil
}
func safeFile(target, name string) (string, error) {
	if !validName(name) && name != Marker {
		return "", errors.New("Unsafe managed filename")
	}
	path := filepath.Join(target, filepath.FromSlash(name))
	if err := noSymlinks(path); err != nil {
		return "", err
	}
	if _, err := ownedRegular(path); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return path, nil
}
func atomic(path string, data []byte, mode os.FileMode) error {
	if err := noSymlinks(path); err != nil {
		return err
	}
	if _, err := ownedRegular(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return errors.New("Cannot create install directory")
	}
	if err := noSymlinks(path); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".cma-")
	if err != nil {
		return errors.New("Cannot stage managed file")
	}
	name := file.Name()
	defer os.Remove(name)
	if err = file.Chmod(mode); err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return errors.New("Cannot write managed file")
	}
	if err := noSymlinks(path); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return errors.New("Cannot replace managed file")
	}
	return nil
}
func shellBlock(target string) string {
	key := digest([]byte(target))[:16]
	// The Bash/Zsh function keyword prevents expansion of pre-existing aliases.
	return fmt.Sprintf("\n# >>> claude-multi-account %s >>>\nexport PATH=%s:\"$PATH\"\nfunction claude() { %s launch -- \"$@\"; }\n# <<< claude-multi-account %s <<<\n", key, quote(filepath.Join(target, "bin")), quote(filepath.Join(target, "bin", "cma")), key)
}
func shellData(target string) (*shellMarker, error) {
	shell := filepath.Base(os.Getenv("SHELL"))
	if shell == "." || shell == "" {
		shell = "bash"
	}
	if shell != "bash" && shell != "zsh" {
		return nil, errors.New("Shell integration supports bash and zsh only")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	rc := filepath.Join(home, "."+shell+"rc")
	if err := noSymlinks(rc); err != nil {
		return nil, err
	}
	if _, err := ownedRegular(rc); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return &shellMarker{rc, shellBlock(target)}, nil
}
func contents(path string) ([]byte, os.FileMode, error) {
	info, err := ownedRegular(path)
	if os.IsNotExist(err) {
		return []byte{}, 0600, nil
	}
	if err != nil {
		return nil, 0, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, errors.New("Cannot read owned file")
	}
	return data, info.Mode().Perm(), nil
}

// Install copies the running compiled executable, never source or a PATH wrapper.
// Shell changes are opt-in and all collisions are checked before any write.
func Install(target string, shell bool) (map[string]any, error) {
	target, err := normalizeTarget(target)
	if err != nil {
		return nil, err
	}
	old, err := readManifest(target)
	if err != nil {
		return nil, err
	}
	if old == nil {
		old = &manifest{Version: 1, Target: target, Files: map[string]string{}}
	}
	source, err := executable()
	if err != nil {
		return nil, errors.New("Cannot locate running executable")
	}
	source, err = filepath.EvalSymlinks(source)
	if err != nil {
		return nil, errors.New("Cannot resolve running executable")
	}
	info, err := os.Stat(source)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return nil, errors.New("Running executable is not a regular executable")
	}
	binary, err := os.ReadFile(source)
	if err != nil {
		return nil, errors.New("Cannot read running executable")
	}
	payloads := map[string][]byte{"bin/cma": binary, "bin/claude-cma": []byte("#!/bin/sh\n# CMA_MANAGED_SHIM: stable compiled entry point.\nexec " + quote(filepath.Join(target, "bin", "cma")) + " launch -- \"$@\"\n")}
	names := []string{"bin/cma", "bin/claude-cma"}
	for _, name := range names {
		path, err := safeFile(target, name)
		if err != nil {
			return nil, err
		}
		data, _, err := contents(path)
		if err != nil {
			return nil, err
		}
		if _, err := os.Lstat(path); err == nil {
			hash, managed := old.Files[name]
			// An untracked file identical to the payload is adopted: nothing is lost.
			if digest(data) != digest(payloads[name]) && (!managed || digest(data) != hash) {
				return nil, fmt.Errorf("Refusing to overwrite unmanaged or modified file: %s", name)
			}
		}
	}
	integration := old.Shell
	var rcContent []byte
	var rcMode os.FileMode
	if shell {
		integration, err = shellData(target)
		if err != nil {
			return nil, err
		}
		if old.Shell != nil && *old.Shell != *integration {
			return nil, errors.New("Existing shell integration differs; uninstall it first")
		}
		rcContent, rcMode, err = contents(integration.RC)
		if err != nil {
			return nil, err
		}
		start := "# >>> claude-multi-account " + digest([]byte(target))[:16] + " >>>"
		if !strings.Contains(string(rcContent), integration.Block) && strings.Contains(string(rcContent), start) {
			return nil, errors.New("Shell integration was edited; preserve it and resolve manually")
		}
		if strings.Count(string(rcContent), start) > 1 {
			return nil, errors.New("Duplicate shell integration marker")
		}
	}
	if err := os.MkdirAll(target, 0700); err != nil {
		return nil, errors.New("Cannot create install target")
	}
	if err := noSymlinks(target); err != nil {
		return nil, err
	}
	files := map[string]string{}
	for _, name := range names {
		if err := atomic(filepath.Join(target, name), payloads[name], 0755); err != nil {
			return nil, err
		}
		files[name] = digest(payloads[name])
	}
	if shell && !strings.Contains(string(rcContent), integration.Block) {
		if err := atomic(integration.RC, append(rcContent, []byte(integration.Block)...), rcMode); err != nil {
			return nil, err
		}
	}
	m := manifest{1, target, files, integration}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := atomic(filepath.Join(target, Marker), append(data, '\n'), 0600); err != nil {
		return nil, err
	}
	return map[string]any{"target": target, "bin": filepath.Join(target, "bin"), "shell_integrated": integration != nil, "files": names}, nil
}

// Uninstall removes only matching tracked files and an exact reconstructed shell
// block. Modified files, arbitrary user files and modified blocks are preserved.
func Uninstall(target string) (map[string]any, error) {
	target, err := normalizeTarget(target)
	if err != nil {
		return nil, err
	}
	m, err := readManifest(target)
	if err != nil {
		return nil, err
	}
	removed, preserved := []string{}, []string{}
	result := map[string]any{"target": target, "removed": removed, "preserved": preserved}
	if m == nil {
		return result, nil
	}
	names := []string{}
	paths := map[string]string{}
	for name := range m.Files {
		path, err := safeFile(target, name)
		if err != nil {
			return nil, err
		}
		paths[name] = path
		names = append(names, name)
	}
	sort.Strings(names)
	var rcContent []byte
	var rcMode os.FileMode
	if m.Shell != nil {
		rcContent, rcMode, err = contents(m.Shell.RC)
		if err != nil {
			return nil, err
		}
	}
	for _, name := range names {
		path := paths[name]
		data, _, err := contents(path)
		if err != nil {
			return nil, err
		}
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			continue
		}
		if digest(data) != m.Files[name] {
			preserved = append(preserved, name)
			continue
		}
		if err := os.Remove(path); err != nil {
			return nil, errors.New("Cannot remove managed file")
		}
		removed = append(removed, name)
	}
	if m.Shell != nil {
		if strings.Contains(string(rcContent), m.Shell.Block) {
			content := strings.Replace(string(rcContent), m.Shell.Block, "", 1)
			if err := atomic(m.Shell.RC, []byte(content), rcMode); err != nil {
				return nil, err
			}
		} else if _, err := os.Lstat(m.Shell.RC); err == nil {
			preserved = append(preserved, m.Shell.RC)
		}
	}
	if err := os.Remove(filepath.Join(target, Marker)); err != nil {
		return nil, errors.New("Cannot remove install marker")
	}
	_ = os.Remove(filepath.Join(target, "bin"))
	_ = os.Remove(target)
	result["removed"] = removed
	result["preserved"] = preserved
	return result, nil
}
