package shaders

import (
	_ "embed"

	"github.com/bankai-tensei/gowaku/paint"
)

//go:embed objects.spv
var objectsSPIRV []byte

// Objects is the GPU 3D raymarched SDF shader for Screeps structures and creeps.
var Objects = paint.NewShader(paint.Shader{
	Name:  "screeps_objects",
	SPIRV: objectsSPIRV,
})
