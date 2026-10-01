# Arquitectura Go Linux

## Componentes

- `cmd/cma/main.go`: ejecutable único, delega en `internal/cli.Main`.
- `internal/core`: perfiles, Secret Service/file, proyecto, validación y estado.
- `internal/cli`: contrato CLI e internos `session-start`/`session-info`.
- `internal/launcher`: supervisor del Claude oficial, selección, conflictos,
  señales/stdio y handoff exacto después de salida normal.
- `internal/install`: copia el ejecutable compilado a un destino estable y crea
  shim POSIX `claude-cma`; función de shell solo opt-in explícito.
- `bin/cma`, `bin/claude-cma`: shims POSIX del checkout para `dist/cma`;
  el segundo invoca `launch --`. No reemplazan al Claude oficial.
- `plugin/skills`: seis skills manual-only; status/list redactados, use con
  consentimiento, project/memory sin secretos y add solo instrucciones humanas.
- `plugin/bin/cma`: bridge de ejecución, sin compilación/instalación implícita.
  SessionStart invoca `cma internal session-start` con JSON stdin.

SOLO Linux, Go >= 1.23, biblioteca estándar sin dependencias. Un binario maneja
administración y launch; no runtime adicional ni copia de código bajo plugin.

## Distribución: una sola fuente del runtime

La entrada `.claude-plugin/marketplace.json` usa **source `./`**, no `./plugin`.
Marketplace empaqueta la raíz completa con fuente Go, Makefile y plugin; declara
skills `./plugin/skills` y hook inline que ejecuta
`${CLAUDE_PLUGIN_ROOT}/plugin/bin/cma`. Usa entrada como manifiesto con
`strict: false`, sin `.claude-plugin/plugin.json` en la raíz.

En desarrollo, `plugin/.claude-plugin/plugin.json` carga layout convencional:
skills y `plugin/hooks/hooks.json`, cuyo comando es `${CLAUDE_PLUGIN_ROOT}/bin/cma`.
Este manifiesto anidado no es el de la raíz marketplace. Los skills usan
`${CLAUDE_SKILL_DIR}/../../bin/cma`: funciona en ambos layouts, sin rutas absolutas
al checkout de desarrollo ni rutas cache efímeras.

Bridge resuelve, en orden:

1. Si `CMA_BINARY` está definido: exige ruta absoluta a un archivo ejecutable
   estable, nunca al propio bridge (incluidas referencias al mismo archivo).
   No cae al binario local si el override es inválido.
2. Sin override: `dist/cma` dos niveles sobre `plugin/bin/cma`, tanto en checkout
   completo como copia marketplace. Debe existir y ser ejecutable.
3. Si falta: error claro que pide `make build` en la raíz completa o `make install`
   explícito desde checkout y `CMA_BINARY` estable; no instala/compila/descarga.

El bridge conserva argumentos, stdin y código de salida mediante `exec`.
No busca `cma` en PATH ni resuelve un runtime desde CWD.

**Instalar plugin no compila ni instala CLI.** `dist/` está ignorado por Git:
el paquete puede contener fuente sin binario. El usuario debe, en terminal,
ejecutar `make build` en la raíz completa de la copia del plugin, o usar una
instalación estable con `CMA_BINARY`. Actualizar cache puede requerir compilar
su nueva copia; la instalación estable evita depender de una ruta cache.
**Una copia aislada de plugin/ no es autosuficiente**: requiere `CMA_BINARY`.
No cambiar source ni duplicar runtime bajo plugin.

`make build` verifica host Linux y `go env GOOS`, compila `CGO_ENABLED=0 GOOS=linux`
a `dist/cma`, y comprueba metadatos de plataforma del resultado.
`make install` solicita build explícito y ejecuta `./dist/cma install`.
El instalador copia **el ejecutable actual** a
`~/.local/share/claude-multi-account/bin/cma` por defecto; crea `bin/claude-cma`
como shim hacia `cma launch --`. Otro destino admite `install --target`.
No copia fuente ni compila en runtime. `--shell` solicita función `claude` con
ruta estable que llama `cma launch -- "$@"`; por defecto no cambia shell.
Los argumentos administrativos se pasan al Claude oficial sin gestionar tokens.

## Contratos y estado

`core.ConfigHome()` usa CMA_CONFIG_HOME o XDG_CONFIG_HOME/claude-multi-account;
`core.StateHome()` usa CMA_STATE_HOME o XDG_STATE_HOME/claude-multi-account.
Defaults XDG bajo home. Overrides son rutas de confianza del usuario.
Registro version 1 con perfiles ID→metadatos y secret_ref. Config del proyecto:
`version`, `profile`, `allowedProfiles` opcional, `memory.mode` y
`switching.confirmContextTransfer`. Sin secretos ni código ejecutable.

El formato JSON preserva compatibilidad con los registros Python previos y
run/session/switch. Solo backends Linux `secret-service` y `file` aceptados;
perfiles macOS/`keychain` no soportados, sin conversión ni acceso a sus secretos.
No hay migración masiva del registro. Se mantiene la validación estricta de
versiones, tipos, campos y permisos; no confundir defaults con datos corruptos.

Wrapper descubre raíz, lee config, valida perfil/conflictos y obtiene token.
Lanza Claude con `CLAUDE_CODE_OAUTH_TOKEN` scoped y mismo CLAUDE_CONFIG_DIR.
Para memoria per-profile agrega `--settings` temporal 0600 con
`autoMemoryDirectory` estable por proyecto+perfil fuera del repo.
No reemplaza settings existentes ni migra memoria.

Por ejecución: `StateHome()/runs/<launch_id>` 0700, nonce aleatorio 32hex.
Entorno no secreto: CMA_LAUNCH_ID, CMA_ACTIVE_PROFILE, CMA_PROJECT_ROOT,
CMA_MANAGED=1 y CMA_RUN_DIR. JSON 0600, escritura atómica:

- `run.json`: version, launch_id, profile, project_root.
- `session.json`: session_id real, cwd, launch_id, project_root. Hook recibe
  `{session_id,cwd,...}` por stdin, no extrae ID de transcripciones/credenciales.
- `switch.json`: destino profile, scope session|project, session_id,
  confirmed=true, launch_id y project_root. Validación ligada a ejecución.

Sin wrapper hook avisa. En managed valida coherencia y rechaza mismatches.
`switch` comprueba consentimiento/perfil/proyecto/ID. Wrapper tras exit normal
procesa solicitud válida y relanza `--resume SESSION_ID` exacto, conservando
opciones sin duplicar resume/continue/session-id. Scope project persiste
asignación; scope session no. Error no auto-reinicia; no matar desde plugin ni
abrir TUI anidada. No sincronización entre sesiones.

## Pruebas y límites de evidencia

`go test ./...`, `go test -race ./...`, `go vet ./...` con temporales,
Claude y secretos falsos, sin red. `internal/plugincheck` verifica manifiestos,
seis skills manual-only, nombres, ausencia de flujo de tokens por modelo,
bridge aislado, argumentos/stdin/código de salida, rutas con espacios y
empaquetado de raíz. Compilación Linux y `sh -n` complementan tests.
`claude plugin validate` valida schemas localmente, no instala ni autentica.

Auth/reanudación intercuentas y recursos organizativos reales requieren prueba
manual autorizada; no se han verificado con los tests falsos. Validación estática
no garantiza todas las versiones del loader: revisar fuentes antes de probar
instalación marketplace en el entorno del usuario. Historial/CLAUDE.md/código
siguen comunes en modo A; sin aislamiento completo ni sincronización claude.ai.
