package lua

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"testing/fstest"

	rt "github.com/arnodel/golua/runtime"

	"go-screeps/internal/engine"
)

func TestPersistRoundTripWorldAndMemory(t *testing.T) {
	botFS := fstest.MapFS{
		"bot/main.lua": &fstest.MapFile{
			Data: []byte(`
local screeps <const> = require("screeps")
local PART <const> = screeps.PART
local OK <const> = screeps.OK

return {
	loop = function(Game, Memory)
		Memory.ticks = (Memory.ticks or 0) + 1
		Memory.lastGameTime = Game.time
		Memory.flag = true
		Memory.label = "alpha-colony"
		Memory.queue = { "harvester", "upgrader", "builder" }
		Memory.nested = {
			level = 2,
			coords = { 10, 20, 30 },
			meta = { active = true, note = "ok" },
		}
		-- Unsupported values should be ignored cleanly without panicking.
		Memory.ignoredFn = function() return 42 end
		Memory.ignoredNaN = 0 / 0

		for name, spawn in pairs(Game.spawns) do
			if not spawn.spawning then
				local cname = "c_" .. tostring(Game.time)
				local rc = spawn:spawnCreep({ PART.WORK, PART.CARRY, PART.MOVE }, cname)
				if rc == OK then
					Memory.creeps[cname] = { role = "harvester", born = Game.time }
				end
			end
		end

		for name, creep in pairs(Game.creeps) do
			if not Memory.creeps[name] then
				Memory.creeps[name] = { role = "worker" }
			end
			Memory.creeps[name].lastSeen = Game.time
			creep:say("hi")
		end
	end
}
`),
		},
	}

	origWorld := engine.NewWorld()
	origWorld.GenerateRoom("W1N1", 12345)

	origRunner := NewLocalRunner(origWorld, "W1N1")
	if err := origRunner.AddEmbeddedBot("Alice", "W1N1", botFS, "bot"); err != nil {
		t.Fatalf("AddEmbeddedBot: %v", err)
	}
	defer origRunner.Close()

	// Step enough ticks for a creep to spawn and act, plus another to be mid-spawn or active.
	for i := 0; i < 15; i++ {
		origRunner.StepTick()
	}

	for _, vm := range origRunner.Bots() {
		if vm.LastError != "" {
			t.Fatalf("original bot Lua error: %s", vm.LastError)
		}
	}

	// Capture expected state before saving.
	expectedWorldState := origWorld.ExportState()
	expectedMem := origRunner.Bots()[0].ExportMemory()

	// Save into a nested directory to also test parent dir creation and atomic rename.
	savePath := filepath.Join(t.TempDir(), "nested", "state", "world_state.json")
	if err := origRunner.SaveState(savePath); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	if _, err := os.Stat(savePath + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temp file %q should not remain after atomic rename", savePath+".tmp")
	}

	// Load save file.
	sf, err := LoadSaveFile(savePath)
	if err != nil {
		t.Fatalf("LoadSaveFile: %v", err)
	}

	// Restore into a completely fresh World and LocalRunner.
	restoredWorld := engine.NewWorld()
	if err := restoredWorld.ImportState(sf.World); err != nil {
		t.Fatalf("ImportState: %v", err)
	}

	restoredRunner := NewLocalRunner(restoredWorld, "W1N1")
	if err := restoredRunner.AttachEmbeddedBot("Alice", "W1N1", botFS, "bot"); err != nil {
		t.Fatalf("AttachEmbeddedBot: %v", err)
	}
	defer restoredRunner.Close()

	for username, mem := range sf.Memories {
		restoredRunner.ApplyBotMemory(username, mem)
	}

	// Verify WorldState matches identically (GameTime, Users, UserByName, NextID, Rooms, Entities).
	gotWorldState := restoredWorld.ExportState()
	if !reflect.DeepEqual(expectedWorldState, gotWorldState) {
		t.Fatalf("restored WorldState mismatch:\nwant: %+v\ngot:  %+v", expectedWorldState, gotWorldState)
	}
	if restoredWorld.GameTime != origWorld.GameTime {
		t.Fatalf("GameTime = %d, want %d", restoredWorld.GameTime, origWorld.GameTime)
	}

	// Verify Lua Memory survived identically.
	restoredVM := restoredRunner.Bots()[0]
	gotMem := restoredVM.ExportMemory()
	if !reflect.DeepEqual(expectedMem, gotMem) {
		t.Fatalf("restored Memory mismatch:\nwant: %#v\ngot:  %#v", expectedMem, gotMem)
	}

	// Verify specific nested values directly inside Lua.
	if err := restoredVM.DoString(`
		local Memory = require("screeps").Memory
		assert(Memory.ticks == 15, "expected Memory.ticks == 15, got " .. tostring(Memory.ticks))
		assert(Memory.flag == true, "expected Memory.flag == true")
		assert(Memory.label == "alpha-colony", "expected Memory.label == alpha-colony")
		assert(#Memory.queue == 3 and Memory.queue[2] == "upgrader", "expected Memory.queue array preserved")
		assert(Memory.nested.level == 2, "expected nested.level == 2")
		assert(#Memory.nested.coords == 3 and Memory.nested.coords[3] == 30, "expected nested.coords preserved")
		assert(Memory.nested.meta.active == true and Memory.nested.meta.note == "ok", "expected nested.meta preserved")
		assert(Memory.ignoredFn == nil, "expected function skipped")
		assert(Memory.ignoredNaN == nil, "expected NaN skipped")
	`); err != nil {
		t.Fatalf("Lua Memory assertion failed: %v", err)
	}

	// Step additional ticks on the restored runner and ensure zero Lua errors and proper continuation.
	startTick := restoredWorld.GameTime
	for i := 0; i < 10; i++ {
		restoredRunner.StepTick()
	}
	if restoredWorld.GameTime != startTick+10 {
		t.Fatalf("after stepping restored world: GameTime = %d, want %d", restoredWorld.GameTime, startTick+10)
	}
	if restoredVM.LastError != "" {
		t.Fatalf("restored bot Lua error after stepping: %s", restoredVM.LastError)
	}
	if err := restoredVM.DoString(`
		local Memory = require("screeps").Memory
		assert(Memory.ticks == 25, "expected Memory.ticks == 25 after 10 more ticks, got " .. tostring(Memory.ticks))
	`); err != nil {
		t.Fatalf("Lua continuation assertion failed: %v", err)
	}
}

func TestAutoSaveIntervalAndClose(t *testing.T) {
	botFS := fstest.MapFS{
		"bot/main.lua": &fstest.MapFile{
			Data: []byte(`
return function(_, Memory)
	Memory.count = (Memory.count or 0) + 1
end
`),
		},
	}

	world := engine.NewWorld()
	world.GenerateRoom("W2N2", 99)

	runner := NewLocalRunner(world, "W2N2")
	if err := runner.AddEmbeddedBot("Bob", "W2N2", botFS, "bot"); err != nil {
		t.Fatalf("AddEmbeddedBot: %v", err)
	}

	savePath := filepath.Join(t.TempDir(), "autosave.json")
	runner.SetStatePath(savePath, 5)

	// NewWorld starts at GameTime = 1; after 3 steps GameTime = 4 (not divisible by 5).
	for i := 0; i < 3; i++ {
		runner.StepTick()
	}
	if _, err := os.Stat(savePath); !os.IsNotExist(err) {
		t.Fatalf("expected no autosave yet at tick %d", world.GameTime)
	}

	// 4th step reaches GameTime = 5 -> triggers autosave.
	runner.StepTick()
	sf, err := LoadSaveFile(savePath)
	if err != nil {
		t.Fatalf("expected autosave at tick 5: %v", err)
	}
	if sf.World.GameTime != 5 {
		t.Fatalf("autosaved GameTime = %d, want 5", sf.World.GameTime)
	}
	if got := sf.Memories["Bob"]["count"]; got != float64(4) {
		t.Fatalf("autosaved Memory.count = %v, want 4", got)
	}

	// Step 2 more ticks (GameTime = 7, not divisible by 5), then Close() should save final state.
	runner.StepTick()
	runner.StepTick()
	runner.Close()

	sf2, err := LoadSaveFile(savePath)
	if err != nil {
		t.Fatalf("LoadSaveFile after Close: %v", err)
	}
	if sf2.World.GameTime != 7 {
		t.Fatalf("saved GameTime after Close = %d, want 7", sf2.World.GameTime)
	}
	if got := sf2.Memories["Bob"]["count"]; got != float64(6) {
		t.Fatalf("saved Memory.count after Close = %v, want 6", got)
	}
}

func TestMemoryConversionEdgeCases(t *testing.T) {
	vm := NewBotVM("EdgeBot", "u99")
	defer vm.Close()

	// Create cyclic table, userdata, function, and mixed values in Memory.
	ud := rt.NewUserData("secret", nil)
	vm.R.GlobalEnv().Set(rt.StringValue("testUD"), rt.UserDataValue(ud))

	if err := vm.DoString(`
		local Memory = require("screeps").Memory
		local cyc = { name = "root" }
		cyc.self = cyc
		Memory.cyclic = cyc
		Memory.ud = testUD
		Memory.fn = function() end
		Memory.empty = {}
		Memory.arr = { 1, "two", true }
	`); err != nil {
		t.Fatalf("DoString: %v", err)
	}

	exported := vm.ExportMemory()
	if _, hasFn := exported["fn"]; hasFn {
		t.Fatalf("expected function to be omitted")
	}
	if _, hasUD := exported["ud"]; hasUD {
		t.Fatalf("expected userdata to be omitted")
	}
	cycMap, ok := exported["cyclic"].(map[string]any)
	if !ok || cycMap["name"] != "root" {
		t.Fatalf("expected cyclic.name == root, got %#v", exported["cyclic"])
	}
	if _, hasSelf := cycMap["self"]; hasSelf {
		t.Fatalf("expected cyclic self-reference to be omitted")
	}
	arr, ok := exported["arr"].([]any)
	if !ok || len(arr) != 3 || arr[0] != float64(1) || arr[1] != "two" || arr[2] != true {
		t.Fatalf("unexpected exported array: %#v", exported["arr"])
	}

	// Import nil should still preserve Memory.creeps and Memory.rooms tables.
	vm.ImportMemory(nil)
	memVal := vm.screepsMod.Get(rt.StringValue("Memory"))
	tbl, ok := memVal.TryTable()
	if !ok {
		t.Fatalf("expected Memory table after ImportMemory(nil)")
	}
	if tbl.Get(rt.StringValue("creeps")).Type() != rt.TableType || tbl.Get(rt.StringValue("rooms")).Type() != rt.TableType {
		t.Fatalf("expected Memory.creeps and Memory.rooms tables after ImportMemory(nil)")
	}
}
