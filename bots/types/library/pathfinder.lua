---@meta
--- PathFinder and PathFinder.CostMatrix definitions for go-screeps.

---@class CostMatrix
local CostMatrix = {}

---Set movement cost (0..255; 255 = impassable) at tile (x, y).
---@param x integer
---@param y integer
---@param cost integer
function CostMatrix:set(x, y, cost) end

---Get movement cost at tile (x, y).
---@param x integer
---@param y integer
---@return integer
function CostMatrix:get(x, y) end

---Create a copy of this CostMatrix.
---@return CostMatrix
function CostMatrix:clone() end

---@class PathFinderGoal
---@field pos RoomPosition|PosLike
---@field range? integer

---@class PathFinderOpts
---@field plainCost? integer
---@field swampCost? integer
---@field flee? boolean
---@field maxOps? integer
---@field roomCallback? fun(roomName: string): CostMatrix|boolean|nil

---@class PathFinderResult
---@field path RoomPosition[]
---@field ops integer
---@field cost integer
---@field incomplete boolean

---@class PathFinder
---@field CostMatrix { new: fun(): CostMatrix }|fun(): CostMatrix
local PathFinder = {}

---Compute an optimal path from origin to goal (or list of goals) using Go-native A*.
---@param origin RoomPosition|PosLike
---@param goal PathFinderGoal|PathFinderGoal[]|RoomPosition|PosLike
---@param opts? PathFinderOpts
---@return PathFinderResult
function PathFinder.search(origin, goal, opts) end
