# Claude Multi Account

Use a different Claude Code profile depending on the project, without manually switching logins.

**Linux only.** Implemented in Go with the standard library. A single binary to manage profiles, launch Claude, and switch accounts; no Python, Node, or Bun.

> Independent project, not affiliated with Anthropic. Authentication and resuming across real accounts require a manual test: automated tests use fake executables and credentials.

## What it lets you do

- Register reusable personal and company profiles across all your projects.
- Use two profiles with the same email but different organizations, or with different users.
- Give each profile a normal full login in its own Claude configuration directory (default), or opt in to a long-lived `setup-token` token.
- Share skills, agents, commands, settings, and project memory/history across config-dir profiles through one shared directory.
- Save each project's profile in `.claude/accounts.local.json`, without secrets.
- Keep typing `claude` with optional shell integration, or use `claude-cma` explicitly.
- List profiles and request switches through the `accounts` plugin slash commands.
- Switch only the current session or update the project's default profile.
- Share automatic memory across profiles or keep it separate by profile.

There is no automatic rotation when the quota is exhausted and no hot authentication switching.

## How it works

```text
claude-cma / claude with shell integration
    │
    ├─ Finds the project and reads .claude/accounts.local.json
    ├─ Resolves the profile in the user's local registry
    ├─ Checks authentication and configuration conflicts
    ├─ config-dir profile (default): sets CLAUDE_CONFIG_DIR to the profile's directory
    │   token profile: retrieves its token from the keychain
    │   and sets CLAUDE_CODE_OAUTH_TOKEN
    └─ Starts the official Claude
           │
           └─ accounts plugin: query, configure, and request switches
```

## Profile modes

| | config-dir (default) | token (opt-in) |
| --- | --- | --- |
| Add | `cma profiles add ID --label L` | `cma profiles add ID --label L --mode token` |
| Login | normal full login, `cma profiles login ID` | `claude setup-token`, pasted into a hidden prompt |
| Account selected by | `CLAUDE_CONFIG_DIR=~/.claude-accounts/profiles/<id>` | `CLAUDE_CODE_OAUTH_TOKEN` |
| Stored by cma | no token; the directory belongs to official Claude | token in Secret Service or a `0600` file |
| Remote control and other login-dependent features | available (unverified with real accounts) | not available |

`setup-token` tokens are inference-only: token-mode sessions start without remote control and other features that need a full login. config-dir mode exists to fix that, and is the default.

### What config-dir mode means for you

- Each profile is one account with its own ordinary, complete Claude login, so Claude starts with everything that account is entitled to.
- Everything else is common to all your accounts: conversations and history, project memory, skills, agents, commands, plugins, and settings. Install a skill or change a setting under one account and it is there under the others.
- You log in to each account once. After that, starting Claude in a project opens it with the account assigned to that project; different projects can use different accounts at the same time.
- You can move a conversation to another account and continue it there.
- Your usual `~/.claude` setup is untouched and still used when you start official Claude without the wrapper. `cma shared import` copies it so your profiles start with the same skills, settings, and history.

A step-by-step description is in [the usage guide](docs/usage.md#how-config-dir-mode-works-in-practice).

In token mode the wrapper preserves Claude's configuration directory (`CLAUDE_CONFIG_DIR`, if defined). In config-dir mode it sets `CLAUDE_CONFIG_DIR` to the profile's directory. In neither mode does it exchange or read `.credentials.json`, modify your usual login, or globally export tokens to the shell.

**The login (config-dir) or the token (token mode) determines the account and organization.** The emails and UUIDs you enter are declared metadata, not a verified identity or a selector that can change the organization.

If a project has no profile, interactive startup lets you choose one and decide whether to save it. You can also continue with your usual login without managing that session. In non-interactive mode (`-p`/`--print`), a missing assignment produces an error, not a selector.

## Quick start

```sh
make build && ./dist/cma install
cma profiles add trabajo --label "Trabajo"
cma profiles login trabajo
cma shared import              # optional: copy skills/settings from ~/.claude
cd /path/to/project
cma project init --profile trabajo
claude-cma
```

Each step is detailed below. Token mode is opt-in: see [token mode](#token-mode-opt-in).

## Requirements

- Linux; WSL also works if you run everything inside the Linux environment.
- Official Claude Code installed and available in `PATH`.
- Go >= 1.23 and `make` to build. The resulting binary does not need Go to run.
- Only for token mode with the default backend: `secret-tool` and an available, unlocked Secret Service. config-dir mode needs no keychain.
- Bash or Zsh only if you want to integrate the `claude` command.

In token mode without a keychain, you can explicitly choose `--backend file`: it stores the token in plaintext with `0600` permissions. This is not an automatic fallback.

## 1. Build and install the CLI

From the repository root:

```sh
make build
./dist/cma install
export PATH="$HOME/.local/share/claude-multi-account/bin:$PATH"
```

Installation copies the binary to `~/.local/share/claude-multi-account/bin/cma` and creates the `claude-cma` shim. It does not modify the default shell. The `export PATH` above only affects the current terminal.

You can try it without installing:

```sh
./bin/cma --help
./bin/claude-cma
```

The checkout shims require that you have run `make build`.

### Optional: keep typing `claude`

```sh
cma install --shell
```

Adds a managed block to `.bashrc` or `.zshrc` with a `claude` function that calls the wrapper. Open a new terminal to load it. The official binary is not replaced.

If you already have a `claude` alias, it may take precedence: check with `type claude` and decide whether to remove it manually, or use `claude-cma`.

Administrative commands such as `claude setup-token`, `claude auth`, and `claude plugin` are passed to official Claude without retrieving tokens from the manager. When the project (or `--profile`) resolves to a config-dir profile, they run against that profile's directory.

## 2. Register your profiles

### config-dir mode (default)

```sh
cma profiles add trabajo --label "Trabajo"
cma profiles login trabajo
```

`profiles add` registers the profile and creates its directory, `~/.claude-accounts/profiles/trabajo` (override the base with `CMA_ACCOUNTS_HOME`). `profiles login` runs the official Claude `auth login` for that directory: run it **in your terminal, outside the chat**, and complete the normal full login. cma stores no token and never opens `.credentials.json`.

Repeat for another account:

```sh
cma profiles add personal --label "Personal"
cma profiles login personal
```

`--email`, `--organization`, and `--organization-id` are all optional: supply them only if known. They are declared, unverified metadata; omitted values are stored empty and displayed as unknown. The tool makes no network requests to discover identity or organizations or verify this metadata. No documented supported API for that discovery has been confirmed.

```sh
cma profiles list
cma profiles remove personal
cma profiles remove personal --purge
```

`profiles list` shows each profile's mode and, for config-dir profiles, whether a login is present. `remove` deletes only the registry entry and keeps and reports the directory; `--purge` also deletes the profile directory, credentials included. Removing a profile does not reconfigure projects that reference it.

### Shared directory

All config-dir profiles link a fixed list of entries into one shared directory, `~/.claude-accounts/shared` (override with `CMA_SHARED_HOME`), through absolute symlinks created inside each profile directory:

| Entry | Kind |
| --- | --- |
| `projects`, `skills`, `agents`, `commands`, `plugins`, `todos`, `plans`, `file-history` | directory |
| `settings.json`, `CLAUDE.md`, `history.jsonl` | file |
| `keybindings.json` | file, linked only if it exists in shared |

`.credentials.json` and `.claude.json` are never shared, linked, or opened: each account keeps its own. Existing entries in a profile directory are never overwritten; a conflict (a regular file or a link elsewhere) is reported as a warning at launch.

```sh
cma shared import [--from DIR]
cma shared sync [--json]
cma shared path
```

- `import` copies entries from `~/.claude` (or `DIR`) into the shared directory, only where the shared entry is missing or empty. A top-level source entry that is a symlink (common with dotfile managers) is followed only when it resolves outside the accounts and shared directories and its kind matches; nested symlinks are recreated as symlinks, never followed. Directories are copied into a temporary sibling and renamed into place only on success, so a failed import leaves no partial entry. It never modifies the source and never copies `.credentials.json` or `.claude.json`.
- `sync` re-creates missing links for every config-dir profile and reports the result; `--json` prints the per-profile link reports as JSON.
- `path` prints the shared directory.

### token mode (opt-in)

Generate a token for the desired account through the official flow, **in your terminal, outside the chat**:

```sh
claude setup-token
```

Then register the profile:

```sh
cma profiles add personal --label "Personal" --mode token
```

`cma` prompts for the token with hidden input. Never pass it as an argument or paste it into a slash command. `--backend`, `--expires-at`, and `--token-stdin` are valid only with `--mode token`. The `--email`, `--organization`, and `--organization-id` metadata is optional here too.

Repeat the official flow for another organization and register another profile:

```sh
cma profiles add empresa --label "Empresa" --mode token
```

Profiles can share an email. Make sure each token corresponds to the intended context: the tool does not verify token identity or organizations.

```sh
cma profiles renew empresa
```

Renewal (token mode only) prompts for the hidden token again. You can declare a known expiration when adding a profile with `--expires-at`; otherwise, it is shown as unknown. Remember the limitation: these tokens are inference-only, so sessions start without remote control and other login-dependent features.

## 3. Install the plugin

```sh
claude plugin marketplace add /ruta/absoluta/claude-multi-account
claude plugin install accounts@claude-multi-account-local
export CMA_BINARY="$HOME/.local/share/claude-multi-account/bin/cma"
```

Define `CMA_BINARY` before starting Claude with the plugin. It must point to the stable binary, not `plugin/bin/cma`.

**Installing the plugin does not build or install the CLI.** The marketplace includes the repository source, but `dist/` is not versioned. Hooks never build, download, or install anything.

For local development, after building:

```sh
./bin/claude-cma --plugin-dir /ruta/absoluta/claude-multi-account/plugin
```

Without `CMA_BINARY`, the bridge looks only for `dist/cma` in the full plugin/checkout root. A standalone copy of `plugin/` requires the stable binary.

## 4. Assign a profile to the project

From the project directory:

```sh
cma project init --profile trabajo
claude-cma
```

If you enabled shell integration, you can start with `claude` instead of `claude-cma`.

The configuration is local:

```json
{
  "version": 1,
  "profile": "trabajo",
  "allowedProfiles": ["trabajo", "personal"],
  "memory": {
    "mode": "shared"
  },
  "switching": {
    "confirmContextTransfer": true
  }
}
```

- `profile`: default profile.
- `allowedProfiles`: optional; restricts the profiles that can be selected for this project.
- `memory.mode`: `shared` or `per-profile`.
- Session switches require explicit consent before transferring context.

Keep `.claude/accounts.local.json` out of Git; for example, by adding a local repository exclusion. The CLI warns you, but does not modify your global Git configuration.

Subdirectories use the Git root. Each worktree has its own assignment. Outside Git, a nearby configuration is searched for in parent directories.

```sh
cma project use personal
cma status
```

`project use` affects future launches, not a session that is already open.

## 5. View and switch from Claude

| Slash command | Action |
| --- | --- |
| `/accounts:status` | Active profile, project assignment, and memory status |
| `/accounts:list` | Available profiles, without secrets |
| `/accounts:use` | List profiles and choose a destination and scope |
| `/accounts:use personal session` | Switch only this session |
| `/accounts:use trabajo project` | Save the project profile and request the switch |
| `/accounts:project init trabajo` | Initialize the project assignment |
| `/accounts:add` | Instructions for registering a profile in the terminal |
| `/accounts:memory per-profile` | Separate automatic memory |

All skills are manual-only: Claude must not choose an account on its own.

### What happens when switching

1. Choose a profile and scope (`session` or `project`).
2. Confirm that conversation context may be sent under the destination profile.
3. The plugin records the request associated with the exact ID of that session.
4. Exit normally with `/exit`.
5. The wrapper relaunches Claude with the destination profile (its directory or token) and `--resume` for the recorded ID. With config-dir profiles, the shared `projects/` directory lets the destination find the session.

It does not kill processes or open another interface inside the current one. An exit with an error does not trigger an automatic restart. Other open sessions keep their profiles.

The operation is also available in the CLI, from the environment of a managed session:

```sh
cma switch personal --scope session --confirm-context-transfer
```

Slash commands may need the model and can fail if the quota is exhausted. For recovery from another terminal, see [the usage guide](docs/usage.md#launch-and-switch): do not invent IDs or reuse another session's state.

## Shared or per-profile memory

```sh
cma project memory shared
cma project memory per-profile
```

- **`shared`**: keeps the project's existing automatic memory when switching profiles.
- **`per-profile`**: configures a different automatic memory folder for each project and profile using a temporary settings overlay. It works in both modes.

It does not automatically copy or migrate existing memories. In config-dir mode, automatic memory and history are already common to all profiles through the shared `projects/` link; in token mode they live in the Claude directory you use.

**Separating memory does not isolate conversations, code, or `CLAUDE.md`.** Claude's history and configuration remain shared (in config-dir mode, also `settings.json`, skills, and the rest of the shared entries). Resuming a conversation with another account may send business content under a personal account: confirm that this is permitted.

## Security and limitations

- config-dir mode (default): cma stores no token and never opens `.credentials.json` (it only checks that the file exists). Each profile directory is owner-only (`0700`) and credentials are never shared.
- Sharing `projects/` makes conversation history and memory readable under every account; a shared `settings.json` applies to all accounts. Switching still requires explicit consent.
- Token mode: tokens are stored in Secret Service by default; a `0600` file is used only when explicitly chosen. They are kept out of arguments, project configuration, and plugin responses.
- In token mode the token is injected into the Claude process environment; this does not protect against code running as the same system user.
- API key, provider, or routing conflicts are rejected; policies are not removed and another account is not selected silently.
- Private OAuth is not implemented, and official login credentials are not read.
- Local Linux terminal only: VS Code and Claude Desktop are out of scope. `--bare` is rejected in both modes; `--bg`/`--background`/`--cloud` are rejected only in token mode (config-dir mode has a full login and forwards them, unverified).
- Token mode: `setup-token` tokens are not equivalent to a full login; remote control and claude.ai skill synchronization are unavailable and organizational resources need verification.
- Nothing has been tested with real accounts. Unverified: that official Claude keeps symlinked `settings.json`/`history.jsonl` as links when rewriting them (otherwise a conflict is reported and that file diverges); that `plugins/` absolute install paths survive purging the profile that installed them; that `claude auth login` honours `CLAUDE_CONFIG_DIR`; and that remote control and cross-account `--resume` work with real accounts. These and compatibility with each Claude version require an authorized manual test.

More details in [security](docs/security.md) and [architecture](docs/architecture.md).

## Uninstall

```sh
cma uninstall
claude plugin uninstall accounts@claude-multi-account-local
claude plugin marketplace remove claude-multi-account-local
```

Uninstallation removes managed files and integration while preserving modified or unrelated files. It does not automatically delete profiles, tokens, profile directories, memory, or history. If you want to remove a token or a profile directory, deliberately run `cma profiles remove ID` (with `--purge` for the directory) before uninstalling.

## Development

```sh
go test ./...
go test -race ./...
go vet ./...
make build
sh -n bin/cma && sh -n bin/claude-cma && sh -n plugin/bin/cma
claude plugin validate ./plugin
claude plugin validate ./.claude-plugin/marketplace.json
```

Tests use temporary directories, executables, and fake secrets, without network access or login. See [AGENTS.md](AGENTS.md) for development rules and [docs/usage.md](docs/usage.md) for the complete CLI contract.

## License

Distributed under the [MIT license](LICENSE).
