package engine

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// WorldState is a JSON-serializable snapshot of the complete simulation World.
type WorldState struct {
	GameTime   int64             `json:"gameTime"`
	Users      map[string]string `json:"users"`
	UserByName map[string]string `json:"userByName"`
	UserGCL    map[string]int    `json:"userGcl,omitempty"`
	NextID     uint64            `json:"nextId"`
	Status     string            `json:"status,omitempty"`
	Rooms      []RoomState       `json:"rooms"`
}

// RoomState is a JSON-serializable snapshot of a single Room.
type RoomState struct {
	Name     string        `json:"name"`
	Terrain  string        `json:"terrain"`
	Entities []EntityState `json:"entities"`
}

// EntityState is a JSON-serializable snapshot of a single Entity.
type EntityState struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Room string `json:"room"`
	X    int    `json:"x"`
	Y    int    `json:"y"`
	User string `json:"user,omitempty"`
	Name string `json:"name,omitempty"`

	Hits    int `json:"hits,omitempty"`
	HitsMax int `json:"hitsMax,omitempty"`

	Store                 map[string]int `json:"store,omitempty"`
	StoreCapacity         int            `json:"storeCapacity,omitempty"`
	StoreCapacityResource map[string]int `json:"storeCapacityResource,omitempty"`

	// Creep fields
	Body     []BodyPart `json:"body,omitempty"`
	Fatigue  int        `json:"fatigue,omitempty"`
	AgeTime  int64      `json:"ageTime,omitempty"`
	Spawning bool       `json:"spawning,omitempty"`
	SayText  string     `json:"sayText,omitempty"`

	// Spawn fields
	SpawningInfo *SpawningInfo `json:"spawningInfo,omitempty"`

	// Source fields
	Energy               int   `json:"energy,omitempty"`
	EnergyCapacity       int   `json:"energyCapacity,omitempty"`
	NextRegenerationTime int64 `json:"nextRegenerationTime,omitempty"`

	// Mineral fields
	MineralType   string `json:"mineralType,omitempty"`
	MineralAmount int    `json:"mineralAmount,omitempty"`

	// Controller / ConstructionSite fields
	Level         int    `json:"level,omitempty"`
	Progress      int    `json:"progress,omitempty"`
	ProgressTotal int    `json:"progressTotal,omitempty"`
	DowngradeTime     int64  `json:"downgradeTime,omitempty"`
	SafeMode           int64  `json:"safeMode,omitempty"`
	SafeModeAvailable  int    `json:"safeModeAvailable,omitempty"`
	SafeModeCooldown   int64  `json:"safeModeCooldown,omitempty"`
	ReservationUser    string `json:"reservationUser,omitempty"`
	ReservationEndTime int64  `json:"reservationEndTime,omitempty"`
	UpgradeBlocked     int64  `json:"upgradeBlocked,omitempty"`
	StructureType     string `json:"structureType,omitempty"`

	// Dropped resource fields
	ResourceType string `json:"resourceType,omitempty"`
	Amount       int    `json:"amount,omitempty"`

	// Tombstone fields
	CreepID   string `json:"creepId,omitempty"`
	CreepName string `json:"creepName,omitempty"`
	DeathTime int64  `json:"deathTime,omitempty"`
	DecayTime int64  `json:"decayTime,omitempty"`

	// Decay / Cooldown
	NextDecayTime int64 `json:"nextDecayTime,omitempty"`
	Cooldown      int   `json:"cooldown,omitempty"`

	// Per-tick action log
	ActionLog map[string]ActionTarget `json:"actionLog,omitempty"`
}

func cloneStringMap(src map[string]string) map[string]string {
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func cloneIntMap(src map[string]int) map[string]int {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]int, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func cloneActionLog(src map[string]ActionTarget) map[string]ActionTarget {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]ActionTarget, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func cloneBody(src []BodyPart) []BodyPart {
	if len(src) == 0 {
		return nil
	}
	dst := make([]BodyPart, len(src))
	copy(dst, src)
	return dst
}

func cloneSpawningInfo(src *SpawningInfo) *SpawningInfo {
	if src == nil {
		return nil
	}
	cp := *src
	if len(src.Directions) > 0 {
		cp.Directions = make([]int, len(src.Directions))
		copy(cp.Directions, src.Directions)
	}
	return &cp
}

func entityToState(e *Entity) EntityState {
	return EntityState{
		ID:                    e.ID,
		Type:                  e.Type,
		Room:                  e.Room,
		X:                     e.X,
		Y:                     e.Y,
		User:                  e.User,
		Name:                  e.Name,
		Hits:                  e.Hits,
		HitsMax:               e.HitsMax,
		Store:                 cloneIntMap(e.Store),
		StoreCapacity:         e.StoreCapacity,
		StoreCapacityResource: cloneIntMap(e.StoreCapacityResource),
		Body:                  cloneBody(e.Body),
		Fatigue:               e.Fatigue,
		AgeTime:               e.AgeTime,
		Spawning:              e.Spawning,
		SayText:               e.SayText,
		SpawningInfo:          cloneSpawningInfo(e.SpawningInfo),
		Energy:                e.Energy,
		EnergyCapacity:        e.EnergyCapacity,
		NextRegenerationTime:  e.NextRegenerationTime,
		MineralType:           e.MineralType,
		MineralAmount:         e.MineralAmount,
		Level:                 e.Level,
		Progress:              e.Progress,
		ProgressTotal:         e.ProgressTotal,
		DowngradeTime:         e.DowngradeTime,
		SafeMode:              e.SafeMode,
		SafeModeAvailable:     e.SafeModeAvailable,
		SafeModeCooldown:      e.SafeModeCooldown,
		ReservationUser:       e.ReservationUser,
		ReservationEndTime:    e.ReservationEndTime,
		UpgradeBlocked:        e.UpgradeBlocked,
		StructureType:         e.StructureType,
		ResourceType:          e.ResourceType,
		Amount:                e.Amount,
		CreepID:               e.CreepID,
		CreepName:             e.CreepName,
		DeathTime:             e.DeathTime,
		DecayTime:             e.DecayTime,
		NextDecayTime:         e.NextDecayTime,
		Cooldown:              e.Cooldown,
		ActionLog:             cloneActionLog(e.ActionLog),
	}
}

func stateToEntity(es EntityState) *Entity {
	return &Entity{
		ID:                    es.ID,
		Type:                  es.Type,
		Room:                  es.Room,
		X:                     es.X,
		Y:                     es.Y,
		User:                  es.User,
		Name:                  es.Name,
		Hits:                  es.Hits,
		HitsMax:               es.HitsMax,
		Store:                 cloneIntMap(es.Store),
		StoreCapacity:         es.StoreCapacity,
		StoreCapacityResource: cloneIntMap(es.StoreCapacityResource),
		Body:                  cloneBody(es.Body),
		Fatigue:               es.Fatigue,
		AgeTime:               es.AgeTime,
		Spawning:              es.Spawning,
		SayText:               es.SayText,
		SpawningInfo:          cloneSpawningInfo(es.SpawningInfo),
		Energy:                es.Energy,
		EnergyCapacity:        es.EnergyCapacity,
		NextRegenerationTime:  es.NextRegenerationTime,
		MineralType:           es.MineralType,
		MineralAmount:         es.MineralAmount,
		Level:                 es.Level,
		Progress:              es.Progress,
		ProgressTotal:         es.ProgressTotal,
		DowngradeTime:         es.DowngradeTime,
		SafeMode:              es.SafeMode,
		SafeModeAvailable:     es.SafeModeAvailable,
		SafeModeCooldown:      es.SafeModeCooldown,
		ReservationUser:       es.ReservationUser,
		ReservationEndTime:    es.ReservationEndTime,
		UpgradeBlocked:        es.UpgradeBlocked,
		StructureType:         es.StructureType,
		ResourceType:          es.ResourceType,
		Amount:                es.Amount,
		CreepID:               es.CreepID,
		CreepName:             es.CreepName,
		DeathTime:             es.DeathTime,
		DecayTime:             es.DecayTime,
		NextDecayTime:         es.NextDecayTime,
		Cooldown:              es.Cooldown,
		ActionLog:             cloneActionLog(es.ActionLog),
	}
}

// ExportState returns a deep-copied, JSON-serializable snapshot of the entire World.
func (w *World) ExportState() WorldState {
	w.mu.RLock()
	defer w.mu.RUnlock()

	roomNames := make([]string, 0, len(w.Rooms))
	for name := range w.Rooms {
		roomNames = append(roomNames, name)
	}
	sort.Strings(roomNames)

	rooms := make([]RoomState, 0, len(roomNames))
	for _, name := range roomNames {
		r := w.Rooms[name]
		if r == nil {
			continue
		}
		ids := make([]string, 0, len(r.Entities))
		for id := range r.Entities {
			ids = append(ids, id)
		}
		sort.Strings(ids)

		entities := make([]EntityState, 0, len(ids))
		for _, id := range ids {
			if e := r.Entities[id]; e != nil {
				entities = append(entities, entityToState(e))
			}
		}

		rooms = append(rooms, RoomState{
			Name:     r.Name,
			Terrain:  r.TerrainString(),
			Entities: entities,
		})
	}

	return WorldState{
		GameTime:   w.GameTime,
		Users:      cloneStringMap(w.Users),
		UserByName: cloneStringMap(w.UserByName),
		UserGCL:    cloneIntMap(w.UserGCL),
		NextID:     w.nextID,
		Status:     w.Status,
		Rooms:      rooms,
	}
}

func parseIDNumericSuffix(id string) uint64 {
	if id == "" {
		return 0
	}
	// Check after the last '_' (e.g. "creep_000042") or after leading 'u' (e.g. "u5").
	suffix := ""
	if idx := strings.LastIndexByte(id, '_'); idx >= 0 && idx+1 < len(id) {
		suffix = id[idx+1:]
	} else if len(id) > 1 && id[0] == 'u' {
		suffix = id[1:]
	} else {
		return 0
	}
	v, err := strconv.ParseUint(suffix, 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// ImportState replaces the World's simulation state with st.
func (w *World) ImportState(st WorldState) error {
	var maxID uint64
	newRooms := make(map[string]*Room, len(st.Rooms))
	for _, rs := range st.Rooms {
		roomName := strings.ToUpper(strings.TrimSpace(rs.Name))
		if roomName == "" {
			return fmt.Errorf("import state: room with empty name")
		}
		if len(rs.Terrain) != 0 && len(rs.Terrain) != RoomSize*RoomSize {
			return fmt.Errorf("import state: room %q terrain length %d, want %d", roomName, len(rs.Terrain), RoomSize*RoomSize)
		}

		room := &Room{
			Name:     roomName,
			Entities: make(map[string]*Entity, len(rs.Entities)),
		}
		for i := 0; i < len(rs.Terrain) && i < RoomSize*RoomSize; i++ {
			switch rs.Terrain[i] {
			case '1', '3':
				room.Terrain[i] = TERRAIN_MASK_WALL
			case '2':
				room.Terrain[i] = TERRAIN_MASK_SWAMP
			default:
				room.Terrain[i] = TERRAIN_MASK_PLAIN
			}
		}
		for _, es := range rs.Entities {
			if es.ID == "" {
				continue
			}
			if !InBounds(es.X, es.Y) {
				return fmt.Errorf("import state: entity %q in room %q has out-of-bounds coords (%d,%d)", es.ID, roomName, es.X, es.Y)
			}
			e := stateToEntity(es)
			e.Room = roomName
			room.Entities[e.ID] = e
			if n := parseIDNumericSuffix(e.ID); n > maxID {
				maxID = n
			}
		}
		newRooms[roomName] = room
	}

	users := cloneStringMap(st.Users)
	userByName := cloneStringMap(st.UserByName)
	for id, name := range users {
		if _, ok := userByName[name]; !ok {
			userByName[name] = id
		}
	}
	for name, id := range userByName {
		if _, ok := users[id]; !ok {
			users[id] = name
		}
	}
	for id := range users {
		if n := parseIDNumericSuffix(id); n > maxID {
			maxID = n
		}
	}

	nextID := st.NextID
	if maxID > nextID {
		nextID = maxID
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	w.GameTime = st.GameTime
	w.Users = users
	w.UserByName = userByName
	w.UserGCL = cloneIntMap(st.UserGCL)
	if w.UserGCL == nil {
		w.UserGCL = make(map[string]int)
	}
	w.nextID = nextID
	if st.Status != "" {
		w.Status = st.Status
	}
	w.Rooms = newRooms
	w.intents = nil
	w.events = nil

	return nil
}
