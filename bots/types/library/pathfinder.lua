---@meta
--- PathFinder and PathFinder.CostMatrix definitions for go-screeps.

---@class PathFinderCostMatrix
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
---@return PathFinderCostMatrix
function CostMatrix:clone() end

---@class PathFinderGoal
---@field pos RoomPosition|PosLike
---@field range? integer

---@class PathFinderOpts
---@field plainCost? integer
---@field swampCost? integer
---@field flee? boolean
---@field maxOps? integer
---@field maxRooms? integer
---@field maxCost? integer
---@field heuristicWeight? number
---@field roomCallback? fun(roomName: string): PathFinderCostMatrix|boolean|nil

---@class PathFinderResult
---@field path RoomPosition[]
---@field ops integer
---@field cost integer
---@field incomplete boolean

---@class PathFinderAPI
---@field CostMatrix { new: fun(): PathFinderCostMatrix }|fun(): PathFinderCostMatrix
local PathFinder = {}

---Compute an optimal path from origin to goal (or list of goals) using Go-native A*.
---@param origin RoomPosition|PosLike
---@param goal PathFinderGoal|PathFinderGoal[]|RoomPosition|PosLike
---@param opts? PathFinderOpts
---@return PathFinderResult
function PathFinder.search(origin, goal, opts) end
