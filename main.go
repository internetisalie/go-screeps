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
	"github.com/bankai-tensei/gowaku/widgets/interact"
	"github.com/bankai-tensei/gowaku/widgets/overlay"
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
	{label: "4x", period: 60 * time.Millisecond},
	{label: "10x", period: 25 * time.Millisecond},
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
	lastTick   int64
	tickAt     time.Time
	tickPeriod time.Duration

	dragging  bool
	orbiting  bool
	dragMoved bool
	lastPos   geom.Offset
	downPos   geom.Offset
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

	s.onUpdate = func() {
		gowaku.Post(func() {
			if ctx.Err() != nil {
				return
			}
			s.SetState(func() {
				if s.provider != nil {
					s.applySnapshot(s.provider.Snapshot())
				}
			})
			s.signal.Notify()
		})
	}

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

func (s *viewerState) startLocalSim(ignoreSaved bool) {
	v := s.cfg

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
				dbPath = "/home/mini/Documents/src/screeps/server/db.json"
			}
			if err := world.ImportFullDBJSON(dbPath); err != nil {
				log.Printf("local db migrate %q failed: %v (falling back to generated room)", dbPath, err)
			}
		} else if dbPath != "" {
			if err := world.LoadFromDBJSON(dbPath); err != nil {
				log.Printf("local db load %q failed: %v (falling back to generated room)", dbPath, err)
			}
		}
	}
	if world.GetRoom(roomName) == nil {
		world.GenerateRoom(roomName, v.seed)
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
					world.GenerateRoom(rivalRoom, v.seed)
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
	s.lastTick = 0
	s.tickAt = time.Time{}

	simCtx, simCancel := context.WithCancel(s.appCtx)
	s.simCancel = simCancel
	s.localRunner = runner
	s.provider = runner
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
		if !s.tickAt.IsZero() {
			dt := now.Sub(s.tickAt)
			if dt >= 10*time.Millisecond && dt <= 2*time.Second {
				s.tickPeriod = (s.tickPeriod*3 + dt) / 4
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
	}
	s.updateCreepPositions()
	if s.cam.FollowID != "" {
		found := false
		for _, obj := range s.snap.Objects {
			if obj.ID == s.cam.FollowID {
				s.cam.SelectedX = obj.X
				s.cam.SelectedY = obj.Y
				found = true
				break
			}
		}
		if !found {
			s.cam.FollowID = ""
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
	for _, rn := range iso.NeighborRoomNames(newRoom, 1) {
		if w.GetRoom(rn) == nil {
			var h int64 = s.cfg.seed
			for i := 0; i < len(rn); i++ {
				h = h*31 + int64(rn[i])
			}
			w.GenerateRoom(rn, h)
		}
	}
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
			s.cam.OrbitYaw(-dx * 0.008)
			s.signal.Notify()
			return
		}
		if !s.dragging {
			return
		}
		dx := e.Pos.X - s.lastPos.X
		dy := e.Pos.Y - s.lastPos.Y
		s.lastPos = e.Pos
		if e.Pos.Sub(s.downPos).Length() > 4 {
			s.dragMoved = true
		}
		s.cam.PanX += dx
		s.cam.PanY += dy
		s.recenterOnPan()
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
					s.cam.FollowID = ""
					for _, obj := range s.snap.Objects {
						if obj.X == gx && obj.Y == gy && (obj.Type == "creep" || obj.Type == "powerCreep") {
							s.cam.FollowID = obj.ID
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

	tps := 0.0
	if s.tickPeriod > 0 {
		tps = 1.0 / s.tickPeriod.Seconds()
	}
	visLabel := "Vis: On"
	if !s.painter.ShowVisuals {
		visLabel = "Vis: Off"
	}

	headerChildren := []core.Widget{
		basic.Text{
			Content: "Screeps",
			Style:   th.Text.Body.WithColor(th.Color.Ink),
		},
		basic.Text{
			Content: fmt.Sprintf("t%d (%.1f/s) • %d objs • %.0f%% • %.0f°", s.snap.GameTime, tps, len(s.snap.Objects), s.cam.Zoom*100, float64(s.cam.YawRadians())*180/math.Pi),
			Style:   th.Text.Caption.WithColor(th.Color.Ink2),
		},
		basic.Expanded{Child: basic.SizedBox{}},
	}
	if s.localRunner != nil {
		pauseLabel := "Pause"
		pauseIcon := iconPause()
		if s.localRunner.Paused() {
			pauseLabel = "Play"
			pauseIcon = iconPlay()
		}
		headerChildren = append(headerChildren,
			overlay.Tooltip{
				Message: pauseLabel,
				Child: control.Button{
					Label:     pauseLabel,
					Icon:      pauseIcon,
					HideLabel: true,
					Size:      control.ButtonSmall,
					OnPress: func() {
						s.SetState(func() {
							s.localRunner.SetPaused(!s.localRunner.Paused())
						})
					},
				},
			},
			overlay.Tooltip{
				Message: "Step +1 tick",
				Child: control.Button{
					Label:     "Step",
					Icon:      iconStep(),
					HideLabel: true,
					Size:      control.ButtonSmall,
					OnPress: func() {
						s.localRunner.StepTick()
						s.SetState(func() {
							s.applySnapshot(s.provider.Snapshot())
						})
						s.signal.Notify()
					},
				},
			},
			overlay.Tooltip{
				Message: "Simulation speed",
				Child: control.Button{
					Label: localSpeeds[s.speedIdx].label,
					Size:  control.ButtonSmall,
					OnPress: func() {
						s.SetState(func() {
							s.speedIdx = (s.speedIdx + 1) % len(localSpeeds)
							spd := localSpeeds[s.speedIdx]
							s.localRunner.SetTickPeriod(spd.period)
							s.tickPeriod = spd.period
						})
					},
				},
			},
			overlay.Tooltip{
				Message: "Reset simulation",
				Child: control.Button{
					Label:     "Reset Sim",
					Icon:      iconReset(),
					HideLabel: true,
					Size:      control.ButtonSmall,
					OnPress: func() {
						s.SetState(func() {
							s.startLocalSim(true)
						})
						s.signal.Notify()
					},
				},
			},
		)
	}
	headerChildren = append(headerChildren,
		control.Button{
			Label: visLabel,
			Size:  control.ButtonSmall,
			OnPress: func() {
				s.SetState(func() { s.painter.ShowVisuals = !s.painter.ShowVisuals })
				s.signal.Notify()
			},
		},
		overlay.Tooltip{
			Message: "Rotate -90°",
			Child: control.Button{
				Label:     "Rotate -90",
				Icon:      iconRotateLeft(),
				HideLabel: true,
				Size:      control.ButtonSmall,
				OnPress: func() {
					s.SetState(func() { s.cam.OrbitYaw(-math.Pi / 2) })
					s.signal.Notify()
				},
			},
		},
		overlay.Tooltip{
			Message: "Rotate +90°",
			Child: control.Button{
				Label:     "Rotate +90",
				Icon:      iconRotateRight(),
				HideLabel: true,
				Size:      control.ButtonSmall,
				OnPress: func() {
					s.SetState(func() { s.cam.OrbitYaw(math.Pi / 2) })
					s.signal.Notify()
				},
			},
		},
		basic.SizedBox{
			Width: 78,
			Child: field.Input{
				Controller:  s.roomCtrl,
				Value:       s.initRoom,
				Label:       "Room",
				HideLabel:   true,
				Placeholder: "W9N8",
				OnSubmit:    func(val string) { s.switchRoom(val) },
			},
		},
		control.Button{
			Label:   "Go",
			Size:    control.ButtonSmall,
			OnPress: func() { s.switchRoom(s.roomCtrl.Text()) },
		},
		overlay.Tooltip{
			Message: "Reset view",
			Child: control.Button{
				Label:     "Reset View",
				Icon:      iconFitView(),
				HideLabel: true,
				Size:      control.ButtonSmall,
				OnPress: func() {
					s.SetState(func() {
						yaw := s.cam.YawRadians()
						s.cam = iso.NewCamera()
						s.cam.SetYaw(yaw)
						s.cam.FitToSize(s.painter.LastSize)
					})
					s.signal.Notify()
				},
			},
		},
	)

	header := basic.Container{
		Color:   th.Color.Card,
		Padding: geom.Symmetric(theme.Space8, theme.Space4),
		Border:  paint.Stroke{Width: 1, Color: th.Color.Hair},
		Child: basic.Row{
			Cross:    layout.CrossCenter,
			Spacing:  theme.Space4,
			Children: headerChildren,
		},
	}

	viewport := interact.Listener{
		OnPointer: s.handlePointer,
		Child:     control.CustomPaint{Painter: s.painter, Repaint: &s.signal},
	}

	inspector := basic.SizedBox{
		Width: 320,
		Child: basic.Container{
			Color:   th.Color.Card,
			Padding: geom.All(theme.Space12),
			Border:  paint.Stroke{Width: 1, Color: th.Color.Hair},
			Child:   basic.Scroll{Child: s.buildInspector(th)},
		},
	}

	return basic.Column{
		Cross: layout.CrossStretch,
		Children: []core.Widget{
			header,
			basic.Expanded{
				Child: basic.Row{
					Cross: layout.CrossStretch,
					Children: []core.Widget{
						basic.Expanded{Child: viewport},
						inspector,
					},
				},
			},
		},
	}
}

func (s *viewerState) selectAndCenter(obj screeps.RoomObject, follow bool) {
	s.SetState(func() {
		s.cam.SelectedX = obj.X
		s.cam.SelectedY = obj.Y
		if follow {
			s.cam.FollowID = obj.ID
		} else {
			s.cam.FollowID = ""
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
		terrainName := "plain"
		if len(s.snap.Terrain) >= iso.GridSize*iso.GridSize {
			switch s.snap.Terrain[s.cam.SelectedY*iso.GridSize+s.cam.SelectedX] {
			case '1':
				terrainName = "wall"
			case '2':
				terrainName = "swamp"
			case '3':
				terrainName = "wall+swamp"
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

		followTag := ""
		if s.cam.FollowID != "" {
			followTag = " [Tracking Creep]"
		}
		lines = append(lines, basic.Text{
			Content: fmt.Sprintf("Tile (%d, %d) • %s%s%s", s.cam.SelectedX, s.cam.SelectedY, terrainName, exitTag, followTag),
			Style:   th.Text.Body.WithColor(th.Color.Accent),
		})
		if s.cam.FollowID != "" {
			lines = append(lines, control.Button{
				Label: "Stop Tracking",
				Size:  control.ButtonSmall,
				OnPress: func() {
					s.SetState(func() { s.cam.FollowID = "" })
				},
			})
		}

		var tileObjs []screeps.RoomObject
		for _, obj := range s.snap.Objects {
			if obj.X == s.cam.SelectedX && obj.Y == s.cam.SelectedY {
				tileObjs = append(tileObjs, obj)
			}
		}
		sort.Slice(tileObjs, func(i, j int) bool {
			if tileObjs[i].Type != tileObjs[j].Type {
				return tileObjs[i].Type < tileObjs[j].Type
			}
			if tileObjs[i].Name != tileObjs[j].Name {
				return tileObjs[i].Name < tileObjs[j].Name
			}
			return tileObjs[i].ID < tileObjs[j].ID
		})
		for _, obj := range tileObjs {
			lines = append(lines, buildObjectCard(obj, s.snap.Users, s.snap.GameTime, th))
		}
		if len(tileObjs) == 0 {
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

func buildObjectCard(obj screeps.RoomObject, users map[string]string, gameTime int64, th *theme.Theme) core.Widget {
	// 1. Static Identity & Specs
	staticRows := []core.Widget{
		basic.Row{
			Cross:   layout.CrossCenter,
			Spacing: theme.Space8,
			Children: []core.Widget{
				basic.Container{
					Color:   objectBadgeColor(obj.Type),
					Padding: geom.Symmetric(6, 2),
					Radius:  th.Radius.Control,
					Child: basic.Text{
						Content: strings.ToUpper(obj.Type),
						Style:   th.Text.Caption.WithColor(geom.Hex(0x0f1216)),
					},
				},
				basic.Expanded{
					Child: basic.Text{
						Content: func() string {
							if obj.Name != "" {
								return obj.Name
							}
							return obj.ID
						}(),
						Style: th.Text.Body.WithColor(th.Color.Ink),
					},
				},
			},
		},
	}

	if obj.Name != "" {
		staticRows = append(staticRows, basic.Text{
			Content: "ID: " + obj.ID,
			Style:   th.Text.Caption.WithColor(th.Color.Ink2),
		})
	}
	if obj.User != "" {
		owner := obj.User
		if u, ok := users[obj.User]; ok {
			owner = u
		}
		staticRows = append(staticRows, basic.Text{
			Content: "Owner: " + owner,
			Style:   th.Text.Caption.WithColor(th.Color.Ink2),
		})
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
	if (obj.Type == "creep" || obj.Type == "powerCreep") && obj.Raw != nil {
		if body, ok := obj.Raw["body"].([]any); ok && len(body) > 0 {
			for _, item := range body {
				if m, ok := item.(map[string]any); ok {
					if t, ok := m["type"].(string); ok && t == "carry" {
						carryParts++
					}
				}
			}
			staticRows = append(staticRows, bodyPartsGraph(body, th))
		}
	}

	// 2. Dynamic Live Telemetry (uniform metric bars)
	var dynRows []core.Widget

	if obj.HitsMax > 0 {
		dynRows = append(dynRows, metricBar(fmt.Sprintf("HP  %d / %d", obj.Hits, obj.HitsMax), obj.Hits, obj.HitsMax, geom.Hex(0x66bb6a), th))
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
			dynRows = append(dynRows, metricBar(fmt.Sprintf("Spawning %s  (%d / %d)", name, done, need), done, max(1, need), geom.Hex(0x00e676), th))
		}
	case "creep", "powerCreep":
		if ageAt, ok := intField(obj.Raw, "ageTime"); ok && gameTime > 0 {
			ttl := max(0, ageAt-int(gameTime))
			dynRows = append(dynRows, metricBar(fmt.Sprintf("TTL  %d / 1500", ttl), ttl, 1500, geom.Hex(0x42a5f5), th))
		}
		fat, _ := intField(obj.Raw, "fatigue")
		dynRows = append(dynRows, metricBar(fmt.Sprintf("Fatigue  %d", fat), min(fat, 50), 50, geom.Hex(0xffa726), th))
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
			Cross:    layout.CrossStretch,
			Spacing:  theme.Space4,
			Children: children,
		},
	}
}

func bodyPartsGraph(body []any, th *theme.Theme) core.Widget {
	type partInfo struct {
		typ  string
		dead bool
	}
	counts := map[string]int{}
	parts := make([]partInfo, 0, len(body))
	for _, item := range body {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		typ, _ := m["type"].(string)
		counts[typ]++
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
		if p.dead {
			col.A = 0.25
		}
		cells = append(cells, basic.SizedBox{
			Width:  10,
			Height: 10,
			Child: basic.Container{
				Color:  col,
				Radius: 2,
			},
		})
	}

	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return bodyPartOrder(keys[i]) < bodyPartOrder(keys[j])
	})
	var legend []core.Widget
	for _, k := range keys {
		legend = append(legend, basic.Row{
			Cross:   layout.CrossCenter,
			Spacing: 3,
			Children: []core.Widget{
				basic.SizedBox{
					Width:  7,
					Height: 7,
					Child:  basic.Container{Color: bodyPartColor(k), Radius: 3.5},
				},
				basic.Text{
					Content: fmt.Sprintf("%dx%s", counts[k], k),
					Style:   th.Text.Caption.WithColor(th.Color.Ink2),
				},
			},
		})
	}

	return basic.Column{
		Cross:   layout.CrossStretch,
		Spacing: 4,
		Children: []core.Widget{
			basic.Text{
				Content: fmt.Sprintf("Body (%d/50)", len(cells)),
				Style:   th.Text.Caption.WithColor(th.Color.Ink2),
			},
			basic.Wrap{
				Spacing:    3,
				RunSpacing: 3,
				Children:   cells,
			},
			basic.Wrap{
				Spacing:    8,
				RunSpacing: 2,
				Children:   legend,
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
	const barW = float32(276)
	return basic.Column{
		Cross:   layout.CrossStretch,
		Spacing: 2,
		Children: []core.Widget{
			basic.Text{Content: label, Style: th.Text.Caption.WithColor(th.Color.Ink)},
			basic.Container{
				Color:  geom.Hex(0x263238),
				Radius: 3,
				Child: basic.Align{
					Alignment: layout.AlignCenterLeft,
					Child: basic.SizedBox{
						Width:  barW * ratio,
						Height: 6,
						Child:  basic.Container{Color: fill, Radius: 3},
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
		return "@go-screeps-instance"
	}
	return filepath.Join(os.TempDir(), "go-screeps.sock")
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
	flag.Parse()

	if ln, ok := acquireSingleInstance(); !ok {
		log.Printf("go-screeps is already running; activated existing window")
		return
	} else if ln != nil {
		defer func() {
			_ = ln.Close()
			if runtime.GOOS != "linux" {
				_ = os.Remove(singleInstanceSocket())
			}
		}()
	}

	app := gowaku.App{
		Title:     "Go-Screeps (Isometric)",
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
	if err := gowaku.Run(app); err != nil {
		log.Fatal(err)
	}
}
