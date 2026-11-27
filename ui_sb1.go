package main

import (
	"fmt"
	"math"
	"sort"
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
)

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
		pauseLbl := "|| Pause"
		pauseActive := !s.localRunner.Paused()
		if s.localRunner.Paused() {
			pauseLbl = "▶ Play"
		}
		children = append(children,
			sbPill(pauseLbl, pauseActive, func() {
				s.SetState(func() { s.localRunner.SetPaused(!s.localRunner.Paused()) })
			}, th),
			sbPill("▶| Step", false, func() {
				s.localRunner.StepTick()
				s.SetState(func() { s.applySnapshot(s.provider.Snapshot()) })
				s.signal.Notify()
			}, th),
		)

		// Segmented speed pills (1x, 2x, 5x, 10x, 25x, Max)
		var speedPills []core.Widget
		for idx, spd := range localSpeeds {
			i := idx
			sp := spd
			speedPills = append(speedPills, sbPill(sp.label, s.speedIdx == i, func() {
				s.SetState(func() {
					s.speedIdx = i
					s.localRunner.SetTickPeriod(sp.period)
					s.tickPeriod = sp.period
				})
			}, th))
		}
		children = append(children, basic.Row{
			Size:     layout.MainMin,
			Cross:    layout.CrossCenter,
			Spacing:  2,
			Children: speedPills,
		})

		children = append(children, sbPill("↺ Reset", false, func() {
			s.SetState(func() { s.startLocalSim(true) })
			s.signal.Notify()
		}, th))
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
		for _, bs := range sums {
			sections = append(sections, s.buildSB1ColonyCard(bs, th))
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
						Cross:   layout.CrossCenter,
						Spacing: 4,
						Children: []core.Widget{
							basic.Expanded{
								Child: basic.Text{
									Content: fmt.Sprintf("[%d] %s", ev.Tick, ev.Message),
									Style:   th.Text.Caption.WithColor(evCol),
								},
							},
							basic.Text{
								Content: "#" + ev.Room,
								Style:   th.Text.Caption.WithColor(colTextMuted),
							},
						},
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

	// 3. Minimap (Empire) — collapsible 5x5 sector grid centered on s.snap.Room
	mmArrow := "▾ "
	if s.collapseMinimap {
		mmArrow = "▸ "
	}
	sections = append(sections, interact.GestureDetector{
		OnTap: func() { s.SetState(func() { s.collapseMinimap = !s.collapseMinimap }) },
		Child: basic.Container{
			Padding: geom.Symmetric(2, 2),
			Child: basic.Text{
				Content: mmArrow + "Minimap (Empire)",
				Style:   th.Text.Body.WithWeight(600).WithColor(colTextBright),
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

func (s *viewerState) buildSB1ColonyCard(bs lua.BotSummary, th *theme.Theme) core.Widget {
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

	targetRoom := bs.HomeRoom
	card := basic.Container{
		Color:   colCardBg,
		Padding: geom.All(9),
		Radius:  7,
		Border:  paint.Stroke{Width: 1, Color: bd},
		Child: basic.Column{
			Size:    layout.MainMin,
			Cross:   layout.CrossStretch,
			Spacing: 4,
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
					Content: fmt.Sprintf("%s  •  %d/%d E  •  %d creeps", bs.HomeRoom, bs.EnergyAvail, bs.EnergyCap, totalCreeps),
					Style:   th.Text.Caption.WithColor(colTextMuted),
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

func (s *viewerState) buildSB1Minimap(th *theme.Theme) core.Widget {
	// Map room -> owner username from LocalRunner world (when local).
	owners := make(map[string]string)
	var primaryOwner string
	if s.localRunner != nil {
		summaries := s.localRunner.BotSummaries()
		if len(summaries) > 0 {
			primaryOwner = summaries[0].Username
		}
		w := s.localRunner.World()
		w.RLock()
		for rName, rm := range w.Rooms {
			if ctrl := rm.FindController(); ctrl != nil && ctrl.User != "" {
				owners[rName] = w.Users[ctrl.User]
			}
		}
		w.RUnlock()
	}

	var rows []core.Widget
	for dy := -2; dy <= 2; dy++ {
		var cells []core.Widget
		for dx := -2; dx <= 2; dx++ {
			rName, ok := engine.AdjacentRoomName(s.snap.Room, dx, dy)
			if !ok {
				rName = ""
			}
			bg := geom.Hex(0x131d2b)
			bd := colBorderDim
			bdW := float32(1)
			fg := colTextMuted
			if owner := owners[rName]; owner != "" {
				fg = colTextBright
				if owner == primaryOwner {
					bg = geom.Hex(0x16385c)
					bd = colBorderFocus
				} else {
					bg = geom.Hex(0x3a1d42)
					bd = geom.Hex(0xa855f7)
				}
			}
			if dx == 0 && dy == 0 {
				bd = colCyan
				bdW = 2
				fg = colCyan
			}
			target := rName
			cell := basic.Expanded{
				Child: interact.GestureDetector{
					OnTap: func() {
						if target != "" {
							s.switchRoom(target)
						}
					},
					Child: basic.Container{
						Color:   bg,
						Padding: geom.Symmetric(1, 6),
						Radius:  4,
						Border:  paint.Stroke{Width: bdW, Color: bd},
						Child: basic.Align{
							Alignment: layout.AlignCenter,
							Child: basic.Text{
								Content: rName,
								Style:   th.Text.Caption.WithSize(9.5).WithMaxLines(1).WithColor(fg),
							},
						},
					},
				},
			}
			cells = append(cells, cell)
		}
		rows = append(rows, basic.Row{
			Cross:    layout.CrossCenter,
			Spacing:  3,
			Children: cells,
		})
	}
	return basic.Container{
		Color:   colPanelBg,
		Padding: geom.All(6),
		Radius:  7,
		Border:  paint.Stroke{Width: 1, Color: colBorderDim},
		Child: basic.Column{
			Size:     layout.MainMin,
			Cross:    layout.CrossStretch,
			Spacing:  3,
			Children: rows,
		},
	}
}

// Viewport with edge neighbor-room pills, top-right Tower DPS legend, bottom-left Hover card, and bottom-right floating Selection card.
func (s *viewerState) buildSB1Viewport(th *theme.Theme) core.Widget {
	layers := []core.Widget{
		interact.Listener{
			OnPointer: s.handlePointer,
			Child:     control.CustomPaint{Painter: s.painter, Repaint: &s.signal},
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

	// 4 Directional Edge Room Navigation Pills (when not in Zen mode)
	if !s.zenMode {
		addEdgePill := func(dx, dy int, prefix, suffix string, align layout.Alignment) {
			if adj, ok := engine.AdjacentRoomName(s.snap.Room, dx, dy); ok {
				target := adj
				layers = append(layers, basic.Align{
					Alignment: align,
					Child: basic.Container{
						Padding: geom.All(8),
						Child:   sbPill(prefix+adj+suffix, false, func() { s.switchRoom(target) }, th),
					},
				})
			}
		}
		addEdgePill(0, -1, "▲ ", "", layout.AlignTopCenter)
		addEdgePill(0, 1, "▼ ", "", layout.AlignBottomCenter)
		addEdgePill(-1, 0, "◀ ", "", layout.AlignCenterLeft)
		addEdgePill(1, 0, "", " ▶", layout.AlignCenterRight)
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
				tabs = append(tabs, sbPill(lbl, i == s.selectedStackIdx, func() {
					s.SetState(func() { s.selectedStackIdx = i })
				}, th))
			}
			rows = append(rows, basic.Wrap{
				Spacing:    4,
				RunSpacing: 4,
				Children:   tabs,
			})
		}

		selObj := tileObjs[s.selectedStackIdx]
		if selObj.Type == "creep" || selObj.Type == "powerCreep" {
			following := s.followCam && s.cam.FollowID == selObj.ID
			followLbl := "◎ Follow"
			if following {
				followLbl = "● Following"
			}
			targetID := selObj.ID
			rows = append(rows, sbPill(followLbl, following, func() {
				s.SetState(func() {
					if s.followCam && s.cam.FollowID == targetID {
						s.followCam = false
					} else {
						s.cam.FollowID = targetID
						s.followCam = true
					}
				})
			}, th))
		}
		var creepMem string
		if s.localRunner != nil && (selObj.Type == "creep" || selObj.Type == "powerCreep") {
			creepMem = s.localRunner.CreepMemoryJSON(selObj.User, selObj.Name)
		}
		rows = append(rows, buildObjectCard(selObj, s.snap.Users, s.snap.GameTime, creepMem, th))
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
