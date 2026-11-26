package lua

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"
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
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	vm.L.SetContext(ctx)
	defer vm.L.RemoveContext()

	fn, err := vm.L.Load(bytes.NewReader(data), "main.lua")
	if err != nil {
		return fmt.Errorf("compile main.lua: %w", err)
	}
	if err := vm.L.CallByParam(lua.P{Fn: fn, NRet: 0, Protect: true}); err != nil {
		return fmt.Errorf("execute main.lua: %w", err)
	}
	return nil
}

func (vm *BotVM) clearUserModules() {
	if vm.L == nil {
		return
	}
	pkg, ok := vm.L.GetGlobal("package").(*lua.LTable)
	if ok {
		if loaded, ok := pkg.RawGetString("loaded").(*lua.LTable); ok {
			for modName := range vm.loadedModules {
				loaded.RawSetString(modName, lua.LNil)
			}
		}
	}
	vm.loadedModules = make(map[string]bool)
}

func (vm *BotVM) luaRequire(L *lua.LState) int {
	modName := L.CheckString(1)

	pkg, _ := L.GetGlobal("package").(*lua.LTable)
	var loaded *lua.LTable
	if pkg != nil {
		loaded, _ = pkg.RawGetString("loaded").(*lua.LTable)
	}

	// Return cached module if already loaded.
	if loaded != nil {
		if existing := loaded.RawGetString(modName); existing != lua.LNil && existing != lua.LFalse {
			L.Push(existing)
			return 1
		}
	}

	relPath, err := sanitizeModulePath(modName)
	if err != nil {
		L.RaiseError("require(%q): %s", modName, err.Error())
		return 0
	}

	data, err := vm.readBotFile(relPath)
	if err != nil {
		L.RaiseError("require(%q): %s", modName, err.Error())
		return 0
	}

	fn, err := L.Load(bytes.NewReader(data), relPath)
	if err != nil {
		L.RaiseError("require(%q): %s", modName, err.Error())
		return 0
	}

	L.Push(fn)
	L.Call(0, 1)
	res := L.Get(-1)
	if res == lua.LNil {
		res = lua.LTrue
		L.Pop(1)
		L.Push(res)
	}

	if loaded != nil {
		loaded.RawSetString(modName, res)
	}
	vm.loadedModules[modName] = true
	return 1
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
