package lua

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	rt "github.com/arnodel/golua/runtime"

	"go-screeps/internal/engine"
	"go-screeps/internal/screeps"
)

// --- Pre-interned rt.Value string keys & table helpers ---

var internedKeys = func() map[string]rt.Value {
	keys := []string{
		"__index", "__call", "__id",
		"id", "pos", "room", "roomName", "name", "x", "y",
		"hits", "hitsMax", "my", "owner", "username", "store",
		"structureType", "ticksToDecay", "fatigue", "spawning", "ticksToLive",
		"body", "type", "boost", "memory", "needTime", "remainingTime",
		"level", "progress", "progressTotal", "safeMode", "safeModeAvailable",
		"safeModeCooldown", "ticksToDowngrade", "activateSafeMode",
		"energy", "energyCapacity", "ticksToRegeneration",
		"mineralType", "mineralAmount", "resourceType", "amount", "cooldown",
		"energyAvailable", "energyCapacityAvailable", "visual", "controller",
		"time", "rooms", "creeps", "spawns", "structures", "constructionSites",
		"getObjectById", "cpu", "limit", "tickLimit", "bucket", "getUsed",
		"map", "describeExits", "Memory", "Game", "PathFinder", "CostMatrix",
		"new", "search", "range", "filter", "plainCost", "swampCost", "flee",
		"maxOps", "maxRooms", "maxCost", "heuristicWeight", "roomCallback",
		"path", "ops", "cost", "incomplete", "ignoreCreeps", "reusePath",
		"dryRun", "color", "fill", "stroke", "strokeWidth", "width", "radius",
		"opacity", "lineStyle", "font", "align",
		"1", "3", "5", "7",
	}
	m := make(map[string]rt.Value, len(keys))
	for _, k := range keys {
		m[k] = rt.StringValue(k)
	}
	return m
}()

func strKey(key string) rt.Value {
	if v, ok := internedKeys[key]; ok {
		return v
	}
	return rt.StringValue(key)
}

func tblGet(t *rt.Table, key string) rt.Value {
	return t.Get(strKey(key))
}

func tblSet(t *rt.Table, key string, val rt.Value) {
	t.Set(strKey(key), val)
}

func tblGetInt(t *rt.Table, idx int) rt.Value {
	return t.Get(rt.IntValue(int64(idx)))
}

func tblSetInt(t *rt.Table, idx int, val rt.Value) {
	t.Set(rt.IntValue(int64(idx)), val)
}

func argAt(c *rt.GoCont, n int) rt.Value {
	if n < 0 || n >= c.NArgs() {
		return rt.NilValue
	}
	return c.Arg(n)
}

func isNumber(v rt.Value) bool {
	tp := v.Type()
	return tp == rt.IntType || tp == rt.FloatType
}

func asInt(v rt.Value) (int, bool) {
	switch v.Type() {
	case rt.IntType:
		return int(v.AsInt()), true
	case rt.FloatType:
		return int(v.AsFloat()), true
	default:
		return 0, false
	}
}

func asFloat(v rt.Value) (float64, bool) {
	switch v.Type() {
	case rt.IntType:
		return float64(v.AsInt()), true
	case rt.FloatType:
		return v.AsFloat(), true
	default:
		return 0, false
	}
}

func checkInt(c *rt.GoCont, n int) (int, error) {
	v := argAt(c, n)
	if f, ok := rt.ToFloat(v); ok {
		return int(f), nil
	}
	return 0, fmt.Errorf("bad argument #%d (number expected, got %s)", n+1, v.TypeName())
}

func checkFloat(c *rt.GoCont, n int) (float64, error) {
	v := argAt(c, n)
	if f, ok := rt.ToFloat(v); ok {
		return f, nil
	}
	return 0, fmt.Errorf("bad argument #%d (number expected, got %s)", n+1, v.TypeName())
}

func checkString(c *rt.GoCont, n int) (string, error) {
	v := argAt(c, n)
	if s, ok := v.TryString(); ok {
		return s, nil
	}
	return "", fmt.Errorf("bad argument #%d (string expected, got %s)", n+1, v.TypeName())
}

func checkTable(c *rt.GoCont, n int) (*rt.Table, error) {
	v := argAt(c, n)
	if t, ok := v.TryTable(); ok {
		return t, nil
	}
	return nil, fmt.Errorf("bad argument #%d (table expected, got %s)", n+1, v.TypeName())
}

func checkUserData(c *rt.GoCont, n int) (*rt.UserData, error) {
	v := argAt(c, n)
	if ud, ok := v.TryUserData(); ok {
		return ud, nil
	}
	return nil, fmt.Errorf("bad argument #%d (userdata expected, got %s)", n+1, v.TypeName())
}

func optString(c *rt.GoCont, n int, def string) string {
	v := argAt(c, n)
	if v.IsNil() {
		return def
	}
	if s, ok := v.TryString(); ok {
		return s
	}
	return def
}

func optInt(c *rt.GoCont, n int, def int) int {
	v := argAt(c, n)
	if v.IsNil() {
		return def
	}
	if f, ok := rt.ToFloat(v); ok {
		return int(f)
	}
	return def
}

// initAPI injects all Screeps constants, reusable metatables, and PathFinder into vm.R.
func (vm *BotVM) initAPI() {
	vm.injectConstants()
	vm.initMetatables()
	vm.injectPathFinder()
}

func (vm *BotVM) injectConstants() {
	env := vm.R.GlobalEnv()

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
		tblSet(env, k, rt.IntValue(int64(v)))
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
		tblSet(env, k, rt.StringValue(v))
	}

	// BODYPART_COST table
	bpCost := rt.NewTable()
	for part, cost := range engine.BODYPART_COST {
		tblSet(bpCost, part, rt.IntValue(int64(cost)))
	}
	tblSet(env, "BODYPART_COST", rt.TableValue(bpCost))

	// RoomPosition constructor global
	tblSet(env, "RoomPosition", rt.FunctionValue(vm.newGoFunc("RoomPosition", func(t *rt.Thread, c *rt.GoCont) (rt.Cont, error) {
		x, err := checkInt(c, 0)
		if err != nil {
			return nil, err
		}
		y, err := checkInt(c, 1)
		if err != nil {
			return nil, err
		}
		roomName, err := checkString(c, 2)
		if err != nil {
			return nil, err
		}
		return c.PushingNext1(t.Runtime, rt.TableValue(vm.newRoomPosition(x, y, roomName))), nil
	}, 3, false)))
}

type methodSpec struct {
	fn    rt.GoFunctionFunc
	nArgs int
}

func (vm *BotVM) initMetatables() {
	makeMT := func(methods map[string]methodSpec) *rt.Table {
		mt := rt.NewTable()
		idx := rt.NewTable()
		for name, spec := range methods {
			tblSet(idx, name, rt.FunctionValue(vm.newGoFunc(name, spec.fn, spec.nArgs, false)))
		}
		tblSet(mt, "__index", rt.TableValue(idx))
		return mt
	}

	vm.roomPosMT = makeMT(map[string]methodSpec{
		"getRangeTo":         {vm.posGetRangeTo, 3},
		"isNearTo":           {vm.posIsNearTo, 3},
		"inRangeTo":          {vm.posInRangeTo, 4},
		"isEqualTo":          {vm.posIsEqualTo, 3},
		"findClosestByRange": {vm.posFindClosestByRange, 2},
	})

	vm.terrainMT = makeMT(map[string]methodSpec{
		"get": {vm.terrainGet, 3},
	})

	vm.visualMT = makeMT(map[string]methodSpec{
		"circle": {vm.visualCircle, 4},
		"line":   {vm.visualLine, 6},
		"rect":   {vm.visualRect, 6},
		"poly":   {vm.visualPoly, 3},
		"text":   {vm.visualText, 5},
	})

	vm.storeMT = makeMT(map[string]methodSpec{
		"getUsedCapacity": {vm.storeGetUsedCapacity, 2},
		"getFreeCapacity": {vm.storeGetFreeCapacity, 2},
		"getCapacity":     {vm.storeGetCapacity, 2},
	})

	vm.roomMT = makeMT(map[string]methodSpec{
		"find":                   {vm.roomFind, 3},
		"createConstructionSite": {vm.roomCreateConstructionSite, 4},
		"getTerrain":             {vm.roomGetTerrain, 1},
		"lookForAt":              {vm.roomLookForAt, 4},
		"findExitTo":             {vm.roomFindExitTo, 2},
	})

	vm.creepMT = makeMT(map[string]methodSpec{
		"move":              {vm.creepMove, 2},
		"moveTo":            {vm.creepMoveTo, 4},
		"harvest":           {vm.creepTargetIntent(engine.IntentHarvest), 2},
		"transfer":          {vm.creepTransfer, 4},
		"withdraw":          {vm.creepWithdraw, 4},
		"pickup":            {vm.creepTargetIntent(engine.IntentPickup), 2},
		"drop":              {vm.creepDrop, 3},
		"build":             {vm.creepTargetIntent(engine.IntentBuild), 2},
		"repair":            {vm.creepTargetIntent(engine.IntentRepair), 2},
		"upgradeController": {vm.creepTargetIntent(engine.IntentUpgradeController), 2},
		"attack":            {vm.creepTargetIntent(engine.IntentAttack), 2},
		"rangedAttack":      {vm.creepTargetIntent(engine.IntentRangedAttack), 2},
		"heal":              {vm.creepTargetIntent(engine.IntentHeal), 2},
		"rangedHeal":        {vm.creepTargetIntent(engine.IntentRangedHeal), 2},
		"dismantle":         {vm.creepTargetIntent(engine.IntentDismantle), 2},
		"say":               {vm.creepSay, 2},
	})

	vm.spawnMT = makeMT(map[string]methodSpec{
		"spawnCreep": {vm.spawnCreep, 4},
	})

	vm.towerMT = makeMT(map[string]methodSpec{
		"attack": {vm.structureTargetIntent(engine.IntentTowerAttack), 2},
		"heal":   {vm.structureTargetIntent(engine.IntentTowerHeal), 2},
		"repair": {vm.structureTargetIntent(engine.IntentTowerRepair), 2},
	})

	vm.linkMT = makeMT(map[string]methodSpec{
		"transferEnergy": {vm.linkTransferEnergy, 3},
	})

	// CostMatrix userdata metatable
	vm.costMatMT = makeMT(map[string]methodSpec{
		"set":   {vm.costMatrixSet, 4},
		"get":   {vm.costMatrixGet, 3},
		"clone": {vm.costMatrixClone, 1},
	})
}

func (vm *BotVM) injectPathFinder() {
	pf := rt.NewTable()

	newCM := vm.newGoFunc("PathFinder.CostMatrix", func(t *rt.Thread, c *rt.GoCont) (rt.Cont, error) {
		ud := rt.NewUserData(engine.NewCostMatrix(), vm.costMatMT)
		return c.PushingNext1(t.Runtime, rt.UserDataValue(ud)), nil
	}, 1, false)

	cmCtor := rt.NewTable()
	tblSet(cmCtor, "new", rt.FunctionValue(newCM))
	cmCtorMT := rt.NewTable()
	tblSet(cmCtorMT, "__call", rt.FunctionValue(newCM))
	cmCtor.SetMetatable(cmCtorMT)

	tblSet(pf, "CostMatrix", rt.TableValue(cmCtor))
	tblSet(pf, "search", rt.FunctionValue(vm.newGoFunc("PathFinder.search", vm.pathFinderSearch, 3, false)))
	tblSet(vm.R.GlobalEnv(), "PathFinder", rt.TableValue(pf))
}

// refreshGame rebuilds the global Game table from world for this VM's UserID.
func (vm *BotVM) refreshGame(world *engine.World) {
	vm.objCache = make(map[string]*rt.Table, 64)
	vm.roomCache = make(map[string]*rt.Table, 8)
	vm.storeMeta = make(map[*rt.Table]storeRef, 64)
	vm.entityRoom = make(map[string]string, 128)

	game := rt.NewTable()
	roomsTbl := rt.NewTable()
	creepsTbl := rt.NewTable()
	spawnsTbl := rt.NewTable()
	structsTbl := rt.NewTable()
	sitesTbl := rt.NewTable()

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

	tblSet(game, "time", rt.IntValue(gameTime))

	for _, rName := range visibleRooms {
		rTbl := vm.wrapRoomByName(rName)
		if rTbl != nil {
			tblSet(roomsTbl, rName, rt.TableValue(rTbl))
		}
	}

	for _, es := range ownedEntities {
		obj := vm.wrapEntityByID(es.id)
		if obj == nil {
			continue
		}
		objVal := rt.TableValue(obj)
		switch {
		case es.typ == engine.TYPE_CREEP:
			if es.name != "" {
				tblSet(creepsTbl, es.name, objVal)
			}
		case es.typ == engine.STRUCTURE_SPAWN:
			if es.name != "" {
				tblSet(spawnsTbl, es.name, objVal)
			}
			tblSet(structsTbl, es.id, objVal)
		case es.typ == engine.TYPE_CONSTRUCTION_SITE:
			tblSet(sitesTbl, es.id, objVal)
		case es.isStruct:
			tblSet(structsTbl, es.id, objVal)
		}
	}

	tblSet(game, "rooms", rt.TableValue(roomsTbl))
	tblSet(game, "creeps", rt.TableValue(creepsTbl))
	tblSet(game, "spawns", rt.TableValue(spawnsTbl))
	tblSet(game, "structures", rt.TableValue(structsTbl))
	tblSet(game, "constructionSites", rt.TableValue(sitesTbl))

	tblSet(game, "getObjectById", rt.FunctionValue(vm.newGoFunc("Game.getObjectById", func(t *rt.Thread, c *rt.GoCont) (rt.Cont, error) {
		id := optString(c, 0, "")
		if id == "" {
			return c.PushingNext1(t.Runtime, rt.NilValue), nil
		}
		obj := vm.wrapEntityByID(id)
		if obj == nil {
			return c.PushingNext1(t.Runtime, rt.NilValue), nil
		}
		return c.PushingNext1(t.Runtime, rt.TableValue(obj)), nil
	}, 1, false)))

	cpuTbl := rt.NewTable()
	limitMs := float64(vm.CPULimit) / float64(time.Millisecond)
	if limitMs <= 0 {
		limitMs = float64(defaultCPULimit) / float64(time.Millisecond)
	}
	tblSet(cpuTbl, "limit", rt.FloatValue(limitMs))
	tblSet(cpuTbl, "tickLimit", rt.FloatValue(limitMs))
	tblSet(cpuTbl, "bucket", rt.IntValue(10000))
	tblSet(cpuTbl, "getUsed", rt.FunctionValue(vm.newGoFunc("Game.cpu.getUsed", func(t *rt.Thread, c *rt.GoCont) (rt.Cont, error) {
		usedMs := float64(time.Since(vm.tickStart)) / float64(time.Millisecond)
		return c.PushingNext1(t.Runtime, rt.FloatValue(usedMs)), nil
	}, 0, false)))
	tblSet(game, "cpu", rt.TableValue(cpuTbl))

	mapTbl := rt.NewTable()
	tblSet(mapTbl, "describeExits", rt.FunctionValue(vm.newGoFunc("Game.map.describeExits", func(t *rt.Thread, c *rt.GoCont) (rt.Cont, error) {
		rName := strings.ToUpper(strings.TrimSpace(optString(c, 0, "")))
		if rName == "" || vm.world == nil {
			return c.PushingNext1(t.Runtime, rt.NilValue), nil
		}
		vm.world.RLock()
		defer vm.world.RUnlock()
		r := vm.world.Rooms[rName]
		if r == nil {
			return c.PushingNext1(t.Runtime, rt.NilValue), nil
		}
		exits := rt.NewTable()
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
				tblSet(exits, d.key, rt.StringValue(adj))
			}
		}
		return c.PushingNext1(t.Runtime, rt.TableValue(exits)), nil
	}, 1, false)))
	tblSet(game, "map", rt.TableValue(mapTbl))

	tblSet(vm.R.GlobalEnv(), "Game", rt.TableValue(game))
}

// --- Memory helpers ---

func (vm *BotVM) ensureMemorySubtable(key string) *rt.Table {
	env := vm.R.GlobalEnv()
	mem, ok := tblGet(env, "Memory").TryTable()
	if !ok {
		mem = rt.NewTable()
		tblSet(env, "Memory", rt.TableValue(mem))
	}
	sub, ok := tblGet(mem, key).TryTable()
	if !ok {
		sub = rt.NewTable()
		tblSet(mem, key, rt.TableValue(sub))
	}
	return sub
}

func (vm *BotVM) getOrCreateCreepMemory(name string) *rt.Table {
	creepsMem := vm.ensureMemorySubtable("creeps")
	cm, ok := tblGet(creepsMem, name).TryTable()
	if !ok {
		cm = rt.NewTable()
		tblSet(creepsMem, name, rt.TableValue(cm))
	}
	return cm
}

func (vm *BotVM) getOrCreateRoomMemory(roomName string) *rt.Table {
	roomsMem := vm.ensureMemorySubtable("rooms")
	rm, ok := tblGet(roomsMem, roomName).TryTable()
	if !ok {
		rm = rt.NewTable()
		tblSet(roomsMem, roomName, rt.TableValue(rm))
	}
	return rm
}

// --- Object wrapping ---

func (vm *BotVM) newRoomPosition(x, y int, roomName string) *rt.Table {
	t := rt.NewTableWithCapacity(0, 3)
	tblSet(t, "x", rt.IntValue(int64(x)))
	tblSet(t, "y", rt.IntValue(int64(y)))
	tblSet(t, "roomName", rt.StringValue(roomName))
	t.SetMetatable(vm.roomPosMT)
	return t
}

func (vm *BotVM) newRoomVisual(roomName string) *rt.Table {
	t := rt.NewTableWithCapacity(0, 1)
	tblSet(t, "roomName", rt.StringValue(roomName))
	t.SetMetatable(vm.visualMT)
	return t
}

func (vm *BotVM) newRoomTerrain(roomName string) rt.Value {
	var grid *[engine.RoomSize * engine.RoomSize]uint8
	if vm.world != nil {
		vm.world.RLock()
		if room := vm.world.Rooms[roomName]; room != nil {
			cp := room.Terrain
			grid = &cp
		}
		vm.world.RUnlock()
	}
	return rt.UserDataValue(rt.NewUserData(grid, vm.terrainMT))
}

func (vm *BotVM) newStore(e *engine.Entity) *rt.Table {
	t := rt.NewTableWithCapacity(0, 2+len(e.Store))
	if e.Capacity(engine.RESOURCE_ENERGY) > 0 {
		tblSet(t, engine.RESOURCE_ENERGY, rt.IntValue(int64(e.UsedCapacity(engine.RESOURCE_ENERGY))))
	}
	for res, amt := range e.Store {
		tblSet(t, res, rt.IntValue(int64(amt)))
	}
	t.SetMetatable(vm.storeMT)
	if vm.storeMeta == nil {
		vm.storeMeta = make(map[*rt.Table]storeRef, 64)
	}
	vm.storeMeta[t] = storeRef{room: e.Room, id: e.ID}
	return t
}

func (vm *BotVM) wrapRoomByName(roomName string) *rt.Table {
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

	t := rt.NewTable()
	// Cache immediately to break room <-> controller reference cycles.
	vm.roomCache[roomName] = t

	tblSet(t, "name", rt.StringValue(roomName))
	tblSet(t, "energyAvailable", rt.IntValue(int64(energyAvail)))
	tblSet(t, "energyCapacityAvailable", rt.IntValue(int64(energyCap)))
	tblSet(t, "visual", rt.TableValue(vm.newRoomVisual(roomName)))
	tblSet(t, "memory", rt.TableValue(vm.getOrCreateRoomMemory(roomName)))
	if ctrlID != "" {
		if ctrlTbl := vm.wrapEntityByID(ctrlID); ctrlTbl != nil {
			tblSet(t, "controller", rt.TableValue(ctrlTbl))
		}
	}
	t.SetMetatable(vm.roomMT)
	return t
}

func (vm *BotVM) wrapEntityByID(id string) *rt.Table {
	if cached, ok := vm.objCache[id]; ok {
		return cached
	}
	if vm.world == nil {
		return nil
	}

	vm.world.RLock()
	var e *engine.Entity
	if rn, ok := vm.entityRoom[id]; ok {
		if r := vm.world.Rooms[rn]; r != nil {
			e = r.Entities[id]
		}
	}
	if e == nil {
		for _, r := range vm.world.Rooms {
			if found, ok := r.Entities[id]; ok {
				e = found
				if vm.entityRoom != nil {
					vm.entityRoom[id] = r.Name
				}
				break
			}
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
	var storeTbl *rt.Table
	if hasStore {
		storeTbl = vm.newStore(e)
	}
	vm.world.RUnlock()

	t := rt.NewTable()
	vm.objCache[id] = t

	tblSet(t, "id", rt.StringValue(id))
	tblSet(t, "pos", rt.TableValue(vm.newRoomPosition(ex, ey, roomName)))
	if rTbl := vm.wrapRoomByName(roomName); rTbl != nil {
		tblSet(t, "room", rt.TableValue(rTbl))
	}
	if name != "" {
		tblSet(t, "name", rt.StringValue(name))
	}
	if hitsMax > 0 {
		tblSet(t, "hits", rt.IntValue(int64(hits)))
		tblSet(t, "hitsMax", rt.IntValue(int64(hitsMax)))
	}
	if user != "" {
		tblSet(t, "my", rt.BoolValue(user == vm.UserID))
		if ownerName != "" {
			owner := rt.NewTable()
			tblSet(owner, "username", rt.StringValue(ownerName))
			tblSet(t, "owner", rt.TableValue(owner))
		}
	} else {
		tblSet(t, "my", rt.BoolValue(false))
	}
	if storeTbl != nil {
		tblSet(t, "store", rt.TableValue(storeTbl))
	}
	if isStruct {
		tblSet(t, "structureType", rt.StringValue(typ))
		if nextDecay > 0 {
			rem := nextDecay - gameTime
			if rem < 0 {
				rem = 0
			}
			tblSet(t, "ticksToDecay", rt.IntValue(rem))
		}
	}

	switch typ {
	case engine.TYPE_CREEP:
		tblSet(t, "fatigue", rt.IntValue(int64(fatigue)))
		tblSet(t, "spawning", rt.BoolValue(spawning))
		if ageTime > 0 {
			ttl := ageTime - gameTime
			if ttl < 0 {
				ttl = 0
			}
			tblSet(t, "ticksToLive", rt.IntValue(ttl))
		}
		bodyTbl := rt.NewTable()
		for i, bp := range bodyCopy {
			bt := rt.NewTable()
			tblSet(bt, "type", rt.StringValue(bp.Type))
			tblSet(bt, "hits", rt.IntValue(int64(bp.Hits)))
			if bp.Boost != "" {
				tblSet(bt, "boost", rt.StringValue(bp.Boost))
			}
			tblSetInt(bodyTbl, i+1, rt.TableValue(bt))
		}
		tblSet(t, "body", rt.TableValue(bodyTbl))
		if user == vm.UserID && name != "" {
			tblSet(t, "memory", rt.TableValue(vm.getOrCreateCreepMemory(name)))
		}
		t.SetMetatable(vm.creepMT)

	case engine.STRUCTURE_SPAWN:
		if spInfo != nil {
			spTbl := rt.NewTable()
			tblSet(spTbl, "name", rt.StringValue(spInfo.Name))
			tblSet(spTbl, "needTime", rt.IntValue(int64(spInfo.NeedTime)))
			rem := spInfo.SpawnTime - gameTime
			if rem < 0 {
				rem = 0
			}
			tblSet(spTbl, "remainingTime", rt.IntValue(rem))
			tblSet(t, "spawning", rt.TableValue(spTbl))
		} else {
			tblSet(t, "spawning", rt.NilValue)
		}
		t.SetMetatable(vm.spawnMT)

	case engine.STRUCTURE_TOWER:
		t.SetMetatable(vm.towerMT)

	case engine.STRUCTURE_LINK:
		tblSet(t, "cooldown", rt.IntValue(int64(cooldown)))
		t.SetMetatable(vm.linkMT)

	case engine.STRUCTURE_CONTROLLER:
		tblSet(t, "level", rt.IntValue(int64(level)))
		tblSet(t, "progress", rt.IntValue(int64(progress)))
		tblSet(t, "progressTotal", rt.IntValue(int64(progressTotal)))
		tblSet(t, "safeModeAvailable", rt.IntValue(int64(safeModeAvail)))
		if safeMode > gameTime {
			tblSet(t, "safeMode", rt.IntValue(safeMode-gameTime))
		}
		if safeModeCD > gameTime {
			tblSet(t, "safeModeCooldown", rt.IntValue(safeModeCD-gameTime))
		}
		if downgradeTime > 0 {
			rem := downgradeTime - gameTime
			if rem < 0 {
				rem = 0
			}
			tblSet(t, "ticksToDowngrade", rt.IntValue(rem))
		}
		ctrlID := id
		tblSet(t, "activateSafeMode", rt.FunctionValue(vm.newGoFunc("Controller.activateSafeMode", func(th *rt.Thread, c *rt.GoCont) (rt.Cont, error) {
			if vm.world == nil {
				return c.PushingNext1(th.Runtime, rt.IntValue(int64(engine.ERR_INVALID_TARGET))), nil
			}
			code := vm.world.QueueIntent(vm.UserID, engine.Intent{
				Type:    engine.IntentActivateSafeMode,
				ActorID: ctrlID,
			})
			return c.PushingNext1(th.Runtime, rt.IntValue(int64(code))), nil
		}, 1, false)))

	case engine.TYPE_SOURCE:
		tblSet(t, "energy", rt.IntValue(int64(energy)))
		tblSet(t, "energyCapacity", rt.IntValue(int64(energyCap)))
		if nextRegen > 0 {
			rem := nextRegen - gameTime
			if rem < 0 {
				rem = 0
			}
			tblSet(t, "ticksToRegeneration", rt.IntValue(rem))
		}

	case engine.TYPE_MINERAL:
		tblSet(t, "mineralType", rt.StringValue(mineralType))
		tblSet(t, "mineralAmount", rt.IntValue(int64(mineralAmt)))

	case engine.TYPE_CONSTRUCTION_SITE:
		tblSet(t, "structureType", rt.StringValue(siteStructType))
		tblSet(t, "progress", rt.IntValue(int64(progress)))
		tblSet(t, "progressTotal", rt.IntValue(int64(progressTotal)))

	case engine.TYPE_RESOURCE:
		tblSet(t, "resourceType", rt.StringValue(resType))
		tblSet(t, "amount", rt.IntValue(int64(amount)))

	case engine.TYPE_TOMBSTONE:
		tblSet(t, "creepId", rt.StringValue(creepID))
		tblSet(t, "deathTime", rt.IntValue(deathTime))
		if decayTime > 0 {
			rem := decayTime - gameTime
			if rem < 0 {
				rem = 0
			}
			tblSet(t, "ticksToDecay", rt.IntValue(rem))
		}
	}

	return t
}

// --- Coordinate extraction helpers ---

func extractPos(v rt.Value) (x, y int, roomName string, ok bool) {
	tbl, isTbl := v.TryTable()
	if !isTbl {
		return 0, 0, "", false
	}
	if posVal := tblGet(tbl, "pos"); !posVal.IsNil() {
		if posTbl, ok2 := posVal.TryTable(); ok2 {
			tbl = posTbl
		}
	}
	xv, okX := asInt(tblGet(tbl, "x"))
	yv, okY := asInt(tblGet(tbl, "y"))
	if !okX || !okY {
		return 0, 0, "", false
	}
	rn, _ := tblGet(tbl, "roomName").TryString()
	return xv, yv, rn, true
}

func extractTargetID(v rt.Value) (string, bool) {
	tbl, ok := v.TryTable()
	if !ok {
		return "", false
	}
	id, ok := tblGet(tbl, "id").TryString()
	if !ok || id == "" {
		return "", false
	}
	return id, true
}

// --- RoomPosition methods ---

func (vm *BotVM) posGetRangeTo(t *rt.Thread, c *rt.GoCont) (rt.Cont, error) {
	x1, y1, r1, ok1 := extractPos(argAt(c, 0))
	if !ok1 {
		return c.PushingNext1(t.Runtime, rt.FloatValue(math.Inf(1))), nil
	}
	var x2, y2 int
	var r2 string
	if isNumber(argAt(c, 1)) {
		var err error
		x2, err = checkInt(c, 1)
		if err != nil {
			return nil, err
		}
		y2, err = checkInt(c, 2)
		if err != nil {
			return nil, err
		}
		r2 = r1
	} else {
		var ok2 bool
		x2, y2, r2, ok2 = extractPos(argAt(c, 1))
		if !ok2 {
			return c.PushingNext1(t.Runtime, rt.FloatValue(math.Inf(1))), nil
		}
	}
	if r1 != "" && r2 != "" && r1 != r2 {
		return c.PushingNext1(t.Runtime, rt.FloatValue(math.Inf(1))), nil
	}
	return c.PushingNext1(t.Runtime, rt.IntValue(int64(engine.ChebyshevDist(x1, y1, x2, y2)))), nil
}

func (vm *BotVM) posIsNearTo(t *rt.Thread, c *rt.GoCont) (rt.Cont, error) {
	x1, y1, r1, ok1 := extractPos(argAt(c, 0))
	if !ok1 {
		return c.PushingNext1(t.Runtime, rt.BoolValue(false)), nil
	}
	var x2, y2 int
	var r2 string
	if isNumber(argAt(c, 1)) {
		var err error
		x2, err = checkInt(c, 1)
		if err != nil {
			return nil, err
		}
		y2, err = checkInt(c, 2)
		if err != nil {
			return nil, err
		}
		r2 = r1
	} else {
		var ok2 bool
		x2, y2, r2, ok2 = extractPos(argAt(c, 1))
		if !ok2 {
			return c.PushingNext1(t.Runtime, rt.BoolValue(false)), nil
		}
	}
	if r1 != "" && r2 != "" && r1 != r2 {
		return c.PushingNext1(t.Runtime, rt.BoolValue(false)), nil
	}
	return c.PushingNext1(t.Runtime, rt.BoolValue(engine.ChebyshevDist(x1, y1, x2, y2) <= 1)), nil
}

func (vm *BotVM) posInRangeTo(t *rt.Thread, c *rt.GoCont) (rt.Cont, error) {
	x1, y1, r1, ok1 := extractPos(argAt(c, 0))
	if !ok1 {
		return c.PushingNext1(t.Runtime, rt.BoolValue(false)), nil
	}
	var x2, y2, rng int
	var r2 string
	var err error
	if isNumber(argAt(c, 1)) {
		if x2, err = checkInt(c, 1); err != nil {
			return nil, err
		}
		if y2, err = checkInt(c, 2); err != nil {
			return nil, err
		}
		if rng, err = checkInt(c, 3); err != nil {
			return nil, err
		}
		r2 = r1
	} else {
		var ok2 bool
		x2, y2, r2, ok2 = extractPos(argAt(c, 1))
		if !ok2 {
			return c.PushingNext1(t.Runtime, rt.BoolValue(false)), nil
		}
		if rng, err = checkInt(c, 2); err != nil {
			return nil, err
		}
	}
	if r1 != "" && r2 != "" && r1 != r2 {
		return c.PushingNext1(t.Runtime, rt.BoolValue(false)), nil
	}
	return c.PushingNext1(t.Runtime, rt.BoolValue(engine.ChebyshevDist(x1, y1, x2, y2) <= rng)), nil
}

func (vm *BotVM) posIsEqualTo(t *rt.Thread, c *rt.GoCont) (rt.Cont, error) {
	x1, y1, r1, ok1 := extractPos(argAt(c, 0))
	if !ok1 {
		return c.PushingNext1(t.Runtime, rt.BoolValue(false)), nil
	}
	var x2, y2 int
	var r2 string
	if isNumber(argAt(c, 1)) {
		var err error
		if x2, err = checkInt(c, 1); err != nil {
			return nil, err
		}
		if y2, err = checkInt(c, 2); err != nil {
			return nil, err
		}
		r2 = r1
	} else {
		var ok2 bool
		x2, y2, r2, ok2 = extractPos(argAt(c, 1))
		if !ok2 {
			return c.PushingNext1(t.Runtime, rt.BoolValue(false)), nil
		}
	}
	if r1 != "" && r2 != "" && r1 != r2 {
		return c.PushingNext1(t.Runtime, rt.BoolValue(false)), nil
	}
	return c.PushingNext1(t.Runtime, rt.BoolValue(x1 == x2 && y1 == y2)), nil
}

func (vm *BotVM) posFindClosestByRange(t *rt.Thread, c *rt.GoCont) (rt.Cont, error) {
	x1, y1, r1, ok1 := extractPos(argAt(c, 0))
	if !ok1 {
		return c.PushingNext1(t.Runtime, rt.NilValue), nil
	}
	list, ok := argAt(c, 1).TryTable()
	if !ok {
		return c.PushingNext1(t.Runtime, rt.NilValue), nil
	}

	best := rt.NilValue
	bestDist := 1 << 30
	n := int(list.Len())
	for i := 1; i <= n; i++ {
		item := tblGetInt(list, i)
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
	return c.PushingNext1(t.Runtime, best), nil
}

// --- Store methods ---

func (vm *BotVM) storeEntityRef(c *rt.GoCont) (storeRef, error) {
	self, err := checkTable(c, 0)
	if err != nil {
		return storeRef{}, err
	}
	if ref, ok := vm.storeMeta[self]; ok {
		return ref, nil
	}
	return storeRef{}, nil
}

func (vm *BotVM) findStoreEntityLocked(ref storeRef) *engine.Entity {
	if ref.id == "" {
		return nil
	}
	if ref.room != "" {
		if r := vm.world.Rooms[ref.room]; r != nil {
			if e, ok := r.Entities[ref.id]; ok {
				return e
			}
		}
	}
	for _, r := range vm.world.Rooms {
		if e, ok := r.Entities[ref.id]; ok {
			return e
		}
	}
	return nil
}

func (vm *BotVM) storeGetUsedCapacity(t *rt.Thread, c *rt.GoCont) (rt.Cont, error) {
	ref, err := vm.storeEntityRef(c)
	if err != nil {
		return nil, err
	}
	res := optString(c, 1, "")
	if ref.id == "" || vm.world == nil {
		return c.PushingNext1(t.Runtime, rt.IntValue(0)), nil
	}
	vm.world.RLock()
	var val int
	if e := vm.findStoreEntityLocked(ref); e != nil {
		val = e.UsedCapacity(res)
	}
	vm.world.RUnlock()
	return c.PushingNext1(t.Runtime, rt.IntValue(int64(val))), nil
}

func (vm *BotVM) storeGetFreeCapacity(t *rt.Thread, c *rt.GoCont) (rt.Cont, error) {
	ref, err := vm.storeEntityRef(c)
	if err != nil {
		return nil, err
	}
	res := optString(c, 1, "")
	if ref.id == "" || vm.world == nil {
		return c.PushingNext1(t.Runtime, rt.IntValue(0)), nil
	}
	vm.world.RLock()
	var val int
	if e := vm.findStoreEntityLocked(ref); e != nil {
		val = e.FreeCapacity(res)
	}
	vm.world.RUnlock()
	return c.PushingNext1(t.Runtime, rt.IntValue(int64(val))), nil
}

func (vm *BotVM) storeGetCapacity(t *rt.Thread, c *rt.GoCont) (rt.Cont, error) {
	ref, err := vm.storeEntityRef(c)
	if err != nil {
		return nil, err
	}
	res := optString(c, 1, "")
	if ref.id == "" || vm.world == nil {
		return c.PushingNext1(t.Runtime, rt.IntValue(0)), nil
	}
	vm.world.RLock()
	var val int
	if e := vm.findStoreEntityLocked(ref); e != nil {
		val = e.Capacity(res)
	}
	vm.world.RUnlock()
	return c.PushingNext1(t.Runtime, rt.IntValue(int64(val))), nil
}

// --- RoomTerrain methods ---

func (vm *BotVM) terrainGet(t *rt.Thread, c *rt.GoCont) (rt.Cont, error) {
	ud, err := checkUserData(c, 0)
	if err != nil {
		return nil, err
	}
	x, err := checkInt(c, 1)
	if err != nil {
		return nil, err
	}
	y, err := checkInt(c, 2)
	if err != nil {
		return nil, err
	}
	grid, _ := ud.Value().(*[engine.RoomSize * engine.RoomSize]uint8)
	if grid == nil || !engine.InBounds(x, y) {
		return c.PushingNext1(t.Runtime, rt.IntValue(int64(engine.TERRAIN_MASK_WALL))), nil
	}
	return c.PushingNext1(t.Runtime, rt.IntValue(int64(grid[y*engine.RoomSize+x]))), nil
}

// --- RoomVisual methods ---

func parseVisualStyle(styleVal rt.Value) (radius float32, fill, col string, opac, sw float32) {
	opac = 0.5
	sw = 0.1
	tbl, ok := styleVal.TryTable()
	if !ok {
		return
	}
	if v, ok := asFloat(tblGet(tbl, "radius")); ok {
		radius = float32(v)
	}
	if v, ok := tblGet(tbl, "fill").TryString(); ok {
		fill = v
	}
	if v, ok := tblGet(tbl, "stroke").TryString(); ok {
		col = v
	}
	if v, ok := tblGet(tbl, "color").TryString(); ok && col == "" {
		col = v
	}
	if v, ok := asFloat(tblGet(tbl, "opacity")); ok {
		opac = float32(v)
	}
	if v, ok := asFloat(tblGet(tbl, "strokeWidth")); ok {
		sw = float32(v)
	} else if v, ok := asFloat(tblGet(tbl, "width")); ok {
		sw = float32(v)
	}
	return
}

func (vm *BotVM) visualCircle(t *rt.Thread, c *rt.GoCont) (rt.Cont, error) {
	self, err := checkTable(c, 0)
	if err != nil {
		return nil, err
	}
	roomName, _ := tblGet(self, "roomName").TryString()
	var x, y float32
	var styleVal rt.Value
	if argAt(c, 1).Type() == rt.TableType {
		px, py, _, _ := extractPos(argAt(c, 1))
		x, y = float32(px), float32(py)
		styleVal = argAt(c, 2)
	} else {
		xf, err := checkFloat(c, 1)
		if err != nil {
			return nil, err
		}
		yf, err := checkFloat(c, 2)
		if err != nil {
			return nil, err
		}
		x, y = float32(xf), float32(yf)
		styleVal = argAt(c, 3)
	}
	r, fill, col, opac, sw := parseVisualStyle(styleVal)
	if vm.world != nil {
		vm.world.AddVisual(roomName, screeps.VisualOp{
			Type: "c", X: x, Y: y, R: r, Fill: fill, Col: col, Opac: opac, SW: sw,
		})
	}
	return c.PushingNext1(t.Runtime, rt.TableValue(self)), nil
}

func (vm *BotVM) visualLine(t *rt.Thread, c *rt.GoCont) (rt.Cont, error) {
	self, err := checkTable(c, 0)
	if err != nil {
		return nil, err
	}
	roomName, _ := tblGet(self, "roomName").TryString()
	var x1, y1, x2, y2 float32
	var styleVal rt.Value
	if argAt(c, 1).Type() == rt.TableType {
		px1, py1, _, _ := extractPos(argAt(c, 1))
		px2, py2, _, _ := extractPos(argAt(c, 2))
		x1, y1, x2, y2 = float32(px1), float32(py1), float32(px2), float32(py2)
		styleVal = argAt(c, 3)
	} else {
		f1, err := checkFloat(c, 1)
		if err != nil {
			return nil, err
		}
		f2, err := checkFloat(c, 2)
		if err != nil {
			return nil, err
		}
		f3, err := checkFloat(c, 3)
		if err != nil {
			return nil, err
		}
		f4, err := checkFloat(c, 4)
		if err != nil {
			return nil, err
		}
		x1, y1, x2, y2 = float32(f1), float32(f2), float32(f3), float32(f4)
		styleVal = argAt(c, 5)
	}
	_, _, col, opac, sw := parseVisualStyle(styleVal)
	if vm.world != nil {
		vm.world.AddVisual(roomName, screeps.VisualOp{
			Type: "l", X: x1, Y: y1, X2: x2, Y2: y2, Col: col, Opac: opac, SW: sw,
		})
	}
	return c.PushingNext1(t.Runtime, rt.TableValue(self)), nil
}

func (vm *BotVM) visualRect(t *rt.Thread, c *rt.GoCont) (rt.Cont, error) {
	self, err := checkTable(c, 0)
	if err != nil {
		return nil, err
	}
	roomName, _ := tblGet(self, "roomName").TryString()
	var x, y, w, h float32
	var styleVal rt.Value
	if argAt(c, 1).Type() == rt.TableType {
		px, py, _, _ := extractPos(argAt(c, 1))
		x, y = float32(px), float32(py)
		wf, err := checkFloat(c, 2)
		if err != nil {
			return nil, err
		}
		hf, err := checkFloat(c, 3)
		if err != nil {
			return nil, err
		}
		w, h = float32(wf), float32(hf)
		styleVal = argAt(c, 4)
	} else {
		xf, err := checkFloat(c, 1)
		if err != nil {
			return nil, err
		}
		yf, err := checkFloat(c, 2)
		if err != nil {
			return nil, err
		}
		wf, err := checkFloat(c, 3)
		if err != nil {
			return nil, err
		}
		hf, err := checkFloat(c, 4)
		if err != nil {
			return nil, err
		}
		x, y, w, h = float32(xf), float32(yf), float32(wf), float32(hf)
		styleVal = argAt(c, 5)
	}
	_, fill, col, opac, sw := parseVisualStyle(styleVal)
	if vm.world != nil {
		vm.world.AddVisual(roomName, screeps.VisualOp{
			Type: "r", X: x, Y: y, W: w, H: h, Fill: fill, Col: col, Opac: opac, SW: sw,
		})
	}
	return c.PushingNext1(t.Runtime, rt.TableValue(self)), nil
}

func (vm *BotVM) visualPoly(t *rt.Thread, c *rt.GoCont) (rt.Cont, error) {
	self, err := checkTable(c, 0)
	if err != nil {
		return nil, err
	}
	roomName, _ := tblGet(self, "roomName").TryString()
	ptsTbl, err := checkTable(c, 1)
	if err != nil {
		return nil, err
	}
	styleVal := argAt(c, 2)
	var pts [][2]float32
	n := int(ptsTbl.Len())
	for i := 1; i <= n; i++ {
		item := tblGetInt(ptsTbl, i)
		if pair, ok := item.TryTable(); ok {
			if px, py, _, ok2 := extractPos(item); ok2 {
				pts = append(pts, [2]float32{float32(px), float32(py)})
			} else if xv, okX := asFloat(tblGetInt(pair, 1)); okX {
				if yv, okY := asFloat(tblGetInt(pair, 2)); okY {
					pts = append(pts, [2]float32{float32(xv), float32(yv)})
				}
			}
		}
	}
	_, fill, col, opac, sw := parseVisualStyle(styleVal)
	if vm.world != nil {
		vm.world.AddVisual(roomName, screeps.VisualOp{
			Type: "p", Pts: pts, Fill: fill, Col: col, Opac: opac, SW: sw,
		})
	}
	return c.PushingNext1(t.Runtime, rt.TableValue(self)), nil
}

func (vm *BotVM) visualText(t *rt.Thread, c *rt.GoCont) (rt.Cont, error) {
	self, err := checkTable(c, 0)
	if err != nil {
		return nil, err
	}
	roomName, _ := tblGet(self, "roomName").TryString()
	text, err := checkString(c, 1)
	if err != nil {
		return nil, err
	}
	var x, y float32
	var styleVal rt.Value
	if argAt(c, 2).Type() == rt.TableType {
		px, py, _, _ := extractPos(argAt(c, 2))
		x, y = float32(px), float32(py)
		styleVal = argAt(c, 3)
	} else {
		xf, err := checkFloat(c, 2)
		if err != nil {
			return nil, err
		}
		yf, err := checkFloat(c, 3)
		if err != nil {
			return nil, err
		}
		x, y = float32(xf), float32(yf)
		styleVal = argAt(c, 4)
	}
	_, _, col, opac, _ := parseVisualStyle(styleVal)
	if vm.world != nil {
		vm.world.AddVisual(roomName, screeps.VisualOp{
			Type: "t", Text: text, X: x, Y: y, Col: col, Opac: opac,
		})
	}
	return c.PushingNext1(t.Runtime, rt.TableValue(self)), nil
}

// --- Room methods ---

func (vm *BotVM) roomFind(t *rt.Thread, c *rt.GoCont) (rt.Cont, error) {
	self, err := checkTable(c, 0)
	if err != nil {
		return nil, err
	}
	findConst, err := checkInt(c, 1)
	if err != nil {
		return nil, err
	}
	optsVal := argAt(c, 2)
	roomName, _ := tblGet(self, "name").TryString()

	resTbl := rt.NewTable()
	if vm.world == nil {
		return c.PushingNext1(t.Runtime, rt.TableValue(resTbl)), nil
	}

	// Exit finds return RoomPosition tables.
	if findConst == engine.FIND_EXIT_TOP || findConst == engine.FIND_EXIT_RIGHT ||
		findConst == engine.FIND_EXIT_BOTTOM || findConst == engine.FIND_EXIT_LEFT ||
		findConst == engine.FIND_EXIT {
		vm.world.RLock()
		room := vm.world.Rooms[roomName]
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
		for i, coord := range coords {
			tblSetInt(resTbl, i+1, rt.TableValue(vm.newRoomPosition(coord[0], coord[1], roomName)))
		}
		return c.PushingNext1(t.Runtime, rt.TableValue(resTbl)), nil
	}

	vm.world.RLock()
	room := vm.world.Rooms[roomName]
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

	var filterFn rt.Value
	if optsTbl, ok := optsVal.TryTable(); ok {
		if f := tblGet(optsTbl, "filter"); f.Type() == rt.FunctionType {
			filterFn = f
		}
	}

	idx := 1
	for _, id := range ids {
		obj := vm.wrapEntityByID(id)
		if obj == nil {
			continue
		}
		objVal := rt.TableValue(obj)
		if !filterFn.IsNil() {
			keep, err := rt.Call1(t, filterFn, objVal)
			if err != nil {
				continue
			}
			if !rt.Truth(keep) {
				continue
			}
		}
		tblSetInt(resTbl, idx, objVal)
		idx++
	}

	return c.PushingNext1(t.Runtime, rt.TableValue(resTbl)), nil
}

func (vm *BotVM) roomCreateConstructionSite(t *rt.Thread, c *rt.GoCont) (rt.Cont, error) {
	self, err := checkTable(c, 0)
	if err != nil {
		return nil, err
	}
	roomName, _ := tblGet(self, "name").TryString()
	if vm.world == nil {
		return c.PushingNext1(t.Runtime, rt.IntValue(int64(engine.ERR_INVALID_ARGS))), nil
	}
	var x, y int
	var structType string
	if argAt(c, 1).Type() == rt.TableType {
		var ok bool
		x, y, _, ok = extractPos(argAt(c, 1))
		if !ok {
			return c.PushingNext1(t.Runtime, rt.IntValue(int64(engine.ERR_INVALID_ARGS))), nil
		}
		structType, err = checkString(c, 2)
		if err != nil {
			return nil, err
		}
	} else {
		if x, err = checkInt(c, 1); err != nil {
			return nil, err
		}
		if y, err = checkInt(c, 2); err != nil {
			return nil, err
		}
		if structType, err = checkString(c, 3); err != nil {
			return nil, err
		}
	}
	_, code := vm.world.CreateConstructionSite(vm.UserID, roomName, x, y, structType)
	return c.PushingNext1(t.Runtime, rt.IntValue(int64(code))), nil
}

func (vm *BotVM) roomGetTerrain(t *rt.Thread, c *rt.GoCont) (rt.Cont, error) {
	self, err := checkTable(c, 0)
	if err != nil {
		return nil, err
	}
	roomName, _ := tblGet(self, "name").TryString()
	return c.PushingNext1(t.Runtime, vm.newRoomTerrain(roomName)), nil
}

func (vm *BotVM) roomFindExitTo(t *rt.Thread, c *rt.GoCont) (rt.Cont, error) {
	self, err := checkTable(c, 0)
	if err != nil {
		return nil, err
	}
	fromName, _ := tblGet(self, "name").TryString()
	target := strings.ToUpper(strings.TrimSpace(optString(c, 1, "")))
	if target == "" || vm.world == nil {
		return c.PushingNext1(t.Runtime, rt.IntValue(int64(engine.ERR_INVALID_ARGS))), nil
	}
	fx, fy, ok1 := engine.ParseRoomCoords(fromName)
	tx, ty, ok2 := engine.ParseRoomCoords(target)
	if !ok1 || !ok2 {
		return c.PushingNext1(t.Runtime, rt.IntValue(int64(engine.ERR_INVALID_ARGS))), nil
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
		return c.PushingNext1(t.Runtime, rt.IntValue(int64(engine.ERR_NO_PATH))), nil
	}
	vm.world.RLock()
	r := vm.world.Rooms[fromName]
	hasExit := r != nil && r.RoomHasExitEdge(exitConst) && vm.world.Rooms[target] != nil
	vm.world.RUnlock()
	if !hasExit {
		return c.PushingNext1(t.Runtime, rt.IntValue(int64(engine.ERR_NO_PATH))), nil
	}
	return c.PushingNext1(t.Runtime, rt.IntValue(int64(exitConst))), nil
}

func (vm *BotVM) roomLookForAt(t *rt.Thread, c *rt.GoCont) (rt.Cont, error) {
	self, err := checkTable(c, 0)
	if err != nil {
		return nil, err
	}
	roomName, _ := tblGet(self, "name").TryString()
	lookType, err := checkString(c, 1)
	if err != nil {
		return nil, err
	}
	var x, y int
	if argAt(c, 2).Type() == rt.TableType {
		var ok bool
		x, y, _, ok = extractPos(argAt(c, 2))
		if !ok {
			return c.PushingNext1(t.Runtime, rt.TableValue(rt.NewTable())), nil
		}
	} else {
		if x, err = checkInt(c, 2); err != nil {
			return nil, err
		}
		if y, err = checkInt(c, 3); err != nil {
			return nil, err
		}
	}

	resTbl := rt.NewTable()
	if vm.world == nil {
		return c.PushingNext1(t.Runtime, rt.TableValue(resTbl)), nil
	}

	if lookType == "terrain" {
		vm.world.RLock()
		room := vm.world.Rooms[roomName]
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
		tblSetInt(resTbl, 1, rt.StringValue(tStr))
		return c.PushingNext1(t.Runtime, rt.TableValue(resTbl)), nil
	}

	vm.world.RLock()
	room := vm.world.Rooms[roomName]
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
			tblSetInt(resTbl, i+1, rt.TableValue(obj))
		}
	}
	return c.PushingNext1(t.Runtime, rt.TableValue(resTbl)), nil
}

// --- Creep methods ---

func (vm *BotVM) creepMove(t *rt.Thread, c *rt.GoCont) (rt.Cont, error) {
	id, ok := extractTargetID(argAt(c, 0))
	if !ok || vm.world == nil {
		return c.PushingNext1(t.Runtime, rt.IntValue(int64(engine.ERR_INVALID_TARGET))), nil
	}
	dir, err := checkInt(c, 1)
	if err != nil {
		return nil, err
	}
	code := vm.world.QueueIntent(vm.UserID, engine.Intent{
		Type:      engine.IntentMove,
		ActorID:   id,
		Direction: dir,
	})
	return c.PushingNext1(t.Runtime, rt.IntValue(int64(code))), nil
}

func (vm *BotVM) creepMoveTo(t *rt.Thread, c *rt.GoCont) (rt.Cont, error) {
	id, ok := extractTargetID(argAt(c, 0))
	if !ok || vm.world == nil {
		return c.PushingNext1(t.Runtime, rt.IntValue(int64(engine.ERR_INVALID_TARGET))), nil
	}

	var tx, ty int
	var optsVal rt.Value
	if isNumber(argAt(c, 1)) {
		var err error
		if tx, err = checkInt(c, 1); err != nil {
			return nil, err
		}
		if ty, err = checkInt(c, 2); err != nil {
			return nil, err
		}
		optsVal = argAt(c, 3)
	} else {
		var okPos bool
		tx, ty, _, okPos = extractPos(argAt(c, 1))
		if !okPos {
			return c.PushingNext1(t.Runtime, rt.IntValue(int64(engine.ERR_INVALID_TARGET))), nil
		}
		optsVal = argAt(c, 2)
	}

	pathOpts := engine.PathOpts{
		MoverUser: vm.UserID,
	}
	hasRange := false
	customRange := 0
	if optsTbl, ok := optsVal.TryTable(); ok {
		if v := tblGet(optsTbl, "ignoreCreeps"); !v.IsNil() {
			pathOpts.IgnoreCreeps = rt.Truth(v)
		}
		if v := tblGet(optsTbl, "ignoreRoads"); !v.IsNil() {
			pathOpts.IgnoreRoads = rt.Truth(v)
		}
		if v, ok := asInt(tblGet(optsTbl, "maxOps")); ok {
			pathOpts.MaxOps = v
		}
		if v, ok := asInt(tblGet(optsTbl, "plainCost")); ok {
			pathOpts.PlainCost = uint8(v)
		}
		if v, ok := asInt(tblGet(optsTbl, "swampCost")); ok {
			pathOpts.SwampCost = uint8(v)
		}
		if v, ok := asInt(tblGet(optsTbl, "range")); ok {
			hasRange = true
			customRange = v
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
		return c.PushingNext1(t.Runtime, rt.IntValue(int64(engine.ERR_INVALID_TARGET))), nil
	}
	if actor.User != vm.UserID {
		vm.world.RUnlock()
		return c.PushingNext1(t.Runtime, rt.IntValue(int64(engine.ERR_NOT_OWNER))), nil
	}
	if actor.Spawning {
		vm.world.RUnlock()
		return c.PushingNext1(t.Runtime, rt.IntValue(int64(engine.ERR_BUSY))), nil
	}
	if actor.CountActiveParts(engine.MOVE) == 0 {
		vm.world.RUnlock()
		return c.PushingNext1(t.Runtime, rt.IntValue(int64(engine.ERR_NO_BODYPART))), nil
	}
	if actor.Fatigue > 0 {
		vm.world.RUnlock()
		return c.PushingNext1(t.Runtime, rt.IntValue(int64(engine.ERR_TIRED))), nil
	}
	if actor.X == tx && actor.Y == ty {
		vm.world.RUnlock()
		return c.PushingNext1(t.Runtime, rt.IntValue(int64(engine.OK))), nil
	}

	rng := 0
	if hasRange {
		rng = customRange
	} else if !room.IsWalkableFor(tx, ty, vm.UserID, true) {
		rng = 1
	}
	if engine.ChebyshevDist(actor.X, actor.Y, tx, ty) <= rng {
		vm.world.RUnlock()
		return c.PushingNext1(t.Runtime, rt.IntValue(int64(engine.OK))), nil
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
		return c.PushingNext1(t.Runtime, rt.IntValue(int64(engine.ERR_NO_PATH))), nil
	}

	code := vm.world.QueueIntent(vm.UserID, engine.Intent{
		Type:      engine.IntentMove,
		ActorID:   id,
		Direction: dir,
	})
	return c.PushingNext1(t.Runtime, rt.IntValue(int64(code))), nil
}

func (vm *BotVM) creepTargetIntent(intentType engine.IntentType) rt.GoFunctionFunc {
	return func(t *rt.Thread, c *rt.GoCont) (rt.Cont, error) {
		actorID, ok := extractTargetID(argAt(c, 0))
		if !ok || vm.world == nil {
			return c.PushingNext1(t.Runtime, rt.IntValue(int64(engine.ERR_INVALID_TARGET))), nil
		}
		targetID, ok := extractTargetID(argAt(c, 1))
		if !ok {
			return c.PushingNext1(t.Runtime, rt.IntValue(int64(engine.ERR_INVALID_TARGET))), nil
		}
		code := vm.world.QueueIntent(vm.UserID, engine.Intent{
			Type:     intentType,
			ActorID:  actorID,
			TargetID: targetID,
		})
		return c.PushingNext1(t.Runtime, rt.IntValue(int64(code))), nil
	}
}

func (vm *BotVM) creepTransfer(t *rt.Thread, c *rt.GoCont) (rt.Cont, error) {
	actorID, ok := extractTargetID(argAt(c, 0))
	if !ok || vm.world == nil {
		return c.PushingNext1(t.Runtime, rt.IntValue(int64(engine.ERR_INVALID_TARGET))), nil
	}
	targetID, ok := extractTargetID(argAt(c, 1))
	if !ok {
		return c.PushingNext1(t.Runtime, rt.IntValue(int64(engine.ERR_INVALID_TARGET))), nil
	}
	resType := optString(c, 2, engine.RESOURCE_ENERGY)
	amount := optInt(c, 3, 0)
	code := vm.world.QueueIntent(vm.UserID, engine.Intent{
		Type:         engine.IntentTransfer,
		ActorID:      actorID,
		TargetID:     targetID,
		ResourceType: resType,
		Amount:       amount,
	})
	return c.PushingNext1(t.Runtime, rt.IntValue(int64(code))), nil
}

func (vm *BotVM) creepWithdraw(t *rt.Thread, c *rt.GoCont) (rt.Cont, error) {
	actorID, ok := extractTargetID(argAt(c, 0))
	if !ok || vm.world == nil {
		return c.PushingNext1(t.Runtime, rt.IntValue(int64(engine.ERR_INVALID_TARGET))), nil
	}
	targetID, ok := extractTargetID(argAt(c, 1))
	if !ok {
		return c.PushingNext1(t.Runtime, rt.IntValue(int64(engine.ERR_INVALID_TARGET))), nil
	}
	resType := optString(c, 2, engine.RESOURCE_ENERGY)
	amount := optInt(c, 3, 0)
	code := vm.world.QueueIntent(vm.UserID, engine.Intent{
		Type:         engine.IntentWithdraw,
		ActorID:      actorID,
		TargetID:     targetID,
		ResourceType: resType,
		Amount:       amount,
	})
	return c.PushingNext1(t.Runtime, rt.IntValue(int64(code))), nil
}

func (vm *BotVM) creepDrop(t *rt.Thread, c *rt.GoCont) (rt.Cont, error) {
	actorID, ok := extractTargetID(argAt(c, 0))
	if !ok || vm.world == nil {
		return c.PushingNext1(t.Runtime, rt.IntValue(int64(engine.ERR_INVALID_TARGET))), nil
	}
	resType := optString(c, 1, engine.RESOURCE_ENERGY)
	amount := optInt(c, 2, 0)
	code := vm.world.QueueIntent(vm.UserID, engine.Intent{
		Type:         engine.IntentDrop,
		ActorID:      actorID,
		ResourceType: resType,
		Amount:       amount,
	})
	return c.PushingNext1(t.Runtime, rt.IntValue(int64(code))), nil
}

func (vm *BotVM) creepSay(t *rt.Thread, c *rt.GoCont) (rt.Cont, error) {
	actorID, ok := extractTargetID(argAt(c, 0))
	if !ok || vm.world == nil {
		return c.PushingNext1(t.Runtime, rt.IntValue(int64(engine.ERR_INVALID_TARGET))), nil
	}
	msg := optString(c, 1, "")
	code := vm.world.QueueIntent(vm.UserID, engine.Intent{
		Type:    engine.IntentSay,
		ActorID: actorID,
		Message: msg,
	})
	return c.PushingNext1(t.Runtime, rt.IntValue(int64(code))), nil
}

// --- StructureSpawn, StructureTower, StructureLink methods ---

func (vm *BotVM) spawnCreep(t *rt.Thread, c *rt.GoCont) (rt.Cont, error) {
	spawnID, ok := extractTargetID(argAt(c, 0))
	if !ok || vm.world == nil {
		return c.PushingNext1(t.Runtime, rt.IntValue(int64(engine.ERR_INVALID_TARGET))), nil
	}
	bodyTbl, ok := argAt(c, 1).TryTable()
	if !ok {
		return c.PushingNext1(t.Runtime, rt.IntValue(int64(engine.ERR_INVALID_ARGS))), nil
	}
	name, err := checkString(c, 2)
	if err != nil {
		return nil, err
	}
	optsVal := argAt(c, 3)

	n := int(bodyTbl.Len())
	body := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		if s, ok := tblGetInt(bodyTbl, i).TryString(); ok {
			body = append(body, s)
		}
	}

	var dirs []int
	var memTbl *rt.Table
	if optsTbl, ok := optsVal.TryTable(); ok {
		if m, ok := tblGet(optsTbl, "memory").TryTable(); ok {
			memTbl = m
		}
		if dTbl, ok := tblGet(optsTbl, "directions").TryTable(); ok {
			dn := int(dTbl.Len())
			for i := 1; i <= dn; i++ {
				if d, ok := asInt(tblGetInt(dTbl, i)); ok {
					dirs = append(dirs, d)
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
		tblSet(creepsMem, name, rt.TableValue(memTbl))
	}

	return c.PushingNext1(t.Runtime, rt.IntValue(int64(code))), nil
}

func (vm *BotVM) structureTargetIntent(intentType engine.IntentType) rt.GoFunctionFunc {
	return func(t *rt.Thread, c *rt.GoCont) (rt.Cont, error) {
		actorID, ok := extractTargetID(argAt(c, 0))
		if !ok || vm.world == nil {
			return c.PushingNext1(t.Runtime, rt.IntValue(int64(engine.ERR_INVALID_TARGET))), nil
		}
		targetID, ok := extractTargetID(argAt(c, 1))
		if !ok {
			return c.PushingNext1(t.Runtime, rt.IntValue(int64(engine.ERR_INVALID_TARGET))), nil
		}
		code := vm.world.QueueIntent(vm.UserID, engine.Intent{
			Type:     intentType,
			ActorID:  actorID,
			TargetID: targetID,
		})
		return c.PushingNext1(t.Runtime, rt.IntValue(int64(code))), nil
	}
}

func (vm *BotVM) linkTransferEnergy(t *rt.Thread, c *rt.GoCont) (rt.Cont, error) {
	actorID, ok := extractTargetID(argAt(c, 0))
	if !ok || vm.world == nil {
		return c.PushingNext1(t.Runtime, rt.IntValue(int64(engine.ERR_INVALID_TARGET))), nil
	}
	targetID, ok := extractTargetID(argAt(c, 1))
	if !ok {
		return c.PushingNext1(t.Runtime, rt.IntValue(int64(engine.ERR_INVALID_TARGET))), nil
	}
	amount := optInt(c, 2, 0)
	code := vm.world.QueueIntent(vm.UserID, engine.Intent{
		Type:     engine.IntentLinkTransfer,
		ActorID:  actorID,
		TargetID: targetID,
		Amount:   amount,
	})
	return c.PushingNext1(t.Runtime, rt.IntValue(int64(code))), nil
}

// --- PathFinder & CostMatrix methods ---

func checkCostMatrix(c *rt.GoCont, idx int) (*engine.CostMatrix, error) {
	ud, err := checkUserData(c, idx)
	if err != nil {
		return nil, err
	}
	cm, ok := ud.Value().(*engine.CostMatrix)
	if !ok {
		return nil, fmt.Errorf("bad argument #%d (expected PathFinder.CostMatrix)", idx+1)
	}
	return cm, nil
}

func (vm *BotVM) costMatrixSet(t *rt.Thread, c *rt.GoCont) (rt.Cont, error) {
	cm, err := checkCostMatrix(c, 0)
	if err != nil {
		return nil, err
	}
	x, err := checkInt(c, 1)
	if err != nil {
		return nil, err
	}
	y, err := checkInt(c, 2)
	if err != nil {
		return nil, err
	}
	val, err := checkInt(c, 3)
	if err != nil {
		return nil, err
	}
	if val < 0 {
		val = 0
	} else if val > 255 {
		val = 255
	}
	cm.Set(x, y, uint8(val))
	return c.Next(), nil
}

func (vm *BotVM) costMatrixGet(t *rt.Thread, c *rt.GoCont) (rt.Cont, error) {
	cm, err := checkCostMatrix(c, 0)
	if err != nil {
		return nil, err
	}
	x, err := checkInt(c, 1)
	if err != nil {
		return nil, err
	}
	y, err := checkInt(c, 2)
	if err != nil {
		return nil, err
	}
	return c.PushingNext1(t.Runtime, rt.IntValue(int64(cm.Get(x, y)))), nil
}

func (vm *BotVM) costMatrixClone(t *rt.Thread, c *rt.GoCont) (rt.Cont, error) {
	cm, err := checkCostMatrix(c, 0)
	if err != nil {
		return nil, err
	}
	ud := rt.NewUserData(cm.Clone(), vm.costMatMT)
	return c.PushingNext1(t.Runtime, rt.UserDataValue(ud)), nil
}

func (vm *BotVM) pathFinderSearch(t *rt.Thread, c *rt.GoCont) (rt.Cont, error) {
	ox, oy, roomName, ok := extractPos(argAt(c, 0))
	if !ok || vm.world == nil {
		res := rt.NewTable()
		tblSet(res, "path", rt.TableValue(rt.NewTable()))
		tblSet(res, "ops", rt.IntValue(0))
		tblSet(res, "cost", rt.IntValue(0))
		tblSet(res, "incomplete", rt.BoolValue(true))
		return c.PushingNext1(t.Runtime, rt.TableValue(res)), nil
	}

	var goals []engine.Goal
	parseGoal := func(v rt.Value) {
		tbl, ok := v.TryTable()
		if !ok {
			return
		}
		gx, gy, _, okPos := extractPos(v)
		if !okPos {
			return
		}
		rng := 0
		if rv, ok := asInt(tblGet(tbl, "range")); ok {
			rng = rv
		}
		goals = append(goals, engine.Goal{X: gx, Y: gy, Range: rng})
	}

	goalArg := argAt(c, 1)
	if gTbl, ok := goalArg.TryTable(); ok {
		gLen := int(gTbl.Len())
		if gLen > 0 {
			for i := 1; i <= gLen; i++ {
				parseGoal(tblGetInt(gTbl, i))
			}
		} else {
			parseGoal(goalArg)
		}
	}

	opts := engine.PathOpts{
		MoverUser: vm.UserID,
	}
	if optsTbl, ok := argAt(c, 2).TryTable(); ok {
		if v, ok := asInt(tblGet(optsTbl, "plainCost")); ok {
			opts.PlainCost = uint8(v)
		}
		if v, ok := asInt(tblGet(optsTbl, "swampCost")); ok {
			opts.SwampCost = uint8(v)
		}
		if v, ok := asInt(tblGet(optsTbl, "maxOps")); ok {
			opts.MaxOps = v
		}
		if v := tblGet(optsTbl, "flee"); !v.IsNil() {
			opts.Flee = rt.Truth(v)
		}
		if cb := tblGet(optsTbl, "roomCallback"); cb.Type() == rt.FunctionType {
			if ret, err := rt.Call1(t, cb, rt.StringValue(roomName)); err == nil {
				if ud, ok := ret.TryUserData(); ok {
					if cm, ok := ud.Value().(*engine.CostMatrix); ok {
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

	pathTbl := rt.NewTable()
	for i, step := range pathRes.Path {
		tblSetInt(pathTbl, i+1, rt.TableValue(vm.newRoomPosition(step.X, step.Y, roomName)))
	}

	out := rt.NewTable()
	tblSet(out, "path", rt.TableValue(pathTbl))
	tblSet(out, "ops", rt.IntValue(int64(pathRes.Ops)))
	tblSet(out, "cost", rt.IntValue(int64(pathRes.Cost)))
	tblSet(out, "incomplete", rt.BoolValue(pathRes.Incomplete))
	return c.PushingNext1(t.Runtime, rt.TableValue(out)), nil
}
