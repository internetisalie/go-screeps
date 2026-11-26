package iso

import (
	"image"
	"image/color"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"go-screeps/internal/iso/shaders"
	"go-screeps/internal/screeps"

	"github.com/bankai-tensei/gowaku/geom"
	"github.com/bankai-tensei/gowaku/paint"
)

const (
	GridSize = 50
	BaseW    = float32(18.5)
	BaseH    = float32(9.25)
)

// Camera holds 2.5D isometric pan, zoom, continuous yaw rotation, and selection state.
type Camera struct {
	PanX      float32
	PanY      float32
	Zoom      float32
	Rot       int     // legacy 0..3 quarter-turn index (kept in sync with Yaw)
	Yaw       float32 // continuous rotation in radians [0, 2*pi); 0=0°, pi/2=90°, etc.
	SelectedX int
	SelectedY int
	FollowID  string
	Fitted    bool
}

// YawRadians returns the active continuous yaw angle in radians.
// If Yaw is 0 and Rot is non-zero (e.g. set by legacy callers/tests), falls back to Rot*pi/2.
func (cam Camera) YawRadians() float32 {
	if cam.Yaw == 0 && cam.Rot != 0 {
		return float32(((cam.Rot%4)+4)%4) * (math.Pi / 2)
	}
	return cam.Yaw
}

// YawCosSin returns cos(yaw) and sin(yaw) as float32.
func (cam Camera) YawCosSin() (float32, float32) {
	y := float64(cam.YawRadians())
	return float32(math.Cos(y)), float32(math.Sin(y))
}

// SetYaw sets the continuous yaw angle (normalized to [0, 2*pi)) and syncs Rot.
func (cam *Camera) SetYaw(rad float32) {
	const twoPi = 2 * math.Pi
	y := math.Mod(float64(rad), twoPi)
	if y < 0 {
		y += twoPi
	}
	cam.Yaw = float32(y)
	cam.Rot = int(math.Round(y/(math.Pi/2))) % 4
}

// OrbitYaw rotates the camera by deltaRad while keeping the world point at the viewport center fixed.
func (cam *Camera) OrbitYaw(deltaRad float32) {
	halfW := (BaseW / 2) * cam.Zoom
	halfH := (BaseH / 2) * cam.Zoom
	if halfW <= 0 || halfH <= 0 {
		cam.SetYaw(cam.YawRadians() + deltaRad)
		return
	}
	oldCos, oldSin := cam.YawCosSin()
	ddx := -cam.PanX / halfW
	ddy := -cam.PanY / halfH
	frx0 := 0.5 * (ddx + ddy)
	fry0 := 0.5 * (ddy - ddx)
	wx, wy := unrotateYawF(frx0, fry0, oldCos, oldSin)

	cam.SetYaw(cam.YawRadians() + deltaRad)
	newCos, newSin := cam.YawCosSin()
	frx1, fry1 := rotateYawF(wx, wy, newCos, newSin)
	cam.PanX = -(frx1 - fry1) * halfW
	cam.PanY = -(frx1 + fry1) * halfH
}

func rotateYawF(gx, gy, cosY, sinY float32) (float32, float32) {
	ux := gx - 24.5
	uy := gy - 24.5
	return ux*cosY - uy*sinY + 24.5, ux*sinY + uy*cosY + 24.5
}

func unrotateYawF(rx, ry, cosY, sinY float32) (float32, float32) {
	ux := rx - 24.5
	uy := ry - 24.5
	return ux*cosY + uy*sinY + 24.5, -ux*sinY + uy*cosY + 24.5
}

// NewCamera returns an isometric camera; FitToSize centers and scales it to the viewport.
func NewCamera() Camera {
	return Camera{
		PanX:      0,
		PanY:      -24.5 * (BaseH / 2),
		Zoom:      1.0,
		SelectedX: -1,
		SelectedY: -1,
	}
}

// fitZoomFor returns the zoom factor that fits the 50x50 diamond inside sz.
func fitZoomFor(sz geom.Size) float32 {
	if sz.W <= 0 || sz.H <= 0 {
		return 1.0
	}
	zx := (sz.W * 0.90) / (float32(GridSize) * BaseW)
	zy := (sz.H * 0.90) / (float32(GridSize) * BaseH)
	return geom.Clamp32(min(zx, zy), 0.4, 4.0)
}

// FitToSize scales and centers the 50x50 diamond inside sz with a comfortable margin.
func (cam *Camera) FitToSize(sz geom.Size) {
	if sz.W <= 0 || sz.H <= 0 {
		return
	}
	cam.Zoom = fitZoomFor(sz)
	cam.PanX = 0
	cam.PanY = -float32(GridSize-1) * (BaseH / 2) * cam.Zoom
	cam.Fitted = true
}

// rotateCoord maps room tile (x, y) into rotated grid coordinates (rx, ry).
func rotateCoord(x, y, rot int) (int, int) {
	switch ((rot % 4) + 4) % 4 {
	case 1:
		return (GridSize - 1) - y, x
	case 2:
		return (GridSize - 1) - x, (GridSize - 1) - y
	case 3:
		return y, (GridSize - 1) - x
	default:
		return x, y
	}
}

// CenterOnTile pans the camera so room tile (x, y) sits at the center of the viewport.
func (cam *Camera) CenterOnTile(x, y int) {
	cosY, sinY := cam.YawCosSin()
	rx, ry := rotateYawF(float32(x), float32(y), cosY, sinY)
	halfW := (BaseW / 2) * cam.Zoom
	halfH := (BaseH / 2) * cam.Zoom
	cam.PanX = -(rx - ry) * halfW
	cam.PanY = -(rx + ry) * halfH
}

// unrotateCoord maps rotated grid coordinates (rx, ry) back to room tile (x, y).
func unrotateCoord(rx, ry, rot int) (int, int) {
	switch ((rot % 4) + 4) % 4 {
	case 1:
		return ry, (GridSize - 1) - rx
	case 2:
		return (GridSize - 1) - rx, (GridSize - 1) - ry
	case 3:
		return (GridSize - 1) - ry, rx
	default:
		return rx, ry
	}
}

// wallDepth returns 1 for edge walls, 2 for 1-tile-deep walls, and 3 for deeper mountain cores.
func wallDepth(terrain string, x, y int) int {
	if len(terrain) < GridSize*GridSize {
		return 1
	}
	isWall := func(nx, ny int) bool {
		if nx < 0 || nx >= GridSize || ny < 0 || ny >= GridSize {
			return true
		}
		c := terrain[ny*GridSize+nx]
		return c == '1' || c == '3'
	}
	for r := 1; r <= 2; r++ {
		for dy := -r; dy <= r; dy++ {
			for dx := -r; dx <= r; dx++ {
				if !isWall(x+dx, y+dy) {
					return r
				}
			}
		}
	}
	return 3
}

func wallHeightFromDepth(d int, z float32) float32 {
	return float32(4+d*4) * z
}

func wallHeight(terrain string, x, y int, z float32) float32 {
	d := wallDepth(terrain, x, y)
	// Subtle deterministic per-tile height variation so ridges look organic.
	jitter := float32(((x*73+y*137)%5)-2) * 0.45
	return (float32(4+d*4) + jitter) * z
}

// ViewportRoomShift returns how many rooms (dx, dy) the viewport center has panned
// away from the current active room [0..49]x[0..49]. Returns (0, 0) while inside.
func (cam Camera) ViewportRoomShift() (dx, dy int) {
	halfW := (BaseW / 2) * cam.Zoom
	halfH := (BaseH / 2) * cam.Zoom
	if halfW <= 0 || halfH <= 0 {
		return 0, 0
	}
	cosY, sinY := cam.YawCosSin()
	ddx := -cam.PanX / halfW
	ddy := -cam.PanY / halfH
	frx := 0.5 * (ddx + ddy)
	fry := 0.5 * (ddy - ddx)
	wgx, wgy := unrotateYawF(frx, fry, cosY, sinY)
	gx := int(math.Floor(float64(wgx) + 0.5))
	gy := int(math.Floor(float64(wgy) + 0.5))
	if gx < 0 {
		dx = (gx+1)/GridSize - 1
	} else if gx >= GridSize {
		dx = gx / GridSize
	}
	if gy < 0 {
		dy = (gy+1)/GridSize - 1
	} else if gy >= GridSize {
		dy = gy / GridSize
	}
	return dx, dy
}

// ShiftByRooms adjusts PanX and PanY so that changing the active room by (dx, dy)
// keeps every world tile at the exact same screen pixel (zero-pixel jump at any yaw).
func (cam *Camera) ShiftByRooms(dx, dy int) {
	if dx == 0 && dy == 0 {
		return
	}
	halfW := (BaseW / 2) * cam.Zoom
	halfH := (BaseH / 2) * cam.Zoom
	cosY, sinY := cam.YawCosSin()
	rx0, ry0 := rotateYawF(0, 0, cosY, sinY)
	rx1, ry1 := rotateYawF(float32(dx*GridSize), float32(dy*GridSize), cosY, sinY)
	drx := rx1 - rx0
	dry := ry1 - ry0
	cam.PanX += (drx - dry) * halfW
	cam.PanY += (drx + dry) * halfH
}

// ScreenToWorldTile maps a logical pixel position inside the canvas to a room tile (roomName, 0..49, 0..49)
// across the center room and any loaded neighbor rooms at any continuous yaw angle.
func (cam Camera) ScreenToWorldTile(pos geom.Offset, sz geom.Size, center *screeps.RoomSnapshot, neighbors []*screeps.RoomSnapshot) (roomName string, gx, gy int, ok bool) {
	halfW := (BaseW / 2) * cam.Zoom
	halfH := (BaseH / 2) * cam.Zoom
	if halfW <= 0 || halfH <= 0 || center == nil {
		return "", 0, 0, false
	}
	originX := sz.W/2 + cam.PanX
	originY := sz.H/2 + cam.PanY
	cosY, sinY := cam.YawCosSin()

	rooms := buildWorldRooms(center, neighbors, cam.Rot)
	if len(rooms) == 0 {
		return "", 0, 0, false
	}

	// Project screen point to ground-plane world coordinates.
	ddx := (pos.X - originX) / halfW
	ddy := (pos.Y - originY) / halfH
	frx0 := 0.5 * (ddx + ddy)
	fry0 := 0.5 * (ddy - ddx)
	wgx0, wgy0 := unrotateYawF(frx0, fry0, cosY, sinY)

	// Index loaded rooms by (offX/50, offY/50) for O(1) lookup.
	byGrid := make(map[[2]int]*worldRoom, len(rooms))
	for _, r := range rooms {
		byGrid[[2]int{r.offX / GridSize, r.offY / GridSize}] = r
	}
	lookupTile := func(wx, wy int) (*worldRoom, int, int, bool) {
		rx := wx / GridSize
		if wx < 0 {
			rx = (wx+1)/GridSize - 1
		}
		ry := wy / GridSize
		if wy < 0 {
			ry = (wy+1)/GridSize - 1
		}
		r, found := byGrid[[2]int{rx, ry}]
		if !found {
			return nil, 0, 0, false
		}
		lx, ly := wx-r.offX, wy-r.offY
		if lx < 0 || lx >= GridSize || ly < 0 || ly >= GridSize {
			return nil, 0, 0, false
		}
		return r, lx, ly, true
	}

	// Test extruded objects & walls near the click ray front-to-back (highest rotated rx+ry first).
	// Ray direction as height increases: D = (0.5*(cosY+sinY), 0.5*(cosY-sinY)).
	type cand struct {
		r      *worldRoom
		lx, ly int
		cx, cy float32
		depth  float32
		h, sc  float32
	}
	var cands []cand
	seen := make(map[[2]int]bool, 32)
	dxRay := 0.5 * (cosY + sinY)
	dyRay := 0.5 * (cosY - sinY)
	for step := 0; step <= 4; step++ {
		fx := wgx0 + dxRay*float32(step)
		fy := wgy0 + dyRay*float32(step)
		bx := int(math.Floor(float64(fx) + 0.5))
		by := int(math.Floor(float64(fy) + 0.5))
		for oy := -1; oy <= 1; oy++ {
			for ox := -1; ox <= 1; ox++ {
				wx, wy := bx+ox, by+oy
				if seen[[2]int{wx, wy}] {
					continue
				}
				seen[[2]int{wx, wy}] = true
				r, lx, ly, found := lookupTile(wx, wy)
				if !found {
					continue
				}
				var maxH, sc float32
				if r.hasTerr {
					t := r.snap.Terrain[ly*GridSize+lx]
					if t == '1' || t == '3' {
						maxH = wallHeightFromDepth(wallDepth(r.snap.Terrain, lx, ly), cam.Zoom)
						sc = 1.0
					}
				}
				for _, obj := range r.snap.Objects {
					if obj.X == lx && obj.Y == ly && !isSpawningCreep(obj) {
						if oh, osc := objectBoxMetrics(obj.Type, cam.Zoom); oh > 0 {
							maxH += oh
							if osc > sc {
								sc = osc
							}
						}
					}
				}
				if maxH > 0 {
					rx, ry := rotateYawF(float32(wx), float32(wy), cosY, sinY)
					cands = append(cands, cand{
						r:     r,
						lx:    lx,
						ly:    ly,
						cx:    originX + (rx-ry)*halfW,
						cy:    originY + (rx+ry)*halfH,
						depth: rx + ry,
						h:     maxH,
						sc:    sc,
					})
				}
			}
		}
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].depth > cands[j].depth })
	for _, c := range cands {
		if hitIsoBox(pos.X, pos.Y, c.cx, c.cy, halfW*c.sc, halfH*c.sc, c.h) {
			return c.r.snap.Room, c.lx, c.ly, true
		}
	}

	// Fall back to the ground tile directly under the click.
	wx := int(math.Floor(float64(wgx0) + 0.5))
	wy := int(math.Floor(float64(wgy0) + 0.5))
	if r, lx, ly, found := lookupTile(wx, wy); found {
		return r.snap.Room, lx, ly, true
	}
	return "", 0, 0, false
}

// ScreenToTile maps a logical pixel position inside the canvas to a room tile (0..49),
// testing extruded 2.5D boxes front-to-back so foreground objects/walls win over height=0 tiles behind them.
func (cam Camera) ScreenToTile(pos geom.Offset, sz geom.Size, snap *screeps.RoomSnapshot) (int, int, bool) {
	if snap != nil {
		_, x, y, ok := cam.ScreenToWorldTile(pos, sz, snap, nil)
		return x, y, ok
	}
	empty := &screeps.RoomSnapshot{}
	_, x, y, ok := cam.ScreenToWorldTile(pos, sz, empty, nil)
	return x, y, ok
}

func hitDiamond(px, py, cx, cy, hw, hh float32) bool {
	return float32(math.Abs(float64(px-cx)))/hw+float32(math.Abs(float64(py-cy)))/hh <= 1.0
}

func hitIsoBox(px, py, cx, cy, hw, hh, h float32) bool {
	if hitDiamond(px, py, cx, cy-h, hw, hh) {
		return true
	}
	if px >= cx-hw && px <= cx {
		yTop := (cy - h) + hh*(px-(cx-hw))/hw
		if py >= yTop && py <= yTop+h {
			return true
		}
	}
	if px >= cx && px <= cx+hw {
		yTop := (cy + hh - h) - hh*(px-cx)/hw
		if py >= yTop && py <= yTop+h {
			return true
		}
	}
	return false
}

func objectBoxMetrics(typ string, z float32) (height, scale float32) {
	switch typ {
	case "constructedWall":
		return 9.5 * z, 0.9
	case "source":
		return 9.5 * z, 0.6
	case "mineral":
		return 8.5 * z, 0.55
	case "controller":
		return 17 * z, 0.65
	case "spawn":
		return 10.5 * z, 0.8
	case "extension":
		return 5.5 * z, 0.5
	case "tower":
		return 14.5 * z, 0.62
	case "storage", "terminal", "factory":
		return 11 * z, 0.8
	case "container":
		return 4 * z, 0.48
	case "resource":
		return 3 * z, 0.35
	case "tombstone", "ruin":
		return 3.5 * z, 0.4
	case "creep", "powerCreep":
		return 6 * z, 0.46
	case "road", "rampart", "constructionSite":
		return 0, 1.0
	default:
		return 5.5 * z, 0.45
	}
}

type worldRoom struct {
	snap     *screeps.RoomSnapshot
	offX     int
	offY     int
	isCenter bool
	rMinX    int
	rMaxX    int
	rMinY    int
	rMaxY    int
	minSum   int
	maxSum   int
	hasTerr  bool
	hasRoad  [GridSize * GridSize]bool
	byTile   map[int][]screeps.RoomObject
}

func makeWorldRoom(snap *screeps.RoomSnapshot, offX, offY int, isCenter bool, rot int) *worldRoom {
	rx0, ry0 := rotateCoord(offX, offY, rot)
	rx1, ry1 := rotateCoord(offX+GridSize-1, offY+GridSize-1, rot)
	rMinX, rMaxX := min(rx0, rx1), max(rx0, rx1)
	rMinY, rMaxY := min(ry0, ry1), max(ry0, ry1)
	return &worldRoom{
		snap:     snap,
		offX:     offX,
		offY:     offY,
		isCenter: isCenter,
		rMinX:    rMinX,
		rMaxX:    rMaxX,
		rMinY:    rMinY,
		rMaxY:    rMaxY,
		minSum:   rMinX + rMinY,
		maxSum:   rMaxX + rMaxY,
		hasTerr:  len(snap.Terrain) >= GridSize*GridSize,
	}
}

func buildWorldRooms(center *screeps.RoomSnapshot, neighbors []*screeps.RoomSnapshot, rot int) []*worldRoom {
	if center == nil {
		return nil
	}
	rooms := make([]*worldRoom, 0, 1+len(neighbors))
	rooms = append(rooms, makeWorldRoom(center, 0, 0, true, rot))

	if len(neighbors) == 0 {
		return rooms
	}
	cwx, cwy, ok := ParseRoomCoords(center.Room)
	if !ok {
		return rooms
	}

	seen := make(map[string]struct{}, 1+len(neighbors))
	seen[strings.ToUpper(strings.TrimSpace(center.Room))] = struct{}{}

	for _, n := range neighbors {
		if n == nil || n.Room == "" {
			continue
		}
		norm := strings.ToUpper(strings.TrimSpace(n.Room))
		if _, dup := seen[norm]; dup {
			continue
		}
		rwx, rwy, rok := ParseRoomCoords(norm)
		if !rok {
			continue
		}
		seen[norm] = struct{}{}
		offX := (rwx - cwx) * GridSize
		offY := (rwy - cwy) * GridSize
		rooms = append(rooms, makeWorldRoom(n, offX, offY, false, rot))
	}

	// Deterministic ordering for neighbors so ground cache key is invariant to slice order.
	if len(rooms) > 2 {
		sort.Slice(rooms[1:], func(i, j int) bool {
			a, b := rooms[1+i], rooms[1+j]
			if a.offY != b.offY {
				return a.offY < b.offY
			}
			return a.offX < b.offX
		})
	}
	return rooms
}

func hashWorldRooms(rooms []*worldRoom) uint64 {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	h := uint64(offset64)
	mix := func(v uint64) {
		h ^= v
		h *= prime64
	}
	for _, r := range rooms {
		for i := 0; i < len(r.snap.Room); i++ {
			mix(uint64(r.snap.Room[i]))
		}
		mix(uint64(r.offX))
		mix(uint64(r.offY))
		if r.isCenter {
			mix(1)
		} else {
			mix(0)
		}
		mix(uint64(len(r.snap.Terrain)))
	}
	return h
}

func roomVisible(r *worldRoom, originX, originY, halfW, halfH, extMargin float32, sz geom.Size) bool {
	xMin := originX + float32(r.rMinX-r.rMaxY-1)*halfW
	xMax := originX + float32(r.rMaxX-r.rMinY+1)*halfW
	yMin := originY + float32(r.minSum-1)*halfH - extMargin
	yMax := originY + float32(r.maxSum+1)*halfH
	return !(xMax < -20 || xMin > sz.W+20 || yMax < -40 || yMin > sz.H+40)
}

type groundCacheKey struct {
	roomHash  uint64
	roomCount int
	rot       int
	zoom      float32
	panX      float32
	panY      float32
	w         float32
	h         float32
}

// Painter implements layout.Painter for the isometric Screeps room (and optional neighbor rooms).
type Painter struct {
	Snap        *screeps.RoomSnapshot
	Neighbors   []*screeps.RoomSnapshot // optional; when populated, renders multi-room neighborhood
	Cam         *Camera
	CreepPos    map[string][2]float32 // interpolated (x, y) per creep ID
	ShowVisuals bool
	LastSize       geom.Size
	groundLayer    paint.Layer
	groundKey      groundCacheKey
	groundValid    bool
	terrainImg     *paint.Image
	terrainImgHash uint64
	terrainImgRot  int
	startTime      time.Time
	creepFacing    map[string]float64    // remembered world-space facing angle (radians) per creep/tower ID
	creepLastPos   map[string][2]float32 // last world-space (gfx, gfy) per creep ID for movement heading
}

func roomVisibleYaw(r *worldRoom, originX, originY, halfW, halfH, extMargin, cosY, sinY float32, sz geom.Size) bool {
	corners := [4][2]float32{
		{float32(r.offX) - 0.5, float32(r.offY) - 0.5},
		{float32(r.offX+GridSize) - 0.5, float32(r.offY) - 0.5},
		{float32(r.offX+GridSize) - 0.5, float32(r.offY+GridSize) - 0.5},
		{float32(r.offX) - 0.5, float32(r.offY+GridSize) - 0.5},
	}
	xMin, xMax := float32(math.MaxFloat32), float32(-math.MaxFloat32)
	yMin, yMax := float32(math.MaxFloat32), float32(-math.MaxFloat32)
	for _, c := range corners {
		rx, ry := rotateYawF(c[0], c[1], cosY, sinY)
		sx := originX + (rx-ry)*halfW
		sy := originY + (rx+ry)*halfH
		if sx < xMin {
			xMin = sx
		}
		if sx > xMax {
			xMax = sx
		}
		if sy < yMin {
			yMin = sy
		}
		if sy > yMax {
			yMax = sy
		}
	}
	return !(xMax < -20 || xMin > sz.W+20 || yMax < -40 || yMin-extMargin > sz.H+40)
}

func (p *Painter) ensureTerrainImage(rooms []*worldRoom) *paint.Image {
	h := hashWorldRooms(rooms)
	if p.terrainImg != nil && p.terrainImgHash == h {
		return p.terrainImg
	}
	const texSize = GridSize * 5 // 250x250 covers 5x5 unrotated world coords [-100..149]
	img := image.NewRGBA(image.Rect(0, 0, texSize, texSize))
	for _, r := range rooms {
		bVal := uint8(128)
		if r.isCenter {
			bVal = 255
		}
		for y := 0; y < GridSize; y++ {
			rowOff := y * GridSize
			for x := 0; x < GridSize; x++ {
				ix, iy := r.offX+x+GridSize*2, r.offY+y+GridSize*2
				if ix < 0 || ix >= texSize || iy < 0 || iy >= texSize {
					continue
				}
				var rVal, gVal uint8 = 64, 0
				if r.hasTerr {
					switch r.snap.Terrain[rowOff+x] {
					case '2':
						rVal = 128
					case '1':
						rVal = 192
						gVal = uint8(wallDepth(r.snap.Terrain, x, y) * 85)
					case '3':
						rVal = 255
						gVal = uint8(wallDepth(r.snap.Terrain, x, y) * 85)
					}
				}
				img.SetRGBA(ix, iy, color.RGBA{R: rVal, G: gVal, B: bVal, A: 255})
			}
		}
	}
	p.terrainImg = paint.NewImage(img)
	p.terrainImgHash = h
	return p.terrainImg
}

func rotateCoordF(fx, fy float32, rot int) (float32, float32) {
	maxC := float32(GridSize - 1)
	switch ((rot % 4) + 4) % 4 {
	case 1:
		return maxC - fy, fx
	case 2:
		return maxC - fx, maxC - fy
	case 3:
		return fy, maxC - fx
	default:
		return fx, fy
	}
}

func drawRoomBorderYaw(cv *paint.Canvas, p *paint.Path, r *worldRoom, originX, originY, halfW, halfH, cosY, sinY float32, col geom.Color, width float32) {
	isoCorner := func(gx, gy float32) (float32, float32) {
		frx, fry := rotateYawF(gx, gy, cosY, sinY)
		return originX + (frx-fry)*halfW, originY + (frx+fry)*halfH
	}
	minX := float32(r.offX) - 0.5
	maxX := float32(r.offX+GridSize) - 0.5
	minY := float32(r.offY) - 0.5
	maxY := float32(r.offY+GridSize) - 0.5

	x0, y0 := isoCorner(minX, minY)
	x1, y1 := isoCorner(maxX, minY)
	x2, y2 := isoCorner(maxX, maxY)
	x3, y3 := isoCorner(minX, maxY)

	p.Reset()
	p.MoveTo(x0, y0)
	p.LineTo(x1, y1)
	p.LineTo(x2, y2)
	p.LineTo(x3, y3)
	p.Close()
	cv.DrawPath(p, paint.Paint{Stroke: &paint.Stroke{Width: width, Color: col}})
}

// Paint renders the 50x50 isometric terrain (plus any loaded neighbor rooms) and live objects back-to-front.
func (p *Painter) Paint(cv *paint.Canvas, sz geom.Size) {
	if p.Cam != nil && sz.W > 0 && sz.H > 0 {
		if !p.Cam.Fitted {
			p.Cam.FitToSize(sz)
		} else if p.LastSize.W > 0 && p.LastSize.H > 0 && (sz.W != p.LastSize.W || sz.H != p.LastSize.H) {
			oldFit := fitZoomFor(p.LastSize)
			newFit := fitZoomFor(sz)
			if oldFit > 0 {
				ratio := newFit / oldFit
				p.Cam.Zoom = geom.Clamp32(p.Cam.Zoom*ratio, 0.15, 4.0)
				p.Cam.PanX *= ratio
				p.Cam.PanY *= ratio
			}
		}
	}
	p.LastSize = sz
	cv.Save()
	cv.ClipRect(geom.Rect{W: sz.W, H: sz.H})

	// Background
	cv.DrawRect(geom.Rect{W: sz.W, H: sz.H}, paint.Paint{Color: geom.Hex(0x0f1216)})

	snap := p.Snap
	cam := p.Cam
	if snap == nil || cam == nil {
		cv.Restore()
		return
	}

	halfW := (BaseW / 2) * cam.Zoom
	halfH := (BaseH / 2) * cam.Zoom
	originX := sz.W/2 + cam.PanX
	originY := sz.H/2 + cam.PanY

	rooms := buildWorldRooms(snap, p.Neighbors, cam.Rot)
	if len(rooms) == 0 {
		cv.Restore()
		return
	}
	multiRoom := len(rooms) > 1

	cosY, sinY := cam.YawCosSin()

	// Filter to rooms overlapping the viewport (with generous margin for tall extrusions).
	extMargin := 40*cam.Zoom + halfH*4
	visRooms := make([]*worldRoom, 0, len(rooms))
	roomByOffset := make(map[[2]int]*worldRoom, len(rooms))
	globalMinSum := math.MaxInt
	globalMaxSum := math.MinInt
	for _, r := range rooms {
		roomByOffset[[2]int{r.offX, r.offY}] = r
		if roomVisibleYaw(r, originX, originY, halfW, halfH, extMargin, cosY, sinY, sz) {
			visRooms = append(visRooms, r)
			if r.minSum < globalMinSum {
				globalMinSum = r.minSum
			}
			if r.maxSum > globalMaxSum {
				globalMaxSum = r.maxSum
			}
		}
	}

	// Index static objects by tile coordinate per room and creeps by continuous depth (frx + fry).
	type creepDraw struct {
		obj   screeps.RoomObject
		room  *worldRoom
		fx    float32
		fy    float32
		gfx   float32
		gfy   float32
		depth float32
	}
	var allCreeps []creepDraw

	for _, r := range visRooms {
		r.byTile = make(map[int][]screeps.RoomObject, len(r.snap.Objects))
		for _, obj := range r.snap.Objects {
			if isSpawningCreep(obj) {
				continue
			}
			if obj.Type == "road" && obj.X >= 0 && obj.X < GridSize && obj.Y >= 0 && obj.Y < GridSize {
				r.hasRoad[obj.Y*GridSize+obj.X] = true
			}
			if obj.Type == "creep" || obj.Type == "powerCreep" {
				fx, fy := float32(obj.X), float32(obj.Y)
				if p.CreepPos != nil {
					if pos, ok := p.CreepPos[obj.ID]; ok {
						fx, fy = pos[0], pos[1]
					}
				}
				gfx := float32(r.offX) + fx
				gfy := float32(r.offY) + fy
				frx, fry := rotateYawF(gfx, gfy, cosY, sinY)
				allCreeps = append(allCreeps, creepDraw{
					obj:   obj,
					room:  r,
					fx:    fx,
					fy:    fy,
					gfx:   gfx,
					gfy:   gfy,
					depth: frx + fry,
				})
				continue
			}
			if obj.X >= 0 && obj.X < GridSize && obj.Y >= 0 && obj.Y < GridSize {
				key := obj.Y*GridSize + obj.X
				r.byTile[key] = append(r.byTile[key], obj)
			}
		}
		for k, list := range r.byTile {
			if len(list) > 1 {
				sort.Slice(list, func(i, j int) bool {
					return layerOrder(list[i].Type) < layerOrder(list[j].Type)
				})
				r.byTile[k] = list
			}
		}
	}

	// Reusable path to avoid per-tile allocations.
	var path paint.Path

	// Compute viewport-clipped sum and horizontal diff bounds so zoomed-in rendering is O(visible tiles).
	var sumStart, sumEnd int
	var diffMin, diffMax float64
	if len(visRooms) > 0 && halfH > 0 && halfW > 0 {
		visMinSum := int(math.Floor(float64((-40-halfH*4-originY)/halfH))) - 1
		visMaxSum := int(math.Ceil(float64((sz.H+40+extMargin-originY)/halfH))) + 1
		sumStart = max(globalMinSum, visMinSum)
		sumEnd = min(globalMaxSum, visMaxSum)
		diffMin = float64((-20 - halfW - originX) / halfW)
		diffMax = float64((sz.W + 20 + halfW - originX) / halfW)
	} else {
		sumStart = 1
		sumEnd = 0
	}

	drawWallAt := func(dst *paint.Canvas, r *worldRoom, rx, ry, x, y int, cx, cy float32) {
		d := wallDepth(r.snap.Terrain, x, y)
		wh := wallHeightFromDepth(d, cam.Zoom)
		topCol := geom.Hex(0x3a404d)
		if d == 2 {
			topCol = geom.Hex(0x434a59)
		} else if d >= 3 {
			topCol = geom.Hex(0x4d5566)
		}
		leftCol := geom.Hex(0x2b303a)
		rightCol := geom.Hex(0x21252d)
		if !r.isCenter {
			topCol = darken(topCol, 0.80)
			leftCol = darken(leftCol, 0.80)
			rightCol = darken(rightCol, 0.80)
		}
		// Hidden-face culling: skip left/right faces when occluded by an adjacent foreground wall of >= height.
		drawLeft := true
		if lx, ly := unrotateCoord(rx, ry+1, cam.Rot); lx >= r.offX && lx < r.offX+GridSize && ly >= r.offY && ly < r.offY+GridSize {
			if wallDepth(r.snap.Terrain, lx-r.offX, ly-r.offY) >= d {
				drawLeft = false
			}
		}
		drawRight := true
		if rx2, ry2 := unrotateCoord(rx+1, ry, cam.Rot); rx2 >= r.offX && rx2 < r.offX+GridSize && ry2 >= r.offY && ry2 < r.offY+GridSize {
			if wallDepth(r.snap.Terrain, rx2-r.offX, ry2-r.offY) >= d {
				drawRight = false
			}
		}
		drawIsoBoxCulled(dst, &path, cx, cy, halfW, halfH, wh, topCol, leftCol, rightCol, drawLeft, drawRight)
	}

	renderStaticTerrainAndWalls := func(dst *paint.Canvas, cached bool) {
		var swampPath paint.Path
		var gridPath paint.Path
		for _, r := range visRooms {
			// 1) Single filled quad for the entire 50x50 room floor (replaces 2,500 individual plain diamonds).
			minX := float32(r.rMinX) - 0.5
			maxX := float32(r.rMaxX) + 0.5
			minY := float32(r.rMinY) - 0.5
			maxY := float32(r.rMaxY) + 0.5
			isoCorner := func(frx, fry float32) (float32, float32) {
				return originX + (frx-fry)*halfW, originY + (frx+fry)*halfH
			}
			x0, y0 := isoCorner(minX, minY)
			x1, y1 := isoCorner(maxX, minY)
			x2, y2 := isoCorner(maxX, maxY)
			x3, y3 := isoCorner(minX, maxY)
			baseFill := geom.Hex(0x1b1f26)
			if cached {
				baseFill = geom.Hex(0x1a1e25)
			}
			if !r.isCenter {
				baseFill = darken(baseFill, 0.78)
			}
			path.Reset()
			path.MoveTo(x0, y0)
			path.LineTo(x1, y1)
			path.LineTo(x2, y2)
			path.LineTo(x3, y3)
			path.Close()
			dst.DrawPath(&path, paint.Paint{Color: baseFill})

			// 2) Batched swamp tiles (1 DrawPath per room).
			if r.hasTerr {
				swampPath.Reset()
				hasSwamp := false
				for y := 0; y < GridSize; y++ {
					rowOff := y * GridSize
					for x := 0; x < GridSize; x++ {
						t := r.snap.Terrain[rowOff+x]
						if t != '2' && t != '3' {
							continue
						}
						rx, ry := rotateCoord(r.offX+x, r.offY+y, cam.Rot)
						cx := originX + float32(rx-ry)*halfW
						cy := originY + float32(rx+ry)*halfH
						if cx+halfW < -20 || cx-halfW > sz.W+20 || cy+halfH < -40 || cy-halfH > sz.H+40 {
							continue
						}
						swampPath.MoveTo(cx, cy-halfH)
						swampPath.LineTo(cx+halfW, cy)
						swampPath.LineTo(cx, cy+halfH)
						swampPath.LineTo(cx-halfW, cy)
						swampPath.Close()
						hasSwamp = true
					}
				}
				if hasSwamp {
					swampCol := geom.Hex(0x1c3125)
					if !r.isCenter {
						swampCol = darken(swampCol, 0.78)
					}
					dst.DrawPath(&swampPath, paint.Paint{Color: swampCol})
				}
			}

			// 3) Batched subtle grid lines (1 DrawPath per room, only when zoomed in enough to see tiles).
			if halfW >= 5.0 {
				gridPath.Reset()
				for i := 1; i < GridSize; i++ {
					fi := float32(r.rMinX+i) - 0.5
					gx0, gy0 := isoCorner(fi, minY)
					gx1, gy1 := isoCorner(fi, maxY)
					gridPath.MoveTo(gx0, gy0)
					gridPath.LineTo(gx1, gy1)

					fj := float32(r.rMinY+i) - 0.5
					hx0, hy0 := isoCorner(minX, fj)
					hx1, hy1 := isoCorner(maxX, fj)
					gridPath.MoveTo(hx0, hy0)
					gridPath.LineTo(hx1, hy1)
				}
				gridCol := hexAlpha(0x0b0e13, 0.35)
				if !r.isCenter {
					gridCol = hexAlpha(0x0b0e13, 0.22)
				}
				dst.DrawPath(&gridPath, paint.Paint{Stroke: &paint.Stroke{Width: 0.75, Color: gridCol}})
			}
		}

		// 4) Static terrain walls baked into the cached layer (hidden-face culled).
		// When zoomed out (halfW < 10), batch all wall faces per room into 5 DrawPath calls
		// (left, right, depth-1/2/3 tops) instead of thousands of individual DrawPath calls.
		if halfW < 10 {
			for _, r := range visRooms {
				if !r.hasTerr {
					continue
				}
				var leftP, rightP, top1P, top2P, top3P paint.Path
				hasL, hasR, hasT1, hasT2, hasT3 := false, false, false, false, false
				for y := 0; y < GridSize; y++ {
					rowOff := y * GridSize
					for x := 0; x < GridSize; x++ {
						t := r.snap.Terrain[rowOff+x]
						if t != '1' && t != '3' {
							continue
						}
						rx, ry := rotateCoord(r.offX+x, r.offY+y, cam.Rot)
						cx := originX + float32(rx-ry)*halfW
						cy := originY + float32(rx+ry)*halfH
						if cx+halfW < -20 || cx-halfW > sz.W+20 || cy+halfH*4 < -40 || cy-extMargin > sz.H+40 {
							continue
						}
						d := wallDepth(r.snap.Terrain, x, y)
						h := wallHeightFromDepth(d, cam.Zoom)

						drawLeft := true
						if lx, ly := unrotateCoord(rx, ry+1, cam.Rot); lx >= r.offX && lx < r.offX+GridSize && ly >= r.offY && ly < r.offY+GridSize {
							if wallDepth(r.snap.Terrain, lx-r.offX, ly-r.offY) >= d {
								drawLeft = false
							}
						}
						if drawLeft {
							leftP.MoveTo(cx-halfW, cy-h)
							leftP.LineTo(cx, cy+halfH-h)
							leftP.LineTo(cx, cy+halfH)
							leftP.LineTo(cx-halfW, cy)
							leftP.Close()
							hasL = true
						}

						drawRight := true
						if rx2, ry2 := unrotateCoord(rx+1, ry, cam.Rot); rx2 >= r.offX && rx2 < r.offX+GridSize && ry2 >= r.offY && ry2 < r.offY+GridSize {
							if wallDepth(r.snap.Terrain, rx2-r.offX, ry2-r.offY) >= d {
								drawRight = false
							}
						}
						if drawRight {
							rightP.MoveTo(cx, cy+halfH-h)
							rightP.LineTo(cx+halfW, cy-h)
							rightP.LineTo(cx+halfW, cy)
							rightP.LineTo(cx, cy+halfH)
							rightP.Close()
							hasR = true
						}

						var tp *paint.Path
						if d >= 3 {
							tp = &top3P
							hasT3 = true
						} else if d == 2 {
							tp = &top2P
							hasT2 = true
						} else {
							tp = &top1P
							hasT1 = true
						}
						tp.MoveTo(cx, cy-halfH-h)
						tp.LineTo(cx+halfW, cy-h)
						tp.LineTo(cx, cy+halfH-h)
						tp.LineTo(cx-halfW, cy-h)
						tp.Close()
					}
				}
				leftCol := geom.Hex(0x2b303a)
				rightCol := geom.Hex(0x21252d)
				top1Col := geom.Hex(0x3a404d)
				top2Col := geom.Hex(0x434a59)
				top3Col := geom.Hex(0x4d5566)
				if !r.isCenter {
					leftCol = darken(leftCol, 0.80)
					rightCol = darken(rightCol, 0.80)
					top1Col = darken(top1Col, 0.80)
					top2Col = darken(top2Col, 0.80)
					top3Col = darken(top3Col, 0.80)
				}
				if hasL {
					dst.DrawPath(&leftP, paint.Paint{Color: leftCol})
				}
				if hasR {
					dst.DrawPath(&rightP, paint.Paint{Color: rightCol})
				}
				if hasT1 {
					dst.DrawPath(&top1P, paint.Paint{Color: top1Col})
				}
				if hasT2 {
					dst.DrawPath(&top2P, paint.Paint{Color: top2Col})
				}
				if hasT3 {
					dst.DrawPath(&top3P, paint.Paint{Color: top3Col})
				}
			}
		} else {
			for sum := sumStart; sum <= sumEnd; sum++ {
				visRxMin := int(math.Floor((float64(sum)+diffMin)/2.0)) - 1
				visRxMax := int(math.Ceil((float64(sum)+diffMax)/2.0)) + 1
				for _, r := range visRooms {
					if !r.hasTerr || sum < r.minSum || sum > r.maxSum {
						continue
					}
					rxMin := max(r.rMinX, max(sum-r.rMaxY, visRxMin))
					rxMax := min(r.rMaxX, min(sum-r.rMinY, visRxMax))
					for rx := rxMin; rx <= rxMax; rx++ {
						ry := sum - rx
						globX, globY := unrotateCoord(rx, ry, cam.Rot)
						x := globX - r.offX
						y := globY - r.offY
						t := r.snap.Terrain[y*GridSize+x]
						if t != '1' && t != '3' {
							continue
						}
						cx := originX + float32(rx-ry)*halfW
						cy := originY + float32(rx+ry)*halfH
						if cx+halfW < -20 || cx-halfW > sz.W+20 || cy+halfH*4 < -40 || cy-extMargin > sz.H+40 {
							continue
						}
						drawWallAt(dst, r, rx, ry, x, y, cx, cy)
					}
				}
			}
		}
	}

	if p.startTime.IsZero() {
		p.startTime = time.Now()
	}
	animDur := time.Since(p.startTime)
	animSec := float32(animDur.Seconds())

	// Object-local 3D axes align with the isometric tile grid (rotated 45 deg from screen X/Y).
	objYaw := float64(cam.YawRadians()) + math.Pi/4
	objCos, objSin := float32(math.Cos(objYaw)), float32(math.Sin(objYaw))

	// Pass 1: Static terrain floor + 2.5D walls rendered in a single GPU fragment shader pass.
	// Falls back to cached vector paint.Layer only if shader/texture is unavailable.
	if img := p.ensureTerrainImage(rooms); img != nil && shaders.Terrain != nil {
		var mr float32
		if multiRoom {
			mr = 1
		}
		cv.DrawShader(geom.Rect{W: sz.W, H: sz.H}, paint.ShaderCall{
			Shader: shaders.Terrain,
			Time:   animDur,
			Params: paint.ShaderParams{
				originX, originY, halfW, halfH,
				cam.Zoom, cosY, sinY, mr,
			},
			Tex0: paint.ShaderTexture{Image: img},
		})
	} else if cv.CanDrawLayer() {
		key := groundCacheKey{
			roomHash:  hashWorldRooms(rooms),
			roomCount: len(rooms),
			rot:       cam.Rot,
			zoom:      cam.Zoom,
			panX:      cam.PanX,
			panY:      cam.PanY,
			w:         sz.W,
			h:         sz.H,
		}
		if !p.groundValid || p.groundKey != key {
			lcv := p.groundLayer.Begin()
			lcv.DrawRect(geom.Rect{W: sz.W, H: sz.H}, paint.Paint{Color: geom.Hex(0x0f1216)})
			renderStaticTerrainAndWalls(lcv, true)
			p.groundLayer.End()
			p.groundKey = key
			p.groundValid = true
		}
		cv.DrawLayer(&p.groundLayer, geom.Offset{})
	} else {
		renderStaticTerrainAndWalls(cv, false)
	}

	// Pass 1b: Dynamic ground overlays (selection, layered sci-fi curb-lit roads, ramparts, construction sites)
	if cam.SelectedX >= 0 && cam.SelectedX < GridSize && cam.SelectedY >= 0 && cam.SelectedY < GridSize {
		sx, sy := float32(cam.SelectedX), float32(cam.SelectedY)
		corners := [4][2]float32{
			{sx - 0.5, sy - 0.5},
			{sx + 0.5, sy - 0.5},
			{sx + 0.5, sy + 0.5},
			{sx - 0.5, sy + 0.5},
		}
		path.Reset()
		for i, c := range corners {
			rx, ry := rotateYawF(c[0], c[1], cosY, sinY)
			px := originX + (rx-ry)*halfW
			py := originY + (rx+ry)*halfH
			if i == 0 {
				path.MoveTo(px, py)
			} else {
				path.LineTo(px, py)
			}
		}
		path.Close()
		cv.DrawPath(&path, paint.Paint{Color: hexAlpha(0x4fc3f7, 0.25)})
		cv.DrawPath(&path, paint.Paint{Stroke: &paint.Stroke{Width: 1.2, Color: geom.Hex(0x4fc3f7)}})
	}

	type roadNode struct {
		cx, cy float32
		wx, wy float32
		deg    int
	}
	type roadSeg struct {
		x1, y1, x2, y2 float32
	}
	var roadNodes []roadNode
	var roadSegs []roadSeg

	hasRoadAt := func(r *worldRoom, nx, ny int) bool {
		if nx >= 0 && nx < GridSize && ny >= 0 && ny < GridSize {
			return r.hasRoad[ny*GridSize+nx]
		}
		if !multiRoom {
			return false
		}
		dxRoom, lnx := 0, nx
		if lnx < 0 {
			dxRoom = -GridSize
			lnx += GridSize
		} else if lnx >= GridSize {
			dxRoom = GridSize
			lnx -= GridSize
		}
		dyRoom, lny := 0, ny
		if lny < 0 {
			dyRoom = -GridSize
			lny += GridSize
		} else if lny >= GridSize {
			dyRoom = GridSize
			lny -= GridSize
		}
		if nr, ok := roomByOffset[[2]int{r.offX + dxRoom, r.offY + dyRoom}]; ok {
			return nr.hasRoad[lny*GridSize+lnx]
		}
		return false
	}

	for _, r := range visRooms {
		for _, obj := range r.snap.Objects {
			if obj.X < 0 || obj.X >= GridSize || obj.Y < 0 || obj.Y >= GridSize {
				continue
			}
			if obj.Type != "road" && obj.Type != "rampart" && obj.Type != "constructionSite" {
				continue
			}
			rx, ry := rotateYawF(float32(r.offX+obj.X), float32(r.offY+obj.Y), cosY, sinY)
			cx := originX + (rx-ry)*halfW
			cy := originY + (rx+ry)*halfH
			if cx+halfW < -20 || cx-halfW > sz.W+20 || cy+halfH*4 < -40 || cy-halfH*4 > sz.H+40 {
				continue
			}
			if obj.Type == "road" {
				deg := 0
				for _, d := range [8][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}, {1, 1}, {-1, -1}, {1, -1}, {-1, 1}} {
					if hasRoadAt(r, obj.X+d[0], obj.Y+d[1]) {
						deg++
					}
				}
				roadNodes = append(roadNodes, roadNode{cx: cx, cy: cy, wx: float32(r.offX + obj.X), wy: float32(r.offY + obj.Y), deg: deg})
				for _, d := range [4][2]int{{1, 0}, {0, 1}, {1, 1}, {1, -1}} {
					nx, ny := obj.X+d[0], obj.Y+d[1]
					if hasRoadAt(r, nx, ny) {
						nrx2, nry2 := rotateYawF(float32(r.offX+nx), float32(r.offY+ny), cosY, sinY)
						roadSegs = append(roadSegs, roadSeg{
							x1: cx, y1: cy,
							x2: originX + (nrx2-nry2)*halfW,
							y2: originY + (nrx2+nry2)*halfH,
						})
					}
				}
			} else {
				drawObject(cv, &path, cx, cy, halfW, halfH, cam.Zoom, objCos, objSin, animDur, animSec, obj)
			}
		}
	}

	// Draw connected sci-fi roadways in 4 clean layers so curbs and intersections merge seamlessly:
	// 1) Outer dark shadow casing  2) Steel-silver curb rails  3) Dark asphalt roadbed  4) Warm-gold dashes & junction diamonds
	if len(roadNodes) > 0 {
		roadW := max(float32(2.0), halfH*0.64)
		shadowStroke := &paint.Stroke{Width: roadW * 1.34, Color: hexAlpha(0x0b0e14, 0.72)}
		curbStroke := &paint.Stroke{Width: roadW * 1.08, Color: geom.Hex(0x6d7d94)}
		bedStroke := &paint.Stroke{Width: roadW * 0.76, Color: geom.Hex(0x212835)}

		// Layer 1: Outer shadow casing
		for _, n := range roadNodes {
			drawIsoEllipse(cv, &path, n.cx, n.cy, halfW*0.42, halfH*0.42, hexAlpha(0x0b0e14, 0.65), nil)
		}
		for _, s := range roadSegs {
			path.Reset()
			path.MoveTo(s.x1, s.y1)
			path.LineTo(s.x2, s.y2)
			cv.DrawPath(&path, paint.Paint{Stroke: shadowStroke})
		}

		// Layer 2: Steel-silver outer curb rails
		for _, n := range roadNodes {
			drawIsoEllipse(cv, &path, n.cx, n.cy, halfW*0.35, halfH*0.35, geom.Hex(0x6d7d94), nil)
		}
		for _, s := range roadSegs {
			path.Reset()
			path.MoveTo(s.x1, s.y1)
			path.LineTo(s.x2, s.y2)
			cv.DrawPath(&path, paint.Paint{Stroke: curbStroke})
		}

		// Layer 3: Dark metallic asphalt roadbed
		for _, n := range roadNodes {
			drawIsoEllipse(cv, &path, n.cx, n.cy, halfW*0.26, halfH*0.26, geom.Hex(0x212835), nil)
		}
		for _, s := range roadSegs {
			path.Reset()
			path.MoveTo(s.x1, s.y1)
			path.LineTo(s.x2, s.y2)
			cv.DrawPath(&path, paint.Paint{Stroke: bedStroke})
		}

		// Layer 4: Dashed warm-gold centerline & glowing intersection diamond pads
		if halfW >= 7 {
			dashStroke := &paint.Stroke{Width: max(float32(1.0), roadW*0.13), Color: hexAlpha(0xffd56b, 0.85)}
			for _, s := range roadSegs {
				dx, dy := s.x2-s.x1, s.y2-s.y1
				path.Reset()
				path.MoveTo(s.x1+dx*0.22, s.y1+dy*0.22)
				path.LineTo(s.x1+dx*0.42, s.y1+dy*0.42)
				path.MoveTo(s.x1+dx*0.58, s.y1+dy*0.58)
				path.LineTo(s.x1+dx*0.78, s.y1+dy*0.78)
				cv.DrawPath(&path, paint.Paint{Stroke: dashStroke})
			}
			juncStroke := &paint.Stroke{Width: max(float32(1.1), roadW*0.14), Color: hexAlpha(0xd8f3ff, 0.90)}
			for _, n := range roadNodes {
				if n.deg < 3 {
					continue
				}
				const rj = float32(0.18)
				corners := [4][2]float32{
					{n.wx - rj, n.wy - rj},
					{n.wx + rj, n.wy - rj},
					{n.wx + rj, n.wy + rj},
					{n.wx - rj, n.wy + rj},
				}
				path.Reset()
				for i, c := range corners {
					rx, ry := rotateYawF(c[0], c[1], cosY, sinY)
					px := originX + (rx-ry)*halfW
					py := originY + (rx+ry)*halfH
					if i == 0 {
						path.MoveTo(px, py)
					} else {
						path.LineTo(px, py)
					}
				}
				path.Close()
				cv.DrawPath(&path, paint.Paint{Color: geom.Hex(0x283242)})
				cv.DrawPath(&path, paint.Paint{Stroke: juncStroke})
			}
		}
	}

	// Pass 2: Dynamic 3D structures and interpolated creeps sorted back-to-front by continuous depth (frx + fry).
	type drawItem struct {
		depth   float32
		tieKey  string
		isCreep bool
		r       *worldRoom
		x, y    int
		cx, cy  float32
		creep   creepDraw
	}
	var items []drawItem

	for _, r := range visRooms {
		for key, objs := range r.byTile {
			has3D := false
			for _, obj := range objs {
				if obj.Type != "road" && obj.Type != "rampart" && obj.Type != "constructionSite" {
					has3D = true
					break
				}
			}
			if !has3D {
				continue
			}
			x, y := key%GridSize, key/GridSize
			rx, ry := rotateYawF(float32(r.offX+x), float32(r.offY+y), cosY, sinY)
			cx := originX + (rx-ry)*halfW
			cy := originY + (rx+ry)*halfH
			if cx+halfW < -20 || cx-halfW > sz.W+20 || cy+halfH*4 < -40 || cy-extMargin > sz.H+40 {
				continue
			}
			items = append(items, drawItem{
				depth:  rx + ry,
				tieKey: r.snap.Room + ":" + strconv.Itoa(key),
				r:      r,
				x:      x,
				y:      y,
				cx:     cx,
				cy:     cy,
			})
		}
	}
	for _, c := range allCreeps {
		frx, fry := rotateYawF(c.gfx, c.gfy, cosY, sinY)
		ocx := originX + (frx-fry)*halfW
		ocy := originY + (frx+fry)*halfH
		if ocx+halfW < -20 || ocx-halfW > sz.W+20 || ocy+halfH*4 < -40 || ocy-extMargin > sz.H+40 {
			continue
		}
		items = append(items, drawItem{
			depth:   c.depth,
			tieKey:  c.obj.ID,
			isCreep: true,
			cx:      ocx,
			cy:      ocy,
			creep:   c,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].depth != items[j].depth {
			return items[i].depth < items[j].depth
		}
		if items[i].isCreep != items[j].isCreep {
			return !items[i].isCreep
		}
		return items[i].tieKey < items[j].tieKey
	})

	for _, it := range items {
		if it.isCreep {
			c := it.creep
			var stackH float32
			tx := int(math.Round(float64(c.fx)))
			ty := int(math.Round(float64(c.fy)))
			if tx >= 0 && tx < GridSize && ty >= 0 && ty < GridSize {
				for _, st := range c.room.byTile[ty*GridSize+tx] {
					if h, _ := objectBoxMetrics(st.Type, cam.Zoom); h > 0 {
						stackH += h * 0.65
					}
				}
			}
			facing := p.resolveUnitFacing(c.obj, c.fx, c.fy, c.gfx, c.gfy)
			cYaw := objYaw + facing
			drawObject(cv, &path, it.cx, it.cy-stackH, halfW, halfH, cam.Zoom, float32(math.Cos(cYaw)), float32(math.Sin(cYaw)), animDur, animSec, c.obj)
			continue
		}
		if objs, ok := it.r.byTile[it.y*GridSize+it.x]; ok {
			var stackH float32
			for _, obj := range objs {
				if obj.Type == "road" || obj.Type == "rampart" || obj.Type == "constructionSite" {
					continue
				}
				oCos, oSin := objCos, objSin
				if obj.Type == "tower" {
					if ang, ok := p.resolveTowerFacing(obj); ok {
						tYaw := objYaw + ang
						oCos, oSin = float32(math.Cos(tYaw)), float32(math.Sin(tYaw))
					}
				}
				drawObject(cv, &path, it.cx, it.cy-stackH, halfW, halfH, cam.Zoom, oCos, oSin, animDur, animSec, obj)
				if h, _ := objectBoxMetrics(obj.Type, cam.Zoom); h > 0 {
					stackH += h * 0.65
				}
			}
		}
	}

	// Room Boundary Delineation: crisp translucent borders along every rendered 50x50 room
	if multiRoom {
		for _, r := range visRooms {
			if !r.isCenter {
				drawRoomBorderYaw(cv, &path, r, originX, originY, halfW, halfH, cosY, sinY, hexAlpha(0x78909c, 0.35), 1.0)
			}
		}
		for _, r := range visRooms {
			if r.isCenter {
				drawRoomBorderYaw(cv, &path, r, originX, originY, halfW, halfH, cosY, sinY, hexAlpha(0x4fc3f7, 0.55), 1.5)
			}
		}
	}

	// Pass 3: Action beams (harvest, upgrade, build, repair, attack, heal)
	for _, r := range visRooms {
		for _, obj := range r.snap.Objects {
			if obj.Raw == nil {
				continue
			}
			al, ok := obj.Raw["actionLog"].(map[string]any)
			if !ok || len(al) == 0 {
				continue
			}
			sx, sy := float32(obj.X), float32(obj.Y)
			if (obj.Type == "creep" || obj.Type == "powerCreep") && p.CreepPos != nil {
				if pos, ok := p.CreepPos[obj.ID]; ok {
					sx, sy = pos[0], pos[1]
				}
			}
			srx, sry := rotateYawF(float32(r.offX)+sx, float32(r.offY)+sy, cosY, sinY)
			scx := originX + (srx-sry)*halfW
			scy := originY + (srx+sry)*halfH - 5*cam.Zoom

			drawActionBeam(cv, &path, scx, scy, originX, originY, halfW, halfH, cam.Zoom, cosY, sinY, r.offX, r.offY, r.byTile, al)
		}
	}

	// Pass 4: RoomVisual overlay (if enabled)
	if p.ShowVisuals {
		for _, r := range visRooms {
			if len(r.snap.Visuals) == 0 {
				continue
			}
			for _, v := range r.snap.Visuals {
				drawVisualOp(cv, &path, originX, originY, halfW, halfH, cam.Zoom, cosY, sinY, float32(r.offX), float32(r.offY), v)
			}
		}
	}

	cv.Restore()
}

func drawIsoEllipse(cv *paint.Canvas, p *paint.Path, cx, cy, rx, ry float32, fill geom.Color, stroke *paint.Stroke) {
	const n = 20
	p.Reset()
	for i := 0; i < n; i++ {
		ang := float64(i) * (2 * math.Pi / n)
		x := cx + rx*float32(math.Cos(ang))
		y := cy + ry*float32(math.Sin(ang))
		if i == 0 {
			p.MoveTo(x, y)
		} else {
			p.LineTo(x, y)
		}
	}
	p.Close()
	cv.DrawPath(p, paint.Paint{Color: fill, Stroke: stroke})
}

func drawDiamond(cv *paint.Canvas, p *paint.Path, cx, cy, hw, hh float32, fill, stroke geom.Color) {
	p.Reset()
	p.MoveTo(cx, cy-hh)
	p.LineTo(cx+hw, cy)
	p.LineTo(cx, cy+hh)
	p.LineTo(cx-hw, cy)
	p.Close()
	var st *paint.Stroke
	if stroke.A > 0 {
		st = &paint.Stroke{Width: 0.75, Color: stroke}
	}
	cv.DrawPath(p, paint.Paint{Color: fill, Stroke: st})
}

func drawIsoBox(cv *paint.Canvas, p *paint.Path, cx, cy, hw, hh, h float32, top, left, right geom.Color) {
	drawIsoBoxCulled(cv, p, cx, cy, hw, hh, h, top, left, right, true, true)
}

func drawIsoBoxCulled(cv *paint.Canvas, p *paint.Path, cx, cy, hw, hh, h float32, top, left, right geom.Color, drawLeft, drawRight bool) {
	if drawLeft {
		p.Reset()
		p.MoveTo(cx-hw, cy-h)
		p.LineTo(cx, cy+hh-h)
		p.LineTo(cx, cy+hh)
		p.LineTo(cx-hw, cy)
		p.Close()
		cv.DrawPath(p, paint.Paint{Color: left})
	}

	if drawRight {
		p.Reset()
		p.MoveTo(cx, cy+hh-h)
		p.LineTo(cx+hw, cy-h)
		p.LineTo(cx+hw, cy)
		p.LineTo(cx, cy+hh)
		p.Close()
		cv.DrawPath(p, paint.Paint{Color: right})
	}

	// Top face with subtle beveled inner plateau (skip inner bevel when tiny/zoomed out)
	if hw >= 6 {
		p.Reset()
		p.MoveTo(cx, cy-hh-h)
		p.LineTo(cx+hw, cy-h)
		p.LineTo(cx, cy+hh-h)
		p.LineTo(cx-hw, cy-h)
		p.Close()
		cv.DrawPath(p, paint.Paint{Color: darken(top, 0.92)})

		ihw, ihh := hw*0.82, hh*0.82
		p.Reset()
		p.MoveTo(cx, cy-ihh-h)
		p.LineTo(cx+ihw, cy-h)
		p.LineTo(cx, cy+ihh-h)
		p.LineTo(cx-ihw, cy-h)
		p.Close()
		cv.DrawPath(p, paint.Paint{Color: top})
	} else {
		p.Reset()
		p.MoveTo(cx, cy-hh-h)
		p.LineTo(cx+hw, cy-h)
		p.LineTo(cx, cy+hh-h)
		p.LineTo(cx-hw, cy-h)
		p.Close()
		cv.DrawPath(p, paint.Paint{Color: top})
	}
}

func drawSoftShadow(cv *paint.Canvas, p *paint.Path, cx, cy, rx, ry float32) {
	// Two-layer elliptical drop shadow shifted down-right to match upper-left sun.
	sx, sy := cx+rx*0.14, cy+ry*0.16
	drawIsoEllipse(cv, p, sx, sy, rx*1.12, ry*1.12, hexAlpha(0x000000, 0.22), nil)
	drawIsoEllipse(cv, p, sx, sy, rx*0.82, ry*0.82, hexAlpha(0x000000, 0.34), nil)
}

func objEnergy(obj screeps.RoomObject) (int, int) {
	en := obj.Energy
	capVal := 0
	if obj.Raw != nil {
		if v, ok := obj.Raw["energyCapacity"].(float64); ok {
			capVal = int(v)
		}
		if st, ok := obj.Raw["store"].(map[string]any); ok {
			if v, ok := st["energy"].(float64); ok {
				en = int(v)
			}
		}
		if sc, ok := obj.Raw["storeCapacityResource"].(map[string]any); ok {
			if v, ok := sc["energy"].(float64); ok && int(v) > capVal {
				capVal = int(v)
			}
		}
		if capVal == 0 {
			if v, ok := obj.Raw["storeCapacity"].(float64); ok {
				capVal = int(v)
			}
		}
	}
	return en, capVal
}

func creepRoleFlags(obj screeps.RoomObject) (hasAttack, hasHeal, hasWork, hasCarry bool) {
	if obj.Raw == nil {
		return false, false, false, false
	}
	body, ok := obj.Raw["body"].([]any)
	if !ok {
		return false, false, false, false
	}
	for _, raw := range body {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch m["type"] {
		case "attack", "ranged_attack":
			hasAttack = true
		case "heal":
			hasHeal = true
		case "work":
			hasWork = true
		case "carry":
			hasCarry = true
		}
	}
	return hasAttack, hasHeal, hasWork, hasCarry
}

func creepRoleFlag(obj screeps.RoomObject) float32 {
	// Prefer explicit role prefix in creep name (e.g. "harvester_123", "R_hauler_456", "upgrader_789").
	lower := strings.ToLower(obj.Name)
	switch {
	case strings.Contains(lower, "defender"), strings.Contains(lower, "brawler"),
		strings.Contains(lower, "skirmisher"), strings.Contains(lower, "medic"),
		strings.Contains(lower, "guardian"), strings.Contains(lower, "assault"),
		strings.Contains(lower, "healer"):
		return 5
	case strings.Contains(lower, "scout"):
		return 4
	case strings.Contains(lower, "upgrader"):
		return 3
	case strings.Contains(lower, "builder"), strings.HasPrefix(lower, "b_"):
		return 2
	case strings.Contains(lower, "hauler"), strings.Contains(lower, "transporter"):
		return 1
	case strings.Contains(lower, "harvester"), strings.Contains(lower, "miner"):
		return 0
	}
	// Fallback: infer from body parts.
	hasAttack, hasHeal, hasWork, hasCarry := creepRoleFlags(obj)
	switch {
	case hasAttack || hasHeal:
		return 5
	case !hasWork && !hasCarry:
		return 4
	case hasCarry && !hasWork:
		return 1
	default:
		return 0
	}
}

func objectActionAngle(obj screeps.RoomObject, fx, fy float32) (float64, bool) {
	if obj.Raw == nil {
		return 0, false
	}
	al, ok := obj.Raw["actionLog"].(map[string]any)
	if !ok || len(al) == 0 {
		return 0, false
	}
	for _, key := range []string{"attack", "rangedAttack", "harvest", "upgradeController", "build", "repair", "heal", "rangedHeal", "transfer", "withdraw"} {
		raw, exists := al[key]
		if !exists || raw == nil {
			continue
		}
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		tx, okX := m["x"].(float64)
		ty, okY := m["y"].(float64)
		if !okX || !okY {
			continue
		}
		dx := tx - float64(fx)
		dy := ty - float64(fy)
		if dx*dx+dy*dy > 1e-4 {
			return math.Atan2(dy, dx), true
		}
	}
	return 0, false
}

func lerpAngle(cur, target, alpha float64) float64 {
	diff := math.Mod(target-cur+math.Pi, 2*math.Pi)
	if diff < 0 {
		diff += 2 * math.Pi
	}
	diff -= math.Pi
	return cur + diff*alpha
}

func (p *Painter) resolveUnitFacing(obj screeps.RoomObject, fx, fy, gfx, gfy float32) float64 {
	if p.creepFacing == nil {
		p.creepFacing = make(map[string]float64, 64)
	}
	if p.creepLastPos == nil {
		p.creepLastPos = make(map[string][2]float32, 64)
	}
	cur, hasCur := p.creepFacing[obj.ID]

	// 1. Prioritize active work/combat target from actionLog.
	if targetAng, ok := objectActionAngle(obj, fx, fy); ok {
		if hasCur {
			cur = lerpAngle(cur, targetAng, 0.35)
		} else {
			cur = targetAng
		}
		p.creepFacing[obj.ID] = cur
		p.creepLastPos[obj.ID] = [2]float32{gfx, gfy}
		return cur
	}

	// 2. Otherwise track movement vector in world coordinates.
	if prev, ok := p.creepLastPos[obj.ID]; ok {
		dx := float64(gfx - prev[0])
		dy := float64(gfy - prev[1])
		dist2 := dx*dx + dy*dy
		if dist2 > 0.0004 && dist2 < 9.0 {
			moveAng := math.Atan2(dy, dx)
			if hasCur {
				cur = lerpAngle(cur, moveAng, 0.28)
			} else {
				cur = moveAng
			}
			p.creepFacing[obj.ID] = cur
		}
	}
	p.creepLastPos[obj.ID] = [2]float32{gfx, gfy}
	return cur
}

func (p *Painter) resolveTowerFacing(obj screeps.RoomObject) (float64, bool) {
	if p.creepFacing == nil {
		p.creepFacing = make(map[string]float64, 64)
	}
	if ang, ok := objectActionAngle(obj, float32(obj.X), float32(obj.Y)); ok {
		if cur, has := p.creepFacing[obj.ID]; has {
			ang = lerpAngle(cur, ang, 0.35)
		}
		p.creepFacing[obj.ID] = ang
		return ang, true
	}
	if cur, has := p.creepFacing[obj.ID]; has {
		return cur, true
	}
	return 0, false
}

func sdfObjectKind(typ string) (kind float32, padX, padTop, padBot float32, ok bool) {
	switch typ {
	case "spawn":
		return 1, 1.65, 1.95, 0.85, true
	case "controller":
		return 2, 1.65, 2.55, 0.85, true
	case "source":
		return 3, 1.50, 1.75, 0.80, true
	case "mineral":
		return 4, 1.50, 1.75, 0.80, true
	case "extension":
		return 5, 1.30, 1.45, 0.75, true
	case "tower":
		return 6, 1.60, 1.90, 0.85, true
	case "storage", "terminal", "factory":
		return 7, 1.60, 1.85, 0.85, true
	case "container":
		return 8, 1.25, 1.35, 0.72, true
	case "creep", "powerCreep":
		return 9, 1.40, 1.50, 0.75, true
	case "resource":
		return 10, 1.10, 1.20, 0.65, true
	case "constructedWall":
		return 11, 1.35, 1.40, 0.72, true
	case "tombstone", "ruin":
		return 12, 1.30, 1.65, 0.72, true
	default:
		return 0, 0, 0, 0, false
	}
}

func drawObject(cv *paint.Canvas, p *paint.Path, cx, cy, hw, hh, z, cosY, sinY float32, animDur time.Duration, animSec float32, obj screeps.RoomObject) {
	if obj.Type == "road" {
		drawIsoEllipse(cv, p, cx, cy, hw*0.36, hh*0.36, geom.Hex(0x4b515d), nil)
		return
	}
	if obj.Type == "rampart" {
		col := geom.Hex(0x36c778)
		if obj.User != "" {
			col = mixColor(col, userColor(obj.User), 0.35)
		}
		fill := col
		fill.A = 0.16
		rim := col
		rim.A = 0.72
		drawIsoEllipse(cv, p, cx, cy, hw*0.84, hh*0.84, fill, &paint.Stroke{Width: max(float32(1), 0.9*z), Color: rim})
		return
	}
	if obj.Type == "constructionSite" {
		col := geom.Hex(0x4fc3f7)
		if obj.User != "" {
			col = mixColor(col, userColor(obj.User), 0.45)
		}
		fill := col
		fill.A = 0.14
		stroke := col
		stroke.A = 0.80
		drawIsoEllipse(cv, p, cx, cy, hw*0.56, hh*0.56, fill,
			&paint.Stroke{Width: max(float32(1), 0.9*z), Color: stroke})
		if obj.Raw != nil {
			prog, _ := obj.Raw["progress"].(float64)
			tot, _ := obj.Raw["progressTotal"].(float64)
			if tot > 0 && prog > 0 {
				ratio := geom.Clamp32(float32(prog/tot), 0.12, 1.0)
				inner := col
				inner.A = 0.45
				drawIsoEllipse(cv, p, cx, cy, hw*0.46*ratio, hh*0.46*ratio, inner, nil)
			}
		}
		return
	}

	// Render true 3D raymarched SDF models on the GPU when available.
	if shaders.Objects != nil {
		if kind, pX, pTop, pBot, ok := sdfObjectKind(obj.Type); ok {
			var factionCol geom.Color
			switch obj.Type {
			case "source", "resource":
				factionCol = geom.Hex(0xffb300)
			case "mineral":
				factionCol = geom.Hex(0x00acc1)
			case "controller":
				if obj.User != "" {
					factionCol = mixColor(geom.Hex(0x80cbc4), userColor(obj.User), 0.55)
				} else {
					factionCol = geom.Hex(0x90a4ae)
				}
			case "spawn":
				factionCol = mixColor(geom.Hex(0x66bb6a), userColor(obj.User), 0.55)
			case "tower":
				factionCol = mixColor(geom.Hex(0xef5350), userColor(obj.User), 0.40)
			case "storage", "terminal", "factory":
				factionCol = geom.Hex(0xab47bc)
			case "container":
				factionCol = geom.Hex(0x8d6e63)
			case "constructedWall":
				factionCol = geom.Hex(0x607d8b)
			default:
				factionCol = userColor(obj.User)
			}

			en, capVal := objEnergy(obj)
			var enRatio float32
			if capVal > 0 {
				enRatio = geom.Clamp32(float32(en)/float32(capVal), 0, 1)
			} else if en > 0 {
				enRatio = 1
			}

			var flags float32
			if obj.Type == "spawn" && len(obj.Spawning) > 0 {
				flags = 4
			} else if obj.Type == "creep" || obj.Type == "powerCreep" {
				flags = creepRoleFlag(obj)
			}

			rx := hw * pX
			rTop := hw * pTop
			rBot := hw * pBot
			rect := geom.Rect{
				X: cx - rx,
				Y: cy - rTop,
				W: rx * 2,
				H: rTop + rBot,
			}
			cv.DrawShader(rect, paint.ShaderCall{
				Shader: shaders.Objects,
				Time:   animDur,
				Params: paint.ShaderParams{
					kind, cosY, sinY, enRatio,
					factionCol.R, factionCol.G, factionCol.B, flags,
					rx, rTop, hw, 0,
				},
			})

			// Crisp HP bar above damaged creeps
			if (obj.Type == "creep" || obj.Type == "powerCreep") && obj.HitsMax > 0 && obj.Hits < obj.HitsMax {
				h, _ := objectBoxMetrics(obj.Type, z)
				ratio := geom.Clamp32(float32(obj.Hits)/float32(obj.HitsMax), 0, 1)
				barW := hw * 1.1
				barH := max(float32(2), 2.5*z)
				bx := cx - barW/2
				by := cy - hh - (h + 5*z)
				hpCol := geom.Hex(0x66bb6a)
				if ratio < 0.35 {
					hpCol = geom.Hex(0xef5350)
				} else if ratio < 0.70 {
					hpCol = geom.Hex(0xffb74d)
				}
				cv.DrawRect(geom.Rect{X: bx, Y: by, W: barW, H: barH}, paint.Paint{Color: geom.Hex(0x1a2228)})
				cv.DrawRect(geom.Rect{X: bx, Y: by, W: barW * ratio, H: barH}, paint.Paint{Color: hpCol})
			}
			return
		}
	}

	h, sc := objectBoxMetrics(obj.Type, z)
	switch obj.Type {
	case "constructedWall":
		drawSoftShadow(cv, p, cx, cy, hw*0.88, hh*0.88)
		drawIsoBox(cv, p, cx, cy, hw*sc, hh*sc, h,
			geom.Hex(0x5a6270), geom.Hex(0x404652), geom.Hex(0x2d323b))
	case "source":
		// Soft warm glow + shadow + dark volcanic pedestal + 3 glowing golden crystal spires
		pulse := float32(0.5 + 0.5*math.Sin(float64(animSec)*2.2+float64(obj.X)))
		drawSoftShadow(cv, p, cx, cy, hw*0.85, hh*0.85)
		drawIsoEllipse(cv, p, cx, cy, hw*0.98, hh*0.98, hexAlpha(0xffb300, 0.10+0.06*pulse), nil)
		bTop, bLeft, bRight := tintedBase(geom.Hex(0xffb300))
		drawIsoOctagon(cv, p, cx, cy, hw*0.76, hh*0.76, 2.4*z, bTop, bLeft, bRight)
		drawIsoPyramid(cv, p, cx-hw*0.18, cy-2*z, hw*0.36, hh*0.36, (11.2+0.6*pulse)*z,
			geom.Hex(0xffe082), geom.Hex(0xffb300), geom.Hex(0xe65100))
		drawIsoPyramid(cv, p, cx+hw*0.18, cy-2*z, hw*0.30, hh*0.30, 8.5*z,
			geom.Hex(0xfff59d), geom.Hex(0xfbc02d), geom.Hex(0xf57f17))
		drawIsoPyramid(cv, p, cx+hw*0.02, cy+hh*0.15-2*z, hw*0.24, hh*0.24, 6.5*z,
			geom.Hex(0xffecb3), geom.Hex(0xffca28), geom.Hex(0xef6c00))
	case "mineral":
		drawSoftShadow(cv, p, cx, cy, hw*0.78, hh*0.78)
		drawIsoEllipse(cv, p, cx, cy, hw*0.92, hh*0.92, hexAlpha(0x00e5ff, 0.10), nil)
		bTop, bLeft, bRight := tintedBase(geom.Hex(0x00acc1))
		drawIsoOctagon(cv, p, cx, cy, hw*0.72, hh*0.72, 2.4*z, bTop, bLeft, bRight)
		drawIsoPyramid(cv, p, cx-hw*0.06, cy-2*z, hw*0.42, hh*0.42, 12*z,
			geom.Hex(0x80deea), geom.Hex(0x00acc1), geom.Hex(0x006064))
		drawIsoPyramid(cv, p, cx+hw*0.22, cy-1.5*z, hw*0.22, hh*0.22, 6.5*z,
			geom.Hex(0xb2ebf2), geom.Hex(0x26c6da), geom.Hex(0x00838f))
	case "controller":
		bob := float32(math.Sin(float64(animSec)*2.0)) * 1.1 * z
		drawSoftShadow(cv, p, cx, cy, hw*0.94, hh*0.94)
		top := geom.Hex(0x90a4ae)
		left := geom.Hex(0x607d8b)
		right := geom.Hex(0x455a64)
		core := geom.Hex(0xb0bec5)
		if obj.User != "" {
			uCol := userColor(obj.User)
			top = mixColor(geom.Hex(0x80cbc4), uCol, 0.55)
			left = darken(top, 0.78)
			right = darken(top, 0.58)
			core = mixColor(top, geom.Hex(0xffffff), 0.45)
			aura := uCol
			aura.A = 0.14
			drawIsoEllipse(cv, p, cx, cy, hw*1.08, hh*1.08, aura, nil)
		}
		bTop, bLeft, bRight := tintedBase(top)
		drawIsoOctagon(cv, p, cx, cy, hw*0.88, hh*0.88, 3.5*z, bTop, bLeft, bRight)
		drawIsoPyramid(cv, p, cx, cy-3.5*z, hw*0.52, hh*0.52, 13.5*z, top, left, right)
		drawIsoPyramid(cv, p, cx, cy-17.5*z+bob, hw*0.24, hh*0.24, 4.5*z, core, darken(core, 0.8), darken(core, 0.6))
	case "spawn":
		uCol := userColor(obj.User)
		ringCol := mixColor(geom.Hex(0x66bb6a), uCol, 0.50)
		spawning := len(obj.Spawning) > 0
		pulse := float32(0.5 + 0.5*math.Sin(float64(animSec)*5.0))
		if spawning {
			ringCol = mixColor(ringCol, geom.Hex(0x00e676), 0.45+0.25*pulse)
		}
		drawSoftShadow(cv, p, cx, cy, hw*0.96, hh*0.96)
		if spawning {
			glow := ringCol
			glow.A = 0.16 + 0.08*pulse
			drawIsoEllipse(cv, p, cx, cy, hw*1.12, hh*1.12, glow, nil)
		}
		bTop, bLeft, bRight := tintedBase(ringCol)
		drawIsoOctagon(cv, p, cx, cy, hw*0.88, hh*0.88, 4.5*z, bTop, bLeft, bRight)
		drawIsoOctagon(cv, p, cx, cy-4.5*z, hw*0.64, hh*0.64, 2.5*z,
			ringCol, darken(ringCol, 0.78), darken(ringCol, 0.62))
		// Inner dark reactor well + proportional golden energy core
		drawIsoEllipse(cv, p, cx, cy-7.1*z, hw*0.36, hh*0.36, geom.Hex(0x161d22), nil)
		en, capVal := objEnergy(obj)
		if capVal <= 0 {
			capVal = 300
		}
		if en > 0 {
			ratio := geom.Clamp32(float32(en)/float32(capVal), 0.2, 1.0)
			drawIsoEllipse(cv, p, cx, cy-7.2*z, hw*0.30*ratio, hh*0.30*ratio, geom.Hex(0xffe082), nil)
		}
	case "extension":
		en, capVal := objEnergy(obj)
		if capVal <= 0 {
			capVal = 50
		}
		ratio := geom.Clamp32(float32(en)/float32(capVal), 0, 1)
		capCol := mixColor(geom.Hex(0x525742), geom.Hex(0xffee58), ratio)
		drawSoftShadow(cv, p, cx, cy, hw*0.64, hh*0.64)
		if ratio > 0.8 {
			drawIsoEllipse(cv, p, cx, cy, hw*0.74, hh*0.74, hexAlpha(0xffee58, 0.10), nil)
		}
		bTop, bLeft, bRight := tintedBase(capCol)
		drawIsoOctagon(cv, p, cx, cy, hw*0.58, hh*0.58, 2.4*z, bTop, bLeft, bRight)
		drawIsoOctagon(cv, p, cx, cy-2.4*z, hw*(0.24+0.18*ratio), hh*(0.24+0.18*ratio), (1.2+1.0*ratio)*z,
			capCol, darken(capCol, 0.82), darken(capCol, 0.68))
	case "tower":
		uCol := userColor(obj.User)
		turretCol := mixColor(geom.Hex(0xef5350), uCol, 0.35)
		drawSoftShadow(cv, p, cx, cy, hw*0.86, hh*0.86)
		bTop, bLeft, bRight := tintedBase(turretCol)
		drawIsoOctagon(cv, p, cx, cy, hw*0.78, hh*0.78, 4.5*z, bTop, bLeft, bRight)
		drawIsoOctagon(cv, p, cx, cy-4.5*z, hw*0.48, hh*0.48, 6.2*z,
			turretCol, darken(turretCol, 0.82), darken(turretCol, 0.62))
		// Metallic cannon barrel + bright muzzle tip
		p.Reset()
		p.MoveTo(cx, cy-9.5*z)
		p.LineTo(cx+hw*0.62, cy-7.5*z)
		p.LineTo(cx+hw*0.62, cy-5.6*z)
		p.LineTo(cx, cy-7.6*z)
		p.Close()
		cv.DrawPath(p, paint.Paint{Color: geom.Hex(0xcfd8dc)})
		drawIsoEllipse(cv, p, cx+hw*0.62, cy-6.5*z, hw*0.10, hh*0.10, geom.Hex(0xffab40), nil)
	case "storage", "terminal", "factory":
		bTop, bLeft, bRight := tintedBase(geom.Hex(0xab47bc))
		drawSoftShadow(cv, p, cx, cy, hw*0.92, hh*0.92)
		drawIsoBox(cv, p, cx, cy, hw*0.84, hh*0.84, 5*z, bTop, bLeft, bRight)
		drawIsoOctagon(cv, p, cx, cy-5*z, hw*0.60, hh*0.60, 6.5*z,
			geom.Hex(0xab47bc), geom.Hex(0x8e24aa), geom.Hex(0x6a1b9a))
	case "container":
		drawSoftShadow(cv, p, cx, cy, hw*0.68, hh*0.68)
		drawIsoBox(cv, p, cx, cy, hw*sc, hh*sc, h,
			geom.Hex(0x8d6e63), geom.Hex(0x6d4c41), geom.Hex(0x4e342e))
		en, capVal := objEnergy(obj)
		if capVal <= 0 {
			capVal = 2000
		}
		if en > 0 {
			ratio := geom.Clamp32(float32(en)/float32(capVal), 0.15, 1.0)
			drawDiamond(cv, p, cx, cy-h-0.3*z, hw*sc*0.68*ratio, hh*sc*0.68*ratio, geom.Hex(0xffd54f), geom.Color{})
		}
	case "resource":
		drawSoftShadow(cv, p, cx, cy, hw*0.42, hh*0.42)
		drawIsoEllipse(cv, p, cx, cy, hw*0.52, hh*0.52, hexAlpha(0xfff176, 0.16), nil)
		drawIsoPyramid(cv, p, cx, cy, hw*0.36, hh*0.36, 4.5*z,
			geom.Hex(0xfff176), geom.Hex(0xfbc02d), geom.Hex(0xf57f17))
	case "tombstone", "ruin":
		drawSoftShadow(cv, p, cx, cy, hw*0.55, hh*0.55)
		drawIsoBox(cv, p, cx, cy, hw*sc, hh*sc, h,
			geom.Hex(0x78909c), geom.Hex(0x546e7a), geom.Hex(0x37474f))
	case "creep", "powerCreep":
		col := userColor(obj.User)
		bTop, bLeft, bRight := tintedBase(col)
		hasAttack, hasHeal, _, _ := creepRoleFlags(obj)
		en, _ := objEnergy(obj)

		drawSoftShadow(cv, p, cx, cy, hw*0.62, hh*0.62)
		if hasAttack {
			// Crisp tactical combat ring around brawlers/defenders/skirmishers
			drawIsoEllipse(cv, p, cx, cy, hw*0.70, hh*0.70, hexAlpha(0xff5252, 0.12),
				&paint.Stroke{Width: max(float32(0.8), 0.75*z), Color: hexAlpha(0xff8a65, 0.70)})
		} else if hasHeal {
			drawIsoEllipse(cv, p, cx, cy, hw*0.70, hh*0.70, hexAlpha(0x66bb6a, 0.14),
				&paint.Stroke{Width: max(float32(0.8), 0.75*z), Color: hexAlpha(0x66bb6a, 0.75)})
		}
		drawIsoOctagon(cv, p, cx, cy, hw*0.56, hh*0.56, 2.2*z, bTop, bLeft, bRight)
		drawIsoOctagon(cv, p, cx, cy-2.2*z, hw*0.44, hh*0.44, 2.8*z,
			col, darken(col, 0.78), darken(col, 0.62))
		coreCol := mixColor(col, geom.Hex(0xffffff), 0.65)
		if en > 0 {
			coreCol = geom.Hex(0xffe082) // glowing gold core when carrying energy
		}
		drawIsoOctagon(cv, p, cx, cy-5.0*z, hw*0.21, hh*0.21, 1.0*z,
			coreCol, darken(coreCol, 0.85), darken(coreCol, 0.7))

		if obj.HitsMax > 0 && obj.Hits < obj.HitsMax {
			ratio := geom.Clamp32(float32(obj.Hits)/float32(obj.HitsMax), 0, 1)
			barW := hw * 1.1
			barH := max(float32(2), 2.5*z)
			bx := cx - barW/2
			by := cy - hh - (h + 5*z)
			hpCol := geom.Hex(0x66bb6a)
			if ratio < 0.35 {
				hpCol = geom.Hex(0xef5350)
			} else if ratio < 0.70 {
				hpCol = geom.Hex(0xffb74d)
			}
			cv.DrawRect(geom.Rect{X: bx, Y: by, W: barW, H: barH}, paint.Paint{Color: geom.Hex(0x1a2228)})
			cv.DrawRect(geom.Rect{X: bx, Y: by, W: barW * ratio, H: barH}, paint.Paint{Color: hpCol})
		}
	default:
		drawSoftShadow(cv, p, cx, cy, hw*0.70, hh*0.70)
		drawIsoBox(cv, p, cx, cy, hw*sc, hh*sc, h,
			geom.Hex(0xb0bec5), geom.Hex(0x78909c), geom.Hex(0x546e7a))
	}
}

func drawIsoPyramid(cv *paint.Canvas, p *paint.Path, cx, cy, hw, hh, h float32, left, right, base geom.Color) {
	// Left-front face
	p.Reset()
	p.MoveTo(cx, cy-h-hh*0.3)
	p.LineTo(cx, cy+hh)
	p.LineTo(cx-hw, cy)
	p.Close()
	cv.DrawPath(p, paint.Paint{Color: left})

	// Right-front face
	p.Reset()
	p.MoveTo(cx, cy-h-hh*0.3)
	p.LineTo(cx+hw, cy)
	p.LineTo(cx, cy+hh)
	p.Close()
	cv.DrawPath(p, paint.Paint{Color: right, Stroke: &paint.Stroke{Width: 0.6, Color: base}})
}

func drawIsoOctagon(cv *paint.Canvas, p *paint.Path, cx, cy, hw, hh, h float32, top, left, right geom.Color) {
	// Smooth 16-segment isometric cylinder with directional shading across the curved front wall.
	const n = 16
	var pts [n]geom.Offset
	for i := 0; i < n; i++ {
		ang := float64(i) * (2 * math.Pi / n)
		pts[i] = geom.Offset{
			X: cx + hw*float32(math.Cos(ang)),
			Y: cy + hh*float32(math.Sin(ang)),
		}
	}

	// Front-facing half of the cylinder (angles 0..pi, indices 0..n/2)
	for i := 0; i < n/2; i++ {
		a := pts[i]
		b := pts[i+1]
		t := float32(i) / float32(n/2-1) // 0=right, 1=left
		col := mixColor(right, left, t)
		p.Reset()
		p.MoveTo(a.X, a.Y-h)
		p.LineTo(b.X, b.Y-h)
		p.LineTo(b.X, b.Y)
		p.LineTo(a.X, a.Y)
		p.Close()
		cv.DrawPath(p, paint.Paint{Color: col})
	}

	// Smooth top ellipse with subtle bright rim
	p.Reset()
	p.MoveTo(pts[0].X, pts[0].Y-h)
	for i := 1; i < n; i++ {
		p.LineTo(pts[i].X, pts[i].Y-h)
	}
	p.Close()
	rim := mixColor(top, geom.Hex(0xffffff), 0.18)
	rim.A = 0.55
	cv.DrawPath(p, paint.Paint{Color: top, Stroke: &paint.Stroke{Width: 0.65, Color: rim}})
}

func isSpawningCreep(obj screeps.RoomObject) bool {
	if obj.Type != "creep" && obj.Type != "powerCreep" {
		return false
	}
	if obj.Raw == nil {
		return false
	}
	b, ok := obj.Raw["spawning"].(bool)
	return ok && b
}

func layerOrder(typ string) int {
	switch typ {
	case "road":
		return 0
	case "rampart":
		return 1
	case "constructionSite", "resource", "tombstone", "ruin":
		return 2
	case "creep", "powerCreep":
		return 4
	default:
		return 3
	}
}

func userColor(user string) geom.Color {
	switch user {
	case "2": // Invader
		return geom.Hex(0xef5350)
	case "3": // Source Keeper
		return geom.Hex(0xff7043)
	case "":
		return geom.Hex(0xbdbdbd)
	case "f6d0563cab8ac07": // Hivemind
		return geom.Hex(0x4c78f0)
	case "adcd51f1dcf2c09": // StarterBot (Raider)
		return geom.Hex(0xff5252)
	default:
		palette := []uint32{
			0x4c78f0, // cobalt-periwinkle blue
			0xff5252, // crimson
			0xab47bc, // violet
			0xffb74d, // amber
			0x26a69a, // teal
			0xec407a, // rose
			0x9ccc65, // lime
			0x7e57c2, // indigo
		}
		var h uint32 = 2166136261
		for i := 0; i < len(user); i++ {
			h ^= uint32(user[i])
			h *= 16777619
		}
		return geom.Hex(palette[int(h)%len(palette)])
	}
}

func hexAlpha(rgb uint32, a float32) geom.Color {
	c := geom.Hex(rgb)
	c.A = a
	return c
}

func drawActionBeam(cv *paint.Canvas, p *paint.Path, sx, sy, originX, originY, hw, hh, z, cosY, sinY float32, offX, offY int, byTile map[int][]screeps.RoomObject, al map[string]any) {
	beams := []struct {
		key string
		col geom.Color
	}{
		{"harvest", geom.Hex(0xffd54f)},
		{"upgradeController", geom.Hex(0x4dd0e1)},
		{"build", geom.Hex(0x81d4fa)},
		{"repair", geom.Hex(0x81d4fa)},
		{"attack", geom.Hex(0xef5350)},
		{"rangedAttack", geom.Hex(0xef5350)},
		{"heal", geom.Hex(0x66bb6a)},
		{"rangedHeal", geom.Hex(0x66bb6a)},
	}
	for _, b := range beams {
		tgt, ok := al[b.key].(map[string]any)
		if !ok {
			continue
		}
		tx, ok1 := tgt["x"].(float64)
		ty, ok2 := tgt["y"].(float64)
		if !ok1 || !ok2 {
			continue
		}
		ix, iy := int(tx), int(ty)
		var targetH float32
		if ix >= 0 && ix < GridSize && iy >= 0 && iy < GridSize {
			for _, st := range byTile[iy*GridSize+ix] {
				if h, _ := objectBoxMetrics(st.Type, z); h > targetH {
					targetH = h * 0.6
				}
			}
		}

		trx, try := rotateYawF(float32(offX)+float32(tx), float32(offY)+float32(ty), cosY, sinY)
		tcx := originX + (trx-try)*hw
		tcy := originY + (trx+try)*hh - targetH

		p.Reset()
		p.MoveTo(sx, sy)
		p.LineTo(tcx, tcy)

		// Outer soft glow
		glow := b.col
		glow.A = 0.25
		cv.DrawPath(p, paint.Paint{Stroke: &paint.Stroke{Width: max(float32(1.5), 2.0*z), Color: glow}})

		// Crisp inner core
		core := b.col
		core.A = 0.90
		cv.DrawPath(p, paint.Paint{Stroke: &paint.Stroke{Width: max(float32(0.75), 0.75*z), Color: core}})

		// Impact spark at target
		sr := max(float32(1.5), 2.2*z)
		drawIsoEllipse(cv, p, tcx, tcy, sr, sr*0.55, core, nil)
	}
}

func drawVisualOp(cv *paint.Canvas, p *paint.Path, originX, originY, hw, hh, z, cosY, sinY, offX, offY float32, v screeps.VisualOp) {
	isoPt := func(gx, gy float32) (float32, float32) {
		rx, ry := rotateYawF(offX+gx, offY+gy, cosY, sinY)
		return originX + (rx-ry)*hw, originY + (rx+ry)*hh
	}
	opac := geom.Clamp32(v.Opac, 0.1, 1.0)
	var fillCol geom.Color
	if v.Fill != "" && !strings.EqualFold(v.Fill, "transparent") && !strings.EqualFold(v.Fill, "none") {
		fillCol = parseHexColor(v.Fill, geom.Hex(0xffffff), opac)
	}
	var strokeCol geom.Color
	if v.Col != "" && !strings.EqualFold(v.Col, "transparent") && !strings.EqualFold(v.Col, "none") {
		strokeCol = parseHexColor(v.Col, geom.Hex(0xffffff), opac)
	} else if fillCol.A == 0 {
		strokeCol = parseHexColor("", geom.Hex(0xffffff), opac)
	}

	makeStroke := func(w float32) *paint.Stroke {
		if strokeCol.A <= 0 {
			return nil
		}
		return &paint.Stroke{Width: w, Color: strokeCol}
	}

	switch v.Type {
	case "l": // line
		x1, y1 := isoPt(v.X, v.Y)
		x2, y2 := isoPt(v.X2, v.Y2)
		p.Reset()
		p.MoveTo(x1, y1)
		p.LineTo(x2, y2)
		cv.DrawPath(p, paint.Paint{Stroke: makeStroke(max(float32(1), v.SW*hw))})
	case "c": // circle
		r := v.R
		if r <= 0 {
			r = 0.35
		}
		cx, cy := isoPt(v.X, v.Y)
		drawIsoEllipse(cv, p, cx, cy, r*hw, r*hh, fillCol, makeStroke(max(float32(0.75), v.SW*hw*0.5)))
	case "r": // rect
		x1, y1 := isoPt(v.X, v.Y)
		x2, y2 := isoPt(v.X+v.W, v.Y)
		x3, y3 := isoPt(v.X+v.W, v.Y+v.H)
		x4, y4 := isoPt(v.X, v.Y+v.H)
		p.Reset()
		p.MoveTo(x1, y1)
		p.LineTo(x2, y2)
		p.LineTo(x3, y3)
		p.LineTo(x4, y4)
		p.Close()
		cv.DrawPath(p, paint.Paint{
			Color:  fillCol,
			Stroke: makeStroke(max(float32(0.75), v.SW*hw*0.5)),
		})
	case "p": // poly
		if len(v.Pts) < 2 {
			return
		}
		p.Reset()
		x0, y0 := isoPt(v.Pts[0][0], v.Pts[0][1])
		p.MoveTo(x0, y0)
		for _, pt := range v.Pts[1:] {
			px, py := isoPt(pt[0], pt[1])
			p.LineTo(px, py)
		}
		cv.DrawPath(p, paint.Paint{
			Color:  fillCol,
			Stroke: makeStroke(max(float32(1), v.SW*hw)),
		})
	}
}

func parseHexColor(s string, fallback geom.Color, alpha float32) geom.Color {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) == 3 {
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	if len(s) != 6 {
		c := fallback
		c.A = alpha
		return c
	}
	val, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		c := fallback
		c.A = alpha
		return c
	}
	c := geom.Hex(uint32(val))
	c.A = alpha
	return c
}

func tintedBase(accent geom.Color) (top, left, right geom.Color) {
	slate := geom.Hex(0x607d8b)
	top = mixColor(slate, accent, 0.28)
	left = darken(top, 0.78)
	right = darken(top, 0.60)
	return top, left, right
}

func mixColor(a, b geom.Color, t float32) geom.Color {
	return geom.Color{
		R: a.R*(1-t) + b.R*t,
		G: a.G*(1-t) + b.G*t,
		B: a.B*(1-t) + b.B*t,
		A: a.A*(1-t) + b.A*t,
	}
}

func darken(c geom.Color, factor float32) geom.Color {
	return geom.Color{
		R: c.R * factor,
		G: c.G * factor,
		B: c.B * factor,
		A: c.A,
	}
}
