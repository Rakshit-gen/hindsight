VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build install test bench web serve
build:
	go build -trimpath -ldflags '$(LDFLAGS)' -o bin/hindsight ./cmd/hindsight

install:
	go install -trimpath -ldflags '$(LDFLAGS)' ./cmd/hindsight

test:
	go test ./...

bench: build
	./bin/hindsight bench

web:
	GOOS=js GOARCH=wasm go build -trimpath -ldflags '-s -w' -o web/hindsight.wasm ./cmd/wasm
	cp "$$(go env GOROOT)/lib/wasm/wasm_exec.js" web/

serve: web
	cd web && python3 -m http.server 8000
