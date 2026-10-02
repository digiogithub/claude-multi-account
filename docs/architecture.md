# Go Linux Architecture

## Components

- `cmd/cma/main.go`: single executable, delegates to `internal/cli.Main`.
- `internal/core`: profiles, Secret Service/file, config-dir accounts and shared
  links (`accounts_linux.go`), project, validation, and state.
- `internal/cli`: CLI contract and internal `session-start`/`session-info`.
- `internal/launcher`: supervisor for official Claude, selection, conflicts,
  signals/stdio, and exact handoff after normal exit.
- `internal/install`: copies the compiled executable to a stable destination and creates
  the POSIX `claude-cma` shim; shell function is explicit opt-in only.
- `bin/cma`, `bin/claude-cma`: POSIX checkout shims for `dist/cma`;
  the latter invokes `launch --`. Neither replaces official Claude.
- `plugin/skills`: six manual-only skills; redacted status/list, use with
  consent, project/memory without secrets, and add with human instructions only.
- `plugin/bin/cma`: execution bridge, with no implicit compilation/installation.
  SessionStart invokes `cma internal session-start` with JSON stdin.

Linux ONLY, Go >= 1.23, standard library with no dependencies. One binary handles
administration and launch; no additional runtime or duplicate code under plugin.

## Distribution: a single source for the runtime

The `.claude-plugin/marketplace.json` entry uses **source `./`**, not `./plugin`.
The marketplace packages the full root with Go source, Makefile, and plugin; it declares
skills at `./plugin/skills` and an inline hook that runs
`${CLAUDE_PLUGIN_ROOT}/plugin/bin/cma`. It uses the entry as a manifest with
`strict: false`, with no `.claude-plugin/plugin.json` at the root.

In development, `plugin/.claude-plugin/plugin.json` loads the conventional layout:
skills and `plugin/hooks/hooks.json`, whose command is `${CLAUDE_PLUGIN_ROOT}/bin/cma`.
This nested manifest is not the root marketplace manifest. Skills use
`${CLAUDE_SKILL_DIR}/../../bin/cma`: this works in both layouts, without absolute paths
to the development checkout or ephemeral cache paths.

The bridge resolves the executable in this order:

1. If `CMA_BINARY` is defined: requires an absolute path to a stable executable,
   never the bridge itself (including references to the same file).
   It does not fall back to the local binary if the override is invalid.
2. Without an override: `dist/cma` two levels above `plugin/bin/cma`, in both the full
   checkout and marketplace copy. It must exist and be executable.
3. If missing: a clear error asks for `make build` at the full root or explicit
   `make install` from the checkout and a stable `CMA_BINARY`; it does not install/compile/download.

The bridge preserves arguments, stdin, and exit code through `exec`.
It does not look for `cma` on PATH or resolve a runtime from CWD.

**Installing the plugin does not compile or install the CLI.** `dist/` is ignored by Git:
the package may contain source without a binary. The user must, in a terminal,
run `make build` at the full root of the plugin copy, or use a stable
installation with `CMA_BINARY`. Updating the cache may require compiling
the new copy; the stable installation avoids depending on a cache path.
**An isolated copy of plugin/ is not self-sufficient**: it requires `CMA_BINARY`.
Do not change the source or duplicate the runtime under plugin.

`make build` verifies the Linux host and `go env GOOS`, compiles with `CGO_ENABLED=0 GOOS=linux`
to `dist/cma`, and checks the resulting platform metadata.
`make install` requests an explicit build and runs `./dist/cma install`.
By default, the installer copies **the current executable** to
`~/.local/share/claude-multi-account/bin/cma`; it creates `bin/claude-cma`
as a shim to `cma launch --`. Another destination accepts `install --target`.
It does not copy source or compile at runtime. `--shell` requests a `claude` function
with a stable path that calls `cma launch -- "$@"`; by default, it does not change the shell.
Administrative arguments are passed to official Claude without managing tokens.

## Contracts and state

`core.ConfigHome()` uses CMA_CONFIG_HOME or XDG_CONFIG_HOME/claude-multi-account;
`core.StateHome()` uses CMA_STATE_HOME or XDG_STATE_HOME/claude-multi-account.
XDG defaults are under home. Overrides are user-trusted paths.
Version 1 registry with ID→metadata profiles and secret_ref. Project config:
`version`, `profile`, optional `allowedProfiles`, `memory.mode`, and
`switching.confirmContextTransfer`. No secrets or executable code.

The JSON format preserves compatibility with the previous Python records and
run/session/switch. Only Linux `secret-service`, `file`, and `config-dir` backends are
accepted; macOS/`keychain` profiles are unsupported, with no conversion or access to
their secrets.
There is no bulk registry migration. Strict validation of
versions, types, fields, and permissions is retained; do not confuse defaults with corrupt data.

## Profile modes

Two modes; **config-dir is the default**, token is opt-in (`profiles add --mode token`).
Reason: `setup-token` tokens are inference-only, so token-mode sessions start without
remote control and other login-dependent features; config-dir has a full login.

`core.AccountsHome()` is `CMA_ACCOUNTS_HOME` or `~/.claude-accounts`;
`core.SharedHome()` is `CMA_SHARED_HOME` or `AccountsHome()/shared`;
`core.ProfileDir(id)` is `AccountsHome()/profiles/<id>`. All are 0700 with no symlinked
ancestors (`SecureDirectory`); the profile directory is that account's `CLAUDE_CONFIG_DIR`.

A config-dir profile is an ordinary version 1 registry record with
`secret_backend: "config-dir"`, a random unused 32-hex `secret_ref`, and
`expires_at: null`; its directory is derived from the ID. There are no new JSON fields,
and existing token records are untouched. Old binaries reject the new backend value.
`GetToken` and `RenewProfile` fail for these profiles, and `RemoveProfile` removes only
the registry entry; the directory is deleted only by `profiles remove --purge`
(`PurgeProfileDir`, which refuses anything but exactly `ProfileDir(id)` or paths with
symlinked ancestors).

Shared entries (fixed list, `core.SharedEntries`) live in `SharedHome()` as real files
and directories and are linked by absolute symlink into each profile directory:
`projects`, `skills`, `agents`, `commands`, `plugins`, `todos`, `plans`, `file-history`
(directories, created 0700 if missing), `settings.json` (`{}` 0600), `CLAUDE.md` and
`history.jsonl` (empty 0600), and `keybindings.json` (optional: linked only if it exists
in shared). `.credentials.json` and `.claude.json` are never shared, linked, or opened;
login state is `Lstat` only.

`EnsureProfileDir` is idempotent and never deletes or overwrites: an absent entry is
linked, a link already resolving to the shared target is present, anything else (a
regular file/directory or a link elsewhere) is a reported conflict, and an optional
entry missing in shared is skipped. The launcher calls it on every config-dir launch
(self-healing) and prints conflicts as warnings on stderr. `ImportShared` copies from a
source (CLI default `~/.claude`) only where the shared entry is missing or an empty
placeholder. A top-level symlinked source entry is followed (EvalSymlinks) only when it
resolves outside accounts and shared homes and its kind matches; nested symlinks are
recreated as symlinks, never followed. Directories are copied into a temporary sibling
(`<target>.cma-import-<random>`) and renamed only on success. It refuses a source inside accounts home, never
touching the source and never copying the two credential files.

## Launcher by mode

The wrapper discovers the root, reads config, validates the profile/conflicts, and
branches on the effective profile:

- config-dir: `EnsureProfileDir`, then Claude runs with `CLAUDE_CONFIG_DIR=<dir>` plus
  the `CMA_*` variables. The token path is never called and `CLAUDE_CODE_OAUTH_TOKEN`
  is not set. User settings are read from the profile directory.
- token: obtains the token and launches Claude with a scoped `CLAUDE_CODE_OAUTH_TOKEN`
  and the ambient `CLAUDE_CONFIG_DIR`; user settings are read from there or `~/.claude`.

Environment and settings auth/routing variables and `apiKeyHelper` conflict in both
modes (they would override the login); `forceLoginMethod`/`forceLoginOrgUUID` conflict
only in token mode. `--bare` is rejected in both modes; `--bg`/`--background`/`--cloud`
are rejected only in token mode. Management commands (`auth`, `setup-token`, `plugin`,
`update`, `install`, `doctor`, `--help`, `--version`) with an explicit `--profile`
require the profile to resolve (ID, existence, expiry; the project's `allowedProfiles` is
not applied) and, if it is config-dir, run with its `CLAUDE_CONFIG_DIR`; without
`--profile` the project profile is tried best-effort and a project load error, no
profile, an unknown profile or a token profile falls back to untouched pass-through,
while a config-dir project profile whose directory cannot be prepared is an error.
Management commands are handled before the nested-launch check; with `CMA_MANAGED=1` and
no `--profile` they pass through with the inherited environment. Nested-launch refusal
applies only to managed conversations. `--bare` is rejected before any interactive
selection. The switch/resume loop recomputes the mode for
each destination, so config-dir, token, and mixed switches work; the shared `projects/`
link lets `--resume SESSION_ID` resolve in the destination directory.

For per-profile memory (both modes), it adds a temporary 0600 `--settings` file with
`autoMemoryDirectory` stable per project+profile and outside the repo.
It does not replace existing settings or migrate memory.

For each run: `StateHome()/runs/<launch_id>` mode 0700, random 32-hex nonce.
Non-secret environment: CMA_LAUNCH_ID, CMA_ACTIVE_PROFILE, CMA_PROJECT_ROOT,
CMA_MANAGED=1, and CMA_RUN_DIR. JSON mode 0600, atomic writes:

- `run.json`: version, launch_id, profile, project_root.
- `session.json`: real session_id, cwd, launch_id, project_root. The hook receives
  `{session_id,cwd,...}` on stdin; it does not extract IDs from transcripts/credentials.
- `switch.json`: destination profile, scope session|project, session_id,
  confirmed=true, launch_id, and project_root. Validation is bound to the run.

Without the wrapper, the hook warns. In managed mode it validates consistency and rejects mismatches.
`switch` checks consent/profile/project/ID. After normal exit, the wrapper
processes a valid request and relaunches the exact `--resume SESSION_ID`, preserving
options without duplicating resume/continue/session-id. Project scope persists
the assignment; session scope does not. Errors do not auto-restart; do not kill from the plugin or
open a nested TUI. No synchronization between sessions.

## Tests and evidence limits

`go test ./...`, `go test -race ./...`, `go vet ./...` use temporary directories,
fake Claude and secrets, and no network. `internal/plugincheck` verifies manifests,
six manual-only skills, names, absence of a model-based token flow,
isolated bridge, arguments/stdin/exit code, paths with spaces, and
root packaging. Linux compilation and `sh -n` complement the tests.
`claude plugin validate` validates schemas locally; it does not install or authenticate.

Cross-account auth/resumption and real organizational resources require an
authorized manual test; they have not been verified with the fake tests. Tests must
set `CMA_ACCOUNTS_HOME` to a temporary directory. Also unverified against real Claude:
whether it keeps a symlinked `settings.json`/`history.jsonl` as a link when rewriting
it (otherwise `EnsureProfileDir` reports a conflict and that profile diverges for the
file); that `plugins/` absolute install paths, which point under the installing
profile's directory, survive purging that profile; and that `claude auth login` honours
`CLAUDE_CONFIG_DIR` and remote control works with real accounts. Static validation
does not guarantee compatibility with every loader version: review sources before testing
marketplace installation in the user's environment. History/CLAUDE.md/code
remain shared (in config-dir mode also the shared entries above); there is no complete isolation or claude.ai synchronization.
