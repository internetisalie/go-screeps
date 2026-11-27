package bots

import "embed"

// StarterFS embeds the starter Lua 5.4 Screeps colony bot scripts.
//
//go:embed starter/*.lua
var StarterFS embed.FS

// RaiderFS embeds the aggressive Raider Lua 5.4 Screeps colony bot scripts.
//
//go:embed raider/*.lua
var RaiderFS embed.FS
