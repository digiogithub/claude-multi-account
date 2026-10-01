//go:build linux

package launcher

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestMemoryOverlayPreservesLargeJSONNumbers(t *testing.T) {
	root, project := isolated(t)
	project.Memory.Mode = "per-profile"
	runDir := t.TempDir()
	args, err := memoryArgs([]string{"--settings", `{"budget":9007199254740993,"nested":{"amount":12345678901234567890}}`}, project, root, "one", runDir)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(args[1])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "9007199254740993") || !strings.Contains(string(data), "12345678901234567890") {
		t.Fatalf("settings numbers changed: %s", data)
	}
	if _, err := decodeSettings([]byte(`{} {}`)); err == nil {
		t.Fatal("trailing JSON accepted")
	}
	if truthy(json.Number("0")) || !truthy(json.Number("1")) {
		t.Fatal("numeric auth conflict semantics changed")
	}
}

func TestConcurrentRunsIsolated(t *testing.T) {
	isolated(t)
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func() {
			status, err := Run([]string{"prompt"}, "")
			if err == nil && status != 0 {
				err = syscall.EINVAL
			}
			results <- err
		}()
	}
	for i := 0; i < 8; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(os.Getenv("CMA_STATE_HOME"), "runs"))
	if err != nil || len(entries) != 8 {
		t.Fatalf("isolated concurrent state %d %v", len(entries), err)
	}
}
func TestResolverRejectsRenamedCompiledCopy(t *testing.T) {
	isolated(t)
	own, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(own)
	if err != nil {
		t.Fatal(err)
	}
	renamed := filepath.Join(t.TempDir(), "official-looking")
	if err := os.WriteFile(renamed, data, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CMA_CLAUDE_BINARY", renamed)
	if _, err := ResolveBinary(); err == nil {
		t.Fatal("renamed compiled wrapper accepted")
	}
}
func TestSettingsRejectsFIFOWithoutBlocking(t *testing.T) {
	isolated(t)
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readSettings(path); err == nil {
		t.Fatal("FIFO accepted")
	}
}
