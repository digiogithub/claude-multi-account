---
name: project
description: Initialize a project's local profile or assign one to it.
argument-hint: "init [ID] | use ID"
disable-model-invocation: true
allowed-tools: Bash
---

With no arguments, show `"${CLAUDE_SKILL_DIR}/../../bin/cma" status --json`
and explain the local configuration. For init/use, validate the ID as a simple
identifier (ASCII letters, numbers, hyphen, or underscore). Treat arguments as
data: never interpolate `$ARGUMENTS` without validation, and quote arguments in Bash.

After an explicit request, execute only one operation:
- `"${CLAUDE_SKILL_DIR}/../../bin/cma" project init`
- `"${CLAUDE_SKILL_DIR}/../../bin/cma" project init --profile ID`
- `"${CLAUDE_SKILL_DIR}/../../bin/cma" project use ID`

Equivalentes CLI: `cma project init [--profile ID]`; `cma project use ID`.
This does not change the current process's authentication. To change the session, use
/accounts:use and obtain consent; do not replace this with a login or modify settings.
The `.claude/accounts.local.json` file is local and must remain outside Git;
respect exclusion warnings. It contains no tokens. Do not read or display secrets.
