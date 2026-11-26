package lua

import (
	"testing"

	"go-screeps/bots"
	"go-screeps/internal/engine"
)

func TestBotSummariesAndEvents(t *testing.T) {
	w := engine.NewWorld()
	w.GenerateRoom("W1N1", 7)
	runner := NewLocalRunner(w, "W1N1")
	defer runner.Close()

	if err := runner.AddEmbeddedBot("StarterTest", "W1N1", bots.StarterFS, "starter"); err != nil {
		t.Fatalf("AddEmbeddedBot: %v", err)
	}
	for i := 0; i < 30; i++ {
		runner.StepTick()
	}

	sums := runner.BotSummaries()
	if len(sums) != 1 {
		t.Fatalf("expected 1 bot summary, got %d", len(sums))
	}
	if sums[0].Username != "StarterTest" || sums[0].HomeRoom != "W1N1" || sums[0].RCL < 1 {
		t.Fatalf("unexpected bot summary: %+v", sums[0])
	}
	if sums[0].PlanSummary == "" {
		t.Fatalf("expected non-empty PlanSummary after 30 ticks; summary=%+v logs=%v", sums[0], runner.Logs())
	}
	if sums[0].NextSpawn == "" {
		t.Fatalf("expected non-empty NextSpawn telemetry; summary=%+v", sums[0])
	}
	if len(sums[0].DesiredRoles) == 0 {
		t.Fatalf("expected non-empty DesiredRoles telemetry; summary=%+v", sums[0])
	}
}

func TestParallelBotVMsNoRace(t *testing.T) {
	w := engine.NewWorld()
	w.GenerateRoom("W9N7", 11)
	w.GenerateRoom("W9N8", 22)
	runner := NewLocalRunner(w, "W9N7")
	defer runner.Close()

	if err := runner.AddEmbeddedBot("Hivemind", "W9N7", bots.StarterFS, "starter"); err != nil {
		t.Fatalf("AddEmbeddedBot starter: %v", err)
	}
	if err := runner.AddEmbeddedBot("RaiderBot", "W9N8", bots.RaiderFS, "raider"); err != nil {
		t.Fatalf("AddEmbeddedBot raider: %v", err)
	}
	for i := 0; i < 40; i++ {
		runner.StepTick()
	}
	sums := runner.BotSummaries()
	if len(sums) != 2 {
		t.Fatalf("expected 2 bot summaries, got %d", len(sums))
	}
}
