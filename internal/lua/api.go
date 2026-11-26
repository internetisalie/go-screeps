package lua

import (
	"math"
	"sort"
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"

	"go-screeps/internal/engine"
	"go-screeps/internal/screeps"
)

const costMatrixTypeName = "PathFinder.CostMatrix"

// initAPI injects all Screeps constants, reusable metatables, and PathFinder into vm.L.
func (vm *BotVM) initAPI() {
	vm.injectConstants()
	vm.initMetatables()
	vm.injectPathFinder()
}

func (vm *BotVM) injectConstants() {
	L := vm.L

	intConsts := map[string]int{
		// Return / error codes
		"OK":                        engine.OK,
		"ERR_NOT_OWNER":             engine.ERR_NOT_OWNER,
		"ERR_NO_PATH":               engine.ERR_NO_PATH,
		"ERR_NAME_EXISTS":           engine.ERR_NAME_EXISTS,
		"ERR_BUSY":                  engine.ERR_BUSY,
		"ERR_NOT_FOUND":             engine.ERR_NOT_FOUND,
		"ERR_NOT_ENOUGH_ENERGY":     engine.ERR_NOT_ENOUGH_ENERGY,
		"ERR_NOT_ENOUGH_RESOURCES":  engine.ERR_NOT_ENOUGH_RESOURCES,
		"ERR_NOT_ENOUGH_EXTENSIONS": engine.ERR_NOT_ENOUGH_EXTENSIONS,
		"ERR_INVALID_TARGET":        engine.ERR_INVALID_TARGET,
		"ERR_FULL":                  engine.ERR_FULL,
		"ERR_NOT_IN_RANGE":          engine.ERR_NOT_IN_RANGE,
		"ERR_INVALID_ARGS":          engine.ERR_INVALID_ARGS,
		"ERR_TIRED":                 engine.ERR_TIRED,
		"ERR_NO_BODYPART":           engine.ERR_NO_BODYPART,
		"ERR_RCL_NOT_ENOUGH":        engine.ERR_RCL_NOT_ENOUGH,
		"ERR_GCL_NOT_ENOUGH":        engine.ERR_GCL_NOT_ENOUGH,

		// Directions
		"TOP":          engine.TOP,
		"TOP_RIGHT":    engine.TOP_RIGHT,
		"RIGHT":        engine.RIGHT,
		"BOTTOM_RIGHT": engine.BOTTOM_RIGHT,
		"BOTTOM":       engine.BOTTOM,
		"BOTTOM_LEFT":  engine.BOTTOM_LEFT,
		"LEFT":         engine.LEFT,
		"TOP_LEFT":     engine.TOP_LEFT,

		// Terrain bitmasks
		"TERRAIN_MASK_PLAIN": engine.TERRAIN_MASK_PLAIN,
		"TERRAIN_MASK_WALL":  engine.TERRAIN_MASK_WALL,
		"TERRAIN_MASK_SWAMP": engine.TERRAIN_MASK_SWAMP,

		// FIND_*
		"FIND_EXIT_TOP":                   engine.FIND_EXIT_TOP,
		"FIND_EXIT_RIGHT":                 engine.FIND_EXIT_RIGHT,
		"FIND_EXIT_BOTTOM":                engine.FIND_EXIT_BOTTOM,
		"FIND_EXIT_LEFT":                  engine.FIND_EXIT_LEFT,
		"FIND_EXIT":                       engine.FIND_EXIT,
		"FIND_CREEPS":                     engine.FIND_CREEPS,
		"FIND_MY_CREEPS":                  engine.FIND_MY_CREEPS,
		"FIND_HOSTILE_CREEPS":             engine.FIND_HOSTILE_CREEPS,
		"FIND_SOURCES_ACTIVE":             engine.FIND_SOURCES_ACTIVE,
		"FIND_SOURCES":                    engine.FIND_SOURCES,
		"FIND_DROPPED_RESOURCES":          engine.FIND_DROPPED_RESOURCES,
		"FIND_STRUCTURES":                 engine.FIND_STRUCTURES,
		"FIND_MY_STRUCTURES":              engine.FIND_MY_STRUCTURES,
		"FIND_HOSTILE_STRUCTURES":         engine.FIND_HOSTILE_STRUCTURES,
		"FIND_FLAGS":                      engine.FIND_FLAGS,
		"FIND_CONSTRUCTION_SITES":         engine.FIND_CONSTRUCTION_SITES,
		"FIND_MY_SPAWNS":                  engine.FIND_MY_SPAWNS,
		"FIND_HOSTILE_SPAWNS":             engine.FIND_HOSTILE_SPAWNS,
		"FIND_MY_CONSTRUCTION_SITES":      engine.FIND_MY_CONSTRUCTION_SITES,
		"FIND_HOSTILE_CONSTRUCTION_SITES": engine.FIND_HOSTILE_CONSTRUCTION_SITES,
		"FIND_MINERALS":                   engine.FIND_MINERALS,
		"FIND_TOMBSTONES":                 engine.FIND_TOMBSTONES,

		// Creep & structure tuning constants
		"MAX_CREEP_SIZE":           engine.MAX_CREEP_SIZE,
		"CREEP_LIFE_TIME":          engine.CREEP_LIFE_TIME,
		"CREEP_CLAIM_LIFE_TIME":    engine.CREEP_CLAIM_LIFE_TIME,
		"CREEP_SPAWN_TIME":         engine.CREEP_SPAWN_TIME,
		"CARRY_CAPACITY":           engine.CARRY_CAPACITY,
		"HARVEST_POWER":            engine.HARVEST_POWER,
		"REPAIR_POWER":             engine.REPAIR_POWER,
		"BUILD_POWER":              engine.BUILD_POWER,
		"ATTACK_POWER":             engine.ATTACK_POWER,
		"UPGRADE_CONTROLLER_POWER": engine.UPGRADE_CONTROLLER_POWER,
		"RANGED_ATTACK_POWER":      engine.RANGED_ATTACK_POWER,
		"HEAL_POWER":               engine.HEAL_POWER,
		"RANGED_HEAL_POWER":        engine.RANGED_HEAL_POWER,
		"SOURCE_ENERGY_CAPACITY":   engine.SOURCE_ENERGY_CAPACITY,
		"SPAWN_ENERGY_CAPACITY":    engine.SPAWN_ENERGY_CAPACITY,
		"STORAGE_CAPACITY":         engine.STORAGE_CAPACITY,
		"CONTAINER_CAPACITY":       engine.CONTAINER_CAPACITY,
		"LINK_CAPACITY":            engine.LINK_CAPACITY,
		"TOWER_CAPACITY":           engine.TOWER_CAPACITY,
	}
	for k, v := range intConsts {
		L.SetGlobal(k, lua.LNumber(v))
	}

	strConsts := map[string]string{
		// Body parts
		"MOVE":          engine.MOVE,
		"WORK":          engine.WORK,
		"CARRY":         engine.CARRY,
		"ATTACK":        engine.ATTACK,
		"RANGED_ATTACK": engine.RANGED_ATTACK,
		"TOUGH":         engine.TOUGH,
		"HEAL":          engine.HEAL,
		"CLAIM":         engine.CLAIM,

		// Resources
		"RESOURCE_ENERGY":    engine.RESOURCE_ENERGY,
		"RESOURCE_HYDROGEN":  engine.RESOURCE_HYDROGEN,
		"RESOURCE_OXYGEN":    engine.RESOURCE_OXYGEN,
		"RESOURCE_UTRIUM":    engine.RESOURCE_UTRIUM,
		"RESOURCE_LEMERGIUM": engine.RESOURCE_LEMERGIUM,
		"RESOURCE_KEANIUM":   engine.RESOURCE_KEANIUM,
		"RESOURCE_ZYNTHIUM":  engine.RESOURCE_ZYNTHIUM,
		"RESOURCE_CATALYST":  engine.RESOURCE_CATALYST,

		// Structures
		"STRUCTURE_SPAWN":      engine.STRUCTURE_SPAWN,
		"STRUCTURE_EXTENSION":  engine.STRUCTURE_EXTENSION,
		"STRUCTURE_ROAD":       engine.STRUCTURE_ROAD,
		"STRUCTURE_WALL":       engine.STRUCTURE_WALL,
		"STRUCTURE_RAMPART":    engine.STRUCTURE_RAMPART,
		"STRUCTURE_LINK":       engine.STRUCTURE_LINK,
		"STRUCTURE_STORAGE":    engine.STRUCTURE_STORAGE,
		"STRUCTURE_TOWER":      engine.STRUCTURE_TOWER,
		"STRUCTURE_CONTAINER":  engine.STRUCTURE_CONTAINER,
		"STRUCTURE_CONTROLLER": engine.STRUCTURE_CONTROLLER,
		"STRUCTURE_EXTRACTOR":  engine.STRUCTURE_EXTRACTOR,

		// LOOK_*
		"LOOK_CREEPS":             "creep",
		"LOOK_ENERGY":             "energy",
		"LOOK_RESOURCES":          "resource",
		"LOOK_SOURCES":            "source",
		"LOOK_MINERALS":           "mineral",
		"LOOK_STRUCTURES":         "structure",
		"LOOK_FLAGS":              "flag",
		"LOOK_CONSTRUCTION_SITES": "constructionSite",
		"LOOK_TERRAIN":            "terrain",
		"LOOK_TOMBSTONES":         "tombstone",
	}
	for k, v := range strConsts {
		L.SetGlobal(k, lua.LString(v))
	}

	// BODYPART_COST table
	bpCost := L.NewTable()
	for part, cost := range engine.BODYPART_COST {
		bpCost.RawSetString(part, lua.LNumber(cost))
	}
	L.SetGlobal("BODYPART_COST", bpCost)

	// RoomPosition constructor global
	L.SetGlobal("RoomPosition", L.NewFunction(func(L *lua.LState) int {
		x := L.CheckInt(1)
		y := L.CheckInt(2)
		roomName := L.CheckString(3)
		L.Push(vm.newRoomPosition(x, y, roomName))
		return 1
	}))
}

func (vm *BotVM) initMetatables() {
	L := vm.L

	makeMT := func(methods map[string]lua.LGFunction) *lua.LTable {
		mt := L.NewTable()
		idx := L.NewTable()
		for name, fn := range methods {
			idx.RawSetString(name, L.NewFunction(fn))
		}
		mt.RawSetString("__index", idx)
		return mt
	}

	vm.roomPosMT = makeMT(map[string]lua.LGFunction{
		"getRangeTo":         vm.posGetRangeTo,
		"isNearTo":           vm.posIsNearTo,
		"inRangeTo":          vm.posInRangeTo,
		"isEqualTo":          vm.posIsEqualTo,
		"findClosestByRange": vm.posFindClosestByRange,
	})

	vm.terrainMT = makeMT(map[string]lua.LGFunction{
		"get": vm.terrainGet,
	})

	vm.visualMT = makeMT(map[string]lua.LGFunction{
		"circle": vm.visualCircle,
		"line":   vm.visualLine,
		"rect":   vm.visualRect,
		"poly":   vm.visualPoly,
		"text":   vm.visualText,
	})

	vm.storeMT = makeMT(map[string]lua.LGFunction{
		"getUsedCapacity": vm.storeGetUsedCapacity,
		"getFreeCapacity": vm.storeGetFreeCapacity,
		"getCapacity":     vm.storeGetCapacity,
	})

	vm.roomMT = makeMT(map[string]lua.LGFunction{
		"find":                   vm.roomFind,
		"createConstructionSite": vm.roomCreateConstructionSite,
		"getTerrain":             vm.roomGetTerrain,
		"lookForAt":              vm.roomLookForAt,
		"findExitTo":             vm.roomFindExitTo,
	})

	vm.creepMT = makeMT(map[string]lua.LGFunction{
		"move":              vm.creepMove,
		"moveTo":            vm.creepMoveTo,
		"harvest":           vm.creepTargetIntent(engine.IntentHarvest),
		"transfer":          vm.creepTransfer,
		"withdraw":          vm.creepWithdraw,
		"pickup":            vm.creepTargetIntent(engine.IntentPickup),
		"drop":              vm.creepDrop,
		"build":             vm.creepTargetIntent(engine.IntentBuild),
		"repair":            vm.creepTargetIntent(engine.IntentRepair),
		"upgradeController": vm.creepTargetIntent(engine.IntentUpgradeController),
		"attack":            vm.creepTargetIntent(engine.IntentAttack),
		"rangedAttack":      vm.creepTargetIntent(engine.IntentRangedAttack),
		"heal":              vm.creepTargetIntent(engine.IntentHeal),
		"rangedHeal":        vm.creepTargetIntent(engine.IntentRangedHeal),
		"dismantle":         vm.creepTargetIntent(engine.IntentDismantle),
		"say":               vm.creepSay,
	})

	vm.spawnMT = makeMT(map[string]lua.LGFunction{
		"spawnCreep": vm.spawnCreep,
	})

	vm.towerMT = makeMT(map[string]lua.LGFunction{
		"attack": vm.structureTargetIntent(engine.IntentTowerAttack),
		"heal":   vm.structureTargetIntent(engine.IntentTowerHeal),
		"repair": vm.structureTargetIntent(engine.IntentTowerRepair),
	})

	vm.linkMT = makeMT(map[string]lua.LGFunction{
		"transferEnergy": vm.linkTransferEnergy,
	})

	// CostMatrix userdata metatable
	cmMT := L.NewTypeMetatable(costMatrixTypeName)
	cmIdx := L.NewTable()
	cmIdx.RawSetString("set", L.NewFunction(vm.costMatrixSet))
	cmIdx.RawSetString("get", L.NewFunction(vm.costMatrixGet))
	cmIdx.RawSetString("clone", L.NewFunction(vm.costMatrixClone))
	cmMT.RawSetString("__index", cmIdx)
	vm.costMatMT = cmMT
}

func (vm *BotVM) injectPathFinder() {
	L := vm.L
	pf := L.NewTable()

	newCM := func(L *lua.LState) int {
		ud := L.NewUserData()
		ud.Value = engine.NewCostMatrix()
		L.SetMetatable(ud, vm.costMatMT)
		L.Push(ud)
		return 1
	}

	cmCtor := L.NewTable()
	cmCtor.RawSetString("new", L.NewFunction(newCM))
	cmCtorMT := L.NewTable()
	cmCtorMT.RawSetString("__call", L.NewFunction(newCM))
	L.SetMetatable(cmCtor, cmCtorMT)

	pf.RawSetString("CostMatrix", cmCtor)
	pf.RawSetString("search", L.NewFunction(vm.pathFinderSearch))
	L.SetGlobal("PathFinder", pf)
}

// refreshGame rebuilds the global Game table from world for this VM's UserID.
func (vm *BotVM) refreshGame(world *engine.World) {
	L := vm.L
	vm.objCache = make(map[string]*lua.LTable)
	vm.roomCache = make(map[string]*lua.LTable)

	game := L.NewTable()
	roomsTbl := L.NewTable()
	creepsTbl := L.NewTable()
	spawnsTbl := L.NewTable()
	structsTbl := L.NewTable()
	sitesTbl := L.NewTable()

	world.RLock()
	gameTime := world.GameTime
	var visibleRooms []string
	for name, r := range world.Rooms {
		hasOwned := false
		for _, e := range r.Entities {
			if e.User == vm.UserID {
				hasOwned = true
				break
			}
		}
		if hasOwned {
			visibleRooms = append(visibleRooms, name)
		}
	}
	sort.Strings(visibleRooms)

	type entitySnap struct {
		id       string
		roomName string
		typ      string
		name     string
		user     string
		isStruct bool
	}
	var ownedEntities []entitySnap
	for _, rName := range visibleRooms {
		r := world.Rooms[rName]
		ids := make([]string, 0, len(r.Entities))
		for id := range r.Entities {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			e := r.Entities[id]
			if e.User == vm.UserID {
				ownedEntities = append(ownedEntities, entitySnap{
					id:       e.ID,
					roomName: rName,
					typ:      e.Type,
					name:     e.Name,
					user:     e.User,
					isStruct: e.IsStructure(),
				})
			}
		}
	}
	world.RUnlock()

	game.RawSetString("time", lua.LNumber(gameTime))

	for _, rName := range visibleRooms {
		rTbl := vm.wrapRoomByName(rName)
		if rTbl != nil {
			roomsTbl.RawSetString(rName, rTbl)
		}
	}

	for _, es := range ownedEntities {
		obj := vm.wrapEntityByID(es.id)
		if obj == nil {
			continue
		}
		switch {
		case es.typ == engine.TYPE_CREEP:
			if es.name != "" {
				creepsTbl.RawSetString(es.name, obj)
			}
		case es.typ == engine.STRUCTURE_SPAWN:
			if es.name != "" {
				spawnsTbl.RawSetString(es.name, obj)
			}
			structsTbl.RawSetString(es.id, obj)
		case es.typ == engine.TYPE_CONSTRUCTION_SITE:
			sitesTbl.RawSetString(es.id, obj)
		case es.isStruct:
			structsTbl.RawSetString(es.id, obj)
		}
	}

	game.RawSetString("rooms", roomsTbl)
	game.RawSetString("creeps", creepsTbl)
	game.RawSetString("spawns", spawnsTbl)
	game.RawSetString("structures", structsTbl)
	game.RawSetString("constructionSites", sitesTbl)

	game.RawSetString("getObjectById", L.NewFunction(func(L *lua.LState) int {
		id := L.OptString(1, "")
		if id == "" {
			L.Push(lua.LNil)
			return 1
		}
		obj := vm.wrapEntityByID(id)
		if obj == nil {
			L.Push(lua.LNil)
			return 1
		}
		L.Push(obj)
		return 1
	}))

	cpuTbl := L.NewTable()
	limitMs := float64(vm.CPULimit) / float64(time.Millisecond)
	if limitMs <= 0 {
		limitMs = float64(defaultCPULimit) / float64(time.Millisecond)
	}
	cpuTbl.RawSetString("limit", lua.LNumber(limitMs))
	cpuTbl.RawSetString("tickLimit", lua.LNumber(limitMs))
	cpuTbl.RawSetString("bucket", lua.LNumber(10000))
	cpuTbl.RawSetString("getUsed", L.NewFunction(func(L *lua.LState) int {
		usedMs := float64(time.Since(vm.tickStart)) / float64(time.Millisecond)
		L.Push(lua.LNumber(usedMs))
		return 1
	}))
	game.RawSetString("cpu", cpuTbl)

	mapTbl := L.NewTable()
	mapTbl.RawSetString("describeExits", L.NewFunction(func(L *lua.LState) int {
		rName := strings.ToUpper(strings.TrimSpace(L.OptString(1, "")))
		if rName == "" || vm.world == nil {
			L.Push(lua.LNil)
			return 1
		}
		vm.world.RLock()
		defer vm.world.RUnlock()
		r := vm.world.Rooms[rName]
		if r == nil {
			L.Push(lua.LNil)
			return 1
		}
		exits := L.NewTable()
		dirs := []struct {
			key   string
			exitC int
			dx    int
			dy    int
		}{
			{"1", engine.FIND_EXIT_TOP, 0, -1},
			{"3", engine.FIND_EXIT_RIGHT, 1, 0},
			{"5", engine.FIND_EXIT_BOTTOM, 0, 1},
			{"7", engine.FIND_EXIT_LEFT, -1, 0},
		}
		for _, d := range dirs {
			if !r.RoomHasExitEdge(d.exitC) {
				continue
			}
			adj, ok := engine.AdjacentRoomName(rName, d.dx, d.dy)
			if !ok {
				continue
			}
			if vm.world.Rooms[adj] != nil {
				exits.RawSetString(d.key, lua.LString(adj))
			}
		}
		L.Push(exits)
		return 1
	}))
	game.RawSetString("map", mapTbl)

	L.SetGlobal("Game", game)
}

// --- Memory helpers ---

func (vm *BotVM) ensureMemorySubtable(key string) *lua.LTable {
	L := vm.L
	mem, ok := L.GetGlobal("Memory").(*lua.LTable)
	if !ok {
		mem = L.NewTable()
		L.SetGlobal("Memory", mem)
	}
	sub, ok := mem.RawGetString(key).(*lua.LTable)
	if !ok {
		sub = L.NewTable()
		mem.RawSetString(key, sub)
	}
	return sub
}

func (vm *BotVM) getOrCreateCreepMemory(name string) *lua.LTable {
	creepsMem := vm.ensureMemorySubtable("creeps")
	cm, ok := creepsMem.RawGetString(name).(*lua.LTable)
	if !ok {
		cm = vm.L.NewTable()
		creepsMem.RawSetString(name, cm)
	}
	return cm
}

func (vm *BotVM) getOrCreateRoomMemory(roomName string) *lua.LTable {
	roomsMem := vm.ensureMemorySubtable("rooms")
	rm, ok := roomsMem.RawGetString(roomName).(*lua.LTable)
	if !ok {
		rm = vm.L.NewTable()
		roomsMem.RawSetString(roomName, rm)
	}
	return rm
}

// --- Object wrapping ---

func (vm *BotVM) newRoomPosition(x, y int, roomName string) *lua.LTable {
	L := vm.L
	t := L.NewTable()
	t.RawSetString("x", lua.LNumber(x))
	t.RawSetString("y", lua.LNumber(y))
	t.RawSetString("roomName", lua.LString(roomName))
	L.SetMetatable(t, vm.roomPosMT)
	return t
}

func (vm *BotVM) newRoomVisual(roomName string) *lua.LTable {
	L := vm.L
	t := L.NewTable()
	t.RawSetString("roomName", lua.LString(roomName))
	L.SetMetatable(t, vm.visualMT)
	return t
}

func (vm *BotVM) newRoomTerrain(roomName string) lua.LValue {
	L := vm.L
	var grid *[engine.RoomSize * engine.RoomSize]uint8
	if vm.world != nil {
		vm.world.RLock()
		if room := vm.world.Rooms[roomName]; room != nil {
			cp := room.Terrain
			grid = &cp
		}
		vm.world.RUnlock()
	}
	ud := L.NewUserData()
	ud.Value = grid
	L.SetMetatable(ud, vm.terrainMT)
	return ud
}

func (vm *BotVM) newStore(e *engine.Entity) *lua.LTable {
	L := vm.L
	t := L.NewTable()
	if e.Capacity(engine.RESOURCE_ENERGY) > 0 {
		t.RawSetString(engine.RESOURCE_ENERGY, lua.LNumber(e.UsedCapacity(engine.RESOURCE_ENERGY)))
	}
	for res, amt := range e.Store {
		t.RawSetString(res, lua.LNumber(amt))
	}
	// Attach entity ID in a private closure? Better: use a small metatable wrapper?
	// We use a per-store metatable? No, storeMT is shared; store entity ID on a closure?
	// Even simpler: store the entity ID in a weak-free Go map or inside a per-store metatable?
	// Wait: keeping `storeMT` shared with a `storeIDs` map on vm is O(1) and keeps pairs(store) clean!
	// Actually, we can store `_entityId` in a Go map keyed by *lua.LTable, or just set a metatable
	// whose __index looks up vm.storeMT methods. Let's check: vm.objCache is cleared each tick,
	// so let's add a storeRef or use closure-free lookup. We already have `storeMT`; let's keep a
	// map? Wait: let's check if we declared storeEntity on BotVM. We didn't yet, OR we can just
	// use a metatable per store? No, a metatable with __index pointing to a 3-function table is tiny,
	// OR even better: we can store the entity ID on a `mt`? Wait - creating a closure per store is
	// heavier than a Go map, so let's check: can we store `__entityId` in `mt`? Yes!
	// If `mt := L.NewTable(); mt.RawSetString("__index", storeMethods); mt.RawSetString("__id", lua.LString(e.ID))`,
	// then `pairs(store)` ONLY sees resource keys, `L.GetMetatable(self).RawGetString("__id")` returns the ID,
	// and we don't even need a map! That's idiomatic Lua 5.1.
	mt := L.NewTable()
	mt.RawSetString("__index", vm.storeMT.RawGetString("__index"))
	mt.RawSetString("__id", lua.LString(e.ID))
	L.SetMetatable(t, mt)
	return t
}

func (vm *BotVM) wrapRoomByName(roomName string) *lua.LTable {
	roomName = strings.ToUpper(strings.TrimSpace(roomName))
	if cached, ok := vm.roomCache[roomName]; ok {
		return cached
	}
	if vm.world == nil {
		return nil
	}

	vm.world.RLock()
	room := vm.world.Rooms[roomName]
	if room == nil {
		vm.world.RUnlock()
		return nil
	}
	energyAvail := room.EnergyAvailable(vm.UserID)
	energyCap := room.EnergyCapacityAvailable(vm.UserID)
	var ctrlID string
	if ctrl := room.FindController(); ctrl != nil {
		ctrlID = ctrl.ID
	}
	vm.world.RUnlock()

	L := vm.L
	t := L.NewTable()
	// Cache immediately to break room <-> controller reference cycles.
	vm.roomCache[roomName] = t

	t.RawSetString("name", lua.LString(roomName))
	t.RawSetString("energyAvailable", lua.LNumber(energyAvail))
	t.RawSetString("energyCapacityAvailable", lua.LNumber(energyCap))
	t.RawSetString("visual", vm.newRoomVisual(roomName))
	t.RawSetString("memory", vm.getOrCreateRoomMemory(roomName))
	if ctrlID != "" {
		if ctrlTbl := vm.wrapEntityByID(ctrlID); ctrlTbl != nil {
			t.RawSetString("controller", ctrlTbl)
		}
	}
	L.SetMetatable(t, vm.roomMT)
	return t
}

func (vm *BotVM) wrapEntityByID(id string) *lua.LTable {
	if cached, ok := vm.objCache[id]; ok {
		return cached
	}
	if vm.world == nil {
		return nil
	}

	vm.world.RLock()
	var e *engine.Entity
	for _, r := range vm.world.Rooms {
		if found, ok := r.Entities[id]; ok {
			e = found
			break
		}
	}
	if e == nil {
		vm.world.RUnlock()
		return nil
	}
	gameTime := vm.world.GameTime
	ownerName := vm.world.Users[e.User]
	// Copy needed fields while holding RLock.
	typ := e.Type
	roomName := e.Room
	ex, ey := e.X, e.Y
	user := e.User
	name := e.Name
	hits, hitsMax := e.Hits, e.HitsMax
	fatigue := e.Fatigue
	ageTime := e.AgeTime
	spawning := e.Spawning
	var spInfo *engine.SpawningInfo
	if e.SpawningInfo != nil {
		cp := *e.SpawningInfo
		spInfo = &cp
	}
	var bodyCopy []engine.BodyPart
	if len(e.Body) > 0 {
		bodyCopy = make([]engine.BodyPart, len(e.Body))
		copy(bodyCopy, e.Body)
	}
	energy, energyCap := e.Energy, e.EnergyCapacity
	nextRegen := e.NextRegenerationTime
	mineralType, mineralAmt := e.MineralType, e.MineralAmount
	level, progress, progressTotal := e.Level, e.Progress, e.ProgressTotal
	downgradeTime := e.DowngradeTime
	siteStructType := e.StructureType
	resType, amount := e.ResourceType, e.Amount
	creepID, deathTime, decayTime := e.CreepID, e.DeathTime, e.DecayTime
	nextDecay, cooldown := e.NextDecayTime, e.Cooldown
	safeMode, safeModeAvail, safeModeCD := e.SafeMode, e.SafeModeAvailable, e.SafeModeCooldown
	isStruct := e.IsStructure()
	hasStore := e.StoreCapacity > 0 || len(e.StoreCapacityResource) > 0 || len(e.Store) > 0 ||
		typ == engine.TYPE_CREEP || typ == engine.STRUCTURE_SPAWN || typ == engine.STRUCTURE_EXTENSION ||
		typ == engine.STRUCTURE_TOWER || typ == engine.STRUCTURE_LINK || typ == engine.STRUCTURE_CONTAINER ||
		typ == engine.STRUCTURE_STORAGE || typ == engine.TYPE_TOMBSTONE
	var storeTbl *lua.LTable
	if hasStore {
		storeTbl = vm.newStore(e)
	}
	vm.world.RUnlock()

	L := vm.L
	t := L.NewTable()
	vm.objCache[id] = t

	t.RawSetString("id", lua.LString(id))
	t.RawSetString("pos", vm.newRoomPosition(ex, ey, roomName))
	if rTbl := vm.wrapRoomByName(roomName); rTbl != nil {
		t.RawSetString("room", rTbl)
	}
	if name != "" {
		t.RawSetString("name", lua.LString(name))
	}
	if hitsMax > 0 {
		t.RawSetString("hits", lua.LNumber(hits))
		t.RawSetString("hitsMax", lua.LNumber(hitsMax))
	}
	if user != "" {
		t.RawSetString("my", lua.LBool(user == vm.UserID))
		if ownerName != "" {
			owner := L.NewTable()
			owner.RawSetString("username", lua.LString(ownerName))
			t.RawSetString("owner", owner)
		}
	} else {
		t.RawSetString("my", lua.LFalse)
	}
	if storeTbl != nil {
		t.RawSetString("store", storeTbl)
	}
	if isStruct {
		t.RawSetString("structureType", lua.LString(typ))
		if nextDecay > 0 {
			rem := nextDecay - gameTime
			if rem < 0 {
				rem = 0
			}
			t.RawSetString("ticksToDecay", lua.LNumber(rem))
		}
	}

	switch typ {
	case engine.TYPE_CREEP:
		t.RawSetString("fatigue", lua.LNumber(fatigue))
		t.RawSetString("spawning", lua.LBool(spawning))
		if ageTime > 0 {
			ttl := ageTime - gameTime
			if ttl < 0 {
				ttl = 0
			}
			t.RawSetString("ticksToLive", lua.LNumber(ttl))
		}
		bodyTbl := L.NewTable()
		for i, bp := range bodyCopy {
			bt := L.NewTable()
			bt.RawSetString("type", lua.LString(bp.Type))
			bt.RawSetString("hits", lua.LNumber(bp.Hits))
			if bp.Boost != "" {
				bt.RawSetString("boost", lua.LString(bp.Boost))
			}
			bodyTbl.RawSetInt(i+1, bt)
		}
		t.RawSetString("body", bodyTbl)
		if user == vm.UserID && name != "" {
			t.RawSetString("memory", vm.getOrCreateCreepMemory(name))
		}
		L.SetMetatable(t, vm.creepMT)

	case engine.STRUCTURE_SPAWN:
		if spInfo != nil {
			spTbl := L.NewTable()
			spTbl.RawSetString("name", lua.LString(spInfo.Name))
			spTbl.RawSetString("needTime", lua.LNumber(spInfo.NeedTime))
			rem := spInfo.SpawnTime - gameTime
			if rem < 0 {
				rem = 0
			}
			spTbl.RawSetString("remainingTime", lua.LNumber(rem))
			t.RawSetString("spawning", spTbl)
		} else {
			t.RawSetString("spawning", lua.LNil)
		}
		L.SetMetatable(t, vm.spawnMT)

	case engine.STRUCTURE_TOWER:
		L.SetMetatable(t, vm.towerMT)

	case engine.STRUCTURE_LINK:
		t.RawSetString("cooldown", lua.LNumber(cooldown))
		L.SetMetatable(t, vm.linkMT)

	case engine.STRUCTURE_CONTROLLER:
		t.RawSetString("level", lua.LNumber(level))
		t.RawSetString("progress", lua.LNumber(progress))
		t.RawSetString("progressTotal", lua.LNumber(progressTotal))
		t.RawSetString("safeModeAvailable", lua.LNumber(safeModeAvail))
		if safeMode > gameTime {
			t.RawSetString("safeMode", lua.LNumber(safeMode-gameTime))
		}
		if safeModeCD > gameTime {
			t.RawSetString("safeModeCooldown", lua.LNumber(safeModeCD-gameTime))
		}
		if downgradeTime > 0 {
			rem := downgradeTime - gameTime
			if rem < 0 {
				rem = 0
			}
			t.RawSetString("ticksToDowngrade", lua.LNumber(rem))
		}
		ctrlID := id
		t.RawSetString("activateSafeMode", L.NewFunction(func(L *lua.LState) int {
			if vm.world == nil {
				L.Push(lua.LNumber(engine.ERR_INVALID_TARGET))
				return 1
			}
			code := vm.world.QueueIntent(vm.UserID, engine.Intent{
				Type:    engine.IntentActivateSafeMode,
				ActorID: ctrlID,
			})
			L.Push(lua.LNumber(code))
			return 1
		}))

	case engine.TYPE_SOURCE:
		t.RawSetString("energy", lua.LNumber(energy))
		t.RawSetString("energyCapacity", lua.LNumber(energyCap))
		if nextRegen > 0 {
			rem := nextRegen - gameTime
			if rem < 0 {
				rem = 0
			}
			t.RawSetString("ticksToRegeneration", lua.LNumber(rem))
		}

	case engine.TYPE_MINERAL:
		t.RawSetString("mineralType", lua.LString(mineralType))
		t.RawSetString("mineralAmount", lua.LNumber(mineralAmt))

	case engine.TYPE_CONSTRUCTION_SITE:
		t.RawSetString("structureType", lua.LString(siteStructType))
		t.RawSetString("progress", lua.LNumber(progress))
		t.RawSetString("progressTotal", lua.LNumber(progressTotal))

	case engine.TYPE_RESOURCE:
		t.RawSetString("resourceType", lua.LString(resType))
		t.RawSetString("amount", lua.LNumber(amount))

	case engine.TYPE_TOMBSTONE:
		t.RawSetString("creepId", lua.LString(creepID))
		t.RawSetString("deathTime", lua.LNumber(deathTime))
		if decayTime > 0 {
			rem := decayTime - gameTime
			if rem < 0 {
				rem = 0
			}
			t.RawSetString("ticksToDecay", lua.LNumber(rem))
		}
	}

	return t
}

// --- Coordinate extraction helpers ---

func extractPos(v lua.LValue) (x, y int, roomName string, ok bool) {
	tbl, isTbl := v.(*lua.LTable)
	if !isTbl {
		return 0, 0, "", false
	}
	if posVal := tbl.RawGetString("pos"); posVal != lua.LNil {
		if posTbl, ok2 := posVal.(*lua.LTable); ok2 {
			tbl = posTbl
		}
	}
	xv, okX := tbl.RawGetString("x").(lua.LNumber)
	yv, okY := tbl.RawGetString("y").(lua.LNumber)
	if !okX || !okY {
		return 0, 0, "", false
	}
	rn, _ := tbl.RawGetString("roomName").(lua.LString)
	return int(xv), int(yv), string(rn), true
}

func extractTargetID(v lua.LValue) (string, bool) {
	tbl, ok := v.(*lua.LTable)
	if !ok {
		return "", false
	}
	id, ok := tbl.RawGetString("id").(lua.LString)
	if !ok || id == "" {
		return "", false
	}
	return string(id), true
}

// --- RoomPosition methods ---

func (vm *BotVM) posGetRangeTo(L *lua.LState) int {
	x1, y1, r1, ok1 := extractPos(L.Get(1))
	if !ok1 {
		L.Push(lua.LNumber(math.Inf(1)))
		return 1
	}
	var x2, y2 int
	var r2 string
	if L.Get(2).Type() == lua.LTNumber {
		x2 = L.CheckInt(2)
		y2 = L.CheckInt(3)
		r2 = r1
	} else {
		var ok2 bool
		x2, y2, r2, ok2 = extractPos(L.Get(2))
		if !ok2 {
			L.Push(lua.LNumber(math.Inf(1)))
			return 1
		}
	}
	if r1 != "" && r2 != "" && r1 != r2 {
		L.Push(lua.LNumber(math.Inf(1)))
		return 1
	}
	L.Push(lua.LNumber(engine.ChebyshevDist(x1, y1, x2, y2)))
	return 1
}

func (vm *BotVM) posIsNearTo(L *lua.LState) int {
	x1, y1, r1, ok1 := extractPos(L.Get(1))
	if !ok1 {
		L.Push(lua.LFalse)
		return 1
	}
	var x2, y2 int
	var r2 string
	if L.Get(2).Type() == lua.LTNumber {
		x2 = L.CheckInt(2)
		y2 = L.CheckInt(3)
		r2 = r1
	} else {
		var ok2 bool
		x2, y2, r2, ok2 = extractPos(L.Get(2))
		if !ok2 {
			L.Push(lua.LFalse)
			return 1
		}
	}
	if r1 != "" && r2 != "" && r1 != r2 {
		L.Push(lua.LFalse)
		return 1
	}
	L.Push(lua.LBool(engine.ChebyshevDist(x1, y1, x2, y2) <= 1))
	return 1
}

func (vm *BotVM) posInRangeTo(L *lua.LState) int {
	x1, y1, r1, ok1 := extractPos(L.Get(1))
	if !ok1 {
		L.Push(lua.LFalse)
		return 1
	}
	var x2, y2, rng int
	var r2 string
	if L.Get(2).Type() == lua.LTNumber {
		x2 = L.CheckInt(2)
		y2 = L.CheckInt(3)
		rng = L.CheckInt(4)
		r2 = r1
	} else {
		var ok2 bool
		x2, y2, r2, ok2 = extractPos(L.Get(2))
		if !ok2 {
			L.Push(lua.LFalse)
			return 1
		}
		rng = L.CheckInt(3)
	}
	if r1 != "" && r2 != "" && r1 != r2 {
		L.Push(lua.LFalse)
		return 1
	}
	L.Push(lua.LBool(engine.ChebyshevDist(x1, y1, x2, y2) <= rng))
	return 1
}

func (vm *BotVM) posIsEqualTo(L *lua.LState) int {
	x1, y1, r1, ok1 := extractPos(L.Get(1))
	if !ok1 {
		L.Push(lua.LFalse)
		return 1
	}
	var x2, y2 int
	var r2 string
	if L.Get(2).Type() == lua.LTNumber {
		x2 = L.CheckInt(2)
		y2 = L.CheckInt(3)
		r2 = r1
	} else {
		var ok2 bool
		x2, y2, r2, ok2 = extractPos(L.Get(2))
		if !ok2 {
			L.Push(lua.LFalse)
			return 1
		}
	}
	if r1 != "" && r2 != "" && r1 != r2 {
		L.Push(lua.LFalse)
		return 1
	}
	L.Push(lua.LBool(x1 == x2 && y1 == y2))
	return 1
}

func (vm *BotVM) posFindClosestByRange(L *lua.LState) int {
	x1, y1, r1, ok1 := extractPos(L.Get(1))
	if !ok1 {
		L.Push(lua.LNil)
		return 1
	}
	list, ok := L.Get(2).(*lua.LTable)
	if !ok {
		L.Push(lua.LNil)
		return 1
	}

	var best lua.LValue = lua.LNil
	bestDist := 1 << 30
	n := list.Len()
	for i := 1; i <= n; i++ {
		item := list.RawGetInt(i)
		x2, y2, r2, ok2 := extractPos(item)
		if !ok2 {
			continue
		}
		if r1 != "" && r2 != "" && r1 != r2 {
			continue
		}
		d := engine.ChebyshevDist(x1, y1, x2, y2)
		if d < bestDist {
			bestDist = d
			best = item
		}
	}
	L.Push(best)
	return 1
}

// --- Store methods ---

func (vm *BotVM) storeEntityID(L *lua.LState) string {
	self := L.CheckTable(1)
	mt, ok := L.GetMetatable(self).(*lua.LTable)
	if !ok {
		return ""
	}
	id, _ := mt.RawGetString("__id").(lua.LString)
	return string(id)
}

func (vm *BotVM) storeGetUsedCapacity(L *lua.LState) int {
	id := vm.storeEntityID(L)
	res := L.OptString(2, "")
	if id == "" || vm.world == nil {
		L.Push(lua.LNumber(0))
		return 1
	}
	vm.world.RLock()
	var val int
	for _, r := range vm.world.Rooms {
		if e, ok := r.Entities[id]; ok {
			val = e.UsedCapacity(res)
			break
		}
	}
	vm.world.RUnlock()
	L.Push(lua.LNumber(val))
	return 1
}

func (vm *BotVM) storeGetFreeCapacity(L *lua.LState) int {
	id := vm.storeEntityID(L)
	res := L.OptString(2, "")
	if id == "" || vm.world == nil {
		L.Push(lua.LNumber(0))
		return 1
	}
	vm.world.RLock()
	var val int
	for _, r := range vm.world.Rooms {
		if e, ok := r.Entities[id]; ok {
			val = e.FreeCapacity(res)
			break
		}
	}
	vm.world.RUnlock()
	L.Push(lua.LNumber(val))
	return 1
}

func (vm *BotVM) storeGetCapacity(L *lua.LState) int {
	id := vm.storeEntityID(L)
	res := L.OptString(2, "")
	if id == "" || vm.world == nil {
		L.Push(lua.LNumber(0))
		return 1
	}
	vm.world.RLock()
	var val int
	for _, r := range vm.world.Rooms {
		if e, ok := r.Entities[id]; ok {
			val = e.Capacity(res)
			break
		}
	}
	vm.world.RUnlock()
	L.Push(lua.LNumber(val))
	return 1
}

// --- RoomTerrain methods ---

func (vm *BotVM) terrainGet(L *lua.LState) int {
	ud := L.CheckUserData(1)
	x := L.CheckInt(2)
	y := L.CheckInt(3)
	grid, _ := ud.Value.(*[engine.RoomSize * engine.RoomSize]uint8)
	if grid == nil || !engine.InBounds(x, y) {
		L.Push(lua.LNumber(engine.TERRAIN_MASK_WALL))
		return 1
	}
	L.Push(lua.LNumber(grid[y*engine.RoomSize+x]))
	return 1
}

// --- RoomVisual methods ---

func parseVisualStyle(styleVal lua.LValue) (radius float32, fill, col string, opac, sw float32) {
	opac = 0.5
	sw = 0.1
	tbl, ok := styleVal.(*lua.LTable)
	if !ok {
		return
	}
	if v, ok := tbl.RawGetString("radius").(lua.LNumber); ok {
		radius = float32(v)
	}
	if v, ok := tbl.RawGetString("fill").(lua.LString); ok {
		fill = string(v)
	}
	if v, ok := tbl.RawGetString("stroke").(lua.LString); ok {
		col = string(v)
	}
	if v, ok := tbl.RawGetString("color").(lua.LString); ok && col == "" {
		col = string(v)
	}
	if v, ok := tbl.RawGetString("opacity").(lua.LNumber); ok {
		opac = float32(v)
	}
	if v, ok := tbl.RawGetString("strokeWidth").(lua.LNumber); ok {
		sw = float32(v)
	} else if v, ok := tbl.RawGetString("width").(lua.LNumber); ok {
		sw = float32(v)
	}
	return
}

func (vm *BotVM) visualCircle(L *lua.LState) int {
	self := L.CheckTable(1)
	roomName, _ := self.RawGetString("roomName").(lua.LString)
	var x, y float32
	var styleVal lua.LValue
	if L.Get(2).Type() == lua.LTTable {
		px, py, _, _ := extractPos(L.Get(2))
		x, y = float32(px), float32(py)
		styleVal = L.Get(3)
	} else {
		x = float32(L.CheckNumber(2))
		y = float32(L.CheckNumber(3))
		styleVal = L.Get(4)
	}
	r, fill, col, opac, sw := parseVisualStyle(styleVal)
	if vm.world != nil {
		vm.world.AddVisual(string(roomName), screeps.VisualOp{
			Type: "c", X: x, Y: y, R: r, Fill: fill, Col: col, Opac: opac, SW: sw,
		})
	}
	L.Push(self)
	return 1
}

func (vm *BotVM) visualLine(L *lua.LState) int {
	self := L.CheckTable(1)
	roomName, _ := self.RawGetString("roomName").(lua.LString)
	var x1, y1, x2, y2 float32
	var styleVal lua.LValue
	if L.Get(2).Type() == lua.LTTable {
		px1, py1, _, _ := extractPos(L.Get(2))
		px2, py2, _, _ := extractPos(L.Get(3))
		x1, y1, x2, y2 = float32(px1), float32(py1), float32(px2), float32(py2)
		styleVal = L.Get(4)
	} else {
		x1 = float32(L.CheckNumber(2))
		y1 = float32(L.CheckNumber(3))
		x2 = float32(L.CheckNumber(4))
		y2 = float32(L.CheckNumber(5))
		styleVal = L.Get(6)
	}
	_, _, col, opac, sw := parseVisualStyle(styleVal)
	if vm.world != nil {
		vm.world.AddVisual(string(roomName), screeps.VisualOp{
			Type: "l", X: x1, Y: y1, X2: x2, Y2: y2, Col: col, Opac: opac, SW: sw,
		})
	}
	L.Push(self)
	return 1
}

func (vm *BotVM) visualRect(L *lua.LState) int {
	self := L.CheckTable(1)
	roomName, _ := self.RawGetString("roomName").(lua.LString)
	var x, y, w, h float32
	var styleVal lua.LValue
	if L.Get(2).Type() == lua.LTTable {
		px, py, _, _ := extractPos(L.Get(2))
		x, y = float32(px), float32(py)
		w = float32(L.CheckNumber(3))
		h = float32(L.CheckNumber(4))
		styleVal = L.Get(5)
	} else {
		x = float32(L.CheckNumber(2))
		y = float32(L.CheckNumber(3))
		w = float32(L.CheckNumber(4))
		h = float32(L.CheckNumber(5))
		styleVal = L.Get(6)
	}
	_, fill, col, opac, sw := parseVisualStyle(styleVal)
	if vm.world != nil {
		vm.world.AddVisual(string(roomName), screeps.VisualOp{
			Type: "r", X: x, Y: y, W: w, H: h, Fill: fill, Col: col, Opac: opac, SW: sw,
		})
	}
	L.Push(self)
	return 1
}

func (vm *BotVM) visualPoly(L *lua.LState) int {
	self := L.CheckTable(1)
	roomName, _ := self.RawGetString("roomName").(lua.LString)
	ptsTbl := L.CheckTable(2)
	styleVal := L.Get(3)
	var pts [][2]float32
	n := ptsTbl.Len()
	for i := 1; i <= n; i++ {
		item := ptsTbl.RawGetInt(i)
		if pair, ok := item.(*lua.LTable); ok {
			if px, py, _, ok2 := extractPos(pair); ok2 {
				pts = append(pts, [2]float32{float32(px), float32(py)})
			} else if xv, okX := pair.RawGetInt(1).(lua.LNumber); okX {
				if yv, okY := pair.RawGetInt(2).(lua.LNumber); okY {
					pts = append(pts, [2]float32{float32(xv), float32(yv)})
				}
			}
		}
	}
	_, fill, col, opac, sw := parseVisualStyle(styleVal)
	if vm.world != nil {
		vm.world.AddVisual(string(roomName), screeps.VisualOp{
			Type: "p", Pts: pts, Fill: fill, Col: col, Opac: opac, SW: sw,
		})
	}
	L.Push(self)
	return 1
}

func (vm *BotVM) visualText(L *lua.LState) int {
	self := L.CheckTable(1)
	roomName, _ := self.RawGetString("roomName").(lua.LString)
	text := L.CheckString(2)
	var x, y float32
	var styleVal lua.LValue
	if L.Get(3).Type() == lua.LTTable {
		px, py, _, _ := extractPos(L.Get(3))
		x, y = float32(px), float32(py)
		styleVal = L.Get(4)
	} else {
		x = float32(L.CheckNumber(3))
		y = float32(L.CheckNumber(4))
		styleVal = L.Get(5)
	}
	_, _, col, opac, _ := parseVisualStyle(styleVal)
	if vm.world != nil {
		vm.world.AddVisual(string(roomName), screeps.VisualOp{
			Type: "t", Text: text, X: x, Y: y, Col: col, Opac: opac,
		})
	}
	L.Push(self)
	return 1
}

// --- Room methods ---

func (vm *BotVM) roomFind(L *lua.LState) int {
	self := L.CheckTable(1)
	findConst := L.CheckInt(2)
	optsVal := L.Get(3)
	roomName, _ := self.RawGetString("name").(lua.LString)

	resTbl := L.NewTable()
	if vm.world == nil {
		L.Push(resTbl)
		return 1
	}

	// Exit finds return RoomPosition tables.
	if findConst == engine.FIND_EXIT_TOP || findConst == engine.FIND_EXIT_RIGHT ||
		findConst == engine.FIND_EXIT_BOTTOM || findConst == engine.FIND_EXIT_LEFT ||
		findConst == engine.FIND_EXIT {
		vm.world.RLock()
		room := vm.world.Rooms[string(roomName)]
		var coords [][2]int
		if room != nil {
			addEdge := func(edge int) {
				switch edge {
				case engine.FIND_EXIT_TOP:
					for x := 0; x < engine.RoomSize; x++ {
						if room.TerrainAt(x, 0) != engine.TERRAIN_MASK_WALL {
							coords = append(coords, [2]int{x, 0})
						}
					}
				case engine.FIND_EXIT_RIGHT:
					for y := 0; y < engine.RoomSize; y++ {
						if room.TerrainAt(engine.RoomSize-1, y) != engine.TERRAIN_MASK_WALL {
							coords = append(coords, [2]int{engine.RoomSize - 1, y})
						}
					}
				case engine.FIND_EXIT_BOTTOM:
					for x := 0; x < engine.RoomSize; x++ {
						if room.TerrainAt(x, engine.RoomSize-1) != engine.TERRAIN_MASK_WALL {
							coords = append(coords, [2]int{x, engine.RoomSize - 1})
						}
					}
				case engine.FIND_EXIT_LEFT:
					for y := 0; y < engine.RoomSize; y++ {
						if room.TerrainAt(0, y) != engine.TERRAIN_MASK_WALL {
							coords = append(coords, [2]int{0, y})
						}
					}
				}
			}
			if findConst == engine.FIND_EXIT {
				addEdge(engine.FIND_EXIT_TOP)
				addEdge(engine.FIND_EXIT_RIGHT)
				addEdge(engine.FIND_EXIT_BOTTOM)
				addEdge(engine.FIND_EXIT_LEFT)
			} else {
				addEdge(findConst)
			}
		}
		vm.world.RUnlock()
		for i, c := range coords {
			resTbl.RawSetInt(i+1, vm.newRoomPosition(c[0], c[1], string(roomName)))
		}
		L.Push(resTbl)
		return 1
	}

	vm.world.RLock()
	room := vm.world.Rooms[string(roomName)]
	var ids []string
	if room != nil {
		for _, e := range room.Entities {
			match := false
			switch findConst {
			case engine.FIND_CREEPS:
				match = e.Type == engine.TYPE_CREEP
			case engine.FIND_MY_CREEPS:
				match = e.Type == engine.TYPE_CREEP && e.User == vm.UserID
			case engine.FIND_HOSTILE_CREEPS:
				match = e.Type == engine.TYPE_CREEP && e.User != vm.UserID
			case engine.FIND_SOURCES:
				match = e.Type == engine.TYPE_SOURCE
			case engine.FIND_SOURCES_ACTIVE:
				match = e.Type == engine.TYPE_SOURCE && e.Energy > 0
			case engine.FIND_DROPPED_RESOURCES:
				match = e.Type == engine.TYPE_RESOURCE
			case engine.FIND_STRUCTURES:
				match = e.IsStructure()
			case engine.FIND_MY_STRUCTURES:
				match = e.IsStructure() && e.User == vm.UserID
			case engine.FIND_HOSTILE_STRUCTURES:
				match = e.IsStructure() && e.User != "" && e.User != vm.UserID
			case engine.FIND_CONSTRUCTION_SITES:
				match = e.Type == engine.TYPE_CONSTRUCTION_SITE
			case engine.FIND_MY_CONSTRUCTION_SITES:
				match = e.Type == engine.TYPE_CONSTRUCTION_SITE && e.User == vm.UserID
			case engine.FIND_HOSTILE_CONSTRUCTION_SITES:
				match = e.Type == engine.TYPE_CONSTRUCTION_SITE && e.User != vm.UserID
			case engine.FIND_MY_SPAWNS:
				match = e.Type == engine.STRUCTURE_SPAWN && e.User == vm.UserID
			case engine.FIND_HOSTILE_SPAWNS:
				match = e.Type == engine.STRUCTURE_SPAWN && e.User != vm.UserID
			case engine.FIND_MINERALS:
				match = e.Type == engine.TYPE_MINERAL
			case engine.FIND_TOMBSTONES:
				match = e.Type == engine.TYPE_TOMBSTONE
			}
			if match {
				ids = append(ids, e.ID)
			}
		}
	}
	vm.world.RUnlock()

	sort.Strings(ids)

	var filterFn *lua.LFunction
	if optsTbl, ok := optsVal.(*lua.LTable); ok {
		if f, ok := optsTbl.RawGetString("filter").(*lua.LFunction); ok {
			filterFn = f
		}
	}

	idx := 1
	for _, id := range ids {
		obj := vm.wrapEntityByID(id)
		if obj == nil {
			continue
		}
		if filterFn != nil {
			if err := L.CallByParam(lua.P{Fn: filterFn, NRet: 1, Protect: true}, obj); err != nil {
				continue
			}
			keep := L.Get(-1)
			L.Pop(1)
			if !lua.LVAsBool(keep) {
				continue
			}
		}
		resTbl.RawSetInt(idx, obj)
		idx++
	}

	L.Push(resTbl)
	return 1
}

func (vm *BotVM) roomCreateConstructionSite(L *lua.LState) int {
	self := L.CheckTable(1)
	roomName, _ := self.RawGetString("name").(lua.LString)
	if vm.world == nil {
		L.Push(lua.LNumber(engine.ERR_INVALID_ARGS))
		return 1
	}
	var x, y int
	var structType string
	if L.Get(2).Type() == lua.LTTable {
		var ok bool
		x, y, _, ok = extractPos(L.Get(2))
		if !ok {
			L.Push(lua.LNumber(engine.ERR_INVALID_ARGS))
			return 1
		}
		structType = L.CheckString(3)
	} else {
		x = L.CheckInt(2)
		y = L.CheckInt(3)
		structType = L.CheckString(4)
	}
	_, code := vm.world.CreateConstructionSite(vm.UserID, string(roomName), x, y, structType)
	L.Push(lua.LNumber(code))
	return 1
}

func (vm *BotVM) roomGetTerrain(L *lua.LState) int {
	self := L.CheckTable(1)
	roomName, _ := self.RawGetString("name").(lua.LString)
	L.Push(vm.newRoomTerrain(string(roomName)))
	return 1
}

func (vm *BotVM) roomFindExitTo(L *lua.LState) int {
	self := L.CheckTable(1)
	fromName, _ := self.RawGetString("name").(lua.LString)
	target := strings.ToUpper(strings.TrimSpace(L.OptString(2, "")))
	if target == "" || vm.world == nil {
		L.Push(lua.LNumber(engine.ERR_INVALID_ARGS))
		return 1
	}
	fx, fy, ok1 := engine.ParseRoomCoords(string(fromName))
	tx, ty, ok2 := engine.ParseRoomCoords(target)
	if !ok1 || !ok2 {
		L.Push(lua.LNumber(engine.ERR_INVALID_ARGS))
		return 1
	}
	dx, dy := tx-fx, ty-fy
	var exitConst int
	switch {
	case dx == 0 && dy == -1:
		exitConst = engine.FIND_EXIT_TOP
	case dx == 1 && dy == 0:
		exitConst = engine.FIND_EXIT_RIGHT
	case dx == 0 && dy == 1:
		exitConst = engine.FIND_EXIT_BOTTOM
	case dx == -1 && dy == 0:
		exitConst = engine.FIND_EXIT_LEFT
	default:
		L.Push(lua.LNumber(engine.ERR_NO_PATH))
		return 1
	}
	vm.world.RLock()
	r := vm.world.Rooms[string(fromName)]
	hasExit := r != nil && r.RoomHasExitEdge(exitConst) && vm.world.Rooms[target] != nil
	vm.world.RUnlock()
	if !hasExit {
		L.Push(lua.LNumber(engine.ERR_NO_PATH))
		return 1
	}
	L.Push(lua.LNumber(exitConst))
	return 1
}

func (vm *BotVM) roomLookForAt(L *lua.LState) int {
	self := L.CheckTable(1)
	roomName, _ := self.RawGetString("name").(lua.LString)
	lookType := L.CheckString(2)
	var x, y int
	if L.Get(3).Type() == lua.LTTable {
		var ok bool
		x, y, _, ok = extractPos(L.Get(3))
		if !ok {
			L.Push(L.NewTable())
			return 1
		}
	} else {
		x = L.CheckInt(3)
		y = L.CheckInt(4)
	}

	resTbl := L.NewTable()
	if vm.world == nil {
		L.Push(resTbl)
		return 1
	}

	if lookType == "terrain" {
		vm.world.RLock()
		room := vm.world.Rooms[string(roomName)]
		tStr := "wall"
		if room != nil {
			switch room.TerrainAt(x, y) {
			case engine.TERRAIN_MASK_PLAIN:
				tStr = "plain"
			case engine.TERRAIN_MASK_SWAMP:
				tStr = "swamp"
			default:
				tStr = "wall"
			}
		}
		vm.world.RUnlock()
		resTbl.RawSetInt(1, lua.LString(tStr))
		L.Push(resTbl)
		return 1
	}

	vm.world.RLock()
	room := vm.world.Rooms[string(roomName)]
	var ids []string
	if room != nil {
		for _, e := range room.EntitiesAt(x, y) {
			match := false
			switch lookType {
			case "creep":
				match = e.Type == engine.TYPE_CREEP
			case "structure":
				match = e.IsStructure()
			case "source":
				match = e.Type == engine.TYPE_SOURCE
			case "mineral":
				match = e.Type == engine.TYPE_MINERAL
			case "constructionSite":
				match = e.Type == engine.TYPE_CONSTRUCTION_SITE
			case "resource", "energy":
				match = e.Type == engine.TYPE_RESOURCE
			case "tombstone":
				match = e.Type == engine.TYPE_TOMBSTONE
			}
			if match {
				ids = append(ids, e.ID)
			}
		}
	}
	vm.world.RUnlock()

	for i, id := range ids {
		if obj := vm.wrapEntityByID(id); obj != nil {
			resTbl.RawSetInt(i+1, obj)
		}
	}
	L.Push(resTbl)
	return 1
}

// --- Creep methods ---

func (vm *BotVM) creepMove(L *lua.LState) int {
	id, ok := extractTargetID(L.Get(1))
	if !ok || vm.world == nil {
		L.Push(lua.LNumber(engine.ERR_INVALID_TARGET))
		return 1
	}
	dir := L.CheckInt(2)
	code := vm.world.QueueIntent(vm.UserID, engine.Intent{
		Type:      engine.IntentMove,
		ActorID:   id,
		Direction: dir,
	})
	L.Push(lua.LNumber(code))
	return 1
}

func (vm *BotVM) creepMoveTo(L *lua.LState) int {
	id, ok := extractTargetID(L.Get(1))
	if !ok || vm.world == nil {
		L.Push(lua.LNumber(engine.ERR_INVALID_TARGET))
		return 1
	}

	var tx, ty int
	var optsVal lua.LValue
	if L.Get(2).Type() == lua.LTNumber {
		tx = L.CheckInt(2)
		ty = L.CheckInt(3)
		optsVal = L.Get(4)
	} else {
		var okPos bool
		tx, ty, _, okPos = extractPos(L.Get(2))
		if !okPos {
			L.Push(lua.LNumber(engine.ERR_INVALID_TARGET))
			return 1
		}
		optsVal = L.Get(3)
	}

	pathOpts := engine.PathOpts{
		MoverUser: vm.UserID,
	}
	hasRange := false
	customRange := 0
	if optsTbl, ok := optsVal.(*lua.LTable); ok {
		if v := optsTbl.RawGetString("ignoreCreeps"); v != lua.LNil {
			pathOpts.IgnoreCreeps = lua.LVAsBool(v)
		}
		if v := optsTbl.RawGetString("ignoreRoads"); v != lua.LNil {
			pathOpts.IgnoreRoads = lua.LVAsBool(v)
		}
		if v, ok := optsTbl.RawGetString("maxOps").(lua.LNumber); ok {
			pathOpts.MaxOps = int(v)
		}
		if v, ok := optsTbl.RawGetString("plainCost").(lua.LNumber); ok {
			pathOpts.PlainCost = uint8(v)
		}
		if v, ok := optsTbl.RawGetString("swampCost").(lua.LNumber); ok {
			pathOpts.SwampCost = uint8(v)
		}
		if v, ok := optsTbl.RawGetString("range").(lua.LNumber); ok {
			hasRange = true
			customRange = int(v)
		}
	}

	vm.world.RLock()
	var actor *engine.Entity
	var room *engine.Room
	for _, r := range vm.world.Rooms {
		if e, found := r.Entities[id]; found {
			actor = e
			room = r
			break
		}
	}
	if actor == nil || room == nil {
		vm.world.RUnlock()
		L.Push(lua.LNumber(engine.ERR_INVALID_TARGET))
		return 1
	}
	if actor.User != vm.UserID {
		vm.world.RUnlock()
		L.Push(lua.LNumber(engine.ERR_NOT_OWNER))
		return 1
	}
	if actor.Spawning {
		vm.world.RUnlock()
		L.Push(lua.LNumber(engine.ERR_BUSY))
		return 1
	}
	if actor.CountActiveParts(engine.MOVE) == 0 {
		vm.world.RUnlock()
		L.Push(lua.LNumber(engine.ERR_NO_BODYPART))
		return 1
	}
	if actor.Fatigue > 0 {
		vm.world.RUnlock()
		L.Push(lua.LNumber(engine.ERR_TIRED))
		return 1
	}
	if actor.X == tx && actor.Y == ty {
		vm.world.RUnlock()
		L.Push(lua.LNumber(engine.OK))
		return 1
	}

	rng := 0
	if hasRange {
		rng = customRange
	} else if !room.IsWalkableFor(tx, ty, vm.UserID, true) {
		rng = 1
	}
	if engine.ChebyshevDist(actor.X, actor.Y, tx, ty) <= rng {
		vm.world.RUnlock()
		L.Push(lua.LNumber(engine.OK))
		return 1
	}

	res := engine.SearchPath(room, actor.X, actor.Y, []engine.Goal{{X: tx, Y: ty, Range: rng}}, pathOpts)
	if len(res.Path) == 0 && !pathOpts.IgnoreCreeps {
		fallbackOpts := pathOpts
		fallbackOpts.IgnoreCreeps = true
		res = engine.SearchPath(room, actor.X, actor.Y, []engine.Goal{{X: tx, Y: ty, Range: rng}}, fallbackOpts)
	}
	var dir int
	if len(res.Path) > 0 {
		dir = res.Path[0].Direction
	}
	vm.world.RUnlock()

	if dir == 0 {
		L.Push(lua.LNumber(engine.ERR_NO_PATH))
		return 1
	}

	code := vm.world.QueueIntent(vm.UserID, engine.Intent{
		Type:      engine.IntentMove,
		ActorID:   id,
		Direction: dir,
	})
	L.Push(lua.LNumber(code))
	return 1
}

func (vm *BotVM) creepTargetIntent(intentType engine.IntentType) lua.LGFunction {
	return func(L *lua.LState) int {
		actorID, ok := extractTargetID(L.Get(1))
		if !ok || vm.world == nil {
			L.Push(lua.LNumber(engine.ERR_INVALID_TARGET))
			return 1
		}
		targetID, ok := extractTargetID(L.Get(2))
		if !ok {
			L.Push(lua.LNumber(engine.ERR_INVALID_TARGET))
			return 1
		}
		code := vm.world.QueueIntent(vm.UserID, engine.Intent{
			Type:     intentType,
			ActorID:  actorID,
			TargetID: targetID,
		})
		L.Push(lua.LNumber(code))
		return 1
	}
}

func (vm *BotVM) creepTransfer(L *lua.LState) int {
	actorID, ok := extractTargetID(L.Get(1))
	if !ok || vm.world == nil {
		L.Push(lua.LNumber(engine.ERR_INVALID_TARGET))
		return 1
	}
	targetID, ok := extractTargetID(L.Get(2))
	if !ok {
		L.Push(lua.LNumber(engine.ERR_INVALID_TARGET))
		return 1
	}
	resType := L.OptString(3, engine.RESOURCE_ENERGY)
	amount := L.OptInt(4, 0)
	code := vm.world.QueueIntent(vm.UserID, engine.Intent{
		Type:         engine.IntentTransfer,
		ActorID:      actorID,
		TargetID:     targetID,
		ResourceType: resType,
		Amount:       amount,
	})
	L.Push(lua.LNumber(code))
	return 1
}

func (vm *BotVM) creepWithdraw(L *lua.LState) int {
	actorID, ok := extractTargetID(L.Get(1))
	if !ok || vm.world == nil {
		L.Push(lua.LNumber(engine.ERR_INVALID_TARGET))
		return 1
	}
	targetID, ok := extractTargetID(L.Get(2))
	if !ok {
		L.Push(lua.LNumber(engine.ERR_INVALID_TARGET))
		return 1
	}
	resType := L.OptString(3, engine.RESOURCE_ENERGY)
	amount := L.OptInt(4, 0)
	code := vm.world.QueueIntent(vm.UserID, engine.Intent{
		Type:         engine.IntentWithdraw,
		ActorID:      actorID,
		TargetID:     targetID,
		ResourceType: resType,
		Amount:       amount,
	})
	L.Push(lua.LNumber(code))
	return 1
}

func (vm *BotVM) creepDrop(L *lua.LState) int {
	actorID, ok := extractTargetID(L.Get(1))
	if !ok || vm.world == nil {
		L.Push(lua.LNumber(engine.ERR_INVALID_TARGET))
		return 1
	}
	resType := L.OptString(2, engine.RESOURCE_ENERGY)
	amount := L.OptInt(3, 0)
	code := vm.world.QueueIntent(vm.UserID, engine.Intent{
		Type:         engine.IntentDrop,
		ActorID:      actorID,
		ResourceType: resType,
		Amount:       amount,
	})
	L.Push(lua.LNumber(code))
	return 1
}

func (vm *BotVM) creepSay(L *lua.LState) int {
	actorID, ok := extractTargetID(L.Get(1))
	if !ok || vm.world == nil {
		L.Push(lua.LNumber(engine.ERR_INVALID_TARGET))
		return 1
	}
	msg := L.OptString(2, "")
	code := vm.world.QueueIntent(vm.UserID, engine.Intent{
		Type:    engine.IntentSay,
		ActorID: actorID,
		Message: msg,
	})
	L.Push(lua.LNumber(code))
	return 1
}

// --- StructureSpawn, StructureTower, StructureLink methods ---

func (vm *BotVM) spawnCreep(L *lua.LState) int {
	spawnID, ok := extractTargetID(L.Get(1))
	if !ok || vm.world == nil {
		L.Push(lua.LNumber(engine.ERR_INVALID_TARGET))
		return 1
	}
	bodyTbl, ok := L.Get(2).(*lua.LTable)
	if !ok {
		L.Push(lua.LNumber(engine.ERR_INVALID_ARGS))
		return 1
	}
	name := L.CheckString(3)
	optsVal := L.Get(4)

	n := bodyTbl.Len()
	body := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		if s, ok := bodyTbl.RawGetInt(i).(lua.LString); ok {
			body = append(body, string(s))
		}
	}

	var dirs []int
	var memTbl *lua.LTable
	if optsTbl, ok := optsVal.(*lua.LTable); ok {
		if m, ok := optsTbl.RawGetString("memory").(*lua.LTable); ok {
			memTbl = m
		}
		if dTbl, ok := optsTbl.RawGetString("directions").(*lua.LTable); ok {
			dn := dTbl.Len()
			for i := 1; i <= dn; i++ {
				if d, ok := dTbl.RawGetInt(i).(lua.LNumber); ok {
					dirs = append(dirs, int(d))
				}
			}
		}
	}

	code := vm.world.QueueIntent(vm.UserID, engine.Intent{
		Type:       engine.IntentSpawnCreep,
		ActorID:    spawnID,
		CreepName:  name,
		Body:       body,
		Directions: dirs,
	})

	if code == engine.OK && memTbl != nil {
		creepsMem := vm.ensureMemorySubtable("creeps")
		creepsMem.RawSetString(name, memTbl)
	}

	L.Push(lua.LNumber(code))
	return 1
}

func (vm *BotVM) structureTargetIntent(intentType engine.IntentType) lua.LGFunction {
	return func(L *lua.LState) int {
		actorID, ok := extractTargetID(L.Get(1))
		if !ok || vm.world == nil {
			L.Push(lua.LNumber(engine.ERR_INVALID_TARGET))
			return 1
		}
		targetID, ok := extractTargetID(L.Get(2))
		if !ok {
			L.Push(lua.LNumber(engine.ERR_INVALID_TARGET))
			return 1
		}
		code := vm.world.QueueIntent(vm.UserID, engine.Intent{
			Type:     intentType,
			ActorID:  actorID,
			TargetID: targetID,
		})
		L.Push(lua.LNumber(code))
		return 1
	}
}

func (vm *BotVM) linkTransferEnergy(L *lua.LState) int {
	actorID, ok := extractTargetID(L.Get(1))
	if !ok || vm.world == nil {
		L.Push(lua.LNumber(engine.ERR_INVALID_TARGET))
		return 1
	}
	targetID, ok := extractTargetID(L.Get(2))
	if !ok {
		L.Push(lua.LNumber(engine.ERR_INVALID_TARGET))
		return 1
	}
	amount := L.OptInt(3, 0)
	code := vm.world.QueueIntent(vm.UserID, engine.Intent{
		Type:     engine.IntentLinkTransfer,
		ActorID:  actorID,
		TargetID: targetID,
		Amount:   amount,
	})
	L.Push(lua.LNumber(code))
	return 1
}

// --- PathFinder & CostMatrix methods ---

func checkCostMatrix(L *lua.LState, idx int) *engine.CostMatrix {
	ud := L.CheckUserData(idx)
	cm, ok := ud.Value.(*engine.CostMatrix)
	if !ok {
		L.ArgError(idx, "expected PathFinder.CostMatrix")
		return nil
	}
	return cm
}

func (vm *BotVM) costMatrixSet(L *lua.LState) int {
	cm := checkCostMatrix(L, 1)
	x := L.CheckInt(2)
	y := L.CheckInt(3)
	val := L.CheckInt(4)
	if val < 0 {
		val = 0
	} else if val > 255 {
		val = 255
	}
	cm.Set(x, y, uint8(val))
	return 0
}

func (vm *BotVM) costMatrixGet(L *lua.LState) int {
	cm := checkCostMatrix(L, 1)
	x := L.CheckInt(2)
	y := L.CheckInt(3)
	L.Push(lua.LNumber(cm.Get(x, y)))
	return 1
}

func (vm *BotVM) costMatrixClone(L *lua.LState) int {
	cm := checkCostMatrix(L, 1)
	ud := L.NewUserData()
	ud.Value = cm.Clone()
	L.SetMetatable(ud, vm.costMatMT)
	L.Push(ud)
	return 1
}

func (vm *BotVM) pathFinderSearch(L *lua.LState) int {
	ox, oy, roomName, ok := extractPos(L.Get(1))
	if !ok || vm.world == nil {
		res := L.NewTable()
		res.RawSetString("path", L.NewTable())
		res.RawSetString("ops", lua.LNumber(0))
		res.RawSetString("cost", lua.LNumber(0))
		res.RawSetString("incomplete", lua.LTrue)
		L.Push(res)
		return 1
	}

	var goals []engine.Goal
	parseGoal := func(v lua.LValue) {
		tbl, ok := v.(*lua.LTable)
		if !ok {
			return
		}
		gx, gy, _, okPos := extractPos(tbl)
		if !okPos {
			return
		}
		rng := 0
		if rv, ok := tbl.RawGetString("range").(lua.LNumber); ok {
			rng = int(rv)
		}
		goals = append(goals, engine.Goal{X: gx, Y: gy, Range: rng})
	}

	goalArg := L.Get(2)
	if gTbl, ok := goalArg.(*lua.LTable); ok {
		if gTbl.Len() > 0 {
			for i := 1; i <= gTbl.Len(); i++ {
				parseGoal(gTbl.RawGetInt(i))
			}
		} else {
			parseGoal(gTbl)
		}
	}

	opts := engine.PathOpts{
		MoverUser: vm.UserID,
	}
	if optsTbl, ok := L.Get(3).(*lua.LTable); ok {
		if v, ok := optsTbl.RawGetString("plainCost").(lua.LNumber); ok {
			opts.PlainCost = uint8(v)
		}
		if v, ok := optsTbl.RawGetString("swampCost").(lua.LNumber); ok {
			opts.SwampCost = uint8(v)
		}
		if v, ok := optsTbl.RawGetString("maxOps").(lua.LNumber); ok {
			opts.MaxOps = int(v)
		}
		if v := optsTbl.RawGetString("flee"); v != lua.LNil {
			opts.Flee = lua.LVAsBool(v)
		}
		if cb, ok := optsTbl.RawGetString("roomCallback").(*lua.LFunction); ok {
			if err := L.CallByParam(lua.P{Fn: cb, NRet: 1, Protect: true}, lua.LString(roomName)); err == nil {
				ret := L.Get(-1)
				L.Pop(1)
				if ud, ok := ret.(*lua.LUserData); ok {
					if cm, ok := ud.Value.(*engine.CostMatrix); ok {
						opts.CostMatrix = cm
					}
				}
			}
		}
	}

	vm.world.RLock()
	room := vm.world.Rooms[strings.ToUpper(strings.TrimSpace(roomName))]
	pathRes := engine.SearchPath(room, ox, oy, goals, opts)
	vm.world.RUnlock()

	pathTbl := L.NewTable()
	for i, step := range pathRes.Path {
		pathTbl.RawSetInt(i+1, vm.newRoomPosition(step.X, step.Y, roomName))
	}

	out := L.NewTable()
	out.RawSetString("path", pathTbl)
	out.RawSetString("ops", lua.LNumber(pathRes.Ops))
	out.RawSetString("cost", lua.LNumber(pathRes.Cost))
	out.RawSetString("incomplete", lua.LBool(pathRes.Incomplete))
	L.Push(out)
	return 1
}
