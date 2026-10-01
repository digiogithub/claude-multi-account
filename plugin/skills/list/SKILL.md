---
name: list
description: Listar perfiles locales sin mostrar secretos.
disable-model-invocation: true
allowed-tools: Bash
---

Perfiles locales redactados:
!`"${CLAUDE_SKILL_DIR}/../../bin/cma" profiles list --json`

Presenta los IDs y metadatos del resultado; conserva la indicación unverified y
caducidad desconocida cuando corresponda. No prometas descubrir organizaciones
ni verificar identidad. CLI equivalente: `cma profiles list --json`.
No leas tokens, variables de autenticación ni `.credentials.json`.
