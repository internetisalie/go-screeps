---@meta
--- Room, RoomPosition, RoomTerrain, RoomVisual, RoomObject, and Store definitions.

---@class PosLike
---@field x integer
---@field y integer
---@field roomName? string

---@class RoomObject
---@field id string
---@field pos RoomPosition
---@field room Room
---@field hits? integer
---@field hitsMax? integer
---@field my? boolean
---@field owner? Owner
---@field store? Store

---@class RoomPosition
---@field x integer
---@field y integer
---@field roomName string
local RoomPosition = {}

---Chebyshev distance to another position or RoomObject (or x, y coordinates).
---@overload fun(self: RoomPosition, x: integer, y: integer): integer
---@param target RoomObject|RoomPosition|PosLike
---@return integer
function RoomPosition:getRangeTo(target) end

---Returns true if Chebyshev distance to target is <= 1.
---@overload fun(self: RoomPosition, x: integer, y: integer): boolean
---@param target RoomObject|RoomPosition|PosLike
---@return boolean
function RoomPosition:isNearTo(target) end

---Returns true if Chebyshev distance to target is <= range.
---@overload fun(self: RoomPosition, x: integer, y: integer, range: integer): boolean
---@param target RoomObject|RoomPosition|PosLike
---@param range integer
---@return boolean
function RoomPosition:inRangeTo(target, range) end

---Returns true if coordinates (and roomName when present) match.
---@overload fun(self: RoomPosition, x: integer, y: integer): boolean
---@param target RoomObject|RoomPosition|PosLike
---@return boolean
function RoomPosition:isEqualTo(target) end

---Finds the closest object from a list (or FIND_* constant) by Chebyshev range.
---@generic T: RoomObject|PosLike
---@param targets T[]|FindConstant
---@return T|nil
function RoomPosition:findClosestByRange(targets) end

---@class RoomTerrain
local RoomTerrain = {}

---Returns terrain mask (TERRAIN_MASK_PLAIN=0, TERRAIN_MASK_WALL=1, TERRAIN_MASK_SWAMP=2) at (x, y).
---@param x integer
---@param y integer
---@return TerrainMask
function RoomTerrain:get(x, y) end

---@class VisualStyle
---@field color? string
---@field fill? string
---@field stroke? string
---@field strokeWidth? number
---@field width? number
---@field radius? number
---@field opacity? number
---@field lineStyle? "solid"|"dashed"|"dotted"
---@field font? string|number
---@field align? "left"|"center"|"right"

---@class RoomVisual
---@field roomName string
local RoomVisual = {}

---@overload fun(self: RoomVisual, pos: RoomPosition|PosLike|RoomObject, style?: VisualStyle): RoomVisual
---@param x number
---@param y number
---@param style? VisualStyle
---@return RoomVisual
function RoomVisual:circle(x, y, style) end

---@overload fun(self: RoomVisual, pos1: RoomPosition|PosLike|RoomObject, pos2: RoomPosition|PosLike|RoomObject, style?: VisualStyle): RoomVisual
---@param x1 number
---@param y1 number
---@param x2 number
---@param y2 number
---@param style? VisualStyle
---@return RoomVisual
function RoomVisual:line(x1, y1, x2, y2, style) end

---@overload fun(self: RoomVisual, pos: RoomPosition|PosLike|RoomObject, w: number, h: number, style?: VisualStyle): RoomVisual
---@param x number
---@param y number
---@param w number
---@param h number
---@param style? VisualStyle
---@return RoomVisual
function RoomVisual:rect(x, y, w, h, style) end

---@param points (number[]|RoomPosition|PosLike)[]
---@param style? VisualStyle
---@return RoomVisual
function RoomVisual:poly(points, style) end

---@overload fun(self: RoomVisual, text: string, pos: RoomPosition|PosLike|RoomObject, style?: VisualStyle): RoomVisual
---@param text string
---@param x number
---@param y number
---@param style? VisualStyle
---@return RoomVisual
function RoomVisual:text(text, x, y, style) end

---@class Store
---@field energy? integer
---@field [string] any
local Store = {}

---Returns amount of the given resource (or total used capacity if nil).
---@param resource? ResourceType
---@return integer
function Store:getUsedCapacity(resource) end

---Returns free capacity for the given resource (or total free capacity if nil).
---@param resource? ResourceType
---@return integer
function Store:getFreeCapacity(resource) end

---Returns total capacity for the given resource (or overall capacity if nil).
---@param resource? ResourceType
---@return integer
function Store:getCapacity(resource) end

---@class Room
---@field name string
---@field energyAvailable integer
---@field energyCapacityAvailable integer
---@field controller? StructureController
---@field visual RoomVisual
---@field memory table<string, any>
local Room = {}

---Find objects in the room matching a FIND_* constant.
---@param findType FindConstant
---@param opts? { filter?: fun(obj: RoomObject): boolean }
---@return any[]
function Room:find(findType, opts) end

---Queue a construction site at (x, y) or pos for the given structureType.
---@overload fun(self: Room, pos: RoomPosition|PosLike|RoomObject, structureType: StructureType): ScreepsReturnCode
---@param x integer
---@param y integer
---@param structureType StructureType
---@return ScreepsReturnCode
function Room:createConstructionSite(x, y, structureType) end

---Returns the RoomTerrain accessor for this room.
---@return RoomTerrain
function Room:getTerrain() end

---Returns objects of the specified LOOK_* type at (x, y) or pos.
---@overload fun(self: Room, lookType: LookType, pos: RoomPosition|PosLike|RoomObject): any[]
---@param lookType LookType
---@param x integer
---@param y integer
---@return any[]
function Room:lookForAt(lookType, x, y) end

---Returns exit direction constant (FIND_EXIT_TOP/RIGHT/BOTTOM/LEFT) toward targetRoom, or ERR_NO_PATH.
---@param targetRoom string
---@return FindConstant|ScreepsReturnCode
function Room:findExitTo(targetRoom) end
