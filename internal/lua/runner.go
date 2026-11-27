package lua

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"strings"
	"sync"
	"time"

	rt "github.com/arnodel/golua/runtime"

	"go-screeps/internal/engine"
	"go-screeps/internal/screeps"
)

// EmpireStats holds high-level telemetry for the top Empire bar (UC-4.2).
type EmpireStats struct {
	Colonies    int
	Rooms       int
	Creeps      int
	GCLLevel    int
	GCLProgress int
	GCLTotal    int
}

// LocalRunner orchestrates an engine.World and one or more sandboxed Lua BotVMs.
type LocalRunner struct {
	world *engine.World

	stepMu sync.Mutex
	bots   []*BotVM

	stateMu     sync.RWMutex
	activeRoom  string
	tickPeriod  time.Duration
	paused      bool
	savePath    string
	saveEvery   int
	summaries   []BotSummary
	viewerState *ViewerState
	loopCancel  context.CancelFunc

	loopWg   sync.WaitGroup
	periodCh chan time.Duration
}

// NewLocalRunner creates a LocalRunner driving world, initially viewing activeRoom.
func NewLocalRunner(world *engine.World, activeRoom string) *LocalRunner {
	if world == nil {
		world = engine.NewWorld()
	}
	return &LocalRunner{
		world:      world,
		activeRoom: strings.ToUpper(strings.TrimSpace(activeRoom)),
		tickPeriod: 200 * time.Millisecond,
		periodCh:   make(chan time.Duration, 1),
	}
}

// World returns the underlying simulation world.
func (r *LocalRunner) World() *engine.World {
	return r.world
}

// Bots returns a copy of the registered BotVM slice.
func (r *LocalRunner) Bots() []*BotVM {
	r.stepMu.Lock()
	defer r.stepMu.Unlock()
	out := make([]*BotVM, len(r.bots))
	copy(out, r.bots)
	return out
}

// AddEmbeddedBot spawns username in roomName and loads its Lua scripts from fsys/subdir.
func (r *LocalRunner) AddEmbeddedBot(username, roomName string, fsys fs.FS, subdir string) error {
	r.stepMu.Lock()
	defer r.stepMu.Unlock()

	roomName = strings.ToUpper(strings.TrimSpace(roomName))
	if _, err := r.world.SpawnBot(username, roomName); err != nil {
		return fmt.Errorf("spawn bot %q in %q: %w", username, roomName, err)
	}
	userID := r.world.EnsureUser(username)

	vm := NewBotVM(username, userID)
	if err := vm.LoadFS(fsys, subdir); err != nil {
		vm.Close()
		return fmt.Errorf("load embedded bot %q: %w", username, err)
	}
	r.bots = append(r.bots, vm)

	r.stateMu.Lock()
	if r.activeRoom == "" {
		r.activeRoom = roomName
	}
	r.stateMu.Unlock()

	return nil
}

// AddDirBot spawns username in roomName and loads its Lua scripts from a local directory with hot-reload.
func (r *LocalRunner) AddDirBot(username, roomName, dir string) error {
	r.stepMu.Lock()
	defer r.stepMu.Unlock()

	roomName = strings.ToUpper(strings.TrimSpace(roomName))
	if _, err := r.world.SpawnBot(username, roomName); err != nil {
		return fmt.Errorf("spawn bot %q in %q: %w", username, roomName, err)
	}
	userID := r.world.EnsureUser(username)

	vm := NewBotVM(username, userID)
	if err := vm.LoadDir(dir); err != nil {
		vm.Close()
		return fmt.Errorf("load dir bot %q: %w", username, err)
	}
	r.bots = append(r.bots, vm)

	r.stateMu.Lock()
	if r.activeRoom == "" {
		r.activeRoom = roomName
	}
	r.stateMu.Unlock()

	return nil
}

// existingBotUserInRoom checks whether username already owns a spawn or controller in roomName.
func (r *LocalRunner) existingBotUserInRoom(username, roomName string) (string, bool) {
	r.world.RLock()
	defer r.world.RUnlock()

	var userID string
	if r.world.UserByName != nil {
		userID = r.world.UserByName[username]
	}
	if userID == "" {
		for id, name := range r.world.Users {
			if name == username {
				userID = id
				break
			}
		}
	}
	if userID == "" {
		return "", false
	}

	room := r.world.Rooms[roomName]
	if room == nil {
		return "", false
	}
	for _, e := range room.Entities {
		if e.User == userID && (e.Type == engine.STRUCTURE_SPAWN || e.Type == engine.STRUCTURE_CONTROLLER) {
			return userID, true
		}
	}
	return "", false
}

// AttachEmbeddedBot attaches an embedded Lua bot to an existing owned room/user (e.g. imported via
// ImportFullDBJSON) without overwriting existing structures, or falls back to SpawnBot if unowned.
func (r *LocalRunner) AttachEmbeddedBot(username, roomName string, fsys fs.FS, subdir string) error {
	r.stepMu.Lock()
	defer r.stepMu.Unlock()

	roomName = strings.ToUpper(strings.TrimSpace(roomName))
	userID, exists := r.existingBotUserInRoom(username, roomName)
	if !exists {
		if _, err := r.world.SpawnBot(username, roomName); err != nil {
			return fmt.Errorf("spawn bot %q in %q: %w", username, roomName, err)
		}
		userID = r.world.EnsureUser(username)
	}

	vm := NewBotVM(username, userID)
	if err := vm.LoadFS(fsys, subdir); err != nil {
		vm.Close()
		return fmt.Errorf("load embedded bot %q: %w", username, err)
	}
	r.bots = append(r.bots, vm)

	r.stateMu.Lock()
	if r.activeRoom == "" {
		r.activeRoom = roomName
	}
	r.stateMu.Unlock()

	return nil
}

// AttachDirBot attaches a disk-loaded Lua bot (with hot-reload) to an existing owned room/user
// without overwriting existing structures, or falls back to SpawnBot if unowned.
func (r *LocalRunner) AttachDirBot(username, roomName, dir string) error {
	r.stepMu.Lock()
	defer r.stepMu.Unlock()

	roomName = strings.ToUpper(strings.TrimSpace(roomName))
	userID, exists := r.existingBotUserInRoom(username, roomName)
	if !exists {
		if _, err := r.world.SpawnBot(username, roomName); err != nil {
			return fmt.Errorf("spawn bot %q in %q: %w", username, roomName, err)
		}
		userID = r.world.EnsureUser(username)
	}

	vm := NewBotVM(username, userID)
	if err := vm.LoadDir(dir); err != nil {
		vm.Close()
		return fmt.Errorf("load dir bot %q: %w", username, err)
	}
	r.bots = append(r.bots, vm)

	r.stateMu.Lock()
	if r.activeRoom == "" {
		r.activeRoom = roomName
	}
	r.stateMu.Unlock()

	return nil
}

// StepTick runs all bots for the current tick and then advances the simulation world by 1 tick.
func (r *LocalRunner) StepTick() {
	r.stepMu.Lock()
	defer r.stepMu.Unlock()

	if len(r.bots) == 1 {
		_ = r.bots[0].RunTick(r.world)
	} else if len(r.bots) > 1 {
		var wg sync.WaitGroup
		wg.Add(len(r.bots))
		for _, vm := range r.bots {
			go func(b *BotVM) {
				defer wg.Done()
				_ = b.RunTick(r.world)
			}(vm)
		}
		wg.Wait()
	}
	r.world.Step()
	r.refreshSummariesLocked()

	r.stateMu.RLock()
	savePath := r.savePath
	saveEvery := r.saveEvery
	r.stateMu.RUnlock()
	if savePath != "" && saveEvery > 0 {
		r.world.RLock()
		tick := r.world.GameTime
		r.world.RUnlock()
		if tick%int64(saveEvery) == 0 {
			if err := r.saveStateLocked(savePath); err != nil {
				log.Printf("save state %q at tick %d: %v", savePath, tick, err)
			}
		}
	}
}

// Start launches the background simulation loop, calling onUpdate after each stepped tick.
// The loop stops when ctx is cancelled or Close() is called.
func (r *LocalRunner) Start(ctx context.Context, tickPeriod time.Duration, onUpdate func()) {
	if tickPeriod <= 0 {
		tickPeriod = 200 * time.Millisecond
	}
	r.SetTickPeriod(tickPeriod)

	loopCtx, cancel := context.WithCancel(ctx)
	r.stateMu.Lock()
	if r.loopCancel != nil {
		r.loopCancel()
	}
	r.loopCancel = cancel
	r.stateMu.Unlock()

	r.loopWg.Add(1)
	go func() {
		defer r.loopWg.Done()
		defer cancel()

		r.stateMu.RLock()
		period := r.tickPeriod
		r.stateMu.RUnlock()

		ticker := time.NewTicker(period)
		defer ticker.Stop()

		for {
			// Uncapped "Max" mode (period <= 2ms): run as fast as CPU allows without OS timer coalescing.
			if period <= 2*time.Millisecond {
				select {
				case <-loopCtx.Done():
					return
				case newPeriod := <-r.periodCh:
					if newPeriod > 0 {
						period = newPeriod
						ticker.Reset(period)
					}
					continue
				default:
				}
				if r.Paused() {
					timer := time.NewTimer(25 * time.Millisecond)
					select {
					case <-loopCtx.Done():
						timer.Stop()
						return
					case newPeriod := <-r.periodCh:
						timer.Stop()
						if newPeriod > 0 {
							period = newPeriod
							ticker.Reset(period)
						}
					case <-timer.C:
					}
					continue
				}
				r.StepTick()
				if onUpdate != nil {
					onUpdate()
				}
				continue
			}

			select {
			case <-loopCtx.Done():
				return
			case newPeriod := <-r.periodCh:
				if newPeriod > 0 {
					period = newPeriod
					ticker.Reset(period)
				}
			case <-ticker.C:
				if r.Paused() {
					continue
				}
				r.StepTick()
				if onUpdate != nil {
					onUpdate()
				}
			}
		}
	}()
}

// SetTickPeriod updates the simulation tick interval.
func (r *LocalRunner) SetTickPeriod(d time.Duration) {
	if d <= 0 {
		return
	}
	r.stateMu.Lock()
	r.tickPeriod = d
	r.stateMu.Unlock()

	select {
	case r.periodCh <- d:
	default:
		// Replace any unread pending update.
		select {
		case <-r.periodCh:
		default:
		}
		select {
		case r.periodCh <- d:
		default:
		}
	}
}

// SetPaused pauses or resumes the background simulation loop.
func (r *LocalRunner) SetPaused(paused bool) {
	r.stateMu.Lock()
	r.paused = paused
	r.stateMu.Unlock()
}

// Paused reports whether the background simulation loop is paused.
func (r *LocalRunner) Paused() bool {
	r.stateMu.RLock()
	defer r.stateMu.RUnlock()
	return r.paused
}

// SwitchRoom changes the active room returned by Snapshot().
func (r *LocalRunner) SwitchRoom(room string) {
	r.stateMu.Lock()
	r.activeRoom = strings.ToUpper(strings.TrimSpace(room))
	r.stateMu.Unlock()
}

// EmpireStats returns high-level colony, room, creep, and primary bot GCL telemetry.
func (r *LocalRunner) EmpireStats() EmpireStats {
	r.stepMu.Lock()
	var primaryUser string
	colonies := len(r.bots)
	if len(r.bots) > 0 {
		primaryUser = r.bots[0].UserID
	}
	r.stepMu.Unlock()

	r.world.RLock()
	defer r.world.RUnlock()

	st := EmpireStats{
		Colonies: colonies,
		Rooms:    len(r.world.Rooms),
		GCLLevel: 1,
		GCLTotal: engine.GCL_MULTIPLY,
	}
	for _, rm := range r.world.Rooms {
		for _, e := range rm.Entities {
			if e.Type == engine.TYPE_CREEP {
				st.Creeps++
			}
		}
	}
	if primaryUser != "" {
		st.GCLLevel, st.GCLProgress, st.GCLTotal = r.world.UserGCLInfoLocked(primaryUser)
	}
	return st
}

// FindEntityRoom returns the room name currently containing entityID (used for cross-room creep following).
func (r *LocalRunner) FindEntityRoom(entityID string) string {
	if entityID == "" {
		return ""
	}
	r.world.RLock()
	defer r.world.RUnlock()
	for name, rm := range r.world.Rooms {
		if _, ok := rm.Entities[entityID]; ok {
			return name
		}
	}
	return ""
}

// CreepMemoryJSON returns a compact JSON representation of Memory.creeps[creepName] for userID.
func (r *LocalRunner) CreepMemoryJSON(userID, creepName string) string {
	if userID == "" || creepName == "" {
		return ""
	}
	r.stepMu.Lock()
	defer r.stepMu.Unlock()
	for _, vm := range r.bots {
		if vm.UserID != userID {
			continue
		}
		memTbl, ok := vm.screepsMod.Get(rt.StringValue("Memory")).TryTable()
		if !ok {
			return ""
		}
		creepsTbl, ok := memTbl.Get(rt.StringValue("creeps")).TryTable()
		if !ok {
			return ""
		}
		cmVal := creepsTbl.Get(rt.StringValue(creepName))
		if cmVal.IsNil() {
			return ""
		}
		visited := make(map[*rt.Table]bool)
		budget := 256
		goVal, ok := luaValueToGo(cmVal, visited, 0, &budget)
		if !ok {
			return ""
		}
		raw, err := json.Marshal(goVal)
		if err != nil {
			return ""
		}
		return string(raw)
	}
	return ""
}

// Snapshot returns an immutable RoomSnapshot of the currently active room.
func (r *LocalRunner) Snapshot() screeps.RoomSnapshot {
	r.stateMu.RLock()
	room := r.activeRoom
	r.stateMu.RUnlock()
	return r.world.Snapshot(room)
}

// Logs aggregates buffered log lines from all registered bots.
func (r *LocalRunner) Logs() []string {
	r.stepMu.Lock()
	bots := make([]*BotVM, len(r.bots))
	copy(bots, r.bots)
	r.stepMu.Unlock()

	var out []string
	for _, vm := range bots {
		out = append(out, vm.Logs()...)
	}
	return out
}

// Close stops any background simulation loop, persists state (if configured), and shuts down all BotVMs.
func (r *LocalRunner) Close() {
	r.stateMu.Lock()
	cancel := r.loopCancel
	r.loopCancel = nil
	r.stateMu.Unlock()
	if cancel != nil {
		cancel()
	}
	r.loopWg.Wait()

	r.stepMu.Lock()
	defer r.stepMu.Unlock()

	r.stateMu.RLock()
	savePath := r.savePath
	r.stateMu.RUnlock()
	if savePath != "" {
		if err := r.saveStateLocked(savePath); err != nil {
			log.Printf("save state %q on close: %v", savePath, err)
		}
	}

	for _, vm := range r.bots {
		vm.Close()
	}
	r.bots = nil
}
