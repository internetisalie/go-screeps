package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"go-screeps/internal/engine"
	"go-screeps/internal/iso"
	"go-screeps/internal/lua"
	"go-screeps/internal/screeps"

	"github.com/bankai-tensei/gowaku/core"
	"github.com/bankai-tensei/gowaku/geom"
	"github.com/bankai-tensei/gowaku/layout"
	"github.com/bankai-tensei/gowaku/paint"
	"github.com/bankai-tensei/gowaku/theme"
	"github.com/bankai-tensei/gowaku/widgets/basic"
	"github.com/bankai-tensei/gowaku/widgets/control"
	"github.com/bankai-tensei/gowaku/widgets/field"
	"github.com/bankai-tensei/gowaku/widgets/interact"
	"github.com/bankai-tensei/gowaku/widgets/overlay"
)

// Cached vector icon paths (kept stable for gowaku's icon atlas).
var (
	sbIconPlay  = iconPlay()
	sbIconPause = iconPause()
	sbIconStep  = iconStep()
	sbIconReset = iconReset()
	sbIconEye   = mustSVGPath("M2 12 C5 6.5 9 4.5 12 4.5 C15 4.5 19 6.5 22 12 C19 17.5 15 19.5 12 19.5 C9 19.5 5 17.5 2 12 Z M12 8.8 C13.8 8.8 15.2 10.2 15.2 12 C15.2 13.8 13.8 15.2 12 15.2 C10.2 15.2 8.8 13.8 8.8 12 C8.8 10.2 10.2 8.8 12 8.8 Z")
)

func mustSVGPath(d string) *paint.Path {
	p, _ := paint.ParseSVGPath(d)
	return p
}

//go:embed assets/icons/events/event_icons.json
var eventIconsJSON []byte

//go:embed assets/icons/entities/entity_icons.json
var entityIconsJSON []byte

type eventIconSpec struct {
	path  *paint.Path
	color geom.Color
}

var (
	eventIconMap  = loadIconManifest(eventIconsJSON)
	entityIconMap = loadIconManifest(entityIconsJSON)
)

func parseHexColor(s string, fallback geom.Color) geom.Color {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 {
		return fallback
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return fallback
	}
	return geom.Hex(uint32(v))
}

func loadIconManifest(raw []byte) map[string]eventIconSpec {
	var manifest struct {
		Icons map[string]struct {
			Color string `json:"color"`
			Path  string `json:"path"`
		} `json:"icons"`
	}
	out := make(map[string]eventIconSpec)
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return out
	}
	for k, v := range manifest.Icons {
		if p, err := paint.ParseSVGPath(v.Path); err == nil && p != nil {
			out[k] = eventIconSpec{
				path:  p,
				color: parseHexColor(v.Color, colTextMuted),
			}
		}
	}
	return out
}

// entityIconKey resolves a RoomObject to its entity_icons.json key (supporting creep role detection).
func entityIconKey(obj screeps.RoomObject) string {
	if obj.Type == "creep" || obj.Type == "powerCreep" {
		low := strings.ToLower(obj.Name)
		switch {
		case strings.Contains(low, "mineral"):
			return "mineralHarvester"
		case strings.Contains(low, "harvester") || strings.Contains(low, "miner"):
			return "harvester"
		case strings.Contains(low, "hauler") || strings.Contains(low, "carrier") || strings.Contains(low, "transporter"):
			return "hauler"
		case strings.Contains(low, "upgrader"):
			return "upgrader"
		case strings.Contains(low, "pioneer") || strings.Contains(low, "remotebuilder"):
			return "pioneer"
		case strings.Contains(low, "builder") || strings.Contains(low, "repair"):
			return "builder"
		case strings.Contains(low, "dismantler") || strings.Contains(low, "siege"):
			return "dismantler"
		case strings.Contains(low, "healer") || strings.Contains(low, "medic"):
			return "healer"
		case strings.Contains(low, "skirmisher") || strings.Contains(low, "ranger") || strings.Contains(low, "archer"):
			return "skirmisher"
		case strings.Contains(low, "defender") || strings.Contains(low, "brawler") || strings.Contains(low, "attacker") || strings.Contains(low, "guard"):
			return "defender"
		case strings.Contains(low, "claimer") || strings.Contains(low, "reserver"):
			return "claimer"
		default:
			return "creep"
		}
	}
	if _, ok := entityIconMap[obj.Type]; ok {
		return obj.Type
	}
	return "creep"
}

func entityIconWidget(obj screeps.RoomObject, size float32) core.Widget {
	spec, ok := entityIconMap[entityIconKey(obj)]
	if !ok {
		return nil
	}
	return basic.SizedBox{
		Width:  size,
		Height: size,
		Child:  control.CustomPaint{Painter: strokedEventIconPainter{path: spec.path, color: spec.color}},
	}
}

func entityIconBadge(obj screeps.RoomObject, boxSize, iconSize float32) core.Widget {
	spec, ok := entityIconMap[entityIconKey(obj)]
	if !ok {
		return nil
	}
	bg := spec.color
	bg.A = 0.14
	bd := spec.color
	bd.A = 0.45
	return basic.SizedBox{
		Width:  boxSize,
		Height: boxSize,
		Child: basic.Container{
			Color:  bg,
			Radius: 5,
			Border: paint.Stroke{Width: 1, Color: bd},
			Child: basic.Align{
				Alignment: layout.AlignCenter,
				Child: basic.SizedBox{
					Width:  iconSize,
					Height: iconSize,
					Child:  control.CustomPaint{Painter: strokedEventIconPainter{path: spec.path, color: spec.color}},
				},
			},
		},
	}
}

type strokedEventIconPainter struct {
	path  *paint.Path
	color geom.Color
}

func (p strokedEventIconPainter) Paint(c *paint.Canvas, sz geom.Size) {
	if p.path == nil || sz.W <= 0 || sz.H <= 0 {
		return
	}
	scale := min(sz.W, sz.H) / 24.0
	c.Save()
	c.Scale(scale)
	c.DrawPath(p.path, paint.Paint{
		Stroke: &paint.Stroke{Width: 2.1, Color: p.color},
	})
	c.Restore()
}

// SB-1 dark sci-fi telemetry palette matching docs/mockups/SB-1.jpg.
var (
	colBgDeep      = geom.Hex(0x0b1017)
	colBarBg       = geom.Hex(0x101722)
	colPanelBg     = geom.Hex(0x131c29)
	colCardBg      = geom.Hex(0x162131)
	colBorderDim   = geom.Hex(0x233249)
	colBorderFocus = geom.Hex(0x2f81f7)
	colPillIdleBg  = geom.Hex(0x172233)
	colPillIdleBd  = geom.Hex(0x273852)
	colPillActBg   = geom.Hex(0x1f6feb)
	colPillActBd   = geom.Hex(0x58a6ff)
	colTextBright  = geom.Hex(0xe6edf3)
	colTextMuted   = geom.Hex(0x8b9bb4)
	colCyan        = geom.Hex(0x38bdf8)
	colGreen       = geom.Hex(0x4ade80)
	colAmber       = geom.Hex(0xfbbf24)
	colRed         = geom.Hex(0xf87171)
)

func sbPill(label string, active bool, onTap func(), th *theme.Theme) core.Widget {
	bg := colPillIdleBg
	bd := colPillIdleBd
	fg := colTextBright
	if active {
		bg = colPillActBg
		bd = colPillActBd
		fg = geom.Hex(0xffffff)
	}
	pill := basic.Container{
		Color:   bg,
		Padding: geom.Symmetric(9, 4),
		Radius:  5,
		Border:  paint.Stroke{Width: 1, Color: bd},
		Child: basic.Text{
			Content: label,
			Style:   th.Text.Caption.WithColor(fg),
		},
	}
	if onTap == nil {
		return pill
	}
	return interact.GestureDetector{
		OnTap: onTap,
		Child: pill,
	}
}

// sbRadioGroup wraps segments inside a single unified recessed track container
// so they read unmistakably as a mutually-exclusive segmented radio control.
func sbRadioGroup(items ...core.Widget) core.Widget {
	return basic.Container{
		Color:   geom.Hex(0x0d1520),
		Padding: geom.All(2),
		Radius:  6,
		Border:  paint.Stroke{Width: 1, Color: colPillIdleBd},
		Child: basic.Row{
			Size:     layout.MainMin,
			Cross:    layout.CrossCenter,
			Spacing:  1,
			Children: items,
		},
	}
}

func sbRadioTextItem(label string, active bool, onTap func(), th *theme.Theme) core.Widget {
	bg := geom.Color{}
	fg := colTextMuted
	if active {
		bg = colPillActBg
		fg = geom.Hex(0xffffff)
	}
	return interact.GestureDetector{
		OnTap: onTap,
		Child: basic.Container{
			Color:   bg,
			Padding: geom.Symmetric(9, 4),
			Radius:  4,
			Child: basic.Text{
				Content: label,
				Style:   th.Text.Caption.WithColor(fg),
			},
		},
	}
}

func sbRadioIconItem(icon *paint.Path, tooltip string, active bool, onTap func()) core.Widget {
	bg := geom.Color{}
	fg := colTextMuted
	if active {
		bg = colPillActBg
		fg = geom.Hex(0xffffff)
	}
	return overlay.Tooltip{
		Message: tooltip,
		Child: interact.GestureDetector{
			OnTap: onTap,
			Child: basic.Container{
				Color:   bg,
				Padding: geom.Symmetric(8, 4),
				Radius:  4,
				Child: basic.Icon{
					Path:  icon,
					Size:  14,
					Color: fg,
					Label: tooltip,
				},
			},
		},
	}
}

func (s *viewerState) buildSB1(th *theme.Theme) core.Widget {
	topBar := s.buildEmpireBar(th)
	viewport := s.buildSB1Viewport(th)

	var mainColChildren []core.Widget
	if !s.zenMode {
		mainColChildren = append(mainColChildren, s.buildControlStrip(th), s.buildLayersStrip(th))
	}
	mainColChildren = append(mainColChildren, basic.Expanded{Child: viewport})
	if !s.zenMode {
		mainColChildren = append(mainColChildren, s.buildHintBar(th))
	}
	mainCol := basic.Column{
		Cross:    layout.CrossStretch,
		Children: mainColChildren,
	}

	var bodyChildren []core.Widget
	if !s.zenMode {
		bodyChildren = append(bodyChildren, s.buildLeftSidebar(th))
	}
	bodyChildren = append(bodyChildren, basic.Expanded{Child: mainCol})

	return basic.Container{
		Color: colBgDeep,
		Child: basic.Column{
			Cross: layout.CrossStretch,
			Children: []core.Widget{
				topBar,
				basic.Expanded{
					Child: basic.Row{
						Cross:    layout.CrossStretch,
						Children: bodyChildren,
					},
				},
			},
		},
	}
}

// Row 1: Empire Telemetry Bar (Room pill, Tick, live t/s, GCL progress bar, Colonies/Rooms/Creeps, Room Jump).
func (s *viewerState) buildEmpireBar(th *theme.Theme) core.Widget {
	tps := 0.0
	if s.tickPeriod > 0 {
		tps = 1.0 / s.tickPeriod.Seconds()
	}
	tpsText := fmt.Sprintf("%.0f t/s", tps)
	tpsBg := geom.Hex(0x112b20)
	tpsBd := geom.Hex(0x226646)
	tpsFg := colGreen
	if s.localRunner != nil && s.localRunner.Paused() {
		tpsText = "PAUSED"
		tpsBg = geom.Hex(0x2b2212)
		tpsBd = geom.Hex(0x7c5c1f)
		tpsFg = colAmber
	}

	children := []core.Widget{
		basic.Container{
			Color:   geom.Hex(0x14263d),
			Padding: geom.Symmetric(8, 3),
			Radius:  5,
			Border:  paint.Stroke{Width: 1, Color: colBorderFocus},
			Child: basic.Text{
				Content: s.snap.Room,
				Style:   th.Text.Caption.WithColor(colCyan),
			},
		},
		basic.Text{
			Content: fmt.Sprintf("Tick %d", s.snap.GameTime),
			Style:   th.Text.Caption.WithColor(colTextBright),
		},
		basic.Container{
			Color:   tpsBg,
			Padding: geom.Symmetric(7, 2),
			Radius:  5,
			Border:  paint.Stroke{Width: 1, Color: tpsBd},
			Child: basic.Text{
				Content: tpsText,
				Style:   th.Text.Caption.WithColor(tpsFg),
			},
		},
	}

	if s.localRunner != nil {
		emp := s.localRunner.EmpireStats()
		ratio := float32(0)
		pct := 0
		if emp.GCLTotal > 0 {
			ratio = geom.Clamp32(float32(emp.GCLProgress)/float32(emp.GCLTotal), 0, 1)
			pct = int(ratio * 100)
		}
		const gclBarW = float32(84)
		gclBar := basic.SizedBox{
			Width:  gclBarW,
			Height: 7,
			Child: basic.Container{
				Color:  geom.Hex(0x1c293b),
				Radius: 3,
				Child: basic.Align{
					Alignment: layout.AlignCenterLeft,
					Child: basic.SizedBox{
						Width:  max(float32(3), gclBarW*ratio),
						Height: 7,
						Child:  basic.Container{Color: colCyan, Radius: 3},
					},
				},
			},
		}
		children = append(children,
			basic.Text{Content: fmt.Sprintf("GCL %d", emp.GCLLevel), Style: th.Text.Caption.WithColor(colTextBright)},
			gclBar,
			basic.Text{Content: fmt.Sprintf("%d%%", pct), Style: th.Text.Caption.WithColor(colTextMuted)},
			basic.Text{
				Content: fmt.Sprintf("•  Colonies %d   Rooms %d   Creeps %d", emp.Colonies, emp.Rooms, emp.Creeps),
				Style:   th.Text.Caption.WithColor(colTextMuted),
			},
		)
	}

	children = append(children,
		basic.Expanded{Child: basic.SizedBox{}},
		basic.SizedBox{
			Width: 74,
			Child: field.Input{
				Controller:  s.roomCtrl,
				Value:       s.initRoom,
				Label:       "Room",
				HideLabel:   true,
				Placeholder: "W9N7",
				OnSubmit:    func(val string) { s.switchRoom(val) },
			},
		},
		sbPill("Go", false, func() { s.switchRoom(s.roomCtrl.Text()) }, th),
	)
	if s.zenMode {
		children = append(children, sbPill("Exit Zen", true, func() {
			s.SetState(func() { s.zenMode = false })
		}, th))
	}

	return basic.Container{
		Color:   colBarBg,
		Padding: geom.Symmetric(10, 6),
		Border:  paint.Stroke{Width: 1, Color: colBorderDim},
		Child: basic.Row{
			Cross:    layout.CrossCenter,
			Spacing:  theme.Space8,
			Children: children,
		},
	}
}

// Row 2 (right of left sidebar): Pause, Step, segmented 1x..Max speed pills, Reset, Yaw, Zoom, Reset Cam, Zen (H).
func (s *viewerState) buildControlStrip(th *theme.Theme) core.Widget {
	var children []core.Widget

	if s.localRunner != nil {
		paused := s.localRunner.Paused()
		// Unified playback icon radio group: Pause | Play | Step
		children = append(children, sbRadioGroup(
			sbRadioIconItem(sbIconPause, "Pause (Space)", paused, func() {
				s.SetState(func() { s.localRunner.SetPaused(true) })
			}),
			sbRadioIconItem(sbIconPlay, "Play (Space)", !paused, func() {
				s.SetState(func() { s.localRunner.SetPaused(false) })
			}),
			sbRadioIconItem(sbIconStep, "Step +1 tick (.)", false, func() {
				s.localRunner.StepTick()
				s.SetState(func() { s.applySnapshot(s.provider.Snapshot()) })
				s.signal.Notify()
			}),
		))

		// Unified segmented speed radio group: 1x | 2x | 5x | 10x | 25x | Max
		var speedItems []core.Widget
		for idx, spd := range localSpeeds {
			i := idx
			sp := spd
			speedItems = append(speedItems, sbRadioTextItem(sp.label, s.speedIdx == i, func() {
				s.SetState(func() {
					s.speedIdx = i
					s.localRunner.SetTickPeriod(sp.period)
					s.tickPeriod = sp.period
				})
			}, th))
		}
		children = append(children, sbRadioGroup(speedItems...))

		// Reset icon button
		children = append(children, overlay.Tooltip{
			Message: "Reset simulation",
			Child: interact.GestureDetector{
				OnTap: func() {
					s.SetState(func() { s.startLocalSim(true) })
					s.signal.Notify()
				},
				Child: basic.Container{
					Color:   colPillIdleBg,
					Padding: geom.Symmetric(8, 5),
					Radius:  5,
					Border:  paint.Stroke{Width: 1, Color: colPillIdleBd},
					Child: basic.Icon{
						Path:  sbIconReset,
						Size:  14,
						Color: colTextBright,
						Label: "Reset simulation",
					},
				},
			},
		})
	}

	yawDeg := ((int(math.Round(float64(s.cam.YawRadians())*180/math.Pi)) % 360) + 360) % 360
	children = append(children,
		basic.Expanded{Child: basic.SizedBox{}},
		sbPill(fmt.Sprintf("Yaw %d°", yawDeg), false, func() {
			s.SetState(func() { s.cam.OrbitYaw(math.Pi / 2) })
			s.signal.Notify()
		}, th),
		sbPill(fmt.Sprintf("Zoom %.1fx", s.cam.Zoom), false, nil, th),
		sbPill("Reset Cam", false, func() {
			s.SetState(func() {
				yaw := s.cam.YawRadians()
				s.cam = iso.NewCamera()
				s.cam.SetYaw(yaw)
				s.cam.FitToSize(s.painter.LastSize)
			})
			s.signal.Notify()
		}, th),
		sbPill("Zen (H)", s.zenMode, func() {
			s.SetState(func() { s.zenMode = !s.zenMode })
		}, th),
	)

	return basic.Container{
		Color:   colBarBg,
		Padding: geom.Symmetric(10, 5),
		Border:  paint.Stroke{Width: 1, Color: colBorderDim},
		Child: basic.Row{
			Cross:    layout.CrossCenter,
			Spacing:  theme.Space8,
			Children: children,
		},
	}
}

// Row 3: Layers Bar (Visuals, Beams, HP Bars, Grid, Tower DPS).
func (s *viewerState) buildLayersStrip(th *theme.Theme) core.Widget {
	children := []core.Widget{
		basic.Text{Content: "Layers:", Style: th.Text.Caption.WithColor(colTextMuted)},
		sbPill("Visuals", s.painter.ShowVisuals, func() {
			s.SetState(func() { s.painter.ShowVisuals = !s.painter.ShowVisuals })
			s.signal.Notify()
		}, th),
		sbPill("Beams", !s.painter.HideBeams, func() {
			s.SetState(func() { s.painter.HideBeams = !s.painter.HideBeams })
			s.signal.Notify()
		}, th),
		sbPill("HP Bars", !s.painter.HideHPBars, func() {
			s.SetState(func() { s.painter.HideHPBars = !s.painter.HideHPBars })
			s.signal.Notify()
		}, th),
		sbPill("Grid", s.painter.ShowGrid, func() {
			s.SetState(func() { s.painter.ShowGrid = !s.painter.ShowGrid })
			s.signal.Notify()
		}, th),
		sbPill("Tower DPS", s.painter.ShowTowerDPS, func() {
			s.SetState(func() { s.painter.ShowTowerDPS = !s.painter.ShowTowerDPS })
			s.signal.Notify()
		}, th),
	}

	return basic.Container{
		Color:   colBgDeep,
		Padding: geom.Symmetric(10, 5),
		Border:  paint.Stroke{Width: 1, Color: colBorderDim},
		Child: basic.Row{
			Cross:    layout.CrossCenter,
			Spacing:  6,
			Children: children,
		},
	}
}

// Left Sidebar (276px): Colonies, collapsible Event Feed (Live), collapsible 5x5 Empire Minimap.
func (s *viewerState) buildLeftSidebar(th *theme.Theme) core.Widget {
	var sections []core.Widget

	// 1. Colonies section
	sections = append(sections, basic.Text{
		Content: "Colonies",
		Style:   th.Text.Body.WithWeight(600).WithColor(colTextBright),
	})
	if s.localRunner != nil {
		sums := s.localRunner.BotSummaries()
		if len(sums) == 0 {
			s.localRunner.RefreshSummaries()
			sums = s.localRunner.BotSummaries()
		}
		for idx, bs := range sums {
			sections = append(sections, s.buildSB1ColonyCard(bs, idx == 0, th))
		}
	} else {
		sections = append(sections, basic.Container{
			Color:   colCardBg,
			Padding: geom.All(8),
			Radius:  6,
			Border:  paint.Stroke{Width: 1, Color: colBorderDim},
			Child: basic.Text{
				Content: fmt.Sprintf("Remote Room %s (%d objs)", s.snap.Room, len(s.snap.Objects)),
				Style:   th.Text.Caption.WithColor(colTextMuted),
			},
		})
	}

	sections = append(sections, basic.Container{Color: colBorderDim, Height: 1})

	// 2. Event Feed (Live) — collapsible
	evArrow := "▾ "
	if s.collapseEvents {
		evArrow = "▸ "
	}
	sections = append(sections, interact.GestureDetector{
		OnTap: func() { s.SetState(func() { s.collapseEvents = !s.collapseEvents }) },
		Child: basic.Container{
			Padding: geom.Symmetric(2, 2),
			Child: basic.Text{
				Content: evArrow + "Event Feed (Live)",
				Style:   th.Text.Body.WithWeight(600).WithColor(colTextBright),
			},
		},
	})
	if !s.collapseEvents && s.localRunner != nil {
		events := s.localRunner.World().RecentEvents(6)
		var evRows []core.Widget
		for _, ev := range events {
			targetRoom := ev.Room
			evCol := colTextMuted
			var evIconWidget core.Widget
			if spec, ok := eventIconMap[ev.Kind]; ok {
				evCol = spec.color
				evIconWidget = basic.SizedBox{
					Width:  14,
					Height: 14,
					Child:  control.CustomPaint{Painter: strokedEventIconPainter{path: spec.path, color: spec.color}},
				}
			} else {
				switch ev.Kind {
				case "danger":
					evCol = colRed
				case "combat":
					evCol = colAmber
				case "raid":
					evCol = geom.Hex(0xc084fc)
				case "rcl", "spawn":
					evCol = colGreen
				case "build":
					evCol = colCyan
				}
			}
			rowChildren := make([]core.Widget, 0, 3)
			if evIconWidget != nil {
				rowChildren = append(rowChildren, evIconWidget)
			}
			rowChildren = append(rowChildren,
				basic.Expanded{
					Child: basic.Text{
						Content: fmt.Sprintf("[%d] %s", ev.Tick, ev.Message),
						Style:   th.Text.Caption.WithMaxLines(1).WithColor(evCol),
					},
				},
				basic.Text{
					Content: "#" + ev.Room,
					Style:   th.Text.Caption.WithColor(colTextMuted),
				},
			)
			evRows = append(evRows, interact.GestureDetector{
				OnTap: func() {
					if targetRoom != "" {
						s.switchRoom(targetRoom)
					}
				},
				Child: basic.Container{
					Color:   colCardBg,
					Padding: geom.Symmetric(7, 4),
					Radius:  4,
					Border:  paint.Stroke{Width: 1, Color: colBorderDim},
					Child: basic.Row{
						Cross:    layout.CrossCenter,
						Spacing:  6,
						Children: rowChildren,
					},
				},
			})
		}
		if len(evRows) == 0 {
			evRows = append(evRows, basic.Text{
				Content: "No events yet.",
				Style:   th.Text.Caption.WithColor(colTextMuted),
			})
		}
		sections = append(sections, basic.Column{
			Size:     layout.MainMin,
			Cross:    layout.CrossStretch,
			Spacing:  3,
			Children: evRows,
		})
	}

	sections = append(sections, basic.Container{Color: colBorderDim, Height: 1})

	// 3. Minimap (Empire) — collapsible graphical 5x5 sector radar centered on s.snap.Room
	mmArrow := "▾ "
	if s.collapseMinimap {
		mmArrow = "▸ "
	}
	mmLabel := s.hoverMinimapRoom
	if mmLabel == "" {
		mmLabel = s.snap.Room
	}
	sections = append(sections, interact.GestureDetector{
		OnTap: func() { s.SetState(func() { s.collapseMinimap = !s.collapseMinimap }) },
		Child: basic.Container{
			Padding: geom.Symmetric(2, 2),
			Child: basic.Row{
				Cross: layout.CrossCenter,
				Children: []core.Widget{
					basic.Expanded{
						Child: basic.Text{
							Content: mmArrow + "Minimap (Empire)",
							Style:   th.Text.Body.WithWeight(600).WithColor(colTextBright),
						},
					},
					basic.Text{
						Content: mmLabel,
						Style:   th.Text.Caption.WithColor(colCyan),
					},
				},
			},
		},
	})
	if !s.collapseMinimap {
		sections = append(sections, s.buildSB1Minimap(th))
	}

	return basic.SizedBox{
		Width: 288,
		Child: basic.Container{
			Color:   colBarBg,
			Padding: geom.All(10),
			Border:  paint.Stroke{Width: 1, Color: colBorderDim},
			Child: basic.Scroll{
				Child: basic.Column{
					Size:     layout.MainMin,
					Cross:    layout.CrossStretch,
					Spacing:  theme.Space8,
					Children: sections,
				},
			},
		},
	}
}

type botIconPainter struct {
	primary bool
}

func (p botIconPainter) Paint(c *paint.Canvas, sz geom.Size) {
	if sz.W <= 0 || sz.H <= 0 {
		return
	}
	cx := sz.W / 2
	cy := sz.H / 2
	r := min(sz.W, sz.H) * 0.44

	// Subtle radial/rounded emblem backdrop.
	bgCol := geom.RGBA(0.66, 0.33, 0.97, 0.16)
	bdCol := geom.RGBA(0.66, 0.33, 0.97, 0.45)
	topCol := geom.Hex(0xd8b4fe)
	leftCol := geom.Hex(0x9333ea)
	rightCol := geom.Hex(0x581c87)
	accentCol := geom.Hex(0xf472b6)
	if p.primary {
		bgCol = geom.RGBA(0.22, 0.74, 0.97, 0.16)
		bdCol = geom.RGBA(0.22, 0.74, 0.97, 0.45)
		topCol = geom.Hex(0x7dd3fc)
		leftCol = geom.Hex(0x2563eb)
		rightCol = geom.Hex(0x1e3a8a)
		accentCol = geom.Hex(0x38bdf8)
	}

	c.DrawRRect(geom.RRectAll(geom.Rect{W: sz.W, H: sz.H}, 6), mmFill(bgCol))
	c.DrawRRect(geom.RRectAll(geom.Rect{X: 0.5, Y: 0.5, W: sz.W - 1, H: sz.H - 1}, 6), mmStroke(1, bdCol))

	// Isometric hex-cube geometry centered at (cx, cy).
	dx := r * 0.866
	dy := r * 0.5
	top := geom.Offset{X: cx, Y: cy - r}
	topL := geom.Offset{X: cx - dx, Y: cy - dy}
	topR := geom.Offset{X: cx + dx, Y: cy - dy}
	mid := geom.Offset{X: cx, Y: cy}
	botL := geom.Offset{X: cx - dx, Y: cy + dy}
	botR := geom.Offset{X: cx + dx, Y: cy + dy}
	bot := geom.Offset{X: cx, Y: cy + r}

	triQuad := func(a, b, d, e geom.Offset) *paint.Path {
		pt := &paint.Path{}
		pt.MoveTo(a.X, a.Y)
		pt.LineTo(b.X, b.Y)
		pt.LineTo(d.X, d.Y)
		pt.LineTo(e.X, e.Y)
		pt.Close()
		return pt
	}

	// Top, left, right faces of the isometric bot emblem.
	c.DrawPath(triQuad(top, topR, mid, topL), mmFill(topCol))
	c.DrawPath(triQuad(topL, mid, bot, botL), mmFill(leftCol))
	c.DrawPath(triQuad(mid, topR, botR, bot), mmFill(rightCol))

	// Crisp specular edge + glowing core/visor accent.
	if p.primary {
		// Glowing cyan hive node in center of top face.
		c.DrawRRect(geom.RRectAll(
			geom.Rect{X: cx - 2.5, Y: cy - dy*1.25 - 1.5, W: 5, H: 3}, 1.5,
		), mmFill(geom.Hex(0xe0f2fe)))
	} else {
		// Raider twin visor eyes on left/right faces.
		c.DrawRect(geom.Rect{X: cx - dx*0.58, Y: cy + dy*0.05, W: 3.2, H: 2.2}, mmFill(accentCol))
		c.DrawRect(geom.Rect{X: cx + dx*0.26, Y: cy + dy*0.05, W: 3.2, H: 2.2}, mmFill(accentCol))
	}
}

func (s *viewerState) buildSB1ColonyCard(bs lua.BotSummary, isPrimary bool, th *theme.Theme) core.Widget {
	active := bs.HomeRoom != "" && bs.HomeRoom == s.snap.Room
	bd := colBorderDim
	if active {
		bd = colBorderFocus
	}

	totalCreeps := 0
	for _, n := range bs.RoleCounts {
		totalCreeps += n
	}

	statusCol := colGreen
	statusLine := "Economy stable"
	if bs.ThreatLevel > 0 || bs.Hostiles > 0 {
		statusCol = colRed
		statusLine = fmt.Sprintf("Threat L%d (%d hostiles)", bs.ThreatLevel, bs.Hostiles)
	} else if bs.RaidTarget != "" {
		statusCol = colAmber
		statusLine = fmt.Sprintf("Raid → %s", bs.RaidTarget)
	} else if bs.NextSpawn != "" {
		statusLine = bs.NextSpawn
	}

	botIcon := basic.SizedBox{
		Width:  30,
		Height: 30,
		Child:  control.CustomPaint{Painter: botIconPainter{primary: isPrimary}},
	}

	targetRoom := bs.HomeRoom
	card := basic.Container{
		Color:   colCardBg,
		Padding: geom.All(9),
		Radius:  7,
		Border:  paint.Stroke{Width: 1, Color: bd},
		Child: basic.Column{
			Size:    layout.MainMin,
			Cross:   layout.CrossStretch,
			Spacing: 5,
			Children: []core.Widget{
				basic.Row{
					Cross:   layout.CrossCenter,
					Spacing: 8,
					Children: []core.Widget{
						botIcon,
						basic.Expanded{
							Child: basic.Column{
								Size:    layout.MainMin,
								Cross:   layout.CrossStretch,
								Spacing: 2,
								Children: []core.Widget{
									basic.Row{
										Cross: layout.CrossCenter,
										Children: []core.Widget{
											basic.Expanded{
												Child: basic.Text{
													Content: bs.Username,
													Style:   th.Text.Body.WithWeight(600).WithColor(colTextBright),
												},
											},
											basic.Container{
												Color:   geom.Hex(0x14263d),
												Padding: geom.Symmetric(6, 2),
												Radius:  4,
												Border:  paint.Stroke{Width: 1, Color: colPillIdleBd},
												Child: basic.Text{
													Content: fmt.Sprintf("RCL %d", bs.RCL),
													Style:   th.Text.Caption.WithSize(10).WithColor(colCyan),
												},
											},
										},
									},
									basic.Text{
										Content: fmt.Sprintf("%s • %d/%d E • %d creeps", bs.HomeRoom, bs.EnergyAvail, bs.EnergyCap, totalCreeps),
										Style:   th.Text.Caption.WithSize(11).WithColor(colTextMuted),
									},
								},
							},
						},
					},
				},
				basic.Text{
					Content: "● " + statusLine,
					Style:   th.Text.Caption.WithColor(statusCol),
				},
			},
		},
	}
	if targetRoom == "" {
		return card
	}
	return interact.GestureDetector{
		OnTap: func() { s.switchRoom(targetRoom) },
		Child: card,
	}
}

type minimapPainter struct {
	state *viewerState
}

func mmFill(col geom.Color) paint.Paint {
	return paint.Paint{Color: col}
}

func mmStroke(w float32, col geom.Color) paint.Paint {
	return paint.Paint{Stroke: &paint.Stroke{Width: w, Color: col}}
}

func (p *minimapPainter) Paint(c *paint.Canvas, sz geom.Size) {
	if sz.W <= 0 || sz.H <= 0 {
		return
	}
	const gridN = 5
	cellW := sz.W / gridN
	cellH := sz.H / gridN

	// Deep tactical radar backdrop.
	c.DrawRect(geom.Rect{W: sz.W, H: sz.H}, mmFill(geom.Hex(0x09111a)))

	var w *engine.World
	var primaryOwner string
	if p.state.localRunner != nil {
		w = p.state.localRunner.World()
		if sums := p.state.localRunner.BotSummaries(); len(sums) > 0 {
			primaryOwner = sums[0].Username
		}
	}

	if w != nil {
		w.RLock()
	}

	// Render terrain + territory + entity blips for each room in the 5x5 sector.
	const subN = 10 // 10x10 downsampled terrain blocks per 50x50 room
	subW := cellW / subN
	subH := cellH / subN
	step := iso.GridSize / subN

	for dy := -2; dy <= 2; dy++ {
		for dx := -2; dx <= 2; dx++ {
			col := dx + 2
			row := dy + 2
			ox := float32(col) * cellW
			oy := float32(row) * cellH

			rName, ok := engine.AdjacentRoomName(p.state.snap.Room, dx, dy)
			if !ok {
				continue
			}

			var rm *engine.Room
			if w != nil {
				rm = w.Rooms[rName]
			}
			if rm != nil && len(rm.Terrain) >= iso.GridSize*iso.GridSize {
				// Downsample 50x50 terrain into 10x10 glowing emerald/slate radar cells.
				for sy := 0; sy < subN; sy++ {
					for sx := 0; sx < subN; sx++ {
						walls, swamps := 0, 0
						for ty := sy * step; ty < (sy+1)*step; ty++ {
							base := ty * iso.GridSize
							for tx := sx * step; tx < (sx+1)*step; tx++ {
								t := rm.Terrain[base+tx]
								if t&engine.TERRAIN_MASK_WALL != 0 {
									walls++
								} else if t&engine.TERRAIN_MASK_SWAMP != 0 {
									swamps++
								}
							}
						}
						// Low-brightness terrain palette so buildings (medium) and units (high) pop clearly.
						var tileCol geom.Color
						switch {
						case walls >= (step*step)/2:
							tileCol = geom.Hex(0x070b11) // very dark wall mass
						case swamps >= 5:
							tileCol = geom.Hex(0x132b22) // muted dark emerald swamp
						case swamps > 0:
							tileCol = geom.Hex(0x0f201b)
						default:
							tileCol = geom.Hex(0x0d1520) // dark walkable plain
						}
						c.DrawRect(geom.Rect{
							X: ox + float32(sx)*subW,
							Y: oy + float32(sy)*subH,
							W: subW + 0.4,
							H: subH + 0.4,
						}, mmFill(tileCol))
					}
				}

				// Subtle territory wash for owned rooms.
				if ctrl := rm.FindController(); ctrl != nil && ctrl.User != "" {
					ownerName := w.Users[ctrl.User]
					wash := geom.RGBA(0.66, 0.33, 0.97, 0.16) // rival purple
					if ownerName == primaryOwner {
						wash = geom.RGBA(0.22, 0.74, 0.97, 0.12) // friendly cyan
					}
					c.DrawRect(geom.Rect{X: ox, Y: oy, W: cellW, H: cellH}, mmFill(wash))
				}

				// Pass 1: Medium-brightness buildings & muted sources.
				for _, e := range rm.Entities {
					if e.Type == engine.TYPE_CREEP {
						continue
					}
					px := ox + (float32(e.X)+0.5)/iso.GridSize*cellW
					py := oy + (float32(e.Y)+0.5)/iso.GridSize*cellH
					var blipCol geom.Color
					var bsz float32 = 2.2
					switch e.Type {
					case engine.TYPE_SOURCE:
						blipCol = geom.Hex(0x9a7820) // muted gold
						bsz = 2.0
					case engine.STRUCTURE_SPAWN, engine.STRUCTURE_CONTROLLER:
						bsz = 3.2
						if e.User == "" {
							blipCol = geom.Hex(0x475569)
						} else if w.Users[e.User] == primaryOwner {
							blipCol = geom.Hex(0x2b82c9) // medium-bright blue
						} else {
							blipCol = geom.Hex(0x8e44c9) // medium-bright purple
						}
					case engine.STRUCTURE_TOWER, engine.STRUCTURE_EXTENSION, engine.STRUCTURE_STORAGE,
						engine.STRUCTURE_CONTAINER, engine.STRUCTURE_LINK, engine.STRUCTURE_EXTRACTOR:
						bsz = 2.2
						if w.Users[e.User] == primaryOwner {
							blipCol = geom.Hex(0x1f6f99)
						} else if e.User != "" {
							blipCol = geom.Hex(0x7332a8)
						} else {
							blipCol = geom.Hex(0x3b4d63)
						}
					default:
						continue
					}
					c.DrawRect(geom.Rect{X: px - bsz/2, Y: py - bsz/2, W: bsz, H: bsz}, mmFill(blipCol))
				}

				// Pass 2: High-brightness units (creeps) drawn on top so they pop immediately.
				for _, e := range rm.Entities {
					if e.Type != engine.TYPE_CREEP {
						continue
					}
					px := ox + (float32(e.X)+0.5)/iso.GridSize*cellW
					py := oy + (float32(e.Y)+0.5)/iso.GridSize*cellH
					var unitCol geom.Color
					if w.Users[e.User] == primaryOwner {
						unitCol = geom.Hex(0x7efca0) // high-brightness neon green/cyan
					} else {
						unitCol = geom.Hex(0xff6b7a) // high-brightness crimson/coral
					}
					c.DrawRect(geom.Rect{X: px - 1.5, Y: py - 1.5, W: 3.0, H: 3.0}, mmFill(unitCol))
				}
			}

			// Hover highlight cell.
			if rName != "" && rName == p.state.hoverMinimapRoom {
				c.DrawRect(geom.Rect{X: ox + 0.5, Y: oy + 0.5, W: cellW - 1, H: cellH - 1},
					mmStroke(1, geom.RGBA(0.58, 0.80, 0.99, 0.65)))
			}
		}
	}

	if w != nil {
		w.RUnlock()
	}

	// Faint tactical radar gridlines (room boundaries + subtle 2x sub-lines).
	subGrid := mmFill(geom.RGBA(0.18, 0.30, 0.44, 0.22))
	roomGrid := mmFill(geom.RGBA(0.22, 0.36, 0.52, 0.50))
	for i := 1; i < gridN*2; i++ {
		pt := subGrid
		if i%2 == 0 {
			pt = roomGrid
		}
		x := float32(i) * (cellW / 2)
		y := float32(i) * (cellH / 2)
		c.DrawRect(geom.Rect{X: x, Y: 0, W: 1, H: sz.H}, pt)
		c.DrawRect(geom.Rect{X: 0, Y: y, W: sz.W, H: 1}, pt)
	}

	// Glowing cyan viewport reticle centered on the active room (dx=0, dy=0), matching SB-1.jpg.
	cx := 2 * cellW
	cy := 2 * cellH
	c.DrawRect(geom.Rect{X: cx, Y: cy, W: cellW, H: cellH}, mmFill(geom.RGBA(0.22, 0.74, 0.97, 0.14)))
	c.DrawRect(geom.Rect{X: cx + 1, Y: cy + 1, W: cellW - 2, H: cellH - 2}, mmStroke(1.5, colCyan))

	// Bright cyan corner brackets & edge crosshair ticks on active room reticle.
	tickCol := mmFill(geom.Hex(0x7dd3fc))
	const brk = float32(7)
	const thk = float32(2)
	// Top-left bracket
	c.DrawRect(geom.Rect{X: cx, Y: cy, W: brk, H: thk}, tickCol)
	c.DrawRect(geom.Rect{X: cx, Y: cy, W: thk, H: brk}, tickCol)
	// Top-right bracket
	c.DrawRect(geom.Rect{X: cx + cellW - brk, Y: cy, W: brk, H: thk}, tickCol)
	c.DrawRect(geom.Rect{X: cx + cellW - thk, Y: cy, W: thk, H: brk}, tickCol)
	// Bottom-left bracket
	c.DrawRect(geom.Rect{X: cx, Y: cy + cellH - thk, W: brk, H: thk}, tickCol)
	c.DrawRect(geom.Rect{X: cx, Y: cy + cellH - brk, W: thk, H: brk}, tickCol)
	// Bottom-right bracket
	c.DrawRect(geom.Rect{X: cx + cellW - brk, Y: cy + cellH - thk, W: brk, H: thk}, tickCol)
	c.DrawRect(geom.Rect{X: cx + cellW - thk, Y: cy + cellH - brk, W: thk, H: brk}, tickCol)
	// 4 outer edge crosshair ticks
	c.DrawRect(geom.Rect{X: cx + cellW/2 - 1, Y: cy - 4, W: 2, H: 4}, tickCol)
	c.DrawRect(geom.Rect{X: cx + cellW/2 - 1, Y: cy + cellH, W: 2, H: 4}, tickCol)
	c.DrawRect(geom.Rect{X: cx - 4, Y: cy + cellH/2 - 1, W: 4, H: 2}, tickCol)
	c.DrawRect(geom.Rect{X: cx + cellW, Y: cy + cellH/2 - 1, W: 4, H: 2}, tickCol)
}

func (s *viewerState) buildSB1Minimap(th *theme.Theme) core.Widget {
	const mmW = float32(266)
	const mmH = float32(216)

	roomAt := func(pos geom.Offset) string {
		if pos.X < 0 || pos.Y < 0 || pos.X >= mmW || pos.Y >= mmH {
			return ""
		}
		dx := int(pos.X/(mmW/5)) - 2
		dy := int(pos.Y/(mmH/5)) - 2
		if rName, ok := engine.AdjacentRoomName(s.snap.Room, dx, dy); ok {
			return rName
		}
		return ""
	}

	return basic.Container{
		Color:  geom.Hex(0x09111a),
		Radius: 7,
		Border: paint.Stroke{Width: 1, Color: colBorderDim},
		Child: basic.SizedBox{
			Width:  mmW,
			Height: mmH,
			Child: interact.MouseRegion{
				OnHover: func(pos geom.Offset) {
					if hr := roomAt(pos); hr != s.hoverMinimapRoom {
						s.SetState(func() { s.hoverMinimapRoom = hr })
					}
				},
				OnExit: func() {
					if s.hoverMinimapRoom != "" {
						s.SetState(func() { s.hoverMinimapRoom = "" })
					}
				},
				Child: interact.Listener{
					OnPointer: func(e *core.PointerEvent) {
						if e.Kind == core.PointerDown {
							if target := roomAt(e.Pos); target != "" && target != s.snap.Room {
								s.switchRoom(target)
							}
						}
					},
					Child: control.CustomPaint{
						Painter: &minimapPainter{state: s},
						Repaint: &s.signal,
					},
				},
			},
		},
	}
}

// Viewport with edge neighbor-room pills, top-right Tower DPS legend, bottom-left Hover card, and bottom-right floating Selection card.
func (s *viewerState) buildSB1Viewport(th *theme.Theme) core.Widget {
	layers := []core.Widget{
		interact.MouseRegion{
			OnHover: func(pos geom.Offset) {
				if s.dragging || s.orbiting || s.painter.LastSize.W <= 0 {
					return
				}
				hitRoom, gx, gy, ok := s.cam.ScreenToWorldTile(pos, s.painter.LastSize, &s.snap, s.painter.Neighbors)
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
			},
			OnExit: func() {
				if s.cam.HoverX >= 0 || s.cam.HoverY >= 0 {
					s.SetState(func() {
						s.cam.HoverX = -1
						s.cam.HoverY = -1
						s.hoverRoom = ""
					})
					s.signal.Notify()
				}
			},
			Child: interact.Listener{
				OnPointer: s.handlePointer,
				Child:     control.CustomPaint{Painter: s.painter, Repaint: &s.signal},
			},
		},
	}

	// Top-left subtle room watermark
	layers = append(layers, basic.Align{
		Alignment: layout.AlignTopLeft,
		Child: basic.Container{
			Padding: geom.All(12),
			Child: basic.Text{
				Content: s.snap.Room,
				Style:   th.Text.Title.WithColor(geom.Hex(0x475a75)),
			},
		},
	})

	// Top-right floating Tower DPS gradient legend card (when Tower DPS layer is on)
	if s.painter.ShowTowerDPS {
		layers = append(layers, basic.Align{
			Alignment: layout.AlignTopRight,
			Child: basic.Container{
				Padding: geom.All(12),
				Child: basic.Container{
					Color:   geom.Hex(0x1b141a),
					Padding: geom.All(8),
					Radius:  6,
					Border:  paint.Stroke{Width: 1, Color: geom.Hex(0x8c353d)},
					Child: basic.Column{
						Size:    layout.MainMin,
						Cross:   layout.CrossStart,
						Spacing: 4,
						Children: []core.Widget{
							basic.Text{
								Content: "Tower DPS: 600 (r≤5) → 150 (r≥20)",
								Style:   th.Text.Caption.WithColor(geom.Hex(0xfca5a5)),
							},
							basic.Row{
								Size:    layout.MainMin,
								Cross:   layout.CrossCenter,
								Spacing: 2,
								Children: []core.Widget{
									basic.SizedBox{Width: 44, Height: 6, Child: basic.Container{Color: geom.Hex(0xef5350), Radius: 2}},
									basic.SizedBox{Width: 44, Height: 6, Child: basic.Container{Color: geom.Hex(0xff9800), Radius: 2}},
									basic.SizedBox{Width: 44, Height: 6, Child: basic.Container{Color: geom.Hex(0xffca28), Radius: 2}},
									basic.SizedBox{Width: 44, Height: 6, Child: basic.Container{Color: geom.Hex(0x66bb6a), Radius: 2}},
								},
							},
							basic.Text{
								Content: "600       450       300       150",
								Style:   th.Text.Caption.WithColor(colTextMuted),
							},
						},
					},
				},
			},
		})
	}

	// 4 Directional Edge Room Navigation Pills aligned to the true isometric room edges (rotates with camera yaw).
	if !s.zenMode {
		cosY, sinY := s.cam.YawCosSin()
		compassArrow := func(sx, sy float32) string {
			ang := math.Atan2(float64(sy), float64(sx)) * 180 / math.Pi
			if ang < 0 {
				ang += 360
			}
			switch {
			case ang < 22.5 || ang >= 337.5:
				return "→"
			case ang < 67.5:
				return "↘"
			case ang < 112.5:
				return "↓"
			case ang < 157.5:
				return "↙"
			case ang < 202.5:
				return "←"
			case ang < 247.5:
				return "↖"
			case ang < 292.5:
				return "↑"
			default:
				return "↗"
			}
		}
		sz := s.painter.LastSize
		for _, dir := range [][2]int{{0, -1}, {1, 0}, {0, 1}, {-1, 0}} {
			dx, dy := dir[0], dir[1]
			adj, ok := engine.AdjacentRoomName(s.snap.Room, dx, dy)
			if !ok {
				continue
			}
			// Project unit neighbor direction through camera yaw + isometric basis for compass arrow.
			rx := float32(dx)*cosY - float32(dy)*sinY
			ry := float32(dx)*sinY + float32(dy)*cosY
			sx := (rx - ry) * (iso.BaseW / 2)
			sy := (rx + ry) * (iso.BaseH / 2)

			var ax, ay float32
			if sz.W > 0 && sz.H > 0 {
				// Anchor directly on the corresponding cyan room border midpoint (just outside the edge).
				edgeX := float32(24.5) + float32(dx)*25.5
				edgeY := float32(24.5) + float32(dy)*25.5
				pt := s.cam.TileToScreen(edgeX, edgeY, sz)
				ax = geom.Clamp32(pt.X/sz.W, 0.03, 0.97)
				ay = geom.Clamp32(pt.Y/sz.H, 0.04, 0.96)
			} else {
				mag := float32(math.Hypot(float64(sx), float64(sy)))
				if mag <= 0 {
					continue
				}
				ax = geom.Clamp32(0.5+(sx/mag)*0.36, 0.04, 0.96)
				ay = geom.Clamp32(0.5+(sy/mag)*0.36, 0.04, 0.96)
			}
			arr := compassArrow(sx, sy)
			label := arr + " " + adj
			if sx > 0 {
				label = adj + " " + arr
			}
			target := adj
			layers = append(layers, basic.Align{
				Alignment: layout.Alignment{X: ax, Y: ay},
				Child: basic.Container{
					Padding: geom.All(4),
					Child:   sbPill(label, false, func() { s.switchRoom(target) }, th),
				},
			})
		}
	}

	// Bottom-left Hover Quick-Inspect Card (UC-3.1)
	if hoverCard := s.buildHoverCard(th); hoverCard != nil {
		layers = append(layers, basic.Align{
			Alignment: layout.AlignBottomLeft,
			Child: basic.Container{
				Padding: geom.All(12),
				Child:   hoverCard,
			},
		})
	}

	// Bottom-right Floating Dismissible Selection / Location Detail Card (SB-1 + SB-2)
	if selCard := s.buildFloatingSelectionCard(th); selCard != nil {
		layers = append(layers, basic.Align{
			Alignment: layout.AlignBottomRight,
			Child: basic.Container{
				Padding: geom.All(12),
				Child:   selCard,
			},
		})
	}

	return basic.Stack{Children: layers}
}

// Floating Bottom-Right Location & Entity Detail Card (dismissible via ✕).
func (s *viewerState) buildFloatingSelectionCard(th *theme.Theme) core.Widget {
	if s.cam.SelectedX < 0 || s.cam.SelectedY < 0 {
		return nil
	}

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

	rows := []core.Widget{
		basic.Row{
			Cross: layout.CrossCenter,
			Children: []core.Widget{
				basic.Expanded{
					Child: basic.Text{
						Content: fmt.Sprintf("Location %s (%d, %d)", s.snap.Room, s.cam.SelectedX, s.cam.SelectedY),
						Style:   th.Text.Body.WithColor(colTextBright),
					},
				},
				sbPill("✕", false, func() {
					s.SetState(func() {
						s.cam.SelectedX = -1
						s.cam.SelectedY = -1
						s.cam.FollowID = ""
						s.followCam = false
					})
					s.signal.Notify()
				}, th),
			},
		},
		basic.Text{
			Content: fmt.Sprintf("Terrain: %s • Move Cost: %d", terrainName, moveCost),
			Style:   th.Text.Caption.WithColor(colTextMuted),
		},
	}

	if len(tileObjs) > 0 {
		if s.selectedStackIdx < 0 || s.selectedStackIdx >= len(tileObjs) {
			s.selectedStackIdx = 0
		}
		if len(tileObjs) > 1 {
			var tabs []core.Widget
			for idx, obj := range tileObjs {
				i := idx
				lbl := obj.Type
				if obj.Name != "" {
					lbl = obj.Name
				}
				activeTab := i == s.selectedStackIdx
				bg := colPillIdleBg
				bd := colPillIdleBd
				fg := colTextBright
				if activeTab {
					bg = colPillActBg
					bd = colPillActBd
					fg = geom.Hex(0xffffff)
				}
				tabChildren := make([]core.Widget, 0, 2)
				if ico := entityIconWidget(obj, 13); ico != nil {
					tabChildren = append(tabChildren, ico)
				}
				tabChildren = append(tabChildren, basic.Text{
					Content: lbl,
					Style:   th.Text.Caption.WithColor(fg),
				})
				tabs = append(tabs, interact.GestureDetector{
					OnTap: func() {
						s.SetState(func() { s.selectedStackIdx = i })
					},
					Child: basic.Container{
						Color:   bg,
						Padding: geom.Symmetric(8, 4),
						Radius:  5,
						Border:  paint.Stroke{Width: 1, Color: bd},
						Child: basic.Row{
							Size:     layout.MainMin,
							Cross:    layout.CrossCenter,
							Spacing:  5,
							Children: tabChildren,
						},
					},
				})
			}
			rows = append(rows, basic.Wrap{
				Spacing:    4,
				RunSpacing: 4,
				Children:   tabs,
			})
		}

		selObj := tileObjs[s.selectedStackIdx]
		var followToggle core.Widget
		if selObj.Type == "creep" || selObj.Type == "powerCreep" {
			following := s.followCam && s.cam.FollowID == selObj.ID
			tip := "Follow creep"
			bg := colPillIdleBg
			bd := colPillIdleBd
			fg := colTextMuted
			if following {
				tip = "Following creep (click to unfollow)"
				bg = colPillActBg
				bd = colPillActBd
				fg = geom.Hex(0xffffff)
			}
			targetID := selObj.ID
			followToggle = overlay.Tooltip{
				Message: tip,
				Child: interact.GestureDetector{
					OnTap: func() {
						s.SetState(func() {
							if s.followCam && s.cam.FollowID == targetID {
								s.followCam = false
							} else {
								s.cam.FollowID = targetID
								s.followCam = true
							}
						})
					},
					Child: basic.SizedBox{
						Width:  26,
						Height: 26,
						Child: basic.Container{
							Color:  bg,
							Radius: 5,
							Border: paint.Stroke{Width: 1, Color: bd},
							Child: basic.Align{
								Alignment: layout.AlignCenter,
								Child: basic.SizedBox{
									Width:  16,
									Height: 16,
									Child:  control.CustomPaint{Painter: strokedEventIconPainter{path: sbIconEye, color: fg}},
								},
							},
						},
					},
				},
			}
		}
		var creepMem string
		if s.localRunner != nil && (selObj.Type == "creep" || selObj.Type == "powerCreep") {
			creepMem = s.localRunner.CreepMemoryJSON(selObj.User, selObj.Name)
		}
		rows = append(rows, buildObjectCard(selObj, s.snap.Users, s.snap.GameTime, creepMem, followToggle, th))
	} else {
		rows = append(rows, basic.Text{
			Content: "Empty tile.",
			Style:   th.Text.Caption.WithColor(colTextMuted),
		})
	}

	return basic.SizedBox{
		Width: 312,
		Child: basic.Container{
			Color:   colPanelBg,
			Padding: geom.All(10),
			Radius:  8,
			Border:  paint.Stroke{Width: 1, Color: colBorderFocus},
			Child: basic.Column{
				Size:     layout.MainMin,
				Cross:    layout.CrossStretch,
				Spacing:  6,
				Children: rows,
			},
		},
	}
}

// Bottom Hotkey Hint Strip matching SB-1.jpg.
func (s *viewerState) buildHintBar(th *theme.Theme) core.Widget {
	return basic.Container{
		Color:   colBarBg,
		Padding: geom.Symmetric(10, 4),
		Border:  paint.Stroke{Width: 1, Color: colBorderDim},
		Child: basic.Row{
			Cross:   layout.CrossCenter,
			Spacing: theme.Space12,
			Children: []core.Widget{
				basic.Text{
					Content: "Drag: Pan   •   Wheel: Zoom   •   Right-Drag: Orbit   •   Click: Inspect   •   Zen: H",
					Style:   th.Text.Caption.WithColor(colTextMuted),
				},
				basic.Expanded{Child: basic.SizedBox{}},
				basic.Text{
					Content: strings.TrimSpace(s.snap.Status),
					Style:   th.Text.Caption.WithColor(colTextMuted),
				},
			},
		},
	}
}
