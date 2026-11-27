package main

import (
	"flag"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"go-screeps/bots"
	"go-screeps/internal/engine"
	"go-screeps/internal/lua"
)

func main() {
	dbPath := flag.String("db", "", "Optional path to Screeps db.json (empty generates a procedural 5x5 world)")
	seed := flag.Int64("seed", 42, "Procedural world seed when -db is empty")
	ticks := flag.Int("ticks", 10000, "Number of ticks to simulate")
	reportEvery := flag.Int("report", 2000, "Print status summary every N ticks")
	homeRoom := flag.String("home", "W9N7", "Starter bot room")
	rivalRoom := flag.String("rival", "W9N8", "Raider bot room")
	flag.Parse()

	world := engine.NewWorld()
	if strings.TrimSpace(*dbPath) != "" {
		if err := world.ImportFullDBJSON(*dbPath); err != nil {
			log.Fatalf("ImportFullDBJSON(%q): %v", *dbPath, err)
		}
	} else {
		ensureNeighborhood(world, *homeRoom, *seed, 2)
		if world.GetRoom(*rivalRoom) == nil {
			world.GenerateRoom(*rivalRoom, roomSeed(*seed, *rivalRoom))
		}
	}

	runner := lua.NewLocalRunner(world, *homeRoom)
	defer runner.Close()

	_, homeOwner := world.RoomOwner(*homeRoom)
	if homeOwner == "" {
		homeOwner = "Hivemind"
	}
	if err := runner.AttachEmbeddedBot(homeOwner, *homeRoom, bots.StarterFS, "starter"); err != nil {
		log.Fatalf("attach starter bot: %v", err)
	}

	_, rivalOwner := world.RoomOwner(*rivalRoom)
	if rivalOwner == "" || rivalOwner == homeOwner {
		rivalOwner = "RaiderBot"
	}
	if err := runner.AttachEmbeddedBot(rivalOwner, *rivalRoom, bots.RaiderFS, "raider"); err != nil {
		log.Fatalf("attach raider bot: %v", err)
	}

	startTick := world.GameTime
	startWall := time.Now()
	fmt.Printf("=== Headless Match: %s (%s, starter) vs %s (%s, raider) ===\n", homeOwner, *homeRoom, rivalOwner, *rivalRoom)
	fmt.Printf("Start tick: %d | Target ticks: %d\n\n", startTick, *ticks)

	printSnapshot := func(step int) {
		elapsed := time.Since(startWall).Seconds()
		tps := 0.0
		if elapsed > 0 {
			tps = float64(step) / elapsed
		}
		fmt.Printf("--- Tick %d (+%d, %.0f t/s) ---\n", world.GameTime, step, tps)
		for _, bs := range runner.BotSummaries() {
			spawnHP, extCount, twrCount, ctrlProg := roomStats(world, bs.HomeRoom)
			roles := formatRoles(bs.RoleCounts, bs.DesiredRoles)
			fmt.Printf("  %-10s [%s] RCL %d (%s) | SpawnHP:%d Ext:%d Twr:%d | En:%d/%d | %s\n",
				bs.Username, bs.HomeRoom, bs.RCL, ctrlProg, spawnHP, extCount, twrCount,
				bs.EnergyAvail, bs.EnergyCap, bs.NextSpawn)
			fmt.Printf("             Roles: %s", roles)
			if bs.RemoteRoom != "" {
				fmt.Printf(" | Remote:%s", bs.RemoteRoom)
			}
			if bs.ThreatLevel > 0 || bs.Hostiles > 0 {
				fmt.Printf(" | THREAT L%d (%d)", bs.ThreatLevel, bs.Hostiles)
			}
			if bs.RaidTarget != "" {
				fmt.Printf(" | Raid->%s (active=%v)", bs.RaidTarget, bs.RaidActive)
			}
			fmt.Println()
		}
	}

	for i := 1; i <= *ticks; i++ {
		runner.StepTick()
		if *reportEvery > 0 && (i == 1 || i%*reportEvery == 0 || i == *ticks) {
			printSnapshot(i)
		}
	}

	elapsed := time.Since(startWall)
	fmt.Printf("\n=== Finished %d ticks in %s (%.0f t/s) ===\n", *ticks, elapsed.Round(time.Millisecond), float64(*ticks)/elapsed.Seconds())

	events := world.RecentEvents(30)
	if len(events) > 0 {
		fmt.Println("\n=== Recent Colony Events (newest first, up to 30) ===")
		for _, ev := range events {
			fmt.Printf("  t%d [%s] (%s) %s\n", ev.Tick, ev.Room, ev.Kind, ev.Message)
		}
	}

	logs := runner.Logs()
	if len(logs) > 0 {
		fmt.Printf("\n=== Console Logs (%d total, showing last 10) ===\n", len(logs))
		start := 0
		if len(logs) > 10 {
			start = len(logs) - 10
		}
		for _, line := range logs[start:] {
			fmt.Println(" ", line)
		}
	} else {
		fmt.Println("\n=== Console Logs: 0 errors ===")
	}
}

func roomSeed(baseSeed int64, roomName string) int64 {
	h := baseSeed*1099511628211 ^ 1469598103934665603
	for i := 0; i < len(roomName); i++ {
		h ^= int64(roomName[i])
		h *= 1099511628211
	}
	return h
}

func ensureNeighborhood(w *engine.World, centerRoom string, baseSeed int64, radius int) {
	if w.GetRoom(centerRoom) == nil {
		w.GenerateRoom(centerRoom, roomSeed(baseSeed, centerRoom))
	}
	for dy := -radius; dy <= radius; dy++ {
		for dx := -radius; dx <= radius; dx++ {
			if dx == 0 && dy == 0 {
				continue
			}
			rn, ok := engine.AdjacentRoomName(centerRoom, dx, dy)
			if !ok {
				continue
			}
			if w.GetRoom(rn) == nil {
				w.GenerateRoom(rn, roomSeed(baseSeed, rn))
			}
		}
	}
}

func roomStats(w *engine.World, roomName string) (spawnHP, extCount, twrCount int, ctrlProg string) {
	w.RLock()
	defer w.RUnlock()
	r := w.Rooms[roomName]
	if r == nil {
		return 0, 0, 0, "-"
	}
	for _, e := range r.Entities {
		switch e.Type {
		case engine.STRUCTURE_SPAWN:
			spawnHP += e.Hits
		case engine.STRUCTURE_EXTENSION:
			extCount++
		case engine.STRUCTURE_TOWER:
			twrCount++
		case engine.STRUCTURE_CONTROLLER:
			if e.ProgressTotal > 0 {
				ctrlProg = fmt.Sprintf("%d/%d", e.Progress, e.ProgressTotal)
			} else {
				ctrlProg = fmt.Sprintf("%d", e.Progress)
			}
		}
	}
	return spawnHP, extCount, twrCount, ctrlProg
}

func formatRoles(cur, want map[string]int) string {
	set := make(map[string]struct{})
	for k := range cur {
		set[k] = struct{}{}
	}
	for k, v := range want {
		if v > 0 {
			set[k] = struct{}{}
		}
	}
	names := make([]string, 0, len(set))
	for k := range set {
		names = append(names, k)
	}
	sort.Strings(names)
	var parts []string
	for _, k := range names {
		if w, ok := want[k]; ok && w > 0 {
			parts = append(parts, fmt.Sprintf("%s:%d/%d", k, cur[k], w))
		} else if cur[k] > 0 {
			parts = append(parts, fmt.Sprintf("%s:%d", k, cur[k]))
		}
	}
	return strings.Join(parts, " ")
}
