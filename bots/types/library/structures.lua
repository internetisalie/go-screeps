---@meta
--- Structure, Source, Mineral, Resource, Tombstone, and ConstructionSite definitions.

---@class Structure : RoomObject
---@field structureType StructureType
---@field hits? integer
---@field hitsMax? integer
---@field ticksToDecay? integer

---@class SpawningInfo
---@field name string
---@field needTime integer
---@field remainingTime integer

---@class SpawnCreepOpts
---@field memory? table<string, any>
---@field directions? ScreepsDirection[]
---@field dryRun? boolean

---@class StructureSpawn : Structure
---@field name string
---@field store Store
---@field spawning? SpawningInfo
local StructureSpawn = {}

---Spawn a new creep with the specified body parts and name.
---@param body BodyPartType[]
---@param name string
---@param opts? SpawnCreepOpts
---@return ScreepsReturnCode
function StructureSpawn:spawnCreep(body, name, opts) end

---@class StructureController : Structure
---@field level integer
---@field progress integer
---@field progressTotal integer
---@field safeMode? integer
---@field safeModeAvailable integer
---@field safeModeCooldown? integer
---@field ticksToDowngrade? integer
local StructureController = {}

---Activate Safe Mode on this controller if available and off cooldown.
---@return ScreepsReturnCode
function StructureController:activateSafeMode() end

---@class StructureTower : Structure
---@field store Store
local StructureTower = {}

---@param target Creep|Structure
---@return ScreepsReturnCode
function StructureTower:attack(target) end

---@param target Creep
---@return ScreepsReturnCode
function StructureTower:heal(target) end

---@param target Structure
---@return ScreepsReturnCode
function StructureTower:repair(target) end

---@class StructureLink : Structure
---@field store Store
---@field cooldown integer
local StructureLink = {}

---@param target StructureLink
---@param amount? integer
---@return ScreepsReturnCode
function StructureLink:transferEnergy(target, amount) end

---@class StructureStorage : Structure
---@field store Store

---@class StructureContainer : Structure
---@field store Store

---@class StructureExtension : Structure
---@field store Store

---@class StructureRampart : Structure
---@class StructureWall : Structure
---@class StructureRoad : Structure

---@class Source : RoomObject
---@field energy integer
---@field energyCapacity integer
---@field ticksToRegeneration? integer

---@class Mineral : RoomObject
---@field mineralType ResourceType
---@field mineralAmount integer

---@class Resource : RoomObject
---@field resourceType ResourceType
---@field amount integer

---@class Tombstone : RoomObject
---@field store Store
---@field creepId string
---@field deathTime integer
---@field ticksToDecay? integer

---@class ConstructionSite : RoomObject
---@field structureType StructureType
---@field progress integer
---@field progressTotal integer
