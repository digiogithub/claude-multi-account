---
created_at: 2026-10-02T07:41:56.440841124Z
updated_at: 2026-10-02T07:42:24.812212064Z
tags:
    - plan
    - agent-instructions
---
# Merge mandatory AI-agent rules into AGENTS.md

## Goal
Integrate the canonical mandatory operating requirements into the existing root `AGENTS.md` while preserving all project-specific build, security, architecture, and workflow guidance.

## Phases
1. Recover KB context and read root instructions; compare the current document against each canonical requirement. **Complete.**
2. Add a clearly labelled mandatory section, adapting tool guidance and retaining project-specific requirements. **Complete.**
3. Re-read and verify Markdown structure, mandatory coverage, and unchanged project guidance; record the completed change in the KB. **Complete.**

## Verification
Re-read the final file. Confirmed each canonical topic is covered under an explicitly `MANDATORY`-labelled heading and the original development commands, safety constraints, architecture notes, and historical-document instructions remain intact. `git diff --check -- AGENTS.md` passed. No code tests were needed for this documentation-only change.