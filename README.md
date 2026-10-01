# Claude Multi Account

Usa un perfil de Claude Code distinto según el proyecto, sin cambiar manualmente de login ni mantener una carpeta de configuración de Claude por cuenta.

**Solo Linux.** Implementado en Go con biblioteca estándar. Un único binario para gestionar perfiles, lanzar Claude y cambiar de cuenta; sin Python, Node ni Bun.

> Proyecto independiente, no afiliado a Anthropic. La autenticación y la reanudación entre cuentas reales requieren una prueba manual: los tests automatizados utilizan ejecutables y credenciales ficticias.

## Qué permite

- Registrar perfiles personales y de empresa reutilizables en todos tus proyectos.
- Usar dos perfiles con el mismo email pero tokens de organizaciones diferentes, o con usuarios distintos.
- Guardar el perfil de cada proyecto en `.claude/accounts.local.json`, sin secretos.
- Seguir escribiendo `claude` mediante integración de shell opcional, o usar `claude-cma` explícitamente.
- Listar perfiles y solicitar cambios mediante comandos slash del plugin `accounts`.
- Cambiar solo la sesión actual o actualizar el perfil predeterminado del proyecto.
- Compartir memoria automática entre perfiles o separarla por perfil.

No hay rotación automática al agotar la cuota ni cambio de autenticación en caliente.

## Cómo funciona

```text
claude-cma / claude con integración de shell
    │
    ├─ Localiza el proyecto y lee .claude/accounts.local.json
    ├─ Resuelve el perfil en el registro local del usuario
    ├─ Recupera su token del llavero
    ├─ Comprueba conflictos de autenticación y configuración
    └─ Arranca el Claude oficial con CLAUDE_CODE_OAUTH_TOKEN
           │
           └─ Plugin accounts: consultar, configurar y solicitar cambios
```

El wrapper conserva el directorio de configuración de Claude (`CLAUDE_CONFIG_DIR`, si lo tienes definido). No intercambia ni lee `.credentials.json`, no modifica tu login habitual y no exporta tokens globalmente al shell.

**El token determina la cuenta y organización.** Los emails y UUID que introduces son metadatos declarados, no una identidad verificada ni un selector que pueda cambiar la organización del token.

Si un proyecto no tiene perfil, el arranque interactivo permite elegir uno y decidir si guardarlo. También puedes continuar con el login habitual sin gestionar esa sesión. En modo no interactivo (`-p`/`--print`), una asignación ausente produce un error, no un selector.

## Requisitos

- Linux; WSL también si ejecutas todo dentro del entorno Linux.
- Claude Code oficial instalado y accesible en `PATH`.
- Go >= 1.23 y `make` para compilar. El binario resultante no necesita Go para ejecutarse.
- Para el backend predeterminado: `secret-tool` y un servicio Secret Service disponible y desbloqueado.
- Bash o Zsh solo si quieres integrar el comando `claude`.

Sin llavero puedes elegir explícitamente `--backend file`: guarda el token en texto claro con permisos `0600`. No es un fallback automático.

## 1. Compilar e instalar el CLI

Desde la raíz del repositorio:

```sh
make build
./dist/cma install
export PATH="$HOME/.local/share/claude-multi-account/bin:$PATH"
```

La instalación copia el binario a `~/.local/share/claude-multi-account/bin/cma` y crea el shim `claude-cma`. No modifica el shell por defecto. El `export PATH` anterior solo afecta a la terminal actual.

Puedes probar sin instalar:

```sh
./bin/cma --help
./bin/claude-cma
```

Los shims del checkout necesitan que hayas ejecutado `make build`.

### Opcional: seguir escribiendo `claude`

```sh
cma install --shell
```

Añade un bloque gestionado a `.bashrc` o `.zshrc` con una función `claude` que llama al wrapper. Abre una terminal nueva para cargarlo. El binario oficial no se reemplaza.

Si ya tienes un alias `claude`, puede tener prioridad: compruébalo con `type claude` y decide si retirarlo manualmente, o utiliza `claude-cma`.

Los comandos administrativos como `claude setup-token`, `claude auth` y `claude plugin` se pasan al Claude oficial sin recuperar tokens del gestor.

## 2. Registrar tus perfiles

Genera un token para la cuenta deseada con el flujo oficial, **en tu terminal, fuera del chat**:

```sh
claude setup-token
```

Después registra el perfil. Sustituye los metadatos de ejemplo por los tuyos:

```sh
cma profiles add personal \
  --label "Personal" \
  --email "usuario@example.com" \
  --organization "Mi organización personal" \
  --organization-id "00000000-0000-0000-0000-000000000001"
```

`cma` solicita el token con entrada oculta. Nunca lo pases como argumento ni lo pegues en un comando slash.

Repite el flujo oficial para la organización de empresa y registra otro perfil:

```sh
cma profiles add empresa \
  --label "Empresa" \
  --email "usuario@example.com" \
  --organization "Mi empresa" \
  --organization-id "00000000-0000-0000-0000-000000000002"
```

Los perfiles pueden compartir email. Asegúrate de que cada token corresponde al contexto deseado: la herramienta no descubre organizaciones ni verifica esos metadatos.

```sh
cma profiles list
cma profiles renew empresa
cma profiles remove empresa
```

La renovación vuelve a pedir el token oculto. Puedes declarar una caducidad conocida al añadir un perfil con `--expires-at`; si no, aparece como desconocida. Eliminar un perfil no reconfigura los proyectos que lo referencian.

## 3. Instalar el plugin

```sh
claude plugin marketplace add /ruta/absoluta/claude-multi-account
claude plugin install accounts@claude-multi-account-local
export CMA_BINARY="$HOME/.local/share/claude-multi-account/bin/cma"
```

Define `CMA_BINARY` antes de arrancar Claude con el plugin. Debe apuntar al binario estable, no a `plugin/bin/cma`.

**Instalar el plugin no compila ni instala el CLI.** El marketplace incluye la fuente del repositorio, pero `dist/` no se versiona. Los hooks nunca compilan, descargan ni instalan nada.

Para desarrollo local, después de compilar:

```sh
./bin/claude-cma --plugin-dir /ruta/absoluta/claude-multi-account/plugin
```

Sin `CMA_BINARY`, el bridge busca únicamente `dist/cma` en la raíz completa del plugin/checkout. Una copia aislada de `plugin/` requiere el binario estable.

## 4. Asignar un perfil al proyecto

Desde el directorio del proyecto:

```sh
cma project init --profile empresa
claude-cma
```

Si activaste la integración de shell, puedes arrancar con `claude` en lugar de `claude-cma`.

La configuración es local:

```json
{
  "version": 1,
  "profile": "empresa",
  "allowedProfiles": ["empresa", "personal"],
  "memory": {
    "mode": "shared"
  },
  "switching": {
    "confirmContextTransfer": true
  }
}
```

- `profile`: perfil predeterminado.
- `allowedProfiles`: opcional; restringe los perfiles seleccionables en ese proyecto.
- `memory.mode`: `shared` o `per-profile`.
- Los cambios de sesión requieren consentimiento explícito antes de transferir contexto.

Mantén `.claude/accounts.local.json` fuera de Git; por ejemplo, mediante la exclusión local del repositorio. El CLI avisa, pero no modifica tu configuración global de Git.

Desde subdirectorios se utiliza la raíz Git. Cada worktree tiene su propia asignación. Fuera de Git, se busca una configuración cercana en los directorios ascendentes.

```sh
cma project use personal
cma status
```

`project use` afecta a futuros arranques, no a una sesión ya abierta.

## 5. Consultar y cambiar desde Claude

| Comando slash | Acción |
| --- | --- |
| `/accounts:status` | Perfil activo, asignación del proyecto y estado de memoria |
| `/accounts:list` | Perfiles disponibles, sin secretos |
| `/accounts:use` | Listar perfiles y elegir destino y alcance |
| `/accounts:use personal session` | Cambiar solo esta sesión |
| `/accounts:use empresa project` | Guardar el perfil del proyecto y solicitar el cambio |
| `/accounts:project init empresa` | Inicializar la asignación del proyecto |
| `/accounts:add` | Instrucciones para registrar un perfil en terminal |
| `/accounts:memory per-profile` | Separar la memoria automática |

Todos los skills son manuales: Claude no debe elegir una cuenta por su cuenta.

### Qué ocurre al cambiar

1. Eliges perfil y alcance (`session` o `project`).
2. Confirmas que el contexto de la conversación puede enviarse bajo el perfil destino.
3. El plugin registra la solicitud asociada al ID exacto de esa sesión.
4. Sales normalmente con `/exit`.
5. El wrapper relanza Claude con el nuevo token y `--resume` del ID registrado.

No mata procesos ni abre otra interfaz dentro de la actual. Una salida con error no provoca reinicio automático. Las demás sesiones abiertas conservan sus perfiles.

La operación también existe en el CLI, desde el entorno de una sesión gestionada:

```sh
cma switch personal --scope session --confirm-context-transfer
```

Los comandos slash pueden necesitar al modelo y fallar si la cuota está agotada. Para recuperación desde otra terminal, consulta [la guía de uso](docs/usage.md#lanzamiento-y-cambio): no inventes IDs ni reutilices el estado de otra sesión.

## Memoria compartida o por perfil

```sh
cma project memory shared
cma project memory per-profile
```

- **`shared`**: mantiene la memoria automática existente del proyecto al cambiar de perfil.
- **`per-profile`**: configura una carpeta de memoria automática distinta por proyecto y perfil mediante un overlay temporal de settings.

No copia ni migra automáticamente memorias existentes. No necesita enlaces simbólicos para el caso habitual.

**Separar memoria no aísla conversaciones, código ni `CLAUDE.md`.** El historial y la configuración de Claude permanecen comunes. Reanudar una conversación con otra cuenta puede enviar contenido empresarial bajo una cuenta personal: confirma que está permitido.

## Seguridad y límites

- Tokens en Secret Service por defecto; archivo `0600` únicamente con elección explícita.
- Tokens fuera de argumentos, configuración del proyecto y respuestas del plugin.
- El token se inyecta en el entorno del proceso Claude; esto no protege contra código ejecutado con tu mismo usuario del sistema.
- Conflictos de API keys, proveedores o routing se rechazan; no se eliminan políticas ni se cambia silenciosamente a otra cuenta.
- No se implementa OAuth privado ni se leen credenciales del login oficial.
- Solo terminal local Linux: VS Code, Claude Desktop, cloud y background quedan fuera del alcance.
- Los tokens de `setup-token` no equivalen a un login completo; la sincronización de skills de claude.ai no está disponible y los recursos organizativos necesitan comprobación.
- La reanudación con cuentas reales y compatibilidad con cada versión de Claude requieren prueba manual autorizada.

Más detalle en [seguridad](docs/security.md) y [arquitectura](docs/architecture.md).

## Desinstalar

```sh
cma uninstall
claude plugin uninstall accounts@claude-multi-account-local
claude plugin marketplace remove claude-multi-account-local
```

La desinstalación retira archivos e integración gestionados, preservando archivos modificados o ajenos. No borra automáticamente perfiles, tokens, memoria ni historial. Si quieres retirar un token, elimina deliberadamente su perfil antes de desinstalar.

## Desarrollo

```sh
go test ./...
go test -race ./...
go vet ./...
make build
sh -n bin/cma && sh -n bin/claude-cma && sh -n plugin/bin/cma
claude plugin validate ./plugin
claude plugin validate ./.claude-plugin/marketplace.json
```

Los tests usan temporales, ejecutables y secretos ficticios, sin red ni login. Consulta [AGENTS.md](AGENTS.md) para las reglas de desarrollo y [docs/usage.md](docs/usage.md) para el contrato CLI completo.

## Licencia

Distribuido bajo la [licencia MIT](LICENSE).
