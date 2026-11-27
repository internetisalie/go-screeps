---@meta
--- Global Game, Memory, and console declarations for go-screeps.

---@class Owner
---@field username string

---@class GameCPU
---@field limit number
---@field tickLimit number
---@field bucket number
local GameCPU = {}

---Returns CPU time used during the current tick in milliseconds.
---@return number
function GameCPU.getUsed() end

---@class GameMap
local GameMap = {}

---Returns a map of exit direction strings ("1", "3", "5", "7") to adjacent room names.
---@param roomName string
---@return table<string, string>|nil
function GameMap.describeExits(roomName) end

---@class GameGlobal
---@field time integer Current simulation tick
---@field rooms table<string, Room> Visible rooms keyed by room name
---@field creeps table<string, Creep> Owned creeps keyed by creep name
---@field spawns table<string, StructureSpawn> Owned spawns keyed by spawn name
---@field structures table<string, Structure> Owned structures keyed by entity ID
---@field constructionSites table<string, ConstructionSite> Owned construction sites keyed by entity ID
---@field cpu GameCPU
---@field map GameMap
Game = {}

---Look up any visible room object by its entity ID.
---@param id string
---@return RoomObject|nil
function Game.getObjectById(id) end

---@class MemoryGlobal
---@field creeps table<string, table<string, any>>
---@field rooms table<string, table<string, any>>
---@field [string] any
Memory = {}

---@class ConsoleGlobal
console = {}

---Log one or more values to the bot console.
---@param ... any
function console.log(...) end

---Bot entrypoint called once per simulation tick by the Go engine.
function loop() end
