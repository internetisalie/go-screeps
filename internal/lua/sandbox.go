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

	"go-screeps/internal/engine"
)

const (
	defaultCPULimit = 150 * time.Millisecond
	defaultMaxLogs  = 200

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

	// Per-tick state used by API bindings.
	world     *engine.World
	tickStart time.Time
	objCache   map[string]*rt.Table
	roomCache  map[string]*rt.Table
	storeMeta  map[*rt.Table]storeRef
	entityRoom map[string]string

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
	if pkg, ok := env.Get(rt.StringValue("package")).TryTable(); ok {
		pkg.Set(rt.StringValue("loadlib"), rt.NilValue)
		pkg.Set(rt.StringValue("cpath"), rt.StringValue(""))
		pkg.Set(rt.StringValue("path"), rt.StringValue(""))
	}

	vm := &BotVM{
		Username:      username,
		UserID:        userID,
		R:             r,
		cleanup:       cleanup,
		CPULimit:      defaultCPULimit,
		loadedModules: make(map[string]bool),
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

func (vm *BotVM) newGoFunc(name string, fn rt.GoFunctionFunc, nArgs int, hasEtc bool) *rt.GoFunction {
	gf := rt.NewGoFunction(fn, name, nArgs, hasEtc)
	gf.SolemnlyDeclareCompliance(botComplianceFlags)
	return gf
}

// DoString compiles and executes a Lua chunk in the VM's global environment.
func (vm *BotVM) DoString(src string) error {
	if vm.R == nil {
		return fmt.Errorf("bot VM %q is closed", vm.Username)
	}
	clos, err := vm.R.CompileAndLoadLuaChunk("<string>", []byte(src), rt.TableValue(vm.R.GlobalEnv()))
	if err != nil {
		return err
	}
	_, err = rt.Call1(vm.R.MainThread(), rt.FunctionValue(clos))
	return err
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
	parts := make([]string, len(args))
	for i, v := range args {
		s, err := base.ToString(t, v)
		if err != nil {
			parts[i] = v.TypeName()
		} else {
			parts[i] = s
		}
	}
	msg := strings.Join(parts, "\t")
	if vm.Username != "" {
		vm.AppendLog(fmt.Sprintf("[%s] %s", vm.Username, msg))
	} else {
		vm.AppendLog(msg)
	}
	return c.Next(), nil
}

// RunTick executes one tick of the bot's exported loop(Game, Memory) function against world.
// Enforces CPULimit via golua's native RuntimeContext HardLimits.Millis and checks disk hot-reload if configured.
func (vm *BotVM) RunTick(world *engine.World) error {
	if vm.R == nil {
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

	if vm.loopFn == nil {
		vm.LastCPU = time.Since(vm.tickStart)
		vm.LastError = ""
		return nil
	}

	gameVal := tblGet(vm.screepsMod, "Game")
	memVal := tblGet(vm.screepsMod, "Memory")

	limit := vm.CPULimit
	if limit <= 0 {
		limit = defaultCPULimit
	}
	limitMs := uint64(limit / time.Millisecond)
	if limitMs == 0 {
		limitMs = 1
	}

	ctx, err := vm.R.MainThread().CallContext(rt.RuntimeContextDef{
		HardLimits: rt.RuntimeResources{
			Millis: limitMs,
		},
	}, func() error {
		_, callErr := rt.Call1(vm.R.MainThread(), rt.FunctionValue(vm.loopFn), gameVal, memVal)
		return callErr
	})
	vm.LastCPU = time.Since(vm.tickStart)

	if err != nil {
		var termErr rt.ContextTerminationError
		if (ctx != nil && ctx.Status() == rt.StatusKilled) || errors.As(err, &termErr) {
			err = fmt.Errorf("%w: %v", context.DeadlineExceeded, err)
		}
		vm.LastError = err.Error()
		vm.AppendLog(fmt.Sprintf("[%s] error: %v", vm.Username, err))
		return err
	}

	vm.LastError = ""
	return nil
}
