package bots

import (
	"io/fs"
	"testing"
	"time"

	lua "github.com/yuin/gopher-lua"

	"go-screeps/internal/engine"
	botlua "go-screeps/internal/lua"
)

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
	L := lua.NewState()
	defer L.Close()

	// Seed Screeps global constants required by the bot modules.
	constants := map[string]lua.LValue{
		"OK":                      lua.LNumber(0),
		"ERR_NOT_IN_RANGE":        lua.LNumber(-9),
		"FIND_SOURCES":            lua.LNumber(105),
		"FIND_MY_CREEPS":          lua.LNumber(102),
		"FIND_HOSTILE_CREEPS":     lua.LNumber(103),
		"FIND_STRUCTURES":         lua.LNumber(107),
		"FIND_MY_STRUCTURES":      lua.LNumber(108),
		"FIND_MY_SPAWNS":          lua.LNumber(112),
		"FIND_CONSTRUCTION_SITES": lua.LNumber(111),
		"FIND_DROPPED_RESOURCES":  lua.LNumber(106),
		"FIND_TOMBSTONES":         lua.LNumber(118),
		"STRUCTURE_SPAWN":         lua.LString("spawn"),
		"STRUCTURE_EXTENSION":     lua.LString("extension"),
		"STRUCTURE_ROAD":          lua.LString("road"),
		"STRUCTURE_TOWER":         lua.LString("tower"),
		"STRUCTURE_CONTAINER":     lua.LString("container"),
		"STRUCTURE_CONTROLLER":    lua.LString("controller"),
		"STRUCTURE_STORAGE":       lua.LString("storage"),
		"STRUCTURE_LINK":          lua.LString("link"),
		"STRUCTURE_RAMPART":       lua.LString("rampart"),
		"STRUCTURE_WALL":          lua.LString("constructedWall"),
		"RESOURCE_ENERGY":         lua.LString("energy"),
		"WORK":                    lua.LString("work"),
		"CARRY":                   lua.LString("carry"),
		"MOVE":                    lua.LString("move"),
		"ATTACK":                  lua.LString("attack"),
		"RANGED_ATTACK":           lua.LString("ranged_attack"),
		"HEAL":                    lua.LString("heal"),
		"TOUGH":                   lua.LString("tough"),
		"CLAIM":                   lua.LString("claim"),
	}
	for k, v := range constants {
		L.SetGlobal(k, v)
	}

	preload := L.GetField(L.GetField(L.Get(lua.EnvironIndex), "package"), "preload")
	modules := []struct {
		modName string
		path    string
	}{
		{"roles", "starter/roles.lua"},
		{"planner", "starter/planner.lua"},
		{"defense", "starter/defense.lua"},
	}

	for _, mod := range modules {
		src, err := fs.ReadFile(StarterFS, mod.path)
		if err != nil {
			t.Fatalf("read %s: %v", mod.path, err)
		}
		fn, err := L.LoadString(string(src))
		if err != nil {
			t.Fatalf("compile %s in gopher-lua: %v", mod.path, err)
		}
		L.SetField(preload, mod.modName, fn)
	}

	mainSrc, err := fs.ReadFile(StarterFS, "starter/main.lua")
	if err != nil {
		t.Fatalf("read main.lua: %v", err)
	}
	if err := L.DoString(string(mainSrc)); err != nil {
		t.Fatalf("load main.lua in gopher-lua: %v", err)
	}

	loopFn := L.GetGlobal("loop")
	if loopFn.Type() != lua.LTFunction {
		t.Fatalf("expected global loop to be a function, got %s", loopFn.Type())
	}

	// Smoke-test loop() with a mocked room (RCL 5, planner tick, visuals, spawning, towers, links, creeps).
	mockSetup := `
		Memory = { creeps = { dead_creep = { role = "harvester" } }, rooms = {} }

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
			structureType = STRUCTURE_SPAWN,
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
			structureType = STRUCTURE_TOWER,
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
			if what == FIND_SOURCES then return { source1 } end
			if what == FIND_MY_SPAWNS then return { spawn } end
			if what == FIND_MY_STRUCTURES then return { spawn, tower } end
			if what == FIND_STRUCTURES then return { spawn, tower } end
			if what == FIND_MY_CREEPS then return creepsList end
			if what == FIND_HOSTILE_CREEPS then return {} end
			if what == FIND_CONSTRUCTION_SITES then return {} end
			if what == FIND_DROPPED_RESOURCES then return {} end
			if what == FIND_TOMBSTONES then return {} end
			return {}
		end

		local creepsMap = {}
		local byId = { src1 = source1, spawn1 = spawn, ctrl1 = ctrl, tower1 = tower }
		for _, c in ipairs(creepsList) do
			creepsMap[c.name] = c
			byId[c.id] = c
		end

		Game = {
			time = 25,
			rooms = { W1N1 = room },
			creeps = creepsMap,
			spawns = { Spawn1 = spawn },
			getObjectById = function(id) return byId[id] end,
		}
	`
	if err := L.DoString(mockSetup); err != nil {
		t.Fatalf("mock setup failed: %v", err)
	}

	if err := L.CallByParam(lua.P{
		Fn:      loopFn,
		NRet:    0,
		Protect: true,
	}); err != nil {
		t.Fatalf("loop() smoke tick failed: %v", err)
	}

	// Verify dead_creep was cleaned up and planner created construction sites.
	verify := `
		assert(Memory.creeps.dead_creep == nil, "dead_creep should be cleaned up")
		assert(Memory.rooms.W1N1 ~= nil and Memory.rooms.W1N1.threat ~= nil, "threat memory should be populated")
		assert(Memory.rooms.W1N1.plan ~= nil, "planner memory should be populated")
		assert(Game.rooms.W1N1.sitesCreated > 0, "planner should place construction sites at RCL 5")
		assert(Game.rooms.W1N1.visual.circles > 0, "visual overlays should be drawn")
	`
	if err := L.DoString(verify); err != nil {
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
	L := lua.NewState()
	defer L.Close()

	constants := map[string]lua.LValue{
		"OK":                              lua.LNumber(0),
		"ERR_NO_PATH":                     lua.LNumber(-2),
		"ERR_NOT_IN_RANGE":                lua.LNumber(-9),
		"FIND_EXIT_TOP":                   lua.LNumber(1),
		"FIND_EXIT_RIGHT":                 lua.LNumber(3),
		"FIND_EXIT_BOTTOM":                lua.LNumber(5),
		"FIND_EXIT_LEFT":                  lua.LNumber(7),
		"FIND_SOURCES":                    lua.LNumber(105),
		"FIND_SOURCES_ACTIVE":             lua.LNumber(104),
		"FIND_MY_CREEPS":                  lua.LNumber(102),
		"FIND_HOSTILE_CREEPS":             lua.LNumber(103),
		"FIND_STRUCTURES":                 lua.LNumber(107),
		"FIND_MY_STRUCTURES":              lua.LNumber(108),
		"FIND_HOSTILE_STRUCTURES":         lua.LNumber(109),
		"FIND_MY_SPAWNS":                  lua.LNumber(112),
		"FIND_HOSTILE_SPAWNS":             lua.LNumber(113),
		"FIND_CONSTRUCTION_SITES":         lua.LNumber(111),
		"FIND_MY_CONSTRUCTION_SITES":      lua.LNumber(114),
		"FIND_HOSTILE_CONSTRUCTION_SITES": lua.LNumber(115),
		"FIND_DROPPED_RESOURCES":          lua.LNumber(106),
		"FIND_TOMBSTONES":                 lua.LNumber(118),
		"TERRAIN_MASK_WALL":               lua.LNumber(1),
		"STRUCTURE_SPAWN":                 lua.LString("spawn"),
		"STRUCTURE_EXTENSION":             lua.LString("extension"),
		"STRUCTURE_ROAD":                  lua.LString("road"),
		"STRUCTURE_TOWER":                 lua.LString("tower"),
		"STRUCTURE_CONTAINER":             lua.LString("container"),
		"STRUCTURE_CONTROLLER":            lua.LString("controller"),
		"STRUCTURE_STORAGE":               lua.LString("storage"),
		"STRUCTURE_RAMPART":               lua.LString("rampart"),
		"STRUCTURE_WALL":                  lua.LString("constructedWall"),
		"RESOURCE_ENERGY":                 lua.LString("energy"),
		"WORK":                            lua.LString("work"),
		"CARRY":                           lua.LString("carry"),
		"MOVE":                            lua.LString("move"),
		"ATTACK":                          lua.LString("attack"),
		"RANGED_ATTACK":                   lua.LString("ranged_attack"),
		"HEAL":                            lua.LString("heal"),
		"TOUGH":                           lua.LString("tough"),
		"CLAIM":                           lua.LString("claim"),
	}
	for k, v := range constants {
		L.SetGlobal(k, v)
	}

	preload := L.GetField(L.GetField(L.Get(lua.EnvironIndex), "package"), "preload")
	for _, mod := range []struct{ name, path string }{
		{"economy", "raider/economy.lua"},
		{"planner", "raider/planner.lua"},
		{"combat", "raider/combat.lua"},
	} {
		src, err := fs.ReadFile(RaiderFS, mod.path)
		if err != nil {
			t.Fatalf("read %s: %v", mod.path, err)
		}
		fn, err := L.LoadString(string(src))
		if err != nil {
			t.Fatalf("compile %s: %v", mod.path, err)
		}
		L.SetField(preload, mod.name, fn)
	}

	mainSrc, err := fs.ReadFile(RaiderFS, "raider/main.lua")
	if err != nil {
		t.Fatalf("read raider/main.lua: %v", err)
	}
	if err := L.DoString(string(mainSrc)); err != nil {
		t.Fatalf("load raider/main.lua: %v", err)
	}

	script := `
		Memory = { creeps = {}, rooms = {} }
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
			id = "sp1", name = "Spawn1", structureType = STRUCTURE_SPAWN,
			pos = makePos(25, 25, "W1N1"), store = makeStore(300, 300),
			hits = 5000, hitsMax = 5000, spawning = false,
			spawnCreep = function() return OK end,
		}
		local enemySpawn = {
			id = "esp1", name = "EnemySpawn", structureType = STRUCTURE_SPAWN,
			pos = makePos(25, 25, "W2N1"), store = makeStore(300, 300),
			hits = 5000, hitsMax = 5000,
		}
		local enemyCreep = {
			id = "ec1", name = "EnemyWorker",
			pos = makePos(22, 25, "W2N1"), hits = 100, hitsMax = 100,
			body = { { type = WORK, hits = 100 }, { type = MOVE, hits = 100 } },
		}

		local homeRoom = {
			name = "W1N1",
			controller = { id = "c1", pos = makePos(30, 30, "W1N1"), level = 3, my = true },
			energyAvailable = 800, energyCapacityAvailable = 800,
			visual = visual, memory = {},
			getTerrain = function() return terrain end,
			createConstructionSite = function() return OK end,
			findExitTo = function(_, tgt) if tgt == "W2N1" then return FIND_EXIT_LEFT end return ERR_NO_PATH end,
		}
		local enemyRoom = {
			name = "W2N1",
			controller = { id = "c2", pos = makePos(30, 30, "W2N1"), level = 2, my = false, owner = { username = "Rival" } },
			energyAvailable = 0, energyCapacityAvailable = 0,
			visual = visual, memory = {},
			getTerrain = function() return terrain end,
			findExitTo = function(_, tgt) if tgt == "W1N1" then return FIND_EXIT_RIGHT end return ERR_NO_PATH end,
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

		local scout = makeCreep("s1", "R_scout_1", "scout", enemyRoom, 20, 20, { { type = MOVE, hits = 100 } }, 100, 100)
		local brawler = makeCreep("b1", "R_brawler_1", "brawler", enemyRoom, 21, 25, { { type = ATTACK, hits = 100 }, { type = MOVE, hits = 100 } }, 200, 200)
		local skirm = makeCreep("k1", "R_skirm_1", "skirmisher", enemyRoom, 20, 25, { { type = RANGED_ATTACK, hits = 100 }, { type = MOVE, hits = 100 } }, 200, 200)
		local medic = makeCreep("m1", "R_medic_1", "medic", enemyRoom, 19, 25, { { type = HEAL, hits = 100 }, { type = MOVE, hits = 100 } }, 200, 200)
		local hurtBrawler = makeCreep("b2", "R_brawler_2", "brawler", enemyRoom, 20, 26, { { type = ATTACK, hits = 100 }, { type = MOVE, hits = 100 } }, 30, 200)

		function homeRoom:find(what)
			if what == FIND_MY_SPAWNS then return { homeSpawn } end
			if what == FIND_MY_STRUCTURES or what == FIND_STRUCTURES then return { homeSpawn } end
			if what == FIND_EXIT_LEFT then return { makePos(0, 25, "W1N1") } end
			return {}
		end
		function enemyRoom:find(what)
			if what == FIND_HOSTILE_SPAWNS then return { enemySpawn } end
			if what == FIND_HOSTILE_CREEPS then return { enemyCreep } end
			if what == FIND_HOSTILE_STRUCTURES then return { enemySpawn } end
			if what == FIND_MY_CREEPS then return { scout, brawler, skirm, medic, hurtBrawler } end
			if what == FIND_EXIT_RIGHT then return { makePos(49, 25, "W2N1") } end
			return {}
		end

		Game = {
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

		loop()

		assert(Memory.raidTarget == "W2N1", "scout/intel should record W2N1 as Memory.raidTarget")
		assert(brawler.attacked > 0, "brawler should attack hostile creep in range")
		assert(skirm.ranged > 0, "skirmisher should rangedAttack hostile creep in range")
		assert(medic.healed > 0, "medic should heal wounded brawler")
		assert(hurtBrawler.moved > 0, "low-HP brawler (<25%) should retreat toward medic")
	`
	if err := L.DoString(script); err != nil {
		t.Fatalf("raider scouting & combat smoke failed: %v", err)
	}
}
