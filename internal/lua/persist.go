package lua

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"

	rt "github.com/arnodel/golua/runtime"

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
// Unsupported types (functions, userdata, threads, NaN/Inf) return (nil, false).
func luaValueToGo(v rt.Value, visited map[*rt.Table]bool, depth int) (any, bool) {
	if v.IsNil() || depth > maxPersistDepth {
		return nil, false
	}
	switch v.Type() {
	case rt.NilType:
		return nil, false
	case rt.BoolType:
		return v.AsBool(), true
	case rt.IntType:
		return float64(v.AsInt()), true
	case rt.FloatType:
		f := v.AsFloat()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, false
		}
		return f, true
	case rt.StringType:
		return v.AsString(), true
	case rt.TableType:
		tbl := v.AsTable()
		if visited[tbl] {
			return nil, false
		}
		return luaTableToGo(tbl, visited, depth+1), true
	default:
		// Functions, userdata, threads are skipped cleanly.
		return nil, false
	}
}

// luaTableToGo converts a Lua table to either []any (if it is a contiguous 1..N array)
// or map[string]any (for associative tables or empty tables).
func luaTableToGo(tbl *rt.Table, visited map[*rt.Table]bool, depth int) any {
	if tbl == nil || depth > maxPersistDepth {
		return map[string]any{}
	}
	visited[tbl] = true
	defer delete(visited, tbl)

	type kv struct {
		key rt.Value
		val any
	}
	var entries []kv
	allIntKeys := true
	maxIdx := 0

	for k, v, ok := tbl.Next(rt.NilValue); ok && !k.IsNil(); k, v, ok = tbl.Next(k) {
		gv, valid := luaValueToGo(v, visited, depth)
		if !valid {
			continue
		}
		switch k.Type() {
		case rt.IntType:
			n := k.AsInt()
			if n >= 1 && n <= math.MaxInt32 {
				idx := int(n)
				if idx > maxIdx {
					maxIdx = idx
				}
			} else {
				allIntKeys = false
			}
		case rt.FloatType:
			f := k.AsFloat()
			if f >= 1 && f == math.Trunc(f) && f <= float64(math.MaxInt32) {
				idx := int(f)
				if idx > maxIdx {
					maxIdx = idx
				}
			} else {
				allIntKeys = false
			}
		default:
			allIntKeys = false
		}
		entries = append(entries, kv{key: k, val: gv})
	}

	if len(entries) == 0 {
		return map[string]any{}
	}

	if allIntKeys && maxIdx == len(entries) {
		arr := make([]any, len(entries))
		valid := true
		seen := make([]bool, len(entries))
		for _, e := range entries {
			var idx int
			if e.key.Type() == rt.IntType {
				idx = int(e.key.AsInt()) - 1
			} else {
				idx = int(e.key.AsFloat()) - 1
			}
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

func luaKeyToString(k rt.Value) (string, bool) {
	switch k.Type() {
	case rt.StringType:
		return k.AsString(), true
	case rt.IntType:
		return strconv.FormatInt(k.AsInt(), 10), true
	case rt.FloatType:
		f := k.AsFloat()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return "", false
		}
		return strconv.FormatFloat(f, 'f', -1, 64), true
	case rt.BoolType:
		if k.AsBool() {
			return "true", true
		}
		return "false", true
	default:
		return "", false
	}
}

// luaTableToMap converts a top-level Lua table (such as Memory) to map[string]any.
func luaTableToMap(tbl *rt.Table) map[string]any {
	out := make(map[string]any)
	if tbl == nil {
		return out
	}
	visited := map[*rt.Table]bool{tbl: true}
	for k, v, ok := tbl.Next(rt.NilValue); ok && !k.IsNil(); k, v, ok = tbl.Next(k) {
		keyStr, okKey := luaKeyToString(k)
		if !okKey {
			continue
		}
		gv, okVal := luaValueToGo(v, visited, 1)
		if !okVal {
			continue
		}
		out[keyStr] = gv
	}
	return out
}

// floatToLuaValue preserves integer-valued JSON numbers as Lua 5.4 integers.
func floatToLuaValue(f float64) rt.Value {
	if !math.IsNaN(f) && !math.IsInf(f, 0) && f == math.Trunc(f) && f >= -9007199254740992 && f <= 9007199254740992 {
		return rt.IntValue(int64(f))
	}
	return rt.FloatValue(f)
}

// goValueToLua converts a JSON-decoded Go value into a Lua value.
func goValueToLua(v any) rt.Value {
	if v == nil {
		return rt.NilValue
	}
	switch val := v.(type) {
	case bool:
		return rt.BoolValue(val)
	case string:
		return rt.StringValue(val)
	case float64:
		return floatToLuaValue(val)
	case float32:
		return floatToLuaValue(float64(val))
	case int:
		return rt.IntValue(int64(val))
	case int64:
		return rt.IntValue(val)
	case int32:
		return rt.IntValue(int64(val))
	case uint:
		return rt.IntValue(int64(val))
	case uint64:
		return rt.IntValue(int64(val))
	case json.Number:
		if i, err := val.Int64(); err == nil {
			return rt.IntValue(i)
		}
		if f, err := val.Float64(); err == nil {
			return floatToLuaValue(f)
		}
		return rt.StringValue(val.String())
	case []any:
		tbl := rt.NewTable()
		for i, item := range val {
			tbl.Set(rt.IntValue(int64(i+1)), goValueToLua(item))
		}
		return rt.TableValue(tbl)
	case map[string]any:
		return rt.TableValue(goMapToLuaTable(val))
	default:
		return rt.NilValue
	}
}

// goMapToLuaTable converts a Go map[string]any into a Lua table.
func goMapToLuaTable(m map[string]any) *rt.Table {
	tbl := rt.NewTable()
	for k, v := range m {
		lv := goValueToLua(v)
		if !lv.IsNil() {
			tbl.Set(rt.StringValue(k), lv)
		}
	}
	return tbl
}

// ExportMemory converts the bot VM's screeps.Memory table into a JSON-serializable map.
func (vm *BotVM) ExportMemory() map[string]any {
	if vm == nil || vm.R == nil || vm.screepsMod == nil {
		return map[string]any{}
	}
	memVal := tblGet(vm.screepsMod, "Memory")
	tbl, ok := memVal.TryTable()
	if !ok {
		return map[string]any{}
	}
	return luaTableToMap(tbl)
}

// ImportMemory replaces the bot VM's screeps.Memory table with data from mem.
func (vm *BotVM) ImportMemory(mem map[string]any) {
	if vm == nil || vm.R == nil || vm.screepsMod == nil {
		return
	}
	tbl := goMapToLuaTable(mem)
	if tbl.Get(rt.StringValue("creeps")).IsNil() {
		tbl.Set(rt.StringValue("creeps"), rt.TableValue(rt.NewTable()))
	}
	if tbl.Get(rt.StringValue("rooms")).IsNil() {
		tbl.Set(rt.StringValue("rooms"), rt.TableValue(rt.NewTable()))
	}
	tblSet(vm.screepsMod, "Memory", rt.TableValue(tbl))
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
		if vm != nil && vm.R != nil {
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
