---
name: add
description: Explicar alta segura desde una terminal, nunca importar tokens en el chat.
argument-hint: "<ID>"
disable-model-invocation: true
---

No ejecutes importaciones ni setup-token mediante herramientas del modelo. No
pidas tokens en el chat, no los recibas en argumentos, no construyas tuberías
con secretos y no leas `.credentials.json`. Si el usuario pega un token,
no lo reproduzcas ni lo guardes; recomienda revocarlo/renovarlo oficialmente.

Indica que en OTRA terminal local el usuario ejecuta el flujo oficial:

```sh
claude setup-token
cma profiles add personal --label "Personal" --email "usuario@example.com" --organization "Mi organización" --organization-id "00000000-0000-0000-0000-000000000000"
```

El usuario sustituye los metadatos y el ID; la importación solicita el token con
entrada oculta en terminal Linux. Son metadatos declarados, no identidad verificada
ni selección independiente de organización. El backend auto utiliza Secret Service
con secret-tool disponible; `--backend file` es opt-in explícito con riesgo de
texto claro 0600. Para renovar: `cma profiles renew personal` desde terminal,
también con entrada oculta. No uses `--token-stdin` mediante el modelo; es solo
para automatización externa consciente, sin tokens en historial ni logs.

Si falta cma, guía al usuario sin ejecutar la instalación: instalar el plugin NO
compila/instala CLI ni modifica shell. En OTRA terminal Linux, desde la raíz del
checkout completo con Go >= 1.23, puede ejecutar `make build` y `make install`
explícitamente, o `./dist/cma install` después de compilar. El ejecutable estable
por defecto es `~/.local/share/claude-multi-account/bin/cma`; puede usar su ruta
absoluta si no está en PATH. La integración de shell requiere `install --shell`
explícito y no es necesaria.

El bridge necesita `CMA_BINARY` absoluto al ejecutable estable, o `make build`
explícito en la raíz completa de la copia del plugin para crear `dist/cma`.
Una copia aislada de plugin/ requiere CMA_BINARY. El marketplace incluye fuente
completa, pero dist/ está ignorado y puede no incluir binario. Hooks nunca compilan
ni instalan. No prometer autoinstalación ni acceso a credenciales tras `/plugin install`.
