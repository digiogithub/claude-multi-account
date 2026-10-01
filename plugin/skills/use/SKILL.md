---
name: use
description: Solicitar cambio de perfil temporal o persistente con consentimiento.
argument-hint: "<ID> [session|project]"
disable-model-invocation: true
allowed-tools: Bash
---

Si falta el ID, ejecuta `"${CLAUDE_SKILL_DIR}/../../bin/cma" profiles list --json`,
muestra los perfiles disponibles y pide al usuario que elija uno. No elijas por él.
Interpreta los argumentos como datos, nunca como código shell. El ID debe ser
un identificador simple de perfil (letras ASCII, números, guion o guion bajo;
si hay duda muestra `profiles list --json`). Scope solamente session o project;
si falta, pregunta al usuario y no ejecutes el cambio todavía. No interpolar
`$ARGUMENTS` directamente en Bash; citar cada argumento validado.

Antes de ejecutar, advierte que reanudar transfiere la conversación/contexto al
perfil destino, que el historial y archivos siguen compartidos y que project
cambia la asignación persistente. Pide consentimiento explícito para esa
transferencia. No añadir la opción de confirmación sin ese consentimiento.

Tras confirmar, ejecuta con la herramienta Bash:
`"${CLAUDE_SKILL_DIR}/../../bin/cma" switch ID --scope session --confirm-context-transfer`
Sustituye ID por el ID validado y session por el scope elegido.
CLI equivalente: `cma switch ID --scope session|project --confirm-context-transfer`
(donde session|project representa una elección, no una tubería).

Si tiene éxito, indica salir normalmente con `/exit`; el wrapper reanudará el
ID exacto registrado. No mates procesos, no abras otra TUI y no afirmes cambio
instantáneo. Si falla, muestra el error sin intentar otro perfil. Sin sesión
gestionada o con cuota agotada, explica el fallback en otra terminal heredando
el entorno CMA de la sesión, o salir y ejecutar `cma launch --profile ID --
--resume SESSION_ID` con consentimiento y el ID real; nunca inventar un ID.
Reanudación real entre cuentas no verificada. No recibir ni imprimir secretos.
