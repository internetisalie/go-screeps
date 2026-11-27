package lua

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"go-screeps/bots"
	"go-screeps/internal/engine"
)

func TestConstantsAndAPI(t *testing.T) {
	w := engine.NewWorld()
	w.GenerateRoom("W1N1", 101)
	if _, err := w.SpawnBot("Alice", "W1N1"); err != nil {
		t.Fatalf("SpawnBot: %v", err)
	}
	uid := w.EnsureUser("Alice")

	fsys := fstest.MapFS{
		"bot/main.lua": &fstest.MapFile{
			Data: []byte(`
local screeps <const> = require("screeps")
local OK <const> = screeps.OK
local ERR <const> = screeps.ERR
local FIND <const> = screeps.FIND
local STRUCTURE <const> = screeps.STRUCTURE
local RESOURCE <const> = screeps.RESOURCE
local PART <const> = screeps.PART
local DIR <const> = screeps.DIR
local TERRAIN <const> = screeps.TERRAIN
local COST <const> = screeps.COST
local LOOK <const> = screeps.LOOK
local CONST <const> = screeps.CONST
local PathFinder <const> = screeps.PathFinder
local RoomPosition <const> = screeps.RoomPosition

local function loop(Game, Memory)
    assert(Game == screeps.Game, "Game arg must match screeps.Game")
    assert(Memory == screeps.Memory, "Memory arg must match screeps.Memory")
    assert(OK == 0, "OK")
    assert(ERR.NOT_IN_RANGE == -9, "ERR.NOT_IN_RANGE")
    assert(FIND.SOURCES == 105, "FIND.SOURCES")
    assert(STRUCTURE.SPAWN == "spawn", "STRUCTURE.SPAWN")
    assert(RESOURCE.ENERGY == "energy", "RESOURCE.ENERGY")
    assert(PART.WORK == "work" and PART.CARRY == "carry" and PART.MOVE == "move", "bodyparts")
    assert(DIR.TOP == 1 and DIR.TOP_LEFT == 8, "directions")
    assert(TERRAIN.WALL == 1 and TERRAIN.SWAMP == 2, "terrain")
    assert(COST[PART.WORK] == 100, "COST")
    assert(CONST.MAX_CREEP_SIZE == 50, "CONST.MAX_CREEP_SIZE")

    local rp = RoomPosition(10, 20, "W1N1")
    assert(rp.x == 10 and rp.y == 20 and rp.roomName == "W1N1", "RoomPosition")

    local room = Game.rooms["W1N1"]
    assert(room ~= nil, "room W1N1 missing")
    assert(room.name == "W1N1", "room name")
    assert(room.energyAvailable == 300, "energyAvailable=" .. tostring(room.energyAvailable))
    assert(room.energyCapacityAvailable == 300, "energyCap=" .. tostring(room.energyCapacityAvailable))
    assert(room.controller ~= nil and room.controller.level == 1, "controller")

    local spawn = Game.spawns["Spawn1"]
    assert(spawn ~= nil, "Spawn1 missing")
    assert(spawn.my == true, "spawn.my")
    assert(spawn.store[RESOURCE.ENERGY] == 300, "store index")
    assert(spawn.store:getUsedCapacity(RESOURCE.ENERGY) == 300, "store getUsedCapacity")
    assert(spawn.store:getFreeCapacity(RESOURCE.ENERGY) == 0, "store getFreeCapacity")
    assert(spawn.store:getCapacity(RESOURCE.ENERGY) == 300, "store getCapacity")

    local byId = Game.getObjectById(spawn.id)
    assert(byId ~= nil and byId.id == spawn.id, "getObjectById")

    local sources = room:find(FIND.SOURCES)
    assert(#sources == 2, "expected 2 sources, got " .. tostring(#sources))
    local closest = spawn.pos:findClosestByRange(sources)
    assert(closest ~= nil, "findClosestByRange")
    assert(spawn.pos:getRangeTo(closest) >= 1, "getRangeTo")
    assert(spawn.pos:isEqualTo(spawn.pos.x, spawn.pos.y), "isEqualTo")
    assert(spawn.pos:isNearTo(spawn.pos.x + 1, spawn.pos.y), "isNearTo")
    assert(spawn.pos:inRangeTo(spawn.pos.x + 2, spawn.pos.y, 2), "inRangeTo")

    local terrain = room:getTerrain()
    assert(terrain:get(0, 0) == TERRAIN.WALL, "border wall")
    assert(terrain:get(spawn.pos.x, spawn.pos.y) == TERRAIN.PLAIN, "spawn plain")

    local look = room:lookForAt(LOOK.STRUCTURES, spawn.pos.x, spawn.pos.y)
    assert(#look == 1 and look[1].id == spawn.id, "lookForAt")

    local cm = PathFinder.CostMatrix()
    cm:set(10, 10, 42)
    assert(cm:get(10, 10) == 42, "CostMatrix get/set")
    local cm2 = cm:clone()
    assert(cm2:get(10, 10) == 42, "CostMatrix clone")

    local pfRes = PathFinder.search(spawn.pos, { pos = sources[1].pos, range = 1 }, {
        roomCallback = function(rn) return cm end
    })
    assert(pfRes.incomplete == false and #pfRes.path > 0, "PathFinder.search")

    room.visual:circle(spawn.pos.x, spawn.pos.y, { radius = 0.5, fill = "#00ff00" })
    room.visual:line(spawn.pos.x, spawn.pos.y, sources[1].pos.x, sources[1].pos.y, { color = "#ffffff", width = 0.1 })
    room.visual:rect(2, 2, 3, 3, { fill = "#0000ff" })
    room.visual:poly({ {1, 1}, {2, 2}, {3, 1} }, { stroke = "#ff0000" })
    room.visual:text("HUD", 2, 4, { color = "#ffffff" })

    assert(Game.cpu.getUsed() >= 0, "cpu.getUsed")
    screeps.log("api_ok")
end

return { loop = loop }
`),
		},
	}

	vm := NewBotVM("Alice", uid)
	defer vm.Close()
	if err := vm.LoadFS(fsys, "bot"); err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	if err := vm.RunTick(w); err != nil {
		t.Fatalf("RunTick: %v", err)
	}
	w.Step()

	logs := vm.Logs()
	if len(logs) == 0 || !strings.Contains(logs[len(logs)-1], "api_ok") {
		t.Fatalf("expected api_ok log, got %v", logs)
	}
	snap := w.Snapshot("W1N1")
	if len(snap.Visuals) != 5 {
		t.Fatalf("expected 5 visuals, got %d", len(snap.Visuals))
	}
}

func TestMemoryPersistence(t *testing.T) {
	w := engine.NewWorld()
	w.GenerateRoom("W2N2", 7)
	if _, err := w.SpawnBot("Bob", "W2N2"); err != nil {
		t.Fatalf("SpawnBot: %v", err)
	}
	uid := w.EnsureUser("Bob")

	fsys := fstest.MapFS{
		"bot/main.lua": &fstest.MapFile{
			Data: []byte(`
local screeps <const> = require("screeps")
local PART <const> = screeps.PART
local OK <const> = screeps.OK

return function(Game, Memory)
    Memory.ticks = (Memory.ticks or 0) + 1
    local spawn = Game.spawns["Spawn1"]
    if Game.time == 1 and spawn then
        local rc = spawn:spawnCreep({ PART.WORK, PART.CARRY, PART.MOVE }, "worker1", {
            memory = { role = "harvester", count = 10 }
        })
        assert(rc == OK, "spawnCreep failed: " .. tostring(rc))
    end
    local c = Game.creeps["worker1"]
    if c then
        assert(c.memory.role == "harvester", "creep memory role")
        c.memory.count = (c.memory.count or 0) + 1
    end
end
`),
		},
	}

	vm := NewBotVM("Bob", uid)
	defer vm.Close()
	if err := vm.LoadFS(fsys, "bot"); err != nil {
		t.Fatalf("LoadFS: %v", err)
	}

	for i := 0; i < 15; i++ {
		if err := vm.RunTick(w); err != nil {
			t.Fatalf("tick %d: %v", i+1, err)
		}
		w.Step()
	}

	// Verify Memory.ticks == 15 and Memory.creeps.worker1.count > 10.
	if err := vm.DoString(`
		local Memory = require("screeps").Memory
		assert(Memory.ticks == 15, "ticks=" .. tostring(Memory.ticks))
		assert(Memory.creeps.worker1 ~= nil, "worker1 memory missing")
		assert(Memory.creeps.worker1.role == "harvester", "role mismatch")
		assert(Memory.creeps.worker1.count > 10, "count did not increment: " .. tostring(Memory.creeps.worker1.count))
	`); err != nil {
		t.Fatalf("memory assertion failed: %v", err)
	}
}

func TestZeroPlatformGlobals(t *testing.T) {
	w := engine.NewWorld()
	w.GenerateRoom("W1N1", 101)
	_, _ = w.SpawnBot("Pure", "W1N1")
	uid := w.EnsureUser("Pure")

	fsys := fstest.MapFS{
		"bot/main.lua": &fstest.MapFile{
			Data: []byte(`
local forbidden = {
    "Game", "Memory", "OK", "ERR_NOT_IN_RANGE", "FIND_MY_CREEPS",
    "STRUCTURE_SPAWN", "RESOURCE_ENERGY", "MOVE", "WORK", "BODYPART_COST",
    "PathFinder", "RoomPosition", "console", "loop",
}
for _, name in ipairs(forbidden) do
    assert(_G[name] == nil, "global " .. name .. " must be nil in _G")
end

local screeps <const> = require("screeps")
assert(type(screeps) == "table", "require('screeps') must return a table")

return function(Game, Memory)
    for _, name in ipairs(forbidden) do
        assert(_G[name] == nil, "global " .. name .. " must remain nil during tick")
    end
    assert(Game.time >= 1, "Game.time passed to loop")
    Memory.checked = true
end
`),
		},
	}

	vm := NewBotVM("Pure", uid)
	defer vm.Close()
	if err := vm.LoadFS(fsys, "bot"); err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	if err := vm.RunTick(w); err != nil {
		t.Fatalf("RunTick: %v", err)
	}
	mem := vm.ExportMemory()
	if mem["checked"] != true {
		t.Fatalf("expected Memory.checked == true, got %#v", mem)
	}
}

func TestRequireSandboxing(t *testing.T) {
	fsys := fstest.MapFS{
		"bot/main.lua": &fstest.MapFile{
			Data: []byte(`
assert(io == nil, "io must be nil")
assert(os == nil, "os must be nil")
assert(debug == nil, "debug must be nil")
assert(dofile == nil, "dofile must be nil")
assert(loadfile == nil, "loadfile must be nil")

local h1 = require("helper")
local h2 = require("helper")
assert(h1 == h2, "require must cache in package.loaded")
assert(h1.value == 123, "helper value")

local ok1 = pcall(require, "../secret")
assert(not ok1, "path traversal must fail")

local ok2 = pcall(require, "/etc/passwd")
assert(not ok2, "absolute path must fail")

return { loop = function() end }
`),
		},
		"bot/helper.lua": &fstest.MapFile{
			Data: []byte(`return { value = 123 }`),
		},
	}

	vm := NewBotVM("SandboxTest", "u1")
	defer vm.Close()
	if err := vm.LoadFS(fsys, "bot"); err != nil {
		t.Fatalf("LoadFS failed: %v", err)
	}
}

func TestCPUTimeout(t *testing.T) {
	w := engine.NewWorld()
	w.GenerateRoom("W3N3", 3)
	_, _ = w.SpawnBot("Looper", "W3N3")
	uid := w.EnsureUser("Looper")

	fsys := fstest.MapFS{
		"bot/main.lua": &fstest.MapFile{
			Data: []byte(`
return function()
    while true do end
end
`),
		},
	}

	vm := NewBotVM("Looper", uid)
	defer vm.Close()
	vm.CPULimit = 25 * time.Millisecond
	if err := vm.LoadFS(fsys, "bot"); err != nil {
		t.Fatalf("LoadFS: %v", err)
	}

	start := time.Now()
	err := vm.RunTick(w)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatalf("expected CPU timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("expected context deadline exceeded, got: %v", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("timeout took too long: %v", elapsed)
	}
	if vm.LastError == "" {
		t.Fatalf("expected LastError to be populated")
	}
}

func TestDiskHotReload(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.lua")
	modPath := filepath.Join(dir, "greeting.lua")

	if err := os.WriteFile(modPath, []byte(`return "v1"`), 0o644); err != nil {
		t.Fatalf("write greeting.lua: %v", err)
	}
	if err := os.WriteFile(mainPath, []byte(`
local screeps <const> = require("screeps")
local g = require("greeting")
return {
    loop = function(_, Memory)
        assert(Memory == screeps.Memory, "Memory preserved across reload")
        Memory.persisted = Memory.persisted or 42
        print("ver=" .. g .. " mem=" .. tostring(Memory.persisted))
    end
}
`), 0o644); err != nil {
		t.Fatalf("write main.lua: %v", err)
	}

	w := engine.NewWorld()
	w.GenerateRoom("W4N4", 4)
	_, _ = w.SpawnBot("Hot", "W4N4")
	uid := w.EnsureUser("Hot")

	vm := NewBotVM("Hot", uid)
	defer vm.Close()
	if err := vm.LoadDir(dir); err != nil {
		t.Fatalf("LoadDir: %v", err)
	}

	if err := vm.RunTick(w); err != nil {
		t.Fatalf("tick 1: %v", err)
	}
	logs1 := vm.Logs()
	if len(logs1) == 0 || !strings.Contains(logs1[len(logs1)-1], "ver=v1 mem=42") {
		t.Fatalf("expected v1 log, got %v", logs1)
	}

	// Update greeting.lua and advance mtime explicitly to avoid filesystem timestamp granularity issues.
	if err := os.WriteFile(modPath, []byte(`return "v2"`), 0o644); err != nil {
		t.Fatalf("rewrite greeting.lua: %v", err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(modPath, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	if err := vm.RunTick(w); err != nil {
		t.Fatalf("tick 2: %v", err)
	}
	logs2 := vm.Logs()
	if len(logs2) == 0 || !strings.Contains(logs2[len(logs2)-1], "ver=v2 mem=42") {
		t.Fatalf("expected v2 log with preserved Memory, got %v", logs2)
	}
}

func TestLocalRunnerStartPauseSwitch(t *testing.T) {
	w := engine.NewWorld()
	w.GenerateRoom("W5N5", 5)
	w.GenerateRoom("W5N6", 6)

	runner := NewLocalRunner(w, "W5N5")
	defer runner.Close()

	if err := runner.AddEmbeddedBot("Starter", "W5N5", bots.StarterFS, "starter"); err != nil {
		t.Fatalf("AddEmbeddedBot: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var updates atomic.Int32
	runner.Start(ctx, 5*time.Millisecond, func() {
		updates.Add(1)
	})

	deadline := time.Now().Add(2 * time.Second)
	for updates.Load() < 5 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if updates.Load() < 5 {
		t.Fatalf("expected >= 5 ticks from Start loop, got %d", updates.Load())
	}

	runner.SetPaused(true)
	if !runner.Paused() {
		t.Fatalf("expected Paused() == true")
	}
	time.Sleep(15 * time.Millisecond)
	pausedCount := updates.Load()
	time.Sleep(25 * time.Millisecond)
	if updates.Load() != pausedCount {
		t.Fatalf("ticks advanced while paused: %d -> %d", pausedCount, updates.Load())
	}

	runner.SetTickPeriod(10 * time.Millisecond)
	runner.SwitchRoom("W5N6")
	snap := runner.Snapshot()
	if snap.Room != "W5N6" {
		t.Fatalf("expected switched room W5N6, got %q", snap.Room)
	}
	cancel()
}

func TestStarterBotSoak600Ticks(t *testing.T) {
	w := engine.NewWorld()
	w.GenerateRoom("W9N7", 42)

	runner := NewLocalRunner(w, "W9N7")
	defer runner.Close()

	if err := runner.AddEmbeddedBot("Starter", "W9N7", bots.StarterFS, "starter"); err != nil {
		t.Fatalf("AddEmbeddedBot: %v", err)
	}

	botList := runner.Bots()
	if len(botList) != 1 {
		t.Fatalf("expected 1 bot, got %d", len(botList))
	}
	vm := botList[0]
	// Allow headroom for Go -race instrumentation on pure-Go gopher-lua BFS ticks.
	vm.CPULimit = 250 * time.Millisecond

	maxCreeps := 0
	harvested := false
	sitesSeen := 0
	visualsSeen := 0

	for tick := 1; tick <= 600; tick++ {
		runner.StepTick()
		if vm.LastError != "" {
			t.Fatalf("Lua runtime error at tick %d: %s", tick, vm.LastError)
		}

		snap := runner.Snapshot()
		if len(snap.Visuals) > visualsSeen {
			visualsSeen = len(snap.Visuals)
		}
		creeps := 0
		sites := 0
		for _, obj := range snap.Objects {
			switch obj.Type {
			case engine.TYPE_CREEP:
				creeps++
			case engine.TYPE_SOURCE:
				if obj.Energy < engine.SOURCE_ENERGY_CAPACITY {
					harvested = true
				}
			case engine.TYPE_CONSTRUCTION_SITE:
				sites++
			}
		}
		if creeps > maxCreeps {
			maxCreeps = creeps
		}
		if sites > sitesSeen {
			sitesSeen = sites
		}
	}

	// Inspect final room state.
	room := w.GetRoom("W9N7")
	w.RLock()
	ctrl := room.FindController()
	ctrlLevel := 0
	ctrlProgress := 0
	builtStructures := 0
	if ctrl != nil {
		ctrlLevel = ctrl.Level
		ctrlProgress = ctrl.Progress
	}
	for _, e := range room.Entities {
		if e.IsStructure() && e.Type != engine.STRUCTURE_SPAWN && e.Type != engine.STRUCTURE_CONTROLLER {
			builtStructures++
		}
	}
	w.RUnlock()

	t.Logf("600-tick soak summary: maxCreeps=%d, harvested=%v, RCL=%d, progress=%d, maxSites=%d, builtStructures=%d, maxVisuals=%d, lastCPU=%v",
		maxCreeps, harvested, ctrlLevel, ctrlProgress, sitesSeen, builtStructures, visualsSeen, vm.LastCPU)

	if maxCreeps == 0 {
		t.Fatalf("expected creeps to be spawned over 600 ticks")
	}
	if !harvested {
		t.Fatalf("expected energy to be harvested from sources")
	}
	if ctrlLevel < 2 && ctrlProgress == 0 {
		t.Fatalf("expected controller progress > 0 or RCL >= 2, got RCL=%d progress=%d", ctrlLevel, ctrlProgress)
	}
	if sitesSeen == 0 && builtStructures == 0 {
		t.Fatalf("expected construction sites to be placed (RCL=%d, maxSites=%d, built=%d)", ctrlLevel, sitesSeen, builtStructures)
	}
	if visualsSeen == 0 {
		t.Fatalf("expected RoomVisuals to be emitted")
	}
}

func TestMigrateDBJSONW9N7(t *testing.T) {
	const dbPath = "/home/mini/Documents/src/screeps/server/db.json"
	if _, err := os.Stat(dbPath); err != nil {
		t.Skipf("skipping live db.json migration test (%s not present: %v)", dbPath, err)
	}

	w := engine.NewWorld()
	if err := w.ImportFullDBJSON(dbPath); err != nil {
		t.Fatalf("ImportFullDBJSON: %v", err)
	}

	w.RLock()
	startTime := w.GameTime
	w.RUnlock()
	if startTime <= 1000 {
		t.Fatalf("expected migrated GameTime > 1000, got %d", startTime)
	}

	ownerID, ownerName := w.RoomOwner("W9N7")
	if ownerID == "" || ownerName != "Hivemind" {
		t.Fatalf("expected W9N7 owner Hivemind, got id=%q name=%q", ownerID, ownerName)
	}

	runner := NewLocalRunner(w, "W9N7")
	defer runner.Close()

	if err := runner.AttachEmbeddedBot(ownerName, "W9N7", bots.StarterFS, "starter"); err != nil {
		t.Fatalf("AttachEmbeddedBot: %v", err)
	}

	botList := runner.Bots()
	if len(botList) != 1 {
		t.Fatalf("expected 1 attached bot, got %d", len(botList))
	}
	vm := botList[0]
	if vm.UserID != ownerID {
		t.Fatalf("expected attached bot UserID %q, got %q", ownerID, vm.UserID)
	}
	vm.CPULimit = 250 * time.Millisecond

	for i := 1; i <= 50; i++ {
		runner.StepTick()
		if vm.LastError != "" {
			t.Fatalf("Lua runtime error at migrated tick %d: %s", i, vm.LastError)
		}
	}

	snap := runner.Snapshot()
	if snap.GameTime != startTime+50 {
		t.Fatalf("expected GameTime=%d after 50 ticks, got %d", startTime+50, snap.GameTime)
	}

	hasSpawn1 := false
	extensions := 0
	creeps := 0
	for _, obj := range snap.Objects {
		switch obj.Type {
		case engine.STRUCTURE_SPAWN:
			if obj.Name == "Spawn1" && obj.User == ownerID {
				hasSpawn1 = true
			}
		case engine.STRUCTURE_EXTENSION:
			if obj.User == ownerID {
				extensions++
			}
		case engine.TYPE_CREEP:
			if obj.User == ownerID {
				creeps++
			}
		}
	}

	t.Logf("W9N7 migration summary: startTick=%d endTick=%d owner=%s(%s) Spawn1=%v extensions=%d creeps=%d visuals=%d",
		startTime, snap.GameTime, ownerName, ownerID, hasSpawn1, extensions, creeps, len(snap.Visuals))

	if !hasSpawn1 {
		t.Fatalf("expected W9N7 to retain Spawn1 owned by %s", ownerName)
	}
	if extensions == 0 {
		t.Fatalf("expected W9N7 to retain extensions, got 0")
	}
	if creeps == 0 {
		t.Fatalf("expected W9N7 to retain creeps, got 0")
	}
}

func TestSandboxHardeningAdversarial(t *testing.T) {
	vm := NewBotVM("Adversary", "u_adv")
	defer vm.Close()
	vm.CPULimit = 25 * time.Millisecond

	// C3: collectgarbage and unsafe package fields must be nil.
	if err := vm.DoString(`
		assert(collectgarbage == nil, "collectgarbage must be nil")
		assert(package.loadlib == nil, "package.loadlib must be nil")
		assert(package.searchpath == nil, "package.searchpath must be nil")
		assert(package.searchers == nil, "package.searchers must be nil")
		assert(package.preload == nil, "package.preload must be nil")
		assert(package.path == nil, "package.path must be nil")
		assert(package.cpath == nil, "package.cpath must be nil")
	`); err != nil {
		t.Fatalf("sandbox globals check failed: %v", err)
	}

	// C1: catastrophic string.find backtracking must terminate promptly via CPU limit (no uint64 underflow hang).
	start := time.Now()
	if err := vm.DoString(`string.find(string.rep("a", 35), "a*a*a*a*a*a*a*a*a*b")`); err == nil {
		t.Fatalf("expected pattern backtracking to hit CPU limit")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("pattern backtracking took too long (%v)", time.Since(start))
	}

	// C1: string.rep OOM bomb must be caught by HardLimits.Memory.
	if err := vm.DoString(`local s = string.rep("A", 100000000)`); err == nil {
		t.Fatalf("expected string.rep OOM bomb to fail under memory limit")
	}

	// C2: 60-level DAG in Memory must terminate quickly under ExportMemory node budget.
	if err := vm.DoString(`
		local Memory = require("screeps").Memory
		local node = { 1 }
		for i = 1, 60 do
			node = { a = node, b = node }
		end
		Memory.dag = node
	`); err != nil {
		t.Fatalf("build DAG failed: %v", err)
	}
	dagStart := time.Now()
	_ = vm.ExportMemory()
	if time.Since(dagStart) > 2*time.Second {
		t.Fatalf("ExportMemory DAG took too long (%v)", time.Since(dagStart))
	}

	// H1 + M3: Fog-of-war visibility, dryRun, and findClosestByRange(FIND_*).
	w := engine.NewWorld()
	w.GenerateRoom("W1N1", 101)
	w.GenerateRoom("W1N2", 102)
	s1, err := w.SpawnBot("Alice", "W1N1")
	if err != nil {
		t.Fatalf("SpawnBot Alice: %v", err)
	}
	s2, err := w.SpawnBot("Bob", "W1N2")
	if err != nil {
		t.Fatalf("SpawnBot Bob: %v", err)
	}
	u1 := s1.User
	hiddenSpawnID := s2.ID

	vm2 := NewBotVM("Alice", u1)
	defer vm2.Close()
	script := `
local screeps <const> = require("screeps")
local function loop(game, memory)
	local room = game.rooms["W1N1"]
	assert(room ~= nil, "expected W1N1 visible")
	assert(game.rooms["W1N2"] == nil, "W1N2 must not be visible")
	assert(game.getObjectById("` + hiddenSpawnID + `") == nil, "hidden room object must not be visible")

	-- Tampering with room.name must not grant vision of W1N2
	room.name = "W1N2"
	assert(#room:find(screeps.FIND.HOSTILE_SPAWNS) == 0, "mutated room.name must not leak W1N2")

	-- dryRun must validate without spawning or mutating Memory.creeps
	local spawn = game.spawns["Spawn1"]
	local code = spawn:spawnCreep({screeps.PART.WORK, screeps.PART.CARRY, screeps.PART.MOVE}, "DryTest", { dryRun = true, memory = { role = "test" } })
	assert(code == screeps.OK, "expected dryRun OK, got " .. tostring(code))
	assert(memory.creeps["DryTest"] == nil, "dryRun must not write Memory.creeps")

	-- findClosestByRange must accept FIND_* integer constants
	local closestSrc = spawn.pos:findClosestByRange(screeps.FIND.SOURCES)
	assert(closestSrc ~= nil, "expected findClosestByRange(FIND.SOURCES) to return a source")
end
return { loop = loop }
`
	if err := vm2.LoadFS(fstest.MapFS{"bot/main.lua": &fstest.MapFile{Data: []byte(script)}}, "bot"); err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	if err := vm2.RunTick(w); err != nil {
		t.Fatalf("RunTick failed: %v", err)
	}
	w.Step()
	// Verify DryTest was NOT spawned.
	for _, e := range w.Rooms["W1N1"].Entities {
		if e.Type == engine.TYPE_CREEP && e.Name == "DryTest" {
			t.Fatalf("dryRun unexpectedly spawned DryTest")
		}
	}
}
