---
name: memory
description: Choose shared or per-profile automatic memory for future launches.
argument-hint: "shared|per-profile"
disable-model-invocation: true
allowed-tools: Bash
---

With no arguments, show `"${CLAUDE_SKILL_DIR}/../../bin/cma" status --json`.
Accept only shared or per-profile, and do not interpolate `$ARGUMENTS` directly
as shell code. After an explicit request, execute with the validated value:
`"${CLAUDE_SKILL_DIR}/../../bin/cma" project memory per-profile`
or `"${CLAUDE_SKILL_DIR}/../../bin/cma" project memory shared`.
CLI equivalent: `cma project memory shared|per-profile` (choose one value).

The policy affects future launches; it does not migrate or copy existing memory.
With config-dir profiles, automatic memory and history are already common to all of
them through the shared `projects/` directory; `per-profile` separates automatic
memory in both modes. per-profile configures only autoMemoryDirectory using a temporary wrapper
overlay. History, resumed conversation, CLAUDE.md, code, and the rest of the
configuration remain shared: this is NOT complete account isolation.
Respect existing memory policy/settings warnings. Do not delete,
link, or change memory files directly; do not read secrets.
