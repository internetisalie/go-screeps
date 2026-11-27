---@meta screeps
--- Built-in `require("screeps")` module definition for go-screeps (Lua 5.4).
--- No Screeps platform globals are injected into _G.

---@alias ScreepsReturnCode integer
---@alias ScreepsDirection 1|2|3|4|5|6|7|8
---@alias TerrainMask 0|1|2
---@alias FindConstant integer
---@alias BodyPartType "move"|"work"|"carry"|"attack"|"ranged_attack"|"tough"|"heal"|"claim"
---@alias ResourceType "energy"|"H"|"O"|"U"|"L"|"K"|"Z"|"X"|string
---@alias StructureType "spawn"|"extension"|"road"|"constructedWall"|"rampart"|"link"|"storage"|"tower"|"container"|"controller"|"extractor"|string
---@alias LookType "creep"|"energy"|"resource"|"source"|"mineral"|"structure"|"flag"|"constructionSite"|"terrain"|"tombstone"

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

---@class Game
---@field time integer Current simulation tick
---@field rooms table<string, Room> Visible rooms keyed by room name
---@field creeps table<string, Creep> Owned creeps keyed by creep name
---@field spawns table<string, StructureSpawn> Owned spawns keyed by spawn name
---@field structures table<string, Structure> Owned structures keyed by entity ID
---@field constructionSites table<string, ConstructionSite> Owned construction sites keyed by entity ID
---@field cpu GameCPU
---@field map GameMap
local Game = {}

---Look up any visible room object by its entity ID.
---@param id string
---@return RoomObject|nil
function Game.getObjectById(id) end

---@class Memory
---@field creeps table<string, table<string, any>>
---@field rooms table<string, table<string, any>>
---@field [string] any

---@class ScreepsErr
---@field NOT_OWNER ScreepsReturnCode
---@field NO_PATH ScreepsReturnCode
---@field NAME_EXISTS ScreepsReturnCode
---@field BUSY ScreepsReturnCode
---@field NOT_FOUND ScreepsReturnCode
---@field NOT_ENOUGH_ENERGY ScreepsReturnCode
---@field NOT_ENOUGH_RESOURCES ScreepsReturnCode
---@field NOT_ENOUGH_EXTENSIONS ScreepsReturnCode
---@field INVALID_TARGET ScreepsReturnCode
---@field FULL ScreepsReturnCode
---@field NOT_IN_RANGE ScreepsReturnCode
---@field INVALID_ARGS ScreepsReturnCode
---@field TIRED ScreepsReturnCode
---@field NO_BODYPART ScreepsReturnCode
---@field RCL_NOT_ENOUGH ScreepsReturnCode
---@field GCL_NOT_ENOUGH ScreepsReturnCode

---@class ScreepsDir
---@field TOP ScreepsDirection
---@field TOP_RIGHT ScreepsDirection
---@field RIGHT ScreepsDirection
---@field BOTTOM_RIGHT ScreepsDirection
---@field BOTTOM ScreepsDirection
---@field BOTTOM_LEFT ScreepsDirection
---@field LEFT ScreepsDirection
---@field TOP_LEFT ScreepsDirection

---@class ScreepsTerrain
---@field PLAIN TerrainMask
---@field WALL TerrainMask
---@field SWAMP TerrainMask

---@class ScreepsFind
---@field EXIT_TOP FindConstant
---@field EXIT_RIGHT FindConstant
---@field EXIT_BOTTOM FindConstant
---@field EXIT_LEFT FindConstant
---@field EXIT FindConstant
---@field CREEPS FindConstant
---@field MY_CREEPS FindConstant
---@field HOSTILE_CREEPS FindConstant
---@field SOURCES_ACTIVE FindConstant
---@field SOURCES FindConstant
---@field DROPPED_RESOURCES FindConstant
---@field STRUCTURES FindConstant
---@field MY_STRUCTURES FindConstant
---@field HOSTILE_STRUCTURES FindConstant
---@field FLAGS FindConstant
---@field CONSTRUCTION_SITES FindConstant
---@field MY_SPAWNS FindConstant
---@field HOSTILE_SPAWNS FindConstant
---@field MY_CONSTRUCTION_SITES FindConstant
---@field HOSTILE_CONSTRUCTION_SITES FindConstant
---@field MINERALS FindConstant
---@field TOMBSTONES FindConstant

---@class ScreepsPart
---@field MOVE BodyPartType
---@field WORK BodyPartType
---@field CARRY BodyPartType
---@field ATTACK BodyPartType
---@field RANGED_ATTACK BodyPartType
---@field TOUGH BodyPartType
---@field HEAL BodyPartType
---@field CLAIM BodyPartType

---@class ScreepsCost
---@field move integer
---@field work integer
---@field carry integer
---@field attack integer
---@field ranged_attack integer
---@field tough integer
---@field heal integer
---@field claim integer
---@field [BodyPartType] integer

---@class ScreepsResource
---@field ENERGY ResourceType
---@field HYDROGEN ResourceType
---@field OXYGEN ResourceType
---@field UTRIUM ResourceType
---@field LEMERGIUM ResourceType
---@field KEANIUM ResourceType
---@field ZYNTHIUM ResourceType
---@field CATALYST ResourceType

---@class ScreepsStructure
---@field SPAWN StructureType
---@field EXTENSION StructureType
---@field ROAD StructureType
---@field WALL StructureType
---@field RAMPART StructureType
---@field LINK StructureType
---@field STORAGE StructureType
---@field TOWER StructureType
---@field CONTAINER StructureType
---@field CONTROLLER StructureType
---@field EXTRACTOR StructureType

---@class ScreepsLook
---@field CREEPS LookType
---@field ENERGY LookType
---@field RESOURCES LookType
---@field SOURCES LookType
---@field MINERALS LookType
---@field STRUCTURES LookType
---@field FLAGS LookType
---@field CONSTRUCTION_SITES LookType
---@field TERRAIN LookType
---@field TOMBSTONES LookType

---@class ScreepsConst
---@field MAX_CREEP_SIZE integer
---@field CREEP_LIFE_TIME integer
---@field CREEP_CLAIM_LIFE_TIME integer
---@field CREEP_SPAWN_TIME integer
---@field CARRY_CAPACITY integer
---@field HARVEST_POWER integer
---@field REPAIR_POWER integer
---@field BUILD_POWER integer
---@field ATTACK_POWER integer
---@field UPGRADE_CONTROLLER_POWER integer
---@field RANGED_ATTACK_POWER integer
---@field HEAL_POWER integer
---@field RANGED_HEAL_POWER integer
---@field SOURCE_ENERGY_CAPACITY integer
---@field SPAWN_ENERGY_CAPACITY integer
---@field STORAGE_CAPACITY integer
---@field CONTAINER_CAPACITY integer
---@field LINK_CAPACITY integer
---@field TOWER_CAPACITY integer

---@class screeps
---@field OK ScreepsReturnCode
---@field ERR ScreepsErr
---@field DIR ScreepsDir
---@field TERRAIN ScreepsTerrain
---@field FIND ScreepsFind
---@field PART ScreepsPart
---@field COST ScreepsCost
---@field RESOURCE ScreepsResource
---@field STRUCTURE ScreepsStructure
---@field LOOK ScreepsLook
---@field CONST ScreepsConst
---@field Game Game
---@field Memory Memory
---@field PathFinder PathFinder
local screeps = {}

---Construct a new RoomPosition.
---@param x integer
---@param y integer
---@param roomName string
---@return RoomPosition
function screeps.RoomPosition(x, y, roomName) end

---Log one or more values to the bot console.
---@param ... any
function screeps.log(...) end

return screeps
