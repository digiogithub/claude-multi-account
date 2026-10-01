# Desarrollo

SOLO Linux, Go >= 1.23, biblioteca estándar; tests Go con temporales aislados.
No acceder a credenciales reales, ejecutar login, instalar en la máquina, modificar
shell ni hacer commits sin autorización. Nunca imprimir tokens ni leer
`.credentials.json`. Los tests usan backends y binarios falsos, sin red.

Desde la raíz del repositorio:

```sh
go test ./...
go test -race ./...
go vet ./...
make build
sh -n bin/cma && sh -n bin/claude-cma && sh -n plugin/bin/cma
claude plugin validate ./plugin
claude plugin validate ./.claude-plugin/marketplace.json
```

La validación de Claude es local; omitirla indicando el motivo si no está disponible.
No hay linter/typechecker externo obligatorio; usar `gofmt` y `go vet`.
No añadir dependencias sin necesidad. No ejecutar `make install` durante pruebas.

`make build` genera `dist/cma` para Linux. `bin/cma` y `bin/claude-cma` son shims
POSIX que ejecutan ese único binario; el segundo usa `cma launch --`.
`plugin/bin/cma` usa un ejecutable estable mediante `CMA_BINARY` absoluto, o
`dist/cma` de la raíz completa. Nunca compila ni instala desde hooks.
El marketplace empaqueta la raíz completa: no cambiar source a `./plugin` ni
duplicar runtime. `dist/` no se versiona: el usuario debe compilar explícitamente
o configurar su instalación estable; instalar plugin no instala el CLI.
`make install` es solo una acción explícita humana y ejecuta `./dist/cma install`;
destino estable por defecto `~/.local/share/claude-multi-account/bin/cma`.
La integración de shell exige `cma install --shell` explícito.

Los skills son manual-only y no reciben secretos. El formato JSON version 1 es
compatible con registros previos; solo backends Linux secret-service/file.
Los documentos previos en `docs/claude-multi-account/` son análisis/planes
históricos: conservarlos sin cambios.
