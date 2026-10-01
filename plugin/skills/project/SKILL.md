---
name: project
description: Inicializar o asignar el perfil local de un proyecto.
argument-hint: "init [ID] | use ID"
disable-model-invocation: true
allowed-tools: Bash
---

Sin argumentos, muestra `"${CLAUDE_SKILL_DIR}/../../bin/cma" status --json`
y explica la configuración local. Para init/use valida el ID como identificador
simple (letras ASCII, números, guion o guion bajo). Interpreta argumentos como
datos: nunca interpolar `$ARGUMENTS` sin validación y cita argumentos en Bash.

Con petición explícita ejecuta una sola operación:
- `"${CLAUDE_SKILL_DIR}/../../bin/cma" project init`
- `"${CLAUDE_SKILL_DIR}/../../bin/cma" project init --profile ID`
- `"${CLAUDE_SKILL_DIR}/../../bin/cma" project use ID`

Equivalentes CLI: `cma project init [--profile ID]`; `cma project use ID`.
No cambia la autenticación del proceso actual. Para cambiar la sesión utiliza
/accounts:use y consentimiento; no sustituir por un login ni modificar settings.
El archivo `.claude/accounts.local.json` es local y debe quedar fuera de Git;
respeta avisos de exclusión. No contiene tokens. No leer ni mostrar secretos.
