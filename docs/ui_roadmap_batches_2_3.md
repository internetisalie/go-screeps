# Go-Screeps Desktop UI Roadmap — Batches 2 & 3

This document captures the implementation plan for the remaining four storyboard screens in [docs/mockups/](mockups/), building directly on the completed **Batch 1 (SB-1 + SB-2)** foundation (`ui_sb1.go`, `assets/icons/{events,entities}/`, `internal/iso`, `internal/lua`).

---

## Completed Baseline (Batch 1 — SB-1 + SB-2)

- **SB-1 ([SB-1.jpg](mockups/SB-1.jpg))**: Two-row Empire Telemetry & Playback/Camera control chrome (unified icon/speed radio groups, GCL progress bar, Room Jump), styled Layers bar + floating Tower DPS legend, 288px left sidebar (Colony cards with isometric bot emblems, live Event Feed with embedded SVG icons, graphical 5×5 Empire radar minimap with low/medium/high terrain/building/unit brightness hierarchy), 4 yaw-tracked room-edge jump pills, bottom hotkey hint bar, and Zen mode.
- **SB-2 ([SB-2.jpg](mockups/SB-2.jpg))**: `MouseRegion`-powered Hover Quick-Inspect Card (bottom-left), dismissible floating Location & Entity Detail Card (bottom-right) with stacked-entity icon tabs, all 30 embedded entity SVG icons, Owner pill, balanced `26×26` eyeball Follow toggle + auto-follow on click, split left/right metric bars, lettered `T/W/C/M/A/R/H/K` body-part tiles + inline TTL, target-icon `Action:` line, and database-icon `Memory.creeps[...]` code box.
- **Self-Verification**: Live headless capture pipeline (`xvfb-run` + Lavapipe Vulkan ICD + `-multi` + `scrot`) for pixel-level visual iteration.

---

## Shared Navigation Chrome (Introduced at Start of Batch 2)

Add a compact segmented **View Switcher** in the top Empire Telemetry Bar (matching the radio-group styling from SB-1) plus a `?` shortcuts icon button:

| Mode | View | Shortcut | Storyboard |
| :--- | :--- | :--- | :--- |
| `Tactical` | 3D Isometric Viewport + HUD (default) | `F1` | SB-1 + SB-2 |
| `Colonies` | Colony Dashboard, 5×5 Sector & Event Feed | `F2` | SB-3 |
| `Workbench` | Bot Dev Workbench (Profiler, Console/REPL, Memory Tree) | `F3` | SB-4 |
| `Session` | Remote Server, Snapshots & Bot Slot Manager | `F4` | SB-5 |
| `?` Overlay | Modal Keyboard Shortcuts Cheat Sheet | `?` / `Esc` | SB-6 |

---

## Batch 2 — Empire Intelligence & Keyboard Ergonomics (SB-3 + SB-6)

### 1. SB-3: Colony Dashboard & Filtered Event Feed ([SB-3.jpg](mockups/SB-3.jpg))

**Target file**: `ui_sb3.go` (plus small telemetry helpers in `internal/lua/telemetry.go` and `main.go`).

#### Layout (3-Column Full-Window View below Top Empire Bar)
1. **Left Rail (`240px`) — Room Directory & Bookmarks**:
   - **Jump to Room** search box (`W9N7`, Enter to jump & switch to Tactical view).
   - **Bookmarked Rooms** (persisted in `viewerState` / `ViewerState` save file; star toggle on any room row to pin/unpin).
   - **My Rooms** (owned rooms with RCL pill and active-room indicator).
   - **Scouted / Nearby Rooms** (generated/scouted rooms with Neutral / Rival / Remote status).
2. **Center Column (Flexible) — Colony Dashboard + 5×5 Sector Minimap**:
   - **Expandable Colony Cards** (primary colony expanded by default, others collapsible):
     - Header: Bot emblem icon, `Room — Username`, tag pill (`Home` / `Threat` / `Remote`), `RCL N` badge.
     - Economy row: Energy progress bar (`1,180 / 1,300` + `%`), Spawn HP (`3,000 / 3,000 HP`).
     - **Roles Grid (Actual / Desired)**: 2-column compact table (`harvester 2/2`, `hauler 2/2`, `upgrader 2/2`, `builder 1/1`, `defender 2/2`, `dismantler 1/1`, `healer 1/1`, etc.) sourced directly from `BotSummary.RoleCounts` & `BotSummary.DesiredRoles`.
     - Right status column: `Next Spawn` (with ETA/saving progress), `Threat` level badge (`Low` / `Medium` / `High`), and `Targets` (e.g., `W9N8 (raid)`).
   - **5×5 Sector Minimap + Legend**:
     - Interactive 5×5 room sector panel (reusing `minimapPainter` / sector data) paired with a crisp color/icon Legend (`Owned`, `Active`, `Neutral`, `Remote`, `Expansion`, `Combat/Raid`, `Bookmarked`).
3. **Right Column (`360px`) — Filtered Colony Event Feed + Detail Preview**:
   - **Category Filter Pills** (radio/filter strip): `All | Combat | Raid | RCL | Build | Danger | Spawn` using the embedded SVG icons from `assets/icons/events/`.
   - **Scrollable Event List**: `[tick]`, `#ROOM`, embedded event SVG icon, message, and clickable `Jump →` button (switches room + returns to Tactical view). Clicking a row selects it into the preview card below.
   - **Selected Event Detail Card** (bottom-right): Event icon + title, tick badge, Room / Type / Actor metadata, full description, and `Center Camera` / `Open in Viewport` action buttons.

#### Backend / State Additions
- Add `BookmarkedRooms []string` to `lua.ViewerState` (persisted in `world_state.json`).
- Add `SpawnHP`, `SpawnHitsMax` to `lua.BotSummary` in `internal/lua/telemetry.go`.
- Add `Actor string`, `X, Y int` (optional coordinates) to `engine.WorldEvent` in `internal/engine/world.go` so `Center Camera` can snap directly to the event's tile.

---

### 2. SB-6: Keyboard Shortcuts Overlay & Global Hotkeys ([SB-6.jpg](mockups/SB-6.jpg))

**Target file**: `ui_sb6.go` (plus keyboard handler wiring in `main.go`).

#### Global Hotkey Bindings (disabled automatically when a text `field.Input` has focus)
| Category | Key | Action |
| :--- | :--- | :--- |
| **Playback** | `Space` | Pause / Play simulation |
| | `.` | Step +1 tick |
| | `1`–`6` | Set speed (`1x`, `2x`, `5x`, `10x`, `25x`, `Max`) |
| | `Shift+R` | Reset local simulation |
| **Camera** | `W`/`A`/`S`/`D` or Arrows | Pan viewport |
| | `Q` / `E` | Orbit yaw `-90°` / `+90°` |
| | `0` | Reset camera fit |
| | `G` | Focus Room Jump input |
| **Selection** | `Tab` | Cycle stacked entities on selected tile |
| | `F` | Toggle Follow on selected creep |
| | `Esc` | Close modal overlay (`?`) or clear tile selection |
| **Panels & Views** | `H` | Toggle Zen mode |
| | `F1`–`F4` | Switch view (`Tactical`, `Colonies`, `Workbench`, `Session`) |
| | `?` | Toggle Keyboard Shortcuts Overlay |
| **Layers** | `V` | Toggle Visuals |
| | `A` | Toggle Action Beams |
| | `B` | Toggle HP Bars |
| | `G` (or `Shift+G`) | Toggle Grid |
| | `D` | Toggle Tower DPS heatmap |

#### Overlay Modal (`ui_sb6.go`)
- Translucent dark backdrop over the current view, centered floating panel (`max ~980×620px`) with `Esc to close` pill + `×` button.
- 6 structured category cards (`PLAYBACK`, `CAMERA`, `SELECTION`, `PANELS & UI`, `OVERLAYS (LAYERS)`, `SESSION & BOTS`) with styled keycap badges (`Space`, `1`..`6`, `W A S D`, etc.).
- Bottom strip: 3 **Focus Flows** (`Inspect an event`, `Debug a creep`, `Analyze tower pressure`) + **Legend** card.

#### Batch 2 Verification
- `go vet ./...`, `make test`, `make build`.
- Xvfb screenshots for **SB-3** (Colony Dashboard + filtered Event Feed) and **SB-6** (`?` shortcuts modal) verified visually against [SB-3.jpg](mockups/SB-3.jpg) and [SB-6.jpg](mockups/SB-6.jpg).

---

## Batch 3 — Bot Dev Workbench & Session Management (SB-4 + SB-5)

### 3. SB-4: Bot Dev Workbench ([SB-4.jpg](mockups/SB-4.jpg))

**Target files**: `ui_sb4.go`, `internal/lua/workbench.go` (metrics ring buffer, REPL eval, hot-reload, structured memory tree).

#### Layout (3-Column Developer Workspace)
1. **Top Sandbox Error Banner** (conditional, dismissible):
   - Appears whenever a bot hits a Lua runtime error or CPU limit exceeded (`Sandbox Error - Hivemind (t2415): combat.lua:412: CPU limit exceeded`), with `Copy Trace` and `×` buttons.
2. **Left Rail (`240px`) — Bots, Script Tree & Hot-Reload**:
   - **Bots selector**: Clickable cards showing each bot's status dot, live CPU ms (`% of limit`), and Lua VM memory.
   - **Scripts file tree**: Lists loaded `.lua` modules for the selected bot (read-only preview or click to highlight).
   - **Hot-Reload card**: Shows last reload tick & changed files + `Reload Scripts (<Bot>)` button (`Ctrl+R`) that re-reads bot scripts from disk (or embedded FS) without resetting world state.
3. **Center Column — Profiler Sparklines + Console & Lua REPL**:
   - **CPU & Memory Profiler**:
     - Summary stats (`avg`, `peak`, `min` CPU ms; `avg`, `peak` Lua VM MB) + window switcher (`60 ticks | 300 | 1,000`).
     - Custom `control.CustomPaint` area sparklines for **CPU (ms)** (with red limit spike markers) and **Memory (MB)**.
   - **Console & Lua REPL**:
     - Filter pills (`All | Info | Warn | Error`), live log search input, and `Clear` button.
     - Color-coded scrollable log stream (`t2415 [ERROR] ... #W9N7`).
     - Bottom interactive **Lua REPL input** (`> require("screeps").Game.rooms["W9N7"]:find(105)` + `Run` button) executing sandboxed expressions inside the selected bot's `BotVM` and appending the formatted result to the console.
4. **Right Column (`320px`) — Interactive Memory Tree**:
   - Search filter (`Search Memory... e.g. rooms.W9N7`).
   - Expandable/collapsible tree view of the selected bot's live `Memory` table (`rooms`, `creeps`, `spawns`, `stats`, etc.) with type glyphs (`{}`, `#`, `""`, folder counts) and click-to-copy JSON node.

#### Backend Additions (`internal/lua/`)
- Rolling ring buffer (`1000` ticks) per `BotVM` recording `Tick`, `CPUMs`, `MemBytes`, `LastError`, `LastErrorTick`.
- Structured `LogEntry{Tick, Level, Room, Bot, Message}` ring buffer replacing flat `[]string`.
- `LocalRunner.EvalREPL(botName, expr string) (string, error)` (executes under `stepMu` with strict CPU cap).
- `LocalRunner.ReloadBot(botName string) ([]string, error)` (reloads Lua modules in-place while preserving `Memory`).
- `LocalRunner.ExportMemoryTree(botName string) (map[string]any, error)` (depth/budget-bounded snapshot for the tree viewer).

---

### 4. SB-5: Session & Bots Manager ([SB-5.jpg](mockups/SB-5.jpg))

**Target files**: `ui_sb5.go`, `internal/lua/snapshots.go`.

#### Layout (3-Column Control Center + Top Mode Sub-Tabs)
1. **Header Strip**: `Session & Bots` title, active mode pill (`Local Simulation` vs `Remote Server`), and current session/autosave label (`autosave (t2400)`).
2. **Column 1 — Connect to Remote Screeps Server (`UC-1.7`)**:
   - Live connection status badge (`Connected` / `Local Only` / `Disconnected`) + banner (`Latency`, `API`, `Player`).
   - Inputs: `Server URL`, `Email / Token`, `Initial Room`.
   - Collapsible `Connection Details` table (`Status`, `Latency`, `User`, `Rooms owned`, `Last Sync`).
   - Actions: `Reconnect` and `Switch to Local Sim` / `Connect Remote` buttons.
3. **Column 2 — Simulation Snapshots (`UC-1.6`)**:
   - Quick action buttons: `Quicksave (F5)`, `Quickload (F9)`, `Save As...`, `Load...`.
   - Scrollable snapshot table (`Name`, `Tick`, `Size`, `Date`) scanning the user config snapshots directory (`~/.config/go-screeps/snapshots/*.json`).
   - Selected snapshot preview card (`Rooms`, `Creeps`, `GCL`, `Size`, `Created`) + `Load Selected` and `Delete` buttons.
4. **Column 3 — Bot Slot Manager (`UC-6.4`)**:
   - Action bar: `+ Add Bot...`, `Swap Bots...`, `Remove Bot`.
   - Active slots table (`Room`, `Bot / Owner`, `Type`, `Status`) with row selection.
   - **Bot Templates** picker (`Starter`, `Raider`, `From Directory...`) showing roles/goal summary + `Assign "<Template>" to Selected Room (<Room>)` button.

#### Backend Additions (`internal/lua/`)
- Named snapshot save/load/list/delete helpers in `internal/lua/snapshots.go` (reusing the existing `SaveState` JSON format with metadata header for fast listing without full deserialization).
- `LocalRunner.RemoveBot(username string)` and `LocalRunner.SwapBotScript(username, templateOrDir string)` under `stepMu`.
- Live runtime switching between `LocalRunner` and `screeps.Client` in `viewerState` (`main.go`).

#### Batch 3 Verification
- Unit tests in `internal/lua/` for REPL sandboxing/CPU cap, hot-reload preserving `Memory`, snapshot save/list/load/delete, and bot slot add/swap/remove (`go test -race -count=1 ./...`).
- Xvfb screenshots for **SB-4** (Bot Dev Workbench) and **SB-5** (Session & Bots) verified visually against [SB-4.jpg](mockups/SB-4.jpg) and [SB-5.jpg](mockups/SB-5.jpg).
