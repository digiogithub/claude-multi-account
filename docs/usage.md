# Uso de Claude Multi Account (v1)

## Requisitos y alcance

Terminal local SOLO Linux (WSL con entorno Linux); Go >= 1.23 para compilar;
Claude Code oficial instalado. Ejecución con un único binario, sin runtime adicional.
No integración con VSCode, Desktop ni procesos background. Perfiles locales
reutilizables por usuario; no sincronización de cuentas/estado con claude.ai.
No descubre organizaciones ni comprueba que email/organización coincidan con el
token: siempre `verification: unverified`. No usar esta herramienta para eludir
políticas de la organización.

### Compatibilidad de datos existentes

Se conserva el formato JSON version 1 de perfiles, configuración y protocolos
run/session/switch de la implementación anterior en Python, sin migración masiva
ni cambios de referencias a secretos. Solo se admiten perfiles `secret-service`
y `file` en Linux. Los perfiles macOS/`keychain` existentes son **no soportados**:
no se convierten ni se leen sus secretos. Para usarlos en Linux, importa un token
nuevo mediante el flujo oficial en terminal y un perfil Linux; conserva los datos
anteriores hasta decidir explícitamente cómo retirarlos.

## Instalación explícita

En terminal del usuario, desde la raíz del checkout completo en Linux:

```sh
make build
make install
```

`make build` comprueba host Linux y `go env GOOS=linux`, y compila `dist/cma`.
`make install` ejecuta `./dist/cma install` después de compilar explícitamente.
El instalador copia el ejecutable actual a la ruta estable
`~/.local/share/claude-multi-account/bin/cma` y crea el shim POSIX `claude-cma`.
No copia otro runtime ni descarga Go/Claude; no hace login. Añade la ruta `bin`
informada a PATH por decisión propia o usa sus ejecutables por ruta absoluta.
Con destino propio, después de `make build`:

```sh
./dist/cma install --target /ruta/absoluta/cma-runtime
```

El mismo binario maneja CLI y supervisor con `cma launch -- ...`; `claude-cma`
es solo un shim hacia esa operación. Los shims del checkout `./bin/cma` y
`./bin/claude-cma` apuntan a `dist/cma`, no compilan automáticamente.

`claude-cma` es el wrapper opt-in sin reemplazar el binario oficial. La opción
`cma install --shell` solicita integración explícita mediante función `claude`;
no es necesaria para usar el wrapper. Revisa el informe del instalador y el
shell compatible. El `claude` real sigue disponible y no se sustituye en disco.
Si necesitas fijarlo, usa `CMA_CLAUDE_BINARY=/ruta/absoluta/claude` al lanzar.
La integración conserva los aliases que ya tengas. Si existe un alias `claude`,
puede tener prioridad sobre la función: revísalo con `type claude` y elimínalo
manualmente si quieres usar el wrapper, o ejecuta `claude-cma` directamente.

Plugin (CLI oficial, no invoca login):

```sh
claude plugin marketplace add /ruta/absoluta/claude-multi-account
claude plugin install accounts@claude-multi-account-local
```

Desde Claude también existe `/plugin marketplace add /ruta/absoluta/claude-multi-account`
y `/plugin install accounts@claude-multi-account-local` (interfaz del gestor).
Instalar el plugin **no compila ni instala cma en PATH, no modifica shell y no
importa credenciales**. Source `./` incluye la fuente Go y Makefile de la raíz
completa. `dist/` está ignorado por Git y el binario compilado puede no viajar en
el paquete. El bridge jamás compila desde SessionStart o un skill.

Opción recomendada: compilar e instalar desde un checkout completo y, antes de
lanzar Claude, definir en el terminal:

```sh
export CMA_BINARY="$HOME/.local/share/claude-multi-account/bin/cma"
```

Debe ser ruta absoluta al ejecutable estable, no al bridge. También funciona
con plugin local `claude --plugin-dir ./plugin` y una copia aislada de `plugin/`.
No dependas de una ruta cache fija: las actualizaciones pueden cambiarla.

Alternativa: en la **raíz completa de la copia marketplace** (padre de `plugin/`),
ejecutar `make build` explícitamente para crear `dist/cma` allí. Sin `CMA_BINARY`,
el bridge solo usa ese binario. Para instalarlo estable después de compilar,
el usuario puede ejecutar `./dist/cma install` en terminal; no ocurre al cargar.

Para probar desde el checkout, después de `make build`:

```sh
./bin/claude-cma --plugin-dir /ruta/absoluta/claude-multi-account/plugin
```

Copiar solo `plugin/` no incluye runtime: requiere instalación estable y
`CMA_BINARY`. No cambiar marketplace source a `./plugin` ni duplicar código Go
bajo plugin. El source actual `./` mantiene una única fuente canónica.

## Alta, renovación y eliminación

Haz login/setup-token **oficial fuera del modelo** para la cuenta deseada:

```sh
claude setup-token
cma profiles add trabajo --label "Trabajo" --email "usuario@example.com" --organization "Empresa" --organization-id "00000000-0000-0000-0000-000000000000"
```

Sustituye ID y metadatos, y pega el token solamente en el prompt oculto del
terminal cma. Dos perfiles pueden compartir email: su ID local los distingue.
El setup-token oficial puede mostrar material sensible: no compartas su salida
ni lo pegues en el chat. La herramienta no lee `.credentials.json` ni implementa
OAuth privado. El token manda; no hay selector separado de organización.

Opciones: `--expires-at ISO` declara caducidad local conocida; si falta, se
muestra desconocida (no se infiere). `--backend auto|secret-service|file`.
`auto` selecciona Secret Service; el archivo es opt-in, no fallback silencioso.
Secret Service requiere `secret-tool` y sesión de llavero disponible en Linux.
El backend file guarda texto claro 0600: evalúa antes sus riesgos.

```sh
cma profiles list
cma profiles list --json
cma profiles renew trabajo
cma profiles remove trabajo
```

Renovar usa también entrada oculta. Eliminar requiere petición explícita del
usuario y deja las asignaciones que apunten a ese ID inválidas hasta corregirlas.
`--token-stdin` en add/renew existe para automatización externa consciente; nunca
lo uses mediante herramientas del modelo ni pongas tokens en argumentos,
variables globales, chat, JSON, historial de comandos o logs.

## Proyecto y memoria

```sh
cma project init --profile trabajo
cma project use trabajo
cma project memory per-profile
cma status --json
```

Config local `.claude/accounts.local.json`, sin secretos:

```json
{
  "version": 1,
  "profile": "trabajo",
  "allowedProfiles": ["trabajo", "personal"],
  "memory": {"mode": "per-profile"},
  "switching": {"confirmContextTransfer": true}
}
```

`allowedProfiles` es opcional y limita destinos; no es una frontera contra un
usuario local que pueda editar el archivo. El gestor intenta exclusión Git local
o avisa: comprueba `git status` antes de compartir. No cambia Git config global.
Desde subdirectorios usa raíz Git; cada worktree tiene asignación propia. Fuera
Git busca ancestro cercano con configuración, sin atravesar home/root de forma
arbitraria. Configuración inválida, perfil desconocido/expirado o no permitido:
error explícito, sin cambiar a otra cuenta.

`cma project init` sin perfil permite selección antes de lanzar. En terminal
interactiva, sin perfil, el wrapper ofrece selector, guardar la elección en el
proyecto y opción de login habitual sin gestionar; no pide tokens. Sin interacción
y sin asignación falla. `-p`/`--print` nunca abre selector, aunque haya terminal.
Los comandos administrativos `auth`, `setup-token`, `plugin`, `update`, `install`
y `doctor`, así como `--help`/`--version`, se pasan directamente al Claude oficial
sin consultar perfiles ni recuperar tokens de cma. Esto permite usarlos después
de activar la función de shell `claude`. `--bare`, background y cloud se rechazan
al lanzar conversaciones gestionadas; quedan fuera del contrato del wrapper.
`project use` afecta lanzamientos futuros, no cambia el proceso Claude actual.

`shared` mantiene memoria existente. `per-profile` separa solo memoria automática
mediante autoMemoryDirectory y overlay temporal; no migra ni copia memoria.
Historial, conversación, CLAUDE.md, código y resto de configuración permanecen
comunes. Atiende avisos de settings/managed memory; no es aislamiento completo.

## Lanzamiento y cambio

```sh
claude-cma
cma launch --profile trabajo -- --resume ID_REAL_DE_SESION
```

Todo tras `--` se pasa a Claude. El wrapper conserva stdio, señales y código de
salida. No recupera automáticamente una cuenta con cuota agotada ni falla hacia
otra identidad. Detecta conflictos de autenticación/routing sin borrar políticas.

Skills reales namespaced (manual-only):

| Skill | Equivalente CLI / efecto |
| --- | --- |
| `/accounts:status` | `cma status --json` |
| `/accounts:list` | `cma profiles list --json` |
| `/accounts:use ID session` | `cma switch ID --scope session --confirm-context-transfer`, solo después del consentimiento |
| `/accounts:use ID project` | `cma switch ID --scope project --confirm-context-transfer`, después del consentimiento; persistente |
| `/accounts:project init ID` | `cma project init --profile ID` |
| `/accounts:project use ID` | `cma project use ID` |
| `/accounts:add ID` | Instrucciones de `claude setup-token` y `cma profiles add`; importación solo en terminal |
| `/accounts:memory shared` | `cma project memory shared` |
| `/accounts:memory per-profile` | `cma project memory per-profile` |

Para cambio gestionado el hook SessionStart debe haber registrado el ID real en
esta ejecución. Elige scope y **autoriza transferir contexto**. Entonces:

```sh
cma switch personal --scope session --confirm-context-transfer
```

Dentro del modelo lo solicita `/accounts:use`. Después escribe `/exit`: el
wrapper detecta la solicitud al salir normalmente y reanuda con `--resume` y el
ID exacto registrado. No mata la TUI ni inyecta auth en caliente. Salida con error
no reinicia; otras sesiones no cambian. `scope project` persiste la asignación;
`scope session` no. Cambio de cwd/proyecto requiere detección/aviso, no un cambio
silencioso de auth. Sin wrapper el hook avisa, y switch no está disponible.

**Cuota agotada:** un skill puede necesitar al modelo y no ejecutarse. Usa la
CLI desde una terminal que herede el entorno de ESA ejecución (`CMA_LAUNCH_ID`,
`CMA_RUN_DIR`, `CMA_PROJECT_ROOT`, `CMA_ACTIVE_PROFILE`, `CMA_MANAGED`); no los
inventes ni reutilices desde otra sesión. En una terminal independiente sin ese
entorno, sal normalmente y lanza `cma launch --profile personal -- --resume
ID_REAL_DE_SESION` solo con consentimiento sobre transferencia de contexto.
Si no conoces el ID real, inicia una nueva sesión: nunca adivinarlo ni usar
`--continue` como sustituto del handoff exacto.

## Desinstalación

Acciones deliberadas del usuario en terminal:

```sh
cma uninstall
# Si instalaste en otro destino:
cma uninstall --target /ruta/absoluta/cma-runtime
claude plugin uninstall accounts@claude-multi-account-local
claude plugin marketplace remove claude-multi-account-local
```

Revisa el informe: elimina únicamente instalación/shims/integración gestionada,
no el binario oficial ni los archivos ajenos del shell. No elimina perfiles,
secretos, historial o memoria automáticamente. Para retirar un secreto usa
`cma profiles remove ID` deliberadamente antes de desinstalar. No borrar
credenciales oficiales ni datos de terceros.

## Verificación pendiente

Tests locales con Claude/backends falsos no verifican auth real. Para una prueba
manual autorizada: generar/importar tokens fuera del modelo, iniciar wrapper con
plugin, comprobar cuenta/recursos mediante interfaz oficial, consentir el cambio,
salir con `/exit` y comprobar ID exacto, perfil y persistencia deseada. Usar
contenido de prueba no sensible. **Reanudación real entre cuentas, disponibilidad
de recursos de organización y compatibilidad con cada versión de Claude no
verificadas aquí**; no afirmar éxito antes de esa prueba.
