package iso

import (
	"slices"
	"strings"
	"testing"

	"go-screeps/internal/screeps"

	"github.com/bankai-tensei/gowaku/geom"
)

func TestParseAndFormatRoomCoords(t *testing.T) {
	cases := []struct {
		name string
		wx   int
		wy   int
	}{
		{"W9N7", -10, -8},
		{"W0N0", -1, -1},
		{"E0S0", 0, 0},
		{"E12S5", 12, 5},
		{"W0S0", -1, 0},
		{"E0N0", 0, -1},
	}

	for _, tc := range cases {
		wx, wy, ok := ParseRoomCoords(tc.name)
		if !ok {
			t.Fatalf("ParseRoomCoords(%q) returned ok=false, want true", tc.name)
		}
		if wx != tc.wx || wy != tc.wy {
			t.Errorf("ParseRoomCoords(%q) = (%d, %d), want (%d, %d)", tc.name, wx, wy, tc.wx, tc.wy)
		}
		got := FormatRoomCoords(wx, wy)
		if got != tc.name {
			t.Errorf("FormatRoomCoords(%d, %d) = %q, want %q", wx, wy, got, tc.name)
		}
	}

	// Lowercase and whitespace should also parse cleanly.
	wx, wy, ok := ParseRoomCoords("  w9n7 ")
	if !ok || wx != -10 || wy != -8 {
		t.Errorf("ParseRoomCoords(\"  w9n7 \") = (%d, %d, %v), want (-10, -8, true)", wx, wy, ok)
	}

	// Invalid room names.
	for _, bad := range []string{"", "sim", "W9", "N7", "W-1N0", "X1Y2", "W9N7Z", "WN"} {
		if _, _, ok := ParseRoomCoords(bad); ok {
			t.Errorf("ParseRoomCoords(%q) returned ok=true, want false", bad)
		}
	}
}

func TestNeighborRoomNames(t *testing.T) {
	got := NeighborRoomNames("W9N7", 1)
	want := []string{
		"W10N8", "W9N8", "W8N8",
		"W10N7", "W9N7", "W8N7",
		"W10N6", "W9N6", "W8N6",
	}
	if len(got) != len(want) {
		t.Fatalf("NeighborRoomNames(\"W9N7\", 1) returned %d rooms (%v), want %d", len(got), got, len(want))
	}
	for _, w := range want {
		if !slices.Contains(got, w) {
			t.Errorf("NeighborRoomNames(\"W9N7\", 1) missing %q (got %v)", w, got)
		}
	}

	// Adjacency across W0/E0 and N0/S0 boundaries.
	cross := NeighborRoomNames("W0N0", 1)
	for _, w := range []string{"W1N1", "W0N1", "E0N1", "W1N0", "W0N0", "E0N0", "W1S0", "W0S0", "E0S0"} {
		if !slices.Contains(cross, w) {
			t.Errorf("NeighborRoomNames(\"W0N0\", 1) missing %q (got %v)", w, cross)
		}
	}

	if r0 := NeighborRoomNames("W9N7", 0); len(r0) != 1 || r0[0] != "W9N7" {
		t.Errorf("NeighborRoomNames(\"W9N7\", 0) = %v, want [W9N7]", r0)
	}
	if bad := NeighborRoomNames("invalid", 1); bad != nil {
		t.Errorf("NeighborRoomNames(\"invalid\", 1) = %v, want nil", bad)
	}
}

func TestScreenToWorldTile(t *testing.T) {
	plain := strings.Repeat("0", GridSize*GridSize)
	center := &screeps.RoomSnapshot{Room: "W9N7", Terrain: plain}
	east := &screeps.RoomSnapshot{Room: "W8N7", Terrain: plain}
	north := &screeps.RoomSnapshot{Room: "W9N8", Terrain: plain}
	neighbors := []*screeps.RoomSnapshot{center, east, north}

	sz := geom.Size{W: 1200, H: 800}

	for rot := 0; rot < 4; rot++ {
		cam := NewCamera()
		cam.Rot = rot
		cam.Zoom = 0.5
		cam.PanX = 0
		cam.PanY = 0
		halfW := (BaseW / 2) * cam.Zoom
		halfH := (BaseH / 2) * cam.Zoom
		originX := sz.W/2 + cam.PanX
		originY := sz.H/2 + cam.PanY

		project := func(offX, offY, x, y int) geom.Offset {
			rx, ry := rotateCoord(offX+x, offY+y, rot)
			return geom.Offset{
				X: originX + float32(rx-ry)*halfW,
				Y: originY + float32(rx+ry)*halfH,
			}
		}

		// Pick tile in center room W9N7
		posC := project(0, 0, 24, 17)
		room, gx, gy, ok := cam.ScreenToWorldTile(posC, sz, center, neighbors)
		if !ok || room != "W9N7" || gx != 24 || gy != 17 {
			t.Errorf("rot=%d center pick = (%q, %d, %d, %v), want (\"W9N7\", 24, 17, true)", rot, room, gx, gy, ok)
		}

		// Backward-compatible ScreenToTile on center room
		sx, sy, sok := cam.ScreenToTile(posC, sz, center)
		if !sok || sx != 24 || sy != 17 {
			t.Errorf("rot=%d ScreenToTile = (%d, %d, %v), want (24, 17, true)", rot, sx, sy, sok)
		}

		// Pick tile in East neighbor room W8N7 (offX=+50, offY=0)
		posE := project(GridSize, 0, 12, 33)
		room, gx, gy, ok = cam.ScreenToWorldTile(posE, sz, center, neighbors)
		if !ok || room != "W8N7" || gx != 12 || gy != 33 {
			t.Errorf("rot=%d east pick = (%q, %d, %d, %v), want (\"W8N7\", 12, 33, true)", rot, room, gx, gy, ok)
		}

		// Pick tile in North neighbor room W9N8 (offX=0, offY=-50)
		posN := project(0, -GridSize, 40, 45)
		room, gx, gy, ok = cam.ScreenToWorldTile(posN, sz, center, neighbors)
		if !ok || room != "W9N8" || gx != 40 || gy != 45 {
			t.Errorf("rot=%d north pick = (%q, %d, %d, %v), want (\"W9N8\", 40, 45, true)", rot, room, gx, gy, ok)
		}

		// Point far outside any loaded room should miss
		if r, x, y, hit := cam.ScreenToWorldTile(geom.Offset{X: -5000, Y: -5000}, sz, center, neighbors); hit {
			t.Errorf("rot=%d far outside hit = (%q, %d, %d), want miss", rot, r, x, y)
		}
	}
}
