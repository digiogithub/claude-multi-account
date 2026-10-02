# Plan: config-dir mode (new default)

## Problem

Token mode launches Claude with `CLAUDE_CODE_OAUTH_TOKEN` from `claude setup-token`.
Those long-lived tokens are inference-only: Claude starts without remote control and
other features that require a full interactive login. This is an unwanted effect.

## Goal

A second profile mode, **config-dir**, which becomes the default. Each profile owns a
separate Claude configuration directory, selected with `CLAUDE_CONFIG_DIR`, where the
user performs the normal full login. No token is stored, read, or injected by cma.
Project memory, history, and common configuration are shared across profiles through
symbolic links into one `shared` directory. Token mode stays available, opt-in.

The existing selection mechanism is unchanged: `.claude/accounts.local.json` in the
project names the profile ID; the `claude` shell function / `claude-cma` shim call
`cma launch --`.

## Layout

`core.AccountsHome()`: `CMA_ACCOUNTS_HOME` if set, else `~/.claude-accounts`.
`core.SharedHome()`: `CMA_SHARED_HOME` if set, else `AccountsHome()/shared`.
`core.ProfileDir(id)`: `AccountsHome()/profiles/<id>`.

```
~/.claude-accounts/            0700
  shared/                      0700  real files and directories
    projects/ skills/ agents/ commands/ plugins/ todos/ plans/ file-history/
    settings.json CLAUDE.md history.jsonl [keybindings.json]
  profiles/                    0700
    <id>/                      0700  = CLAUDE_CONFIG_DIR for that account
      .credentials.json        written by official Claude only; cma never opens it
      .claude.json             per account, written by official Claude
      projects -> <shared>/projects      (absolute symlinks)
      settings.json -> <shared>/settings.json
      ...
```

Shared entries (fixed list, `core.SharedEntries`):

| Name | Kind | When missing in shared |
| --- | --- | --- |
| `projects`, `skills`, `agents`, `commands`, `plugins`, `todos`, `plans`, `file-history` | dir | create 0700 |
| `settings.json` | file | create `{}\n` 0600 |
| `CLAUDE.md`, `history.jsonl` | file | create empty 0600 |
| `keybindings.json` | file | optional: link only if it exists in shared |

Never shared, never linked, never opened: `.credentials.json`, `.claude.json`.

## Registry: format version 1 unchanged

No new JSON fields. A config-dir profile is a v1 `Profile` record with
`secret_backend: "config-dir"`, a random unused 32-hex `secret_ref`, and
`expires_at: null`. Its directory is derived from the ID. Existing token records
(`secret-service`, `file`) keep working untouched. Old binaries reject the new
backend value explicitly rather than misusing it.

## Core API (`internal/core/accounts_linux.go`, new)

```go
const ConfigDirBackend = "config-dir"
func AccountsHome() string
func SharedHome() string
func ProfileDir(id string) (string, error)          // validates ID
func IsConfigDir(p Profile) bool                     // p.SecretBackend == ConfigDirBackend
type SharedEntry struct{ Name string; Dir, Optional bool }
var SharedEntries []SharedEntry
type LinkReport struct{ Linked, Present, Conflicts, Skipped []string }
func EnsureProfileDir(id string) (string, LinkReport, error)
func ImportShared(from string) (copied, skipped []string, err error)
func LoginState(id string) string                    // "present" | "absent", Lstat only
func AddConfigDirProfile(p Profile) (Profile, LinkReport, error)
func PurgeProfileDir(id string) error
```

`EnsureProfileDir`: `SecureDirectory` on accounts home, `profiles`, the profile
directory, and shared home; create missing shared targets per the table; then for
each entry in the profile directory:

- absent: create an absolute symlink to the shared target (`Linked`);
- symlink already resolving to the shared target: `Present`;
- anything else (regular file/dir, or a link elsewhere): leave it alone, `Conflicts`;
- optional entry absent in shared: `Skipped`.

It is idempotent and never deletes or overwrites. The launcher calls it on every
config-dir launch (self-healing) and prints conflicts as warnings on stderr.

`ImportShared(from)`: copies entries from `from` (CLI default `~/.claude`) into shared
only where the shared entry is missing or is an empty directory / freshly created
placeholder (`{}` settings, empty file). A top-level source entry that is a symlink is
resolved with `EvalSymlinks` and copied only if it resolves outside accounts/shared homes
and its kind matches (else skipped). Recursive copy never follows nested symlinks (they
are recreated as symlinks), preserving modes masked to owner; directories are copied into
a sibling `<target>.cma-import-<random>` and renamed over the empty/missing target only on
success (removed on failure); refuses when
`from` resolves inside accounts home; never touches the source; never copies any
file named `.credentials.json` or `.claude.json`.

`PurgeProfileDir`: unlinks symlinks in the profile directory without following them,
then removes the remaining per-account files and the directory. Refuses a path that
is not exactly `ProfileDir(id)` or contains symlinked ancestors.

Changes in existing core code:

- `validateMetadata`: accept `config-dir` backend.
- `GetToken`: config-dir profile returns
  `"Profile uses a configuration directory; it has no token."`.
- `RenewProfile`: config-dir profile returns an error pointing to `cma profiles login ID`.
- `RemoveProfile`: config-dir profile removes only the registry entry (no secret
  backend call).
- `secretSet`/`secretRemove`: never reached for config-dir.
- `Status`: add `mode` (`"config-dir"` | `"token"` | nil), `config_dir`, `login`
  for the effective profile.

## Launcher

For the effective profile `p`:

- `core.IsConfigDir(p)`: `EnsureProfileDir`, child env `CLAUDE_CONFIG_DIR=<dir>` plus
  the existing `CMA_*` variables; `getToken` is **never** called and
  `CLAUDE_CODE_OAUTH_TOKEN` is not set.
- otherwise: current token behaviour, unchanged.

`settings(args, root, configDir)`: user settings are read from the profile directory
in config-dir mode, from ambient `CLAUDE_CONFIG_DIR`/`~/.claude` in token mode.

`authConflicts(values, tokenMode)`: environment and settings auth/routing variables
and `apiKeyHelper` conflict in both modes (they would override the directory login).
`forceLoginMethod`/`forceLoginOrgUUID` conflict only in token mode.

Unsupported options: `--bare` rejected in both modes, before any interactive selection. `--bg`/`--background`/`--cloud`
rejected only in token mode; config-dir mode has a full login and forwards them.

Management commands (`auth`, `setup-token`, `plugin`, `update`, `install`, `doctor`,
`--help`, `--version`): with an explicit `--profile`, the profile must resolve; if it
is config-dir the command runs with its `CLAUDE_CONFIG_DIR`. The project's
`allowedProfiles` is not applied to it (login is account administration); ID, existence
and expiry are still validated. Without `--profile`, the project profile is tried
best-effort; a project load error, no profile, an unknown profile or a token profile
falls back to the current untouched pass-through, but a config-dir profile whose
directory cannot be prepared is an error. Management commands are handled before the
nested-launch check; with `CMA_MANAGED=1` and no `--profile` they pass through with the
inherited environment. Nested-launch refusal applies only to managed conversations.

Switch/resume loop: unchanged protocol. Each iteration recomputes mode for the
destination profile, so config-dir to config-dir, and mixed, switches work. Shared
`projects/` makes `--resume SESSION_ID` resolve in the destination directory.

Per-profile memory overlay (`autoMemoryDirectory`) keeps working in both modes.

A new seam `ensureProfileDir = core.EnsureProfileDir` keeps tests isolated.

## CLI

- `cma profiles add ID --label L [--mode {config-dir,token}] ...`; default
  `config-dir`. `--backend`, `--expires-at`, `--token-stdin` are valid only with
  `--mode token` (else argument error). Config-dir add never prompts for a token and
  prints the next step: `cma profiles login ID`.
- `cma profiles login ID`: config-dir only; runs official Claude `auth login` with
  that profile's `CLAUDE_CONFIG_DIR` via `launcher.Run([]string{"auth","login"}, id)`.
- `cma profiles remove ID [--purge]`: `--purge` also deletes a config-dir profile
  directory (credentials included) before the registry entry is removed, so a failed
  purge leaves the profile registered; without it the directory is kept and reported.
- `cma profiles list`: text adds `mode:` and, for config-dir, `login: present|absent`.
  `--json` output stays the v1 record array (mode is visible as `secret_backend`).
- `cma shared import [--from DIR]`, `cma shared sync [--json]` (re-run `EnsureProfileDir` for
  every config-dir profile and report), `cma shared path`.
- `cma status`: text adds `Mode:`, `Config directory:`, `Login:`.
- `help.go` updated for every new usage.

## Security

- cma never opens `.credentials.json`; login state uses `Lstat` only.
- Accounts home, shared, `profiles`, and each profile directory: owner-only 0700,
  no symlinked ancestors (`SecureDirectory`).
- Symlinks are created only inside a profile directory and only to fixed shared
  names; nothing is overwritten.
- Shared `settings.json` applies to all accounts: an `apiKeyHelper`/auth `env` entry
  there is detected as a conflict before launch.
- Sharing `projects/` means conversation history and memory are readable under every
  account; switching still requires `--confirm-context-transfer`.

## Unverified against real Claude (manual test required)

- Whether official Claude keeps a symlinked `settings.json`/`history.jsonl` as a
  symlink when rewriting it. If it replaces the link with a regular file,
  `EnsureProfileDir` reports a conflict and that profile diverges for that file.
- `plugins/` stores absolute install paths under the writing profile's directory;
  they resolve through that profile's symlink. Purging that profile can break them.
- `claude auth login` honouring `CLAUDE_CONFIG_DIR`, and remote control and
  cross-account `--resume` working with real accounts.

## Work packages

1. **core**: `accounts_linux.go`, profile/session changes, `accounts_linux_test.go`.
2. **launcher**: mode branch, settings/conflict parameters, management, tests.
3. **cli**: new commands/flags, help, tests.
4. **docs/skills**: README, `docs/*.md`, AGENTS.md, skills, `plugincheck` expectations.
5. **verification**: full command list from AGENTS.md, independent review.

Order: 1 and 4 in parallel; then 2 and 3 in parallel; then 5.
