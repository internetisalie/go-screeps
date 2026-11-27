---@meta
--- Go-Screeps engine constants injected into every Lua 5.1 bot VM by internal/lua/api.go.

---@alias ScreepsReturnCode integer
---@alias ScreepsDirection 1|2|3|4|5|6|7|8
---@alias TerrainMask 0|1|2
---@alias FindConstant integer
---@alias BodyPartType "move"|"work"|"carry"|"attack"|"ranged_attack"|"tough"|"heal"|"claim"
---@alias ResourceType "energy"|"H"|"O"|"U"|"L"|"K"|"Z"|"X"|string
---@alias StructureType "spawn"|"extension"|"road"|"constructedWall"|"rampart"|"link"|"storage"|"tower"|"container"|"controller"|"extractor"|string
---@alias LookType "creep"|"energy"|"resource"|"source"|"mineral"|"structure"|"flag"|"constructionSite"|"terrain"|"tombstone"

-- Return / error codes
---@type ScreepsReturnCode
OK = 0
---@type ScreepsReturnCode
ERR_NOT_OWNER = -1
---@type ScreepsReturnCode
ERR_NO_PATH = -2
---@type ScreepsReturnCode
ERR_NAME_EXISTS = -3
---@type ScreepsReturnCode
ERR_BUSY = -4
---@type ScreepsReturnCode
ERR_NOT_FOUND = -5
---@type ScreepsReturnCode
ERR_NOT_ENOUGH_ENERGY = -6
---@type ScreepsReturnCode
ERR_NOT_ENOUGH_RESOURCES = -6
---@type ScreepsReturnCode
ERR_NOT_ENOUGH_EXTENSIONS = -6
---@type ScreepsReturnCode
ERR_INVALID_TARGET = -7
---@type ScreepsReturnCode
ERR_FULL = -8
---@type ScreepsReturnCode
ERR_NOT_IN_RANGE = -9
---@type ScreepsReturnCode
ERR_INVALID_ARGS = -10
---@type ScreepsReturnCode
ERR_TIRED = -11
---@type ScreepsReturnCode
ERR_NO_BODYPART = -12
---@type ScreepsReturnCode
ERR_RCL_NOT_ENOUGH = -14
---@type ScreepsReturnCode
ERR_GCL_NOT_ENOUGH = -15

-- Directions
---@type ScreepsDirection
TOP = 1
---@type ScreepsDirection
TOP_RIGHT = 2
---@type ScreepsDirection
RIGHT = 3
---@type ScreepsDirection
BOTTOM_RIGHT = 4
---@type ScreepsDirection
BOTTOM = 5
---@type ScreepsDirection
BOTTOM_LEFT = 6
---@type ScreepsDirection
LEFT = 7
---@type ScreepsDirection
TOP_LEFT = 8

-- Terrain bitmasks
---@type TerrainMask
TERRAIN_MASK_PLAIN = 0
---@type TerrainMask
TERRAIN_MASK_WALL = 1
---@type TerrainMask
TERRAIN_MASK_SWAMP = 2

-- FIND_* constants
---@type FindConstant
FIND_EXIT_TOP = 1
---@type FindConstant
FIND_EXIT_RIGHT = 3
---@type FindConstant
FIND_EXIT_BOTTOM = 5
---@type FindConstant
FIND_EXIT_LEFT = 7
---@type FindConstant
FIND_EXIT = 10
---@type FindConstant
FIND_CREEPS = 101
---@type FindConstant
FIND_MY_CREEPS = 102
---@type FindConstant
FIND_HOSTILE_CREEPS = 103
---@type FindConstant
FIND_SOURCES_ACTIVE = 104
---@type FindConstant
FIND_SOURCES = 105
---@type FindConstant
FIND_DROPPED_RESOURCES = 106
---@type FindConstant
FIND_STRUCTURES = 107
---@type FindConstant
FIND_MY_STRUCTURES = 108
---@type FindConstant
FIND_HOSTILE_STRUCTURES = 109
---@type FindConstant
FIND_FLAGS = 110
---@type FindConstant
FIND_CONSTRUCTION_SITES = 111
---@type FindConstant
FIND_MY_SPAWNS = 112
---@type FindConstant
FIND_HOSTILE_SPAWNS = 113
---@type FindConstant
FIND_MY_CONSTRUCTION_SITES = 114
---@type FindConstant
FIND_HOSTILE_CONSTRUCTION_SITES = 115
---@type FindConstant
FIND_MINERALS = 116
---@type FindConstant
FIND_TOMBSTONES = 118

-- Creep & structure tuning constants
---@type integer
MAX_CREEP_SIZE = 50
---@type integer
CREEP_LIFE_TIME = 1500
---@type integer
CREEP_CLAIM_LIFE_TIME = 600
---@type integer
CREEP_SPAWN_TIME = 3
---@type integer
CARRY_CAPACITY = 50
---@type integer
HARVEST_POWER = 2
---@type integer
REPAIR_POWER = 100
---@type integer
BUILD_POWER = 5
---@type integer
ATTACK_POWER = 30
---@type integer
UPGRADE_CONTROLLER_POWER = 1
---@type integer
RANGED_ATTACK_POWER = 10
---@type integer
HEAL_POWER = 12
---@type integer
RANGED_HEAL_POWER = 4
---@type integer
SOURCE_ENERGY_CAPACITY = 3000
---@type integer
SPAWN_ENERGY_CAPACITY = 300
---@type integer
STORAGE_CAPACITY = 1000000
---@type integer
CONTAINER_CAPACITY = 2000
---@type integer
LINK_CAPACITY = 800
---@type integer
TOWER_CAPACITY = 1000

-- Body parts
---@type BodyPartType
MOVE = "move"
---@type BodyPartType
WORK = "work"
---@type BodyPartType
CARRY = "carry"
---@type BodyPartType
ATTACK = "attack"
---@type BodyPartType
RANGED_ATTACK = "ranged_attack"
---@type BodyPartType
TOUGH = "tough"
---@type BodyPartType
HEAL = "heal"
---@type BodyPartType
CLAIM = "claim"

---@type table<BodyPartType, integer>
BODYPART_COST = {
    move = 50,
    work = 100,
    carry = 50,
    attack = 80,
    ranged_attack = 150,
    tough = 10,
    heal = 250,
    claim = 600,
}

-- Resources
---@type ResourceType
RESOURCE_ENERGY = "energy"
---@type ResourceType
RESOURCE_HYDROGEN = "H"
---@type ResourceType
RESOURCE_OXYGEN = "O"
---@type ResourceType
RESOURCE_UTRIUM = "U"
---@type ResourceType
RESOURCE_LEMERGIUM = "L"
---@type ResourceType
RESOURCE_KEANIUM = "K"
---@type ResourceType
RESOURCE_ZYNTHIUM = "Z"
---@type ResourceType
RESOURCE_CATALYST = "X"

-- Structures
---@type StructureType
STRUCTURE_SPAWN = "spawn"
---@type StructureType
STRUCTURE_EXTENSION = "extension"
---@type StructureType
STRUCTURE_ROAD = "road"
---@type StructureType
STRUCTURE_WALL = "constructedWall"
---@type StructureType
STRUCTURE_RAMPART = "rampart"
---@type StructureType
STRUCTURE_LINK = "link"
---@type StructureType
STRUCTURE_STORAGE = "storage"
---@type StructureType
STRUCTURE_TOWER = "tower"
---@type StructureType
STRUCTURE_CONTAINER = "container"
---@type StructureType
STRUCTURE_CONTROLLER = "controller"
---@type StructureType
STRUCTURE_EXTRACTOR = "extractor"

-- LOOK_* constants
---@type LookType
LOOK_CREEPS = "creep"
---@type LookType
LOOK_ENERGY = "energy"
---@type LookType
LOOK_RESOURCES = "resource"
---@type LookType
LOOK_SOURCES = "source"
---@type LookType
LOOK_MINERALS = "mineral"
---@type LookType
LOOK_STRUCTURES = "structure"
---@type LookType
LOOK_FLAGS = "flag"
---@type LookType
LOOK_CONSTRUCTION_SITES = "constructionSite"
---@type LookType
LOOK_TERRAIN = "terrain"
---@type LookType
LOOK_TOMBSTONES = "tombstone"
