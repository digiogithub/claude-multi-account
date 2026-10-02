---
name: status
description: Show the profile, project, and redacted local status.
disable-model-invocation: true
allowed-tools: Bash
---

Local status (no tokens):
!`"${CLAUDE_SKILL_DIR}/../../bin/cma" status --json`

Explain only this data, including the profile mode, configuration directory, and
login state when present. If there is no managed wrapper, say so: installing
the plugin does not activate the wrapper. CLI equivalent: `cma status --json`.
Do not inspect authentication variables, secret stores, or `.credentials.json`.
The organization/email are declared metadata and are not verified by the token.
