package lua

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"sync"
	"time"

	"github.com/arnodel/golua/lib"
	"github.com/arnodel/golua/lib/base"
	"github.com/arnodel/golua/lib/mathlib"
	"github.com/arnodel/golua/lib/packagelib"
	"github.com/arnodel/golua/lib/stringlib"
	"github.com/arnodel/golua/lib/tablelib"
	"github.com/arnodel/golua/lib/utf8lib"
	rt "github.com/arnodel/golua/runtime"
	"github.com/arnodel/golua/safeio"

	"go-screeps/internal/engine"
)

const (
	defaultCPULimit     = 150 * time.Millisecond
	defaultMaxLogs      = 200
	defaultMaxMemory    = 64 * 1024 * 1024 // 64 MiB per bot execution context
	cpuUnitsPerMs       = 100_000          // golua CPU units per millisecond of CPULimit
	maxLogLineLen       = 4096
	maxVisualsPerTick   = 500
	maxVisualPolyPoints = 256
	maxPathFinderOps    = 20000
	maxPathFinderGoals  = 64
	maxCreepBodyParts   = 50
	maxRequiresPerTick  = 64

	botComplianceFlags = rt.ComplyCpuSafe | rt.ComplyMemSafe | rt.ComplyTimeSafe | rt.ComplyIoSafe
)

// BotVM manages a single sandboxed golua (Lua 5.4) VM for one Screeps player.
type storeRef struct {
	room string
	id   string
}

type BotVM struct {
	Username  string
	UserID    string
	R         *rt.Runtime
	cleanup   func()
	CPULimit  time.Duration
	statsMu   sync.RWMutex
	LastCPU   time.Duration
	LastError string

	// Built-in screeps module table and bot tick entrypoint.
	screepsMod *rt.Table
	loopFn     rt.Callable

	// Source loader configuration (either embedded FS or local directory).
	fsys          fs.FS
	subdir        string
	dirPath       string
	modTimes      map[string]time.Time
	loadedModules map[string]bool
	requireCount  int

	// Per-tick state used by API bindings.
	world        *engine.World
	tickStart    time.Time
	objCache     map[string]*rt.Table
	roomCache    map[string]*rt.Table
	storeMeta    map[*rt.Table]storeRef
	entityRoom   map[string]string
	visibleRooms map[string]bool
	roomMeta     map[*rt.Table]string
	visualMeta   map[*rt.Table]string
	visualCount  int

	// Reusable metatables.
	roomMT    *rt.Table
	roomPosMT *rt.Table
	terrainMT *rt.Table
	visualMT  *rt.Table
	storeMT   *rt.Table
	creepMT   *rt.Table
	spawnMT   *rt.Table
	towerMT   *rt.Table
	linkMT    *rt.Table
	costMatMT *rt.Table

	// Thread-safe log ring buffer.
	logMu   sync.Mutex
	logs    []string
	maxLogs int
}

// NewBotVM creates a sandboxed Lua 5.4 VM for username/userID with the built-in require("screeps") module.
func NewBotVM(username, userID string) *BotVM {
	r := rt.New(io.Discard)

	// Open only safe standard libraries (no io, os, debug, golib, runtimelib).
	// packagelib.LibLoader is required by tablelib/stringlib/mathlib/utf8lib registration;
	// all filesystem/loader fields on package are stripped immediately below.
	cleanup := lib.LoadLibs(
		r,
		base.LibLoader,
		packagelib.LibLoader,
		tablelib.LibLoader,
		stringlib.LibLoader,
		mathlib.LibLoader,
		utf8lib.LibLoader,
	)

	env := r.GlobalEnv()

	// Remove filesystem / unsafe base globals.
	env.Set(rt.StringValue("dofile"), rt.NilValue)
	env.Set(rt.StringValue("loadfile"), rt.NilValue)
	env.Set(rt.StringValue("collectgarbage"), rt.NilValue)

	// Strip all host filesystem and C/Lua loader hooks from package, leaving only package.loaded.
	if pkgTbl, ok := env.Get(rt.StringValue("package")).TryTable(); ok {
		pkgTbl.Set(rt.StringValue("loadlib"), rt.NilValue)
		pkgTbl.Set(rt.StringValue("searchpath"), rt.NilValue)
		pkgTbl.Set(rt.StringValue("searchers"), rt.NilValue)
		pkgTbl.Set(rt.StringValue("preload"), rt.NilValue)
		pkgTbl.Set(rt.StringValue("path"), rt.NilValue)
		pkgTbl.Set(rt.StringValue("cpath"), rt.NilValue)
		pkgTbl.Set(rt.StringValue("config"), rt.NilValue)
	}

	vm := &BotVM{
		Username:      username,
		UserID:        userID,
		R:             r,
		cleanup:       cleanup,
		CPULimit:      defaultCPULimit,
		loadedModules: make(map[string]bool),
		objCache:      make(map[string]*rt.Table, 64),
		roomCache:     make(map[string]*rt.Table, 8),
		storeMeta:     make(map[*rt.Table]storeRef, 64),
		entityRoom:    make(map[string]string, 128),
		visibleRooms:  make(map[string]bool, 8),
		roomMeta:      make(map[*rt.Table]string, 8),
		visualMeta:    make(map[*rt.Table]string, 8),
		maxLogs:       defaultMaxLogs,
	}

	// Install sandboxed print() and require(). No Screeps platform globals are placed in _G.
	printFn := vm.newGoFunc("print", vm.luaPrint, 0, true)
	env.Set(rt.StringValue("print"), rt.FunctionValue(printFn))
	env.Set(rt.StringValue("require"), rt.FunctionValue(vm.newGoFunc("require", vm.luaRequire, 1, false)))

	// Build the built-in require("screeps") module and reusable metatables.
	vm.initAPI()

	return vm
}

var noFSAccess = func() *safeio.FSAccess {
	a, err := safeio.Unrestricted.Restrict(nil)
	if err != nil {
		panic("lua sandbox: failed to create restricted FSAccess: " + err.Error())
	}
	return a
}()

func (vm *BotVM) contextDef(limit time.Duration) rt.RuntimeContextDef {
	if limit <= 0 {
		limit = defaultCPULimit
	}
	limitMs := uint64(limit / time.Millisecond)
	if limitMs == 0 {
		limitMs = 1
	}
	return rt.RuntimeContextDef{
		HardLimits: rt.RuntimeResources{
			Cpu:    limitMs * cpuUnitsPerMs,
			Memory: defaultMaxMemory,
			Millis: limitMs,
		},
		RequiredFlags: botComplianceFlags,
		FSAccess:      noFSAccess,
	}
}

// Stats returns the last tick's CPU duration and error string in a thread-safe manner.
func (vm *BotVM) Stats() (time.Duration, string) {
	vm.statsMu.RLock()
	defer vm.statsMu.RUnlock()
	return vm.LastCPU, vm.LastError
}

func (vm *BotVM) setStats(cpu time.Duration, errStr string) {
	vm.statsMu.Lock()
	vm.LastCPU = cpu
	vm.LastError = errStr
	vm.statsMu.Unlock()
}

func (vm *BotVM) newGoFunc(name string, fn rt.GoFunctionFunc, nArgs int, hasEtc bool) *rt.GoFunction {
	gf := rt.NewGoFunction(fn, name, nArgs, hasEtc)
	gf.SolemnlyDeclareCompliance(botComplianceFlags)
	return gf
}

// DoString compiles and executes a Lua chunk in the VM's global environment inside a sandboxed CallContext.
func (vm *BotVM) DoString(src string) error {
	if vm.R == nil {
		return fmt.Errorf("bot VM %q is closed", vm.Username)
	}
	clos, err := vm.R.CompileAndLoadLuaChunk("<string>", []byte(src), rt.TableValue(vm.R.GlobalEnv()))
	if err != nil {
		return err
	}
	ctx, err := vm.R.MainThread().CallContext(vm.contextDef(vm.CPULimit), func() error {
		_, callErr := rt.Call1(vm.R.MainThread(), rt.FunctionValue(clos))
		return callErr
	})
	if err != nil {
		var termErr rt.ContextTerminationError
		if (ctx != nil && ctx.Status() == rt.StatusKilled) || errors.As(err, &termErr) {
			return fmt.Errorf("%w: %v", context.DeadlineExceeded, err)
		}
		return err
	}
	return nil
}

// Close shuts down the underlying Lua runtime.
func (vm *BotVM) Close() {
	if vm.cleanup != nil {
		vm.cleanup()
		vm.cleanup = nil
	}
	if vm.R != nil {
		vm.R.Close(nil)
		vm.R = nil
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

func (vm *BotVM) luaPrint(t *rt.Thread, c *rt.GoCont) (rt.Cont, error) {
	args := c.Etc()
	t.RequireCPU(uint64(len(args)*10 + 10))
	var b strings.Builder
	for i, v := range args {
		if i > 0 {
			if b.Len() >= maxLogLineLen {
				break
			}
			b.WriteByte('\t')
		}
		s, err := base.ToString(t, v)
		if err != nil {
			s = v.TypeName()
		}
		rem := maxLogLineLen - b.Len()
		if rem <= 0 {
			break
		}
		if len(s) > rem {
			b.WriteString(s[:rem])
			break
		}
		b.WriteString(s)
	}
	msg := b.String()
	if vm.Username != "" {
		vm.AppendLog(fmt.Sprintf("[%s] %s", vm.Username, msg))
	} else {
		vm.AppendLog(msg)
	}
	return c.Next(), nil
}

// RunTick executes one tick of the bot's exported loop(Game, Memory) function against world.
// Enforces CPULimit via golua's native RuntimeContext HardLimits and checks disk hot-reload if configured.
func (vm *BotVM) RunTick(world *engine.World) error {
	if vm.R == nil {
		return fmt.Errorf("bot VM %q is closed", vm.Username)
	}

	// Check for disk hot-reload before refreshing Game and calling loop().
	if vm.dirPath != "" {
		if _, err := vm.CheckReload(); err != nil {
			vm.setStats(0, err.Error())
			vm.AppendLog(fmt.Sprintf("[%s] reload error: %v", vm.Username, err))
			return err
		}
	}

	vm.world = world
	vm.tickStart = time.Now()
	vm.refreshGame(world)

	if vm.loopFn == nil {
		vm.setStats(time.Since(vm.tickStart), "")
		return nil
	}

	gameVal := tblGet(vm.screepsMod, "Game")
	memVal := tblGet(vm.screepsMod, "Memory")

	ctx, err := vm.R.MainThread().CallContext(vm.contextDef(vm.CPULimit), func() error {
		_, callErr := rt.Call1(vm.R.MainThread(), rt.FunctionValue(vm.loopFn), gameVal, memVal)
		return callErr
	})
	elapsed := time.Since(vm.tickStart)

	if err != nil {
		var termErr rt.ContextTerminationError
		if (ctx != nil && ctx.Status() == rt.StatusKilled) || errors.As(err, &termErr) {
			err = fmt.Errorf("%w: %v", context.DeadlineExceeded, err)
		}
		vm.setStats(elapsed, err.Error())
		vm.AppendLog(fmt.Sprintf("[%s] error: %v", vm.Username, err))
		return err
	}

	vm.setStats(elapsed, "")
	return nil
}
