package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go-screeps/bots"
	"go-screeps/internal/engine"
	"go-screeps/internal/iso"
	"go-screeps/internal/lua"
	"go-screeps/internal/screeps"

	"github.com/bankai-tensei/gowaku"
	"github.com/bankai-tensei/gowaku/core"
	"github.com/bankai-tensei/gowaku/geom"
	"github.com/bankai-tensei/gowaku/input"
	"github.com/bankai-tensei/gowaku/layout"
	"github.com/bankai-tensei/gowaku/paint"
	"github.com/bankai-tensei/gowaku/theme"
	"github.com/bankai-tensei/gowaku/widgets/basic"
	"github.com/bankai-tensei/gowaku/widgets/control"
	"github.com/bankai-tensei/gowaku/widgets/field"
)

type worldProvider interface {
	Snapshot() screeps.RoomSnapshot
	SwitchRoom(room string)
}

var localSpeeds = []struct {
	label  string
	period time.Duration
}{
	{label: "1x", period: 250 * time.Millisecond},
	{label: "2x", period: 125 * time.Millisecond},
	{label: "5x", period: 50 * time.Millisecond},
	{label: "10x", period: 25 * time.Millisecond},
	{label: "25x", period: 10 * time.Millisecond},
	{label: "Max", period: 1 * time.Millisecond},
}

type viewerApp struct {
	core.StatefulBase
	serverURL  string
	username   string
	password   string
	room       string
	local      bool
	migrate    bool
	dbPath     string
	statePath  string
	botSpec    string
	rival      string
	tickPeriod time.Duration
	seed       int64
}

type creepAnim struct {
	prevX, prevY float32
	curX, curY   float32
}

type viewerState struct {
	core.StateBase
	cfg         viewerApp
	appCtx      context.Context
	simCancel   context.CancelFunc
	onUpdate    func()
	provider    worldProvider
	localRunner *lua.LocalRunner
	cancel      context.CancelFunc
	snap        screeps.RoomSnapshot
	cam         iso.Camera
	painter     *iso.Painter
	signal      layout.RepaintSignal
	roomCtrl    *field.InputController
	initRoom    string
	speedIdx    int

	anims      map[string]creepAnim
	creepPos   map[string][2]float32
	lastTick    int64
	tickAt      time.Time
	tickPeriod  time.Duration
	uiPending   atomic.Bool
	uiTrailing  atomic.Bool
	uiDirty     atomic.Bool
	lastUISync  atomic.Int64 // unix nanos of last UI snapshot apply

	dragging  bool
	orbiting  bool
	dragMoved bool
	lastPos   geom.Offset
	downPos   geom.Offset

	// SB-1 & SB-2 UI state
	zenMode          bool
	followCam        bool
	selectedStackIdx int
	hoverRoom        string
	hoverMinimapRoom string
	collapseEvents   bool
	collapseMinimap  bool
}

func (v viewerApp) CreateState() core.State {
	initTick := 250 * time.Millisecond
	if v.local && v.tickPeriod > 0 {
		initTick = v.tickPeriod
	}
	speedIdx := 0
	for i, spd := range localSpeeds {
		if spd.period == initTick {
			speedIdx = i
			break
		}
	}

	s := &viewerState{
		cfg:        v,
		cam:        iso.NewCamera(),
		roomCtrl:   &field.InputController{},
		initRoom:   v.room,
		speedIdx:   speedIdx,
		anims:      make(map[string]creepAnim),
		creepPos:   make(map[string][2]float32),
		tickPeriod: initTick,
	}
	s.painter = &iso.Painter{Snap: &s.snap, Cam: &s.cam, CreepPos: s.creepPos, ShowVisuals: true}

	ctx, cancel := context.WithCancel(context.Background())
	s.appCtx = ctx
	s.cancel = cancel

	var scheduleUI func()
	scheduleUI = func() {
		if ctx.Err() != nil {
			return
		}
		// Coalesce UI refreshes (leading + trailing) so high-speed simulation never backlogs
		// Cocoa/Metal VSync while guaranteeing the latest snapshot is never silently dropped.
		const minInterval = int64(16 * time.Millisecond)
		elapsed := time.Now().UnixNano() - s.lastUISync.Load()
		if elapsed < minInterval {
			if s.uiTrailing.CompareAndSwap(false, true) {
				delay := time.Duration(minInterval - elapsed)
				time.AfterFunc(delay, func() {
					s.uiTrailing.Store(false)
					scheduleUI()
				})
			}
			return
		}
		s.uiDirty.Store(true)
		if !s.uiPending.CompareAndSwap(false, true) {
			return
		}
		gowaku.Post(func() {
			if ctx.Err() != nil {
				s.uiPending.Store(false)
				return
			}
			s.uiDirty.Store(false)
			s.lastUISync.Store(time.Now().UnixNano())
			s.SetState(func() {
				if s.provider != nil {
					s.applySnapshot(s.provider.Snapshot())
				}
			})
			s.signal.Notify()
			s.uiPending.Store(false)
			if s.uiDirty.Load() {
				scheduleUI()
			}
		})
	}
	s.onUpdate = scheduleUI

	if v.local {
		s.startLocalSim(false)
	} else {
		client := screeps.NewClient(v.serverURL, v.username, v.password, s.onUpdate)
		s.provider = client
		go client.Start(ctx, v.room)
	}

	// 60 FPS ease ticker when creeps are mid-step
	go func() {
		ticker := time.NewTicker(16 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				gowaku.Post(func() {
					if ctx.Err() != nil {
						return
					}
					if s.updateCreepPositions() {
						s.signal.Notify()
					}
				})
			}
		}
	}()

	return s
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
	for _, rn := range iso.NeighborRoomNames(centerRoom, radius) {
		if w.GetRoom(rn) == nil {
			w.GenerateRoom(rn, roomSeed(baseSeed, rn))
		}
	}
}

func (s *viewerState) startLocalSim(ignoreSaved bool) {
	v := s.cfg
	if ignoreSaved && !v.migrate && strings.TrimSpace(v.dbPath) == "" {
		// Fresh procedural reset: advance seed so each Reset creates a new varied world.
		s.cfg.seed = time.Now().UnixNano()
		v = s.cfg
	}

	if s.simCancel != nil {
		s.simCancel()
		s.simCancel = nil
	}
	if s.localRunner != nil {
		if ignoreSaved {
			// Clear savePath before closing so Close() doesn't re-write the old state.
			s.localRunner.SetStatePath("", 0)
		}
		s.localRunner.Close()
		s.localRunner = nil
	}

	roomName := strings.ToUpper(strings.TrimSpace(v.room))
	if roomName == "" {
		roomName = "W9N7"
	}
	world := engine.NewWorld()
	statePath := strings.TrimSpace(v.statePath)
	if ignoreSaved && statePath != "" {
		_ = os.Remove(statePath)
	}

	var loadedSave *lua.SaveFile
	if !ignoreSaved && statePath != "" {
		sf, err := lua.LoadSaveFile(statePath)
		if err == nil {
			if err := world.ImportState(sf.World); err != nil {
				log.Printf("failed to restore state from %q: %v", statePath, err)
			} else {
				loadedSave = sf
				log.Printf("restored world state from %q (tick %d, rooms %d)", statePath, world.GameTime, len(world.Rooms))
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			log.Printf("failed to load state file %q: %v (starting fresh)", statePath, err)
		}
	}

	if loadedSave == nil {
		dbPath := strings.TrimSpace(v.dbPath)
		if v.migrate {
			if dbPath == "" {
				log.Printf("local -migrate requires an explicit -db path (falling back to generated room)")
			} else if err := world.ImportFullDBJSON(dbPath); err != nil {
				log.Printf("local db migrate %q failed: %v (falling back to generated room)", dbPath, err)
			}
		} else if dbPath != "" {
			if err := world.LoadFromDBJSON(dbPath); err != nil {
				log.Printf("local db load %q failed: %v (falling back to generated room)", dbPath, err)
			}
		}
	}
	if world.GetRoom(roomName) == nil {
		ensureNeighborhood(world, roomName, v.seed, 2)
	} else {
		// Ensure full 5x5 neighborhood exists around the starting room (seamless multi-room rendering).
		ensureNeighborhood(world, roomName, v.seed, 2)
	}

	runner := lua.NewLocalRunner(world, roomName)
	attachExisting := v.migrate || loadedSave != nil
	if strings.TrimSpace(v.botSpec) == "" {
		primaryOwner := "LuaBot"
		if attachExisting {
			_, ownerName := world.RoomOwner(roomName)
			if ownerName != "" {
				primaryOwner = ownerName
			}
			if err := runner.AttachEmbeddedBot(primaryOwner, roomName, bots.StarterFS, "starter"); err != nil {
				log.Printf("failed to attach embedded starter bot for %q in %s: %v", primaryOwner, roomName, err)
			}
		} else {
			if err := runner.AddEmbeddedBot(primaryOwner, roomName, bots.StarterFS, "starter"); err != nil {
				log.Printf("failed to spawn embedded starter bot: %v", err)
			}
		}

		// Optional competing Raider Lua bot in an adjacent connected room (default: "auto").
		if rivalSpec := strings.TrimSpace(v.rival); rivalSpec != "" {
			rivalRoom := ""
			if strings.EqualFold(rivalSpec, "auto") {
				rivalRoom = pickAutoRivalRoom(world, roomName, primaryOwner)
			} else {
				rivalRoom = strings.ToUpper(rivalSpec)
			}
			if rivalRoom != "" && rivalRoom != roomName {
				if world.GetRoom(rivalRoom) == nil {
					world.GenerateRoom(rivalRoom, roomSeed(v.seed, rivalRoom))
				}
				_, rivalOwner := world.RoomOwner(rivalRoom)
				if rivalOwner == "" || rivalOwner == primaryOwner {
					rivalOwner = "RaiderBot"
				}
				if err := runner.AttachEmbeddedBot(rivalOwner, rivalRoom, bots.RaiderFS, "raider"); err != nil {
					log.Printf("failed to attach competing raider bot for %q in %s: %v", rivalOwner, rivalRoom, err)
				} else {
					log.Printf("competing raider bot %q active in %s", rivalOwner, rivalRoom)
				}
			}
		}
	} else {
		for _, raw := range strings.Split(v.botSpec, ",") {
			entry := strings.TrimSpace(raw)
			if entry == "" {
				continue
			}
			eqIdx := strings.IndexByte(entry, '=')
			if eqIdx <= 0 || eqIdx == len(entry)-1 {
				log.Printf("ignoring malformed -bot entry %q (expected Name=Dir@Room)", entry)
				continue
			}
			botName := strings.TrimSpace(entry[:eqIdx])
			rest := strings.TrimSpace(entry[eqIdx+1:])
			botDir := rest
			botRoom := roomName
			if atIdx := strings.LastIndexByte(rest, '@'); atIdx >= 0 {
				botDir = strings.TrimSpace(rest[:atIdx])
				if r := strings.ToUpper(strings.TrimSpace(rest[atIdx+1:])); r != "" {
					botRoom = r
				}
			}
			if world.GetRoom(botRoom) == nil {
				world.GenerateRoom(botRoom, v.seed)
			}
			if attachExisting {
				if err := runner.AttachDirBot(botName, botRoom, botDir); err != nil {
					log.Printf("failed to attach bot %q (%s@%s): %v", botName, botDir, botRoom, err)
				}
			} else {
				if err := runner.AddDirBot(botName, botRoom, botDir); err != nil {
					log.Printf("failed to add bot %q (%s@%s): %v", botName, botDir, botRoom, err)
				}
			}
		}
	}

	if loadedSave != nil {
		for uname, mem := range loadedSave.Memories {
			runner.ApplyBotMemory(uname, mem)
		}
		if vs := loadedSave.Viewer; vs != nil {
			if vs.Room != "" && world.GetRoom(vs.Room) != nil {
				runner.SwitchRoom(vs.Room)
				s.initRoom = vs.Room
			}
			if vs.SpeedIdx >= 0 && vs.SpeedIdx < len(localSpeeds) {
				s.speedIdx = vs.SpeedIdx
				s.tickPeriod = localSpeeds[vs.SpeedIdx].period
			}
			runner.SetPaused(vs.Paused)
			s.painter.ShowVisuals = vs.ShowVisuals
			if vs.Zoom > 0 {
				s.cam.PanX = vs.PanX
				s.cam.PanY = vs.PanY
				s.cam.Zoom = vs.Zoom
				s.cam.SetYaw(vs.Yaw)
				s.cam.Fitted = true
			}
		}
	}
	if statePath != "" {
		runner.SetStatePath(statePath, 100)
	}

	s.cam.SelectedX = -1
	s.cam.SelectedY = -1
	s.cam.FollowID = ""
	s.anims = make(map[string]creepAnim)
	s.creepPos = make(map[string][2]float32)
	s.painter.CreepPos = s.creepPos
	s.painter.ClearUnitTracking()
	s.lastTick = 0
	s.tickAt = time.Time{}

	simCtx, simCancel := context.WithCancel(s.appCtx)
	s.simCancel = simCancel
	s.localRunner = runner
	s.provider = runner
	runner.RefreshSummaries()
	s.syncViewerState()
	s.applySnapshot(runner.Snapshot())
	runner.Start(simCtx, s.tickPeriod, s.onUpdate)
}

func (s *viewerState) syncViewerState() {
	if s.localRunner == nil {
		return
	}
	room := s.snap.Room
	if room == "" {
		room = s.initRoom
	}
	s.localRunner.SetViewerState(lua.ViewerState{
		Room:        room,
		PanX:        s.cam.PanX,
		PanY:        s.cam.PanY,
		Zoom:        s.cam.Zoom,
		Yaw:         s.cam.YawRadians(),
		SpeedIdx:    s.speedIdx,
		Paused:      s.localRunner.Paused(),
		ShowVisuals: s.painter.ShowVisuals,
	})
}

func (s *viewerState) applySnapshot(newSnap screeps.RoomSnapshot) {
	s.snap = newSnap
	if s.localRunner != nil {
		s.syncViewerState()
		w := s.localRunner.World()
		names := iso.NeighborRoomNames(newSnap.Room, 2)
		neighbors := make([]*screeps.RoomSnapshot, 0, len(names))
		for _, rn := range names {
			if rn == newSnap.Room {
				continue
			}
			if w.GetRoom(rn) != nil {
				ns := w.Snapshot(rn)
				neighbors = append(neighbors, &ns)
			}
		}
		s.painter.Neighbors = neighbors
	}
	if newSnap.GameTime != s.lastTick && newSnap.GameTime > 0 {
		now := time.Now()
		if !s.tickAt.IsZero() && s.lastTick > 0 && newSnap.GameTime > s.lastTick {
			deltaTicks := newSnap.GameTime - s.lastTick
			dt := now.Sub(s.tickAt)
			perTick := dt / time.Duration(deltaTicks)
			if perTick >= 500*time.Microsecond && perTick <= 2*time.Second {
				s.tickPeriod = (s.tickPeriod*3 + perTick) / 4
			}
		}
		s.tickAt = now
		s.lastTick = newSnap.GameTime

		nextAnims := make(map[string]creepAnim, len(newSnap.Objects))
		addRoomCreeps := func(objs []screeps.RoomObject) {
			for _, obj := range objs {
				if obj.Type != "creep" && obj.Type != "powerCreep" {
					continue
				}
				nx, ny := float32(obj.X), float32(obj.Y)
				if old, ok := s.anims[obj.ID]; ok && math.Abs(float64(nx-old.curX)) <= 1 && math.Abs(float64(ny-old.curY)) <= 1 {
					nextAnims[obj.ID] = creepAnim{prevX: old.curX, prevY: old.curY, curX: nx, curY: ny}
				} else {
					nextAnims[obj.ID] = creepAnim{prevX: nx, prevY: ny, curX: nx, curY: ny}
				}
			}
		}
		addRoomCreeps(newSnap.Objects)
		for _, nb := range s.painter.Neighbors {
			if nb != nil {
				addRoomCreeps(nb.Objects)
			}
		}
		s.anims = nextAnims
		for id := range s.creepPos {
			if _, alive := nextAnims[id]; !alive {
				delete(s.creepPos, id)
			}
		}
	}
	s.updateCreepPositions()
	if s.cam.FollowID != "" {
		found := false
		for _, obj := range s.snap.Objects {
			if obj.ID == s.cam.FollowID {
				s.cam.SelectedX = obj.X
				s.cam.SelectedY = obj.Y
				if s.followCam {
					s.cam.CenterOnTile(obj.X, obj.Y)
				}
				found = true
				break
			}
		}
		if !found {
			if s.followCam && s.localRunner != nil {
				if nextRoom := s.localRunner.FindEntityRoom(s.cam.FollowID); nextRoom != "" && nextRoom != s.snap.Room {
					followID := s.cam.FollowID
					s.switchRoom(nextRoom)
					s.cam.FollowID = followID
					s.followCam = true
					return
				}
			}
			s.cam.FollowID = ""
			s.followCam = false
		}
	}
}

func (s *viewerState) updateCreepPositions() bool {
	if len(s.anims) == 0 || s.tickAt.IsZero() {
		return false
	}
	rawT := float32(time.Since(s.tickAt)) / float32(s.tickPeriod)
	if rawT > 1 {
		rawT = 1
	}
	// Smoothstep easing
	ease := rawT * rawT * (3 - 2*rawT)
	moving := false
	for id, a := range s.anims {
		if a.prevX != a.curX || a.prevY != a.curY {
			if rawT < 1 {
				moving = true
			}
			s.creepPos[id] = [2]float32{
				a.prevX + (a.curX-a.prevX)*ease,
				a.prevY + (a.curY-a.prevY)*ease,
			}
		} else {
			s.creepPos[id] = [2]float32{a.curX, a.curY}
		}
	}
	return moving
}

func (s *viewerState) Dispose() {
	if s.localRunner != nil {
		s.syncViewerState()
	}
	if s.simCancel != nil {
		s.simCancel()
	}
	if s.cancel != nil {
		s.cancel()
	}
	if s.localRunner != nil {
		s.localRunner.Close()
	}
}

func (s *viewerState) switchRoom(name string) {
	name = strings.ToUpper(strings.TrimSpace(name))
	if name == "" {
		return
	}
	s.cam.SelectedX = -1
	s.cam.SelectedY = -1
	s.cam.FollowID = ""
	s.anims = make(map[string]creepAnim)
	s.creepPos = make(map[string][2]float32)
	s.painter.CreepPos = s.creepPos
	s.painter.ClearUnitTracking()
	s.provider.SwitchRoom(name)
	if s.localRunner != nil {
		s.SetState(func() {
			s.applySnapshot(s.provider.Snapshot())
		})
		s.signal.Notify()
	}
}

func (s *viewerState) recenterOnPan() {
	if s.localRunner == nil || s.snap.Room == "" {
		return
	}
	rdx, rdy := s.cam.ViewportRoomShift()
	if rdx == 0 && rdy == 0 {
		return
	}
	newRoom, ok := engine.AdjacentRoomName(s.snap.Room, rdx, rdy)
	if !ok || newRoom == "" || newRoom == s.snap.Room {
		return
	}
	w := s.localRunner.World()
	ensureNeighborhood(w, newRoom, s.cfg.seed, 2)
	s.cam.ShiftByRooms(rdx, rdy)
	s.cam.SelectedX = -1
	s.cam.SelectedY = -1
	s.cam.FollowID = ""
	s.provider.SwitchRoom(newRoom)
	s.SetState(func() {
		s.applySnapshot(s.provider.Snapshot())
	})
}

func (s *viewerState) handlePointer(e *core.PointerEvent) {
	switch e.Kind {
	case core.PointerDown:
		s.lastPos = e.Pos
		s.downPos = e.Pos
		s.dragMoved = false
		if e.Button == input.ButtonSecondary {
			s.orbiting = true
			s.dragging = false
		} else {
			s.dragging = true
			s.orbiting = false
		}
	case core.PointerMove:
		if s.orbiting {
			dx := e.Pos.X - s.lastPos.X
			s.lastPos = e.Pos
			s.SetState(func() {
				s.cam.OrbitYaw(-dx * 0.008)
			})
			s.signal.Notify()
			return
		}
		if !s.dragging {
			// Live tile hover tracking for Hover Quick-Inspect card (UC-3.1).
			if s.painter.LastSize.W > 0 {
				hitRoom, gx, gy, ok := s.cam.ScreenToWorldTile(e.Pos, s.painter.LastSize, &s.snap, s.painter.Neighbors)
				if !ok || (hitRoom != "" && hitRoom != s.snap.Room) {
					gx, gy, hitRoom = -1, -1, ""
				}
				if gx != s.cam.HoverX || gy != s.cam.HoverY || hitRoom != s.hoverRoom {
					s.SetState(func() {
						s.cam.HoverX = gx
						s.cam.HoverY = gy
						s.hoverRoom = hitRoom
					})
					s.signal.Notify()
				}
			}
			return
		}
		dx := e.Pos.X - s.lastPos.X
		dy := e.Pos.Y - s.lastPos.Y
		s.lastPos = e.Pos
		if e.Pos.Sub(s.downPos).Length() > 4 {
			s.dragMoved = true
			s.followCam = false
		}
		s.SetState(func() {
			s.cam.PanX += dx
			s.cam.PanY += dy
			s.recenterOnPan()
		})
		s.signal.Notify()
	case core.PointerUp, core.PointerCancel:
		wasClick := s.dragging && !s.dragMoved && e.Kind == core.PointerUp
		s.dragging = false
		s.orbiting = false
		if wasClick && s.painter.LastSize.W > 0 {
			if hitRoom, gx, gy, ok := s.cam.ScreenToWorldTile(e.Pos, s.painter.LastSize, &s.snap, s.painter.Neighbors); ok {
				if hitRoom != "" && hitRoom != s.snap.Room {
					s.switchRoom(hitRoom)
				}
				s.SetState(func() {
					s.cam.SelectedX = gx
					s.cam.SelectedY = gy
					s.selectedStackIdx = 0
					s.cam.FollowID = ""
					s.followCam = false
					for _, obj := range s.snap.Objects {
						if obj.X == gx && obj.Y == gy && (obj.Type == "creep" || obj.Type == "powerCreep") {
							s.cam.FollowID = obj.ID
							s.followCam = true
							s.cam.CenterOnTile(gx, gy)
							break
						}
					}
				})
				s.signal.Notify()
			}
		}
	case core.PointerScroll:
		if e.Modifiers&input.ModControl != 0 {
			if e.Scroll.Y > 0 {
				s.SetState(func() { s.cam.OrbitYaw(math.Pi / 2) })
				s.signal.Notify()
			} else if e.Scroll.Y < 0 {
				s.SetState(func() { s.cam.OrbitYaw(-math.Pi / 2) })
				s.signal.Notify()
			}
			return
		}
		factor := float32(1.0)
		if e.Scroll.Y > 0 {
			factor = 1.12
		} else if e.Scroll.Y < 0 {
			factor = 1 / 1.12
		}
		if factor != 1.0 {
			s.zoomAt(factor, e.Pos)
		}
	case core.PointerPinch:
		if e.Modifiers&input.ModControl == 0 && e.Pinch > 0 {
			s.zoomAt(e.Pinch, e.Pos)
		}
	}
}

func (s *viewerState) zoomAt(factor float32, anchor geom.Offset) {
	oldZoom := s.cam.Zoom
	newZoom := geom.Clamp32(oldZoom*factor, 0.4, 4.0)
	if newZoom == oldZoom {
		return
	}
	sz := s.painter.LastSize
	if sz.W > 0 && sz.H > 0 {
		cx := sz.W/2 + s.cam.PanX
		cy := sz.H/2 + s.cam.PanY
		scale := newZoom / oldZoom
		s.cam.PanX = anchor.X - (anchor.X-cx)*scale - sz.W/2
		s.cam.PanY = anchor.Y - (anchor.Y-cy)*scale - sz.H/2
	}
	s.SetState(func() {
		s.cam.Zoom = newZoom
	})
	s.signal.Notify()
}

func (s *viewerState) Build(ctx core.BuildContext) core.Widget {
	if w := gowaku.WindowOf(ctx); w != nil {
		setMainWindow(w)
	}
	th := basic.ThemeOf(ctx)
	return s.buildSB1(th)
}

func (s *viewerState) buildHoverCard(th *theme.Theme) core.Widget {
	if s.cam.HoverX < 0 || s.cam.HoverY < 0 || s.cam.HoverX >= iso.GridSize || s.cam.HoverY >= iso.GridSize {
		return nil
	}
	var objs []screeps.RoomObject
	for _, obj := range s.snap.Objects {
		if obj.X == s.cam.HoverX && obj.Y == s.cam.HoverY {
			objs = append(objs, obj)
		}
	}
	if len(objs) == 0 {
		return nil
	}
	sort.Slice(objs, func(i, j int) bool {
		return entityInspectPriority(objs[i].Type) < entityInspectPriority(objs[j].Type)
	})
	top := objs[0]
	title := top.Name
	if title == "" {
		title = strings.ToUpper(top.Type)
	}
	owner := "Neutral"
	if top.User != "" {
		owner = top.User
		if u, ok := s.snap.Users[top.User]; ok && u != "" {
			owner = u
		}
	}
	titleRow := make([]core.Widget, 0, 3)
	if ico := entityIconWidget(top, 15); ico != nil {
		titleRow = append(titleRow, ico)
	}
	titleRow = append(titleRow,
		basic.Text{Content: title, Style: th.Text.Body.WithColor(th.Color.Ink)},
		basic.Text{Content: fmt.Sprintf("(%d, %d)", s.cam.HoverX, s.cam.HoverY), Style: th.Text.Caption.WithColor(th.Color.Ink2)},
	)
	rows := []core.Widget{
		basic.Row{
			Size:     layout.MainMin,
			Cross:    layout.CrossCenter,
			Spacing:  6,
			Children: titleRow,
		},
		basic.Text{
			Content: fmt.Sprintf("%s • %s", top.Type, owner),
			Style:   th.Text.Caption.WithColor(th.Color.Ink2),
		},
	}
	if top.HitsMax > 0 {
		rows = append(rows, basic.Text{
			Content: fmt.Sprintf("HP %d / %d", top.Hits, top.HitsMax),
			Style:   th.Text.Caption.WithColor(geom.Hex(0x66bb6a)),
		})
	}
	if act := formatActionLog(top); act != "" {
		rows = append(rows, basic.Text{
			Content: act,
			Style:   th.Text.Caption.WithColor(geom.Hex(0x4fc3f7)),
		})
	}
	if len(objs) > 1 {
		rows = append(rows, basic.Text{
			Content: fmt.Sprintf("+%d more on tile • Click to inspect", len(objs)-1),
			Style:   th.Text.Caption.WithColor(th.Color.Ink2),
		})
	}
	return basic.Container{
		Color:   geom.Hex(0x131b26),
		Padding: geom.All(theme.Space8),
		Radius:  th.Radius.Control,
		Border:  paint.Stroke{Width: 1, Color: geom.Hex(0x4fc3f7)},
		Child: basic.Column{
			Size:     layout.MainMin,
			Cross:    layout.CrossStart,
			Spacing:  2,
			Children: rows,
		},
	}
}

func entityInspectPriority(typ string) int {
	switch typ {
	case "creep", "powerCreep":
		return 0
	case "spawn":
		return 1
	case "controller":
		return 2
	case "tower":
		return 3
	case "storage", "terminal", "link", "extractor", "extension", "container":
		return 4
	case "source", "mineral":
		return 5
	case "tombstone", "resource":
		return 6
	case "constructionSite":
		return 7
	case "rampart", "constructedWall":
		return 8
	case "road":
		return 9
	default:
		return 10
	}
}

func formatActionLog(obj screeps.RoomObject) string {
	if obj.Raw == nil {
		return ""
	}
	al, ok := obj.Raw["actionLog"].(map[string]any)
	if !ok || len(al) == 0 {
		return ""
	}
	for _, key := range []string{"attack", "rangedAttack", "rangedMassAttack", "dismantle", "heal", "rangedHeal", "harvest", "build", "repair", "upgradeController", "claimController", "reserveController", "attackController", "transfer", "withdraw"} {
		raw, exists := al[key]
		if !exists || raw == nil {
			continue
		}
		if m, ok := raw.(map[string]any); ok {
			tx, okX := intField(m, "x")
			ty, okY := intField(m, "y")
			if okX && okY {
				return fmt.Sprintf("Action: %s → (%d, %d)", key, tx, ty)
			}
		}
		return "Action: " + key
	}
	return ""
}

func (s *viewerState) selectAndCenter(obj screeps.RoomObject, follow bool) {
	s.SetState(func() {
		s.cam.SelectedX = obj.X
		s.cam.SelectedY = obj.Y
		s.selectedStackIdx = 0
		if follow {
			s.cam.FollowID = obj.ID
			s.followCam = true
		} else {
			s.cam.FollowID = ""
			s.followCam = false
		}
		s.cam.CenterOnTile(obj.X, obj.Y)
	})
	s.signal.Notify()
}

func (s *viewerState) buildInspector(th *theme.Theme) core.Widget {
	var creepObjs []screeps.RoomObject
	var landmarkObjs []screeps.RoomObject
	spawns, sources := 0, 0
	var ctrlSummary string
	for _, obj := range s.snap.Objects {
		switch obj.Type {
		case "creep", "powerCreep":
			creepObjs = append(creepObjs, obj)
		case "spawn":
			spawns++
			landmarkObjs = append(landmarkObjs, obj)
		case "source":
			sources++
		case "controller":
			landmarkObjs = append(landmarkObjs, obj)
			owner := "unowned"
			if obj.User != "" {
				owner = obj.User
				if u, ok := s.snap.Users[obj.User]; ok {
					owner = u
				}
			}
			ctrlSummary = fmt.Sprintf("RCL %d (%s)", obj.Level, owner)
			if prog, ok := intField(obj.Raw, "progress"); ok {
				if tot, ok2 := intField(obj.Raw, "progressTotal"); ok2 && tot > 0 {
					ctrlSummary += fmt.Sprintf(" • %d/%d", prog, tot)
				}
			}
		}
	}
	sort.Slice(creepObjs, func(i, j int) bool { return creepObjs[i].Name < creepObjs[j].Name })
	sort.Slice(landmarkObjs, func(i, j int) bool { return landmarkObjs[i].Type < landmarkObjs[j].Type })

	overview := fmt.Sprintf("Creeps: %d  •  Spawns: %d  •  Sources: %d", len(creepObjs), spawns, sources)
	if ctrlSummary != "" {
		overview += "\nController: " + ctrlSummary
	}

	lines := []core.Widget{
		basic.Text{
			Content: fmt.Sprintf("Room %s", s.snap.Room),
			Style:   th.Text.Title.WithColor(th.Color.Ink),
		},
		basic.Container{
			Color:   th.Color.Canvas,
			Padding: geom.All(theme.Space8),
			Radius:  th.Radius.Control,
			Child: basic.Text{
				Content: overview,
				Style:   th.Text.Caption.WithColor(th.Color.Ink2),
			},
		},
	}

	// Fixed-position Landmarks row right below Room Overview
	if len(landmarkObjs) > 0 {
		var chips []core.Widget
		for _, lm := range landmarkObjs {
			target := lm
			lbl := target.Type
			if target.Name != "" {
				lbl = target.Name
			}
			chips = append(chips, control.Button{
				Label:   lbl,
				Size:    control.ButtonSmall,
				OnPress: func() { s.selectAndCenter(target, false) },
			})
		}
		lines = append(lines, basic.Wrap{
			Spacing:    theme.Space4,
			RunSpacing: theme.Space4,
			Children:   chips,
		})
	}

	if s.cam.SelectedX < 0 || s.cam.SelectedY < 0 {
		lines = append(lines, basic.Text{
			Content: "Drag to pan • Right-drag to rotate\nScroll to zoom • Click any tile",
			Style:   th.Text.Caption.WithColor(th.Color.Ink2),
		})
	} else {
		terrainName, moveCost := "plain", 2
		if len(s.snap.Terrain) >= iso.GridSize*iso.GridSize {
			switch s.snap.Terrain[s.cam.SelectedY*iso.GridSize+s.cam.SelectedX] {
			case '1':
				terrainName, moveCost = "wall", 255
			case '2':
				terrainName, moveCost = "swamp", 10
			case '3':
				terrainName, moveCost = "wall+swamp", 255
			}
		}
		exitTag := ""
		if s.cam.SelectedX == 0 {
			exitTag += " [W-Exit]"
		} else if s.cam.SelectedX == iso.GridSize-1 {
			exitTag += " [E-Exit]"
		}
		if s.cam.SelectedY == 0 {
			exitTag += " [N-Exit]"
		} else if s.cam.SelectedY == iso.GridSize-1 {
			exitTag += " [S-Exit]"
		}

		lines = append(lines, basic.Text{
			Content: fmt.Sprintf("Location %s (%d, %d) • %s (cost %d)%s", s.snap.Room, s.cam.SelectedX, s.cam.SelectedY, terrainName, moveCost, exitTag),
			Style:   th.Text.Body.WithColor(th.Color.Accent),
		})

		var tileObjs []screeps.RoomObject
		for _, obj := range s.snap.Objects {
			if obj.X == s.cam.SelectedX && obj.Y == s.cam.SelectedY {
				tileObjs = append(tileObjs, obj)
			}
		}
		sort.Slice(tileObjs, func(i, j int) bool {
			pi, pj := entityInspectPriority(tileObjs[i].Type), entityInspectPriority(tileObjs[j].Type)
			if pi != pj {
				return pi < pj
			}
			if tileObjs[i].Name != tileObjs[j].Name {
				return tileObjs[i].Name < tileObjs[j].Name
			}
			return tileObjs[i].ID < tileObjs[j].ID
		})
		if len(tileObjs) > 0 {
			if s.selectedStackIdx < 0 || s.selectedStackIdx >= len(tileObjs) {
				s.selectedStackIdx = 0
			}
			// Stacked entity disambiguation pills (UC-3.2).
			if len(tileObjs) > 1 {
				var tabs []core.Widget
				for idx, obj := range tileObjs {
					i := idx
					lbl := obj.Type
					if obj.Name != "" {
						lbl = obj.Name
					}
					if i == s.selectedStackIdx {
						lbl = "● " + lbl
					}
					tabs = append(tabs, control.Button{
						Label: lbl,
						Size:  control.ButtonSmall,
						OnPress: func() {
							s.SetState(func() { s.selectedStackIdx = i })
						},
					})
				}
				lines = append(lines, basic.Wrap{
					Spacing:    theme.Space4,
					RunSpacing: theme.Space4,
					Children:   tabs,
				})
			}

			selObj := tileObjs[s.selectedStackIdx]
			if selObj.Type == "creep" || selObj.Type == "powerCreep" {
				followLbl := "Follow Creep"
				if s.followCam && s.cam.FollowID == selObj.ID {
					followLbl = "● Following (Stop)"
				}
				targetID := selObj.ID
				lines = append(lines, control.Button{
					Label: followLbl,
					Size:  control.ButtonSmall,
					OnPress: func() {
						s.SetState(func() {
							if s.followCam && s.cam.FollowID == targetID {
								s.followCam = false
							} else {
								s.cam.FollowID = targetID
								s.followCam = true
							}
						})
					},
				})
			}
			var creepMem string
			if s.localRunner != nil && (selObj.Type == "creep" || selObj.Type == "powerCreep") {
				creepMem = s.localRunner.CreepMemoryJSON(selObj.User, selObj.Name)
			}
			lines = append(lines, buildObjectCard(selObj, s.snap.Users, s.snap.GameTime, creepMem, nil, th))
		} else {
			lines = append(lines, basic.Text{
				Content: "No objects on this tile.",
				Style:   th.Text.Caption.WithColor(th.Color.Ink2),
			})
		}
	}

	if s.localRunner != nil {
		if summaries := s.localRunner.BotSummaries(); len(summaries) > 0 {
			brainWidgets := make([]core.Widget, 0, len(summaries)+1)
			brainWidgets = append(brainWidgets, basic.Text{
				Content: "Bot Brains",
				Style:   th.Text.Body.WithColor(th.Color.Accent),
			})
			for _, bs := range summaries {
				brainWidgets = append(brainWidgets, s.buildBotBrainCard(bs, th))
			}
			lines = append(lines, basic.Column{
				Cross:    layout.CrossStretch,
				Spacing:  theme.Space4,
				Children: brainWidgets,
			})
		}

		if events := s.localRunner.World().RecentEvents(8); len(events) > 0 {
			evWidgets := make([]core.Widget, 0, len(events)+1)
			evWidgets = append(evWidgets, basic.Text{
				Content: "Colony Events",
				Style:   th.Text.Body.WithColor(th.Color.Accent),
			})
			for _, ev := range events {
				evCol := th.Color.Ink2
				switch ev.Kind {
				case "danger":
					evCol = geom.Hex(0xff5252)
				case "combat":
					evCol = geom.Hex(0xff8a65)
				case "raid":
					evCol = geom.Hex(0xffab40)
				case "rcl":
					evCol = geom.Hex(0x4fc3f7)
				case "build":
					evCol = geom.Hex(0x81c784)
				}
				evWidgets = append(evWidgets, basic.Text{
					Content: fmt.Sprintf("t%d [%s] %s", ev.Tick, ev.Room, ev.Message),
					Style:   th.Text.Caption.WithColor(evCol),
				})
			}
			lines = append(lines, basic.Container{
				Color:   th.Color.Canvas,
				Padding: geom.All(theme.Space8),
				Radius:  th.Radius.Control,
				Border:  paint.Stroke{Width: 1, Color: th.Color.Hair},
				Child: basic.Column{
					Cross:    layout.CrossStretch,
					Spacing:  2,
					Children: evWidgets,
				},
			})
		}

		if logs := s.localRunner.Logs(); len(logs) > 0 {
			const maxLines = 6
			if len(logs) > maxLines {
				logs = logs[len(logs)-maxLines:]
			}
			logWidgets := make([]core.Widget, 0, len(logs)+1)
			logWidgets = append(logWidgets, basic.Text{
				Content: "Console",
				Style:   th.Text.Body.WithColor(th.Color.Accent),
			})
			for _, line := range logs {
				logWidgets = append(logWidgets, basic.Text{
					Content: line,
					Style:   th.Text.Caption.WithColor(th.Color.Ink2),
				})
			}
			lines = append(lines, basic.Container{
				Color:   th.Color.Canvas,
				Padding: geom.All(theme.Space8),
				Radius:  th.Radius.Control,
				Border:  paint.Stroke{Width: 1, Color: th.Color.Hair},
				Child: basic.Column{
					Cross:    layout.CrossStretch,
					Spacing:  2,
					Children: logWidgets,
				},
			})
		}
	}

	return basic.Column{
		Cross:    layout.CrossStretch,
		Spacing:  theme.Space8,
		Children: lines,
	}
}

func (s *viewerState) buildBotBrainCard(bs lua.BotSummary, th *theme.Theme) core.Widget {
	rows := make([]core.Widget, 0, 6)

	roomLabel := bs.HomeRoom
	if roomLabel == "" {
		roomLabel = "—"
	}
	header := fmt.Sprintf("%s  •  %s (RCL %d)", bs.Username, roomLabel, bs.RCL)
	if bs.HomeRoom != "" && bs.HomeRoom != s.snap.Room {
		targetRoom := bs.HomeRoom
		rows = append(rows, basic.Row{
			Cross:   layout.CrossCenter,
			Spacing: theme.Space4,
			Children: []core.Widget{
				basic.Text{
					Content: header,
					Style:   th.Text.Caption.WithColor(th.Color.Ink),
				},
				control.Button{
					Label:   "Go",
					Size:    control.ButtonSmall,
					OnPress: func() { s.switchRoom(targetRoom) },
				},
			},
		})
	} else {
		rows = append(rows, basic.Text{
			Content: header,
			Style:   th.Text.Caption.WithColor(th.Color.Ink),
		})
	}

	// Energy + Next Spawn decision
	spawnLine := fmt.Sprintf("Energy: %d/%d", bs.EnergyAvail, bs.EnergyCap)
	if bs.NextSpawn != "" {
		spawnLine += "  •  " + bs.NextSpawn
	}
	rows = append(rows, basic.Text{
		Content: spawnLine,
		Style:   th.Text.Caption.WithColor(th.Color.Ink2),
	})

	// Roles: current / desired
	roleSet := make(map[string]struct{})
	for r := range bs.RoleCounts {
		roleSet[r] = struct{}{}
	}
	for r, want := range bs.DesiredRoles {
		if want > 0 {
			roleSet[r] = struct{}{}
		}
	}
	if len(roleSet) > 0 {
		roleNames := make([]string, 0, len(roleSet))
		for r := range roleSet {
			roleNames = append(roleNames, r)
		}
		sort.Strings(roleNames)
		var roleParts []string
		for _, r := range roleNames {
			cur := bs.RoleCounts[r]
			if want, ok := bs.DesiredRoles[r]; ok && want > 0 {
				roleParts = append(roleParts, fmt.Sprintf("%s %d/%d", r, cur, want))
			} else if cur > 0 {
				roleParts = append(roleParts, fmt.Sprintf("%s %d", r, cur))
			}
		}
		if len(roleParts) > 0 {
			rows = append(rows, basic.Text{
				Content: "Roles: " + strings.Join(roleParts, " • "),
				Style:   th.Text.Caption.WithColor(th.Color.Ink2),
			})
		}
	}

	if bs.PlanSummary != "" {
		rows = append(rows, basic.Text{
			Content: "Plan: " + bs.PlanSummary,
			Style:   th.Text.Caption.WithColor(th.Color.Ink2),
		})
	}

	if bs.RemoteRoom != "" {
		rows = append(rows, basic.Text{
			Content: "Remote Mining: " + bs.RemoteRoom,
			Style:   th.Text.Caption.WithColor(geom.Hex(0x4fc3f7)),
		})
	}

	if bs.ThreatLevel > 0 || bs.Hostiles > 0 {
		rows = append(rows, basic.Text{
			Content: fmt.Sprintf("Threat: Lvl %d (%d hostiles)", bs.ThreatLevel, bs.Hostiles),
			Style:   th.Text.Caption.WithColor(geom.Hex(0xff5252)),
		})
	}

	if bs.RaidTarget != "" || len(bs.ScoutedRooms) > 0 {
		var raidParts []string
		if bs.RaidTarget != "" {
			status := "planning"
			if bs.RaidActive {
				status = "ACTIVE"
			}
			raidParts = append(raidParts, fmt.Sprintf("Raid -> %s (%s)", bs.RaidTarget, status))
		}
		if len(bs.ScoutedRooms) > 0 {
			raidParts = append(raidParts, "Scouted: "+strings.Join(bs.ScoutedRooms, ", "))
		}
		rows = append(rows, basic.Text{
			Content: strings.Join(raidParts, " • "),
			Style:   th.Text.Caption.WithColor(geom.Hex(0xffab40)),
		})
	}

	return basic.Container{
		Color:   th.Color.Canvas,
		Padding: geom.All(theme.Space8),
		Radius:  th.Radius.Control,
		Border:  paint.Stroke{Width: 1, Color: th.Color.Hair},
		Child: basic.Column{
			Cross:    layout.CrossStretch,
			Spacing:  2,
			Children: rows,
		},
	}
}

func buildObjectCard(obj screeps.RoomObject, users map[string]string, gameTime int64, creepMemJSON string, trailing core.Widget, th *theme.Theme) core.Widget {
	// 1. Static Identity & Specs
	headerIcon := entityIconBadge(obj, 26, 16)
	if headerIcon == nil {
		headerIcon = basic.Container{
			Color:   objectBadgeColor(obj.Type),
			Padding: geom.Symmetric(6, 2),
			Radius:  th.Radius.Control,
			Child: basic.Text{
				Content: strings.ToUpper(obj.Type),
				Style:   th.Text.Caption.WithColor(geom.Hex(0x0f1216)),
			},
		}
	}
	headerRowChildren := []core.Widget{
		headerIcon,
		basic.Expanded{
			Child: basic.Column{
				Size:    layout.MainMin,
				Cross:   layout.CrossStart,
				Spacing: 1,
				Children: []core.Widget{
					basic.Text{
						Content: func() string {
							if obj.Name != "" {
								return obj.Name
							}
							return obj.ID
						}(),
						Style: th.Text.Body.WithWeight(600).WithColor(th.Color.Ink),
					},
					basic.Text{
						Content: obj.Type,
						Style:   th.Text.Caption.WithSize(10).WithColor(th.Color.Ink2),
					},
				},
			},
		},
	}
	if obj.User != "" {
		owner := obj.User
		if u, ok := users[obj.User]; ok {
			owner = u
		}
		headerRowChildren = append(headerRowChildren, basic.Container{
			Color:   geom.Hex(0x111c2b),
			Padding: geom.Symmetric(7, 3),
			Radius:  5,
			Border:  paint.Stroke{Width: 1, Color: geom.Hex(0x2a3f5c)},
			Child: basic.Text{
				Content: owner,
				Style:   th.Text.Caption.WithSize(10.5).WithColor(geom.Hex(0xe2e8f0)),
			},
		})
	}
	if trailing != nil {
		headerRowChildren = append(headerRowChildren, trailing)
	}
	staticRows := []core.Widget{
		basic.Row{
			Cross:    layout.CrossCenter,
			Spacing:  6,
			Children: headerRowChildren,
		},
	}
	if obj.Type == "controller" {
		staticRows = append(staticRows, basic.Text{
			Content: fmt.Sprintf("Level: RCL %d", obj.Level),
			Style:   th.Text.Caption.WithColor(th.Color.Ink2),
		})
	}
	if obj.Type == "mineral" {
		if mt, ok := obj.Raw["mineralType"].(string); ok {
			staticRows = append(staticRows, basic.Text{
				Content: "Type: " + mt,
				Style:   th.Text.Caption.WithColor(th.Color.Ink2),
			})
		}
	}

	var carryParts int
	var bodyWidget core.Widget
	if (obj.Type == "creep" || obj.Type == "powerCreep") && obj.Raw != nil {
		if body, ok := obj.Raw["body"].([]any); ok && len(body) > 0 {
			for _, item := range body {
				if m, ok := item.(map[string]any); ok {
					if t, ok := m["type"].(string); ok && t == "carry" {
						carryParts++
					}
				}
			}
			ttl := -1
			if ageAt, ok := intField(obj.Raw, "ageTime"); ok && gameTime > 0 {
				ttl = max(0, ageAt-int(gameTime))
			}
			bodyWidget = bodyPartsGraph(body, ttl, th)
		}
	}

	// 2. Dynamic Live Telemetry (uniform metric bars)
	var dynRows []core.Widget

	if obj.HitsMax > 0 {
		dynRows = append(dynRows, metricBar(fmt.Sprintf("HP  %d / %d", obj.Hits, obj.HitsMax), obj.Hits, obj.HitsMax, geom.Hex(0x4ade80), th))
	}
	if bodyWidget != nil {
		dynRows = append(dynRows, bodyWidget)
	}

	switch obj.Type {
	case "controller":
		if p, ok := intField(obj.Raw, "progress"); ok {
			if pt, ok2 := intField(obj.Raw, "progressTotal"); ok2 && pt > 0 {
				dynRows = append(dynRows, metricBar(fmt.Sprintf("Progress  %d / %d", p, pt), p, pt, geom.Hex(0x26a69a), th))
			}
		}
		if dg, ok := intField(obj.Raw, "downgradeTime"); ok && gameTime > 0 {
			rem := max(0, dg-int(gameTime))
			dynRows = append(dynRows, metricBar(fmt.Sprintf("Downgrade  %d ticks", rem), rem, 20000, geom.Hex(0x78909c), th))
		}
		if sm, ok := intField(obj.Raw, "safeMode"); ok && int64(sm) > gameTime {
			rem := sm - int(gameTime)
			dynRows = append(dynRows, metricBar(fmt.Sprintf("Safe Mode  %d ticks", rem), rem, 20000, geom.Hex(0x66bb6a), th))
		} else if smAvail, ok := intField(obj.Raw, "safeModeAvailable"); ok {
			dynRows = append(dynRows, basic.Text{
				Content: fmt.Sprintf("Safe Mode Available: %d", smAvail),
				Style:   th.Text.Caption.WithColor(th.Color.Ink2),
			})
		}
		if resUser, ok := obj.Raw["reservationUser"].(string); ok && resUser != "" {
			if resEnd, ok2 := intField(obj.Raw, "reservationEndTime"); ok2 && int64(resEnd) > gameTime {
				resOwner := resUser
				if u, ok3 := users[resUser]; ok3 && u != "" {
					resOwner = u
				}
				rem := resEnd - int(gameTime)
				dynRows = append(dynRows, metricBar(fmt.Sprintf("Reserved (%s)  %d ticks", resOwner, rem), rem, 5000, geom.Hex(0x26c6da), th))
			}
		}
		if ub, ok := intField(obj.Raw, "upgradeBlocked"); ok && int64(ub) > gameTime {
			dynRows = append(dynRows, basic.Text{
				Content: fmt.Sprintf("Upgrade Blocked: %d ticks", ub-int(gameTime)),
				Style:   th.Text.Caption.WithColor(geom.Hex(0xff5252)),
			})
		}
	case "source":
		cap, _ := intField(obj.Raw, "energyCapacity")
		if cap > 0 {
			dynRows = append(dynRows, metricBar(fmt.Sprintf("Energy  %d / %d", obj.Energy, cap), obj.Energy, cap, geom.Hex(0xffca28), th))
		}
		if regenAt, ok := intField(obj.Raw, "nextRegenerationTime"); ok && gameTime > 0 {
			rem := max(0, regenAt-int(gameTime))
			dynRows = append(dynRows, metricBar(fmt.Sprintf("Regen  %d ticks", rem), 300-min(300, rem), 300, geom.Hex(0xffa726), th))
		}
	case "mineral":
		if amt, ok := intField(obj.Raw, "mineralAmount"); ok {
			dynRows = append(dynRows, metricBar(fmt.Sprintf("Amount  %d", amt), amt, 100000, geom.Hex(0x4dd0e1), th))
		}
	case "spawn":
		if len(obj.Spawning) > 0 {
			name, _ := obj.Spawning["name"].(string)
			need, _ := intField(obj.Spawning, "needTime")
			spawnAt, _ := intField(obj.Spawning, "spawnTime")
			rem := max(0, spawnAt-int(gameTime))
			done := max(0, need-rem)
			dynRows = append(dynRows, metricBar(fmt.Sprintf("Spawning  %s (%d/%d)", name, done, need), done, max(1, need), geom.Hex(0x38bdf8), th))
		}
	case "creep", "powerCreep":
		if fat, ok := intField(obj.Raw, "fatigue"); ok && fat > 0 {
			dynRows = append(dynRows, metricBar(fmt.Sprintf("Fatigue  %d", fat), min(fat, 50), 50, geom.Hex(0xffa726), th))
		}
		if act := formatActionLog(obj); act != "" {
			actVal := strings.TrimPrefix(act, "Action: ")
			dynRows = append(dynRows, basic.Row{
				Size:    layout.MainMin,
				Cross:   layout.CrossCenter,
				Spacing: 4,
				Children: []core.Widget{
					basic.Text{
						Content: "Action:",
						Style:   th.Text.Caption.WithColor(colTextMuted),
					},
					basic.Expanded{
						Child: basic.Text{
							Content: actVal,
							Style:   th.Text.Caption.WithColor(colCyan),
						},
					},
				},
			})
		}
		if creepMemJSON != "" {
			memHeader := fmt.Sprintf("Memory.creeps[%q]", obj.Name)
			dynRows = append(dynRows, basic.Container{
				Color:   geom.Hex(0x0b111b),
				Padding: geom.All(8),
				Radius:  6,
				Border:  paint.Stroke{Width: 1, Color: colBorderDim},
				Child: basic.Column{
					Size:    layout.MainMin,
					Cross:   layout.CrossStretch,
					Spacing: 4,
					Children: []core.Widget{
						basic.Text{
							Content: memHeader,
							Style:   th.Text.Caption.WithSize(10.5).WithColor(colTextMuted),
						},
						basic.Text{
							Content: creepMemJSON,
							Style:   th.Text.Caption.WithSize(10.5).WithColor(colTextBright),
						},
					},
				},
			})
		}
	}

	if len(obj.Store) > 0 {
		totalUsed := 0
		keys := make([]string, 0, len(obj.Store))
		for k := range obj.Store {
			keys = append(keys, k)
			if v, ok := obj.Store[k].(float64); ok {
				totalUsed += int(v)
			}
		}
		sort.Strings(keys)
		var st []string
		for _, k := range keys {
			st = append(st, fmt.Sprintf("%s: %v", k, obj.Store[k]))
		}
		cap := storeCapacity(obj, carryParts)
		if cap > 0 {
			dynRows = append(dynRows, metricBar(fmt.Sprintf("Store  %d / %d (%s)", totalUsed, cap, strings.Join(st, ", ")), totalUsed, cap, geom.Hex(0xffe082), th))
		} else {
			dynRows = append(dynRows, metricBar(fmt.Sprintf("Store  %s", strings.Join(st, ", ")), totalUsed, max(1, totalUsed), geom.Hex(0xffe082), th))
		}
	}

	if decayAt, ok := intField(obj.Raw, "nextDecayTime"); ok && gameTime > 0 {
		rem := max(0, decayAt-int(gameTime))
		dynRows = append(dynRows, metricBar(fmt.Sprintf("Decay  %d ticks", rem), rem, 1000, geom.Hex(0x90a4ae), th))
	}

	children := append([]core.Widget{}, staticRows...)
	if len(dynRows) > 0 {
		children = append(children, basic.Container{
			Color:  th.Color.Hair,
			Child:  basic.SizedBox{Height: 1},
		})
		children = append(children, dynRows...)
	}

	return basic.Container{
		Color:   th.Color.Canvas,
		Padding: geom.All(theme.Space8),
		Radius:  th.Radius.Control,
		Border:  paint.Stroke{Width: 1, Color: th.Color.Hair},
		Child: basic.Column{
			Size:     layout.MainMin,
			Cross:    layout.CrossStretch,
			Spacing:  theme.Space4,
			Children: children,
		},
	}
}

func bodyPartLetter(typ string) (string, geom.Color) {
	switch typ {
	case "tough":
		return "T", geom.Hex(0x111827)
	case "work":
		return "W", geom.Hex(0x111827)
	case "carry":
		return "C", geom.Hex(0xffffff)
	case "move":
		return "M", geom.Hex(0x111827)
	case "attack":
		return "A", geom.Hex(0xffffff)
	case "ranged_attack":
		return "R", geom.Hex(0xffffff)
	case "heal":
		return "H", geom.Hex(0x111827)
	case "claim":
		return "K", geom.Hex(0xffffff)
	default:
		return "?", geom.Hex(0xffffff)
	}
}

func bodyPartsGraph(body []any, ttl int, th *theme.Theme) core.Widget {
	type partInfo struct {
		typ  string
		dead bool
	}
	parts := make([]partInfo, 0, len(body))
	for _, item := range body {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		typ, _ := m["type"].(string)
		dead := false
		if hits, ok := m["hits"].(float64); ok && hits <= 0 {
			dead = true
		}
		parts = append(parts, partInfo{typ: typ, dead: dead})
	}
	sort.Slice(parts, func(i, j int) bool {
		oi, oj := bodyPartOrder(parts[i].typ), bodyPartOrder(parts[j].typ)
		if oi != oj {
			return oi < oj
		}
		return !parts[i].dead && parts[j].dead
	})

	var cells []core.Widget
	for _, p := range parts {
		col := bodyPartColor(p.typ)
		letter, fg := bodyPartLetter(p.typ)
		if p.dead {
			col.A = 0.25
			fg.A = 0.4
		}
		cells = append(cells, basic.SizedBox{
			Width:  16,
			Height: 16,
			Child: basic.Container{
				Color:  col,
				Radius: 3,
				Child: basic.Align{
					Alignment: layout.AlignCenter,
					Child: basic.Text{
						Content: letter,
						Style:   th.Text.Caption.WithSize(9.5).WithWeight(700).WithColor(fg),
					},
				},
			},
		})
	}

	headerRow := []core.Widget{
		basic.Expanded{
			Child: basic.Text{
				Content: fmt.Sprintf("Body (%d parts)", len(cells)),
				Style:   th.Text.Caption.WithColor(colTextMuted),
			},
		},
	}
	if ttl >= 0 {
		headerRow = append(headerRow, basic.Text{
			Content: fmt.Sprintf("TTL %d", ttl),
			Style:   th.Text.Caption.WithColor(colTextMuted),
		})
	}

	return basic.Column{
		Size:    layout.MainMin,
		Cross:   layout.CrossStretch,
		Spacing: 5,
		Children: []core.Widget{
			basic.Row{
				Cross:    layout.CrossCenter,
				Children: headerRow,
			},
			basic.Wrap{
				Spacing:    3,
				RunSpacing: 3,
				Children:   cells,
			},
		},
	}
}

func bodyPartOrder(typ string) int {
	switch typ {
	case "work":
		return 0
	case "carry":
		return 1
	case "move":
		return 2
	case "attack":
		return 3
	case "ranged_attack":
		return 4
	case "heal":
		return 5
	case "claim":
		return 6
	case "tough":
		return 7
	default:
		return 8
	}
}

func bodyPartColor(typ string) geom.Color {
	switch typ {
	case "work":
		return geom.Hex(0xffe56d)
	case "carry":
		return geom.Hex(0x90a4ae)
	case "move":
		return geom.Hex(0x4fc3f7)
	case "attack":
		return geom.Hex(0xef5350)
	case "ranged_attack":
		return geom.Hex(0x5c6bc0)
	case "heal":
		return geom.Hex(0x66bb6a)
	case "claim":
		return geom.Hex(0xab47bc)
	case "tough":
		return geom.Hex(0xeceff1)
	default:
		return geom.Hex(0xb0bec5)
	}
}

func storeCapacity(obj screeps.RoomObject, carryParts int) int {
	if obj.Raw != nil {
		if sc, ok := obj.Raw["storeCapacityResource"].(map[string]any); ok {
			if e, ok := sc["energy"].(float64); ok && e > 0 {
				return int(e)
			}
		}
		if sc, ok := intField(obj.Raw, "storeCapacity"); ok && sc > 0 {
			return sc
		}
	}
	if obj.Type == "creep" && carryParts > 0 {
		return carryParts * 50
	}
	return 0
}

func metricBar(label string, val, maxVal int, fill geom.Color, th *theme.Theme) core.Widget {
	ratio := float32(0)
	if maxVal > 0 {
		ratio = geom.Clamp32(float32(val)/float32(maxVal), 0, 1)
	}
	const barW = float32(274)
	var header core.Widget
	if left, right, ok := strings.Cut(label, "  "); ok {
		header = basic.Row{
			Cross: layout.CrossCenter,
			Children: []core.Widget{
				basic.Expanded{
					Child: basic.Text{Content: left, Style: th.Text.Caption.WithColor(colTextMuted)},
				},
				basic.Text{Content: right, Style: th.Text.Caption.WithColor(colTextBright)},
			},
		}
	} else {
		header = basic.Text{Content: label, Style: th.Text.Caption.WithColor(colTextBright)}
	}
	return basic.Column{
		Size:    layout.MainMin,
		Cross:   layout.CrossStretch,
		Spacing: 3,
		Children: []core.Widget{
			header,
			basic.Container{
				Color:  geom.Hex(0x1b2636),
				Radius: 3.5,
				Child: basic.Align{
					Alignment: layout.AlignCenterLeft,
					Child: basic.SizedBox{
						Width:  barW * ratio,
						Height: 7,
						Child:  basic.Container{Color: fill, Radius: 3.5},
					},
				},
			},
		},
	}
}

func objectBadgeColor(typ string) geom.Color {
	switch typ {
	case "spawn":
		return geom.Hex(0x66bb6a)
	case "creep", "powerCreep":
		return geom.Hex(0x42a5f5)
	case "controller":
		return geom.Hex(0x80cbc4)
	case "source":
		return geom.Hex(0xffd54f)
	case "mineral":
		return geom.Hex(0x4dd0e1)
	case "tower":
		return geom.Hex(0xef5350)
	case "extension":
		return geom.Hex(0xffee58)
	case "storage", "terminal", "factory":
		return geom.Hex(0xab47bc)
	default:
		return geom.Hex(0xb0bec5)
	}
}

func intField(raw map[string]any, key string) (int, bool) {
	if raw == nil {
		return 0, false
	}
	v, ok := raw[key].(float64)
	return int(v), ok
}

func pickAutoRivalRoom(world *engine.World, homeRoom, primaryOwner string) string {
	r := world.GetRoom(homeRoom)
	if r == nil {
		return ""
	}
	dirs := []struct {
		exitC int
		dx    int
		dy    int
	}{
		{engine.FIND_EXIT_TOP, 0, -1},
		{engine.FIND_EXIT_RIGHT, 1, 0},
		{engine.FIND_EXIT_BOTTOM, 0, 1},
		{engine.FIND_EXIT_LEFT, -1, 0},
	}
	var fallbackWithCtrl string
	var fallbackAny string
	for _, d := range dirs {
		if !r.RoomHasExitEdge(d.exitC) {
			continue
		}
		adj, ok := engine.AdjacentRoomName(homeRoom, d.dx, d.dy)
		if !ok {
			continue
		}
		if fallbackAny == "" {
			fallbackAny = adj
		}
		nr := world.GetRoom(adj)
		if nr == nil {
			continue
		}
		_, owner := world.RoomOwner(adj)
		if owner != "" && owner != primaryOwner {
			return adj
		}
		if fallbackWithCtrl == "" {
			world.RLock()
			for _, e := range nr.Entities {
				if e.Type == engine.STRUCTURE_CONTROLLER {
					fallbackWithCtrl = adj
					break
				}
			}
			world.RUnlock()
		}
	}
	if fallbackWithCtrl != "" {
		return fallbackWithCtrl
	}
	return fallbackAny
}

func iconPlay() *paint.Path {
	p := &paint.Path{}
	p.MoveTo(8, 5)
	p.LineTo(19, 12)
	p.LineTo(8, 19)
	p.Close()
	return p
}

func iconPause() *paint.Path {
	p := &paint.Path{}
	p.MoveTo(6, 5)
	p.LineTo(10, 5)
	p.LineTo(10, 19)
	p.LineTo(6, 19)
	p.Close()
	p.MoveTo(14, 5)
	p.LineTo(18, 5)
	p.LineTo(18, 19)
	p.LineTo(14, 19)
	p.Close()
	return p
}

func iconStep() *paint.Path {
	p := &paint.Path{}
	p.MoveTo(5, 6)
	p.LineTo(14, 12)
	p.LineTo(5, 18)
	p.Close()
	p.MoveTo(16, 6)
	p.LineTo(19, 6)
	p.LineTo(19, 18)
	p.LineTo(16, 18)
	p.Close()
	return p
}

func iconReset() *paint.Path {
	// Circular counter-clockwise refresh arrow on a 24x24 grid.
	p := &paint.Path{}
	const (
		cx, cy = 12.0, 12.0
		rOut   = 7.5
		rIn    = 5.2
		steps  = 16
	)
	startDeg := 145.0
	endDeg := 420.0
	for i := 0; i <= steps; i++ {
		ang := (startDeg + (endDeg-startDeg)*float64(i)/steps) * math.Pi / 180
		x := float32(cx + rOut*math.Cos(ang))
		y := float32(cy + rOut*math.Sin(ang))
		if i == 0 {
			p.MoveTo(x, y)
		} else {
			p.LineTo(x, y)
		}
	}
	for i := steps; i >= 0; i-- {
		ang := (startDeg + (endDeg-startDeg)*float64(i)/steps) * math.Pi / 180
		x := float32(cx + rIn*math.Cos(ang))
		y := float32(cy + rIn*math.Sin(ang))
		p.LineTo(x, y)
	}
	p.Close()
	// Arrowhead pointing down-left at ~145 deg
	p.MoveTo(3.5, 11.5)
	p.LineTo(9.5, 11.5)
	p.LineTo(5.5, 16.8)
	p.Close()
	return p
}

func iconRotateLeft() *paint.Path {
	// 90-degree CCW arc with arrowhead on top-left.
	p := &paint.Path{}
	const (
		cx, cy = 12.0, 13.0
		rOut   = 7.0
		rIn    = 4.8
		steps  = 12
	)
	startDeg := -90.0
	endDeg := 135.0
	for i := 0; i <= steps; i++ {
		ang := (startDeg + (endDeg-startDeg)*float64(i)/steps) * math.Pi / 180
		x := float32(cx + rOut*math.Cos(ang))
		y := float32(cy + rOut*math.Sin(ang))
		if i == 0 {
			p.MoveTo(x, y)
		} else {
			p.LineTo(x, y)
		}
	}
	for i := steps; i >= 0; i-- {
		ang := (startDeg + (endDeg-startDeg)*float64(i)/steps) * math.Pi / 180
		x := float32(cx + rIn*math.Cos(ang))
		y := float32(cy + rIn*math.Sin(ang))
		p.LineTo(x, y)
	}
	p.Close()
	// Left-pointing arrowhead at top (12, 6)
	p.MoveTo(7.5, 6.0)
	p.LineTo(12.5, 2.8)
	p.LineTo(12.5, 9.2)
	p.Close()
	return p
}

func iconRotateRight() *paint.Path {
	// 90-degree CW arc with arrowhead on top-right.
	p := &paint.Path{}
	const (
		cx, cy = 12.0, 13.0
		rOut   = 7.0
		rIn    = 4.8
		steps  = 12
	)
	startDeg := 45.0
	endDeg := 270.0
	for i := 0; i <= steps; i++ {
		ang := (startDeg + (endDeg-startDeg)*float64(i)/steps) * math.Pi / 180
		x := float32(cx + rOut*math.Cos(ang))
		y := float32(cy + rOut*math.Sin(ang))
		if i == 0 {
			p.MoveTo(x, y)
		} else {
			p.LineTo(x, y)
		}
	}
	for i := steps; i >= 0; i-- {
		ang := (startDeg + (endDeg-startDeg)*float64(i)/steps) * math.Pi / 180
		x := float32(cx + rIn*math.Cos(ang))
		y := float32(cy + rIn*math.Sin(ang))
		p.LineTo(x, y)
	}
	p.Close()
	// Right-pointing arrowhead at top (12, 6)
	p.MoveTo(16.5, 6.0)
	p.LineTo(11.5, 2.8)
	p.LineTo(11.5, 9.2)
	p.Close()
	return p
}

func iconFitView() *paint.Path {
	// Four corner brackets (fit-to-screen).
	p := &paint.Path{}
	addRect := func(x, y, w, h float32) {
		p.MoveTo(x, y)
		p.LineTo(x+w, y)
		p.LineTo(x+w, y+h)
		p.LineTo(x, y+h)
		p.Close()
	}
	// Top-left
	addRect(4, 4, 6, 2)
	addRect(4, 4, 2, 6)
	// Top-right
	addRect(14, 4, 6, 2)
	addRect(18, 4, 2, 6)
	// Bottom-left
	addRect(4, 18, 6, 2)
	addRect(4, 14, 2, 6)
	// Bottom-right
	addRect(14, 18, 6, 2)
	addRect(18, 14, 2, 6)
	return p
}

var (
	mainWinMu sync.RWMutex
	mainWin   *gowaku.WindowHandle
)

func setMainWindow(w *gowaku.WindowHandle) {
	mainWinMu.Lock()
	mainWin = w
	mainWinMu.Unlock()
}

func activateMainWindow() {
	gowaku.Post(func() {
		mainWinMu.RLock()
		w := mainWin
		mainWinMu.RUnlock()
		if w != nil {
			w.Show()
			w.Activate()
		}
	})
}

// acquireSingleInstance uses a Linux abstract Unix domain socket so the kernel
// automatically cleans it up on exit/crash (no stale lock files). If another
func defaultStatePath() string {
	// Prefer a local world_state.json if one already exists in the working directory.
	if _, err := os.Stat("world_state.json"); err == nil {
		if abs, absErr := filepath.Abs("world_state.json"); absErr == nil {
			return abs
		}
		return "world_state.json"
	}
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "go-screeps", "world_state.json")
	}
	return "world_state.json"
}

func singleInstanceSocket() string {
	if runtime.GOOS == "linux" {
		return fmt.Sprintf("@go-screeps-instance-%d", os.Getuid())
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("go-screeps-%d.sock", os.Getuid()))
}

// acquireSingleInstance enforces a single running instance via a Unix domain socket.
// If another instance is already running, it signals it to activate and returns (nil, false).
func acquireSingleInstance() (net.Listener, bool) {
	socketName := singleInstanceSocket()
	ln, err := net.Listen("unix", socketName)
	if err == nil {
		go func() {
			for {
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				_ = conn.Close()
				activateMainWindow()
			}
		}()
		return ln, true
	}
	// Another instance is running: nudge it to raise/activate its window.
	if conn, dialErr := net.DialTimeout("unix", socketName, 500*time.Millisecond); dialErr == nil {
		_, _ = conn.Write([]byte("ACTIVATE\n"))
		_ = conn.Close()
		return nil, false
	}
	// On non-Linux Unix (e.g. macOS), a crashed process can leave a stale socket file behind.
	if runtime.GOOS != "linux" && !strings.HasPrefix(socketName, "@") {
		_ = os.Remove(socketName)
		if ln2, err2 := net.Listen("unix", socketName); err2 == nil {
			go func() {
				for {
					conn, err := ln2.Accept()
					if err != nil {
						return
					}
					_ = conn.Close()
					activateMainWindow()
				}
			}()
			return ln2, true
		}
	}
	return nil, true
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	server := flag.String("server", "http://localhost:21025", "Screeps server base URL")
	user := flag.String("user", "StarterBot", "Screeps username/email")
	pass := flag.String("pass", "screeps123", "Screeps password")
	room := flag.String("room", "W9N7", "Initial room name")
	local := flag.Bool("local", false, "Run embedded Go engine + Lua bot locally (no Node.js server required)")
	migrate := flag.Bool("migrate", false, "Import full live state (structures, creeps, GameTime) from -db in -local mode")
	dbPath := flag.String("db", "", "Optional path to Screeps db.json to import rooms from in -local mode")
	statePath := flag.String("state", defaultStatePath(), "Path to auto-save/load local world + Lua Memory state (empty disables persistence)")
	botSpec := flag.String("bot", "", "Optional comma-separated Name=Dir@Room bots for -local mode")
	rival := flag.String("rival", "auto", "Rival room for competing embedded raider Lua bot in -local mode ('auto' picks a connected neighbor, '' disables)")
	tick := flag.Duration("tick", 250*time.Millisecond, "Tick period in -local mode")
	seed := flag.Int64("seed", 42, "Room generation seed in -local mode")
	multi := flag.Bool("multi", false, "Allow multiple concurrent viewer windows (skip single-instance lock)")
	flag.Parse()

	if *migrate && strings.TrimSpace(*dbPath) == "" {
		return fmt.Errorf("-migrate requires an explicit -db path")
	}

	if !*multi {
		if ln, ok := acquireSingleInstance(); !ok {
			log.Printf("go-screeps is already running; activated existing window")
			return nil
		} else if ln != nil {
			defer func() {
				_ = ln.Close()
			}()
		}
	}

	app := gowaku.App{
		Title:     "Go-Screeps",
		Width:     1280,
		Height:    800,
		ThemeMode: gowaku.ThemeDark,
		Home: viewerApp{
			serverURL:  *server,
			username:   *user,
			password:   *pass,
			room:       *room,
			local:      *local,
			migrate:    *migrate,
			dbPath:     *dbPath,
			statePath:  *statePath,
			botSpec:    *botSpec,
			rival:      *rival,
			tickPeriod: *tick,
			seed:       *seed,
		},
	}
	return gowaku.Run(app)
}
