package engine

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"go-screeps/internal/screeps"
)

func setupTestRoom(t *testing.T) (*World, *Room, string, *Entity) {
	t.Helper()
	w := NewWorld()
	room := w.GenerateRoom("W1N1", 42)
	spawn, err := w.SpawnBot("Alice", "W1N1")
	if err != nil {
		t.Fatalf("SpawnBot failed: %v", err)
	}
	return w, room, spawn.User, spawn
}

func addTestCreep(w *World, room *Room, user, name string, x, y int, bodyParts []string) *Entity {
	body := make([]BodyPart, len(bodyParts))
	carry := 0
	for i, pt := range bodyParts {
		body[i] = BodyPart{Type: pt, Hits: 100}
		if pt == CARRY {
			carry++
		}
	}
	w.Lock()
	defer w.Unlock()
	id := w.nextEntityID("creep")
	c := &Entity{
		ID:            id,
		Type:          TYPE_CREEP,
		Room:          room.Name,
		X:             x,
		Y:             y,
		User:          user,
		Name:          name,
		Hits:          len(body) * 100,
		HitsMax:       len(body) * 100,
		Body:          body,
		Store:         make(map[string]int),
		StoreCapacity: carry * CARRY_CAPACITY,
		AgeTime:       w.GameTime + CREEP_LIFE_TIME,
	}
	room.Entities[id] = c
	return c
}

func TestGenerateRoomAndSpawnBot(t *testing.T) {
	w := NewWorld()
	room := w.GenerateRoom("w5n5", 12345)
	if room.Name != "W5N5" {
		t.Fatalf("expected normalized room name W5N5, got %s", room.Name)
	}
	if len(room.TerrainString()) != RoomSize*RoomSize {
		t.Fatalf("expected terrain length 2500, got %d", len(room.TerrainString()))
	}

	sources, minerals, controllers := 0, 0, 0
	for _, e := range room.Entities {
		switch e.Type {
		case TYPE_SOURCE:
			sources++
		case TYPE_MINERAL:
			minerals++
		case STRUCTURE_CONTROLLER:
			controllers++
		}
	}
	if sources != 2 || minerals != 1 || controllers != 1 {
		t.Fatalf("expected 2 sources, 1 mineral, 1 controller; got %d, %d, %d", sources, minerals, controllers)
	}

	spawn, err := w.SpawnBot("TestBot", "W5N5")
	if err != nil {
		t.Fatalf("SpawnBot failed: %v", err)
	}
	if spawn.Name != "Spawn1" || spawn.UsedCapacity(RESOURCE_ENERGY) != SPAWN_ENERGY_START {
		t.Fatalf("unexpected spawn state: %+v", spawn)
	}
	ctrl := room.FindController()
	if ctrl == nil || ctrl.Level != 1 || ctrl.User != spawn.User {
		t.Fatalf("expected claimed RCL 1 controller, got %+v", ctrl)
	}
	if _, err := w.SpawnBot("TestBot", "NOPE"); err == nil {
		t.Fatal("expected error for unknown room")
	}
	if names := w.RoomNames(); len(names) != 1 || names[0] != "W5N5" {
		t.Fatalf("unexpected RoomNames: %v", names)
	}
	if w.GetRoom("w5n5") == nil || w.GetEntity(spawn.ID) == nil {
		t.Fatal("GetRoom / GetEntity lookup failed")
	}
}

func TestLoadFromDBJSON(t *testing.T) {
	terrain := make([]byte, RoomSize*RoomSize)
	for i := range terrain {
		terrain[i] = '0'
	}
	terrain[0] = '1'
	terrain[100] = '2'

	dbJSON := `{
		"collections": [
			{"name": "rooms.terrain", "data": [{"room": "W9N7", "terrain": "` + string(terrain) + `"}]},
			{"name": "rooms.objects", "data": [
				{"_id": "s1", "room": "W9N7", "type": "source", "x": 10, "y": 10, "energy": 3000, "energyCapacity": 3000},
				{"_id": "m1", "room": "W9N7", "type": "mineral", "x": 20, "y": 20, "mineralType": "U", "mineralAmount": 50000},
				{"_id": "c1", "room": "W9N7", "type": "controller", "x": 30, "y": 30}
			]}
		]
	}`

	tmp := filepath.Join(t.TempDir(), "db.json")
	if err := os.WriteFile(tmp, []byte(dbJSON), 0644); err != nil {
		t.Fatal(err)
	}

	w := NewWorld()
	if err := w.LoadFromDBJSON(tmp); err != nil {
		t.Fatalf("LoadFromDBJSON failed: %v", err)
	}
	room := w.GetRoom("W9N7")
	if room == nil {
		t.Fatal("room W9N7 not loaded")
	}
	if room.TerrainAt(0, 0) != TERRAIN_MASK_WALL || room.TerrainAt(0, 2) != TERRAIN_MASK_SWAMP {
		t.Fatalf("unexpected terrain values: (0,0)=%d (0,2)=%d", room.TerrainAt(0, 0), room.TerrainAt(0, 2))
	}
	if len(room.Entities) != 3 {
		t.Fatalf("expected 3 static objects, got %d", len(room.Entities))
	}
	if room.Entities["m1"].MineralType != "U" || room.Entities["m1"].MineralAmount != 50000 {
		t.Fatalf("unexpected mineral: %+v", room.Entities["m1"])
	}

	if err := w.LoadFromDBJSON(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestPathfinderAndCostMatrix(t *testing.T) {
	w := NewWorld()
	room := &Room{Name: "W0N0", Entities: make(map[string]*Entity)}
	w.Rooms["W0N0"] = room

	// Place a vertical wall at x=10 from y=5..15.
	for y := 5; y <= 15; y++ {
		room.Terrain[y*RoomSize+10] = TERRAIN_MASK_WALL
	}
	// Place a swamp strip at y=4, x=10 and a road at y=16, x=10.
	room.Terrain[4*RoomSize+10] = TERRAIN_MASK_SWAMP
	room.Entities["road1"] = &Entity{ID: "road1", Type: STRUCTURE_ROAD, Room: "W0N0", X: 10, Y: 16}

	res := SearchPath(room, 8, 10, []Goal{{X: 12, Y: 10, Range: 0}}, PathOpts{})
	if res.Incomplete || len(res.Path) == 0 {
		t.Fatalf("expected complete path around wall, got %+v", res)
	}
	last := res.Path[len(res.Path)-1]
	if last.X != 12 || last.Y != 10 {
		t.Fatalf("path ended at (%d,%d), want (12,10)", last.X, last.Y)
	}
	for _, st := range res.Path {
		if room.TerrainAt(st.X, st.Y) == TERRAIN_MASK_WALL {
			t.Fatalf("path stepped on wall at (%d,%d)", st.X, st.Y)
		}
	}

	// Test CostMatrix override blocking y=4 so it must route around y=16.
	cm := NewCostMatrix()
	cm.Set(10, 4, 255)
	cm.Set(10, 3, 255)
	cm.Set(10, 2, 255)
	cm.Set(10, 1, 255)
	cm.Set(10, 0, 255)
	cmClone := cm.Clone()
	if cmClone.Get(10, 4) != 255 || cmClone.Get(-1, 0) != 255 {
		t.Fatal("CostMatrix Clone/Get mismatch")
	}

	res2 := SearchPath(room, 8, 10, []Goal{{X: 12, Y: 10, Range: 0}}, PathOpts{CostMatrix: cmClone})
	if res2.Incomplete {
		t.Fatal("expected complete path via bottom gap")
	}
	for _, st := range res2.Path {
		if st.X == 10 && st.Y <= 4 {
			t.Fatalf("path ignored CostMatrix block at (%d,%d)", st.X, st.Y)
		}
	}

	// Test Flee mode.
	fleeRes := SearchPath(room, 20, 20, []Goal{{X: 20, Y: 20, Range: 4}}, PathOpts{Flee: true})
	if fleeRes.Incomplete || len(fleeRes.Path) == 0 {
		t.Fatalf("expected valid flee path, got %+v", fleeRes)
	}
	fEnd := fleeRes.Path[len(fleeRes.Path)-1]
	if ChebyshevDist(20, 20, fEnd.X, fEnd.Y) < 4 {
		t.Fatalf("flee path did not reach range 4: (%d,%d)", fEnd.X, fEnd.Y)
	}

	// Test FindDirection.
	dir := FindDirection(room, 5, 5, 8, 5, true)
	if dir != RIGHT {
		t.Fatalf("expected RIGHT (%d), got %d", RIGHT, dir)
	}
	if FindDirection(room, 5, 5, 5, 5, true) != 0 {
		t.Fatal("expected 0 when already at target")
	}
}

func TestMovementFatigueAndCollisions(t *testing.T) {
	w := NewWorld()
	room := &Room{Name: "W0N0", Entities: make(map[string]*Entity)}
	w.Rooms["W0N0"] = room
	user := w.EnsureUser("Bob")

	// Set (12,10) to swamp and (13,10) to road.
	room.Terrain[10*RoomSize+12] = TERRAIN_MASK_SWAMP
	room.Entities["road1"] = &Entity{ID: "road1", Type: STRUCTURE_ROAD, Room: "W0N0", X: 13, Y: 10}

	// Creep with 1 WORK, 1 CARRY (empty), 1 MOVE -> 1 heavy part when empty, 2 heavy parts when carrying energy.
	c := addTestCreep(w, room, user, "worker", 10, 10, []string{WORK, CARRY, MOVE})

	// Move onto plain (11,10): heavy=1 * plain(2) - 2*move(1) = 0 fatigue.
	if code := w.QueueIntent(user, Intent{Type: IntentMove, ActorID: c.ID, Direction: RIGHT}); code != OK {
		t.Fatalf("move onto plain failed: %d", code)
	}
	w.Step()
	if c.X != 11 || c.Y != 10 || c.Fatigue != 0 {
		t.Fatalf("expected (11,10) fatigue 0, got (%d,%d) fatigue %d", c.X, c.Y, c.Fatigue)
	}

	// Load energy so CARRY part becomes heavy (heavy=2), move onto swamp (12,10): 2*10 - 2 = 18 fatigue.
	c.AddStore(RESOURCE_ENERGY, 10)
	if code := w.QueueIntent(user, Intent{Type: IntentMove, ActorID: c.ID, Direction: RIGHT}); code != OK {
		t.Fatalf("move onto swamp failed: %d", code)
	}
	w.Step()
	if c.X != 12 || c.Y != 10 || c.Fatigue != 18 {
		t.Fatalf("expected (12,10) fatigue 18, got (%d,%d) fatigue %d", c.X, c.Y, c.Fatigue)
	}
	// Attempting to move while tired returns ERR_TIRED.
	if code := w.QueueIntent(user, Intent{Type: IntentMove, ActorID: c.ID, Direction: RIGHT}); code != ERR_TIRED {
		t.Fatalf("expected ERR_TIRED, got %d", code)
	}

	// Step 9 ticks to clear 18 fatigue (2 per tick).
	for i := 0; i < 9; i++ {
		w.Step()
	}
	if c.Fatigue != 0 {
		t.Fatalf("expected fatigue 0 after decay, got %d", c.Fatigue)
	}

	// Move onto road (13,10) with heavy=2: 2*1 - 2 = 0 fatigue!
	if code := w.QueueIntent(user, Intent{Type: IntentMove, ActorID: c.ID, Direction: RIGHT}); code != OK {
		t.Fatalf("move onto road failed: %d", code)
	}
	w.Step()
	if c.X != 13 || c.Y != 10 || c.Fatigue != 0 {
		t.Fatalf("expected (13,10) fatigue 0 on road, got (%d,%d) fatigue %d", c.X, c.Y, c.Fatigue)
	}

	// Test swap between two creeps A(20,20) and B(21,20).
	a := addTestCreep(w, room, user, "A", 20, 20, []string{MOVE})
	b := addTestCreep(w, room, user, "B", 21, 20, []string{MOVE})
	w.QueueIntent(user, Intent{Type: IntentMove, ActorID: a.ID, Direction: RIGHT})
	w.QueueIntent(user, Intent{Type: IntentMove, ActorID: b.ID, Direction: LEFT})
	w.Step()
	if a.X != 21 || b.X != 20 {
		t.Fatalf("expected swap A->21, B->20; got A=%d, B=%d", a.X, b.X)
	}

	// Test collision: two creeps targeting same empty tile (25,25).
	c1 := addTestCreep(w, room, user, "C1", 24, 25, []string{MOVE})
	c2 := addTestCreep(w, room, user, "C2", 26, 25, []string{MOVE})
	w.QueueIntent(user, Intent{Type: IntentMove, ActorID: c1.ID, Direction: RIGHT})
	w.QueueIntent(user, Intent{Type: IntentMove, ActorID: c2.ID, Direction: LEFT})
	w.Step()
	if c1.X != 25 || c2.X != 26 {
		t.Fatalf("expected c1 (lower ID) to win (25,25) and c2 stay at 26; got c1=%d c2=%d", c1.X, c2.X)
	}
}

func TestSpawningWithExtensions(t *testing.T) {
	w, room, user, spawn := setupTestRoom(t)

	// Add 2 extensions with 50 energy each (total available = 300 + 100 = 400).
	ext1 := &Entity{
		ID: "ext_1", Type: STRUCTURE_EXTENSION, Room: room.Name, X: spawn.X + 2, Y: spawn.Y,
		User: user, Hits: 1000, HitsMax: 1000, Energy: 50,
		Store:                 map[string]int{RESOURCE_ENERGY: 50},
		StoreCapacityResource: map[string]int{RESOURCE_ENERGY: 50},
	}
	ext2 := &Entity{
		ID: "ext_2", Type: STRUCTURE_EXTENSION, Room: room.Name, X: spawn.X + 3, Y: spawn.Y,
		User: user, Hits: 1000, HitsMax: 1000, Energy: 50,
		Store:                 map[string]int{RESOURCE_ENERGY: 50},
		StoreCapacityResource: map[string]int{RESOURCE_ENERGY: 50},
	}
	room.Entities[ext1.ID] = ext1
	room.Entities[ext2.ID] = ext2

	// Body costing 350 energy: 2xWORK (200) + 2xCARRY (100) + 1xMOVE (50) = 350.
	body := []string{WORK, WORK, CARRY, CARRY, MOVE}
	if code := w.QueueIntent(user, Intent{Type: IntentSpawnCreep, ActorID: spawn.ID, CreepName: "Harvester1", Body: body}); code != OK {
		t.Fatalf("spawnCreep failed: %d", code)
	}
	// Duplicate / busy checks.
	if code := w.QueueIntent(user, Intent{Type: IntentSpawnCreep, ActorID: spawn.ID, CreepName: "Harvester2", Body: []string{MOVE}}); code != ERR_BUSY {
		t.Fatalf("expected ERR_BUSY, got %d", code)
	}

	w.Step() // Starts spawning; deducts 300 from spawn + 50 from ext1.
	if spawn.SpawningInfo == nil {
		t.Fatal("expected spawn.SpawningInfo to be set")
	}
	if spawn.UsedCapacity(RESOURCE_ENERGY) != 0 || ext1.UsedCapacity(RESOURCE_ENERGY) != 0 || ext2.UsedCapacity(RESOURCE_ENERGY) != 50 {
		t.Fatalf("unexpected energy after spawn deduction: spawn=%d ext1=%d ext2=%d",
			spawn.UsedCapacity(RESOURCE_ENERGY), ext1.UsedCapacity(RESOURCE_ENERGY), ext2.UsedCapacity(RESOURCE_ENERGY))
	}

	// Verify spawning creep has Raw["spawning"] == true and spawn has Spawning map in Snapshot.
	snap := w.Snapshot(room.Name)
	foundSpawnMap, foundSpawningCreep := false, false
	for _, obj := range snap.Objects {
		if obj.ID == spawn.ID && len(obj.Spawning) > 0 {
			if _, ok := obj.Spawning["needTime"].(float64); ok {
				foundSpawnMap = true
			}
		}
		if obj.Type == TYPE_CREEP && obj.Name == "Harvester1" {
			if b, ok := obj.Raw["spawning"].(bool); ok && b {
				foundSpawningCreep = true
			}
		}
	}
	if !foundSpawnMap || !foundSpawningCreep {
		t.Fatalf("snapshot spawning metadata missing: spawnMap=%v spawningCreep=%v", foundSpawnMap, foundSpawningCreep)
	}

	// Advance 5*3 = 15 ticks for spawn completion.
	for i := 0; i < len(body)*CREEP_SPAWN_TIME; i++ {
		w.Step()
	}
	if spawn.SpawningInfo != nil {
		t.Fatal("expected spawn to finish")
	}

	var creep *Entity
	for _, e := range room.Entities {
		if e.Type == TYPE_CREEP && e.Name == "Harvester1" {
			creep = e
			break
		}
	}
	if creep == nil || creep.Spawning {
		t.Fatalf("expected live creep Harvester1, got %+v", creep)
	}
	if ChebyshevDist(spawn.X, spawn.Y, creep.X, creep.Y) != 1 {
		t.Fatalf("expected creep adjacent to spawn, got (%d,%d) vs (%d,%d)", creep.X, creep.Y, spawn.X, spawn.Y)
	}
	if creep.AgeTime != w.GameTime+CREEP_LIFE_TIME {
		t.Fatalf("expected AgeTime %d, got %d", w.GameTime+CREEP_LIFE_TIME, creep.AgeTime)
	}
}

func TestHarvestBuildUpgradeAndActions(t *testing.T) {
	w, room, user, spawn := setupTestRoom(t)

	// Find a source and controller.
	var src, ctrl *Entity
	for _, e := range room.Entities {
		if e.Type == TYPE_SOURCE && src == nil {
			src = e
		}
		if e.Type == STRUCTURE_CONTROLLER {
			ctrl = e
		}
	}

	// Place harvester adjacent to source.
	h := addTestCreep(w, room, user, "H1", src.X+1, src.Y, []string{WORK, WORK, CARRY, MOVE})
	if code := w.QueueIntent(user, Intent{Type: IntentHarvest, ActorID: h.ID, TargetID: src.ID}); code != OK {
		t.Fatalf("harvest failed: %d", code)
	}
	w.Step()
	if h.UsedCapacity(RESOURCE_ENERGY) != 4 || src.Energy != SOURCE_ENERGY_CAPACITY-4 {
		t.Fatalf("unexpected harvest result: creep=%d source=%d", h.UsedCapacity(RESOURCE_ENERGY), src.Energy)
	}
	if _, ok := h.ActionLog["harvest"]; !ok {
		t.Fatal("expected harvest in ActionLog")
	}

	// Test drop & pickup.
	if code := w.QueueIntent(user, Intent{Type: IntentDrop, ActorID: h.ID, ResourceType: RESOURCE_ENERGY, Amount: 4}); code != OK {
		t.Fatalf("drop failed: %d", code)
	}
	w.Step()
	var resEnt *Entity
	for _, e := range room.Entities {
		if e.Type == TYPE_RESOURCE {
			resEnt = e
			break
		}
	}
	if resEnt == nil || resEnt.Amount < 3 {
		t.Fatalf("expected dropped resource, got %+v", resEnt)
	}
	if code := w.QueueIntent(user, Intent{Type: IntentPickup, ActorID: h.ID, TargetID: resEnt.ID}); code != OK {
		t.Fatalf("pickup failed: %d", code)
	}
	w.Step()
	if h.UsedCapacity(RESOURCE_ENERGY) == 0 {
		t.Fatal("expected creep to pick up energy")
	}

	// Test transfer & withdraw with spawn.
	h.X, h.Y = spawn.X+1, spawn.Y
	spawn.SubStore(RESOURCE_ENERGY, 100)
	if code := w.QueueIntent(user, Intent{Type: IntentTransfer, ActorID: h.ID, TargetID: spawn.ID, ResourceType: RESOURCE_ENERGY}); code != OK {
		t.Fatalf("transfer failed: %d", code)
	}
	w.Step()
	if h.UsedCapacity(RESOURCE_ENERGY) != 0 {
		t.Fatal("expected creep energy transferred to spawn")
	}
	if code := w.QueueIntent(user, Intent{Type: IntentWithdraw, ActorID: h.ID, TargetID: spawn.ID, ResourceType: RESOURCE_ENERGY, Amount: 50}); code != OK {
		t.Fatalf("withdraw failed: %d", code)
	}
	w.Step()
	if h.UsedCapacity(RESOURCE_ENERGY) != 50 {
		t.Fatalf("expected 50 energy withdrawn, got %d", h.UsedCapacity(RESOURCE_ENERGY))
	}

	// Test CreateConstructionSite + build completion (road costs 300).
	siteID, code := w.CreateConstructionSite(user, room.Name, spawn.X+2, spawn.Y, STRUCTURE_ROAD)
	if code != OK || siteID == "" {
		t.Fatalf("CreateConstructionSite failed: %d", code)
	}
	// Attempt to build an extension at RCL 1 should fail with ERR_RCL_NOT_ENOUGH.
	if _, extCode := w.CreateConstructionSite(user, room.Name, spawn.X+3, spawn.Y, STRUCTURE_EXTENSION); extCode != ERR_RCL_NOT_ENOUGH {
		t.Fatalf("expected ERR_RCL_NOT_ENOUGH for extension at RCL 1, got %d", extCode)
	}

	builder := addTestCreep(w, room, user, "B1", spawn.X+2, spawn.Y+1, []string{
		WORK, WORK, WORK, WORK, WORK, WORK, WORK, WORK, WORK, WORK,
		CARRY, CARRY, CARRY, CARRY, CARRY, CARRY, MOVE,
	})
	builder.AddStore(RESOURCE_ENERGY, 300)
	for i := 0; i < 6; i++ {
		if c := w.QueueIntent(user, Intent{Type: IntentBuild, ActorID: builder.ID, TargetID: siteID}); c != OK {
			t.Fatalf("build tick %d failed: %d", i, c)
		}
		w.Step()
	}
	if _, stillSite := room.Entities[siteID]; stillSite {
		t.Fatal("expected construction site to complete and be removed")
	}
	if !room.HasRoad(spawn.X+2, spawn.Y) {
		t.Fatal("expected completed road at construction site tile")
	}

	// Test repair on the road.
	var road *Entity
	for _, e := range room.Entities {
		if e.Type == STRUCTURE_ROAD && e.X == spawn.X+2 && e.Y == spawn.Y {
			road = e
			break
		}
	}
	road.Hits = 4000
	builder.AddStore(RESOURCE_ENERGY, 50)
	if c := w.QueueIntent(user, Intent{Type: IntentRepair, ActorID: builder.ID, TargetID: road.ID}); c != OK {
		t.Fatalf("repair failed: %d", c)
	}
	w.Step()
	if road.Hits != 5000 {
		t.Fatalf("expected road repaired to 5000, got %d", road.Hits)
	}

	// Test upgradeController RCL 1 -> RCL 2 (requires 200 energy).
	upgrader := addTestCreep(w, room, user, "U1", ctrl.X+1, ctrl.Y, []string{
		WORK, WORK, WORK, WORK, WORK, WORK, WORK, WORK, WORK, WORK,
		CARRY, CARRY, CARRY, CARRY, MOVE,
	})
	upgrader.AddStore(RESOURCE_ENERGY, 200)
	for i := 0; i < 20; i++ {
		if c := w.QueueIntent(user, Intent{Type: IntentUpgradeController, ActorID: upgrader.ID, TargetID: ctrl.ID}); c != OK {
			t.Fatalf("upgrade tick %d failed: %d", i, c)
		}
		w.Step()
	}
	if ctrl.Level != 2 || ctrl.ProgressTotal != CONTROLLER_LEVELS[2] {
		t.Fatalf("expected controller RCL 2, got level=%d progressTotal=%d", ctrl.Level, ctrl.ProgressTotal)
	}

	// Now extension construction site is allowed at RCL 2!
	if _, extCode := w.CreateConstructionSite(user, room.Name, spawn.X+3, spawn.Y, STRUCTURE_EXTENSION); extCode != OK {
		t.Fatalf("expected OK for extension at RCL 2, got %d", extCode)
	}
}

func TestCombatTowersLinksAndPassives(t *testing.T) {
	w := NewWorld()
	room := &Room{Name: "W2N2", Entities: make(map[string]*Entity)}
	w.Rooms["W2N2"] = room
	alice := w.EnsureUser("Alice")
	invader := "2"

	// Place tower at (20,20) with 1000 energy.
	tower := &Entity{
		ID: "tower1", Type: STRUCTURE_TOWER, Room: "W2N2", X: 20, Y: 20,
		User: alice, Hits: 3000, HitsMax: 3000, Energy: 1000,
		Store:                 map[string]int{RESOURCE_ENERGY: 1000},
		StoreCapacityResource: map[string]int{RESOURCE_ENERGY: TOWER_CAPACITY},
	}
	room.Entities[tower.ID] = tower

	// Hostile creep at optimal range (23,20) dist=3 -> 600 damage!
	enemy := addTestCreep(w, room, invader, "Inv1", 23, 20, []string{TOUGH, TOUGH, TOUGH, TOUGH, TOUGH, TOUGH, ATTACK, MOVE})
	if code := w.QueueIntent(alice, Intent{Type: IntentTowerAttack, ActorID: tower.ID, TargetID: enemy.ID}); code != OK {
		t.Fatalf("tower attack failed: %d", code)
	}
	w.Step()
	if enemy.Hits != 200 || tower.UsedCapacity(RESOURCE_ENERGY) != 990 {
		t.Fatalf("expected enemy 200 hits and tower 990 energy, got hits=%d energy=%d", enemy.Hits, tower.UsedCapacity(RESOURCE_ENERGY))
	}
	if _, ok := tower.ActionLog["attack"]; !ok {
		t.Fatal("expected tower attack actionLog")
	}

	// Friendly creep at far range (40,20) dist=20 -> 25% of 400 = 100 heal!
	ally := addTestCreep(w, room, alice, "Ally1", 40, 20, []string{TOUGH, TOUGH, MOVE})
	ally.TakeDamage(150)
	if code := w.QueueIntent(alice, Intent{Type: IntentTowerHeal, ActorID: tower.ID, TargetID: ally.ID}); code != OK {
		t.Fatalf("tower heal failed: %d", code)
	}
	w.Step()
	if ally.Hits != 250 {
		t.Fatalf("expected ally healed by 100 to 250, got %d", ally.Hits)
	}

	// Tower repair on wall at mid range (32,20) dist=12.
	wall := &Entity{ID: "wall1", Type: STRUCTURE_WALL, Room: "W2N2", X: 32, Y: 20, Hits: 100, HitsMax: 300000000}
	room.Entities[wall.ID] = wall
	if code := w.QueueIntent(alice, Intent{Type: IntentTowerRepair, ActorID: tower.ID, TargetID: wall.ID}); code != OK {
		t.Fatalf("tower repair failed: %d", code)
	}
	w.Step()
	if wall.Hits <= 100 {
		t.Fatalf("expected wall repaired, got hits=%d", wall.Hits)
	}

	// Link transfer test.
	link1 := &Entity{
		ID: "link1", Type: STRUCTURE_LINK, Room: "W2N2", X: 10, Y: 10, User: alice,
		Hits: 1000, HitsMax: 1000, Energy: 500,
		Store:                 map[string]int{RESOURCE_ENERGY: 500},
		StoreCapacityResource: map[string]int{RESOURCE_ENERGY: LINK_CAPACITY},
	}
	link2 := &Entity{
		ID: "link2", Type: STRUCTURE_LINK, Room: "W2N2", X: 15, Y: 10, User: alice,
		Hits: 1000, HitsMax: 1000, Energy: 0,
		Store:                 map[string]int{RESOURCE_ENERGY: 0},
		StoreCapacityResource: map[string]int{RESOURCE_ENERGY: LINK_CAPACITY},
	}
	room.Entities[link1.ID] = link1
	room.Entities[link2.ID] = link2
	if code := w.QueueIntent(alice, Intent{Type: IntentLinkTransfer, ActorID: link1.ID, TargetID: link2.ID, Amount: 100}); code != OK {
		t.Fatalf("link transfer failed: %d", code)
	}
	w.Step()
	if link1.UsedCapacity(RESOURCE_ENERGY) != 400 || link2.UsedCapacity(RESOURCE_ENERGY) != 97 || link1.Cooldown != 5 {
		t.Fatalf("unexpected link state: l1=%d l2=%d cd=%d",
			link1.UsedCapacity(RESOURCE_ENERGY), link2.UsedCapacity(RESOURCE_ENERGY), link1.Cooldown)
	}

	// Creep melee/ranged combat, heal, dismantle, say.
	fighter := addTestCreep(w, room, alice, "Fighter", 24, 20, []string{ATTACK, RANGED_ATTACK, HEAL, WORK, CARRY, MOVE})
	w.QueueIntent(alice, Intent{Type: IntentAttack, ActorID: fighter.ID, TargetID: enemy.ID})
	w.QueueIntent(alice, Intent{Type: IntentRangedAttack, ActorID: fighter.ID, TargetID: enemy.ID})
	w.QueueIntent(alice, Intent{Type: IntentHeal, ActorID: fighter.ID, TargetID: fighter.ID})
	w.QueueIntent(alice, Intent{Type: IntentSay, ActorID: fighter.ID, Message: "gg"})
	w.Step()
	if fighter.SayText != "gg" || enemy.Hits != 160 {
		t.Fatalf("expected say=gg and enemy hits=160, got say=%q hits=%d", fighter.SayText, enemy.Hits)
	}

	// Creep death via AgeTime -> Tombstone -> Tombstone decay drops resources.
	dying := addTestCreep(w, room, alice, "Old", 5, 5, []string{CARRY, MOVE})
	dying.AddStore(RESOURCE_ENERGY, 25)
	dying.AgeTime = w.GameTime + 1
	w.Step()
	if _, exists := room.Entities[dying.ID]; exists {
		t.Fatal("expected dying creep to be removed")
	}
	var tomb *Entity
	for _, e := range room.Entities {
		if e.Type == TYPE_TOMBSTONE {
			tomb = e
			break
		}
	}
	if tomb == nil || tomb.UsedCapacity(RESOURCE_ENERGY) != 25 {
		t.Fatalf("expected tombstone with 25 energy, got %+v", tomb)
	}

	// Source regeneration & road decay.
	src := &Entity{ID: "src1", Type: TYPE_SOURCE, Room: "W2N2", X: 8, Y: 8, Energy: 100, EnergyCapacity: 3000, NextRegenerationTime: w.GameTime + 1}
	road := &Entity{ID: "r1", Type: STRUCTURE_ROAD, Room: "W2N2", X: 9, Y: 9, Hits: 5000, HitsMax: 5000, NextDecayTime: w.GameTime + 1}
	room.Entities[src.ID] = src
	room.Entities[road.ID] = road
	w.Step()
	if src.Energy != 3000 || src.NextRegenerationTime != 0 {
		t.Fatalf("expected source regenerated to 3000, got %d", src.Energy)
	}
	if road.Hits != 5000-ROAD_DECAY_AMOUNT {
		t.Fatalf("expected road decayed to %d, got %d", 5000-ROAD_DECAY_AMOUNT, road.Hits)
	}
}

func TestSnapshotFidelityAndConcurrency(t *testing.T) {
	w, room, user, spawn := setupTestRoom(t)
	w.SetStatus("Running")

	// Add a visual and a creep with an action log.
	w.AddVisual(room.Name, screeps.VisualOp{Type: "c", X: 25, Y: 25, R: 0.5, Fill: "#ff0000", Opac: 0.8})
	c := addTestCreep(w, room, user, "SnapCreep", spawn.X+1, spawn.Y, []string{WORK, CARRY, MOVE})
	c.AddStore(RESOURCE_ENERGY, 20)
	w.Step()

	snap := w.Snapshot(room.Name)
	if snap.Status != "Running" || snap.GameTime != 2 || len(snap.Visuals) != 1 {
		t.Fatalf("unexpected snapshot header: status=%q tick=%d visuals=%d", snap.Status, snap.GameTime, len(snap.Visuals))
	}

	checkedCreep, checkedSource, checkedCtrl, checkedMineral := false, false, false, false
	for _, obj := range snap.Objects {
		switch obj.Type {
		case TYPE_CREEP:
			checkedCreep = true
			if _, ok := obj.Raw["ageTime"].(float64); !ok {
				t.Fatal("creep Raw[ageTime] not float64")
			}
			if _, ok := obj.Raw["fatigue"].(float64); !ok {
				t.Fatal("creep Raw[fatigue] not float64")
			}
			if _, ok := obj.Raw["body"].([]any); !ok {
				t.Fatal("creep Raw[body] not []any")
			}
			if _, ok := obj.Raw["storeCapacity"].(float64); !ok {
				t.Fatal("creep Raw[storeCapacity] not float64")
			}
			if v, ok := obj.Store[RESOURCE_ENERGY].(float64); !ok || v != 20 {
				t.Fatalf("creep Store[energy] invalid: %#v", obj.Store)
			}
		case TYPE_SOURCE:
			checkedSource = true
			if _, ok := obj.Raw["energyCapacity"].(float64); !ok {
				t.Fatal("source Raw[energyCapacity] not float64")
			}
		case STRUCTURE_CONTROLLER:
			checkedCtrl = true
			if _, ok := obj.Raw["progress"].(float64); !ok {
				t.Fatal("controller Raw[progress] not float64")
			}
			if _, ok := obj.Raw["progressTotal"].(float64); !ok {
				t.Fatal("controller Raw[progressTotal] not float64")
			}
			if _, ok := obj.Raw["downgradeTime"].(float64); !ok {
				t.Fatal("controller Raw[downgradeTime] not float64")
			}
		case TYPE_MINERAL:
			checkedMineral = true
			if _, ok := obj.Raw["mineralType"].(string); !ok {
				t.Fatal("mineral Raw[mineralType] not string")
			}
			if _, ok := obj.Raw["mineralAmount"].(float64); !ok {
				t.Fatal("mineral Raw[mineralAmount] not float64")
			}
		}
	}
	if !checkedCreep || !checkedSource || !checkedCtrl || !checkedMineral {
		t.Fatalf("missing object types in snapshot: creep=%v src=%v ctrl=%v min=%v",
			checkedCreep, checkedSource, checkedCtrl, checkedMineral)
	}

	// Verify concurrent Snapshot() calls while Step() runs are race-free.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			w.Step()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_ = w.Snapshot(room.Name)
		}
	}()
	wg.Wait()
}

func TestIntentValidationErrors(t *testing.T) {
	w, room, user, spawn := setupTestRoom(t)
	other := w.EnsureUser("Eve")

	// Unknown actor & wrong owner.
	if code := w.QueueIntent(user, Intent{Type: IntentMove, ActorID: "nope", Direction: TOP}); code != ERR_INVALID_TARGET {
		t.Fatalf("expected ERR_INVALID_TARGET, got %d", code)
	}
	if code := w.QueueIntent(other, Intent{Type: IntentSpawnCreep, ActorID: spawn.ID, CreepName: "X", Body: []string{MOVE}}); code != ERR_NOT_OWNER {
		t.Fatalf("expected ERR_NOT_OWNER, got %d", code)
	}

	// Spawn validation errors.
	if code := w.QueueIntent(user, Intent{Type: IntentSpawnCreep, ActorID: spawn.ID, CreepName: "", Body: []string{MOVE}}); code != ERR_INVALID_ARGS {
		t.Fatalf("expected ERR_INVALID_ARGS for empty name, got %d", code)
	}
	if code := w.QueueIntent(user, Intent{Type: IntentSpawnCreep, ActorID: spawn.ID, CreepName: "BadBody", Body: []string{"laser"}}); code != ERR_INVALID_ARGS {
		t.Fatalf("expected ERR_INVALID_ARGS for bad body, got %d", code)
	}
	if code := w.QueueIntent(user, Intent{Type: IntentSpawnCreep, ActorID: spawn.ID, CreepName: "TooBig", Body: []string{CLAIM}}); code != ERR_NOT_ENOUGH_ENERGY {
		t.Fatalf("expected ERR_NOT_ENOUGH_ENERGY, got %d", code)
	}

	// Creep with only TOUGH part: missing bodyparts for all actions.
	c := addTestCreep(w, room, user, "Dummy", spawn.X+1, spawn.Y, []string{TOUGH})
	if code := w.QueueIntent(user, Intent{Type: IntentSpawnCreep, ActorID: spawn.ID, CreepName: "Dummy", Body: []string{MOVE}}); code != ERR_NAME_EXISTS {
		t.Fatalf("expected ERR_NAME_EXISTS, got %d", code)
	}
	if code := w.QueueIntent(user, Intent{Type: IntentMove, ActorID: c.ID, Direction: 99}); code != ERR_INVALID_ARGS {
		t.Fatalf("expected ERR_INVALID_ARGS for dir 99, got %d", code)
	}
	if code := w.QueueIntent(user, Intent{Type: IntentMove, ActorID: c.ID, Direction: TOP}); code != ERR_NO_BODYPART {
		t.Fatalf("expected ERR_NO_BODYPART for move, got %d", code)
	}
	if code := w.QueueIntent(user, Intent{Type: IntentHarvest, ActorID: c.ID, TargetID: spawn.ID}); code != ERR_NO_BODYPART {
		t.Fatalf("expected ERR_NO_BODYPART for harvest, got %d", code)
	}
	if code := w.QueueIntent(user, Intent{Type: IntentAttack, ActorID: c.ID, TargetID: spawn.ID}); code != ERR_NO_BODYPART {
		t.Fatalf("expected ERR_NO_BODYPART for attack, got %d", code)
	}
	if code := w.QueueIntent(user, Intent{Type: IntentRangedAttack, ActorID: c.ID, TargetID: spawn.ID}); code != ERR_NO_BODYPART {
		t.Fatalf("expected ERR_NO_BODYPART for rangedAttack, got %d", code)
	}
	if code := w.QueueIntent(user, Intent{Type: IntentHeal, ActorID: c.ID, TargetID: c.ID}); code != ERR_NO_BODYPART {
		t.Fatalf("expected ERR_NO_BODYPART for heal, got %d", code)
	}
	if code := w.QueueIntent(user, Intent{Type: IntentRangedHeal, ActorID: c.ID, TargetID: c.ID}); code != ERR_NO_BODYPART {
		t.Fatalf("expected ERR_NO_BODYPART for rangedHeal, got %d", code)
	}
	if code := w.QueueIntent(user, Intent{Type: IntentDismantle, ActorID: c.ID, TargetID: spawn.ID}); code != ERR_NO_BODYPART {
		t.Fatalf("expected ERR_NO_BODYPART for dismantle, got %d", code)
	}
	if code := w.QueueIntent(user, Intent{Type: IntentDrop, ActorID: c.ID}); code != ERR_NOT_ENOUGH_RESOURCES {
		t.Fatalf("expected ERR_NOT_ENOUGH_RESOURCES for drop, got %d", code)
	}
	if code := w.QueueIntent(user, Intent{Type: "unknownIntent", ActorID: c.ID}); code != ERR_INVALID_ARGS {
		t.Fatalf("expected ERR_INVALID_ARGS for unknown intent, got %d", code)
	}

	// CreateConstructionSite edge cases.
	if _, code := w.CreateConstructionSite(user, "NOPE", 10, 10, STRUCTURE_ROAD); code != ERR_INVALID_ARGS {
		t.Fatalf("expected ERR_INVALID_ARGS for unknown room, got %d", code)
	}
	if _, code := w.CreateConstructionSite(user, room.Name, 0, 0, STRUCTURE_ROAD); code != ERR_INVALID_TARGET {
		t.Fatalf("expected ERR_INVALID_TARGET for border tile, got %d", code)
	}
	if _, code := w.CreateConstructionSite(user, room.Name, 25, 25, "badStruct"); code != ERR_INVALID_ARGS {
		t.Fatalf("expected ERR_INVALID_ARGS for bad structure type, got %d", code)
	}
	if _, code := w.CreateConstructionSite(user, room.Name, spawn.X, spawn.Y, STRUCTURE_ROAD); code != ERR_INVALID_TARGET {
		t.Fatalf("expected ERR_INVALID_TARGET on spawn tile, got %d", code)
	}
	if capAvail := room.EnergyCapacityAvailable(user); capAvail != SPAWN_ENERGY_CAPACITY {
		t.Fatalf("expected EnergyCapacityAvailable %d, got %d", SPAWN_ENERGY_CAPACITY, capAvail)
	}
	if ents := room.EntitiesAt(spawn.X, spawn.Y); len(ents) != 1 || ents[0].ID != spawn.ID {
		t.Fatalf("unexpected EntitiesAt spawn tile: %+v", ents)
	}
}

func TestDismantleRangedHealMineralDecayAndSites(t *testing.T) {
	w, room, user, _ := setupTestRoom(t)

	// Upgrade controller to RCL 8 so all structure types can be constructed.
	ctrl := room.FindController()
	ctrl.Level = 8
	ctrl.Progress = 0
	ctrl.ProgressTotal = 0

	// Test completing multiple structure construction sites via completeConstructionSite.
	types := []string{STRUCTURE_EXTENSION, STRUCTURE_TOWER, STRUCTURE_LINK, STRUCTURE_STORAGE, STRUCTURE_CONTAINER, STRUCTURE_RAMPART}
	worker := addTestCreep(w, room, user, "SuperBuilder", 20, 20, []string{WORK, WORK, CARRY, HEAL, MOVE})
	worker.StoreCapacity = 50000
	for i, st := range types {
		x, y := 19+i, 21
		room.Terrain[y*RoomSize+x] = TERRAIN_MASK_PLAIN
		siteID, code := w.CreateConstructionSite(user, room.Name, x, y, st)
		if code != OK {
			t.Fatalf("CreateConstructionSite %s failed: %d", st, code)
		}
		site := room.Entities[siteID]
		site.Progress = site.ProgressTotal - 1
		worker.X, worker.Y = x, y-1
		worker.AddStore(RESOURCE_ENERGY, 10)
		if c := w.QueueIntent(user, Intent{Type: IntentBuild, ActorID: worker.ID, TargetID: siteID}); c != OK {
			t.Fatalf("build %s failed: %d", st, c)
		}
		w.Step()
		if _, exists := room.Entities[siteID]; exists {
			t.Fatalf("expected site %s to complete", st)
		}
	}

	// Test dismantle on a wall & rangedHeal on an injured creep.
	wall := &Entity{ID: "w_dismantle", Type: STRUCTURE_WALL, Room: room.Name, X: worker.X + 1, Y: worker.Y, Hits: 200, HitsMax: 300000000}
	room.Entities[wall.ID] = wall
	injured := addTestCreep(w, room, user, "Injured", worker.X+2, worker.Y, []string{TOUGH, MOVE})
	injured.TakeDamage(50)

	if c := w.QueueIntent(user, Intent{Type: IntentDismantle, ActorID: worker.ID, TargetID: wall.ID}); c != OK {
		t.Fatalf("dismantle failed: %d", c)
	}
	if c := w.QueueIntent(user, Intent{Type: IntentRangedHeal, ActorID: worker.ID, TargetID: injured.ID}); c != OK {
		t.Fatalf("rangedHeal failed: %d", c)
	}
	w.Step()
	if wall.Hits != 100 || injured.Hits != 154 {
		t.Fatalf("unexpected dismantle/rangedHeal: wall=%d injured=%d", wall.Hits, injured.Hits)
	}

	// Test mineral harvesting.
	var minEnt *Entity
	for _, e := range room.Entities {
		if e.Type == TYPE_MINERAL {
			minEnt = e
			break
		}
	}
	worker.X, worker.Y = minEnt.X+1, minEnt.Y
	prevAmt := minEnt.MineralAmount
	if c := w.QueueIntent(user, Intent{Type: IntentHarvest, ActorID: worker.ID, TargetID: minEnt.ID}); c != OK {
		t.Fatalf("mineral harvest failed: %d", c)
	}
	w.Step()
	if minEnt.MineralAmount != prevAmt-2 || worker.UsedCapacity(minEnt.MineralType) != 2 {
		t.Fatalf("unexpected mineral harvest: rem=%d creep=%d", minEnt.MineralAmount, worker.UsedCapacity(minEnt.MineralType))
	}

	// Test container, rampart, tombstone decay, and controller downgrade.
	container := &Entity{ID: "cont1", Type: STRUCTURE_CONTAINER, Room: room.Name, X: 25, Y: 26, Hits: 10000, HitsMax: 250000, NextDecayTime: w.GameTime + 1}
	rampart := &Entity{ID: "ramp1", Type: STRUCTURE_RAMPART, Room: room.Name, X: 25, Y: 27, User: user, Hits: 300, HitsMax: 300000000, NextDecayTime: w.GameTime + 1}
	tomb := &Entity{ID: "tomb1", Type: TYPE_TOMBSTONE, Room: room.Name, X: 25, Y: 28, DecayTime: w.GameTime + 1, Store: map[string]int{RESOURCE_ENERGY: 40}}
	room.Entities[container.ID] = container
	room.Entities[rampart.ID] = rampart
	room.Entities[tomb.ID] = tomb
	ctrl.DowngradeTime = w.GameTime + 1

	w.Step()
	if container.Hits != 10000-CONTAINER_DECAY {
		t.Fatalf("expected container decay, got hits=%d", container.Hits)
	}
	if _, exists := room.Entities[rampart.ID]; exists {
		t.Fatal("expected 300-hit rampart to decay and be destroyed")
	}
	if _, exists := room.Entities[tomb.ID]; exists {
		t.Fatal("expected tombstone to decay")
	}
	if ctrl.Level != 7 {
		t.Fatalf("expected controller downgrade 8->7, got %d", ctrl.Level)
	}

	// Test SearchPath incomplete (MaxOps=2) and FindDirection toward impassable mineral.
	inc := SearchPath(room, 5, 5, []Goal{{X: 45, Y: 45, Range: 0}}, PathOpts{MaxOps: 2})
	if !inc.Incomplete {
		t.Fatal("expected incomplete path with MaxOps=2")
	}
	if d := FindDirection(room, minEnt.X+3, minEnt.Y, minEnt.X, minEnt.Y, true); d != LEFT {
		t.Fatalf("expected LEFT (%d) toward mineral, got %d", LEFT, d)
	}
}

func TestCrossRoomMovement(t *testing.T) {
	w := NewWorld()
	south := &Room{Name: "W9N7", Entities: make(map[string]*Entity)}
	north := &Room{Name: "W9N8", Entities: make(map[string]*Entity)}
	w.Rooms["W9N7"] = south
	w.Rooms["W9N8"] = north

	user := w.EnsureUser("ScoutUser")
	c := addTestCreep(w, south, user, "S1", 25, 1, []string{MOVE})

	// Move north onto y=0 -> should transition to W9N8 at (25, 49).
	if code := w.QueueIntent(user, Intent{Type: IntentMove, ActorID: c.ID, Direction: TOP}); code != OK {
		t.Fatalf("queue move failed: %d", code)
	}
	w.Step()

	if c.Room != "W9N8" || c.X != 25 || c.Y != RoomSize-1 {
		t.Fatalf("expected creep in W9N8@(25,49), got %s@(%d,%d)", c.Room, c.X, c.Y)
	}
	if _, inSouth := south.Entities[c.ID]; inSouth {
		t.Fatal("creep should have been removed from W9N7")
	}
	if _, inNorth := north.Entities[c.ID]; !inNorth {
		t.Fatal("creep should exist in W9N8")
	}

	// Next tick with no move intent should NOT bounce back to W9N7.
	w.Step()
	if c.Room != "W9N8" || c.X != 25 || c.Y != RoomSize-1 {
		t.Fatalf("creep bounced unexpectedly: %s@(%d,%d)", c.Room, c.X, c.Y)
	}
}
