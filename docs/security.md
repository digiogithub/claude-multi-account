# Security and Limitations

## Trust boundary

In config-dir mode (the default) the Claude subprocess gets only `CLAUDE_CONFIG_DIR`
pointing to the profile's directory; cma stores, reads, and injects no token. In token
mode it uses only `CLAUDE_CODE_OAUTH_TOKEN` and preserves the ambient
`CLAUDE_CONFIG_DIR`. In neither mode does it read/exchange `.credentials.json`,
implement private OAuth, or modify official login credentials. It does not use
the undocumented `CLAUDE_CODE_ORGANIZATION_UUID` variable as a selector. The login
(config-dir) or token determines identity; email, organization, and organization ID are **unverified**
metadata declared by the user, not verified from the token. `--email`,
`--organization`, and `--organization-id` are all optional; omitted values are
stored empty and displayed as unknown. There is no automatic network/token identity
or organization discovery: no documented supported API for that discovery has been
confirmed. There is no synchronization with claude.ai.

This does not fully isolate accounts. History, transcripts,
CLAUDE.md, code, project files, and usual configuration are shared; in config-dir mode
this includes everything linked into the shared directory. `per-profile`
separates automatic memory only through autoMemoryDirectory; it does not migrate
memory or delete prior content. Resuming across profiles transfers conversation
and potentially sensitive context: it requires explicit consent. Even
separate memory paths do not prevent the model from accessing authorized files.

Users/processes with the same system identity, root, malware, or backups
may access local processes/data. Permissions 0700/0600 and the keychain reduce
exposure; they do not protect against those adversaries. The manager is not a sandbox
or an organizational boundary. `allowedProfiles` is editable local policy,
not server authorization.

## Config-dir mode and the shared directory

- cma never opens `.credentials.json`; login state uses `Lstat` only. `.credentials.json`
  and `.claude.json` are never shared, linked, copied by `shared import`, or opened.
- Accounts home, shared, `profiles`, and each profile directory are owner-only 0700 with
  no symlinked ancestors (`SecureDirectory`). Like the keychain, this does not protect
  against the same UID, root, malware, or backups.
- Symlinks are created only inside a profile directory and only to fixed shared names;
  nothing is overwritten (conflicts are reported). `profiles remove --purge` unlinks
  symlinks without following them and refuses paths that are not exactly the profile
  directory or that have symlinked ancestors.
- **Sharing `projects/` means conversation history and memory are readable under every
  account.** Switching still requires `--confirm-context-transfer`.
- **Shared `settings.json` applies to all accounts.** An `apiKeyHelper` or auth `env`
  entry there is detected as a conflict before launch.
- `shared import` copies from a source directory into shared; never import from a
  directory you do not trust. Top-level symlinked entries are followed only when they
  resolve outside the accounts and shared homes; nested symlinks are recreated as symlinks,
  never followed. Imported settings, skills, and plugins apply to every account.

## Tokens and storage (token mode)

- Official `claude setup-token` flow outside the model; import in a Linux terminal
  with hidden input (echo disabled and restored on completion).
  Never request a token in chat or arguments; nor use stdin handled by the model.
  `--token-stdin` is reserved for deliberate external automation.
- `auto`: Secret Service through `secret-tool` on Linux. If no keychain is available,
  fail and explain; do not silently fall back to writing a file.
- `--backend file`: explicit opt-in, plaintext 0600 under a private directory;
  risk from backups, malware, and same UID. No encryption equivalent to the keychain.
- Registry contains metadata and secret references only, not tokens. The
  list/status/JSON commands and run logs must not include them.
- No global export, token in settings, logs, or arguments to the Claude process.
  The child environment contains the token: do not dump or capture it with `env`.
- Known local expiration blocks launch until renewal; an unknown expiration is
  shown as such and does not prove validity. The manager does not verify the token over the network.

Do not enable shell trace (`set -x`) or environment logging during secret flows.
External keychain commands are within the host's trust boundary; a
compromised backend invalidates the protection. If a token is shared by mistake,
do not reproduce it: revoke/renew it using official tools.

## Authentication and policies

Before launch, conflicts involving API keys/auth tokens, apiKeyHelper,
cloud/gateway providers, and custom endpoints are detected in relevant environment/settings.
The manager fails without removing those policies and without choosing another
profile. Environment and settings auth/routing variables and `apiKeyHelper` conflict in
both modes, because they would override the directory login; `forceLoginMethod` and
`forceLoginOrgUUID` conflict only in token mode. This is not an exhaustive check of external infrastructure or a way to bypass
Claude administration. There is no automatic quota rotation,
hot authentication, or silent account fallback.

## Switch requests

Each wrapper creates an independent run with a nonce, mode-0700 directory, and mode-0600 JSON.
SessionStart records ID/cwd with validation; switch validates the run,
allowed profile, project, registered session, and consent. A request
for another run or a tampered request must not permit handoff. There is no global file
that changes all sessions. The TUI is not killed: the request is processed after normal exit;
Claude errors do not cause an automatic restart. It resumes the exact ID, not `--continue`.
A user with the same UID can manipulate files; validation reduces mistakes
and casual forgery, but does not replace an OS boundary.

## Plugin and installation

All `accounts` skills use `disable-model-invocation: true`; this controls
invocation, but does not turn skill text into a sandbox. Import/renewal
are human-only terminal actions. SessionStart reads only session JSON from
stdin and calls the core; it receives no tokens. Loading the bridge does not
install anything. An absolute `CMA_BINARY` is a trusted executable selected by
the user; do not point it at untrusted code.

The marketplace includes the full root as the canonical Go source. Review
content and version before compiling or installing. `dist/` is ignored, and the
binary may not be included: use explicit `make build` at the full plugin root
or set `CMA_BINARY` to a stable installation. Hooks do not compile or install.
Do not publish local data alongside the checkout; `.gitignore` does not prevent copying
unrelated files into a manual package. Installing the plugin does not install the CLI/shims or
change the shell. Stable installation and `--shell` are explicit. Local config
`.claude/accounts.local.json` is excluded from Git locally or warned about;
always check status before publishing.

The existing JSON format (version 1) is preserved; config-dir profiles use
`secret_backend: "config-dir"` with no secret, and otherwise only `secret-service` and
`file` secrets are supported. Profiles from other systems are neither converted nor do their
secrets get automatically recovered; preserve prior data and decide on an explicit
Linux import in the terminal.

## Out of scope / tests

Local Linux terminal only (WSL with a Linux environment); no VSCode, Desktop, or background. No promise
to synchronize claude.ai, organizational resources, or provide complete isolation. Tests
with fake binaries/backends do not verify real cross-account resumption or auth.
Those tests require permission, the user's login, and non-sensitive content; they
were not performed here. Unverified with real Claude: whether it keeps symlinked
`settings.json`/`history.jsonl` as links when rewriting them (otherwise a conflict is
reported and that file diverges per profile); that `plugins/` absolute install paths
survive purging the profile that wrote them; that `claude auth login` honours
`CLAUDE_CONFIG_DIR`; and that remote control and cross-account `--resume` work. Do not access real credentials or install/modify the shell
to run development tests.
