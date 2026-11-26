package lua

import (
	"fmt"
	"sort"
	"strings"

	"go-screeps/internal/engine"
)

// BotSummary holds a high-level snapshot of one Lua bot's economy, planning,
// and combat/raid strategy for display in the viewer's Bot Brain sidebar panel.
type BotSummary struct {
	Username     string
	HomeRoom     string
	RCL          int
	EnergyAvail  int
	EnergyCap    int
	CreepCount   int
	RoleCounts   map[string]int
	DesiredRoles map[string]int
	NextSpawn    string
	ThreatLevel  int
	Hostiles     int
	RemoteRoom   string
	RaidTarget   string
	RaidActive   bool
	ScoutedRooms []string
	PlanSummary  string
}

// BotSummaries returns the most recently computed strategy summaries for all running bots.
func (r *LocalRunner) BotSummaries() []BotSummary {
	r.stateMu.RLock()
	defer r.stateMu.RUnlock()
	if len(r.summaries) == 0 {
		return nil
	}
	out := make([]BotSummary, len(r.summaries))
	copy(out, r.summaries)
	return out
}

// refreshSummariesLocked builds BotSummary snapshots for all bots (caller must hold r.stepMu).
func (r *LocalRunner) refreshSummariesLocked() {
	summaries := make([]BotSummary, 0, len(r.bots))
	for _, vm := range r.bots {
		summaries = append(summaries, buildBotSummary(r.world, vm))
	}
	r.stateMu.Lock()
	r.summaries = summaries
	r.stateMu.Unlock()
}

func buildBotSummary(world *engine.World, vm *BotVM) BotSummary {
	mem := vm.ExportMemory()
	sum := BotSummary{
		Username:     vm.Username,
		RoleCounts:   make(map[string]int),
		DesiredRoles: make(map[string]int),
	}

	// Pull creep role map from Memory.creeps.
	creepRoles := make(map[string]string)
	if mc, ok := mem["creeps"].(map[string]any); ok {
		for cname, raw := range mc {
			if cm, ok := raw.(map[string]any); ok {
				if role, ok := cm["role"].(string); ok && role != "" {
					creepRoles[cname] = role
				}
			}
		}
	}

	// Pull global raid / scout fields (used by Raider bot).
	if rt, ok := mem["raidTarget"].(string); ok {
		sum.RaidTarget = rt
	}
	if ra, ok := mem["raidActive"].(bool); ok {
		sum.RaidActive = ra
	}
	if sr, ok := mem["scoutedRooms"].(map[string]any); ok {
		for rName := range sr {
			sum.ScoutedRooms = append(sum.ScoutedRooms, rName)
		}
		sort.Strings(sum.ScoutedRooms)
	}

	// Scan world entities owned by this bot.
	world.RLock()
	bestRoom := ""
	bestRCL := -1
	var spawningLabel string
	for rName, room := range world.Rooms {
		roomHasSpawn := false
		roomRCL := -1
		roomAvail := 0
		roomCap := 0
		roomHostiles := 0
		for _, e := range room.Entities {
			if e.Type == engine.TYPE_CREEP && e.User != "" && e.User != vm.UserID {
				roomHostiles++
			}
			if e.User != vm.UserID {
				continue
			}
			switch e.Type {
			case engine.TYPE_CREEP:
				sum.CreepCount++
				role := creepRoles[e.Name]
				if role == "" {
					role = "creep"
				}
				sum.RoleCounts[role]++
			case engine.STRUCTURE_SPAWN:
				roomHasSpawn = true
				roomAvail += e.UsedCapacity(engine.RESOURCE_ENERGY)
				roomCap += e.Capacity(engine.RESOURCE_ENERGY)
				if e.SpawningInfo != nil && e.SpawningInfo.Name != "" {
					role := creepRoles[e.SpawningInfo.Name]
					if role != "" {
						spawningLabel = "spawning " + role
					} else {
						spawningLabel = "spawning " + e.SpawningInfo.Name
					}
				}
			case engine.STRUCTURE_EXTENSION:
				roomAvail += e.UsedCapacity(engine.RESOURCE_ENERGY)
				roomCap += e.Capacity(engine.RESOURCE_ENERGY)
			case engine.STRUCTURE_CONTROLLER:
				roomRCL = e.Level
			}
		}
		if roomHasSpawn || roomRCL > bestRCL {
			if bestRoom == "" || roomHasSpawn || roomRCL > bestRCL {
				bestRoom = rName
				bestRCL = roomRCL
				sum.EnergyAvail = roomAvail
				sum.EnergyCap = roomCap
				sum.Hostiles = roomHostiles
			}
		}
	}
	world.RUnlock()

	sum.HomeRoom = bestRoom
	if bestRCL > 0 {
		sum.RCL = bestRCL
	}

	// Pull room-specific memory (nextSpawn, desiredRoles, threat, plan summary).
	if mRooms, ok := mem["rooms"].(map[string]any); ok && bestRoom != "" {
		if rm, ok := mRooms[bestRoom].(map[string]any); ok {
			if ns, ok := rm["nextSpawn"].(string); ok && ns != "" {
				sum.NextSpawn = ns
			}
			if dr, ok := rm["desiredRoles"].(map[string]any); ok {
				for role, v := range dr {
					if f, ok := v.(float64); ok {
						sum.DesiredRoles[role] = int(f)
					}
				}
			}
			if th, ok := rm["threat"].(map[string]any); ok {
				if lv, ok := th["level"].(float64); ok {
					sum.ThreatLevel = int(lv)
				}
			}
			if rr, ok := rm["remoteRoom"].(string); ok && rr != "" {
				sum.RemoteRoom = rr
			}
			sum.PlanSummary = summarizeRoomPlan(rm)
		}
	}
	if sum.NextSpawn == "" && spawningLabel != "" {
		sum.NextSpawn = spawningLabel
	}

	return sum
}

func summarizeRoomPlan(rm map[string]any) string {
	var plan map[string]any
	if p, ok := rm["plan"].(map[string]any); ok {
		plan = p
	} else if p, ok := rm["raiderPlan"].(map[string]any); ok {
		plan = p
	}
	if plan == nil {
		return ""
	}
	countOf := func(key string) int {
		switch v := plan[key].(type) {
		case []any:
			return len(v)
		case map[string]any:
			if _, hasX := v["x"]; hasX {
				return 1
			}
			return len(v)
		default:
			return 0
		}
	}
	var parts []string
	if n := countOf("extensions"); n > 0 {
		parts = append(parts, fmt.Sprintf("%d ext", n))
	}
	if n := countOf("towers"); n > 0 {
		parts = append(parts, fmt.Sprintf("%d twr", n))
	}
	if n := countOf("containers"); n > 0 {
		parts = append(parts, fmt.Sprintf("%d cont", n))
	}
	if n := countOf("roads"); n > 0 {
		parts = append(parts, fmt.Sprintf("%d rd", n))
	}
	if n := countOf("ramparts"); n > 0 {
		parts = append(parts, fmt.Sprintf("%d ramp", n))
	}
	if n := countOf("storage"); n > 0 {
		parts = append(parts, "storage")
	}
	return strings.Join(parts, " • ")
}
