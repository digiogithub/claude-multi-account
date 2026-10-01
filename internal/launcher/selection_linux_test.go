//go:build linux

package launcher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"claude-multi-account/internal/core"
)

func selectionInput(t *testing.T, text string) {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "input")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(text); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	prior := os.Stdin
	os.Stdin = file
	t.Cleanup(func() { os.Stdin = prior; file.Close() })
}
func TestInteractiveSelectorNormalAndPersistent(t *testing.T) {
	for _, kind := range []string{"normal", "persist", "invalid", "eof"} {
		t.Run(kind, func(t *testing.T) {
			root, project := isolated(t)
			project.Profile = nil
			loadProject = func(string) (string, core.Project, error) { return root, project, nil }
			stdinTTY = func() bool { return true }
			persisted := false
			saveProject = func(root string, p core.Project) error {
				persisted = true
				if p.Profile == nil || *p.Profile != "one" {
					t.Fatal("incorrect persisted profile")
				}
				return nil
			}
			wait = func(command, env []string) (int, error) {
				if kind == "normal" && envMap(env)["CMA_MANAGED"] != "" {
					t.Fatal("normal login is managed")
				}
				return 0, nil
			}
			input := map[string]string{"normal": "0\n", "persist": "1\ny\n", "invalid": "9\n", "eof": ""}[kind]
			selectionInput(t, input)
			status, err := Run(nil, "")
			if kind == "invalid" || kind == "eof" {
				if err == nil {
					t.Fatal("bad input accepted")
				}
			} else if status != 0 || err != nil {
				t.Fatalf("%d %v", status, err)
			}
			if persisted != (kind == "persist") {
				t.Fatal("persist consent")
			}
		})
	}
}
func TestResumeRechecksAuthenticationAndExpiry(t *testing.T) {
	for _, kind := range []string{"env", "expiry", "settings"} {
		t.Run(kind, func(t *testing.T) {
			root, _ := isolated(t)
			count := 0
			wait = func(command, env []string) (int, error) {
				count++
				writeRequest(t, env, nil)
				switch kind {
				case "env":
					t.Setenv("ANTHROPIC_BASE_URL", "https://invalid.example")
				case "expiry":
					old := getProfile
					getProfile = func(id string) (core.Profile, error) {
						p, err := old(id)
						if id == "two" {
							value := "2000-01-01T00:00:00Z"
							p.ExpiresAt = &value
						}
						return p, err
					}
				case "settings":
					if err := os.MkdirAll(filepath.Join(root, ".claude"), 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(root, ".claude", "settings.json"), []byte(`{"apiKeyHelper":"helper"}`), 0600); err != nil {
						t.Fatal(err)
					}
				}
				return 0, nil
			}
			if _, err := Run(nil, ""); err == nil {
				t.Fatal("resume not revalidated")
			}
			if count != 1 {
				t.Fatal("unsafe second launch")
			}
		})
	}
}
func TestCompleteCoreProjectScopeRequest(t *testing.T) {
	root, project := isolated(t)
	if err := core.SaveProject(root, project); err != nil {
		t.Fatal(err)
	}
	loadProject = core.LoadProject
	getProfile = core.GetProfile
	getToken = core.GetToken
	oldCwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldCwd) })
	count := 0
	wait = func(command, env []string) (int, error) {
		count++
		if count == 1 {
			for key, value := range envMap(env) {
				if strings.HasPrefix(key, "CMA_") {
					t.Setenv(key, value)
				}
			}
			if err := core.RegisterSession(core.Session{SessionID: sessionID, Cwd: root}); err != nil {
				t.Fatal(err)
			}
			if err := core.RequestSwitch("two", "project", true); err != nil {
				t.Fatal(err)
			} // Remove child-only identity from parent environment before next iteration.
			t.Setenv("CMA_MANAGED", "")
		}
		return 0, nil
	}
	status, err := Run(nil, "")
	if status != 0 || err != nil || count != 2 {
		t.Fatalf("%d %v count=%d", status, err, count)
	}
	_, persisted, err := core.LoadProject(root)
	if err != nil || persisted.Profile == nil || *persisted.Profile != "two" {
		t.Fatal("core project scope not persisted", err)
	}
}
