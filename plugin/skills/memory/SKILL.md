---
name: memory
description: Elegir memoria automática shared o per-profile para próximos lanzamientos.
argument-hint: "shared|per-profile"
disable-model-invocation: true
allowed-tools: Bash
---

Sin argumentos muestra `"${CLAUDE_SKILL_DIR}/../../bin/cma" status --json`.
Acepta solamente shared o per-profile y no interpoles `$ARGUMENTS` directamente
como shell. Tras petición explícita ejecuta, con valor validado:
`"${CLAUDE_SKILL_DIR}/../../bin/cma" project memory per-profile`
o `"${CLAUDE_SKILL_DIR}/../../bin/cma" project memory shared`.
Equivalente CLI: `cma project memory shared|per-profile` (elige un valor).

La política afecta próximos lanzamientos; no migra ni copia memoria existente.
per-profile configura solo autoMemoryDirectory con un overlay temporal del
wrapper. Historial, conversación reanudada, CLAUDE.md, código y resto de
configuración siguen compartidos: NO es aislamiento completo de cuentas.
Respeta los avisos de políticas/settings de memoria existentes. No borres,
enlaces ni cambies archivos de memoria directamente; no leas secretos.
