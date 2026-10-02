---
name: add
description: Explain safe setup from a terminal; never import tokens in chat.
argument-hint: "<ID>"
disable-model-invocation: true
---

Do not run imports, logins, or setup-token using model tools. Do not
ask for tokens in chat, accept them as arguments, build pipelines
with secrets, or read `.credentials.json`. If the user pastes a token,
do not repeat or save it; recommend revoking/renewing it through official channels.

There are two profile modes. **config-dir is the default and the recommended one**:
each profile has its own Claude configuration directory (under
`~/.claude-accounts/profiles/<id>`, or `CMA_ACCOUNTS_HOME`) with a normal full login,
and cma stores no token. Token mode is the opt-in alternative.

## Primary flow: config-dir mode

Tell the user to run this in ANOTHER local terminal:

```sh
cma profiles add personal --label "Personal"
cma profiles login personal
```

The user replaces the ID and label. `profiles add` registers the profile and creates
its directory; `profiles login` runs the official Claude login for that directory, in
the user's terminal and outside the model. Optionally, `cma shared import` copies
existing skills, agents, settings, and similar entries from `~/.claude` into the shared
directory that all config-dir profiles link to; it is non-destructive. `--email`,
`--organization`, and `--organization-id` are all optional: supply them only if known.
They are declared, unverified metadata, not verified identity or independent
organization selection. Omitted values are stored empty and displayed as unknown. There
is no automatic network or identity discovery.

## Secondary flow: token mode

Use it only if the user explicitly wants a long-lived token. `setup-token` tokens are
inference-only, so sessions start without remote control and other features that need a
full login. In ANOTHER local terminal:

```sh
claude setup-token
cma profiles add personal --label "Personal" --mode token
```

The import prompts for the token with hidden input in a Linux terminal. The auto
backend uses Secret Service when secret-tool is available; `--backend file` is an
explicit opt-in with the risk of 0600 plaintext storage. To renew (token mode only):
run `cma profiles renew personal` from a terminal, also with hidden input.
Do not use `--token-stdin` through the model; it is only for deliberate external automation, with
no tokens in history or logs.

## Installing cma

If cma is missing, guide the user without performing the installation: installing the plugin does NOT
build/install the CLI or modify the shell. In ANOTHER Linux terminal, from the root of the
complete checkout with Go >= 1.23, they can explicitly run `make build` and `make install`,
or run `./dist/cma install` after building. The default stable executable
is `~/.local/share/claude-multi-account/bin/cma`; they can use its absolute path
if it is not in PATH. Shell integration requires explicit `install --shell`
and is not necessary.

The bridge requires an absolute `CMA_BINARY` path to the stable executable, or an explicit `make build`
from the root of the complete plugin checkout to create `dist/cma`.
An isolated copy of plugin/ requires CMA_BINARY. The marketplace includes the complete
source, but dist/ is ignored and may not include a binary. Hooks never build
or install. Do not promise automatic installation or credential access after `/plugin install`.
