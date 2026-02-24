.PHONY: all build wasm frontend embed clean

all: build

# Build WASM module
wasm:
	GOOS=js GOARCH=wasm go build -o frontend/dist/gothub.wasm ./wasm/
	cp "$$(go env GOROOT)/lib/wasm/wasm_exec.js" frontend/dist/wasm_exec.js

# Build frontend
frontend:
	cd frontend && npm run build

# Copy frontend dist into Go embed directory
embed: frontend wasm
	rm -rf internal/web/dist
	cp -r frontend/dist internal/web/dist

# Build the gothub binary (requires embed step first)
build: embed
	go build -o gothub ./cmd/gothub

clean:
	rm -rf gothub frontend/dist internal/web/dist
