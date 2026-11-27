package bots

import (
	"io"
	"io/fs"
	"testing"
	"time"

	"github.com/arnodel/golua/lib"
	"github.com/arnodel/golua/lib/base"
	"github.com/arnodel/golua/lib/mathlib"
	"github.com/arnodel/golua/lib/packagelib"
	"github.com/arnodel/golua/lib/stringlib"
	"github.com/arnodel/golua/lib/tablelib"
	"github.com/arnodel/golua/lib/utf8lib"
	rt "github.com/arnodel/golua/runtime"

	"go-screeps/internal/engine"
	botlua "go-screeps/internal/lua"
)

func newTestLuaRuntime(t *testing.T) *rt.Runtime {
	t.Helper()
	r := rt.New(io.Discard)
	cleanup := lib.LoadLibs(
		r,
		base.LibLoader,
		packagelib.LibLoader,
		tablelib.LibLoader,
		stringlib.LibLoader,
		mathlib.LibLoader,
		utf8lib.LibLoader,
	)
	t.Cleanup(func() {
		cleanup()
		r.Close(nil)
	})
	return r
}

func doLuaString(r *rt.Runtime, name, src string) error {
	clos, err := r.CompileAndLoadLuaChunk(name, []byte(src), rt.TableValue(r.GlobalEnv()))
	if err != nil {
		return err
	}
	_, err = rt.Call1(r.MainThread(), rt.FunctionValue(clos))
	return err
}

func doLuaStringRet(r *rt.Runtime, name, src string) (rt.Value, error) {
	clos, err := r.CompileAndLoadLuaChunk(name, []byte(src), rt.TableValue(r.GlobalEnv()))
	if err != nil {
		return rt.NilValue, err
	}
	return rt.Call1(r.MainThread(), rt.FunctionValue(clos))
}

func seedMockScreepsModule(t *testing.T, r *rt.Runtime) {
	t.Helper()
	src := `
		local screeps = {
			OK = 0,
			ERR = {
				NOT_OWNER = -1, NO_PATH = -2, NAME_EXISTS = -3, BUSY = -4,
				NOT_FOUND = -5, NOT_ENOUGH_ENERGY = -6, NOT_ENOUGH_RESOURCES = -6,
				NOT_ENOUGH_EXTENSIONS = -6, INVALID_TARGET = -7, FULL = -8,
				NOT_IN_RANGE = -9, INVALID_ARGS = -10, TIRED = -11, NO_BODYPART = -12,
				RCL_NOT_ENOUGH = -14, GCL_NOT_ENOUGH = -15,
			},
			DIR = {
				TOP = 1, TOP_RIGHT = 2, RIGHT = 3, BOTTOM_RIGHT = 4,
				BOTTOM = 5, BOTTOM_LEFT = 6, LEFT = 7, TOP_LEFT = 8,
			},
			TERRAIN = { PLAIN = 0, WALL = 1, SWAMP = 2 },
			FIND = {
				EXIT_TOP = 1, EXIT_RIGHT = 3, EXIT_BOTTOM = 5, EXIT_LEFT = 7, EXIT = 10,
				CREEPS = 101, MY_CREEPS = 102, HOSTILE_CREEPS = 103,
				SOURCES_ACTIVE = 104, SOURCES = 105, DROPPED_RESOURCES = 106,
				STRUCTURES = 107, MY_STRUCTURES = 108, HOSTILE_STRUCTURES = 109,
				FLAGS = 110, CONSTRUCTION_SITES = 111, MY_SPAWNS = 112,
				HOSTILE_SPAWNS = 113, MY_CONSTRUCTION_SITES = 114,
				HOSTILE_CONSTRUCTION_SITES = 115, MINERALS = 116, TOMBSTONES = 118,
			},
			PART = {
				MOVE = "move", WORK = "work", CARRY = "carry", ATTACK = "attack",
				RANGED_ATTACK = "ranged_attack", TOUGH = "tough", HEAL = "heal", CLAIM = "claim",
			},
			COST = {
				move = 50, work = 100, carry = 50, attack = 80,
				ranged_attack = 150, tough = 10, heal = 250, claim = 600,
			},
			RESOURCE = {
				ENERGY = "energy", HYDROGEN = "H", OXYGEN = "O", UTRIUM = "U",
				LEMERGIUM = "L", KEANIUM = "K", ZYNTHIUM = "Z", CATALYST = "X",
			},
			STRUCTURE = {
				SPAWN = "spawn", EXTENSION = "extension", ROAD = "road",
				WALL = "constructedWall", RAMPART = "rampart", LINK = "link",
				STORAGE = "storage", TOWER = "tower", CONTAINER = "container",
				CONTROLLER = "controller", EXTRACTOR = "extractor",
			},
			LOOK = {
				CREEPS = "creep", ENERGY = "energy", RESOURCES = "resource",
				SOURCES = "source", MINERALS = "mineral", STRUCTURES = "structure",
				FLAGS = "flag", CONSTRUCTION_SITES = "constructionSite",
				TERRAIN = "terrain", TOMBSTONES = "tombstone",
			},
			CONST = {
				MAX_CREEP_SIZE = 50, CREEP_LIFE_TIME = 1500, CREEP_CLAIM_LIFE_TIME = 600,
				CREEP_SPAWN_TIME = 3, CARRY_CAPACITY = 50, HARVEST_POWER = 2,
				REPAIR_POWER = 100, BUILD_POWER = 5, ATTACK_POWER = 30,
				UPGRADE_CONTROLLER_POWER = 1, RANGED_ATTACK_POWER = 10,
				HEAL_POWER = 12, RANGED_HEAL_POWER = 4, SOURCE_ENERGY_CAPACITY = 3000,
				SPAWN_ENERGY_CAPACITY = 300, STORAGE_CAPACITY = 1000000,
				CONTAINER_CAPACITY = 2000, LINK_CAPACITY = 800, TOWER_CAPACITY = 1000,
			},
			Game = {},
			Memory = { creeps = {}, rooms = {} },
			log = function() end,
		}
		package.loaded["screeps"] = screeps
	`
	if err := doLuaString(r, "mock_screeps", src); err != nil {
		t.Fatalf("seedMockScreepsModule: %v", err)
	}
}

func preloadTable(t *testing.T, r *rt.Runtime) *rt.Table {
	t.Helper()
	pkg, ok := r.GlobalEnv().Get(rt.StringValue("package")).TryTable()
	if !ok {
		t.Fatalf("package table missing")
	}
	preload, ok := pkg.Get(rt.StringValue("preload")).TryTable()
	if !ok {
		t.Fatalf("package.preload table missing")
	}
	return preload
}

func TestStarterFSEmbed(t *testing.T) {
	required := []string{
		"starter/main.lua",
		"starter/roles.lua",
		"starter/planner.lua",
		"starter/defense.lua",
	}
	for _, name := range required {
		data, err := fs.ReadFile(StarterFS, name)
		if err != nil {
			t.Fatalf("failed to read %s from StarterFS: %v", name, err)
		}
		if len(data) == 0 {
			t.Fatalf("%s is empty", name)
		}
	}
}

func TestStarterLuaCompileAndSmoke(t *testing.T) {
	r := newTestLuaRuntime(t)
	env := r.GlobalEnv()
	seedMockScreepsModule(t, r)

	preload := preloadTable(t, r)
	modules := []struct {
		modName string
		path    string
	}{
		{"roles", "starter/roles.lua"},
		{"planner", "starter/planner.lua"},
		{"defense", "starter/defense.lua"},
		{"colony", "starter/colony.lua"},
	}

	for _, mod := range modules {
		src, err := fs.ReadFile(StarterFS, mod.path)
		if err != nil {
			t.Fatalf("read %s: %v", mod.path, err)
		}
		fn, err := r.CompileAndLoadLuaChunk(mod.path, src, rt.TableValue(env))
		if err != nil {
			t.Fatalf("compile %s in golua: %v", mod.path, err)
		}
		preload.Set(rt.StringValue(mod.modName), rt.FunctionValue(fn))
	}

	mainSrc, err := fs.ReadFile(StarterFS, "starter/main.lua")
	if err != nil {
		t.Fatalf("read main.lua: %v", err)
	}
	mainRet, err := doLuaStringRet(r, "starter/main.lua", string(mainSrc))
	if err != nil {
		t.Fatalf("load main.lua in golua: %v", err)
	}
	mainTbl, ok := mainRet.TryTable()
	if !ok {
		t.Fatalf("expected starter/main.lua to return a table, got %s", mainRet.TypeName())
	}
	loopFn := mainTbl.Get(rt.StringValue("loop"))
	if loopFn.Type() != rt.FunctionType {
		t.Fatalf("expected returned loop to be a function, got %s", loopFn.TypeName())
	}

	// Smoke-test loop() with a mocked room (RCL 5, planner tick, visuals, spawning, towers, links, creeps).
	mockSetup := `
		local screeps = require("screeps")
		local OK = screeps.OK
		local FIND = screeps.FIND
		local STRUCTURE = screeps.STRUCTURE

		screeps.Memory = { creeps = { dead_creep = { role = "harvester" } }, rooms = {} }

		local function makePos(x, y)
			local p = { x = x, y = y }
			function p:getRangeTo(targetOrX, ty)
				local tx, y2
				if type(targetOrX) == "table" then
					local tp = targetOrX.pos or targetOrX
					tx, y2 = tp.x, tp.y
				else
					tx, y2 = targetOrX, ty
				end
				return math.max(math.abs(self.x - tx), math.abs(self.y - y2))
			end
			function p:findClosestByRange(list)
				local best, bestD = nil, 999
				for _, item in ipairs(list) do
					local d = self:getRangeTo(item)
					if d < bestD then bestD = d; best = item end
				end
				return best
			end
			return p
		end

		local function makeStore(used, cap)
			local s = {}
			function s:getUsedCapacity() return used end
			function s:getFreeCapacity() return cap - used end
			return s
		end

		local spawn = {
			id = "spawn1",
			name = "Spawn1",
			structureType = STRUCTURE.SPAWN,
			pos = makePos(25, 25),
			store = makeStore(300, 300),
			hits = 5000,
			hitsMax = 5000,
			spawning = false,
		}
		function spawn:spawnCreep(body, name, opts)
			self.lastSpawn = { body = body, name = name, opts = opts }
			return OK
		end

		local source1 = { id = "src1", pos = makePos(15, 25), energy = 3000 }
		local ctrl = { id = "ctrl1", pos = makePos(35, 25), level = 5, my = true }
		local tower = {
			id = "tower1",
			structureType = STRUCTURE.TOWER,
			pos = makePos(23, 25),
			store = makeStore(600, 1000),
			hits = 3000,
			hitsMax = 3000,
		}
		function tower:attack() return OK end
		function tower:heal() return OK end
		function tower:repair() return OK end

		local terrain = {}
		function terrain:get(x, y) return 0 end

		local visual = { circles = 0, lines = 0 }
		function visual:circle() self.circles = self.circles + 1 end
		function visual:line() self.lines = self.lines + 1 end

		local room = {
			name = "W1N1",
			controller = ctrl,
			energyAvailable = 800,
			energyCapacityAvailable = 1800,
			visual = visual,
			sitesCreated = 0,
		}
		function room:getTerrain() return terrain end
		function room:createConstructionSite(x, y, stype)
			self.sitesCreated = self.sitesCreated + 1
			return OK
		end

		local function makeCreep(id, name, role, x, y, used, cap)
			local c = {
				id = id,
				name = name,
				room = room,
				pos = makePos(x, y),
				store = makeStore(used, cap),
				hits = 100,
				hitsMax = 100,
				memory = { role = role, working = (used > 0) },
			}
			function c:moveTo() return OK end
			function c:harvest() return OK end
			function c:transfer() return OK end
			function c:withdraw() return OK end
			function c:pickup() return OK end
			function c:build() return OK end
			function c:repair() return OK end
			function c:upgradeController() return OK end
			function c:attack() return OK end
			function c:rangedAttack() return OK end
			function c:say() return OK end
			return c
		end

		local creepsList = {
			makeCreep("c1", "harv_1", "harvester", 20, 25, 0, 100),
			makeCreep("c2", "miner_1", "miner", 16, 25, 0, 50),
			makeCreep("c3", "haul_1", "hauler", 24, 25, 50, 100),
			makeCreep("c4", "upg_1", "upgrader", 34, 25, 50, 100),
			makeCreep("c5", "bld_1", "builder", 26, 25, 50, 100),
			makeCreep("c6", "def_1", "defender", 25, 23, 0, 0),
		}

		function room:find(what)
			if what == FIND.SOURCES then return { source1 } end
			if what == FIND.MY_SPAWNS then return { spawn } end
			if what == FIND.MY_STRUCTURES then return { spawn, tower } end
			if what == FIND.STRUCTURES then return { spawn, tower } end
			if what == FIND.MY_CREEPS then return creepsList end
			if what == FIND.HOSTILE_CREEPS then return {} end
			if what == FIND.CONSTRUCTION_SITES then return {} end
			if what == FIND.DROPPED_RESOURCES then return {} end
			if what == FIND.TOMBSTONES then return {} end
			return {}
		end

		local creepsMap = {}
		local byId = { src1 = source1, spawn1 = spawn, ctrl1 = ctrl, tower1 = tower }
		for _, c in ipairs(creepsList) do
			creepsMap[c.name] = c
			byId[c.id] = c
		end

		screeps.Game = {
			time = 25,
			rooms = { W1N1 = room },
			creeps = creepsMap,
			spawns = { Spawn1 = spawn },
			getObjectById = function(id) return byId[id] end,
		}
	`
	if err := doLuaString(r, "mockSetup", mockSetup); err != nil {
		t.Fatalf("mock setup failed: %v", err)
	}

	screepsVal, err := doLuaStringRet(r, "getScreeps", `return require("screeps")`)
	if err != nil {
		t.Fatalf("get screeps module: %v", err)
	}
	screepsTbl := screepsVal.AsTable()
	if _, err := rt.Call1(r.MainThread(), loopFn, screepsTbl.Get(rt.StringValue("Game")), screepsTbl.Get(rt.StringValue("Memory"))); err != nil {
		t.Fatalf("loop() smoke tick failed: %v", err)
	}

	// Verify dead_creep was cleaned up and planner created construction sites.
	verify := `
		local screeps = require("screeps")
		local Memory, Game = screeps.Memory, screeps.Game
		assert(Memory.creeps.dead_creep == nil, "dead_creep should be cleaned up")
		assert(Memory.rooms.W1N1 ~= nil and Memory.rooms.W1N1.threat ~= nil, "threat memory should be populated")
		assert(Memory.rooms.W1N1.plan ~= nil, "planner memory should be populated")
		assert(Game.rooms.W1N1.sitesCreated > 0, "planner should place construction sites at RCL 5")
		assert(Game.rooms.W1N1.visual.circles > 0, "visual overlays should be drawn")
	`
	if err := doLuaString(r, "verify", verify); err != nil {
		t.Fatalf("post-tick assertions failed: %v", err)
	}
}

func TestRaiderFSEmbed(t *testing.T) {
	required := []string{
		"raider/main.lua",
		"raider/economy.lua",
		"raider/planner.lua",
		"raider/combat.lua",
	}
	for _, name := range required {
		data, err := fs.ReadFile(RaiderFS, name)
		if err != nil {
			t.Fatalf("failed to read %s from RaiderFS: %v", name, err)
		}
		if len(data) == 0 {
			t.Fatalf("%s is empty", name)
		}
	}
}

func TestRaiderRuns200TicksCleanly(t *testing.T) {
	w := engine.NewWorld()
	w.GenerateRoom("W1N1", 42)
	w.GenerateRoom("W2N1", 99)

	if _, err := w.SpawnBot("Raider", "W1N1"); err != nil {
		t.Fatalf("SpawnBot Raider: %v", err)
	}
	if _, err := w.SpawnBot("Rival", "W2N1"); err != nil {
		t.Fatalf("SpawnBot Rival: %v", err)
	}

	runner := botlua.NewLocalRunner(w, "W1N1")
	defer runner.Close()

	if err := runner.AttachEmbeddedBot("Raider", "W1N1", RaiderFS, "raider"); err != nil {
		t.Fatalf("AttachEmbeddedBot Raider: %v", err)
	}

	botList := runner.Bots()
	if len(botList) != 1 {
		t.Fatalf("expected 1 attached bot, got %d", len(botList))
	}
	vm := botList[0]
	vm.CPULimit = 250 * time.Millisecond

	w.RLock()
	startTime := w.GameTime
	w.RUnlock()

	const totalTicks = 220
	for tick := 1; tick <= totalTicks; tick++ {
		runner.StepTick()
		if vm.LastError != "" {
			t.Fatalf("Raider Lua error at tick %d: %s", tick, vm.LastError)
		}
	}

	snap := runner.Snapshot()
	if snap.GameTime != startTime+totalTicks {
		t.Fatalf("expected GameTime=%d, got %d", startTime+totalTicks, snap.GameTime)
	}

	creeps := 0
	sites := 0
	for _, obj := range snap.Objects {
		switch obj.Type {
		case engine.TYPE_CREEP:
			if obj.User == vm.UserID {
				creeps++
			}
		case engine.TYPE_CONSTRUCTION_SITE:
			if obj.User == vm.UserID {
				sites++
			}
		}
	}

	t.Logf("Raider 220-tick summary: creeps=%d sites=%d visuals=%d", creeps, sites, len(snap.Visuals))

	if creeps == 0 {
		t.Fatalf("expected Raider to spawn creeps within %d ticks, got 0", totalTicks)
	}
	if len(snap.Visuals) == 0 {
		t.Fatalf("expected Raider to draw RoomVisual overlays, got 0")
	}
}

func TestRaiderScoutingAndCombatSmoke(t *testing.T) {
	r := newTestLuaRuntime(t)
	env := r.GlobalEnv()
	seedMockScreepsModule(t, r)

	preload := preloadTable(t, r)
	for _, mod := range []struct{ name, path string }{
		{"economy", "raider/economy.lua"},
		{"planner", "raider/planner.lua"},
		{"combat", "raider/combat.lua"},
		{"colony", "raider/colony.lua"},
	} {
		src, err := fs.ReadFile(RaiderFS, mod.path)
		if err != nil {
			t.Fatalf("read %s: %v", mod.path, err)
		}
		fn, err := r.CompileAndLoadLuaChunk(mod.path, src, rt.TableValue(env))
		if err != nil {
			t.Fatalf("compile %s: %v", mod.path, err)
		}
		preload.Set(rt.StringValue(mod.name), rt.FunctionValue(fn))
	}

	mainSrc, err := fs.ReadFile(RaiderFS, "raider/main.lua")
	if err != nil {
		t.Fatalf("read raider/main.lua: %v", err)
	}
	mainRet, err := doLuaStringRet(r, "raider/main.lua", string(mainSrc))
	if err != nil {
		t.Fatalf("load raider/main.lua: %v", err)
	}
	mainTbl, ok := mainRet.TryTable()
	if !ok {
		t.Fatalf("expected raider/main.lua to return a table, got %s", mainRet.TypeName())
	}
	loopFn := mainTbl.Get(rt.StringValue("loop"))
	if loopFn.Type() != rt.FunctionType {
		t.Fatalf("expected returned loop to be a function, got %s", loopFn.TypeName())
	}
	env.Set(rt.StringValue("__test_loop"), loopFn)

	script := `
		local screeps = require("screeps")
		local OK = screeps.OK
		local ERR = screeps.ERR
		local FIND = screeps.FIND
		local PART = screeps.PART
		local STRUCTURE = screeps.STRUCTURE

		screeps.Memory = { creeps = {}, rooms = {} }
		local function makePos(x, y, rn)
			local p = { x = x, y = y, roomName = rn }
			function p:getRangeTo(a, b)
				local tx, ty
				if type(a) == "table" then
					local tp = a.pos or a
					tx, ty = tp.x, tp.y
				else
					tx, ty = a, b
				end
				return math.max(math.abs(self.x - tx), math.abs(self.y - ty))
			end
			function p:findClosestByRange(list)
				local best, bestD = nil, 999
				for _, item in ipairs(list) do
					local d = self:getRangeTo(item)
					if d < bestD then bestD = d; best = item end
				end
				return best
			end
			return p
		end
		local function makeStore(used, cap)
			local s = {}
			function s:getUsedCapacity() return used end
			function s:getFreeCapacity() return cap - used end
			return s
		end

		local terrain = { get = function() return 0 end }
		local visual = {
			circles = 0, lines = 0, texts = 0,
			circle = function(self) self.circles = self.circles + 1 end,
			line = function(self) self.lines = self.lines + 1 end,
			text = function(self) self.texts = self.texts + 1 end,
		}

		local homeSpawn = {
			id = "sp1", name = "Spawn1", structureType = STRUCTURE.SPAWN,
			pos = makePos(25, 25, "W1N1"), store = makeStore(300, 300),
			hits = 5000, hitsMax = 5000, spawning = false,
			spawnCreep = function() return OK end,
		}
		local enemySpawn = {
			id = "esp1", name = "EnemySpawn", structureType = STRUCTURE.SPAWN,
			pos = makePos(25, 25, "W2N1"), store = makeStore(300, 300),
			hits = 5000, hitsMax = 5000,
		}
		local enemyCreep = {
			id = "ec1", name = "EnemyWorker",
			pos = makePos(22, 25, "W2N1"), hits = 100, hitsMax = 100,
			body = { { type = PART.WORK, hits = 100 }, { type = PART.MOVE, hits = 100 } },
		}

		local homeRoom = {
			name = "W1N1",
			controller = { id = "c1", pos = makePos(30, 30, "W1N1"), level = 3, my = true },
			energyAvailable = 800, energyCapacityAvailable = 800,
			visual = visual, memory = {},
			getTerrain = function() return terrain end,
			createConstructionSite = function() return OK end,
			findExitTo = function(_, tgt) if tgt == "W2N1" then return FIND.EXIT_LEFT end return ERR.NO_PATH end,
		}
		local enemyRoom = {
			name = "W2N1",
			controller = { id = "c2", pos = makePos(30, 30, "W2N1"), level = 2, my = false, owner = { username = "Rival" } },
			energyAvailable = 0, energyCapacityAvailable = 0,
			visual = visual, memory = {},
			getTerrain = function() return terrain end,
			findExitTo = function(_, tgt) if tgt == "W1N1" then return FIND.EXIT_RIGHT end return ERR.NO_PATH end,
		}

		local function makeCreep(id, name, role, room, x, y, body, hits, hitsMax)
			local c = {
				id = id, name = name, room = room, pos = makePos(x, y, room.name),
				store = makeStore(0, 50), body = body, hits = hits or 200, hitsMax = hitsMax or 200,
				memory = { role = role, homeRoom = "W1N1", targetRoom = "W2N1", state = "raid" },
				attacked = 0, ranged = 0, healed = 0, moved = 0,
			}
			function c:moveTo() self.moved = self.moved + 1; return OK end
			function c:attack() self.attacked = self.attacked + 1; return OK end
			function c:rangedAttack() self.ranged = self.ranged + 1; return OK end
			function c:heal() self.healed = self.healed + 1; return OK end
			function c:rangedHeal() self.healed = self.healed + 1; return OK end
			return c
		end

		local scout = makeCreep("s1", "R_scout_1", "scout", enemyRoom, 20, 20, { { type = PART.MOVE, hits = 100 } }, 100, 100)
		local brawler = makeCreep("b1", "R_brawler_1", "brawler", enemyRoom, 21, 25, { { type = PART.ATTACK, hits = 100 }, { type = PART.MOVE, hits = 100 } }, 200, 200)
		local skirm = makeCreep("k1", "R_skirm_1", "skirmisher", enemyRoom, 20, 25, { { type = PART.RANGED_ATTACK, hits = 100 }, { type = PART.MOVE, hits = 100 } }, 200, 200)
		local medic = makeCreep("m1", "R_medic_1", "medic", enemyRoom, 19, 25, { { type = PART.HEAL, hits = 100 }, { type = PART.MOVE, hits = 100 } }, 200, 200)
		local hurtBrawler = makeCreep("b2", "R_brawler_2", "brawler", enemyRoom, 20, 26, { { type = PART.ATTACK, hits = 100 }, { type = PART.MOVE, hits = 100 } }, 30, 200)

		function homeRoom:find(what)
			if what == FIND.MY_SPAWNS then return { homeSpawn } end
			if what == FIND.MY_STRUCTURES or what == FIND.STRUCTURES then return { homeSpawn } end
			if what == FIND.EXIT_LEFT then return { makePos(0, 25, "W1N1") } end
			return {}
		end
		function enemyRoom:find(what)
			if what == FIND.HOSTILE_SPAWNS then return { enemySpawn } end
			if what == FIND.HOSTILE_CREEPS then return { enemyCreep } end
			if what == FIND.HOSTILE_STRUCTURES then return { enemySpawn } end
			if what == FIND.MY_CREEPS then return { scout, brawler, skirm, medic, hurtBrawler } end
			if what == FIND.EXIT_RIGHT then return { makePos(49, 25, "W2N1") } end
			return {}
		end

		screeps.Game = {
			time = 40,
			rooms = { W1N1 = homeRoom, W2N1 = enemyRoom },
			creeps = {
				R_scout_1 = scout,
				R_brawler_1 = brawler,
				R_skirm_1 = skirm,
				R_medic_1 = medic,
				R_brawler_2 = hurtBrawler,
			},
			spawns = { Spawn1 = homeSpawn },
			map = {
				describeExits = function(rn)
					if rn == "W1N1" then return { ["7"] = "W2N1" } end
					if rn == "W2N1" then return { ["3"] = "W1N1" } end
					return {}
				end,
			},
		}

		__test_loop(screeps.Game, screeps.Memory)

		assert(screeps.Memory.raidTarget == "W2N1", "scout/intel should record W2N1 as Memory.raidTarget")
		assert(brawler.attacked > 0, "brawler should attack hostile creep in range")
		assert(skirm.ranged > 0, "skirmisher should rangedAttack hostile creep in range")
		assert(medic.healed > 0, "medic should heal wounded brawler")
		assert(hurtBrawler.moved > 0, "low-HP brawler (<25%) should retreat toward medic")
	`
	if err := doLuaString(r, "raiderSmoke", script); err != nil {
		t.Fatalf("raider scouting & combat smoke failed: %v", err)
	}
}
