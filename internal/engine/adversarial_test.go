package engine

import (
	"testing"
)

func TestSafeModeAndRampartBlockDismantleAndTowerAttack(t *testing.T) {
	w := NewWorld()
	room := w.GenerateRoom("W1N1", 42)
	alice := w.EnsureUser("Alice")
	bob := w.EnsureUser("Bob")

	ctrl := room.FindController()
	ctrl.User = alice
	ctrl.Level = 3
	ctrl.SafeMode = w.GameTime + 100

	// Clear area around (20,20).
	for dy := -2; dy <= 2; dy++ {
		for dx := -2; dx <= 2; dx++ {
			room.Terrain[(20+dy)*RoomSize+(20+dx)] = TERRAIN_MASK_PLAIN
		}
	}

	targetSpawn := &Entity{
		ID: "sp_alice", Type: STRUCTURE_SPAWN, Room: room.Name, X: 20, Y: 20,
		User: alice, Name: "Spawn1", Hits: 5000, HitsMax: 5000,
		Store:                 map[string]int{RESOURCE_ENERGY: 300},
		StoreCapacityResource: map[string]int{RESOURCE_ENERGY: SPAWN_ENERGY_CAPACITY},
	}
	rampart := &Entity{
		ID: "rmp_alice", Type: STRUCTURE_RAMPART, Room: room.Name, X: 20, Y: 20,
		User: alice, Hits: 1000, HitsMax: 1000000,
	}
	bobTower := &Entity{
		ID: "twr_bob", Type: STRUCTURE_TOWER, Room: room.Name, X: 22, Y: 20,
		User: bob, Hits: 3000, HitsMax: 3000,
		Store:                 map[string]int{RESOURCE_ENERGY: 500},
		StoreCapacityResource: map[string]int{RESOURCE_ENERGY: TOWER_CAPACITY},
	}
	room.Entities[targetSpawn.ID] = targetSpawn
	room.Entities[rampart.ID] = rampart
	room.Entities[bobTower.ID] = bobTower

	dismantler := addTestCreep(w, room, bob, "Breaker", 21, 20, []string{WORK, CARRY, MOVE})

	// Under Safe Mode: dismantle and towerAttack must deal 0 damage and grant 0 energy.
	if code := w.QueueIntent(bob, Intent{Type: IntentDismantle, ActorID: dismantler.ID, TargetID: targetSpawn.ID}); code != OK {
		t.Fatalf("QueueIntent dismantle: %d", code)
	}
	if code := w.QueueIntent(bob, Intent{Type: IntentTowerAttack, ActorID: bobTower.ID, TargetID: targetSpawn.ID}); code != OK {
		t.Fatalf("QueueIntent towerAttack: %d", code)
	}
	w.Step()
	if targetSpawn.Hits != 5000 || rampart.Hits != 1000 {
		t.Fatalf("Safe Mode bypassed: spawn=%d rampart=%d", targetSpawn.Hits, rampart.Hits)
	}
	if dismantler.UsedCapacity(RESOURCE_ENERGY) != 0 {
		t.Fatalf("expected 0 energy gained under Safe Mode, got %d", dismantler.UsedCapacity(RESOURCE_ENERGY))
	}

	// Expire Safe Mode: damage must redirect to the covering friendly Rampart, not the Spawn.
	ctrl.SafeMode = 0
	w.QueueIntent(bob, Intent{Type: IntentDismantle, ActorID: dismantler.ID, TargetID: targetSpawn.ID})
	w.QueueIntent(bob, Intent{Type: IntentTowerAttack, ActorID: bobTower.ID, TargetID: targetSpawn.ID})
	w.Step()
	if targetSpawn.Hits != 5000 {
		t.Fatalf("Rampart bypassed: spawn hits=%d, want 5000", targetSpawn.Hits)
	}
	if rampart.Hits >= 1000 {
		t.Fatalf("expected rampart to absorb dismantle+tower damage, hits=%d", rampart.Hits)
	}
}

func TestMultiSpawnEnergyDeductionAndInvalidDirections(t *testing.T) {
	w := NewWorld()
	room := w.GenerateRoom("W2N2", 7)
	alice := w.EnsureUser("Alice")

	for dy := -2; dy <= 2; dy++ {
		for dx := -2; dx <= 2; dx++ {
			room.Terrain[(25+dy)*RoomSize+(25+dx)] = TERRAIN_MASK_PLAIN
		}
	}

	sp1 := &Entity{
		ID: "sp1", Type: STRUCTURE_SPAWN, Room: room.Name, X: 25, Y: 25,
		User: alice, Name: "Spawn1", Hits: 5000, HitsMax: 5000,
		Store:                 map[string]int{RESOURCE_ENERGY: 100},
		StoreCapacityResource: map[string]int{RESOURCE_ENERGY: SPAWN_ENERGY_CAPACITY},
	}
	sp2 := &Entity{
		ID: "sp2", Type: STRUCTURE_SPAWN, Room: room.Name, X: 27, Y: 25,
		User: alice, Name: "Spawn2", Hits: 5000, HitsMax: 5000,
		Store:                 map[string]int{RESOURCE_ENERGY: 300},
		StoreCapacityResource: map[string]int{RESOURCE_ENERGY: SPAWN_ENERGY_CAPACITY},
	}
	room.Entities[sp1.ID] = sp1
	room.Entities[sp2.ID] = sp2

	// Invalid directions must be rejected at QueueIntent.
	if code := w.QueueIntent(alice, Intent{
		Type:       IntentSpawnCreep,
		ActorID:    sp1.ID,
		CreepName:  "BadDir",
		Body:       []string{WORK, CARRY, MOVE, MOVE, MOVE}, // 300 energy
		Directions: []int{0},
	}); code != ERR_INVALID_ARGS {
		t.Fatalf("expected ERR_INVALID_ARGS for direction 0, got %d", code)
	}

	// Valid 300-energy spawn must deduct 100 from sp1 and 200 from sp2.
	if code := w.QueueIntent(alice, Intent{
		Type:      IntentSpawnCreep,
		ActorID:   sp1.ID,
		CreepName: "MultiSpawnCreep",
		Body:      []string{WORK, CARRY, MOVE, MOVE, MOVE}, // 300 energy
	}); code != OK {
		t.Fatalf("QueueIntent spawn failed: %d", code)
	}
	w.Step()
	// Account for +1 passive spawn regen on sp2 (total energy after deduction is 100 < 300).
	if sp1.UsedCapacity(RESOURCE_ENERGY) != 0 {
		t.Fatalf("expected sp1 energy=0, got %d", sp1.UsedCapacity(RESOURCE_ENERGY))
	}
	if sp2.UsedCapacity(RESOURCE_ENERGY) > 101 {
		t.Fatalf("expected sp2 energy deducted to ~100, got %d", sp2.UsedCapacity(RESOURCE_ENERGY))
	}

	// Destroy sp1 while spawning: orphaned spawning creep must be cleaned up so name is reusable.
	sp1.Hits = 0
	w.Step()
	for _, e := range room.Entities {
		if e.Type == TYPE_CREEP && e.Name == "MultiSpawnCreep" {
			t.Fatalf("orphaned spawning creep %s survived spawn destruction", e.ID)
		}
	}
}

func TestWithdrawRejectsResourceAndCreep(t *testing.T) {
	w := NewWorld()
	room := w.GenerateRoom("W3N3", 9)
	alice := w.EnsureUser("Alice")

	c1 := addTestCreep(w, room, alice, "C1", 20, 20, []string{CARRY, MOVE})
	c2 := addTestCreep(w, room, alice, "C2", 21, 20, []string{CARRY, MOVE})
	c2.AddStore(RESOURCE_ENERGY, 50)
	res := &Entity{
		ID: "res1", Type: TYPE_RESOURCE, Room: room.Name, X: 20, Y: 21,
		ResourceType: RESOURCE_ENERGY, Amount: 50,
	}
	room.Entities[res.ID] = res

	if code := w.QueueIntent(alice, Intent{Type: IntentWithdraw, ActorID: c1.ID, TargetID: res.ID, ResourceType: RESOURCE_ENERGY}); code != ERR_INVALID_TARGET {
		t.Fatalf("expected ERR_INVALID_TARGET withdrawing from dropped resource, got %d", code)
	}
	if code := w.QueueIntent(alice, Intent{Type: IntentWithdraw, ActorID: c1.ID, TargetID: c2.ID, ResourceType: RESOURCE_ENERGY}); code != ERR_INVALID_TARGET {
		t.Fatalf("expected ERR_INVALID_TARGET withdrawing from creep, got %d", code)
	}
}

func TestSelfAttackSpawningTargetAndConflictGroups(t *testing.T) {
	w := NewWorld()
	room := w.GenerateRoom("W4N4", 11)
	alice := w.EnsureUser("Alice")
	bob := w.EnsureUser("Bob")

	a := addTestCreep(w, room, alice, "A", 20, 20, []string{ATTACK, HEAL, MOVE})
	b := addTestCreep(w, room, bob, "B", 21, 20, []string{ATTACK, MOVE})
	spawning := addTestCreep(w, room, bob, "SpawningB", 20, 21, []string{MOVE})
	spawning.Spawning = true

	if code := w.QueueIntent(alice, Intent{Type: IntentAttack, ActorID: a.ID, TargetID: a.ID}); code != ERR_INVALID_TARGET {
		t.Fatalf("expected ERR_INVALID_TARGET on self-attack, got %d", code)
	}
	if code := w.QueueIntent(alice, Intent{Type: IntentAttack, ActorID: a.ID, TargetID: spawning.ID}); code != ERR_INVALID_TARGET {
		t.Fatalf("expected ERR_INVALID_TARGET attacking spawning creep, got %d", code)
	}

	// Order-independent duel: both creeps at 30 HP kill each other simultaneously.
	a.Hits = 30
	b.Hits = 30
	w.QueueIntent(alice, Intent{Type: IntentAttack, ActorID: a.ID, TargetID: b.ID})
	w.QueueIntent(bob, Intent{Type: IntentAttack, ActorID: b.ID, TargetID: a.ID})
	w.Step()
	if _, aliveA := room.Entities[a.ID]; aliveA {
		t.Fatalf("expected creep A to die in simultaneous duel")
	}
	if _, aliveB := room.Entities[b.ID]; aliveB {
		t.Fatalf("expected creep B to die in simultaneous duel")
	}
}

func TestProceduralExitsAllReachable(t *testing.T) {
	for seed := int64(1); seed <= 25; seed++ {
		w := NewWorld()
		room := w.GenerateRoom("W5N5", seed)
		// Find any interior plain tile reachable from an exit.
		for x := 1; x < RoomSize-1; x++ {
			if room.TerrainAt(x, 0) != TERRAIN_MASK_WALL && room.TerrainAt(x, 1) == TERRAIN_MASK_WALL {
				t.Fatalf("seed %d: top exit (%d,0) is walled off at (%d,1)", seed, x, x)
			}
			if room.TerrainAt(x, RoomSize-1) != TERRAIN_MASK_WALL && room.TerrainAt(x, RoomSize-2) == TERRAIN_MASK_WALL {
				t.Fatalf("seed %d: bottom exit (%d,49) is walled off at (%d,48)", seed, x, x)
			}
		}
		for y := 1; y < RoomSize-1; y++ {
			if room.TerrainAt(0, y) != TERRAIN_MASK_WALL && room.TerrainAt(1, y) == TERRAIN_MASK_WALL {
				t.Fatalf("seed %d: left exit (0,%d) is walled off at (1,%d)", seed, y, y)
			}
			if room.TerrainAt(RoomSize-1, y) != TERRAIN_MASK_WALL && room.TerrainAt(RoomSize-2, y) == TERRAIN_MASK_WALL {
				t.Fatalf("seed %d: right exit (49,%d) is walled off at (48,%d)", seed, y, y)
			}
		}
	}
}

func TestConstructionSiteExtractorAndRampartRules(t *testing.T) {
	w := NewWorld()
	room := w.GenerateRoom("W6N6", 13)
	alice := w.EnsureUser("Alice")
	bob := w.EnsureUser("Bob")

	ctrl := room.FindController()
	ctrl.User = alice
	ctrl.Level = 6

	room.Terrain[20*RoomSize+20] = TERRAIN_MASK_PLAIN
	// Extractor without mineral must fail.
	if _, code := w.CreateConstructionSite(alice, room.Name, 20, 20, STRUCTURE_EXTRACTOR); code != ERR_INVALID_TARGET {
		t.Fatalf("expected ERR_INVALID_TARGET placing extractor off mineral, got %d", code)
	}

	// Rampart over hostile structure must fail.
	room.Entities["hostile_ext"] = &Entity{
		ID: "hostile_ext", Type: STRUCTURE_EXTENSION, Room: room.Name, X: 20, Y: 20,
		User: bob, Hits: 1000, HitsMax: 1000,
	}
	if _, code := w.CreateConstructionSite(alice, room.Name, 20, 20, STRUCTURE_RAMPART); code != ERR_INVALID_TARGET {
		t.Fatalf("expected ERR_INVALID_TARGET placing rampart over hostile structure, got %d", code)
	}
}

func TestImportStateClearsEventsAndAdvancesNextID(t *testing.T) {
	w := NewWorld()
	w.GenerateRoom("W7N7", 15)
	w.recordEventLocked("W7N7", "test", "old event")

	st := w.ExportState()
	st.NextID = 1 // Deliberately lower than existing entity IDs.
	st.Rooms[0].Entities = append(st.Rooms[0].Entities, EntityState{
		ID: "creep_999", Type: TYPE_CREEP, Room: "WRONG", X: 25, Y: 25, Hits: 100, HitsMax: 100,
	})

	if err := w.ImportState(st); err != nil {
		t.Fatalf("ImportState: %v", err)
	}
	if evs := w.RecentEvents(10); len(evs) != 0 {
		t.Fatalf("expected events cleared on ImportState, got %d", len(evs))
	}
	if w.Rooms["W7N7"].Entities["creep_999"].Room != "W7N7" {
		t.Fatalf("expected entity room normalized to W7N7, got %q", w.Rooms["W7N7"].Entities["creep_999"].Room)
	}
	if next := w.nextEntityID("creep"); next == "creep_1" || next == "creep_999" {
		t.Fatalf("expected nextID advanced past 999, got %q", next)
	}
}
