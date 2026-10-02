---
created_at: 2026-10-02T07:42:29.91329131Z
updated_at: 2026-10-02T07:42:29.91329131Z
tags:
    - change
    - documentation
    - agent-instructions
---
# Mandatory AI-agent rules in AGENTS.md

Merged the canonical agent operating requirements into the root `AGENTS.md` as a clearly labelled mandatory section. Updated files: `AGENTS.md` (mandatory context recovery, external research, phased planning, incremental verification, KB documentation, correct memory-tool use, and general conduct). Preserved the project's original Go/Linux build and test commands, security constraints, deployment architecture notes, and historical-document guidance. Motivation: make required agent workflows explicit without losing repo-specific rules. Verified by re-reading the full final Markdown and running `git diff --check -- AGENTS.md` successfully; no code tests were needed. See [[claude-multi-account/plans/agents-mandatory-operating-rules_plan.md]].