package plugincheck

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Anchor to this source file, not a package test working directory or HOME.
func repository(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test source")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

type hook struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
}
type hookGroup struct {
	Hooks []hook `json:"hooks"`
}
type hookConfig map[string][]hookGroup

type plugin struct {
	Name   string     `json:"name"`
	Source string     `json:"source"`
	Strict bool       `json:"strict"`
	Skills []string   `json:"skills"`
	Hooks  hookConfig `json:"hooks"`
}

func decode(t *testing.T, path string, value any) {
	t.Helper()
	if err := json.Unmarshal([]byte(read(t, path)), value); err != nil {
		t.Fatal(err)
	}
}

func marketplace(t *testing.T) plugin {
	t.Helper()
	var market struct {
		Name    string   `json:"name"`
		Plugins []plugin `json:"plugins"`
	}
	decode(t, filepath.Join(repository(t), ".claude-plugin", "marketplace.json"), &market)
	if market.Name != "claude-multi-account-local" || len(market.Plugins) != 1 {
		t.Fatalf("unexpected marketplace: %+v", market)
	}
	return market.Plugins[0]
}

func sessionHook(t *testing.T, config hookConfig) hook {
	t.Helper()
	groups := config["SessionStart"]
	if len(config) != 1 || len(groups) != 1 || len(groups[0].Hooks) != 1 {
		t.Fatal("expected one SessionStart hook")
	}
	h := groups[0].Hooks[0]
	if h.Type != "command" || h.Timeout != 10 {
		t.Fatalf("unexpected hook: %+v", h)
	}
	return h
}

func localHooks(t *testing.T) hookConfig {
	t.Helper()
	var config struct {
		Hooks hookConfig `json:"hooks"`
	}
	decode(t, filepath.Join(repository(t), "plugin", "hooks", "hooks.json"), &config)
	return config.Hooks
}

func TestManifestsPackageWholeSource(t *testing.T) {
	root := repository(t)
	var manifest plugin
	decode(t, filepath.Join(root, "plugin", ".claude-plugin", "plugin.json"), &manifest)
	entry := marketplace(t)
	if manifest.Name != "accounts" || entry.Name != "accounts" || entry.Source != "./" || entry.Strict {
		t.Fatalf("unexpected manifests: %+v %+v", manifest, entry)
	}
	if len(entry.Skills) != 1 || entry.Skills[0] != "./plugin/skills" {
		t.Fatalf("unexpected skill roots: %v", entry.Skills)
	}
	for _, path := range []string{"go.mod", "Makefile", "cmd/cma/main.go"} {
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{".claude-plugin/plugin.json", "plugin/internal", "plugin/cma", "plugin/bin/claude"} {
		if _, err := os.Stat(filepath.Join(root, path)); !os.IsNotExist(err) {
			t.Fatalf("must not duplicate runtime or shadow Claude: %s", path)
		}
	}
	if got := sessionHook(t, localHooks(t)).Command; got != `"${CLAUDE_PLUGIN_ROOT}/bin/cma" internal session-start` {
		t.Fatal(got)
	}
	if got := sessionHook(t, entry.Hooks).Command; got != `"${CLAUDE_PLUGIN_ROOT}/plugin/bin/cma" internal session-start` {
		t.Fatal(got)
	}
	if !strings.Contains(read(t, filepath.Join(root, ".gitignore")), "dist/\n") {
		t.Fatal("compiled binary must not be versioned")
	}
}

func TestSixManualSkillsAndSecretSafeAdd(t *testing.T) {
	root := filepath.Join(repository(t), "plugin", "skills")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"add": true, "list": true, "memory": true, "project": true, "status": true, "use": true}
	if len(entries) != len(want) {
		t.Fatal("expected six skills")
	}
	for _, entry := range entries {
		if !want[entry.Name()] || !entry.IsDir() {
			t.Fatal(entry.Name())
		}
		text := read(t, filepath.Join(root, entry.Name(), "SKILL.md"))
		parts := strings.SplitN(text, "---", 3)
		if len(parts) != 3 || !strings.Contains(parts[1], "name: "+entry.Name()+"\n") ||
			!strings.Contains(parts[1], "disable-model-invocation: true") || strings.Contains(parts[1], "$ARGUMENTS") {
			t.Fatalf("invalid manual skill: %s", entry.Name())
		}
	}
	add := read(t, filepath.Join(root, "add", "SKILL.md"))
	for _, required := range []string{"claude setup-token", "entrada oculta", "make build", "make install", "CMA_BINARY", "No uses `--token-stdin`", "no leas `.credentials.json`"} {
		if !strings.Contains(add, required) {
			t.Fatalf("add missing safety/bootstrap guidance: %s", required)
		}
	}
	if strings.Contains(add, "!`") || strings.Contains(strings.SplitN(add, "---", 3)[1], "allowed-tools:") {
		t.Fatal("add must guide humans, not execute imports or installation")
	}
	for name, command := range map[string]string{"status": "status --json", "list": "profiles list --json"} {
		if !strings.Contains(read(t, filepath.Join(root, name, "SKILL.md")), "!`\"${CLAUDE_SKILL_DIR}/../../bin/cma\" "+command+"`") {
			t.Fatal("skill bridge path mismatch: " + name)
		}
	}
}

func fixture(t *testing.T) (string, []string) {
	t.Helper()
	home := t.TempDir()
	return home, []string{"PATH=/usr/bin:/bin", "HOME=" + home, "CMA_CONFIG_HOME=" + filepath.Join(home, "config"), "CMA_STATE_HOME=" + filepath.Join(home, "state")}
}

func writeExecutable(t *testing.T, path, source string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(source), 0700); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, cwd string, env []string, input string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir, cmd.Env, cmd.Stdin = cwd, env, strings.NewReader(input)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err == nil {
		return stdout.String(), stderr.String(), 0
	}
	if exit, ok := err.(*exec.ExitError); ok {
		return stdout.String(), stderr.String(), exit.ExitCode()
	}
	t.Fatal(err)
	return "", "", -1
}

const fake = "#!/bin/sh\nprintf '%s\\n' \"$@\"\nwhile IFS= read -r line; do printf '%s\\n' \"$line\"; done\nexit 7\n"

func TestHooksForwardStdinArgumentsAndExitToStableBinary(t *testing.T) {
	home, env := fixture(t)
	binary := filepath.Join(home, "stable runtime with spaces", "cma")
	writeExecutable(t, binary, fake)
	payload := "{\"session_id\":\"exact-session\",\"cwd\":\"/example\"}\n"
	for _, layout := range []struct {
		root   string
		config hookConfig
	}{{filepath.Join(repository(t), "plugin"), localHooks(t)}, {repository(t), marketplace(t).Hooks}} {
		command := sessionHook(t, layout.config).Command
		stdout, stderr, code := run(t, home, append(env, "CMA_BINARY="+binary, "CLAUDE_PLUGIN_ROOT="+layout.root), payload, "/bin/sh", "-c", command)
		if code != 7 || stdout != "internal\nsession-start\n"+payload || stderr != "" {
			t.Fatalf("hook forwarding: code=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
	}
	stdout, stderr, code := run(t, home, append(env, "CMA_BINARY="+binary), "payload\n", filepath.Join(repository(t), "plugin", "bin", "cma"), "one argument", "literal;$(false)")
	if code != 7 || stdout != "one argument\nliteral;$(false)\npayload\n" || stderr != "" {
		t.Fatalf("argument preservation: %d %q %q", code, stdout, stderr)
	}
}

func TestOverrideRequiresAbsoluteExecutableNonBridge(t *testing.T) {
	home, env := fixture(t)
	bridge := filepath.Join(repository(t), "plugin", "bin", "cma")
	alias := filepath.Join(home, "bridge alias")
	if err := os.Symlink(bridge, alias); err != nil {
		t.Fatal(err)
	}
	nonExecutable := filepath.Join(home, "not executable")
	if err := os.WriteFile(nonExecutable, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"", "relative/cma", filepath.Join(home, "missing"), home, nonExecutable, bridge, alias} {
		stdout, stderr, code := run(t, home, append(env, "CMA_BINARY="+value), "", bridge, "status")
		if code != 2 || stdout != "" || !strings.Contains(stderr, "accounts:") {
			t.Fatalf("invalid override %q: %d %q %q", value, code, stdout, stderr)
		}
	}
}

func TestRootPackagedBridgeAndMissingBinaryNeverBuild(t *testing.T) {
	home, env := fixture(t)
	packaged := filepath.Join(home, "marketplace cache with spaces")
	bridge := filepath.Join(packaged, "plugin", "bin", "cma")
	writeExecutable(t, bridge, read(t, filepath.Join(repository(t), "plugin", "bin", "cma")))
	// Trap any attempted implicit compiler/install command through PATH.
	trapDir := filepath.Join(home, "trap")
	for _, name := range []string{"go", "make", "cma"} {
		writeExecutable(t, filepath.Join(trapDir, name), "#!/bin/sh\nprintf forbidden > \"$HOME/implicit-build\"\nexit 99\n")
	}
	env[0] = "PATH=" + trapDir + ":/usr/bin:/bin"
	stdout, stderr, code := run(t, home, env, "", bridge, "status")
	if code != 2 || stdout != "" || !strings.Contains(stderr, "runtime is missing") || !strings.Contains(stderr, "make build") || !strings.Contains(stderr, "make install") {
		t.Fatalf("bootstrap failure: %d %q %q", code, stdout, stderr)
	}
	for _, path := range []string{"implicit-build", ".local", "config", "state"} {
		if _, err := os.Stat(filepath.Join(home, path)); !os.IsNotExist(err) {
			t.Fatalf("bridge must not build/install/access state: %s", path)
		}
	}
	writeExecutable(t, filepath.Join(packaged, "dist", "cma"), fake)
	// CWD and PATH cannot redirect canonical runtime resolution.
	writeExecutable(t, filepath.Join(home, "dist", "cma"), "#!/bin/sh\nexit 99\n")
	stdout, stderr, code = run(t, home, env, "stdin\n", bridge, "profiles", "list")
	if code != 7 || stdout != "profiles\nlist\nstdin\n" || stderr != "" {
		t.Fatalf("packaged runtime: %d %q %q", code, stdout, stderr)
	}
}

func TestRepositoryShimsUseOneBinary(t *testing.T) {
	home, env := fixture(t)
	root := filepath.Join(home, "checkout with spaces")
	for _, name := range []string{"cma", "claude-cma"} {
		shim := filepath.Join(root, "bin", name)
		writeExecutable(t, shim, read(t, filepath.Join(repository(t), "bin", name)))
		_, stderr, code := run(t, home, env, "", shim)
		if code != 2 || !strings.Contains(stderr, "make build") {
			t.Fatalf("missing compiled runtime: %d %q", code, stderr)
		}
	}
	writeExecutable(t, filepath.Join(root, "dist", "cma"), fake)
	for name, prefix := range map[string]string{"cma": "", "claude-cma": "launch\n--\n"} {
		stdout, stderr, code := run(t, home, env, "stdin\n", filepath.Join(root, "bin", name), "--plugin-dir", "path with spaces")
		if code != 7 || stdout != prefix+"--plugin-dir\npath with spaces\nstdin\n" || stderr != "" {
			t.Fatalf("shim %s: %d %q %q", name, code, stdout, stderr)
		}
	}
}

func TestShellSyntaxAndLinuxBuildGuard(t *testing.T) {
	root := repository(t)
	home, env := fixture(t)
	for _, path := range []string{"bin/cma", "bin/claude-cma", "plugin/bin/cma"} {
		_, stderr, code := run(t, home, env, "", "/bin/sh", "-n", filepath.Join(root, path))
		if code != 0 {
			t.Fatal(stderr)
		}
	}
	if runtime.GOOS != "linux" {
		t.Skip("build guard test requires Linux host")
	}
	makePath, err := exec.LookPath("make")
	if err != nil {
		t.Skip("make not available")
	}
	goPath, err := exec.LookPath("go")
	if err != nil {
		t.Skip("Go executable not available")
	}
	env[0] += string(os.PathListSeparator) + filepath.Dir(goPath)
	_, stderr, code := run(t, root, append(env, "GOOS=darwin"), "", makePath, "build")
	if code == 0 || !strings.Contains(stderr, "GOOS must be linux") {
		t.Fatalf("must reject non-Linux target before compiling: %d %q", code, stderr)
	}
}
