package engine

import (
	"fmt"
	"math"
	"sort"
)

// Step advances the simulation by 1 tick, deterministically resolving all queued intents,
// spawns, movement, creep actions, towers, links, regeneration, controller levels, decay, and deaths.
func (w *World) Step() {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.GameTime++

	// Clear previous tick's action logs / say text and promote pending visuals.
	roomNames := make([]string, 0, len(w.Rooms))
	for name, room := range w.Rooms {
		roomNames = append(roomNames, name)
		room.Visuals = room.pendingVisuals
		room.pendingVisuals = nil
		for _, e := range room.Entities {
			e.ActionLog = nil
			e.SayText = ""
		}
	}
	sort.Strings(roomNames)

	// Drain queued intents.
	intents := w.intents
	w.intents = nil

	var spawnIntents []Intent
	var moveIntents []Intent
	var creepIntents []Intent
	var towerIntents []Intent
	var linkIntents []Intent
	var safeModeIntents []Intent
	var siteIntents []Intent

	for _, in := range intents {
		switch in.Type {
		case IntentSpawnCreep:
			spawnIntents = append(spawnIntents, in)
		case IntentMove:
			moveIntents = append(moveIntents, in)
		case IntentTowerAttack, IntentTowerHeal, IntentTowerRepair:
			towerIntents = append(towerIntents, in)
		case IntentLinkTransfer:
			linkIntents = append(linkIntents, in)
		case IntentActivateSafeMode:
			safeModeIntents = append(safeModeIntents, in)
		case IntentCreateConstructionSite:
			siteIntents = append(siteIntents, in)
		default:
			creepIntents = append(creepIntents, in)
		}
	}

	// 0. Safe Mode activations (take effect before combat on this tick) and queued construction sites.
	for _, in := range safeModeIntents {
		ctrl := w.getEntityLocked(in.ActorID)
		if ctrl == nil || ctrl.Type != STRUCTURE_CONTROLLER || ctrl.User != in.UserID || ctrl.Level <= 0 {
			continue
		}
		if ctrl.SafeMode > w.GameTime || ctrl.SafeModeCooldown > w.GameTime || ctrl.SafeModeAvailable <= 0 {
			continue
		}
		ctrl.SafeModeAvailable--
		ctrl.SafeMode = w.GameTime + SAFE_MODE_DURATION
		ctrl.SafeModeCooldown = w.GameTime + SAFE_MODE_COOLDOWN
		w.recordEventLocked(ctrl.Room, "rcl", fmt.Sprintf("%s activated Safe Mode!", w.userNameLocked(ctrl.User)))
	}
	for _, in := range siteIntents {
		w.createConstructionSiteLocked(in.UserID, in.TargetRoom, in.TargetX, in.TargetY, in.StructureType)
	}

	// 1. Spawns: start new spawns and complete finished ones.
	processSpawns(w, roomNames, spawnIntents)

	// 2. Movement & fatigue.
	resolveMovement(w, moveIntents)

	// 3 & 4. Creep actions, Towers, and Links — buffer combat damage/heal deltas so duels are order-independent.
	deltas := newCombatDeltas()
	processCreepActions(w, creepIntents, deltas)
	processTowers(w, towerIntents, deltas)
	deltas.apply()
	processLinks(w, roomNames, linkIntents)

	// 5. Passive room ticks: source regen, controller levels/downgrade, decay, deaths.
	processRoomPassives(w, roomNames)
}

func processSpawns(w *World, roomNames []string, spawnIntents []Intent) {
	// Start newly queued spawns.
	for _, in := range spawnIntents {
		spawn := w.getEntityLocked(in.ActorID)
		if spawn == nil || spawn.Type != STRUCTURE_SPAWN || spawn.User != in.UserID || spawn.SpawningInfo != nil {
			continue
		}
		room := w.Rooms[spawn.Room]
		if room == nil {
			continue
		}
		cost := BodyCost(in.Body)
		if cost <= 0 || room.EnergyAvailable(in.UserID) < cost {
			continue
		}

		// Deduct energy from the spawning spawn first, then other owned spawns and extensions sorted by ID.
		rem := cost
		rem -= spawn.SubStore(RESOURCE_ENERGY, rem)
		if rem > 0 {
			var srcIDs []string
			for id, e := range room.Entities {
				if id != spawn.ID && (e.Type == STRUCTURE_EXTENSION || e.Type == STRUCTURE_SPAWN) && e.User == in.UserID && e.UsedCapacity(RESOURCE_ENERGY) > 0 {
					srcIDs = append(srcIDs, id)
				}
			}
			sort.Strings(srcIDs)
			for _, id := range srcIDs {
				if rem <= 0 {
					break
				}
				rem -= room.Entities[id].SubStore(RESOURCE_ENERGY, rem)
			}
		}
		if rem > 0 {
			// Should not happen since EnergyAvailable was checked, but never allow unpaid spawns.
			continue
		}

		// Build creep body & entity (initially spawning at spawn's coordinates).
		body := make([]BodyPart, len(in.Body))
		carryParts := 0
		for i, pt := range in.Body {
			body[i] = BodyPart{Type: pt, Hits: 100}
			if pt == CARRY {
				carryParts++
			}
		}
		creepID := w.nextEntityID("creep")
		creep := &Entity{
			ID:            creepID,
			Type:          TYPE_CREEP,
			Room:          room.Name,
			X:             spawn.X,
			Y:             spawn.Y,
			User:          in.UserID,
			Name:          in.CreepName,
			Hits:          len(body) * 100,
			HitsMax:       len(body) * 100,
			Body:          body,
			Store:         make(map[string]int),
			StoreCapacity: carryParts * CARRY_CAPACITY,
			Spawning:      true,
		}
		room.Entities[creepID] = creep

		needTime := len(body) * CREEP_SPAWN_TIME
		spawn.SpawningInfo = &SpawningInfo{
			Name:       in.CreepName,
			NeedTime:   needTime,
			SpawnTime:  w.GameTime + int64(needTime),
			CreepID:    creepID,
			Directions: in.Directions,
		}
	}

	// Complete any spawns whose timer has elapsed.
	for _, rName := range roomNames {
		room := w.Rooms[rName]
		var spawnIDs []string
		for id, e := range room.Entities {
			if e.Type == STRUCTURE_SPAWN && e.SpawningInfo != nil {
				spawnIDs = append(spawnIDs, id)
			}
		}
		sort.Strings(spawnIDs)

		for _, sid := range spawnIDs {
			spawn := room.Entities[sid]
			info := spawn.SpawningInfo
			if w.GameTime < info.SpawnTime {
				continue
			}
			creep := room.Entities[info.CreepID]
			if creep == nil {
				spawn.SpawningInfo = nil
				continue
			}

			var dirs []int
			for _, d := range info.Directions {
				if d >= TOP && d <= TOP_LEFT {
					dirs = append(dirs, d)
				}
			}
			if len(dirs) == 0 {
				dirs = []int{TOP, TOP_RIGHT, RIGHT, BOTTOM_RIGHT, BOTTOM, BOTTOM_LEFT, LEFT, TOP_LEFT}
			}
			placed := false
			for _, d := range dirs {
				if d < TOP || d > TOP_LEFT {
					continue
				}
				nx, ny := spawn.X+DirDX[d], spawn.Y+DirDY[d]
				if room.IsWalkableFor(nx, ny, creep.User, false) {
					creep.X = nx
					creep.Y = ny
					creep.Spawning = false
					ttl := int64(CREEP_LIFE_TIME)
					if creep.CountActiveParts(CLAIM) > 0 {
						ttl = int64(CREEP_CLAIM_LIFE_TIME)
					}
					creep.AgeTime = w.GameTime + ttl
					spawn.SpawningInfo = nil
					w.recordEventLocked(room.Name, "spawn", fmt.Sprintf("Spawned: %s", creep.Name))
					placed = true
					break
				}
			}
			if !placed {
				// Delay completion by 1 tick if all adjacent tiles are occupied.
				info.SpawnTime = w.GameTime + 1
			}
		}
	}
}

type combatDeltas struct {
	damage map[*Entity]int
	heal   map[*Entity]int
	order  []*Entity
	seen   map[*Entity]bool
}

func newCombatDeltas() *combatDeltas {
	return &combatDeltas{
		damage: make(map[*Entity]int),
		heal:   make(map[*Entity]int),
		seen:   make(map[*Entity]bool),
	}
}

func (d *combatDeltas) addDamage(e *Entity, amt int) {
	if e == nil || amt <= 0 {
		return
	}
	if !d.seen[e] {
		d.seen[e] = true
		d.order = append(d.order, e)
	}
	d.damage[e] += amt
}

func (d *combatDeltas) addHeal(e *Entity, amt int) {
	if e == nil || amt <= 0 {
		return
	}
	if !d.seen[e] {
		d.seen[e] = true
		d.order = append(d.order, e)
	}
	d.heal[e] += amt
}

func (d *combatDeltas) apply() {
	for _, e := range d.order {
		if dmg := d.damage[e]; dmg > 0 {
			e.TakeDamage(dmg)
		}
		if heal := d.heal[e]; heal > 0 && e.Hits > 0 {
			e.HealDamage(heal)
		}
	}
}

func actionConflictGroup(t IntentType) string {
	switch t {
	case IntentHarvest, IntentBuild, IntentRepair, IntentUpgradeController, IntentDismantle, IntentClaimController, IntentReserveController, IntentAttackController:
		return "work"
	case IntentHeal, IntentRangedHeal:
		return "heal"
	case IntentAttack:
		return "attack"
	case IntentRangedAttack, IntentRangedMassAttack:
		return "ranged"
	default:
		return string(t)
	}
}

func processCreepActions(w *World, intents []Intent, deltas *combatDeltas) {
	// Keep one intent per (actorID, conflictGroup) — last intent wins.
	type key struct {
		actor string
		group string
	}
	dedup := make(map[key]Intent, len(intents))
	order := make([]key, 0, len(intents))
	for _, in := range intents {
		k := key{actor: in.ActorID, group: actionConflictGroup(in.Type)}
		if _, exists := dedup[k]; !exists {
			order = append(order, k)
		}
		dedup[k] = in
	}

	for _, k := range order {
		in := dedup[k]
		actor := w.getEntityLocked(in.ActorID)
		if actor == nil || actor.Type != TYPE_CREEP || actor.Spawning || actor.Hits <= 0 {
			continue
		}
		room := w.Rooms[actor.Room]
		if room == nil {
			continue
		}

		switch in.Type {
		case IntentHarvest:
			target := room.Entities[in.TargetID]
			if target == nil || ChebyshevDist(actor.X, actor.Y, target.X, target.Y) > 1 {
				continue
			}
			work := actor.CountActiveParts(WORK)
			if work == 0 {
				continue
			}
			if target.Type == TYPE_SOURCE && target.Energy > 0 {
				amt := work * HARVEST_POWER
				if amt > target.Energy {
					amt = target.Energy
				}
				target.Energy -= amt
				if target.NextRegenerationTime == 0 {
					target.NextRegenerationTime = w.GameTime + SOURCE_ENERGY_REGEN
				}
				stored := actor.AddStore(RESOURCE_ENERGY, amt)
				if overflow := amt - stored; overflow > 0 {
					dropResource(w, room, actor.X, actor.Y, RESOURCE_ENERGY, overflow)
				}
				actor.SetAction("harvest", target.X, target.Y)
			} else if target.Type == TYPE_MINERAL && target.MineralAmount > 0 {
				amt := work * HARVEST_MINERAL_POWER
				if amt > target.MineralAmount {
					amt = target.MineralAmount
				}
				target.MineralAmount -= amt
				stored := actor.AddStore(target.MineralType, amt)
				if overflow := amt - stored; overflow > 0 {
					dropResource(w, room, actor.X, actor.Y, target.MineralType, overflow)
				}
				actor.SetAction("harvest", target.X, target.Y)
			}

		case IntentPickup:
			target := room.Entities[in.TargetID]
			if target == nil || target.Type != TYPE_RESOURCE || target.Amount <= 0 {
				continue
			}
			if ChebyshevDist(actor.X, actor.Y, target.X, target.Y) > 1 {
				continue
			}
			taken := actor.AddStore(target.ResourceType, target.Amount)
			target.Amount -= taken
			if target.Amount <= 0 {
				delete(room.Entities, target.ID)
			}

		case IntentDrop:
			res := in.ResourceType
			if res == "" {
				res = RESOURCE_ENERGY
			}
			amt := in.Amount
			if amt <= 0 {
				amt = actor.UsedCapacity(res)
			}
			dropped := actor.SubStore(res, amt)
			if dropped > 0 {
				dropResource(w, room, actor.X, actor.Y, res, dropped)
			}

		case IntentTransfer:
			res := in.ResourceType
			if res == "" {
				res = RESOURCE_ENERGY
			}
			target := room.Entities[in.TargetID]
			if target == nil || ChebyshevDist(actor.X, actor.Y, target.X, target.Y) > 1 {
				continue
			}
			amt := in.Amount
			if amt <= 0 {
				amt = actor.UsedCapacity(res)
			}
			free := target.FreeCapacity(res)
			if amt > free {
				amt = free
			}
			taken := actor.SubStore(res, amt)
			if taken > 0 {
				target.AddStore(res, taken)
			}

		case IntentWithdraw:
			res := in.ResourceType
			if res == "" {
				res = RESOURCE_ENERGY
			}
			target := room.Entities[in.TargetID]
			if target == nil || (!target.IsStructure() && target.Type != TYPE_TOMBSTONE) || ChebyshevDist(actor.X, actor.Y, target.X, target.Y) > 1 {
				continue
			}
			amt := in.Amount
			if amt <= 0 {
				amt = target.UsedCapacity(res)
			}
			free := actor.FreeCapacity(res)
			if amt > free {
				amt = free
			}
			taken := target.SubStore(res, amt)
			if taken > 0 {
				actor.AddStore(res, taken)
			}

		case IntentBuild:
			target := room.Entities[in.TargetID]
			if target == nil || target.Type != TYPE_CONSTRUCTION_SITE {
				continue
			}
			if ChebyshevDist(actor.X, actor.Y, target.X, target.Y) > 3 {
				continue
			}
			work := actor.CountActiveParts(WORK)
			energy := actor.UsedCapacity(RESOURCE_ENERGY)
			rem := target.ProgressTotal - target.Progress
			if work == 0 || energy <= 0 || rem <= 0 {
				continue
			}
			amt := work * BUILD_POWER
			if amt > energy {
				amt = energy
			}
			if amt > rem {
				amt = rem
			}
			actor.SubStore(RESOURCE_ENERGY, amt)
			target.Progress += amt
			actor.SetAction("build", target.X, target.Y)
			if target.Progress >= target.ProgressTotal {
				completeConstructionSite(w, room, target)
			}

		case IntentRepair:
			target := room.Entities[in.TargetID]
			if target == nil || !target.IsStructure() || target.HitsMax <= 0 || target.Hits <= 0 || target.Hits >= target.HitsMax {
				continue
			}
			if ChebyshevDist(actor.X, actor.Y, target.X, target.Y) > 3 {
				continue
			}
			work := actor.CountActiveParts(WORK)
			energy := actor.UsedCapacity(RESOURCE_ENERGY)
			if work == 0 || energy <= 0 {
				continue
			}
			maxHits := work * REPAIR_POWER
			needed := target.HitsMax - target.Hits
			if maxHits > needed {
				maxHits = needed
			}
			energyNeeded := int(math.Ceil(float64(maxHits) * REPAIR_COST))
			if energyNeeded > energy {
				energyNeeded = energy
				maxHits = int(float64(energyNeeded) / REPAIR_COST)
				if maxHits > needed {
					maxHits = needed
				}
			}
			if energyNeeded > 0 && maxHits > 0 {
				actor.SubStore(RESOURCE_ENERGY, energyNeeded)
				target.Hits += maxHits
				if target.Hits > target.HitsMax {
					target.Hits = target.HitsMax
				}
				actor.SetAction("repair", target.X, target.Y)
			}

		case IntentUpgradeController:
			target := room.Entities[in.TargetID]
			if target == nil || target.Type != STRUCTURE_CONTROLLER || target.User != actor.User || target.UpgradeBlocked > w.GameTime {
				continue
			}
			if ChebyshevDist(actor.X, actor.Y, target.X, target.Y) > 3 {
				continue
			}
			work := actor.CountActiveParts(WORK)
			energy := actor.UsedCapacity(RESOURCE_ENERGY)
			if work == 0 || energy <= 0 {
				continue
			}
			amt := work * UPGRADE_CONTROLLER_POWER
			if amt > energy {
				amt = energy
			}
			actor.SubStore(RESOURCE_ENERGY, amt)
			target.Progress += amt
			if w.UserGCL == nil {
				w.UserGCL = make(map[string]int)
			}
			w.UserGCL[actor.User] += amt
			if dg, ok := CONTROLLER_DOWNGRADE[target.Level]; ok {
				target.DowngradeTime = w.GameTime + int64(dg)
			}
			actor.SetAction("upgradeController", target.X, target.Y)
			checkControllerUpgrade(w, room, target)

		case IntentClaimController:
			target := room.Entities[in.TargetID]
			if target == nil || target.Type != STRUCTURE_CONTROLLER || target.User != "" {
				continue
			}
			if target.ReservationUser != "" && target.ReservationUser != actor.User && target.ReservationEndTime > w.GameTime {
				continue
			}
			if ChebyshevDist(actor.X, actor.Y, target.X, target.Y) > 1 || actor.CountActiveParts(CLAIM) == 0 {
				continue
			}
			gclLevel, _, _ := w.UserGCLInfoLocked(actor.User)
			if w.OwnedRoomCountLocked(actor.User) >= gclLevel {
				continue
			}
			target.User = actor.User
			target.Level = 1
			target.Progress = 0
			target.ProgressTotal = CONTROLLER_LEVELS[1]
			target.DowngradeTime = w.GameTime + int64(CONTROLLER_DOWNGRADE[1])
			target.SafeModeAvailable = 1
			target.ReservationUser = ""
			target.ReservationEndTime = 0
			syncRoomSourceCapacities(room, true)
			w.recordEventLocked(room.Name, "rcl", fmt.Sprintf("%s claimed %s!", w.userNameLocked(actor.User), room.Name))
			actor.SetAction("claimController", target.X, target.Y)

		case IntentReserveController:
			target := room.Entities[in.TargetID]
			if target == nil || target.Type != STRUCTURE_CONTROLLER || target.User != "" {
				continue
			}
			if target.ReservationUser != "" && target.ReservationUser != actor.User && target.ReservationEndTime > w.GameTime {
				continue
			}
			parts := actor.CountActiveParts(CLAIM)
			if ChebyshevDist(actor.X, actor.Y, target.X, target.Y) > 1 || parts == 0 {
				continue
			}
			if target.ReservationUser != actor.User || target.ReservationEndTime < w.GameTime {
				target.ReservationUser = actor.User
				target.ReservationEndTime = w.GameTime
			}
			target.ReservationEndTime += int64(parts * CONTROLLER_RESERVE)
			maxEnd := w.GameTime + CONTROLLER_RESERVE_MAX
			if target.ReservationEndTime > maxEnd {
				target.ReservationEndTime = maxEnd
			}
			syncRoomSourceCapacities(room, true)
			actor.SetAction("reserveController", target.X, target.Y)

		case IntentAttackController:
			target := room.Entities[in.TargetID]
			if target == nil || target.Type != STRUCTURE_CONTROLLER {
				continue
			}
			parts := actor.CountActiveParts(CLAIM)
			if ChebyshevDist(actor.X, actor.Y, target.X, target.Y) > 1 || parts == 0 {
				continue
			}
			if target.UpgradeBlocked > w.GameTime || (target.SafeMode > w.GameTime && target.User != actor.User) {
				continue
			}
			if target.User != "" {
				target.DowngradeTime -= int64(parts * CONTROLLER_CLAIM_DOWNGRADE)
				target.UpgradeBlocked = w.GameTime + CONTROLLER_ATTACK_BLOCKED_UPGRADE
			} else if target.ReservationUser != "" && target.ReservationEndTime > w.GameTime {
				target.ReservationEndTime -= int64(parts * CONTROLLER_RESERVE)
				if target.ReservationEndTime <= w.GameTime {
					target.ReservationUser = ""
					target.ReservationEndTime = 0
					syncRoomSourceCapacities(room, false)
				}
			}
			actor.SetAction("attackController", target.X, target.Y)

		case IntentDismantle:
			target := room.Entities[in.TargetID]
			if target == nil || !target.IsStructure() || target.HitsMax <= 0 || target.Hits <= 0 {
				continue
			}
			if ChebyshevDist(actor.X, actor.Y, target.X, target.Y) > 1 {
				continue
			}
			work := actor.CountActiveParts(WORK)
			if work == 0 {
				continue
			}
			effective := resolveCombatTarget(w, room, actor.User, target)
			if effective == nil {
				continue
			}
			dmg := work * DISMANTLE_POWER
			if dmg > effective.Hits {
				dmg = effective.Hits
			}
			deltas.addDamage(effective, dmg)
			gained := int(float64(dmg) * DISMANTLE_COST)
			if gained > 0 {
				stored := actor.AddStore(RESOURCE_ENERGY, gained)
				if overflow := gained - stored; overflow > 0 {
					dropResource(w, room, actor.X, actor.Y, RESOURCE_ENERGY, overflow)
				}
			}
			actor.SetAction("dismantle", target.X, target.Y)

		case IntentAttack:
			target := room.Entities[in.TargetID]
			if target == nil || target.ID == actor.ID || target.HitsMax <= 0 || (target.Type == TYPE_CREEP && target.Spawning) || ChebyshevDist(actor.X, actor.Y, target.X, target.Y) > 1 {
				continue
			}
			parts := actor.CountActiveParts(ATTACK)
			if parts == 0 {
				continue
			}
			if effective := resolveCombatTarget(w, room, actor.User, target); effective != nil {
				deltas.addDamage(effective, parts*ATTACK_POWER)
			}
			actor.SetAction("attack", target.X, target.Y)

		case IntentRangedAttack:
			target := room.Entities[in.TargetID]
			if target == nil || target.ID == actor.ID || target.HitsMax <= 0 || (target.Type == TYPE_CREEP && target.Spawning) || ChebyshevDist(actor.X, actor.Y, target.X, target.Y) > 3 {
				continue
			}
			parts := actor.CountActiveParts(RANGED_ATTACK)
			if parts == 0 {
				continue
			}
			if effective := resolveCombatTarget(w, room, actor.User, target); effective != nil {
				deltas.addDamage(effective, parts*RANGED_ATTACK_POWER)
			}
			actor.SetAction("rangedAttack", target.X, target.Y)

		case IntentRangedMassAttack:
			parts := actor.CountActiveParts(RANGED_ATTACK)
			if parts == 0 {
				continue
			}
			for _, target := range room.Entities {
				if target.ID == actor.ID || target.HitsMax <= 0 || (target.Type == TYPE_CREEP && target.Spawning) {
					continue
				}
				if target.User == "" || target.User == actor.User {
					continue
				}
				dist := ChebyshevDist(actor.X, actor.Y, target.X, target.Y)
				if dist < 1 || dist > 3 {
					continue
				}
				dmg := parts * RangedMassAttackPower[dist]
				if effective := resolveCombatTarget(w, room, actor.User, target); effective != nil {
					deltas.addDamage(effective, dmg)
				}
			}
			actor.SetAction("rangedMassAttack", actor.X, actor.Y)

		case IntentHeal:
			target := room.Entities[in.TargetID]
			if target == nil || target.Type != TYPE_CREEP || target.Spawning || target.Hits <= 0 || ChebyshevDist(actor.X, actor.Y, target.X, target.Y) > 1 {
				continue
			}
			parts := actor.CountActiveParts(HEAL)
			if parts == 0 {
				continue
			}
			deltas.addHeal(target, parts*HEAL_POWER)
			actor.SetAction("heal", target.X, target.Y)

		case IntentRangedHeal:
			target := room.Entities[in.TargetID]
			if target == nil || target.Type != TYPE_CREEP || target.Spawning || target.Hits <= 0 || ChebyshevDist(actor.X, actor.Y, target.X, target.Y) > 3 {
				continue
			}
			parts := actor.CountActiveParts(HEAL)
			if parts == 0 {
				continue
			}
			deltas.addHeal(target, parts*RANGED_HEAL_POWER)
			actor.SetAction("rangedHeal", target.X, target.Y)

		case IntentSay:
			actor.SayText = in.Message
		}
	}
}

// towerEffectPower computes Screeps tower falloff power based on Chebyshev distance.
func towerEffectPower(basePower, dist int) int {
	if dist <= TOWER_OPTIMAL_RANGE {
		return basePower
	}
	if dist >= TOWER_FALLOFF_RANGE {
		return int(float64(basePower) * (1.0 - TOWER_FALLOFF))
	}
	ratio := float64(dist-TOWER_OPTIMAL_RANGE) / float64(TOWER_FALLOFF_RANGE-TOWER_OPTIMAL_RANGE)
	return int(float64(basePower) * (1.0 - TOWER_FALLOFF*ratio))
}

func processTowers(w *World, intents []Intent, deltas *combatDeltas) {
	// One action per tower per tick (last wins).
	byTower := make(map[string]Intent, len(intents))
	order := make([]string, 0, len(intents))
	for _, in := range intents {
		if _, exists := byTower[in.ActorID]; !exists {
			order = append(order, in.ActorID)
		}
		byTower[in.ActorID] = in
	}

	for _, tid := range order {
		in := byTower[tid]
		tower := w.getEntityLocked(tid)
		if tower == nil || tower.Type != STRUCTURE_TOWER || tower.Hits <= 0 {
			continue
		}
		if tower.UsedCapacity(RESOURCE_ENERGY) < TOWER_ENERGY_COST {
			continue
		}
		room := w.Rooms[tower.Room]
		if room == nil {
			continue
		}
		target := room.Entities[in.TargetID]
		if target == nil {
			continue
		}

		dist := ChebyshevDist(tower.X, tower.Y, target.X, target.Y)
		switch in.Type {
		case IntentTowerAttack:
			if target.ID == tower.ID || target.HitsMax <= 0 || (target.Type == TYPE_CREEP && target.Spawning) {
				continue
			}
			tower.SubStore(RESOURCE_ENERGY, TOWER_ENERGY_COST)
			if effective := resolveCombatTarget(w, room, tower.User, target); effective != nil {
				deltas.addDamage(effective, towerEffectPower(TOWER_POWER_ATTACK, dist))
			}
			tower.SetAction("attack", target.X, target.Y)
		case IntentTowerHeal:
			if target.Type != TYPE_CREEP || target.Spawning || target.Hits <= 0 {
				continue
			}
			tower.SubStore(RESOURCE_ENERGY, TOWER_ENERGY_COST)
			deltas.addHeal(target, towerEffectPower(TOWER_POWER_HEAL, dist))
			tower.SetAction("heal", target.X, target.Y)
		case IntentTowerRepair:
			if !target.IsStructure() || target.HitsMax <= 0 || target.Hits <= 0 {
				continue
			}
			tower.SubStore(RESOURCE_ENERGY, TOWER_ENERGY_COST)
			deltas.addHeal(target, towerEffectPower(TOWER_POWER_REPAIR, dist))
			tower.SetAction("repair", target.X, target.Y)
		}
	}
}

func processLinks(w *World, roomNames []string, intents []Intent) {
	transferred := make(map[string]bool)
	for _, in := range intents {
		link := w.getEntityLocked(in.ActorID)
		if link == nil || link.Type != STRUCTURE_LINK || link.Cooldown > 0 || link.Hits <= 0 {
			continue
		}
		room := w.Rooms[link.Room]
		if room == nil {
			continue
		}
		target := room.Entities[in.TargetID]
		if target == nil || target.Type != STRUCTURE_LINK || target.User != link.User || target.ID == link.ID {
			continue
		}
		amt := in.Amount
		if amt <= 0 {
			amt = link.UsedCapacity(RESOURCE_ENERGY)
		}
		if free := target.FreeCapacity(RESOURCE_ENERGY); amt > free {
			amt = free
		}
		sent := link.SubStore(RESOURCE_ENERGY, amt)
		if sent <= 0 {
			continue
		}
		loss := int(math.Ceil(float64(sent) * LINK_LOSS_RATIO))
		received := sent - loss
		if received > 0 {
			target.AddStore(RESOURCE_ENERGY, received)
		}
		link.Cooldown = ChebyshevDist(link.X, link.Y, target.X, target.Y) * LINK_COOLDOWN
		transferred[link.ID] = true
	}

	// Decrement cooldown on links that did not transfer this tick.
	for _, rName := range roomNames {
		room := w.Rooms[rName]
		for _, e := range room.Entities {
			if e.Type == STRUCTURE_LINK && e.Cooldown > 0 && !transferred[e.ID] {
				e.Cooldown--
			}
		}
	}
}

func processRoomPassives(w *World, roomNames []string) {
	for _, rName := range roomNames {
		room := w.Rooms[rName]
		ids := make([]string, 0, len(room.Entities))
		for id := range room.Entities {
			ids = append(ids, id)
		}
		sort.Strings(ids)

		ctrl := room.FindController()
		roomOwned := ctrl != nil && ctrl.User != ""

		// Track total spawn+extension energy per user for Screeps passive spawn regen (< 300).
		userEnergy := make(map[string]int)
		for _, id := range ids {
			if e, ok := room.Entities[id]; ok && (e.Type == STRUCTURE_SPAWN || e.Type == STRUCTURE_EXTENSION) {
				userEnergy[e.User] += e.UsedCapacity(RESOURCE_ENERGY)
			}
		}

		for _, id := range ids {
			e, ok := room.Entities[id]
			if !ok {
				continue
			}

			switch e.Type {
			case STRUCTURE_SPAWN:
				if e.SpawningInfo == nil && userEnergy[e.User] < SPAWN_ENERGY_CAPACITY && e.UsedCapacity(RESOURCE_ENERGY) < SPAWN_ENERGY_CAPACITY {
					if added := e.AddStore(RESOURCE_ENERGY, 1); added > 0 {
						userEnergy[e.User] += added
					}
				}

			case TYPE_SOURCE:
				if e.NextRegenerationTime > 0 && w.GameTime >= e.NextRegenerationTime {
					e.Energy = e.EnergyCapacity
					e.NextRegenerationTime = 0
				}

			case STRUCTURE_CONTROLLER:
				if e.User == "" && e.ReservationUser != "" && w.GameTime >= e.ReservationEndTime {
					e.ReservationUser = ""
					e.ReservationEndTime = 0
					syncRoomSourceCapacities(room, false)
				}
				if e.User != "" && e.Level > 0 {
					checkControllerUpgrade(w, room, e)
					if e.DowngradeTime > 0 && w.GameTime >= e.DowngradeTime {
						ownerID := e.User
						ownerName := w.userNameLocked(ownerID)
						e.Level--
						if e.Level <= 0 {
							w.recordEventLocked(room.Name, "danger", fmt.Sprintf("%s lost controller (RCL 0)", ownerName))
							e.Level = 0
							e.User = ""
							e.Progress = 0
							e.ProgressTotal = 0
							e.DowngradeTime = 0
							syncRoomSourceCapacities(room, false)
						} else {
							w.recordEventLocked(room.Name, "danger", fmt.Sprintf("%s controller downgraded to RCL %d", ownerName, e.Level))
							e.Progress = 0
							e.ProgressTotal = CONTROLLER_LEVELS[e.Level]
							e.DowngradeTime = w.GameTime + int64(CONTROLLER_DOWNGRADE[e.Level])
						}
						updateExtensionCapacities(room, ownerID, e.Level)
					}
				}

			case STRUCTURE_ROAD:
				if e.NextDecayTime == 0 {
					e.NextDecayTime = w.GameTime + ROAD_DECAY_TIME
				} else if w.GameTime >= e.NextDecayTime {
					e.Hits -= ROAD_DECAY_AMOUNT
					e.NextDecayTime = w.GameTime + ROAD_DECAY_TIME
				}

			case STRUCTURE_CONTAINER:
				period := int64(CONTAINER_DECAY_TIME)
				if roomOwned {
					period = int64(CONTAINER_DECAY_TIME_OWN)
				}
				if e.NextDecayTime == 0 {
					e.NextDecayTime = w.GameTime + period
				} else if w.GameTime >= e.NextDecayTime {
					e.Hits -= CONTAINER_DECAY
					e.NextDecayTime = w.GameTime + period
				}

			case STRUCTURE_RAMPART:
				if e.NextDecayTime == 0 {
					e.NextDecayTime = w.GameTime + RAMPART_DECAY_TIME
				} else if w.GameTime >= e.NextDecayTime {
					e.Hits -= RAMPART_DECAY_AMOUNT
					e.NextDecayTime = w.GameTime + RAMPART_DECAY_TIME
				}

			case TYPE_RESOURCE:
				decay := int(math.Ceil(float64(e.Amount) / float64(ENERGY_DECAY)))
				if decay < 1 {
					decay = 1
				}
				e.Amount -= decay
				if e.Amount <= 0 {
					delete(room.Entities, id)
				}

			case TYPE_TOMBSTONE:
				if e.DecayTime > 0 && w.GameTime >= e.DecayTime {
					for res, amt := range e.Store {
						if amt > 0 {
							dropResource(w, room, e.X, e.Y, res, amt)
						}
					}
					delete(room.Entities, id)
				}

			case TYPE_CREEP:
				if !e.Spawning && (e.Hits <= 0 || (e.AgeTime > 0 && w.GameTime >= e.AgeTime)) {
					if e.Hits <= 0 {
						w.recordEventLocked(room.Name, "combat", fmt.Sprintf("%s %s killed", w.userNameLocked(e.User), e.Name))
					}
					spawnTombstone(w, room, e)
					delete(room.Entities, id)
				}
			}

			// Remove destroyed structures.
			if e.IsStructure() && e.HitsMax > 0 && e.Hits <= 0 {
				if e.Type == STRUCTURE_SPAWN && e.SpawningInfo != nil {
					// Remove orphaned in-progress spawning creep so its name is not permanently leaked.
					delete(room.Entities, e.SpawningInfo.CreepID)
					e.SpawningInfo = nil
				}
				if e.Type == STRUCTURE_SPAWN || e.Type == STRUCTURE_TOWER || e.Type == STRUCTURE_STORAGE || e.Type == STRUCTURE_EXTENSION {
					label := e.Type
					if e.Name != "" {
						label = e.Name
					}
					w.recordEventLocked(room.Name, "danger", fmt.Sprintf("%s %s destroyed", w.userNameLocked(e.User), label))
				}
				delete(room.Entities, id)
			}
		}
	}
}

// resolveCombatTarget enforces Safe Mode and redirects hostile damage to a covering
// friendly rampart on target's tile if present. Returns nil when blocked by Safe Mode.
func resolveCombatTarget(w *World, room *Room, attackerUser string, target *Entity) *Entity {
	if ctrl := room.FindController(); ctrl != nil && ctrl.SafeMode > w.GameTime && ctrl.User != "" && attackerUser != ctrl.User && target.User == ctrl.User {
		return nil
	}
	if target.Type != STRUCTURE_RAMPART && target.User != "" && target.User != attackerUser {
		for _, e := range room.Entities {
			if e.Type == STRUCTURE_RAMPART && e.X == target.X && e.Y == target.Y && e.User == target.User && e.Hits > 0 {
				return e
			}
		}
	}
	return target
}

func updateExtensionCapacities(room *Room, ownerID string, rcl int) {
	extCap := EXTENSION_ENERGY_CAPACITY[rcl]
	for _, e := range room.Entities {
		if e.Type == STRUCTURE_EXTENSION && e.User == ownerID {
			if e.StoreCapacityResource == nil {
				e.StoreCapacityResource = make(map[string]int)
			}
			e.StoreCapacityResource[RESOURCE_ENERGY] = extCap
		}
	}
}

func syncRoomSourceCapacities(room *Room, claimedOrReserved bool) {
	capVal := SOURCE_ENERGY_NEUTRAL_CAPACITY
	if claimedOrReserved {
		capVal = SOURCE_ENERGY_CAPACITY
	}
	for _, e := range room.Entities {
		if e.Type == TYPE_SOURCE {
			e.EnergyCapacity = capVal
			if e.Energy > capVal {
				e.Energy = capVal
			}
		}
	}
}

func checkControllerUpgrade(w *World, room *Room, ctrl *Entity) {
	for ctrl.Level > 0 && ctrl.Level < 8 {
		needed := CONTROLLER_LEVELS[ctrl.Level]
		if needed <= 0 || ctrl.Progress < needed {
			break
		}
		ctrl.Progress -= needed
		ctrl.Level++
		ctrl.SafeModeAvailable++
		w.recordEventLocked(room.Name, "rcl", fmt.Sprintf("%s reached RCL %d", w.userNameLocked(ctrl.User), ctrl.Level))
		ctrl.ProgressTotal = CONTROLLER_LEVELS[ctrl.Level]
		if dg, ok := CONTROLLER_DOWNGRADE[ctrl.Level]; ok {
			ctrl.DowngradeTime = w.GameTime + int64(dg)
		}
		// Update extension capacities if RCL changed capacity tier (7 or 8).
		extCap := EXTENSION_ENERGY_CAPACITY[ctrl.Level]
		for _, e := range room.Entities {
			if e.Type == STRUCTURE_EXTENSION && e.User == ctrl.User {
				if e.StoreCapacityResource == nil {
					e.StoreCapacityResource = make(map[string]int)
				}
				e.StoreCapacityResource[RESOURCE_ENERGY] = extCap
			}
		}
	}
	if ctrl.Level == 8 {
		ctrl.Progress = 0
		ctrl.ProgressTotal = 0
	}
}

func completeConstructionSite(w *World, room *Room, site *Entity) {
	delete(room.Entities, site.ID)

	st := site.StructureType
	if st == STRUCTURE_SPAWN || st == STRUCTURE_TOWER || st == STRUCTURE_STORAGE || st == STRUCTURE_LINK {
		w.recordEventLocked(room.Name, "build", fmt.Sprintf("%s built %s", w.userNameLocked(site.User), st))
	}
	id := w.nextEntityID(st)
	e := &Entity{
		ID:      id,
		Type:    st,
		Room:    room.Name,
		X:       site.X,
		Y:       site.Y,
		Hits:    STRUCTURE_HITS[st],
		HitsMax: STRUCTURE_HITS_MAX[st],
	}

	// Roads, containers, and walls are neutral; other structures belong to the builder.
	if st != STRUCTURE_ROAD && st != STRUCTURE_CONTAINER && st != STRUCTURE_WALL {
		e.User = site.User
	}

	switch st {
	case STRUCTURE_SPAWN:
		e.Name = w.nextEntityID("Spawn")
		e.Store = map[string]int{RESOURCE_ENERGY: 0}
		e.StoreCapacityResource = map[string]int{RESOURCE_ENERGY: SPAWN_ENERGY_CAPACITY}
	case STRUCTURE_EXTENSION:
		rcl := 2
		if ctrl := room.FindController(); ctrl != nil && ctrl.Level > 0 {
			rcl = ctrl.Level
		}
		e.Store = map[string]int{RESOURCE_ENERGY: 0}
		e.StoreCapacityResource = map[string]int{RESOURCE_ENERGY: EXTENSION_ENERGY_CAPACITY[rcl]}
	case STRUCTURE_TOWER:
		e.Store = map[string]int{RESOURCE_ENERGY: 0}
		e.StoreCapacityResource = map[string]int{RESOURCE_ENERGY: TOWER_CAPACITY}
	case STRUCTURE_LINK:
		e.Store = map[string]int{RESOURCE_ENERGY: 0}
		e.StoreCapacityResource = map[string]int{RESOURCE_ENERGY: LINK_CAPACITY}
	case STRUCTURE_STORAGE:
		e.Store = make(map[string]int)
		e.StoreCapacity = STORAGE_CAPACITY
	case STRUCTURE_CONTAINER:
		e.Store = make(map[string]int)
		e.StoreCapacity = CONTAINER_CAPACITY
		e.NextDecayTime = w.GameTime + CONTAINER_DECAY_TIME_OWN
	case STRUCTURE_ROAD:
		e.NextDecayTime = w.GameTime + ROAD_DECAY_TIME
	case STRUCTURE_RAMPART:
		e.NextDecayTime = w.GameTime + RAMPART_DECAY_TIME
	}

	room.Entities[id] = e
}

func dropResource(w *World, room *Room, x, y int, resType string, amount int) {
	if amount <= 0 {
		return
	}
	// Merge with existing dropped resource of the same type on this tile.
	for _, e := range room.Entities {
		if e.Type == TYPE_RESOURCE && e.X == x && e.Y == y && e.ResourceType == resType {
			e.Amount += amount
			return
		}
	}
	id := w.nextEntityID("res")
	room.Entities[id] = &Entity{
		ID:           id,
		Type:         TYPE_RESOURCE,
		Room:         room.Name,
		X:            x,
		Y:            y,
		ResourceType: resType,
		Amount:       amount,
	}
}

func spawnTombstone(w *World, room *Room, creep *Entity) {
	storeCopy := make(map[string]int, len(creep.Store))
	for k, v := range creep.Store {
		if v > 0 {
			storeCopy[k] = v
		}
	}
	decayTicks := int64(len(creep.Body) * TOMBSTONE_DECAY_PER_PART)
	if decayTicks < 5 {
		decayTicks = 5
	}
	id := w.nextEntityID("tomb")
	room.Entities[id] = &Entity{
		ID:        id,
		Type:      TYPE_TOMBSTONE,
		Room:      room.Name,
		X:         creep.X,
		Y:         creep.Y,
		CreepID:   creep.ID,
		CreepName: creep.Name,
		DeathTime: w.GameTime,
		DecayTime: w.GameTime + decayTicks,
		Store:     storeCopy,
	}
}
