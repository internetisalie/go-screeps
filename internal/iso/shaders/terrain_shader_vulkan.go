//go:build !darwin

package shaders

import (
	_ "embed"

	"github.com/bankai-tensei/gowaku/paint"
)

//go:embed terrain.spv
var terrainSPIRV []byte

// Terrain is the GPU-accelerated multi-room isometric Screeps terrain & wall shader.
var Terrain = paint.NewShader(paint.Shader{
	Name:  "screeps_terrain",
	SPIRV: terrainSPIRV,
})
