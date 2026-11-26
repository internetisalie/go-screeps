package engine

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ParseRoomCoords parses a Screeps room name like "W9N7" into signed world grid coordinates
// where East is +x, West is -x-1, South is +y, North is -y-1.
func ParseRoomCoords(name string) (wx, wy int, ok bool) {
	s := strings.ToUpper(strings.TrimSpace(name))
	if len(s) < 4 {
		return 0, 0, false
	}
	ew := s[0]
	if ew != 'E' && ew != 'W' {
		return 0, 0, false
	}
	nsIdx := strings.IndexAny(s[1:], "NS")
	if nsIdx <= 0 {
		return 0, 0, false
	}
	nsIdx++ // offset from s[1:]
	ns := s[nsIdx]
	xNum, err1 := strconv.Atoi(s[1:nsIdx])
	yNum, err2 := strconv.Atoi(s[nsIdx+1:])
	if err1 != nil || err2 != nil || xNum < 0 || yNum < 0 {
		return 0, 0, false
	}
	if ew == 'W' {
		wx = -xNum - 1
	} else {
		wx = xNum
	}
	if ns == 'N' {
		wy = -yNum - 1
	} else {
		wy = yNum
	}
	return wx, wy, true
}

// FormatRoomCoords converts signed world grid coordinates back to a Screeps room name.
func FormatRoomCoords(wx, wy int) string {
	var ew byte = 'E'
	xNum := wx
	if wx < 0 {
		ew = 'W'
		xNum = -wx - 1
	}
	var ns byte = 'S'
	yNum := wy
	if wy < 0 {
		ns = 'N'
		yNum = -wy - 1
	}
	return fmt.Sprintf("%c%d%c%d", ew, xNum, ns, yNum)
}

// AdjacentRoomName returns the room name at (dx, dy) from roomName (e.g., dy=-1 for North).
func AdjacentRoomName(roomName string, dx, dy int) (string, bool) {
	wx, wy, ok := ParseRoomCoords(roomName)
	if !ok {
		return "", false
	}
	return FormatRoomCoords(wx+dx, wy+dy), true
}

// RoomHasExitEdge reports whether room has at least one walkable exit tile on the given edge
// (FIND_EXIT_TOP, FIND_EXIT_RIGHT, FIND_EXIT_BOTTOM, FIND_EXIT_LEFT).
func (r *Room) RoomHasExitEdge(exitConst int) bool {
	switch exitConst {
	case FIND_EXIT_TOP:
		for x := 0; x < RoomSize; x++ {
			if r.TerrainAt(x, 0) != TERRAIN_MASK_WALL {
				return true
			}
		}
	case FIND_EXIT_BOTTOM:
		for x := 0; x < RoomSize; x++ {
			if r.TerrainAt(x, RoomSize-1) != TERRAIN_MASK_WALL {
				return true
			}
		}
	case FIND_EXIT_LEFT:
		for y := 0; y < RoomSize; y++ {
			if r.TerrainAt(0, y) != TERRAIN_MASK_WALL {
				return true
			}
		}
	case FIND_EXIT_RIGHT:
		for y := 0; y < RoomSize; y++ {
			if r.TerrainAt(RoomSize-1, y) != TERRAIN_MASK_WALL {
				return true
			}
		}
	}
	return false
}

// resolveMovement deterministically resolves all creep move intents and applies terrain fatigue + fatigue decay.
// Caller must hold w.mu.Lock().
func resolveMovement(w *World, moveIntents []Intent) {
	// Keep the last move intent per creep.
	byCreep := make(map[string]Intent, len(moveIntents))
	for _, in := range moveIntents {
		byCreep[in.ActorID] = in
	}

	// Process each room independently in deterministic order.
	roomNames := make([]string, 0, len(w.Rooms))
	for name := range w.Rooms {
		roomNames = append(roomNames, name)
	}
	sort.Strings(roomNames)

	movedCreeps := make(map[string]bool)

	for _, rName := range roomNames {
		room := w.Rooms[rName]

		// Collect all live non-spawning creeps in the room.
		creepIDs := make([]string, 0)
		for id, e := range room.Entities {
			if e.Type == TYPE_CREEP && !e.Spawning {
				creepIDs = append(creepIDs, id)
			}
		}
		sort.Strings(creepIDs)

		// Desired destination for each moving creep: creepID -> tile index (y*50 + x).
		desired := make(map[string]int)
		for _, id := range creepIDs {
			in, ok := byCreep[id]
			if !ok {
				continue
			}
			c := room.Entities[id]
			if c.Fatigue > 0 || c.CountActiveParts(MOVE) == 0 {
				continue
			}
			if in.Direction < TOP || in.Direction > TOP_LEFT {
				continue
			}
			nx, ny := c.X+DirDX[in.Direction], c.Y+DirDY[in.Direction]
			if !room.IsWalkableFor(nx, ny, c.User, true) {
				continue
			}
			desired[id] = ny*RoomSize + nx
		}

		// Fixed-point collision resolution:
		// 1) If multiple creeps target the same tile, lowest creep ID wins.
		// 2) If a creep targets a tile occupied by a stationary creep, cancel it.
		for {
			changed := false

			// Resolve duplicate destinations deterministically.
			claimedBy := make(map[int]string)
			for _, id := range creepIDs {
				dest, ok := desired[id]
				if !ok {
					continue
				}
				if _, taken := claimedBy[dest]; taken {
					delete(desired, id)
					changed = true
				} else {
					claimedBy[dest] = id
				}
			}

			// Build set of tiles occupied by stationary creeps.
			stationary := make(map[int]bool)
			for _, id := range creepIDs {
				if _, moving := desired[id]; !moving {
					c := room.Entities[id]
					stationary[c.Y*RoomSize+c.X] = true
				}
			}

			// Cancel any move into a stationary creep's tile.
			for _, id := range creepIDs {
				dest, ok := desired[id]
				if !ok {
					continue
				}
				if stationary[dest] {
					delete(desired, id)
					changed = true
				}
			}

			if !changed {
				break
			}
		}

		// Commit surviving moves and add terrain fatigue.
		for _, id := range creepIDs {
			dest, ok := desired[id]
			if !ok {
				continue
			}
			c := room.Entities[id]
			nx, ny := dest%RoomSize, dest/RoomSize
			c.X = nx
			c.Y = ny
			movedCreeps[id] = true

			heavy := creepHeavyParts(c)
			if heavy > 0 {
				mult := terrainFatigueMultiplier(room, nx, ny)
				c.Fatigue += heavy * mult
			}
		}

		// Decay fatigue for all live creeps in the room by 2 * active MOVE parts.
		for _, id := range creepIDs {
			c := room.Entities[id]
			if c.Fatigue > 0 {
				moveParts := c.CountActiveParts(MOVE)
				c.Fatigue -= moveParts * 2
				if c.Fatigue < 0 {
					c.Fatigue = 0
				}
			}
		}
	}

	// Cross-room exit transitions: if a creep moved onto an edge exit tile (0 or 49)
	// and the adjacent room exists with a walkable entry tile, transfer the creep across.
	type transfer struct {
		creep    *Entity
		fromRoom *Room
		toRoom   *Room
		nx, ny   int
	}
	var transfers []transfer
	for _, rName := range roomNames {
		room := w.Rooms[rName]
		creepIDs := make([]string, 0)
		for id, e := range room.Entities {
			if e.Type == TYPE_CREEP && !e.Spawning && movedCreeps[id] {
				creepIDs = append(creepIDs, id)
			}
		}
		sort.Strings(creepIDs)
		for _, id := range creepIDs {
			c := room.Entities[id]
			dx, dy, nx, ny := 0, 0, c.X, c.Y
			switch {
			case c.Y == 0:
				dy, ny = -1, RoomSize-1
			case c.Y == RoomSize-1:
				dy, ny = 1, 0
			case c.X == 0:
				dx, nx = -1, RoomSize-1
			case c.X == RoomSize-1:
				dx, nx = 1, 0
			default:
				continue
			}
			adjName, ok := AdjacentRoomName(rName, dx, dy)
			if !ok {
				continue
			}
			destRoom := w.Rooms[adjName]
			if destRoom == nil {
				continue
			}
			if !destRoom.IsWalkableFor(nx, ny, c.User, false) {
				continue
			}
			transfers = append(transfers, transfer{
				creep:    c,
				fromRoom: room,
				toRoom:   destRoom,
				nx:       nx,
				ny:       ny,
			})
		}
	}
	for _, tr := range transfers {
		delete(tr.fromRoom.Entities, tr.creep.ID)
		tr.creep.Room = tr.toRoom.Name
		tr.creep.X = tr.nx
		tr.creep.Y = tr.ny
		tr.toRoom.Entities[tr.creep.ID] = tr.creep

		// Record cross-room incursion when a creep enters a room owned by another player.
		for _, ent := range tr.toRoom.Entities {
			if ent.Type == STRUCTURE_CONTROLLER && ent.User != "" && ent.User != tr.creep.User {
				w.recordEventLocked(tr.toRoom.Name, "raid", fmt.Sprintf("%s %s entered %s", w.userNameLocked(tr.creep.User), tr.creep.Name, tr.toRoom.Name))
				break
			}
		}
	}
}

// creepHeavyParts returns the number of fatigue-generating body parts on a creep.
// MOVE parts generate 0 fatigue; empty CARRY parts generate 0 fatigue.
func creepHeavyParts(c *Entity) int {
	nonCarryHeavy := 0
	carryParts := 0
	for _, bp := range c.Body {
		switch bp.Type {
		case MOVE:
			// Generates no fatigue.
		case CARRY:
			carryParts++
		default:
			nonCarryHeavy++
		}
	}
	loadedCarry := 0
	if carryParts > 0 {
		used := c.UsedCapacity("")
		if used > 0 {
			loadedCarry = (used + CARRY_CAPACITY - 1) / CARRY_CAPACITY
			if loadedCarry > carryParts {
				loadedCarry = carryParts
			}
		}
	}
	return nonCarryHeavy + loadedCarry
}

// terrainFatigueMultiplier returns the Screeps fatigue multiplier per heavy part on (x, y):
// road = 1 (0.5x plain), plain = 2 (1x), swamp = 10 (5x plain).
func terrainFatigueMultiplier(room *Room, x, y int) int {
	if room.HasRoad(x, y) {
		return 1
	}
	if room.TerrainAt(x, y) == TERRAIN_MASK_SWAMP {
		return 10
	}
	return 2
}
