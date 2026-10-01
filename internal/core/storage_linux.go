//go:build linux

package core

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf8"
)

const service = "claude-multi-account"

func home() string { h, _ := os.UserHomeDir(); return h }
func absolute(path string) string {
	if path == "~" {
		path = home()
	} else if strings.HasPrefix(path, "~/") {
		path = filepath.Join(home(), path[2:])
	}
	p, e := filepath.Abs(path)
	if e != nil {
		return filepath.Clean(path)
	}
	return p
}
func ConfigHome() string {
	if p := os.Getenv("CMA_CONFIG_HOME"); p != "" {
		return absolute(p)
	}
	p := os.Getenv("XDG_CONFIG_HOME")
	if p == "" {
		p = filepath.Join(home(), ".config")
	}
	return absolute(filepath.Join(p, service))
}
func StateHome() string {
	if p := os.Getenv("CMA_STATE_HOME"); p != "" {
		return absolute(p)
	}
	p := os.Getenv("XDG_STATE_HOME")
	if p == "" {
		p = filepath.Join(home(), ".local/state")
	}
	return absolute(filepath.Join(p, service))
}
func noLinks(path string) error {
	path = absolute(path)
	for {
		i, e := os.Lstat(path)
		if e == nil && i.Mode()&os.ModeSymlink != 0 {
			return errors.New("Refusing symbolic-link state/configuration directory.")
		}
		if e != nil && !os.IsNotExist(e) {
			return errors.New("State/configuration directory is inaccessible.")
		}
		next := filepath.Dir(path)
		if next == path {
			break
		}
		path = next
	}
	return nil
}
func privateInfo(i os.FileInfo, directory bool) error {
	valid := i.Mode().IsRegular()
	if directory {
		valid = i.IsDir()
	}
	st, ok := i.Sys().(*syscall.Stat_t)
	if !valid || !ok || st.Uid != uint32(os.Getuid()) || i.Mode().Perm()&0077 != 0 {
		return errors.New("Private state must be owned by this user and have mode 0700 (directories) or 0600 (files).")
	}
	return nil
}
func private(path string, directory bool) error {
	ancestor := filepath.Dir(path)
	if directory {
		ancestor = path
	}
	if e := noLinks(ancestor); e != nil {
		return e
	}
	i, e := os.Lstat(path)
	if e != nil {
		return errors.New("Private state is missing or inaccessible.")
	}
	return privateInfo(i, directory)
}
func SecureDirectory(path string) error {
	if e := noLinks(path); e != nil {
		return e
	}
	if e := os.MkdirAll(path, 0700); e != nil {
		return errors.New("Cannot create private state directory.")
	}
	return private(path, true)
}
func ReadJSON(path string, out any, isPrivate bool) error {
	if e := noLinks(filepath.Dir(path)); e != nil {
		return e
	}
	if isPrivate {
		if e := private(path, false); e != nil {
			return e
		}
	}
	fd, e := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if e != nil {
		return errJSON
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() > 1024*1024 {
		return errors.New("JSON state must be a regular file smaller than 1 MiB.")
	}
	if isPrivate {
		if e = privateInfo(info, false); e != nil {
			return e
		}
	}
	data, e := io.ReadAll(io.LimitReader(f, 1024*1024+1))
	if e != nil || len(data) > 1024*1024 || !utf8.Valid(data) {
		return errJSON
	}
	if _, e = parseJSON(data); e != nil {
		return e
	}
	if e = json.Unmarshal(data, out); e != nil {
		return errJSON
	}
	return nil
}
func AtomicWriteJSON(path string, value any) error {
	if i, e := os.Lstat(path); e == nil && i.Mode()&os.ModeSymlink != 0 {
		return errors.New("Refusing to write through a symbolic link.")
	} else if e != nil && !os.IsNotExist(e) {
		return errors.New("Cannot write local configuration/state.")
	}
	parent := filepath.Dir(path)
	if e := noLinks(parent); e != nil {
		return e
	}
	if _, e := os.Stat(parent); os.IsNotExist(e) {
		if e = SecureDirectory(parent); e != nil {
			return e
		}
	}
	data, e := json.MarshalIndent(value, "", "  ")
	if e != nil {
		return errors.New("Cannot encode local configuration/state.")
	}
	data = append(data, '\n')
	f, e := os.CreateTemp(parent, ".cma-")
	if e != nil {
		return errors.New("Cannot write local configuration/state.")
	}
	name := f.Name()
	defer os.Remove(name)
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(data)
	}
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e == nil {
		e = os.Rename(name, path)
	}
	if e == nil {
		var d *os.File
		d, e = os.Open(parent)
		if e == nil {
			e = d.Sync()
			d.Close()
		}
	}
	if e != nil {
		return errors.New("Cannot write local configuration/state.")
	}
	return nil
}
func registryLock() (func(), error) {
	if e := SecureDirectory(ConfigHome()); e != nil {
		return nil, e
	}
	path := filepath.Join(ConfigHome(), "registry.lock")
	fd, e := syscall.Open(path, syscall.O_RDWR|syscall.O_CREAT|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0600)
	if e != nil {
		return nil, errors.New("Cannot lock profile registry.")
	}
	f := os.NewFile(uintptr(fd), path)
	info, e := f.Stat()
	if e == nil {
		e = privateInfo(info, false)
	}
	if e != nil {
		f.Close()
		return nil, e
	}
	if e = syscall.Flock(fd, syscall.LOCK_EX); e != nil {
		f.Close()
		return nil, errors.New("Cannot lock profile registry.")
	}
	return func() { syscall.Flock(fd, syscall.LOCK_UN); f.Close() }, nil
}
func exists(path string) bool { _, e := os.Lstat(path); return !os.IsNotExist(e) }
