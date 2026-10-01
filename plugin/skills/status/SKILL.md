---
name: status
description: Mostrar perfil, proyecto y estado local redactado.
disable-model-invocation: true
allowed-tools: Bash
---

Estado local (sin tokens):
!`"${CLAUDE_SKILL_DIR}/../../bin/cma" status --json`

Explica solamente estos datos. Si no hay wrapper gestionado, indícalo: instalar
el plugin no activa el wrapper. CLI equivalente: `cma status --json`.
No inspecciones variables de autenticación, almacenes secretos ni `.credentials.json`.
La organización/email son metadatos declarados y no verificados por el token.
