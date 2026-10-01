.PHONY: build install test check

# No compilation in hooks: users explicitly build on a Linux host.
build:
	@test "$$(uname -s)" = Linux || { printf '%s\n' 'cma: build supports Linux only.' >&2; exit 2; }
	@test "$$(go env GOOS)" = linux || { printf '%s\n' 'cma: GOOS must be linux.' >&2; exit 2; }
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux go build -trimpath -o dist/cma ./cmd/cma
	@go version -m dist/cma | awk '/GOOS=linux/ { found=1 } END { exit !found }'

# Copies the compiled executable; shell integration remains opt-in.
install: build
	./dist/cma install

test:
	go test ./...

check:
	go vet ./...
	sh -n bin/cma
	sh -n bin/claude-cma
	sh -n plugin/bin/cma
