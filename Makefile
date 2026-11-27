SHELL := bash
DXC ?= $(shell command -v dxc 2>/dev/null || echo $(HOME)/.local/bin/dxc)
SPIRV_CROSS ?= $(shell command -v spirv-cross 2>/dev/null || echo $(HOME)/.local/bin/spirv-cross)
GOWAKU_DIR := $(shell go list -m -f '{{.Dir}}' github.com/bankai-tensei/gowaku)
HLSL_INC := $(GOWAKU_DIR)/internal/shadertool/hlsl
SHADER_DIR := internal/iso/shaders
SHADERS := terrain objects

.PHONY: all build test bench shaders snapshot clean

all: build

build:
	go build -o go-screeps .
	go build -o sim-bench ./cmd/sim-bench

test:
	go vet ./...
	go test -count=1 ./...

bench:
	go run ./cmd/sim-bench -ticks 4000 -report 1000

shaders: $(foreach s,$(SHADERS),$(SHADER_DIR)/$(s).spv $(SHADER_DIR)/$(s).metal)

$(SHADER_DIR)/%.spv: $(SHADER_DIR)/%.hlsl
	@set -euo pipefail; \
	tmpdir=$$(mktemp -d); \
	trap 'rm -rf "$$tmpdir"' EXIT; \
	cat "$(HLSL_INC)/prelude.hlsli" "$(HLSL_INC)/blend.hlsli" "$<" "$(HLSL_INC)/epilogue.hlsli" > "$$tmpdir/shader.hlsl"; \
	"$(DXC)" -spirv -fspv-target-env=vulkan1.0 -T ps_6_0 -E main -O3 \
		-fvk-bind-register b0 0 0 0 \
		-fvk-bind-register s0 0 1 0 \
		-fvk-bind-register t0 0 0 1 \
		-fvk-bind-register t1 0 0 2 \
		-fvk-bind-register t2 0 0 3 \
		-fvk-bind-register t3 0 0 4 \
		-fvk-bind-register t4 0 0 5 \
		"$$tmpdir/shader.hlsl" -Fo "$@"

$(SHADER_DIR)/%.metal: $(SHADER_DIR)/%.hlsl
	@set -euo pipefail; \
	tmpdir=$$(mktemp -d); \
	trap 'rm -rf "$$tmpdir"' EXIT; \
	cat "$(HLSL_INC)/prelude.hlsli" "$(HLSL_INC)/blend.hlsli" "$<" "$(HLSL_INC)/epilogue.hlsli" > "$$tmpdir/shader.hlsl"; \
	"$(DXC)" -spirv -fspv-target-env=vulkan1.0 -T ps_6_0 -E main -O3 \
		-fvk-bind-register b0 0 0 0 \
		-fvk-bind-register t0 0 0 1 \
		-fvk-bind-register t1 0 1 2 \
		-fvk-bind-register t3 0 3 3 \
		"$$tmpdir/shader.hlsl" -Fo "$$tmpdir/shader.spv"; \
	"$(SPIRV_CROSS)" --msl --msl-version 20100 --msl-decoration-binding --output "$@" "$$tmpdir/shader.spv"

snapshot:
	goreleaser release --snapshot --clean

clean:
	rm -rf go-screeps sim-bench dist
