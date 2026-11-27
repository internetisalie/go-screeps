package lua

import (
	"context"
	"fmt"
	"io/fs"
	"strings"
	"sync"
	"time"

	"go-screeps/internal/engine"
	"go-screeps/internal/screeps"
)

// LocalRunner orchestrates an engine.World and one or more sandboxed Lua BotVMs.
type LocalRunner struct {
	world *engine.World

	stepMu sync.Mutex
	bots   []*BotVM

	stateMu    sync.RWMutex
	activeRoom string
	tickPeriod time.Duration
	paused     bool
	savePath    string
	saveEvery   int
	summaries   []BotSummary
	viewerState *ViewerState

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
			_ = r.saveStateLocked(savePath)
		}
	}
}

// Start launches the background simulation loop, calling onUpdate after each stepped tick.
// The loop stops when ctx is cancelled.
func (r *LocalRunner) Start(ctx context.Context, tickPeriod time.Duration, onUpdate func()) {
	if tickPeriod <= 0 {
		tickPeriod = 200 * time.Millisecond
	}
	r.SetTickPeriod(tickPeriod)

	go func() {
		r.stateMu.RLock()
		period := r.tickPeriod
		r.stateMu.RUnlock()

		ticker := time.NewTicker(period)
		defer ticker.Stop()

		for {
			// Uncapped "Max" mode (period <= 2ms): run as fast as CPU allows without OS timer coalescing.
			if period <= 2*time.Millisecond {
				select {
				case <-ctx.Done():
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
					time.Sleep(25 * time.Millisecond)
					continue
				}
				r.StepTick()
				if onUpdate != nil {
					onUpdate()
				}
				continue
			}

			select {
			case <-ctx.Done():
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

// Close persists state (if configured) and shuts down all BotVMs.
func (r *LocalRunner) Close() {
	r.stepMu.Lock()
	defer r.stepMu.Unlock()

	r.stateMu.RLock()
	savePath := r.savePath
	r.stateMu.RUnlock()
	if savePath != "" {
		_ = r.saveStateLocked(savePath)
	}

	for _, vm := range r.bots {
		vm.Close()
	}
	r.bots = nil
}
