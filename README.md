# go-screeps

Pure-Go, zero-CGO Screeps simulation engine, parallel Lua 5.4 (`arnodel/golua`) autonomous colony AI runtime, and Vulkan/Metal GPU-accelerated 3D isometric renderer (built with `gowaku`).

## Features
- **Embedded Go Screeps Engine** (`internal/engine`): tick-based world simulation with harvesting, building, upgrading, tower combat, rampart damage absorption, safe mode, cross-room exits, passive spawn regen, and JSON state persistence.
- **Parallel Lua 5.4 Bot Runtime** (`internal/lua`, `bots/`): runs multiple isolated `arnodel/golua` bot VMs concurrently (`starter`/Hivemind and `raider`) with native hard-limit CPU sandboxing, coordinated multi-unit assault waves, remote mining, blueprint planning, persistent `Memory`, and a Lunar/LuaLS type definition package (`bots/types/`).
- **Vulkan (Linux) & Metal (macOS) GPU Isometric Renderer** (`internal/iso`, `internal/iso/shaders/`):
  - Single-source HLSL shaders compiled to Vulkan SPIR-V (`.spv`) and macOS Metal MSL (`.metal`) via `make shaders`, embedded per-OS via Go build tags.
  - 5×5 multi-room 2D DDA terrain heightfield shader (`terrain.hlsl`) with continuous 360° yaw orbit and infinite seamless scrolling.
  - 3D raymarched SDF models (`objects.hlsl`) with ACES filmic lighting, analytic 1-pixel silhouette AA, translucent glass holo-panels, and smooth 3D unit/tower facing.
  - 4-layer curb-lit sci-fi roads with dashed centerlines and junction diamonds.
- **Headless Battle Benchmark** (`cmd/sim-bench`): cross-platform CLI runner (~230+ ticks/sec) for testing bot matchups.

## Quick Start
```bash
make build
./go-screeps -local -room W9N7
```

Run tests, headless benchmark, or recompile shaders:
```bash
make test
make bench
make shaders
```
