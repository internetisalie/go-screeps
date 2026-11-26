package engine

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"sort"
	"strings"
	"sync"

	"go-screeps/internal/screeps"
)

// BodyPart represents a single body part of a creep.
type BodyPart struct {
	Type  string
	Hits  int
	Boost string
}

// SpawningInfo holds active spawning state on a StructureSpawn.
type SpawningInfo struct {
	Name       string
	NeedTime   int
	SpawnTime  int64
	CreepID    string
	Directions []int
}

// ActionTarget records the target tile of an action performed during the tick.
type ActionTarget struct {
	X int
	Y int
}

// Entity represents any live game object inside a room.
type Entity struct {
	ID   string
	Type string
	Room string
	X    int
	Y    int
	User string
	Name string

	Hits    int
	HitsMax int

	Store                 map[string]int
	StoreCapacity         int
	StoreCapacityResource map[string]int

	// Creep fields
	Body     []BodyPart
	Fatigue  int
	AgeTime  int64
	Spawning bool
	SayText  string

	// Spawn fields
	SpawningInfo *SpawningInfo

	// Source fields
	Energy               int
	EnergyCapacity       int
	NextRegenerationTime int64

	// Mineral fields
	MineralType   string
	MineralAmount int

	// Controller / ConstructionSite fields
	Level         int
	Progress      int
	ProgressTotal int
	DowngradeTime     int64
	SafeMode          int64
	SafeModeAvailable int
	SafeModeCooldown  int64
	StructureType     string // for constructionSite

	// Dropped resource fields
	ResourceType string
	Amount       int

	// Tombstone fields
	CreepID   string
	CreepName string
	DeathTime int64
	DecayTime int64

	// Decay / Cooldown
	NextDecayTime int64
	Cooldown      int

	// Per-tick action log for renderer beams
	ActionLog map[string]ActionTarget
}

// UsedCapacity returns the stored amount of resource, or total stored across all resources if resource == "".
func (e *Entity) UsedCapacity(resource string) int {
	if e.Store == nil {
		return 0
	}
	if resource != "" {
		return e.Store[resource]
	}
	total := 0
	for _, v := range e.Store {
		total += v
	}
	return total
}

// Capacity returns the store capacity for the given resource (or general capacity if resource == "").
func (e *Entity) Capacity(resource string) int {
	if e.StoreCapacityResource != nil {
		if resource == "" {
			total := 0
			for _, v := range e.StoreCapacityResource {
				total += v
			}
			return total
		}
		return e.StoreCapacityResource[resource]
	}
	return e.StoreCapacity
}

// FreeCapacity returns remaining capacity for resource (or general remaining capacity if resource == "").
func (e *Entity) FreeCapacity(resource string) int {
	if e.StoreCapacityResource != nil {
		if resource == "" {
			rem := e.Capacity("") - e.UsedCapacity("")
			if rem < 0 {
				return 0
			}
			return rem
		}
		capRes, ok := e.StoreCapacityResource[resource]
		if !ok {
			return 0
		}
		rem := capRes - e.UsedCapacity(resource)
		if rem < 0 {
			return 0
		}
		return rem
	}
	rem := e.StoreCapacity - e.UsedCapacity("")
	if rem < 0 {
		return 0
	}
	return rem
}

// AddStore adds up to amount of resource to the entity's store and returns the actual amount added.
func (e *Entity) AddStore(resource string, amount int) int {
	if amount <= 0 {
		return 0
	}
	free := e.FreeCapacity(resource)
	if free <= 0 {
		return 0
	}
	add := amount
	if add > free {
		add = free
	}
	if e.Store == nil {
		e.Store = make(map[string]int)
	}
	e.Store[resource] += add
	e.syncEnergyField()
	return add
}

// SubStore removes up to amount of resource from the entity's store and returns the actual amount removed.
func (e *Entity) SubStore(resource string, amount int) int {
	if amount <= 0 || e.Store == nil {
		return 0
	}
	cur := e.Store[resource]
	if cur <= 0 {
		return 0
	}
	sub := amount
	if sub > cur {
		sub = cur
	}
	e.Store[resource] -= sub
	if e.Store[resource] == 0 && resource != RESOURCE_ENERGY {
		delete(e.Store, resource)
	}
	e.syncEnergyField()
	return sub
}

func (e *Entity) syncEnergyField() {
	switch e.Type {
	case STRUCTURE_SPAWN, STRUCTURE_EXTENSION, STRUCTURE_TOWER, STRUCTURE_LINK:
		e.Energy = e.Store[RESOURCE_ENERGY]
	}
}

// CountActiveParts returns the number of body parts of partType with Hits > 0.
func (e *Entity) CountActiveParts(partType string) int {
	n := 0
	for _, p := range e.Body {
		if p.Type == partType && p.Hits > 0 {
			n++
		}
	}
	return n
}

// TakeDamage applies damage to the entity, damaging creep body parts front-to-back.
func (e *Entity) TakeDamage(amount int) {
	if amount <= 0 || e.HitsMax <= 0 {
		return
	}
	e.Hits -= amount
	if e.Hits < 0 {
		e.Hits = 0
	}
	if e.Type == TYPE_CREEP && len(e.Body) > 0 {
		rem := amount
		for i := range e.Body {
			if rem <= 0 {
				break
			}
			if e.Body[i].Hits > 0 {
				dmg := rem
				if dmg > e.Body[i].Hits {
					dmg = e.Body[i].Hits
				}
				e.Body[i].Hits -= dmg
				rem -= dmg
			}
		}
	}
}

// HealDamage restores hits to the entity, healing creep body parts back-to-front.
func (e *Entity) HealDamage(amount int) {
	if amount <= 0 || e.HitsMax <= 0 {
		return
	}
	e.Hits += amount
	if e.Hits > e.HitsMax {
		e.Hits = e.HitsMax
	}
	if e.Type == TYPE_CREEP && len(e.Body) > 0 {
		rem := amount
		for i := len(e.Body) - 1; i >= 0 && rem > 0; i-- {
			missing := 100 - e.Body[i].Hits
			if missing > 0 {
				heal := rem
				if heal > missing {
					heal = missing
				}
				e.Body[i].Hits += heal
				rem -= heal
			}
		}
	}
}

// SetAction logs an action target for the current tick (used by viewer action beams).
func (e *Entity) SetAction(action string, x, y int) {
	if e.ActionLog == nil {
		e.ActionLog = make(map[string]ActionTarget)
	}
	e.ActionLog[action] = ActionTarget{X: x, Y: y}
}

// IsObstacle reports whether this entity blocks creep movement.
func (e *Entity) IsObstacle(moverUser string) bool {
	switch e.Type {
	case STRUCTURE_ROAD, STRUCTURE_CONTAINER, TYPE_CONSTRUCTION_SITE, TYPE_RESOURCE, TYPE_TOMBSTONE:
		return false
	case STRUCTURE_RAMPART:
		return moverUser != "" && e.User != "" && e.User != moverUser
	case TYPE_CREEP:
		return !e.Spawning
	default:
		// spawn, extension, wall, tower, storage, link, controller, source, mineral, extractor
		return true
	}
}

// IsStructure reports whether this entity is a built structure (including controller).
func (e *Entity) IsStructure() bool {
	switch e.Type {
	case STRUCTURE_SPAWN, STRUCTURE_EXTENSION, STRUCTURE_ROAD, STRUCTURE_WALL,
		STRUCTURE_RAMPART, STRUCTURE_LINK, STRUCTURE_STORAGE, STRUCTURE_TOWER,
		STRUCTURE_CONTAINER, STRUCTURE_CONTROLLER, STRUCTURE_EXTRACTOR:
		return true
	default:
		return false
	}
}

// Room holds the 50x50 terrain grid, entities, and visuals for a single Screeps room.
type Room struct {
	Name           string
	Terrain        [RoomSize * RoomSize]uint8 // 0=plain, 1=wall, 2=swamp
	Entities       map[string]*Entity
	Visuals        []screeps.VisualOp
	pendingVisuals []screeps.VisualOp
}

// TerrainAt returns the terrain code (0=plain, 1=wall, 2=swamp) at (x, y). Out-of-bounds returns wall.
func (r *Room) TerrainAt(x, y int) uint8 {
	if !InBounds(x, y) {
		return TERRAIN_MASK_WALL
	}
	return r.Terrain[y*RoomSize+x]
}

// TerrainString encodes the 50x50 terrain grid as a 2500-char string ('0', '1', '2').
func (r *Room) TerrainString() string {
	buf := make([]byte, RoomSize*RoomSize)
	for i, t := range r.Terrain {
		switch t {
		case TERRAIN_MASK_WALL:
			buf[i] = '1'
		case TERRAIN_MASK_SWAMP:
			buf[i] = '2'
		default:
			buf[i] = '0'
		}
	}
	return string(buf)
}

// EntitiesAt returns all entities located at (x, y) sorted deterministically by ID.
func (r *Room) EntitiesAt(x, y int) []*Entity {
	var out []*Entity
	for _, e := range r.Entities {
		if e.X == x && e.Y == y {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// HasRoad reports whether there is a built road at (x, y).
func (r *Room) HasRoad(x, y int) bool {
	for _, e := range r.Entities {
		if e.Type == STRUCTURE_ROAD && e.X == x && e.Y == y {
			return true
		}
	}
	return false
}

// IsWalkable reports whether (x, y) is passable terrain with no blocking structures (and optionally no creeps).
func (r *Room) IsWalkable(x, y int, ignoreCreeps bool) bool {
	return r.IsWalkableFor(x, y, "", ignoreCreeps)
}

// IsWalkableFor reports whether (x, y) is passable for moverUser.
func (r *Room) IsWalkableFor(x, y int, moverUser string, ignoreCreeps bool) bool {
	if !InBounds(x, y) {
		return false
	}
	if r.TerrainAt(x, y) == TERRAIN_MASK_WALL {
		return false
	}
	for _, e := range r.Entities {
		if e.X != x || e.Y != y {
			continue
		}
		if e.Type == TYPE_CREEP && ignoreCreeps {
			continue
		}
		if e.IsObstacle(moverUser) {
			return false
		}
	}
	return true
}

// FindController returns the room's controller entity, or nil if none.
func (r *Room) FindController() *Entity {
	for _, e := range r.Entities {
		if e.Type == STRUCTURE_CONTROLLER {
			return e
		}
	}
	return nil
}

// EnergyAvailable returns total energy across all spawns and extensions owned by user in this room.
func (r *Room) EnergyAvailable(user string) int {
	total := 0
	for _, e := range r.Entities {
		if e.User == user && (e.Type == STRUCTURE_SPAWN || e.Type == STRUCTURE_EXTENSION) {
			total += e.UsedCapacity(RESOURCE_ENERGY)
		}
	}
	return total
}

// EnergyCapacityAvailable returns total energy capacity across all spawns and extensions owned by user.
func (r *Room) EnergyCapacityAvailable(user string) int {
	total := 0
	for _, e := range r.Entities {
		if e.User == user && (e.Type == STRUCTURE_SPAWN || e.Type == STRUCTURE_EXTENSION) {
			total += e.Capacity(RESOURCE_ENERGY)
		}
	}
	return total
}

// World holds the complete simulation state across all rooms.
type World struct {
	mu         sync.RWMutex
	GameTime   int64
	Rooms      map[string]*Room
	Users      map[string]string // userID -> username
	UserByName map[string]string // username -> userID
	Status     string

	nextID  uint64
	intents []Intent
	events  []WorldEvent
}

// WorldEvent records a high-signal colony or combat milestone in the simulation.
type WorldEvent struct {
	Tick    int64
	Room    string
	Kind    string // "combat", "raid", "build", "rcl", "danger"
	Message string
}

const maxWorldEvents = 60

// recordEventLocked appends an event to the rolling ring buffer (caller must hold w.mu.Lock).
func (w *World) recordEventLocked(room, kind, msg string) {
	ev := WorldEvent{
		Tick:    w.GameTime,
		Room:    room,
		Kind:    kind,
		Message: msg,
	}
	w.events = append(w.events, ev)
	if len(w.events) > maxWorldEvents {
		w.events = w.events[len(w.events)-maxWorldEvents:]
	}
}

// RecentEvents returns up to limit most recent world events (newest first).
func (w *World) RecentEvents(limit int) []WorldEvent {
	w.mu.RLock()
	defer w.mu.RUnlock()
	n := len(w.events)
	if n == 0 {
		return nil
	}
	if limit <= 0 || limit > n {
		limit = n
	}
	out := make([]WorldEvent, limit)
	for i := 0; i < limit; i++ {
		out[i] = w.events[n-1-i]
	}
	return out
}

func (w *World) userNameLocked(userID string) string {
	if userID == "" {
		return "Neutral"
	}
	if name, ok := w.Users[userID]; ok && name != "" {
		return name
	}
	return userID
}

// NewWorld creates an empty simulation world.
func NewWorld() *World {
	return &World{
		GameTime: 1,
		Rooms:    make(map[string]*Room),
		Users: map[string]string{
			"2": "Invader",
			"3": "Source Keeper",
		},
		UserByName: map[string]string{
			"Invader":       "2",
			"Source Keeper": "3",
		},
		Status: "Local Simulation",
	}
}

// RLock acquires the world's read lock.
func (w *World) RLock() { w.mu.RLock() }

// RUnlock releases the world's read lock.
func (w *World) RUnlock() { w.mu.RUnlock() }

// Lock acquires the world's write lock.
func (w *World) Lock() { w.mu.Lock() }

// Unlock releases the world's write lock.
func (w *World) Unlock() { w.mu.Unlock() }

// SetStatus sets the status string reported in Snapshots.
func (w *World) SetStatus(status string) {
	w.mu.Lock()
	w.Status = status
	w.mu.Unlock()
}

// nextEntityID generates a deterministic unique entity ID (caller must hold w.mu.Lock).
func (w *World) nextEntityID(prefix string) string {
	w.nextID++
	return fmt.Sprintf("%s_%06d", prefix, w.nextID)
}

// EnsureUser returns the userID for username, creating a new user entry if not present.
func (w *World) EnsureUser(username string) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.ensureUserLocked(username)
}

func (w *World) ensureUserLocked(username string) string {
	if w.UserByName != nil {
		if id, ok := w.UserByName[username]; ok {
			return id
		}
	}
	for id, name := range w.Users {
		if name == username {
			if w.UserByName == nil {
				w.UserByName = make(map[string]string)
			}
			w.UserByName[username] = id
			return id
		}
	}
	w.nextID++
	id := fmt.Sprintf("u%d", w.nextID)
	w.Users[id] = username
	if w.UserByName == nil {
		w.UserByName = make(map[string]string)
	}
	w.UserByName[username] = id
	return id
}

// GetRoom returns the room by name (case-insensitive), or nil.
func (w *World) GetRoom(name string) *Room {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.Rooms[strings.ToUpper(strings.TrimSpace(name))]
}

// RoomNames returns a sorted list of all loaded room names.
func (w *World) RoomNames() []string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	names := make([]string, 0, len(w.Rooms))
	for k := range w.Rooms {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// GetEntity searches all rooms for an entity by ID.
func (w *World) GetEntity(id string) *Entity {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.getEntityLocked(id)
}

func (w *World) getEntityLocked(id string) *Entity {
	for _, r := range w.Rooms {
		if e, ok := r.Entities[id]; ok {
			return e
		}
	}
	return nil
}

// smoothNoise2D evaluates deterministic 2D bilinear value noise in [0, 1].
func smoothNoise2D(x, y float64, seed uint64) float64 {
	hash := func(ix, iy int) float64 {
		h := uint64(ix)*374761393 + uint64(iy)*668265263 + seed*1442695040888963407
		h = (h ^ (h >> 13)) * 1274126177
		h = h ^ (h >> 16)
		return float64(h&0xFFFFFF) / float64(0x1000000)
	}
	x0 := int(math.Floor(x))
	y0 := int(math.Floor(y))
	fx := x - float64(x0)
	fy := y - float64(y0)
	// Smoothstep interpolation
	ux := fx * fx * (3 - 2*fx)
	uy := fy * fy * (3 - 2*fy)
	n00 := hash(x0, y0)
	n10 := hash(x0+1, y0)
	n01 := hash(x0, y0+1)
	n11 := hash(x0+1, y0+1)
	nx0 := n00 + (n10-n00)*ux
	nx1 := n01 + (n11-n01)*ux
	return nx0 + (nx1-nx0)*uy
}

// fbm2D evaluates 2-octave fractal value noise in [0, 1].
func fbm2D(x, y float64, seed uint64) float64 {
	return 0.65*smoothNoise2D(x, y, seed) + 0.35*smoothNoise2D(x*2.1+17.3, y*2.1-9.7, seed^0x9e3779b97f4a7c15)
}

// GenerateRoom procedurally creates a playable 50x50 room with varied biome archetypes,
// organic cellular-automata mountain ranges and swamps, neighbor-matched exits,
// 2 sources, 1 mineral, 1 controller, and guaranteed BFS walkability.
func (w *World) GenerateRoom(name string, seed int64) *Room {
	w.mu.Lock()
	defer w.mu.Unlock()

	name = strings.ToUpper(strings.TrimSpace(name))
	rng := rand.New(rand.NewSource(seed))
	noiseSeed := uint64(seed)*0x517cc1b727220a95 ^ 0x2545f4914f6cdd1d
	room := &Room{
		Name:     name,
		Entities: make(map[string]*Entity),
	}

	// Pick one of 5 distinct room archetypes for rich terrain variety:
	// 0: Canyon / Ridge Valley   1: Marshland / Bayou   2: Fortress / Caldera
	// 3: Archipelago / Mesas     4: Twin Basins / Divide
	archetype := rng.Intn(5)
	wallFreq := 0.11 + rng.Float64()*0.07
	swampFreq := 0.10 + rng.Float64()*0.07
	angle := rng.Float64() * math.Pi // random ridge/river orientation

	var wallGrid [RoomSize * RoomSize]bool
	var swampGrid [RoomSize * RoomSize]bool

	// 1. Seed organic heightfields according to archetype.
	cosA, sinA := math.Cos(angle), math.Sin(angle)
	for y := 0; y < RoomSize; y++ {
		for x := 0; x < RoomSize; x++ {
			idx := y*RoomSize + x
			// Edge distance (0 on border, 24.5 at center).
			edgeDist := float64(min(min(x, RoomSize-1-x), min(y, RoomSize-1-y)))
			dx := float64(x) - 24.5
			dy := float64(y) - 24.5
			rCenter := math.Hypot(dx, dy) / 24.5
			// Rotated coordinate along ridge/river axis.
			perp := math.Abs(dx*sinA - dy*cosA)

			nWall := fbm2D(float64(x)*wallFreq, float64(y)*wallFreq, noiseSeed)
			nSwamp := fbm2D(float64(x)*swampFreq+31.7, float64(y)*swampFreq+19.3, noiseSeed^0xa5a5a5a5)

			isWall := false
			isSwamp := false

			switch archetype {
			case 0: // Canyon / Ridge Valley: sweeping mountain ridges framing a winding valley
				ridgeBias := 0.22 * math.Sin(perp*0.32)
				isWall = (nWall + ridgeBias) > 0.62
				isSwamp = nSwamp > 0.64
			case 1: // Marshland / Bayou: sparse rocks, expansive lush swamp basins
				isWall = nWall > 0.72
				isSwamp = nSwamp > 0.44
			case 2: // Fortress / Caldera: thick stepped outer mountain ring + open central plateau
				ringBias := 0.0
				if edgeDist < 6 {
					ringBias = (6.0 - edgeDist) * 0.09
				} else if rCenter < 0.42 {
					ringBias = -0.22
				}
				isWall = (nWall + ringBias) > 0.61
				isSwamp = nSwamp > 0.66
			case 3: // Archipelago / Mesas: crisp scattered rocky mesas & pillars
				nMesa := fbm2D(float64(x)*0.16, float64(y)*0.16, noiseSeed+7)
				isWall = nMesa > 0.63
				isSwamp = nSwamp > 0.58
			case 4: // Twin Basins: central ridge or swamp river splitting the room with passes
				band := math.Exp(-(perp * perp) / 26.0)
				if seed%2 == 0 {
					isWall = (nWall + band*0.30) > 0.64
					isSwamp = nSwamp > 0.62
				} else {
					isWall = nWall > 0.67
					isSwamp = (nSwamp + band*0.35) > 0.54
				}
			}

			// Natural organic outer border thickness (1-3 tiles deep).
			if edgeDist == 0 || (edgeDist <= 2 && nWall > 0.46) {
				isWall = true
			}
			// Keep central spawn basin gently open.
			if rCenter < 0.22 {
				isWall = false
			}

			wallGrid[idx] = isWall
			swampGrid[idx] = isSwamp && !isWall
		}
	}

	// 2. Cellular automata smoothing (3 passes for crisp organic walls, 2 passes for smooth swamps).
	smoothGrid := func(grid *[RoomSize * RoomSize]bool, passes, birth, survive int, keepBorder bool) {
		var next [RoomSize * RoomSize]bool
		for p := 0; p < passes; p++ {
			for y := 0; y < RoomSize; y++ {
				for x := 0; x < RoomSize; x++ {
					idx := y*RoomSize + x
					if x == 0 || x == RoomSize-1 || y == 0 || y == RoomSize-1 {
						next[idx] = keepBorder
						continue
					}
					cnt := 0
					for oy := -1; oy <= 1; oy++ {
						for ox := -1; ox <= 1; ox++ {
							if ox == 0 && oy == 0 {
								continue
							}
							if grid[(y+oy)*RoomSize+(x+ox)] {
								cnt++
							}
						}
					}
					if grid[idx] {
						next[idx] = cnt >= survive
					} else {
						next[idx] = cnt >= birth
					}
				}
			}
			*grid = next
		}
	}
	smoothGrid(&wallGrid, 3, 5, 4, true)
	smoothGrid(&swampGrid, 2, 5, 4, false)

	for i := 0; i < RoomSize*RoomSize; i++ {
		if wallGrid[i] {
			room.Terrain[i] = TERRAIN_MASK_WALL
		} else if swampGrid[i] {
			room.Terrain[i] = TERRAIN_MASK_SWAMP
		} else {
			room.Terrain[i] = TERRAIN_MASK_PLAIN
		}
	}

	// 3. Carve exits on N/S/W/E edges (matching existing neighbor rooms seamlessly when present).
	type edgeSpec struct {
		dir      int // 1=N, 3=E, 5=S, 7=W
		neighbor string
	}
	nbN, _ := AdjacentRoomName(name, 0, -1)
	nbE, _ := AdjacentRoomName(name, 1, 0)
	nbS, _ := AdjacentRoomName(name, 0, 1)
	nbW, _ := AdjacentRoomName(name, -1, 0)
	edges := []edgeSpec{
		{1, nbN},
		{3, nbE},
		{5, nbS},
		{7, nbW},
	}
	var exitAnchors [][2]int

	carveExitTile := func(dir, pos int) {
		if pos < 2 || pos > RoomSize-3 {
			return
		}
		switch dir {
		case 1: // North (y=0)
			for d := 0; d < 3; d++ {
				room.Terrain[d*RoomSize+pos] = TERRAIN_MASK_PLAIN
			}
		case 5: // South (y=49)
			for d := 0; d < 3; d++ {
				room.Terrain[(RoomSize-1-d)*RoomSize+pos] = TERRAIN_MASK_PLAIN
			}
		case 7: // West (x=0)
			for d := 0; d < 3; d++ {
				room.Terrain[pos*RoomSize+d] = TERRAIN_MASK_PLAIN
			}
		case 3: // East (x=49)
			for d := 0; d < 3; d++ {
				room.Terrain[pos*RoomSize+(RoomSize-1-d)] = TERRAIN_MASK_PLAIN
			}
		}
	}

	for _, e := range edges {
		var openPositions []int
		if nb, exists := w.Rooms[e.neighbor]; exists && nb != nil {
			// Copy matching border exit openings from already-loaded neighbor room.
			for i := 2; i < RoomSize-2; i++ {
				var nbTile uint8
				switch e.dir {
				case 1:
					nbTile = nb.Terrain[(RoomSize-1)*RoomSize+i]
				case 5:
					nbTile = nb.Terrain[0*RoomSize+i]
				case 7:
					nbTile = nb.Terrain[i*RoomSize+(RoomSize-1)]
				case 3:
					nbTile = nb.Terrain[i*RoomSize+0]
				}
				if nbTile != TERRAIN_MASK_WALL {
					carveExitTile(e.dir, i)
					openPositions = append(openPositions, i)
				}
			}
		} else {
			// Carve 1 or 2 varied exit passes (width 4..11) along this edge.
			numPasses := 1
			if rng.Intn(100) < 35 {
				numPasses = 2
			}
			for p := 0; p < numPasses; p++ {
				width := 4 + rng.Intn(8)
				var start int
				if numPasses == 2 {
					if p == 0 {
						start = 6 + rng.Intn(12)
					} else {
						start = 28 + rng.Intn(12)
					}
				} else {
					start = 8 + rng.Intn(34-width)
				}
				for i := start; i < start+width && i < RoomSize-2; i++ {
					carveExitTile(e.dir, i)
					openPositions = append(openPositions, i)
				}
			}
		}
		if len(openPositions) > 0 {
			mid := openPositions[len(openPositions)/2]
			switch e.dir {
			case 1:
				exitAnchors = append(exitAnchors, [2]int{mid, 2})
			case 5:
				exitAnchors = append(exitAnchors, [2]int{mid, RoomSize - 3})
			case 7:
				exitAnchors = append(exitAnchors, [2]int{2, mid})
			case 3:
				exitAnchors = append(exitAnchors, [2]int{RoomSize - 3, mid})
			}
		}
	}

	// Helper to clear a walkable area around (x, y) and ensure (x, y) is plain.
	clearArea := func(x, y, radius int) {
		for dy := -radius; dy <= radius; dy++ {
			for dx := -radius; dx <= radius; dx++ {
				nx, ny := x+dx, y+dy
				if nx >= 2 && nx <= RoomSize-3 && ny >= 2 && ny <= RoomSize-3 {
					if dx*dx+dy*dy <= radius*radius+1 {
						room.Terrain[ny*RoomSize+nx] = TERRAIN_MASK_PLAIN
					}
				}
			}
		}
	}

	// Clear central spawn area.
	clearArea(25, 25, 4)

	// 4. Permute the 4 quadrants so sources, controller, and mineral vary in position per room.
	quads := [4][2]int{
		{8 + rng.Intn(12), 8 + rng.Intn(12)},   // NW
		{30 + rng.Intn(12), 8 + rng.Intn(12)},  // NE
		{30 + rng.Intn(12), 30 + rng.Intn(12)}, // SE
		{8 + rng.Intn(12), 30 + rng.Intn(12)},  // SW
	}
	perm := rng.Perm(4)
	s1 := quads[perm[0]]
	s2 := quads[perm[2]]
	ctrlPos := quads[perm[1]]
	minPos := quads[perm[3]]

	for _, pos := range [][2]int{s1, s2} {
		clearArea(pos[0], pos[1], 2)
		id := w.nextEntityID("src")
		room.Entities[id] = &Entity{
			ID:             id,
			Type:           TYPE_SOURCE,
			Room:           name,
			X:              pos[0],
			Y:              pos[1],
			Energy:         SOURCE_ENERGY_CAPACITY,
			EnergyCapacity: SOURCE_ENERGY_CAPACITY,
		}
	}

	// 5. Place 1 controller.
	clearArea(ctrlPos[0], ctrlPos[1], 2)
	ctrlID := w.nextEntityID("ctrl")
	room.Entities[ctrlID] = &Entity{
		ID:            ctrlID,
		Type:          STRUCTURE_CONTROLLER,
		Room:          name,
		X:             ctrlPos[0],
		Y:             ctrlPos[1],
		Level:         0,
		Progress:      0,
		ProgressTotal: 0,
	}

	// 6. Place 1 mineral.
	minerals := []string{RESOURCE_HYDROGEN, RESOURCE_OXYGEN, RESOURCE_UTRIUM, RESOURCE_LEMERGIUM, RESOURCE_KEANIUM, RESOURCE_ZYNTHIUM, RESOURCE_CATALYST}
	clearArea(minPos[0], minPos[1], 2)
	minID := w.nextEntityID("min")
	room.Entities[minID] = &Entity{
		ID:            minID,
		Type:          TYPE_MINERAL,
		Room:          name,
		X:             minPos[0],
		Y:             minPos[1],
		MineralType:   minerals[rng.Intn(len(minerals))],
		MineralAmount: MINERAL_DENSITY_AMOUNT,
	}

	// 7. Guarantee 100% BFS walkability from center (25,25) to all POIs & exits;
	// if any target is sealed behind a ridge, carve a natural 3-tile pass to it.
	ensureConnected := func(targets [][2]int) {
		bfs := func() [RoomSize * RoomSize]bool {
			var vis [RoomSize * RoomSize]bool
			queue := make([][2]int, 0, 1024)
			vis[25*RoomSize+25] = true
			queue = append(queue, [2]int{25, 25})
			for head := 0; head < len(queue); head++ {
				cur := queue[head]
				for dy := -1; dy <= 1; dy++ {
					for dx := -1; dx <= 1; dx++ {
						if dx == 0 && dy == 0 {
							continue
						}
						nx, ny := cur[0]+dx, cur[1]+dy
						if nx <= 0 || nx >= RoomSize-1 || ny <= 0 || ny >= RoomSize-1 {
							continue
						}
						idx := ny*RoomSize + nx
						if !vis[idx] && room.Terrain[idx] != TERRAIN_MASK_WALL {
							vis[idx] = true
							queue = append(queue, [2]int{nx, ny})
						}
					}
				}
			}
			return vis
		}

		vis := bfs()
		for _, tgt := range targets {
			if vis[tgt[1]*RoomSize+tgt[0]] {
				continue
			}
			// Find closest reachable tile to tgt and carve a smooth pass.
			bestX, bestY := 25, 25
			bestD2 := 999999
			for y := 2; y < RoomSize-2; y++ {
				for x := 2; x < RoomSize-2; x++ {
					if !vis[y*RoomSize+x] {
						continue
					}
					d2 := (x-tgt[0])*(x-tgt[0]) + (y-tgt[1])*(y-tgt[1])
					if d2 < bestD2 {
						bestD2 = d2
						bestX, bestY = x, y
					}
				}
			}
			cx, cy := bestX, bestY
			for steps := 0; steps < 100 && (cx != tgt[0] || cy != tgt[1]); steps++ {
				clearArea(cx, cy, 1)
				if cx < tgt[0] {
					cx++
				} else if cx > tgt[0] {
					cx--
				}
				if cy < tgt[1] {
					cy++
				} else if cy > tgt[1] {
					cy--
				}
			}
			clearArea(tgt[0], tgt[1], 1)
			vis = bfs()
		}
	}

	poiTargets := append([][2]int{s1, s2, ctrlPos, minPos}, exitAnchors...)
	ensureConnected(poiTargets)

	w.Rooms[name] = room
	return room
}

// LoadFromDBJSON imports rooms, terrain, and static sources/minerals/controllers from a Screeps LokiJS db.json file.
func (w *World) LoadFromDBJSON(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var db struct {
		Collections []struct {
			Name string            `json:"name"`
			Data []json.RawMessage `json:"data"`
		} `json:"collections"`
	}
	if err := json.Unmarshal(raw, &db); err != nil {
		return fmt.Errorf("parse db.json: %w", err)
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	ensureRoom := func(roomName string) *Room {
		roomName = strings.ToUpper(strings.TrimSpace(roomName))
		if roomName == "" {
			return nil
		}
		if r, ok := w.Rooms[roomName]; ok {
			return r
		}
		r := &Room{
			Name:     roomName,
			Entities: make(map[string]*Entity),
		}
		w.Rooms[roomName] = r
		return r
	}

	// First pass: terrain.
	for _, col := range db.Collections {
		if col.Name != "rooms.terrain" {
			continue
		}
		for _, item := range col.Data {
			var doc struct {
				Room    string `json:"room"`
				Terrain string `json:"terrain"`
			}
			if err := json.Unmarshal(item, &doc); err != nil || len(doc.Terrain) < RoomSize*RoomSize {
				continue
			}
			r := ensureRoom(doc.Room)
			if r == nil {
				continue
			}
			for i := 0; i < RoomSize*RoomSize; i++ {
				switch doc.Terrain[i] {
				case '1', '3':
					r.Terrain[i] = TERRAIN_MASK_WALL
				case '2':
					r.Terrain[i] = TERRAIN_MASK_SWAMP
				default:
					r.Terrain[i] = TERRAIN_MASK_PLAIN
				}
			}
		}
	}

	// Second pass: static room objects (sources, minerals, controllers).
	for _, col := range db.Collections {
		if col.Name != "rooms.objects" {
			continue
		}
		for _, item := range col.Data {
			var doc struct {
				ID             string `json:"_id"`
				Room           string `json:"room"`
				Type           string `json:"type"`
				X              int    `json:"x"`
				Y              int    `json:"y"`
				Energy         int    `json:"energy"`
				EnergyCapacity int    `json:"energyCapacity"`
				MineralType    string `json:"mineralType"`
				MineralAmount  int    `json:"mineralAmount"`
			}
			if err := json.Unmarshal(item, &doc); err != nil {
				continue
			}
			r := ensureRoom(doc.Room)
			if r == nil || !InBounds(doc.X, doc.Y) {
				continue
			}
			id := doc.ID
			if id == "" {
				id = w.nextEntityID(doc.Type)
			}
			switch doc.Type {
			case TYPE_SOURCE:
				capVal := doc.EnergyCapacity
				if capVal <= 0 {
					capVal = SOURCE_ENERGY_CAPACITY
				}
				energy := doc.Energy
				if energy <= 0 {
					energy = capVal
				}
				r.Entities[id] = &Entity{
					ID:             id,
					Type:           TYPE_SOURCE,
					Room:           r.Name,
					X:              doc.X,
					Y:              doc.Y,
					Energy:         energy,
					EnergyCapacity: capVal,
				}
			case TYPE_MINERAL:
				amt := doc.MineralAmount
				if amt <= 0 {
					amt = MINERAL_DENSITY_AMOUNT
				}
				mt := doc.MineralType
				if mt == "" {
					mt = RESOURCE_HYDROGEN
				}
				r.Entities[id] = &Entity{
					ID:            id,
					Type:          TYPE_MINERAL,
					Room:          r.Name,
					X:             doc.X,
					Y:             doc.Y,
					MineralType:   mt,
					MineralAmount: amt,
				}
			case STRUCTURE_CONTROLLER:
				r.Entities[id] = &Entity{
					ID:   id,
					Type: STRUCTURE_CONTROLLER,
					Room: r.Name,
					X:    doc.X,
					Y:    doc.Y,
				}
			}
		}
	}

	return nil
}

// ImportFullDBJSON imports the full live game state (gameTime, users, terrain, and all live room objects)
// from a Screeps LokiJS db.json file.
func (w *World) ImportFullDBJSON(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var db struct {
		Collections []struct {
			Name string            `json:"name"`
			Data []json.RawMessage `json:"data"`
		} `json:"collections"`
	}
	if err := json.Unmarshal(raw, &db); err != nil {
		return fmt.Errorf("parse db.json: %w", err)
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.Users == nil {
		w.Users = make(map[string]string)
	}
	if w.UserByName == nil {
		w.UserByName = make(map[string]string)
	}

	ensureRoom := func(roomName string) *Room {
		roomName = strings.ToUpper(strings.TrimSpace(roomName))
		if roomName == "" {
			return nil
		}
		if r, ok := w.Rooms[roomName]; ok {
			return r
		}
		r := &Room{
			Name:     roomName,
			Entities: make(map[string]*Entity),
		}
		w.Rooms[roomName] = r
		return r
	}

	copyIntMap := func(src map[string]int) map[string]int {
		if len(src) == 0 {
			return nil
		}
		dst := make(map[string]int, len(src))
		for k, v := range src {
			dst[k] = v
		}
		return dst
	}

	// Pass 1: env (gameTime) and users.
	for _, col := range db.Collections {
		switch col.Name {
		case "env":
			for _, item := range col.Data {
				var doc struct {
					Data struct {
						GameTime int64 `json:"gameTime"`
					} `json:"data"`
				}
				if err := json.Unmarshal(item, &doc); err == nil && doc.Data.GameTime > 0 {
					w.GameTime = doc.Data.GameTime
				}
			}
		case "users":
			for _, item := range col.Data {
				var doc struct {
					ID       string `json:"_id"`
					Username string `json:"username"`
				}
				if err := json.Unmarshal(item, &doc); err == nil && doc.ID != "" && doc.Username != "" {
					w.Users[doc.ID] = doc.Username
					w.UserByName[doc.Username] = doc.ID
				}
			}
		}
	}

	// Pass 2: terrain.
	for _, col := range db.Collections {
		if col.Name != "rooms.terrain" {
			continue
		}
		for _, item := range col.Data {
			var doc struct {
				Room    string `json:"room"`
				Terrain string `json:"terrain"`
			}
			if err := json.Unmarshal(item, &doc); err != nil || len(doc.Terrain) < RoomSize*RoomSize {
				continue
			}
			r := ensureRoom(doc.Room)
			if r == nil {
				continue
			}
			for i := 0; i < RoomSize*RoomSize; i++ {
				switch doc.Terrain[i] {
				case '1', '3':
					r.Terrain[i] = TERRAIN_MASK_WALL
				case '2':
					r.Terrain[i] = TERRAIN_MASK_SWAMP
				default:
					r.Terrain[i] = TERRAIN_MASK_PLAIN
				}
			}
		}
	}

	// Pass 3: all live room objects.
	for _, col := range db.Collections {
		if col.Name != "rooms.objects" {
			continue
		}
		for _, item := range col.Data {
			var doc struct {
				ID                    string          `json:"_id"`
				Room                  string          `json:"room"`
				Type                  string          `json:"type"`
				X                     int             `json:"x"`
				Y                     int             `json:"y"`
				User                  string          `json:"user"`
				Name                  string          `json:"name"`
				Hits                  int             `json:"hits"`
				HitsMax               int             `json:"hitsMax"`
				Store                 map[string]int  `json:"store"`
				StoreCapacity         int             `json:"storeCapacity"`
				StoreCapacityResource map[string]int  `json:"storeCapacityResource"`
				Energy                int             `json:"energy"`
				EnergyCapacity        int             `json:"energyCapacity"`
				NextRegenerationTime  int64           `json:"nextRegenerationTime"`
				MineralType           string          `json:"mineralType"`
				MineralAmount         int             `json:"mineralAmount"`
				Level                 int             `json:"level"`
				Progress              int             `json:"progress"`
				ProgressTotal         int             `json:"progressTotal"`
				DowngradeTime         int64           `json:"downgradeTime"`
				SafeMode              int64           `json:"safeMode"`
				SafeModeAvailable     int             `json:"safeModeAvailable"`
				SafeModeCooldown      int64           `json:"safeModeCooldown"`
				StructureType         string          `json:"structureType"`
				Body                  []struct {
					Type  string `json:"type"`
					Hits  int    `json:"hits"`
					Boost string `json:"boost"`
				} `json:"body"`
				Spawning      json.RawMessage `json:"spawning"`
				Fatigue       int             `json:"fatigue"`
				AgeTime       int64           `json:"ageTime"`
				ResourceType  string          `json:"resourceType"`
				Amount        int             `json:"amount"`
				CreepID       string          `json:"creepId"`
				CreepName     string          `json:"creepName"`
				DeathTime     int64           `json:"deathTime"`
				DecayTime     int64           `json:"decayTime"`
				NextDecayTime int64           `json:"nextDecayTime"`
				Cooldown      int             `json:"cooldown"`
			}
			if err := json.Unmarshal(item, &doc); err != nil {
				continue
			}
			r := ensureRoom(doc.Room)
			if r == nil || !InBounds(doc.X, doc.Y) {
				continue
			}
			id := doc.ID
			if id == "" {
				id = w.nextEntityID(doc.Type)
			}

			switch doc.Type {
			case STRUCTURE_CONTROLLER:
				progTotal := doc.ProgressTotal
				if progTotal == 0 && doc.Level > 0 {
					progTotal = CONTROLLER_LEVELS[doc.Level]
				}
				smAvail := doc.SafeModeAvailable
				if doc.User != "" && doc.Level > 0 && smAvail == 0 && doc.SafeMode == 0 {
					smAvail = 1
				}
				r.Entities[id] = &Entity{
					ID:                id,
					Type:              STRUCTURE_CONTROLLER,
					Room:              r.Name,
					X:                 doc.X,
					Y:                 doc.Y,
					User:              doc.User,
					Level:             doc.Level,
					Progress:          doc.Progress,
					ProgressTotal:     progTotal,
					DowngradeTime:     doc.DowngradeTime,
					SafeMode:          doc.SafeMode,
					SafeModeAvailable: smAvail,
					SafeModeCooldown:  doc.SafeModeCooldown,
				}

			case TYPE_SOURCE:
				capVal := doc.EnergyCapacity
				if capVal <= 0 {
					capVal = SOURCE_ENERGY_CAPACITY
				}
				r.Entities[id] = &Entity{
					ID:                   id,
					Type:                 TYPE_SOURCE,
					Room:                 r.Name,
					X:                    doc.X,
					Y:                    doc.Y,
					Energy:               doc.Energy,
					EnergyCapacity:       capVal,
					NextRegenerationTime: doc.NextRegenerationTime,
				}

			case TYPE_MINERAL:
				mt := doc.MineralType
				if mt == "" {
					mt = RESOURCE_HYDROGEN
				}
				r.Entities[id] = &Entity{
					ID:            id,
					Type:          TYPE_MINERAL,
					Room:          r.Name,
					X:             doc.X,
					Y:             doc.Y,
					MineralType:   mt,
					MineralAmount: doc.MineralAmount,
				}

			case STRUCTURE_SPAWN:
				hits := doc.Hits
				hitsMax := doc.HitsMax
				if hitsMax <= 0 {
					hitsMax = STRUCTURE_HITS_MAX[STRUCTURE_SPAWN]
					if hits <= 0 {
						hits = STRUCTURE_HITS[STRUCTURE_SPAWN]
					}
				}
				store := copyIntMap(doc.Store)
				if store == nil {
					store = map[string]int{RESOURCE_ENERGY: doc.Energy}
				}
				capRes := copyIntMap(doc.StoreCapacityResource)
				if capRes == nil {
					capVal := doc.EnergyCapacity
					if capVal <= 0 {
						capVal = SPAWN_ENERGY_CAPACITY
					}
					capRes = map[string]int{RESOURCE_ENERGY: capVal}
				}
				e := &Entity{
					ID:                    id,
					Type:                  STRUCTURE_SPAWN,
					Room:                  r.Name,
					X:                     doc.X,
					Y:                     doc.Y,
					User:                  doc.User,
					Name:                  doc.Name,
					Hits:                  hits,
					HitsMax:               hitsMax,
					Store:                 store,
					StoreCapacityResource: capRes,
				}
				e.syncEnergyField()
				if len(doc.Spawning) > 0 && string(doc.Spawning) != "null" {
					var sp struct {
						Name       string `json:"name"`
						NeedTime   int    `json:"needTime"`
						SpawnTime  int64  `json:"spawnTime"`
						Directions []int  `json:"directions"`
					}
					if err := json.Unmarshal(doc.Spawning, &sp); err == nil && sp.Name != "" {
						e.SpawningInfo = &SpawningInfo{
							Name:       sp.Name,
							NeedTime:   sp.NeedTime,
							SpawnTime:  sp.SpawnTime,
							Directions: sp.Directions,
						}
					}
				}
				r.Entities[id] = e

			case STRUCTURE_EXTENSION, STRUCTURE_TOWER, STRUCTURE_STORAGE, STRUCTURE_CONTAINER,
				STRUCTURE_LINK, STRUCTURE_ROAD, STRUCTURE_RAMPART, STRUCTURE_WALL, STRUCTURE_EXTRACTOR:
				hits := doc.Hits
				hitsMax := doc.HitsMax
				if hitsMax <= 0 {
					hitsMax = STRUCTURE_HITS_MAX[doc.Type]
					if hits <= 0 {
						hits = STRUCTURE_HITS[doc.Type]
					}
				}
				store := copyIntMap(doc.Store)
				if store == nil && doc.Energy > 0 {
					store = map[string]int{RESOURCE_ENERGY: doc.Energy}
				}
				capRes := copyIntMap(doc.StoreCapacityResource)
				storeCap := doc.StoreCapacity
				if capRes == nil && storeCap <= 0 {
					switch doc.Type {
					case STRUCTURE_EXTENSION:
						capVal := doc.EnergyCapacity
						if capVal <= 0 {
							capVal = 50
						}
						capRes = map[string]int{RESOURCE_ENERGY: capVal}
					case STRUCTURE_TOWER:
						capRes = map[string]int{RESOURCE_ENERGY: TOWER_CAPACITY}
					case STRUCTURE_LINK:
						capRes = map[string]int{RESOURCE_ENERGY: LINK_CAPACITY}
					case STRUCTURE_CONTAINER:
						storeCap = CONTAINER_CAPACITY
					case STRUCTURE_STORAGE:
						storeCap = STORAGE_CAPACITY
					}
				}
				nextDecay := doc.NextDecayTime
				if nextDecay == 0 && doc.DecayTime > 0 {
					nextDecay = doc.DecayTime
				}
				e := &Entity{
					ID:                    id,
					Type:                  doc.Type,
					Room:                  r.Name,
					X:                     doc.X,
					Y:                     doc.Y,
					User:                  doc.User,
					Hits:                  hits,
					HitsMax:               hitsMax,
					Store:                 store,
					StoreCapacity:         storeCap,
					StoreCapacityResource: capRes,
					NextDecayTime:         nextDecay,
					Cooldown:              doc.Cooldown,
				}
				e.syncEnergyField()
				r.Entities[id] = e

			case TYPE_CONSTRUCTION_SITE:
				progTotal := doc.ProgressTotal
				if progTotal <= 0 {
					progTotal = CONSTRUCTION_COST[doc.StructureType]
				}
				r.Entities[id] = &Entity{
					ID:            id,
					Type:          TYPE_CONSTRUCTION_SITE,
					Room:          r.Name,
					X:             doc.X,
					Y:             doc.Y,
					User:          doc.User,
					StructureType: doc.StructureType,
					Progress:      doc.Progress,
					ProgressTotal: progTotal,
				}

			case TYPE_CREEP:
				body := make([]BodyPart, len(doc.Body))
				carryParts := 0
				for i, bp := range doc.Body {
					body[i] = BodyPart{
						Type:  bp.Type,
						Hits:  bp.Hits,
						Boost: bp.Boost,
					}
					if bp.Type == CARRY {
						carryParts++
					}
				}
				store := copyIntMap(doc.Store)
				if store == nil {
					store = make(map[string]int)
				}
				storeCap := doc.StoreCapacity
				if storeCap <= 0 && carryParts > 0 {
					storeCap = carryParts * CARRY_CAPACITY
				}
				var spawning bool
				if len(doc.Spawning) > 0 && string(doc.Spawning) != "null" {
					_ = json.Unmarshal(doc.Spawning, &spawning)
				}
				hitsMax := doc.HitsMax
				if hitsMax <= 0 && len(body) > 0 {
					hitsMax = len(body) * 100
				}
				r.Entities[id] = &Entity{
					ID:            id,
					Type:          TYPE_CREEP,
					Room:          r.Name,
					X:             doc.X,
					Y:             doc.Y,
					User:          doc.User,
					Name:          doc.Name,
					Hits:          doc.Hits,
					HitsMax:       hitsMax,
					Body:          body,
					Store:         store,
					StoreCapacity: storeCap,
					Spawning:      spawning,
					Fatigue:       doc.Fatigue,
					AgeTime:       doc.AgeTime,
				}

			case "energy", TYPE_RESOURCE:
				resType := doc.ResourceType
				if resType == "" {
					resType = RESOURCE_ENERGY
				}
				amt := doc.Amount
				if amt <= 0 {
					amt = doc.Energy
				}
				r.Entities[id] = &Entity{
					ID:            id,
					Type:          TYPE_RESOURCE,
					Room:          r.Name,
					X:             doc.X,
					Y:             doc.Y,
					ResourceType:  resType,
					Amount:        amt,
					Store:         map[string]int{resType: amt},
					NextDecayTime: doc.NextDecayTime,
				}

			case TYPE_TOMBSTONE:
				decayTime := doc.DecayTime
				if decayTime == 0 {
					decayTime = doc.NextDecayTime
				}
				nextDecay := doc.NextDecayTime
				if nextDecay == 0 {
					nextDecay = decayTime
				}
				r.Entities[id] = &Entity{
					ID:            id,
					Type:          TYPE_TOMBSTONE,
					Room:          r.Name,
					X:             doc.X,
					Y:             doc.Y,
					User:          doc.User,
					CreepID:       doc.CreepID,
					CreepName:     doc.CreepName,
					DeathTime:     doc.DeathTime,
					DecayTime:     decayTime,
					NextDecayTime: nextDecay,
					Store:         copyIntMap(doc.Store),
				}
			}
		}
	}

	// Link active spawn.SpawningInfo.CreepID to any matching spawning creep in the same room.
	for _, r := range w.Rooms {
		for _, e := range r.Entities {
			if e.Type == STRUCTURE_SPAWN && e.SpawningInfo != nil && e.SpawningInfo.CreepID == "" {
				for _, c := range r.Entities {
					if c.Type == TYPE_CREEP && c.Spawning && c.User == e.User && c.Name == e.SpawningInfo.Name {
						e.SpawningInfo.CreepID = c.ID
						break
					}
				}
			}
		}
	}

	return nil
}

// RoomOwner returns the userID and username of the room's owner by inspecting
// the room's controller first, then falling back to the first owned spawn.
func (w *World) RoomOwner(roomName string) (userID, username string) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	r, ok := w.Rooms[strings.ToUpper(strings.TrimSpace(roomName))]
	if !ok || r == nil {
		return "", ""
	}

	if ctrl := r.FindController(); ctrl != nil && ctrl.User != "" {
		return ctrl.User, w.Users[ctrl.User]
	}

	var spawnIDs []string
	for id, e := range r.Entities {
		if e.Type == STRUCTURE_SPAWN && e.User != "" {
			spawnIDs = append(spawnIDs, id)
		}
	}
	sort.Strings(spawnIDs)
	if len(spawnIDs) > 0 {
		uid := r.Entities[spawnIDs[0]].User
		return uid, w.Users[uid]
	}

	return "", ""
}

// SpawnBot claims the room's controller at RCL 1 for username and places Spawn1 on a clear walkable tile with 300 energy.
func (w *World) SpawnBot(username, roomName string) (*Entity, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	roomName = strings.ToUpper(strings.TrimSpace(roomName))
	room, ok := w.Rooms[roomName]
	if !ok {
		return nil, fmt.Errorf("room %q not found", roomName)
	}

	userID := w.ensureUserLocked(username)

	// Claim controller at RCL 1.
	ctrl := room.FindController()
	if ctrl != nil {
		ctrl.User = userID
		ctrl.Level = 1
		ctrl.Progress = 0
		ctrl.ProgressTotal = CONTROLLER_LEVELS[1]
		ctrl.DowngradeTime = w.GameTime + int64(CONTROLLER_DOWNGRADE[1])
	}

	// Ensure sources in owned room have full 3000 capacity.
	for _, e := range room.Entities {
		if e.Type == TYPE_SOURCE {
			e.EnergyCapacity = SOURCE_ENERGY_CAPACITY
			if e.Energy < SOURCE_ENERGY_CAPACITY {
				e.Energy = SOURCE_ENERGY_CAPACITY
			}
		}
	}

	// Find a clear walkable tile near the center with at least 4 walkable neighbors.
	sx, sy, found := findSpawnTile(room)
	if !found {
		return nil, fmt.Errorf("no clear spawn tile found in room %q", roomName)
	}

	spawnID := w.nextEntityID("spawn")
	spawn := &Entity{
		ID:      spawnID,
		Type:    STRUCTURE_SPAWN,
		Room:    roomName,
		X:       sx,
		Y:       sy,
		User:    userID,
		Name:    "Spawn1",
		Hits:    STRUCTURE_HITS[STRUCTURE_SPAWN],
		HitsMax: STRUCTURE_HITS_MAX[STRUCTURE_SPAWN],
		Energy:  SPAWN_ENERGY_START,
		Store: map[string]int{
			RESOURCE_ENERGY: SPAWN_ENERGY_START,
		},
		StoreCapacityResource: map[string]int{
			RESOURCE_ENERGY: SPAWN_ENERGY_CAPACITY,
		},
	}
	room.Entities[spawnID] = spawn
	return spawn, nil
}

// findSpawnTile searches outward from (25, 25) for a plain tile with walkable neighbors.
func findSpawnTile(room *Room) (int, int, bool) {
	bestX, bestY := -1, -1
	bestOpen := -1
	for r := 0; r <= 20; r++ {
		for dy := -r; dy <= r; dy++ {
			for dx := -r; dx <= r; dx++ {
				if r > 0 && abs(dx) != r && abs(dy) != r {
					continue
				}
				x, y := 25+dx, 25+dy
				if x < 3 || x > RoomSize-4 || y < 3 || y > RoomSize-4 {
					continue
				}
				if room.TerrainAt(x, y) != TERRAIN_MASK_PLAIN || !room.IsWalkable(x, y, false) {
					continue
				}
				open := 0
				for d := TOP; d <= TOP_LEFT; d++ {
					if room.IsWalkable(x+DirDX[d], y+DirDY[d], false) {
						open++
					}
				}
				if open >= 6 {
					return x, y, true
				}
				if open > bestOpen {
					bestOpen = open
					bestX, bestY = x, y
				}
			}
		}
	}
	if bestX >= 0 {
		return bestX, bestY, true
	}
	return 0, 0, false
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// Snapshot returns a thread-safe immutable screeps.RoomSnapshot for rendering and inspection.
func (w *World) Snapshot(roomName string) screeps.RoomSnapshot {
	w.mu.RLock()
	defer w.mu.RUnlock()

	roomName = strings.ToUpper(strings.TrimSpace(roomName))
	users := make(map[string]string, len(w.Users))
	for k, v := range w.Users {
		users[k] = v
	}

	room, ok := w.Rooms[roomName]
	if !ok {
		return screeps.RoomSnapshot{
			Room:     roomName,
			GameTime: w.GameTime,
			Users:    users,
			Status:   w.Status,
		}
	}

	ids := make([]string, 0, len(room.Entities))
	for id := range room.Entities {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	objs := make([]screeps.RoomObject, 0, len(ids))
	for _, id := range ids {
		e := room.Entities[id]
		objs = append(objs, entityToRoomObject(e))
	}

	visuals := make([]screeps.VisualOp, len(room.Visuals))
	copy(visuals, room.Visuals)

	return screeps.RoomSnapshot{
		Room:     room.Name,
		Terrain:  room.TerrainString(),
		GameTime: w.GameTime,
		Objects:  objs,
		Visuals:  visuals,
		Users:    users,
		Status:   w.Status,
	}
}

func entityToRoomObject(e *Entity) screeps.RoomObject {
	raw := make(map[string]any, 16)
	raw["_id"] = e.ID
	raw["type"] = e.Type
	raw["room"] = e.Room
	raw["x"] = float64(e.X)
	raw["y"] = float64(e.Y)
	if e.User != "" {
		raw["user"] = e.User
	}
	if e.Name != "" {
		raw["name"] = e.Name
	}
	if e.HitsMax > 0 {
		raw["hits"] = float64(e.Hits)
		raw["hitsMax"] = float64(e.HitsMax)
	}

	obj := screeps.RoomObject{
		ID:      e.ID,
		Type:    e.Type,
		Room:    e.Room,
		X:       e.X,
		Y:       e.Y,
		User:    e.User,
		Name:    e.Name,
		Hits:    e.Hits,
		HitsMax: e.HitsMax,
		Level:   e.Level,
		Energy:  e.Energy,
		Raw:     raw,
	}

	// Store
	if len(e.Store) > 0 {
		storeMap := make(map[string]any, len(e.Store))
		for k, v := range e.Store {
			storeMap[k] = float64(v)
		}
		obj.Store = storeMap
		raw["store"] = storeMap
	}
	if len(e.StoreCapacityResource) > 0 {
		capMap := make(map[string]any, len(e.StoreCapacityResource))
		for k, v := range e.StoreCapacityResource {
			capMap[k] = float64(v)
		}
		raw["storeCapacityResource"] = capMap
	}
	if e.StoreCapacity > 0 {
		raw["storeCapacity"] = float64(e.StoreCapacity)
	}

	// ActionLog
	if len(e.ActionLog) > 0 {
		al := make(map[string]any, len(e.ActionLog))
		for k, tgt := range e.ActionLog {
			al[k] = map[string]any{
				"x": float64(tgt.X),
				"y": float64(tgt.Y),
			}
		}
		raw["actionLog"] = al
	}

	// NextDecayTime
	if e.NextDecayTime > 0 {
		raw["nextDecayTime"] = float64(e.NextDecayTime)
	}

	switch e.Type {
	case TYPE_CREEP:
		if len(e.Body) > 0 {
			body := make([]any, len(e.Body))
			for i, bp := range e.Body {
				m := map[string]any{
					"type": bp.Type,
					"hits": float64(bp.Hits),
				}
				if bp.Boost != "" {
					m["boost"] = bp.Boost
				}
				body[i] = m
			}
			raw["body"] = body
		}
		raw["fatigue"] = float64(e.Fatigue)
		if e.AgeTime > 0 {
			raw["ageTime"] = float64(e.AgeTime)
		}
		if e.Spawning {
			raw["spawning"] = true
		}
		if e.SayText != "" {
			raw["say"] = e.SayText
		}
	case STRUCTURE_SPAWN:
		obj.Energy = e.UsedCapacity(RESOURCE_ENERGY)
		raw["energy"] = float64(obj.Energy)
		raw["energyCapacity"] = float64(e.Capacity(RESOURCE_ENERGY))
		if e.SpawningInfo != nil {
			sp := map[string]any{
				"name":      e.SpawningInfo.Name,
				"needTime":  float64(e.SpawningInfo.NeedTime),
				"spawnTime": float64(e.SpawningInfo.SpawnTime),
			}
			obj.Spawning = sp
			raw["spawning"] = sp
		}
	case STRUCTURE_EXTENSION, STRUCTURE_TOWER, STRUCTURE_LINK:
		obj.Energy = e.UsedCapacity(RESOURCE_ENERGY)
		raw["energy"] = float64(obj.Energy)
		raw["energyCapacity"] = float64(e.Capacity(RESOURCE_ENERGY))
		if e.Cooldown > 0 {
			raw["cooldown"] = float64(e.Cooldown)
		}
	case TYPE_SOURCE:
		obj.Energy = e.Energy
		raw["energy"] = float64(e.Energy)
		raw["energyCapacity"] = float64(e.EnergyCapacity)
		if e.NextRegenerationTime > 0 {
			raw["nextRegenerationTime"] = float64(e.NextRegenerationTime)
		}
	case TYPE_MINERAL:
		raw["mineralType"] = e.MineralType
		raw["mineralAmount"] = float64(e.MineralAmount)
	case STRUCTURE_CONTROLLER:
		raw["level"] = float64(e.Level)
		raw["progress"] = float64(e.Progress)
		raw["progressTotal"] = float64(e.ProgressTotal)
		if e.DowngradeTime > 0 {
			raw["downgradeTime"] = float64(e.DowngradeTime)
		}
		if e.SafeMode > 0 {
			raw["safeMode"] = float64(e.SafeMode)
		}
	case TYPE_CONSTRUCTION_SITE:
		raw["structureType"] = e.StructureType
		raw["progress"] = float64(e.Progress)
		raw["progressTotal"] = float64(e.ProgressTotal)
	case TYPE_RESOURCE:
		raw["resourceType"] = e.ResourceType
		raw["amount"] = float64(e.Amount)
		obj.Energy = e.Amount
	case TYPE_TOMBSTONE:
		raw["creepId"] = e.CreepID
		raw["creepName"] = e.CreepName
		raw["deathTime"] = float64(e.DeathTime)
		raw["decayTime"] = float64(e.DecayTime)
		raw["nextDecayTime"] = float64(e.DecayTime)
	}

	return obj
}
