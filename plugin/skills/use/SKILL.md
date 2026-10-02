---
name: use
description: Request a temporary or persistent profile switch with consent.
argument-hint: "<ID> [session|project]"
disable-model-invocation: true
allowed-tools: Bash
---

If the ID is missing, run `"${CLAUDE_SKILL_DIR}/../../bin/cma" profiles list --json`,
show the available profiles, and ask the user to choose one. Do not choose for them.
Treat arguments as data, never as shell code. The ID must be
a simple profile identifier (ASCII letters, numbers, hyphen, or underscore;
if unsure, show `profiles list --json`). Scope must be session or project;
if missing, ask the user and do not run the switch yet. Do not interpolate
`$ARGUMENTS` directly in Bash; quote each validated argument.

Before executing, warn that resuming transfers the conversation/context to the
destination profile, that history and files remain shared, and that project
changes the persistent assignment. Obtain explicit consent for that
transfer. Do not add the confirmation option without that consent.

After confirmation, execute with the Bash tool:
`"${CLAUDE_SKILL_DIR}/../../bin/cma" switch ID --scope session --confirm-context-transfer`
Replace ID with the validated ID and session with the chosen scope.
CLI equivalent: `cma switch ID --scope session|project --confirm-context-transfer`
(where session|project represents a choice, not a pipeline).

If successful, tell the user to exit normally with `/exit`; the wrapper will resume the
exact recorded ID. Do not kill processes, open another TUI, or claim the switch
is instant. If it fails, show the error without trying another profile. Without a managed
session or if the quota is exhausted, explain the fallback: use another terminal inheriting
the session's CMA environment, or exit and run `cma launch --profile ID --
--resume SESSION_ID` with consent and the real ID; never invent an ID.
Switching works in both modes; with config-dir profiles the resume resolves through
the shared `projects/` directory. Actual resumption across accounts is unverified. Do not receive or print secrets.
