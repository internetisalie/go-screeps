package lua

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	rt "github.com/arnodel/golua/runtime"
)

// LoadFS configures the VM to load scripts from an fs.FS (e.g. embed.FS) under subdir
// and executes main.lua.
func (vm *BotVM) LoadFS(fsys fs.FS, subdir string) error {
	vm.fsys = fsys
	vm.subdir = strings.Trim(strings.TrimSpace(subdir), "/")
	vm.dirPath = ""
	vm.modTimes = nil
	vm.clearUserModules()
	return vm.loadMain()
}

// LoadDir configures the VM to load scripts from a local directory with hot-reload tracking
// and executes main.lua.
func (vm *BotVM) LoadDir(dirPath string) error {
	absDir, err := filepath.Abs(dirPath)
	if err != nil {
		return fmt.Errorf("resolve bot dir %q: %w", dirPath, err)
	}
	info, err := os.Stat(absDir)
	if err != nil {
		return fmt.Errorf("stat bot dir %q: %w", absDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("bot path %q is not a directory", absDir)
	}

	mtimes, err := scanDirModTimes(absDir)
	if err != nil {
		return fmt.Errorf("scan bot dir %q: %w", absDir, err)
	}

	vm.fsys = nil
	vm.subdir = ""
	vm.dirPath = absDir
	vm.modTimes = mtimes
	vm.clearUserModules()
	return vm.loadMain()
}

// CheckReload checks if any .lua file in dirPath has been modified, added, or removed.
// If so, it clears the user module cache and reloads main.lua while preserving Memory.
func (vm *BotVM) CheckReload() (bool, error) {
	if vm.dirPath == "" {
		return false, nil
	}

	current, err := scanDirModTimes(vm.dirPath)
	if err != nil {
		return false, err
	}

	if !modTimesChanged(vm.modTimes, current) {
		return false, nil
	}

	vm.modTimes = current
	vm.clearUserModules()

	if err := vm.loadMain(); err != nil {
		return true, err
	}

	vm.AppendLog(fmt.Sprintf("[%s] hot-reloaded scripts from %s", vm.Username, vm.dirPath))
	return true, nil
}

func (vm *BotVM) loadMain() error {
	data, err := vm.readBotFile("main.lua")
	if err != nil {
		return fmt.Errorf("load main.lua: %w", err)
	}

	timeout := vm.CPULimit
	if timeout < 2*time.Second {
		timeout = 2 * time.Second
	}
	limitMs := uint64(timeout / time.Millisecond)
	if limitMs == 0 {
		limitMs = 2000
	}

	clos, err := vm.R.CompileAndLoadLuaChunk("main.lua", data, rt.TableValue(vm.R.GlobalEnv()))
	if err != nil {
		return fmt.Errorf("compile main.lua: %w", err)
	}

	ctx, err := vm.R.MainThread().CallContext(rt.RuntimeContextDef{
		HardLimits: rt.RuntimeResources{
			Millis: limitMs,
		},
	}, func() error {
		_, callErr := rt.Call1(vm.R.MainThread(), rt.FunctionValue(clos))
		return callErr
	})
	if err != nil {
		var termErr rt.ContextTerminationError
		if (ctx != nil && ctx.Status() == rt.StatusKilled) || errors.As(err, &termErr) {
			err = fmt.Errorf("%w: %v", context.DeadlineExceeded, err)
		}
		return fmt.Errorf("execute main.lua: %w", err)
	}
	return nil
}

func (vm *BotVM) clearUserModules() {
	if vm.R == nil {
		return
	}
	if pkg, ok := vm.R.GlobalEnv().Get(rt.StringValue("package")).TryTable(); ok {
		if loaded, ok := pkg.Get(rt.StringValue("loaded")).TryTable(); ok {
			for modName := range vm.loadedModules {
				loaded.Set(rt.StringValue(modName), rt.NilValue)
			}
		}
	}
	vm.loadedModules = make(map[string]bool)
}

func (vm *BotVM) luaRequire(t *rt.Thread, c *rt.GoCont) (rt.Cont, error) {
	if err := c.Check1Arg(); err != nil {
		return nil, err
	}
	modName, err := c.StringArg(0)
	if err != nil {
		return nil, err
	}

	var loaded *rt.Table
	if pkg, ok := vm.R.GlobalEnv().Get(rt.StringValue("package")).TryTable(); ok {
		loaded, _ = pkg.Get(rt.StringValue("loaded")).TryTable()
	}

	// Return cached module if already loaded.
	if loaded != nil {
		existing := loaded.Get(rt.StringValue(modName))
		if rt.Truth(existing) {
			return c.PushingNext1(t.Runtime, existing), nil
		}
	}

	relPath, err := sanitizeModulePath(modName)
	if err != nil {
		return nil, fmt.Errorf("require(%q): %s", modName, err.Error())
	}

	data, err := vm.readBotFile(relPath)
	if err != nil {
		return nil, fmt.Errorf("require(%q): %s", modName, err.Error())
	}

	clos, err := vm.R.CompileAndLoadLuaChunk(relPath, data, rt.TableValue(vm.R.GlobalEnv()))
	if err != nil {
		return nil, fmt.Errorf("require(%q): %s", modName, err.Error())
	}

	res, err := rt.Call1(t, rt.FunctionValue(clos))
	if err != nil {
		return nil, err
	}
	if res.IsNil() {
		res = rt.BoolValue(true)
	}

	if loaded != nil {
		loaded.Set(rt.StringValue(modName), res)
	}
	vm.loadedModules[modName] = true
	return c.PushingNext1(t.Runtime, res), nil
}

// sanitizeModulePath converts a Lua module name (e.g. "roles" or "lib.utils") into a safe
// relative .lua file path, rejecting directory traversal or absolute paths.
func sanitizeModulePath(modName string) (string, error) {
	name := strings.TrimSpace(modName)
	if name == "" {
		return "", fmt.Errorf("empty module name")
	}
	if strings.Contains(name, "\\") || strings.HasPrefix(name, "/") || strings.Contains(name, "..") {
		return "", fmt.Errorf("invalid module path %q", modName)
	}

	// Support optional ".lua" suffix; otherwise convert dots to slashes and append ".lua".
	if strings.HasSuffix(name, ".lua") {
		name = strings.TrimSuffix(name, ".lua")
	}
	name = strings.ReplaceAll(name, ".", "/")
	cleaned := path.Clean(name + ".lua")
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") {
		return "", fmt.Errorf("invalid module path %q", modName)
	}
	return cleaned, nil
}

func (vm *BotVM) readBotFile(relPath string) ([]byte, error) {
	if vm.fsys != nil {
		full := relPath
		if vm.subdir != "" {
			full = path.Join(vm.subdir, relPath)
		}
		return fs.ReadFile(vm.fsys, full)
	}
	if vm.dirPath != "" {
		full := filepath.Join(vm.dirPath, filepath.FromSlash(relPath))
		// Verify resolved path stays within vm.dirPath.
		rel, err := filepath.Rel(vm.dirPath, full)
		if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			return nil, fmt.Errorf("access denied outside bot directory")
		}
		return os.ReadFile(full)
	}
	return nil, fmt.Errorf("no bot source configured")
}

func scanDirModTimes(dirPath string) (map[string]time.Time, error) {
	mtimes := make(map[string]time.Time)
	err := filepath.WalkDir(dirPath, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".lua") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dirPath, p)
		if err != nil {
			return err
		}
		mtimes[filepath.ToSlash(rel)] = info.ModTime()
		return nil
	})
	if err != nil {
		return nil, err
	}
	return mtimes, nil
}

func modTimesChanged(prev, curr map[string]time.Time) bool {
	if len(prev) != len(curr) {
		return true
	}
	for k, t := range curr {
		pt, ok := prev[k]
		if !ok || !pt.Equal(t) {
			return true
		}
	}
	return false
}
