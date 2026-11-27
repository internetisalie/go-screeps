---@meta
--- Creep and body part definitions for go-screeps.

---@class BodyPart
---@field type BodyPartType
---@field hits integer
---@field boost? string

---@class MoveToOpts
---@field range? integer
---@field ignoreCreeps? boolean
---@field ignoreRoads? boolean
---@field maxOps? integer
---@field plainCost? integer
---@field swampCost? integer
---@field visualizePathStyle? VisualStyle

---@class Creep : RoomObject
---@field name string
---@field hits integer
---@field hitsMax integer
---@field my boolean
---@field fatigue integer
---@field spawning boolean
---@field ticksToLive? integer
---@field body BodyPart[]
---@field store Store
---@field memory table<string, any>
local Creep = {}

---Move 1 tile in the given direction (TOP..TOP_LEFT).
---@param direction ScreepsDirection
---@return ScreepsReturnCode
function Creep:move(direction) end

---Pathfind and step toward a target object, RoomPosition, or (x, y) coordinates.
---@overload fun(self: Creep, x: integer, y: integer, opts?: MoveToOpts): ScreepsReturnCode
---@param target RoomObject|RoomPosition|PosLike
---@param opts? MoveToOpts
---@return ScreepsReturnCode
function Creep:moveTo(target, opts) end

---Harvest energy from a Source or mineral from a Mineral.
---@param target Source|Mineral
---@return ScreepsReturnCode
function Creep:harvest(target) end

---Transfer resources to another creep or structure.
---@param target Creep|Structure
---@param resourceType ResourceType
---@param amount? integer
---@return ScreepsReturnCode
function Creep:transfer(target, resourceType, amount) end

---Withdraw resources from a structure or tombstone.
---@param target Structure|Tombstone
---@param resourceType ResourceType
---@param amount? integer
---@return ScreepsReturnCode
function Creep:withdraw(target, resourceType, amount) end

---Pick up a dropped resource pile.
---@param target Resource
---@return ScreepsReturnCode
function Creep:pickup(target) end

---Drop resources onto the ground.
---@param resourceType ResourceType
---@param amount? integer
---@return ScreepsReturnCode
function Creep:drop(resourceType, amount) end

---Build a construction site.
---@param target ConstructionSite
---@return ScreepsReturnCode
function Creep:build(target) end

---Repair a damaged structure.
---@param target Structure
---@return ScreepsReturnCode
function Creep:repair(target) end

---Upgrade a room controller.
---@param target StructureController
---@return ScreepsReturnCode
function Creep:upgradeController(target) end

---Melee attack a hostile creep or structure.
---@param target Creep|Structure
---@return ScreepsReturnCode
function Creep:attack(target) end

---Ranged attack a hostile creep or structure (range <= 3).
---@param target Creep|Structure
---@return ScreepsReturnCode
function Creep:rangedAttack(target) end

---Heal an adjacent creep (or self).
---@param target Creep
---@return ScreepsReturnCode
function Creep:heal(target) end

---Heal a creep within range 3.
---@param target Creep
---@return ScreepsReturnCode
function Creep:rangedHeal(target) end

---Dismantle a structure.
---@param target Structure
---@return ScreepsReturnCode
function Creep:dismantle(target) end

---Display a short speech bubble above the creep.
---@param message string
---@param public? boolean
---@return ScreepsReturnCode
function Creep:say(message, public) end
