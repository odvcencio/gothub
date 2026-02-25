.PHONY: all build wasm wasm-core wasm-full frontend embed clean

all: build

WASM_GO_TAGS ?= grammar_set_core
WASM_GRAMMAR_MODE ?= default
WASM_EXTRA_GO_TAGS ?=
WASM_LDFLAGS ?= -s -w
WASM_TRIMPATH ?= -trimpath

# Build WASM module
wasm:
	@set -eu; \
	tags="$(strip $(WASM_GO_TAGS))"; \
	mode="$(WASM_GRAMMAR_MODE)"; \
	case "$$mode" in \
		default) ;; \
		core) \
			grammars_dir="$$(go list -f '{{.Dir}}' github.com/odvcencio/gotreesitter/grammars 2>/dev/null || true)"; \
			if [ -n "$$grammars_dir" ] && [ -f "$$grammars_dir/language_set_core.go" ]; then \
				tags="grammar_set_core"; \
			else \
				echo "warning: grammar_set_core is unavailable; building with the full grammar set" >&2; \
				tags=""; \
			fi ;; \
		full) tags="";; \
		*) echo "error: WASM_GRAMMAR_MODE must be one of default, core, full (got: $$mode)" >&2; exit 2;; \
	esac; \
	extra_tags="$(strip $(WASM_EXTRA_GO_TAGS))"; \
	if [ -n "$$extra_tags" ]; then \
		if [ -n "$$tags" ]; then tags="$$tags $$extra_tags"; else tags="$$extra_tags"; fi; \
	fi; \
	echo "WASM grammar mode: $$mode"; \
	echo "WASM tags: $${tags:-<none>}"; \
	mkdir -p frontend/dist; \
	if [ -n "$$tags" ]; then \
		GOOS=js GOARCH=wasm go build -tags "$$tags" -ldflags="$(WASM_LDFLAGS)" $(WASM_TRIMPATH) -o frontend/dist/gothub.wasm ./wasm/; \
	else \
		GOOS=js GOARCH=wasm go build -ldflags="$(WASM_LDFLAGS)" $(WASM_TRIMPATH) -o frontend/dist/gothub.wasm ./wasm/; \
	fi; \
	cp "$$(go env GOROOT)/lib/wasm/wasm_exec.js" frontend/dist/wasm_exec.js

# Build WASM module with compile-time core grammar set when supported.
wasm-core:
	@$(MAKE) wasm WASM_GRAMMAR_MODE=core

# Build WASM module with full grammar set.
wasm-full:
	@$(MAKE) wasm WASM_GRAMMAR_MODE=full

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
