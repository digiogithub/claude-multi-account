# Seguridad y límites

## Frontera de confianza

Modo A utiliza exclusivamente `CLAUDE_CODE_OAUTH_TOKEN` en el subprocess Claude;
conserva el mismo `CLAUDE_CONFIG_DIR`. No lee/intercambia `.credentials.json`,
no implementa OAuth privado ni modifica credenciales del login oficial. No usa
la variable no documentada `CLAUDE_CODE_ORGANIZATION_UUID` como selector. El token
decide identidad; email, organización e ID de organización son metadatos
**unverified** declarados por el usuario, no comprobados a partir del token.
No descubrimiento automático de organizaciones, ni sincronización con claude.ai.

Esto no aísla completamente cuentas. Son comunes historial, transcripciones,
CLAUDE.md, código, archivos de proyecto y configuración habitual. `per-profile`
separa únicamente memoria automática mediante autoMemoryDirectory; no migra
memoria ni borra contenido previo. Reanudar entre perfiles transfiere conversación
y contexto potencialmente sensibles: exige consentimiento explícito. Incluso
una ruta separada de memoria no impide al modelo acceder a archivos autorizados.

Usuarios/procesos con la misma identidad del sistema, root, malware o backups
pueden acceder a procesos/datos locales. Permisos 0700/0600 y llavero reducen
exposición, no protegen frente a esos adversarios. El gestor no es un sandbox
ni una frontera de organización. `allowedProfiles` es política local editable,
no autorización de servidor.

## Tokens y almacenamiento

- Flujo oficial `claude setup-token` fuera del modelo; importación en terminal
  Linux con entrada oculta (echo desactivado y restaurado al terminar).
  Nunca pedir token en chat ni argumentos; tampoco stdin manejado por el modelo.
  `--token-stdin` queda para automatización externa deliberada.
- `auto`: Secret Service mediante `secret-tool` en Linux. Sin llavero disponible,
  fallar y explicar; no escribir archivo como fallback silencioso.
- `--backend file`: opt-in explícito, texto claro 0600 bajo directorio privado;
  riesgo ante backup, malware y mismo UID. Sin cifrado equivalente a llavero.
- Registro solo metadatos y referencias de secretos, no tokens. Los comandos
  list/status/JSON y registros de ejecución no deben incluirlos.
- No export global, token en settings, logs ni argumentos al proceso Claude.
  El entorno del hijo contiene el token: no lo vuelques ni captures con `env`.
- La caducidad local conocida bloquea launch hasta renovación; desconocida se
  muestra como tal y no prueba validez. El gestor no verifica el token por red.

No habilitar shell trace (`set -x`) ni logs de entorno durante flujos secretos.
Los comandos externos del llavero están dentro de la confianza del host; un
backend comprometido invalida la protección. Si se comparte un token por error,
no reproducirlo: revocar/renovar mediante herramientas oficiales.

## Autenticación y políticas

Antes de launch se detectan conflictos de API keys/auth token, apiKeyHelper,
proveedores cloud/gateway y endpoints personalizados en entorno/settings
relevantes. El gestor falla sin eliminar esas políticas y sin elegir otro
perfil. No es una verificación exhaustiva de infraestructura externa ni una
forma de saltarse administración de Claude. No hay rotación automática de
cuotas, autenticación en caliente ni fallback silencioso de cuenta.

## Solicitudes de cambio

Cada wrapper crea ejecución independiente con nonce, directorio 0700 y JSON
0600. SessionStart registra ID/cwd con validación; switch valida ejecución,
perfil permitido, proyecto, sesión registrada y consentimiento. Una solicitud
para otra ejecución o manipulada no debe permitir handoff. No hay archivo global
que cambie todas las sesiones. No se mata la TUI: se procesa tras salida normal;
error de Claude no provoca reinicio automático. Reanuda ID exacto, no `--continue`.
Un usuario del mismo UID puede manipular archivos; la validación reduce errores
y falsificaciones casuales, no sustituye una frontera OS.

## Plugin e instalación

Todos los skills `accounts` son `disable-model-invocation: true`; esto controla
invocación, no convierte el texto del skill en un sandbox. Importación/renovación
son exclusivamente acciones humanas en terminal. SessionStart lee solo JSON de
la sesión desde stdin y llama al core; no recibe tokens. El bridge no instala
nada por cargarlo. `CMA_BINARY` absoluto es ejecutable de confianza elegido por
el usuario; no apuntar a código no confiable.

El marketplace incluye la raíz completa como fuente Go canónica. Revisar
contenido y versión antes de compilar o instalar. `dist/` está ignorado y el
binario puede no incluirse: usar `make build` explícito en la raíz completa del
plugin o `CMA_BINARY` a una instalación estable. Hooks no compilan ni instalan.
No publicar datos locales junto al checkout; `.gitignore` no impide copiar
archivos ajenos en un paquete manual. Instalar plugin no instala CLI/shims ni
cambia shell. Instalación estable y `--shell` son explícitos. Config local
`.claude/accounts.local.json` se excluye localmente de Git o se advierte;
comprobar siempre status antes de publicar.

Formato JSON existente preservado; solo secretos `secret-service` y `file`
admitidos. Perfiles de otros sistemas no se convierten ni se recuperan sus
secretos automáticamente; conservar datos previos y decidir una importación
Linux explícita en terminal.

## Fuera de alcance / pruebas

Solo terminal local Linux (WSL con entorno Linux); no VSCode, Desktop ni background. No promesa
de sincronizar claude.ai, recursos organizativos o aislamiento completo. Tests
con binarios/backends falsos no verifican reanudación real intercuentas ni auth.
Esas pruebas requieren permiso, login del usuario y contenido no sensible; no
se realizaron aquí. No acceder a credenciales reales ni instalar/modificar shell
para ejecutar las pruebas de desarrollo.
