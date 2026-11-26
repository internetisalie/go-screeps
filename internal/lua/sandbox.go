package lua

import (
	"context"
	"fmt"
	"io/fs"
	"strings"
	"sync"
	"time"

	lua "github.com/yuin/gopher-lua"

	"go-screeps/internal/engine"
)

const (
	defaultCPULimit = 150 * time.Millisecond
	defaultMaxLogs  = 200
)

// BotVM manages a single sandboxed gopher-lua VM for one Screeps player.
type BotVM struct {
	Username  string
	UserID    string
	L         *lua.LState
	CPULimit  time.Duration
	LastCPU   time.Duration
	LastError string

	// Source loader configuration (either embedded FS or local directory).
	fsys          fs.FS
	subdir        string
	dirPath       string
	modTimes      map[string]time.Time
	loadedModules map[string]bool

	// Per-tick state used by API bindings.
	world     *engine.World
	tickStart time.Time
	objCache  map[string]*lua.LTable
	roomCache map[string]*lua.LTable

	// Reusable metatables.
	roomMT     *lua.LTable
	roomPosMT  *lua.LTable
	terrainMT  *lua.LTable
	visualMT   *lua.LTable
	storeMT    *lua.LTable
	creepMT    *lua.LTable
	spawnMT    *lua.LTable
	towerMT    *lua.LTable
	linkMT     *lua.LTable
	costMatMT  *lua.LTable

	// Thread-safe log ring buffer.
	logMu   sync.Mutex
	logs    []string
	maxLogs int
}

// NewBotVM creates a sandboxed Lua 5.1 VM for username/userID with Screeps globals and persistent Memory.
func NewBotVM(username, userID string) *BotVM {
	L := lua.NewState(lua.Options{
		SkipOpenLibs: true,
	})

	// Open only safe standard libraries (no io, os, debug, channel).
	for _, pair := range []struct {
		name string
		fn   lua.LGFunction
	}{
		{lua.LoadLibName, lua.OpenPackage},
		{lua.BaseLibName, lua.OpenBase},
		{lua.TabLibName, lua.OpenTable},
		{lua.StringLibName, lua.OpenString},
		{lua.MathLibName, lua.OpenMath},
	} {
		L.Push(L.NewFunction(pair.fn))
		L.Push(lua.LString(pair.name))
		L.Call(1, 0)
	}

	// Remove filesystem / unsafe base globals.
	L.SetGlobal("dofile", lua.LNil)
	L.SetGlobal("loadfile", lua.LNil)
	if pkg, ok := L.GetGlobal("package").(*lua.LTable); ok {
		pkg.RawSetString("loadlib", lua.LNil)
		pkg.RawSetString("cpath", lua.LString(""))
		pkg.RawSetString("path", lua.LString(""))
	}

	vm := &BotVM{
		Username:      username,
		UserID:        userID,
		L:             L,
		CPULimit:      defaultCPULimit,
		loadedModules: make(map[string]bool),
		maxLogs:       defaultMaxLogs,
	}

	// Install sandboxed print and console.log.
	printFn := L.NewFunction(vm.luaPrint)
	L.SetGlobal("print", printFn)
	consoleTbl := L.NewTable()
	consoleTbl.RawSetString("log", printFn)
	L.SetGlobal("console", consoleTbl)

	// Install sandboxed require().
	L.SetGlobal("require", L.NewFunction(vm.luaRequire))

	// Initialize persistent Memory table.
	mem := L.NewTable()
	mem.RawSetString("creeps", L.NewTable())
	mem.RawSetString("rooms", L.NewTable())
	L.SetGlobal("Memory", mem)

	// Register Screeps constants, metatables, and PathFinder.
	vm.initAPI()

	return vm
}

// Close shuts down the underlying Lua state.
func (vm *BotVM) Close() {
	if vm.L != nil {
		vm.L.Close()
		vm.L = nil
	}
}

// AppendLog adds a line to the VM's ring buffer.
func (vm *BotVM) AppendLog(line string) {
	vm.logMu.Lock()
	defer vm.logMu.Unlock()
	if vm.maxLogs <= 0 {
		vm.maxLogs = defaultMaxLogs
	}
	if len(vm.logs) >= vm.maxLogs {
		copy(vm.logs, vm.logs[1:])
		vm.logs[len(vm.logs)-1] = line
	} else {
		vm.logs = append(vm.logs, line)
	}
}

// Logs returns a thread-safe copy of the buffered log lines.
func (vm *BotVM) Logs() []string {
	vm.logMu.Lock()
	defer vm.logMu.Unlock()
	out := make([]string, len(vm.logs))
	copy(out, vm.logs)
	return out
}

func (vm *BotVM) luaPrint(L *lua.LState) int {
	n := L.GetTop()
	parts := make([]string, n)
	for i := 1; i <= n; i++ {
		parts[i-1] = L.ToStringMeta(L.Get(i)).String()
	}
	msg := strings.Join(parts, "\t")
	if vm.Username != "" {
		vm.AppendLog(fmt.Sprintf("[%s] %s", vm.Username, msg))
	} else {
		vm.AppendLog(msg)
	}
	return 0
}

// RunTick executes one tick of the bot's global loop() function against world.
// Enforces CPULimit via context.WithTimeout and checks disk hot-reload if configured.
func (vm *BotVM) RunTick(world *engine.World) error {
	if vm.L == nil {
		return fmt.Errorf("bot VM %q is closed", vm.Username)
	}

	// Check for disk hot-reload before refreshing Game and calling loop().
	if vm.dirPath != "" {
		if _, err := vm.CheckReload(); err != nil {
			vm.LastError = err.Error()
			vm.AppendLog(fmt.Sprintf("[%s] reload error: %v", vm.Username, err))
			return err
		}
	}

	vm.world = world
	vm.tickStart = time.Now()
	vm.refreshGame(world)

	limit := vm.CPULimit
	if limit <= 0 {
		limit = defaultCPULimit
	}
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	vm.L.SetContext(ctx)
	defer vm.L.RemoveContext()

	loopFn := vm.L.GetGlobal("loop")
	if loopFn.Type() != lua.LTFunction {
		vm.LastCPU = time.Since(vm.tickStart)
		vm.LastError = ""
		return nil
	}

	err := vm.L.CallByParam(lua.P{
		Fn:      loopFn,
		NRet:    0,
		Protect: true,
	})
	vm.LastCPU = time.Since(vm.tickStart)

	if err != nil {
		vm.LastError = err.Error()
		vm.AppendLog(fmt.Sprintf("[%s] error: %v", vm.Username, err))
		return err
	}

	vm.LastError = ""
	return nil
}
