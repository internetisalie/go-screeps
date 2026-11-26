//go:build darwin

package shaders

import (
	_ "embed"

	"github.com/bankai-tensei/gowaku/paint"
)

//go:embed objects.metal
var objectsMSL []byte

// Objects is the GPU-accelerated 3D raymarched SDF shader for Screeps structures and creeps.
var Objects = paint.NewShader(paint.Shader{
	Name: "screeps_objects",
	MSL:  objectsMSL,
})
