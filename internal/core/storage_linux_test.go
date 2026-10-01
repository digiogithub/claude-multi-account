//go:build linux

package core

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestHomesAndPrivateDirectories(t *testing.T) {
	setup(t)
	t.Setenv("CMA_CONFIG_HOME", "")
	t.Setenv("CMA_STATE_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home(), "xdg-config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home(), "xdg-state"))
	if ConfigHome() != filepath.Join(home(), "xdg-config", service) || StateHome() != filepath.Join(home(), "xdg-state", service) {
		t.Fatal("XDG homes")
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	if ConfigHome() != filepath.Join(home(), ".config", service) || StateHome() != filepath.Join(home(), ".local", "state", service) {
		t.Fatal("default homes")
	}
	t.Setenv("CMA_CONFIG_HOME", "~/custom")
	if ConfigHome() != filepath.Join(home(), "custom") {
		t.Fatal("tilde expansion")
	}
	must(t, SecureDirectory(ConfigHome()))
	must(t, os.Chmod(ConfigHome(), 0755))
	reject(t, SecureDirectory(ConfigHome()))
}
func TestUnsafeRegistryLocks(t *testing.T) {
	setup(t)
	must(t, SecureDirectory(ConfigHome()))
	path := filepath.Join(ConfigHome(), "registry.lock")
	target := filepath.Join(home(), "target-lock")
	must(t, os.WriteFile(target, []byte("original"), 0600))
	must(t, os.Symlink(target, path))
	_, e := AddProfile(metadata("one"), "synthetic", "file")
	reject(t, e)
	must(t, os.Remove(path))
	must(t, os.WriteFile(path, nil, 0644))
	_, e = AddProfile(metadata("one"), "synthetic", "file")
	reject(t, e)
	must(t, os.Remove(path))
	must(t, syscall.Mkfifo(path, 0600))
	_, e = AddProfile(metadata("one"), "synthetic", "file")
	reject(t, e)
	b, e := os.ReadFile(target)
	must(t, e)
	if string(b) != "original" {
		t.Fatal("lock target changed")
	}
}
func TestReadRejectsNonRegularInvalidUTF8AndTrailingData(t *testing.T) {
	setup(t)
	must(t, SecureDirectory(ConfigHome()))
	path := filepath.Join(ConfigHome(), "data.json")
	var out map[string]any
	for _, data := range [][]byte{[]byte(`{"x":1} true`), []byte(`{"x":1,"x":2}`), []byte(`null`), {'{', '"', 'x', '"', ':', '"', 255, '"', '}'}} {
		must(t, os.WriteFile(path, data, 0600))
		reject(t, ReadJSON(path, &out, true))
	}
	must(t, os.Remove(path))
	must(t, syscall.Mkfifo(path, 0600))
	reject(t, ReadJSON(path, &out, true))
	must(t, os.Remove(path))
	must(t, os.Mkdir(path, 0700))
	reject(t, ReadJSON(path, &out, true))
	reject(t, AtomicWriteJSON(filepath.Join(ConfigHome(), "bad.json"), map[string]any{"bad": make(chan int)}))
}
