---
name: list
description: List local profiles without showing secrets.
disable-model-invocation: true
allowed-tools: Bash
---

Redacted local profiles:
!`"${CLAUDE_SKILL_DIR}/../../bin/cma" profiles list --json`

Present the IDs and metadata from the result; the `secret_backend` field shows the
mode (`config-dir` or a token backend); preserve the unverified indicator and
unknown expiry where applicable. Do not promise to discover organizations
or verify identity. CLI equivalent: `cma profiles list --json`.
Do not read tokens, authentication variables, or `.credentials.json`.
