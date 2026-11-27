package lua

import (
	"fmt"
	"sort"
	"strings"

	rt "github.com/arnodel/golua/runtime"

	"go-screeps/internal/engine"
)

const maxTelemetryScan = 2048

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

// RefreshSummaries recomputes bot strategy summaries from current world and memory state.
func (r *LocalRunner) RefreshSummaries() {
	r.stepMu.Lock()
	defer r.stepMu.Unlock()
	r.refreshSummariesLocked()
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
	sum := BotSummary{
		Username:     vm.Username,
		RoleCounts:   make(map[string]int),
		DesiredRoles: make(map[string]int),
	}

	var memTbl *rt.Table
	if vm != nil && vm.R != nil && vm.screepsMod != nil {
		memTbl, _ = tblGet(vm.screepsMod, "Memory").TryTable()
	}

	// Pull creep role map directly from Memory.creeps without deep-exporting Memory.
	creepRoles := make(map[string]string)
	if memTbl != nil {
		if mc, ok := tblGet(memTbl, "creeps").TryTable(); ok {
			scanned := 0
			for k, v, nextOk := mc.Next(rt.NilValue); nextOk && !k.IsNil() && scanned < maxTelemetryScan; k, v, nextOk = mc.Next(k) {
				scanned++
				cname, okName := k.TryString()
				if !okName {
					continue
				}
				if cm, okCM := v.TryTable(); okCM {
					if role, okRole := tblGet(cm, "role").TryString(); okRole && role != "" {
						creepRoles[cname] = role
					}
				}
			}
		}

		// Pull global raid / scout fields (used by Raider bot).
		if rtStr, ok := tblGet(memTbl, "raidTarget").TryString(); ok {
			sum.RaidTarget = rtStr
		}
		if raVal := tblGet(memTbl, "raidActive"); raVal.Type() == rt.BoolType {
			sum.RaidActive = raVal.AsBool()
		}
		if sr, ok := tblGet(memTbl, "scoutedRooms").TryTable(); ok {
			scanned := 0
			for k, _, nextOk := sr.Next(rt.NilValue); nextOk && !k.IsNil() && scanned < maxTelemetryScan; k, _, nextOk = sr.Next(k) {
				scanned++
				if rName, okStr := k.TryString(); okStr {
					sum.ScoutedRooms = append(sum.ScoutedRooms, rName)
				}
			}
			sort.Strings(sum.ScoutedRooms)
		}
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
	if memTbl != nil && bestRoom != "" {
		if mRooms, ok := tblGet(memTbl, "rooms").TryTable(); ok {
			if rm, ok := tblGet(mRooms, bestRoom).TryTable(); ok {
				if ns, ok := tblGet(rm, "nextSpawn").TryString(); ok && ns != "" {
					sum.NextSpawn = ns
				}
				if dr, ok := tblGet(rm, "desiredRoles").TryTable(); ok {
					scanned := 0
					for k, v, nextOk := dr.Next(rt.NilValue); nextOk && !k.IsNil() && scanned < maxTelemetryScan; k, v, nextOk = dr.Next(k) {
						scanned++
						role, okRole := k.TryString()
						if !okRole {
							continue
						}
						if iv, okInt := asInt(v); okInt {
							sum.DesiredRoles[role] = iv
						}
					}
				}
				if th, ok := tblGet(rm, "threat").TryTable(); ok {
					if lv, okInt := asInt(tblGet(th, "level")); okInt {
						sum.ThreatLevel = lv
					}
				}
				if rr, ok := tblGet(rm, "remoteRoom").TryString(); ok && rr != "" {
					sum.RemoteRoom = rr
				}
				sum.PlanSummary = summarizeRoomPlanTable(rm)
			}
		}
	}
	if sum.NextSpawn == "" && spawningLabel != "" {
		sum.NextSpawn = spawningLabel
	}

	return sum
}

func summarizeRoomPlanTable(rm *rt.Table) string {
	var plan *rt.Table
	if p, ok := tblGet(rm, "plan").TryTable(); ok {
		plan = p
	} else if p, ok := tblGet(rm, "raiderPlan").TryTable(); ok {
		plan = p
	}
	if plan == nil {
		return ""
	}
	countOf := func(key string) int {
		tbl, ok := tblGet(plan, key).TryTable()
		if !ok {
			return 0
		}
		if !tblGet(tbl, "x").IsNil() {
			return 1
		}
		count := 0
		for k, _, nextOk := tbl.Next(rt.NilValue); nextOk && !k.IsNil() && count < maxTelemetryScan; k, _, nextOk = tbl.Next(k) {
			count++
		}
		return count
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
