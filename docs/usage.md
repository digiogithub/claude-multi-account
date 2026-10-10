# Using Claude Multi Account (v1)

## Requirements and scope

Local terminal ONLY on Linux (WSL with a Linux environment); Go >= 1.23 to compile;
official Claude Code installed. Runs as a single binary, with no additional runtime.
No integration with VSCode, Desktop, or background processes. Local profiles
reusable by the user; no synchronization of accounts/state with claude.ai.
It does not discover organizations or check that email/organization matches the
token: always `verification: unverified`. Do not use this tool to bypass
organization policies.

### Compatibility with existing data

The version 1 JSON format for profiles, configuration, and run/session/switch protocols
from the previous Python implementation is preserved, with no bulk migration
or changes to secret references. Config-dir profiles are ordinary v1 records with
`secret_backend: "config-dir"`; existing token records keep working untouched. Only
Linux `secret-service`, `file`, and `config-dir` profiles are supported. Existing macOS/`keychain` profiles are **unsupported**:
they are neither converted nor are their secrets read. To use them on Linux, import a new token
through the official terminal flow and create a Linux profile; preserve the prior data
until you explicitly decide how to remove it.

## Explicit installation

In the user's terminal, from the root of the full checkout on Linux:

```sh
make build
make install
```

`make build` checks the Linux host and `go env GOOS=linux`, then compiles `dist/cma`.
`make install` runs `./dist/cma install` after explicit compilation.
The installer copies the current executable to the stable path
`~/.local/share/claude-multi-account/bin/cma` and creates the POSIX `claude-cma` shim.
It does not copy another runtime or download Go/Claude; it does not log in. Add the reported `bin`
path to PATH by your own choice, or use its executables by absolute path.
For a custom destination, after `make build`:

```sh
./dist/cma install --target /ruta/absoluta/cma-runtime
```

The same binary handles the CLI and supervisor with `cma launch -- ...`; `claude-cma`
is only a shim to that operation. The checkout shims `./bin/cma` and
`./bin/claude-cma` point to `dist/cma`; they do not compile automatically.

`claude-cma` is the opt-in wrapper that does not replace the official binary. The option
`cma install --shell` requests explicit integration through a `claude` function;
it is not required to use the wrapper. Review the installer report and
supported shell. The real `claude` remains available and is not replaced on disk.
If you need to pin it, set `CMA_CLAUDE_BINARY=/ruta/absoluta/claude` when launching.
The integration preserves existing aliases. If a `claude` alias exists,
it may take precedence over the function: check with `type claude` and remove it
manually if you want to use the wrapper, or run `claude-cma` directly.

Plugin (official CLI; does not invoke login):

```sh
claude plugin marketplace add /ruta/absoluta/claude-multi-account
claude plugin install accounts@claude-multi-account-local
```

From Claude, you can also use `/plugin marketplace add /ruta/absoluta/claude-multi-account`
and `/plugin install accounts@claude-multi-account-local` (manager interface).
Installing the plugin **does not compile or install cma on PATH, change the shell, or
import credentials**. Source `./` includes the Go source and Makefile from the full
root. `dist/` is ignored by Git and the compiled binary may not be included in
the package. The bridge never compiles from SessionStart or a skill.

Recommended option: compile and install from a full checkout and, before
launching Claude, set this in the terminal:

```sh
export CMA_BINARY="$HOME/.local/share/claude-multi-account/bin/cma"
```

It must be an absolute path to the stable executable, not the bridge. This also works
with the local plugin `claude --plugin-dir ./plugin` and an isolated copy of `plugin/`.
Do not rely on a fixed cache path: updates may change it.

Alternative: at the **full root of the marketplace copy** (parent of `plugin/`),
run `make build` explicitly to create `dist/cma` there. Without `CMA_BINARY`,
the bridge uses only that binary. To install it stably after compiling,
the user can run `./dist/cma install` in a terminal; this does not happen on load.

To test from the checkout, after `make build`:

```sh
./bin/claude-cma --plugin-dir /ruta/absoluta/claude-multi-account/plugin
```

Copying only `plugin/` does not include the runtime: it requires a stable installation and
`CMA_BINARY`. Do not change the marketplace source to `./plugin` or duplicate Go code
under plugin. The current `./` source maintains a single canonical source.

## Profile modes

- **config-dir (default)**: each profile has its own Claude configuration directory,
  `~/.claude-accounts/profiles/<id>` (override the base with `CMA_ACCOUNTS_HOME`), used
  as `CLAUDE_CONFIG_DIR`. The user performs a normal full login; cma stores no token.
- **token** (`--mode token`): the previous behaviour, with a `setup-token` token injected
  as `CLAUDE_CODE_OAUTH_TOKEN`. Those tokens are inference-only: sessions start without
  remote control and other features that need a full login. config-dir mode exists to
  fix that.

## How config-dir mode works, in practice

This section describes the default mode from the user's point of view. The command
reference follows in the next sections.

**The idea.** Each profile is one Claude account with its own complete, normal login,
exactly as if that account were the only one on the machine. Because the login is a
full one, Claude starts with everything that account is entitled to, remote control
included. Everything that is not the login itself is common to all your accounts, so
changing account does not mean losing your setup or your conversations.

**What each account keeps for itself**

- Its login: which user and organization Claude runs as.
- The small amount of account state that Claude itself records next to the login.

**What all accounts have in common**

- Conversations and history, so a conversation started under one account can be
  resumed under another.
- Project memory.
- Skills, agents, commands, and plugins.
- Settings, `CLAUDE.md`, and key bindings.

A change made under one account applies to all of them: install a skill or change a
setting while working as `trabajo`, and it is there when you work as `personal`.

**First time**

1. Create a profile per account: `cma profiles add trabajo --label "Trabajo"`.
2. Log in to each one, once: `cma profiles login trabajo`. This is the ordinary Claude
   login, done in your terminal.
3. Optionally bring your current setup along: `cma shared import` copies your existing
   skills, settings, history, and memory from `~/.claude` so every profile starts with
   them. Your `~/.claude` is left exactly as it was.
4. In each project, say which account it uses: `cma project init --profile trabajo`.

**Day to day**

- Start Claude in a project with `claude-cma` (or `claude`, if you enabled the shell
  integration). It opens with the account assigned to that project. Nothing else to do.
- Different projects can use different accounts, and you can have them open at the
  same time in different terminals.
- A project without an assigned account asks which one to use, and whether to remember
  the answer.
- `cma status` tells you which account the project uses and whether it is logged in.
  `cma profiles list` shows all accounts.
- If an account's login expires or you want to log in as someone else, run
  `cma profiles login ID` again.

**Changing account in the middle of a conversation**

Ask for it with `/accounts:use personal session` (only this conversation) or
`/accounts:use personal project` (also remember it for the project), confirm that the
conversation may continue under the other account, and leave with `/exit`. Claude
reopens the same conversation under the other account.

**Good to know**

- Starting official Claude directly, without the wrapper, still uses your usual
  `~/.claude` login and setup. Profiles do not replace it.
- Because history and memory are common, anything written in a conversation is visible
  under every account. Keep that in mind before continuing work conversations under a
  personal account, or the reverse. If you want memory kept apart per account in a
  project, use `cma project memory per-profile`.
- If you see a "shared link conflict" warning at launch, one account has its own copy
  of something that should be common (for example its own settings). Claude still
  starts; that one item is simply not shared for that account until you resolve it.
  `cma shared sync` shows the situation for every account.
- Removing a profile (`cma profiles remove ID`) forgets the account but keeps its login
  on disk; add `--purge` to delete the login too. What is common to all accounts is
  never deleted.
- Existing token profiles keep working as before. To move one to the new mode, remove
  it, add it again without `--mode token`, and log in.

## Adding, logging in, and removing profiles (config-dir)

```sh
cma profiles add trabajo --label "Trabajo"
cma profiles login trabajo
```

Replace the ID and label. `add` registers the profile, creates its directory with the
shared links, and prints the next step. It never prompts for a token. `login` is
config-dir only: it runs official Claude `auth login` with that profile's
`CLAUDE_CONFIG_DIR`. Run it in a terminal, **outside the model**. The tool does not read
`.credentials.json` (login state is an `Lstat` check) or implement private OAuth. The
login determines identity; there is no separate organization selector. Two profiles can
share an email: their local IDs distinguish them.

`--email`, `--organization`, and `--organization-id` are all optional declared,
unverified metadata; supply them only if known. Omitted values are stored empty
and displayed as unknown. There is no automatic network/identity discovery:
no documented supported API for that discovery has been confirmed.

```sh
cma profiles list
cma profiles list --json
cma profiles remove trabajo
cma profiles remove trabajo --purge
```

`list` text shows `mode:` and, for config-dir profiles, `login: present|absent`; `--json`
stays the v1 record array (the mode is visible as `secret_backend`). `remove` deletes the
registry entry and, without `--purge`, keeps and reports the profile directory; `--purge`
also deletes the directory, credentials included. Removal requires an explicit user
request and leaves assignments that point to that ID invalid until corrected. `renew`
and the token options do not apply to config-dir profiles.

## Shared directory

`~/.claude-accounts/shared` (override with `CMA_SHARED_HOME`) holds the real files that
every config-dir profile links to. Each entry below is an absolute symlink created inside
the profile directory by `add`, re-checked at every launch and by `shared sync`:

| Entry | Kind | When missing in shared |
| --- | --- | --- |
| `projects`, `skills`, `agents`, `commands`, `plugins`, `todos`, `plans`, `file-history` | directory | created 0700 |
| `settings.json` | file | created as `{}` 0600 |
| `CLAUDE.md`, `history.jsonl` | file | created empty 0600 |
| `keybindings.json` | file | optional: linked only if it exists in shared |

`.credentials.json` and `.claude.json` are never shared, linked, or opened. Links are
never overwritten: an existing regular file or a link elsewhere is reported as a
conflict (warning at launch) and left alone.

```sh
cma shared import [--from DIR]
cma shared sync [--json]
cma shared path
```

`import` copies from `~/.claude` (or `DIR`) into shared, non-destructively: only where
the shared entry is missing or an empty placeholder. A top-level source entry that is a
symlink is followed only when it resolves outside the accounts and shared homes and its
kind matches; nested symlinks are recreated as symlinks, never followed. Directories are
copied into a temporary sibling and renamed on success, so a failure leaves no partial
entry. It refuses a source inside the accounts home, never modifies the source, and never
copies `.credentials.json` or `.claude.json`. `sync` re-runs the link step for every
config-dir profile and reports; `--json` prints the per-profile link reports as JSON. `path` prints the shared directory.

## Token mode (opt-in)

Run official setup-token **outside the model** for the desired account:

```sh
claude setup-token
cma profiles add trabajo --label "Trabajo" --mode token
```

Replace the ID and label, and paste the token only into cma's hidden
terminal prompt. The official setup-token command may display sensitive material: do not
share its output or paste it into chat. The token determines identity. Limitation: these
tokens are inference-only, so Claude starts without remote control and other
login-dependent features.

`--email`, `--organization`, and `--organization-id` are all optional declared,
unverified metadata; supply them only if known. Omitted values are stored empty
and displayed as unknown. There is no automatic network/token identity discovery:
no documented supported API for that discovery has been confirmed.

Token-mode options (valid only with `--mode token`; otherwise an argument error):
`--expires-at ISO` declares a known local expiration; if omitted, it is
shown as unknown (not inferred). `--backend auto|secret-service|file`.
`auto` selects Secret Service; file storage is opt-in, not a silent fallback.
Secret Service requires `secret-tool` and an available keychain session on Linux.
The file backend stores plaintext with mode 0600: assess its risks first.

```sh
cma profiles renew trabajo
```

Renewal (token mode only) also uses hidden input; removing a token profile also deletes its
secret. `--token-stdin` in add/renew is available for deliberate external automation; never use it through model tools or put tokens in arguments,
global variables, chat, JSON, command history, or logs.

## Project and memory

```sh
cma project init --profile trabajo
cma project use trabajo
cma project memory per-profile
cma status --json
```

Local config `.claude/accounts.local.json`, without secrets:

```json
{
  "version": 1,
  "profile": "trabajo",
  "allowedProfiles": ["trabajo", "personal"],
  "memory": {"mode": "per-profile"},
  "switching": {"confirmContextTransfer": true}
}
```

`allowedProfiles` is optional and limits destinations; it is not a boundary against a
local user who can edit the file. The manager attempts a local Git exclusion
or warns: check `git status` before sharing. It does not change global Git config.
From subdirectories it uses the Git root; each worktree has its own assignment. Outside
Git it looks for the nearest ancestor with config, without arbitrarily traversing home/root.
Invalid config, unknown/expired or disallowed profile:
explicit error, with no switch to another account.

Mode and login are shown by `cma status` (`Mode:`, `Config directory:`, `Login:`).
`cma project init` without a profile allows selection before launch. In an
interactive terminal, without a profile, the wrapper offers a selector, the option to save the choice in the
project, and the option to use the usual unmanaged login; it does not request tokens. Without interaction
and without an assignment, it fails. `-p`/`--print` never opens a selector, even in a terminal.
Administrative commands `auth`, `setup-token`, `plugin`, `update`, `install`
and `doctor`, as well as `--help`/`--version`, never retrieve tokens from cma. With an
explicit `--profile`, the profile must resolve and, if it is config-dir, the command
runs with that profile's `CLAUDE_CONFIG_DIR`; the current project's `allowedProfiles` is
not applied, since login is account administration. Without `--profile`, the project
profile is tried best-effort; on any failure, or for a token profile, the command is
passed through to official Claude untouched, except that a config-dir project profile
whose directory cannot be prepared is an error (the default `~/.claude` account is never
used by mistake). Management commands also work inside a managed session, where they pass
through with the session's inherited environment; only managed conversations are refused
when nested. This allows their use after enabling the `claude`
shell function. When launching managed conversations, `--bare` is rejected in both modes;
`--bg`/`--background`/`--cloud` are rejected only in token mode (config-dir mode has a
full login and forwards them; unverified with real accounts).
`project use` affects future launches; it does not change the current Claude process.

`shared` keeps existing memory. `per-profile` separates automatic memory only
through autoMemoryDirectory and a temporary overlay, in both modes; it does not migrate
or copy memory. In config-dir mode, automatic memory and history are common to all
profiles through the shared `projects/` link. History, conversation, CLAUDE.md, code,
and the rest of the configuration remain shared. Heed settings/managed memory warnings; this is not complete isolation.

## Launch and switch

```sh
claude-cma
cma launch --profile trabajo -- --resume ID_REAL_DE_SESION
```

Everything after `--` is passed to Claude. The wrapper preserves stdio, signals, and exit
code. In config-dir mode it sets `CLAUDE_CONFIG_DIR` (and never `CLAUDE_CODE_OAUTH_TOKEN`);
in token mode it injects the token. Switching works in both modes and between them; with
config-dir profiles, shared `projects/` lets `--resume SESSION_ID` resolve in the
destination directory. It does not automatically recover an account with an exhausted quota or fall back to
another identity. It detects authentication/routing conflicts without deleting policies.

`cma claude [--profile ID] [--] CLAUDE_ARGS...` forwards every argument verbatim (no `--`
needed). Configuration subcommands (`auth`, `setup-token`, `mcp`, `plugin`/`plugins`,
`update`/`upgrade`, `install`, `doctor`, `auto-mode`, `import`, `purge`, `agents`, `attach`,
`logs`, `respawn`, `rm`, `stop`/`kill`, and top-level `--help`/`--version`) run unsupervised
with the selected config-dir profile's `CLAUDE_CONFIG_DIR`, so `cma claude mcp add ...`
configures the project's profile rather than the default account. Session markers
(`CMA_MANAGED`, `CMA_RUN_DIR`, ...) are not passed to them when a profile is applied. Token
profiles keep the ambient configuration directory for these commands. Any other arguments
start a managed session exactly like `cma launch --`.

Actual namespaced skills (manual-only):

| Skill | CLI equivalent / effect |
| --- | --- |
| `/accounts:status` | `cma status --json` |
| `/accounts:list` | `cma profiles list --json` |
| `/accounts:use ID session` | `cma switch ID --scope session --confirm-context-transfer`, only after consent |
| `/accounts:use ID project` | `cma switch ID --scope project --confirm-context-transfer`, after consent; persistent |
| `/accounts:project init ID` | `cma project init --profile ID` |
| `/accounts:project use ID` | `cma project use ID` |
| `/accounts:add ID` | Instructions for `cma profiles add` + `cma profiles login` (config-dir) or `--mode token` with `claude setup-token`; terminal only |
| `/accounts:memory shared` | `cma project memory shared` |
| `/accounts:memory per-profile` | `cma project memory per-profile` |

For a managed switch, the SessionStart hook must have registered the real ID in
this run. Choose a scope and **authorize context transfer**. Then:

```sh
cma switch personal --scope session --confirm-context-transfer
```

Inside the model, request it with `/accounts:use`. Then enter `/exit`:
the wrapper detects the request after normal exit and resumes with `--resume` and the
exact registered ID. It does not kill the TUI or inject auth at runtime. An error exit
does not restart; other sessions do not change. `scope project` persists the assignment;
`scope session` does not. A cwd/project change requires detection/warning, not a
silent auth change. Without the wrapper, the hook warns, and switch is unavailable.

**Exhausted quota:** a skill may need the model and may not run. Use the
CLI from a terminal that inherits the environment of THAT run (`CMA_LAUNCH_ID`,
`CMA_RUN_DIR`, `CMA_PROJECT_ROOT`, `CMA_ACTIVE_PROFILE`, `CMA_MANAGED`); do not
invent or reuse these from another session. In a separate terminal without that
environment, exit normally and launch `cma launch --profile personal -- --resume
ID_REAL_DE_SESION` only with consent to transfer context.
If you do not know the real ID, start a new session: never guess it or use
`--continue` as a substitute for the exact handoff.

## Uninstallation

Deliberate user actions in the terminal:

```sh
cma uninstall
# If installed at another destination:
cma uninstall --target /absolute/path/cma-runtime
claude plugin uninstall accounts@claude-multi-account-local
claude plugin marketplace remove claude-multi-account-local
```

Review the report: it removes only the managed installation/shims/integration,
not the official binary or unrelated shell files. It does not automatically remove profiles,
secrets, profile directories, history, or memory. To remove a secret or a profile
directory, deliberately run `cma profiles remove ID` (add `--purge` for a config-dir
directory) before uninstalling. Do not delete
official credentials or third-party data.

## Pending verification

Local tests with fake Claude/backends do not verify real auth; nothing here has been
tested with real accounts. For an authorized manual test: log in (or generate/import
tokens) outside the model, start the wrapper with
the plugin, check the account/resources through the official interface, consent to the switch,
exit with `/exit`, and verify the exact ID, profile, and desired persistence. Use
non-sensitive test content. **Real cross-account resumption, availability of
organizational resources, and compatibility with each Claude version have
not been verified here**; do not claim success before that test. Also unverified for
config-dir mode:

- Whether official Claude keeps a symlinked `settings.json`/`history.jsonl` as a symlink
  when rewriting it. If it replaces the link with a regular file, `EnsureProfileDir`
  reports a conflict and that profile diverges for that file.
- `plugins/` stores absolute install paths under the writing profile's directory; they
  resolve through that profile's symlink, and purging that profile can break them.
- `claude auth login` honouring `CLAUDE_CONFIG_DIR`, and remote control and
  cross-account `--resume` working with real accounts.
