package lua

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"

	lua "github.com/yuin/gopher-lua"

	"go-screeps/internal/engine"
)

const maxPersistDepth = 64

// ViewerState captures the interactive viewer's camera and execution settings
// so restarts resume at the exact same room, angle, zoom, speed, and pause state.
type ViewerState struct {
	Room        string  `json:"room,omitempty"`
	PanX        float32 `json:"panX"`
	PanY        float32 `json:"panY"`
	Zoom        float32 `json:"zoom"`
	Yaw         float32 `json:"yaw"`
	SpeedIdx    int     `json:"speedIdx"`
	Paused      bool    `json:"paused"`
	ShowVisuals bool    `json:"showVisuals"`
}

// SaveFile is the complete persisted snapshot of the simulation World, bot Lua Memory tables,
// and optional viewer execution/camera state.
type SaveFile struct {
	World    engine.WorldState         `json:"world"`
	Memories map[string]map[string]any `json:"memories"`
	Viewer   *ViewerState              `json:"viewer,omitempty"`
}

// luaValueToGo converts a Lua value to a JSON-safe Go value.
// Unsupported types (functions, userdata, threads, channels, NaN/Inf) return (nil, false).
func luaValueToGo(v lua.LValue, visited map[*lua.LTable]bool, depth int) (any, bool) {
	if v == nil || depth > maxPersistDepth {
		return nil, false
	}
	switch val := v.(type) {
	case *lua.LNilType:
		return nil, false
	case lua.LBool:
		return bool(val), true
	case lua.LNumber:
		f := float64(val)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, false
		}
		return f, true
	case lua.LString:
		return string(val), true
	case *lua.LTable:
		if visited[val] {
			return nil, false
		}
		return luaTableToGo(val, visited, depth+1), true
	default:
		// Functions, userdata, threads, channels are skipped cleanly.
		return nil, false
	}
}

// luaTableToGo converts a Lua table to either []any (if it is a contiguous 1..N array)
// or map[string]any (for associative tables or empty tables).
func luaTableToGo(tbl *lua.LTable, visited map[*lua.LTable]bool, depth int) any {
	if tbl == nil || depth > maxPersistDepth {
		return map[string]any{}
	}
	visited[tbl] = true
	defer delete(visited, tbl)

	type kv struct {
		key lua.LValue
		val any
	}
	var entries []kv
	allIntKeys := true
	maxIdx := 0

	tbl.ForEach(func(k, v lua.LValue) {
		gv, ok := luaValueToGo(v, visited, depth)
		if !ok {
			return
		}
		if num, isNum := k.(lua.LNumber); isNum {
			f := float64(num)
			if f >= 1 && f == math.Trunc(f) && f <= float64(math.MaxInt32) {
				idx := int(f)
				if idx > maxIdx {
					maxIdx = idx
				}
			} else {
				allIntKeys = false
			}
		} else {
			allIntKeys = false
		}
		entries = append(entries, kv{key: k, val: gv})
	})

	if len(entries) == 0 {
		return map[string]any{}
	}

	if allIntKeys && maxIdx == len(entries) {
		arr := make([]any, len(entries))
		valid := true
		seen := make([]bool, len(entries))
		for _, e := range entries {
			idx := int(float64(e.key.(lua.LNumber))) - 1
			if idx < 0 || idx >= len(arr) || seen[idx] {
				valid = false
				break
			}
			seen[idx] = true
			arr[idx] = e.val
		}
		if valid {
			return arr
		}
	}

	m := make(map[string]any, len(entries))
	for _, e := range entries {
		keyStr, ok := luaKeyToString(e.key)
		if !ok {
			continue
		}
		m[keyStr] = e.val
	}
	return m
}

func luaKeyToString(k lua.LValue) (string, bool) {
	switch kv := k.(type) {
	case lua.LString:
		return string(kv), true
	case lua.LNumber:
		f := float64(kv)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return "", false
		}
		return strconv.FormatFloat(f, 'f', -1, 64), true
	case lua.LBool:
		if bool(kv) {
			return "true", true
		}
		return "false", true
	default:
		return "", false
	}
}

// luaTableToMap converts a top-level Lua table (such as Memory) to map[string]any.
func luaTableToMap(tbl *lua.LTable) map[string]any {
	out := make(map[string]any)
	if tbl == nil {
		return out
	}
	visited := map[*lua.LTable]bool{tbl: true}
	tbl.ForEach(func(k, v lua.LValue) {
		keyStr, ok := luaKeyToString(k)
		if !ok {
			return
		}
		gv, ok := luaValueToGo(v, visited, 1)
		if !ok {
			return
		}
		out[keyStr] = gv
	})
	return out
}

// goValueToLua converts a JSON-decoded Go value into a Lua value.
func goValueToLua(L *lua.LState, v any) lua.LValue {
	if v == nil {
		return lua.LNil
	}
	switch val := v.(type) {
	case bool:
		return lua.LBool(val)
	case string:
		return lua.LString(val)
	case float64:
		return lua.LNumber(val)
	case float32:
		return lua.LNumber(val)
	case int:
		return lua.LNumber(val)
	case int64:
		return lua.LNumber(val)
	case int32:
		return lua.LNumber(val)
	case uint:
		return lua.LNumber(val)
	case uint64:
		return lua.LNumber(val)
	case json.Number:
		if f, err := val.Float64(); err == nil {
			return lua.LNumber(f)
		}
		return lua.LString(val.String())
	case []any:
		tbl := L.NewTable()
		for i, item := range val {
			tbl.RawSetInt(i+1, goValueToLua(L, item))
		}
		return tbl
	case map[string]any:
		return goMapToLuaTable(L, val)
	default:
		return lua.LNil
	}
}

// goMapToLuaTable converts a Go map[string]any into a Lua table.
func goMapToLuaTable(L *lua.LState, m map[string]any) *lua.LTable {
	tbl := L.NewTable()
	for k, v := range m {
		lv := goValueToLua(L, v)
		if lv != lua.LNil {
			tbl.RawSetString(k, lv)
		}
	}
	return tbl
}

// ExportMemory converts the bot VM's global Lua Memory table into a JSON-serializable map.
func (vm *BotVM) ExportMemory() map[string]any {
	if vm == nil || vm.L == nil {
		return map[string]any{}
	}
	memVal := vm.L.GetGlobal("Memory")
	tbl, ok := memVal.(*lua.LTable)
	if !ok {
		return map[string]any{}
	}
	return luaTableToMap(tbl)
}

// ImportMemory replaces the bot VM's global Lua Memory table with data from mem.
func (vm *BotVM) ImportMemory(mem map[string]any) {
	if vm == nil || vm.L == nil {
		return
	}
	tbl := goMapToLuaTable(vm.L, mem)
	if tbl.RawGetString("creeps") == lua.LNil {
		tbl.RawSetString("creeps", vm.L.NewTable())
	}
	if tbl.RawGetString("rooms") == lua.LNil {
		tbl.RawSetString("rooms", vm.L.NewTable())
	}
	vm.L.SetGlobal("Memory", tbl)
}

// SetStatePath configures the file path for automatic periodic saves and shutdown save.
// If saveEvery > 0, StepTick automatically saves whenever world.GameTime % saveEvery == 0.
func (r *LocalRunner) SetStatePath(path string, saveEvery int) {
	r.stateMu.Lock()
	r.savePath = path
	r.saveEvery = saveEvery
	r.stateMu.Unlock()
}

// SetViewerState stores the latest viewer camera & execution settings to include in saves.
func (r *LocalRunner) SetViewerState(vs ViewerState) {
	cp := vs
	r.stateMu.Lock()
	r.viewerState = &cp
	r.stateMu.Unlock()
}

// SaveState atomically writes the World state and all bot Lua Memory tables to path.
func (r *LocalRunner) SaveState(path string) error {
	r.stepMu.Lock()
	defer r.stepMu.Unlock()
	return r.saveStateLocked(path)
}

// saveStateLocked writes the World state and bot memories; caller must hold r.stepMu.
func (r *LocalRunner) saveStateLocked(path string) error {
	if path == "" {
		return fmt.Errorf("save state: empty path")
	}

	memories := make(map[string]map[string]any, len(r.bots))
	for _, vm := range r.bots {
		if vm != nil && vm.L != nil {
			memories[vm.Username] = vm.ExportMemory()
		}
	}

	r.stateMu.RLock()
	var vs *ViewerState
	if r.viewerState != nil {
		cp := *r.viewerState
		vs = &cp
	}
	r.stateMu.RUnlock()

	sf := SaveFile{
		World:    r.world.ExportState(),
		Memories: memories,
		Viewer:   vs,
	}

	data, err := json.Marshal(sf)
	if err != nil {
		return fmt.Errorf("marshal save state: %w", err)
	}

	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create save directory %q: %w", dir, err)
		}
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write temp save file %q: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename temp save file to %q: %w", path, err)
	}
	return nil
}

// LoadSaveFile reads and unmarshals a persisted SaveFile from path.
func LoadSaveFile(path string) (*SaveFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read save file %q: %w", path, err)
	}
	var sf SaveFile
	if err := json.Unmarshal(raw, &sf); err != nil {
		return nil, fmt.Errorf("unmarshal save file %q: %w", path, err)
	}
	return &sf, nil
}

// ApplyBotMemory imports mem into the registered BotVM matching username.
func (r *LocalRunner) ApplyBotMemory(username string, mem map[string]any) {
	r.stepMu.Lock()
	defer r.stepMu.Unlock()
	for _, vm := range r.bots {
		if vm != nil && vm.Username == username {
			vm.ImportMemory(mem)
			return
		}
	}
}
