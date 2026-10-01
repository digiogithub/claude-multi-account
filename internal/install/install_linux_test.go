//go:build linux

package install

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func setup(t *testing.T) (string, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/bash")
	target := filepath.Join(home, "stable target 'quoted'")
	return home, target
}
func mustInstall(t *testing.T, target string, shell bool) map[string]any {
	t.Helper()
	m, err := Install(target, shell)
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
func TestCopyExecutableIdempotentAndIndependent(t *testing.T) {
	home, target := setup(t)
	first := mustInstall(t, target, false)
	second := mustInstall(t, target, false)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("not idempotent")
	}
	binary, _ := os.Executable()
	source, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	installed := read(t, filepath.Join(target, "bin", "cma"))
	if installed != string(source) {
		t.Fatal("not current executable copy")
	}
	info, _ := os.Stat(filepath.Join(target, "bin", "cma"))
	if info.Mode().Perm() != 0755 {
		t.Fatal("binary permissions")
	}
	if _, err := os.Stat(filepath.Join(target, "bin", "claude")); !os.IsNotExist(err) {
		t.Fatal("claude binary overwritten")
	}
	if _, err := os.Stat(filepath.Join(home, ".bashrc")); !os.IsNotExist(err) {
		t.Fatal("implicit shell change")
	}
	// The copied test binary must still execute from another cwd with no source imports.
	cmd := exec.Command(filepath.Join(target, "bin", "cma"), "-test.run=^TestInstalledExecutableHelper$")
	cmd.Dir = home
	cmd.Env = append(os.Environ(), "CMA_INSTALLED_HELPER=1")
	output, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "installed helper") {
		t.Fatalf("copied executable: %v %s", err, output)
	}
}
func TestInstalledExecutableHelper(t *testing.T) {
	if os.Getenv("CMA_INSTALLED_HELPER") == "1" {
		println("installed helper")
	}
}
func TestShimLaunchTerminatorAndArguments(t *testing.T) {
	home, target := setup(t)
	fake := filepath.Join(home, "compiled-fake")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	old := executable
	executable = func() (string, error) { return fake, nil }
	t.Cleanup(func() { executable = old })
	mustInstall(t, target, false)
	cmd := exec.Command(filepath.Join(target, "bin", "claude-cma"), "prompt with spaces", "--continue", "'quoted'")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("shim: %v %s", err, out)
	}
	if string(out) != "launch\n--\nprompt with spaces\n--continue\n'quoted'\n" {
		t.Fatalf("argv %q", out)
	}
}
func TestShellOptInIdempotentPreservesUserEditsAndAlias(t *testing.T) {
	home, target := setup(t)
	rc := filepath.Join(home, ".bashrc")
	original := "shopt -s expand_aliases\nalias claude='echo existing-user-alias'\nexport USER_SETTING=original\n"
	mustWrite(t, rc, original)
	mustInstall(t, target, true)
	mustInstall(t, target, true)
	content := read(t, rc)
	if strings.Count(content, "function claude()") != 1 {
		t.Fatal("duplicate block")
	}
	cmd := exec.Command("bash", "--noprofile", "--norc", "-c", `source "$1"; declare -F claude; alias claude`, "bash", rc)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("alias-safe parsing: %v %s", err, out)
	}
	if !strings.Contains(string(out), "alias claude='echo existing-user-alias'") {
		t.Fatal("alias overwritten")
	}
	mustWrite(t, rc, content+"# later user edit\n")
	result, err := Uninstall(target)
	if err != nil {
		t.Fatal(err)
	}
	if read(t, rc) != original+"# later user edit\n" {
		t.Fatal("user content altered")
	}
	if len(result["removed"].([]string)) != 2 {
		t.Fatal(result)
	}
	if _, err := os.Stat(filepath.Join(target, Marker)); !os.IsNotExist(err) {
		t.Fatal("marker retained")
	}
}
func TestFunctionUsesStableLaunch(t *testing.T) {
	home, target := setup(t)
	fake := filepath.Join(home, "fake-executable")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	old := executable
	executable = func() (string, error) { return fake, nil }
	t.Cleanup(func() { executable = old })
	mustInstall(t, target, true)
	cmd := exec.Command("bash", "--noprofile", "--norc", "-c", `source "$1"; claude 'a b' --continue`, "bash", filepath.Join(home, ".bashrc"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("function: %v %s", err, out)
	}
	if string(out) != "launch\n--\na b\n--continue\n" {
		t.Fatalf("argv %q", out)
	}
}
func TestUninstallPreservesModifiedAndUnmanaged(t *testing.T) {
	_, target := setup(t)
	mustInstall(t, target, false)
	mustWrite(t, filepath.Join(target, "notes.txt"), "keep")
	mustWrite(t, filepath.Join(target, "bin", "cma"), "user edit")
	result, err := Uninstall(target)
	if err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(target, "notes.txt")) != "keep" || read(t, filepath.Join(target, "bin", "cma")) != "user edit" {
		t.Fatal("user file altered")
	}
	if !reflect.DeepEqual(result["preserved"], []string{"bin/cma"}) {
		t.Fatal(result)
	}
	if _, err := os.Stat(filepath.Join(target, "bin", "claude-cma")); !os.IsNotExist(err) {
		t.Fatal("owned shim retained")
	}
}
func TestUnmanagedCollisionPreflight(t *testing.T) {
	home, target := setup(t)
	collision := filepath.Join(target, "bin", "claude-cma")
	mustWrite(t, collision, "keep")
	if _, err := Install(target, true); err == nil {
		t.Fatal("unmanaged collision overwritten")
	}
	if read(t, collision) != "keep" {
		t.Fatal("collision changed")
	}
	for _, path := range []string{filepath.Join(target, "bin", "cma"), filepath.Join(home, ".bashrc"), filepath.Join(target, Marker)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("preflight wrote " + path)
		}
	}
}
func TestModifiedFileInstallRefused(t *testing.T) {
	_, target := setup(t)
	mustInstall(t, target, false)
	path := filepath.Join(target, "bin", "cma")
	mustWrite(t, path, "user edit")
	if _, err := Install(target, false); err == nil {
		t.Fatal("modified managed file overwritten")
	}
	if read(t, path) != "user edit" {
		t.Fatal("modified file changed")
	}
}
func TestNoMarkerNoopAndDedicatedTargets(t *testing.T) {
	home, target := setup(t)
	mustWrite(t, filepath.Join(target, "keep"), "keep")
	result, err := Uninstall(target)
	if err != nil || len(result["removed"].([]string)) != 0 {
		t.Fatal(result, err)
	}
	if read(t, filepath.Join(target, "keep")) != "keep" {
		t.Fatal("untracked changed")
	}
	for _, target := range []string{"", home, "/", "~"} {
		if _, err := Install(target, false); err == nil {
			t.Fatal("unsafe target accepted " + target)
		}
	}
}
func TestTamperedMarkerFailsBeforeDeletion(t *testing.T) {
	for _, kind := range []string{"traversal", "extra-file", "bad-digest", "wrong-target", "unknown-field", "null-files", "shell-content", "shell-path", "wrong-version"} {
		t.Run(kind, func(t *testing.T) {
			home, target := setup(t)
			mustInstall(t, target, true)
			path := filepath.Join(target, Marker)
			var obj map[string]any
			if err := json.Unmarshal([]byte(read(t, path)), &obj); err != nil {
				t.Fatal(err)
			}
			files := obj["files"].(map[string]any)
			switch kind {
			case "traversal":
				files["../victim"] = "bad"
			case "extra-file":
				files["bin/victim"] = digest([]byte("keep"))
			case "bad-digest":
				files["bin/cma"] = "bad"
			case "wrong-target":
				obj["target"] = home
			case "unknown-field":
				obj["unexpected"] = true
			case "null-files":
				obj["files"] = nil
			case "shell-content":
				obj["shell"].(map[string]any)["block"] = "arbitrary"
			case "shell-path":
				obj["shell"].(map[string]any)["rc"] = filepath.Join(home, "victim")
			case "wrong-version":
				obj["version"] = 2
			}
			data, _ := json.Marshal(obj)
			mustWrite(t, path, string(data))
			if _, err := Uninstall(target); err == nil {
				t.Fatal("tampered marker accepted")
			}
			if _, err := os.Stat(filepath.Join(target, "bin", "cma")); err != nil {
				t.Fatal("deleted before full validation")
			}
		})
	}
}
func TestSymlinksRefusedEverywhere(t *testing.T) {
	for _, kind := range []string{"target", "ancestor", "bin", "file", "marker", "rc", "home-ancestor"} {
		t.Run(kind, func(t *testing.T) {
			home, target := setup(t)
			victim := filepath.Join(home, "victim")
			mustWrite(t, victim, "keep")
			switch kind {
			case "target":
				if err := os.Symlink(home, target); err != nil {
					t.Fatal(err)
				}
			case "ancestor":
				link := filepath.Join(home, "linked")
				if err := os.Symlink(home, link); err != nil {
					t.Fatal(err)
				}
				target = filepath.Join(link, "target")
			case "bin":
				if err := os.Mkdir(target, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(home, filepath.Join(target, "bin")); err != nil {
					t.Fatal(err)
				}
			case "file", "marker":
				mustInstall(t, target, false)
				name := "bin/cma"
				if kind == "marker" {
					name = Marker
				}
				path := filepath.Join(target, name)
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(victim, path); err != nil {
					t.Fatal(err)
				}
			case "rc":
				if err := os.Symlink(victim, filepath.Join(home, ".bashrc")); err != nil {
					t.Fatal(err)
				}
			case "home-ancestor":
				actual := filepath.Join(home, "actual")
				if err := os.Mkdir(actual, 0700); err != nil {
					t.Fatal(err)
				}
				link := filepath.Join(home, "linked-home")
				if err := os.Symlink(actual, link); err != nil {
					t.Fatal(err)
				}
				t.Setenv("HOME", link)
			}
			if _, err := Install(target, true); err == nil {
				t.Fatal("symlink accepted")
			}
			// A marker-free target is intentionally a no-op, even with an untracked bin.
			if kind != "rc" && kind != "home-ancestor" && kind != "bin" {
				if _, err := Uninstall(target); err == nil {
					t.Fatal("uninstall symlink accepted")
				}
			}
			if read(t, victim) != "keep" {
				t.Fatal("symlink victim changed")
			}
		})
	}
}
func TestModifiedShellBlockPreservedAndReinstallRefused(t *testing.T) {
	home, target := setup(t)
	mustInstall(t, target, true)
	rc := filepath.Join(home, ".bashrc")
	content := strings.Replace(read(t, rc), `"$@"`, `"user-edited"`, 1)
	mustWrite(t, rc, content)
	if _, err := Install(target, true); err == nil {
		t.Fatal("edited shell block overwritten")
	}
	result, err := Uninstall(target)
	if err != nil {
		t.Fatal(err)
	}
	if read(t, rc) != content || !reflect.DeepEqual(result["preserved"], []string{rc}) {
		t.Fatal("edited block not preserved", result)
	}
}
func TestZshUnsupportedShellAndModes(t *testing.T) {
	home, target := setup(t)
	t.Setenv("SHELL", "/bin/zsh")
	mustInstall(t, target, true)
	rc := filepath.Join(home, ".zshrc")
	info, _ := os.Stat(rc)
	if info.Mode().Perm() != 0600 {
		t.Fatal("new rc mode")
	}
	if !strings.Contains(read(t, rc), "function claude()") {
		t.Fatal("zsh block")
	}
	if zsh, err := exec.LookPath("zsh"); err == nil {
		cmd := exec.Command(zsh, "-f", "-c", `alias claude='echo alias'; source "$1"; functions claude`, "zsh", rc)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("zsh syntax %v %s", err, out)
		}
	} else {
		t.Log("zsh executable unavailable; generated block verified")
	}
	if _, err := Uninstall(target); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", "/bin/fish")
	if _, err := Install(target, true); err == nil {
		t.Fatal("unsupported shell accepted")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("unsupported shell wrote install")
	}
	t.Setenv("SHELL", "/bin/bash")
	rc = filepath.Join(home, ".bashrc")
	mustWrite(t, rc, "# user\n")
	if err := os.Chmod(rc, 0640); err != nil {
		t.Fatal(err)
	}
	mustInstall(t, target, true)
	info, _ = os.Stat(rc)
	if info.Mode().Perm() != 0640 {
		t.Fatal("existing rc mode changed")
	}
}
func TestNonregularManagedAndRCRefused(t *testing.T) {
	for _, kind := range []string{"file", "marker", "rc"} {
		t.Run(kind, func(t *testing.T) {
			home, target := setup(t)
			path := filepath.Join(target, "bin", "cma")
			if kind == "marker" {
				path = filepath.Join(target, Marker)
			} else if kind == "rc" {
				path = filepath.Join(home, ".bashrc")
			}
			if err := os.MkdirAll(path, 0700); err != nil {
				t.Fatal(err)
			}
			if _, err := Install(target, true); err == nil {
				t.Fatal("nonregular accepted")
			}
		})
	}
}
