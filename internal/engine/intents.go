package engine

import (
	"strings"

	"go-screeps/internal/screeps"
)

// IntentType identifies a queued player action to be resolved during Step().
type IntentType string

const (
	IntentMove              IntentType = "move"
	IntentHarvest           IntentType = "harvest"
	IntentPickup            IntentType = "pickup"
	IntentDrop              IntentType = "drop"
	IntentTransfer          IntentType = "transfer"
	IntentWithdraw          IntentType = "withdraw"
	IntentBuild             IntentType = "build"
	IntentRepair            IntentType = "repair"
	IntentUpgradeController      IntentType = "upgradeController"
	IntentClaimController        IntentType = "claimController"
	IntentReserveController      IntentType = "reserveController"
	IntentAttackController       IntentType = "attackController"
	IntentAttack                 IntentType = "attack"
	IntentRangedAttack           IntentType = "rangedAttack"
	IntentRangedMassAttack       IntentType = "rangedMassAttack"
	IntentHeal              IntentType = "heal"
	IntentRangedHeal        IntentType = "rangedHeal"
	IntentDismantle         IntentType = "dismantle"
	IntentSay               IntentType = "say"
	IntentSpawnCreep        IntentType = "spawnCreep"
	IntentTowerAttack       IntentType = "towerAttack"
	IntentTowerHeal         IntentType = "towerHeal"
	IntentTowerRepair       IntentType = "towerRepair"
	IntentLinkTransfer           IntentType = "linkTransfer"
	IntentActivateSafeMode       IntentType = "activateSafeMode"
	IntentCreateConstructionSite IntentType = "createConstructionSite"
)

// Intent represents a validated player command queued for execution on the next Step().
type Intent struct {
	Type          IntentType
	UserID        string
	ActorID       string
	TargetID      string
	Direction     int
	ResourceType  string
	Amount        int
	Message       string
	Body          []string
	CreepName     string
	Directions    []int
	TargetRoom    string
	TargetX       int
	TargetY       int
	StructureType string
}

// QueueIntent validates an intent against current world state and queues it for Step().
// Returns OK (0) or a Screeps ERR_* code.
func (w *World) QueueIntent(userID string, intent Intent) int {
	w.mu.Lock()
	defer w.mu.Unlock()

	intent.UserID = userID
	actor := w.getEntityLocked(intent.ActorID)
	if actor == nil {
		return ERR_INVALID_TARGET
	}
	if actor.User != userID {
		return ERR_NOT_OWNER
	}
	room := w.Rooms[actor.Room]
	if room == nil {
		return ERR_INVALID_TARGET
	}

	switch intent.Type {
	case IntentSpawnCreep:
		if code := w.validateSpawnCreepLocked(userID, actor, room, intent.Body, intent.CreepName, intent.Directions); code != OK {
			return code
		}
		// Copy slice to avoid external mutation.
		bodyCopy := make([]string, len(intent.Body))
		copy(bodyCopy, intent.Body)
		intent.Body = bodyCopy
		if len(intent.Directions) > 0 {
			dirCopy := make([]int, len(intent.Directions))
			copy(dirCopy, intent.Directions)
			intent.Directions = dirCopy
		}
		w.intents = append(w.intents, intent)
		return OK

	case IntentTowerAttack, IntentTowerHeal, IntentTowerRepair:
		if actor.Type != STRUCTURE_TOWER {
			return ERR_INVALID_TARGET
		}
		if actor.UsedCapacity(RESOURCE_ENERGY) < TOWER_ENERGY_COST {
			return ERR_NOT_ENOUGH_ENERGY
		}
		target := room.Entities[intent.TargetID]
		if target == nil {
			return ERR_INVALID_TARGET
		}
		switch intent.Type {
		case IntentTowerAttack:
			if target.ID == actor.ID || target.HitsMax <= 0 || (target.Type == TYPE_CREEP && target.Spawning) {
				return ERR_INVALID_TARGET
			}
		case IntentTowerHeal:
			if target.Type != TYPE_CREEP || target.Spawning {
				return ERR_INVALID_TARGET
			}
		case IntentTowerRepair:
			if !target.IsStructure() || target.HitsMax <= 0 {
				return ERR_INVALID_TARGET
			}
		}
		w.intents = append(w.intents, intent)
		return OK

	case IntentLinkTransfer:
		if actor.Type != STRUCTURE_LINK {
			return ERR_INVALID_TARGET
		}
		if actor.Cooldown > 0 {
			return ERR_TIRED
		}
		if actor.UsedCapacity(RESOURCE_ENERGY) <= 0 {
			return ERR_NOT_ENOUGH_RESOURCES
		}
		target := room.Entities[intent.TargetID]
		if target == nil || target.Type != STRUCTURE_LINK || target.ID == actor.ID {
			return ERR_INVALID_TARGET
		}
		if target.User != userID {
			return ERR_NOT_OWNER
		}
		if target.FreeCapacity(RESOURCE_ENERGY) <= 0 {
			return ERR_FULL
		}
		w.intents = append(w.intents, intent)
		return OK

	case IntentActivateSafeMode:
		if actor.Type != STRUCTURE_CONTROLLER || actor.User != userID || actor.Level <= 0 {
			return ERR_NOT_OWNER
		}
		if actor.SafeMode > w.GameTime {
			return ERR_BUSY
		}
		if actor.SafeModeCooldown > w.GameTime {
			return ERR_TIRED
		}
		if actor.SafeModeAvailable <= 0 {
			return ERR_NOT_ENOUGH_RESOURCES
		}
		w.intents = append(w.intents, intent)
		return OK
	}

	// Remaining intents require a live, non-spawning creep.
	if actor.Type != TYPE_CREEP {
		return ERR_INVALID_TARGET
	}
	if actor.Spawning {
		return ERR_BUSY
	}

	switch intent.Type {
	case IntentMove:
		if intent.Direction < TOP || intent.Direction > TOP_LEFT {
			return ERR_INVALID_ARGS
		}
		if actor.CountActiveParts(MOVE) == 0 {
			return ERR_NO_BODYPART
		}
		if actor.Fatigue > 0 {
			return ERR_TIRED
		}

	case IntentHarvest:
		if actor.CountActiveParts(WORK) == 0 {
			return ERR_NO_BODYPART
		}
		target := room.Entities[intent.TargetID]
		if target == nil || (target.Type != TYPE_SOURCE && target.Type != TYPE_MINERAL) {
			return ERR_INVALID_TARGET
		}
		if ChebyshevDist(actor.X, actor.Y, target.X, target.Y) > 1 {
			return ERR_NOT_IN_RANGE
		}
		if target.Type == TYPE_SOURCE && target.Energy <= 0 {
			return ERR_NOT_ENOUGH_RESOURCES
		}
		if target.Type == TYPE_MINERAL && target.MineralAmount <= 0 {
			return ERR_NOT_ENOUGH_RESOURCES
		}

	case IntentPickup:
		target := room.Entities[intent.TargetID]
		if target == nil || target.Type != TYPE_RESOURCE {
			return ERR_INVALID_TARGET
		}
		if ChebyshevDist(actor.X, actor.Y, target.X, target.Y) > 1 {
			return ERR_NOT_IN_RANGE
		}
		if actor.FreeCapacity(target.ResourceType) <= 0 {
			return ERR_FULL
		}

	case IntentDrop:
		res := intent.ResourceType
		if res == "" {
			res = RESOURCE_ENERGY
			intent.ResourceType = res
		}
		if actor.UsedCapacity(res) <= 0 {
			return ERR_NOT_ENOUGH_RESOURCES
		}

	case IntentTransfer:
		res := intent.ResourceType
		if res == "" {
			res = RESOURCE_ENERGY
			intent.ResourceType = res
		}
		target := room.Entities[intent.TargetID]
		if target == nil || target.ID == actor.ID {
			return ERR_INVALID_TARGET
		}
		if target.Capacity(res) <= 0 {
			return ERR_INVALID_TARGET
		}
		if ChebyshevDist(actor.X, actor.Y, target.X, target.Y) > 1 {
			return ERR_NOT_IN_RANGE
		}
		if actor.UsedCapacity(res) <= 0 {
			return ERR_NOT_ENOUGH_RESOURCES
		}
		if target.FreeCapacity(res) <= 0 {
			return ERR_FULL
		}

	case IntentWithdraw:
		res := intent.ResourceType
		if res == "" {
			res = RESOURCE_ENERGY
			intent.ResourceType = res
		}
		target := room.Entities[intent.TargetID]
		if target == nil || target.ID == actor.ID || (!target.IsStructure() && target.Type != TYPE_TOMBSTONE) {
			return ERR_INVALID_TARGET
		}
		if target.User != "" && target.User != userID && target.Type != TYPE_TOMBSTONE && target.Type != STRUCTURE_CONTAINER {
			return ERR_NOT_OWNER
		}
		if ChebyshevDist(actor.X, actor.Y, target.X, target.Y) > 1 {
			return ERR_NOT_IN_RANGE
		}
		if target.UsedCapacity(res) <= 0 {
			return ERR_NOT_ENOUGH_RESOURCES
		}
		if actor.FreeCapacity(res) <= 0 {
			return ERR_FULL
		}

	case IntentBuild:
		if actor.CountActiveParts(WORK) == 0 {
			return ERR_NO_BODYPART
		}
		if actor.UsedCapacity(RESOURCE_ENERGY) <= 0 {
			return ERR_NOT_ENOUGH_RESOURCES
		}
		target := room.Entities[intent.TargetID]
		if target == nil || target.Type != TYPE_CONSTRUCTION_SITE {
			return ERR_INVALID_TARGET
		}
		if ChebyshevDist(actor.X, actor.Y, target.X, target.Y) > 3 {
			return ERR_NOT_IN_RANGE
		}

	case IntentRepair:
		if actor.CountActiveParts(WORK) == 0 {
			return ERR_NO_BODYPART
		}
		if actor.UsedCapacity(RESOURCE_ENERGY) <= 0 {
			return ERR_NOT_ENOUGH_RESOURCES
		}
		target := room.Entities[intent.TargetID]
		if target == nil || !target.IsStructure() || target.HitsMax <= 0 {
			return ERR_INVALID_TARGET
		}
		if ChebyshevDist(actor.X, actor.Y, target.X, target.Y) > 3 {
			return ERR_NOT_IN_RANGE
		}

	case IntentUpgradeController:
		if actor.CountActiveParts(WORK) == 0 {
			return ERR_NO_BODYPART
		}
		if actor.UsedCapacity(RESOURCE_ENERGY) <= 0 {
			return ERR_NOT_ENOUGH_RESOURCES
		}
		target := room.Entities[intent.TargetID]
		if target == nil || target.Type != STRUCTURE_CONTROLLER {
			return ERR_INVALID_TARGET
		}
		if target.User != userID {
			return ERR_NOT_OWNER
		}
		if target.UpgradeBlocked > w.GameTime {
			return ERR_INVALID_TARGET
		}
		if ChebyshevDist(actor.X, actor.Y, target.X, target.Y) > 3 {
			return ERR_NOT_IN_RANGE
		}

	case IntentClaimController:
		if actor.CountActiveParts(CLAIM) == 0 {
			return ERR_NO_BODYPART
		}
		target := room.Entities[intent.TargetID]
		if target == nil || target.Type != STRUCTURE_CONTROLLER {
			return ERR_INVALID_TARGET
		}
		if target.User != "" {
			return ERR_INVALID_TARGET
		}
		if target.ReservationUser != "" && target.ReservationUser != userID && target.ReservationEndTime > w.GameTime {
			return ERR_INVALID_TARGET
		}
		if ChebyshevDist(actor.X, actor.Y, target.X, target.Y) > 1 {
			return ERR_NOT_IN_RANGE
		}
		gclLevel, _, _ := w.UserGCLInfoLocked(userID)
		if w.OwnedRoomCountLocked(userID) >= gclLevel {
			return ERR_GCL_NOT_ENOUGH
		}

	case IntentReserveController:
		if actor.CountActiveParts(CLAIM) == 0 {
			return ERR_NO_BODYPART
		}
		target := room.Entities[intent.TargetID]
		if target == nil || target.Type != STRUCTURE_CONTROLLER {
			return ERR_INVALID_TARGET
		}
		if target.User != "" {
			return ERR_INVALID_TARGET
		}
		if target.ReservationUser != "" && target.ReservationUser != userID && target.ReservationEndTime > w.GameTime {
			return ERR_INVALID_TARGET
		}
		if ChebyshevDist(actor.X, actor.Y, target.X, target.Y) > 1 {
			return ERR_NOT_IN_RANGE
		}

	case IntentAttackController:
		if actor.CountActiveParts(CLAIM) == 0 {
			return ERR_NO_BODYPART
		}
		target := room.Entities[intent.TargetID]
		if target == nil || target.Type != STRUCTURE_CONTROLLER {
			return ERR_INVALID_TARGET
		}
		hasOwner := target.User != ""
		hasRes := target.ReservationUser != "" && target.ReservationEndTime > w.GameTime
		if !hasOwner && !hasRes {
			return ERR_INVALID_TARGET
		}
		if target.UpgradeBlocked > w.GameTime || (target.SafeMode > w.GameTime && target.User != userID) {
			return ERR_TIRED
		}
		if ChebyshevDist(actor.X, actor.Y, target.X, target.Y) > 1 {
			return ERR_NOT_IN_RANGE
		}

	case IntentRangedMassAttack:
		if actor.CountActiveParts(RANGED_ATTACK) == 0 {
			return ERR_NO_BODYPART
		}

	case IntentAttack:
		if actor.CountActiveParts(ATTACK) == 0 {
			return ERR_NO_BODYPART
		}
		target := room.Entities[intent.TargetID]
		if target == nil || target.ID == actor.ID || target.HitsMax <= 0 || (target.Type == TYPE_CREEP && target.Spawning) {
			return ERR_INVALID_TARGET
		}
		if ChebyshevDist(actor.X, actor.Y, target.X, target.Y) > 1 {
			return ERR_NOT_IN_RANGE
		}

	case IntentRangedAttack:
		if actor.CountActiveParts(RANGED_ATTACK) == 0 {
			return ERR_NO_BODYPART
		}
		target := room.Entities[intent.TargetID]
		if target == nil || target.ID == actor.ID || target.HitsMax <= 0 || (target.Type == TYPE_CREEP && target.Spawning) {
			return ERR_INVALID_TARGET
		}
		if ChebyshevDist(actor.X, actor.Y, target.X, target.Y) > 3 {
			return ERR_NOT_IN_RANGE
		}

	case IntentHeal:
		if actor.CountActiveParts(HEAL) == 0 {
			return ERR_NO_BODYPART
		}
		target := room.Entities[intent.TargetID]
		if target == nil || target.Type != TYPE_CREEP || target.Spawning {
			return ERR_INVALID_TARGET
		}
		if ChebyshevDist(actor.X, actor.Y, target.X, target.Y) > 1 {
			return ERR_NOT_IN_RANGE
		}

	case IntentRangedHeal:
		if actor.CountActiveParts(HEAL) == 0 {
			return ERR_NO_BODYPART
		}
		target := room.Entities[intent.TargetID]
		if target == nil || target.Type != TYPE_CREEP || target.Spawning {
			return ERR_INVALID_TARGET
		}
		if ChebyshevDist(actor.X, actor.Y, target.X, target.Y) > 3 {
			return ERR_NOT_IN_RANGE
		}

	case IntentDismantle:
		if actor.CountActiveParts(WORK) == 0 {
			return ERR_NO_BODYPART
		}
		target := room.Entities[intent.TargetID]
		if target == nil || !target.IsStructure() || target.HitsMax <= 0 {
			return ERR_INVALID_TARGET
		}
		if ChebyshevDist(actor.X, actor.Y, target.X, target.Y) > 1 {
			return ERR_NOT_IN_RANGE
		}

	case IntentSay:
		// Always valid for an owned non-spawning creep.

	default:
		return ERR_INVALID_ARGS
	}

	w.intents = append(w.intents, intent)
	return OK
}

// ValidateSpawnCreep validates a spawnCreep call without queuing an intent (used for dryRun).
func (w *World) ValidateSpawnCreep(userID, spawnID string, body []string, creepName string, directions []int) int {
	w.mu.RLock()
	defer w.mu.RUnlock()

	actor := w.getEntityLocked(spawnID)
	if actor == nil {
		return ERR_INVALID_TARGET
	}
	if actor.User != userID {
		return ERR_NOT_OWNER
	}
	room := w.Rooms[actor.Room]
	if room == nil {
		return ERR_INVALID_TARGET
	}
	return w.validateSpawnCreepLocked(userID, actor, room, body, creepName, directions)
}

func (w *World) validateSpawnCreepLocked(userID string, actor *Entity, room *Room, body []string, creepName string, directions []int) int {
	if actor.Type != STRUCTURE_SPAWN {
		return ERR_INVALID_TARGET
	}
	if actor.SpawningInfo != nil {
		return ERR_BUSY
	}
	if strings.TrimSpace(creepName) == "" {
		return ERR_INVALID_ARGS
	}
	cost := BodyCost(body)
	if cost <= 0 {
		return ERR_INVALID_ARGS
	}
	for _, d := range directions {
		if d < TOP || d > TOP_LEFT {
			return ERR_INVALID_ARGS
		}
	}
	// Check duplicate creep name across world.
	for _, r := range w.Rooms {
		for _, e := range r.Entities {
			if e.Type == TYPE_CREEP && e.User == userID && e.Name == creepName {
				return ERR_NAME_EXISTS
			}
		}
	}
	// Also check pending spawn intents this tick.
	for _, pending := range w.intents {
		if pending.Type == IntentSpawnCreep && pending.UserID == userID {
			if pending.ActorID == actor.ID {
				return ERR_BUSY
			}
			if pending.CreepName == creepName {
				return ERR_NAME_EXISTS
			}
		}
	}
	if room.EnergyAvailable(userID) < cost {
		return ERR_NOT_ENOUGH_ENERGY
	}
	return OK
}

// QueueCreateConstructionSite validates and queues a construction site to be materialized on Step().
func (w *World) QueueCreateConstructionSite(userID, roomName string, x, y int, structureType string) int {
	w.mu.Lock()
	defer w.mu.Unlock()

	roomName = strings.ToUpper(strings.TrimSpace(roomName))
	if _, _, code := w.validateConstructionSiteLocked(userID, roomName, x, y, structureType, true); code != OK {
		return code
	}
	w.intents = append(w.intents, Intent{
		Type:          IntentCreateConstructionSite,
		UserID:        userID,
		TargetRoom:    roomName,
		TargetX:       x,
		TargetY:       y,
		StructureType: structureType,
	})
	return OK
}

// CreateConstructionSite validates and immediately places a new construction site in roomName.
// Returns (siteID, OK) or ("", ERR_*).
func (w *World) CreateConstructionSite(userID, roomName string, x, y int, structureType string) (string, int) {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.createConstructionSiteLocked(userID, roomName, x, y, structureType)
}

func (w *World) createConstructionSiteLocked(userID, roomName string, x, y int, structureType string) (string, int) {
	roomName = strings.ToUpper(strings.TrimSpace(roomName))
	room, cost, code := w.validateConstructionSiteLocked(userID, roomName, x, y, structureType, false)
	if code != OK {
		return "", code
	}
	id := w.nextEntityID("site")
	room.Entities[id] = &Entity{
		ID:            id,
		Type:          TYPE_CONSTRUCTION_SITE,
		Room:          roomName,
		X:             x,
		Y:             y,
		User:          userID,
		StructureType: structureType,
		Progress:      0,
		ProgressTotal: cost,
	}
	return id, OK
}

func (w *World) validateConstructionSiteLocked(userID, roomName string, x, y int, structureType string, checkPending bool) (*Room, int, int) {
	room, ok := w.Rooms[roomName]
	if !ok {
		return nil, 0, ERR_INVALID_ARGS
	}
	if x <= 0 || x >= RoomSize-1 || y <= 0 || y >= RoomSize-1 {
		return nil, 0, ERR_INVALID_TARGET
	}
	cost, validType := CONSTRUCTION_COST[structureType]
	if !validType {
		return nil, 0, ERR_INVALID_ARGS
	}
	if room.TerrainAt(x, y) == TERRAIN_MASK_WALL {
		return nil, 0, ERR_INVALID_TARGET
	}

	// Check RCL structure limit.
	rcl := 0
	if ctrl := room.FindController(); ctrl != nil && ctrl.User == userID {
		rcl = ctrl.Level
	}
	maxAllowed := 0
	if limits, ok := CONTROLLER_STRUCTURES[structureType]; ok {
		maxAllowed = limits[rcl]
	}
	existingCount := 0
	totalSites := 0
	for _, e := range room.Entities {
		if e.Type == TYPE_CONSTRUCTION_SITE {
			totalSites++
			if e.User == userID && e.StructureType == structureType {
				existingCount++
			}
		} else if e.Type == structureType {
			if e.User == userID || structureType == STRUCTURE_ROAD || structureType == STRUCTURE_CONTAINER {
				existingCount++
			}
		}
	}
	if checkPending {
		for _, pending := range w.intents {
			if pending.Type != IntentCreateConstructionSite || pending.TargetRoom != roomName {
				continue
			}
			totalSites++
			if pending.UserID == userID && pending.StructureType == structureType {
				existingCount++
			}
			if pending.TargetX == x && pending.TargetY == y {
				return nil, 0, ERR_INVALID_TARGET
			}
		}
	}
	if totalSites >= CONSTRUCTION_SITE_MAX_NUM {
		return nil, 0, ERR_FULL
	}
	if existingCount >= maxAllowed {
		return nil, 0, ERR_RCL_NOT_ENOUGH
	}

	// Check conflicting objects on the same tile.
	hasMineral := false
	for _, e := range room.Entities {
		if e.X != x || e.Y != y {
			continue
		}
		if e.Type == TYPE_MINERAL {
			hasMineral = true
		}
		if e.Type == TYPE_CONSTRUCTION_SITE {
			return nil, 0, ERR_INVALID_TARGET
		}
		if e.Type == structureType {
			return nil, 0, ERR_INVALID_TARGET
		}
		if structureType == STRUCTURE_RAMPART {
			// Ramparts can be built over friendly/neutral structures except walls/sources/controllers/minerals.
			if e.Type == STRUCTURE_WALL || e.Type == TYPE_SOURCE || e.Type == TYPE_MINERAL || e.Type == STRUCTURE_CONTROLLER {
				return nil, 0, ERR_INVALID_TARGET
			}
			if e.IsStructure() && e.User != "" && e.User != userID {
				return nil, 0, ERR_INVALID_TARGET
			}
			continue
		}
		if structureType == STRUCTURE_ROAD {
			// Roads can be built under ramparts or creeps, not under blocking structures.
			if e.Type != TYPE_CREEP && e.Type != STRUCTURE_RAMPART && e.Type != TYPE_RESOURCE && e.Type != TYPE_TOMBSTONE {
				return nil, 0, ERR_INVALID_TARGET
			}
			continue
		}
		if structureType == STRUCTURE_EXTRACTOR {
			if e.Type == TYPE_MINERAL {
				continue
			}
		}
		if e.Type != TYPE_CREEP && e.Type != STRUCTURE_RAMPART && e.Type != TYPE_RESOURCE && e.Type != TYPE_TOMBSTONE {
			return nil, 0, ERR_INVALID_TARGET
		}
	}
	if structureType == STRUCTURE_EXTRACTOR && !hasMineral {
		return nil, 0, ERR_INVALID_TARGET
	}
	return room, cost, OK
}

// AddVisual appends a RoomVisual command to be exposed on the room's next Snapshot().
func (w *World) AddVisual(roomName string, op screeps.VisualOp) {
	w.mu.Lock()
	defer w.mu.Unlock()

	roomName = strings.ToUpper(strings.TrimSpace(roomName))
	room, ok := w.Rooms[roomName]
	if !ok {
		return
	}
	room.pendingVisuals = append(room.pendingVisuals, op)
}
