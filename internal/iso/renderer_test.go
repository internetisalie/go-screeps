package iso

import (
	"strings"
	"testing"

	"go-screeps/internal/screeps"
)

func TestHashWorldRoomsDetectsTerrainChange(t *testing.T) {
	terrA := strings.Repeat("0", GridSize*GridSize)
	terrB := "1" + strings.Repeat("0", GridSize*GridSize-1)

	snapA := &screeps.RoomSnapshot{Room: "W9N7", Terrain: terrA}
	snapB := &screeps.RoomSnapshot{Room: "W9N7", Terrain: terrB}

	hA := hashWorldRooms(buildWorldRooms(snapA, nil, 0))
	hA2 := hashWorldRooms(buildWorldRooms(snapA, nil, 0))
	hB := hashWorldRooms(buildWorldRooms(snapB, nil, 0))

	if hA != hA2 {
		t.Fatalf("expected deterministic hash for identical terrain, got %x vs %x", hA, hA2)
	}
	if hA == hB {
		t.Fatalf("expected different hash when terrain bytes change (both %x)", hA)
	}
}

func TestPainterClearUnitTracking(t *testing.T) {
	p := &Painter{
		creepFacing:  map[string]float64{"c1": 1.5},
		creepLastPos: map[string][2]float32{"c1": {10, 20}},
	}
	p.ClearUnitTracking()
	if len(p.creepFacing) != 0 || len(p.creepLastPos) != 0 {
		t.Fatalf("expected empty tracking maps after ClearUnitTracking, got facing=%d lastPos=%d",
			len(p.creepFacing), len(p.creepLastPos))
	}
}
