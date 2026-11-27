-- planner.lua: Object-oriented RCL 1-8 automated room planner for Screeps.

local screeps <const> = require("screeps")

---@class PlanCoord
---@field x integer
---@field y integer

---@class PerimeterSlot
---@field x integer
---@field y integer
---@field stype StructureType

---@class RoomPlan
---@field spawnX integer
---@field spawnY integer
---@field storage? PlanCoord
---@field extractor? PlanCoord
---@field mineralContainer? PlanCoord
---@field bays PlanCoord[]
---@field containers PlanCoord[]
---@field extensions PlanCoord[]
---@field towers PlanCoord[]
---@field links PlanCoord[]
---@field roads PlanCoord[]
---@field ramparts PlanCoord[]
---@field perimeter PerimeterSlot[]
---@field lastTick integer
---@field rcl integer

---@class RoomScanState
---@field solid table<integer, StructureType|boolean>
---@field hasRoad table<integer, boolean>
---@field hasRampart table<integer, boolean>
---@field counts table<StructureType, integer>
---@field containers PlanCoord[]
---@field links PlanCoord[]
---@field keyStructures PlanCoord[]
---@field storagePos? PlanCoord

---@class RoomPlanner
---@field room Room
---@field terrain RoomTerrain|nil
---@field spawn StructureSpawn|nil
---@field state RoomScanState|nil
---@field memory table<string, any>|nil
---@field EXTENSION_LIMITS table<integer, integer>
---@field TOWER_LIMITS table<integer, integer>
---@field LINK_LIMITS table<integer, integer>
local RoomPlanner = {}
RoomPlanner.__index = RoomPlanner

local MAX_EXT_SITES_PER_RUN <const> = 5
local MAX_ROAD_SITES_PER_RUN <const> = 6
local MAX_RAMPART_SITES_PER_RUN <const> = 2
local MAX_PERIMETER_SITES_PER_RUN <const> = 2
local MAX_REMOTE_SITES <const> = 10

RoomPlanner.EXTENSION_LIMITS = {
    [1] = 0,
    [2] = 5,
    [3] = 10,
    [4] = 20,
    [5] = 30,
    [6] = 40,
    [7] = 50,
    [8] = 60,
}

RoomPlanner.TOWER_LIMITS = {
    [1] = 0,
    [2] = 0,
    [3] = 1,
    [4] = 1,
    [5] = 2,
    [6] = 2,
    [7] = 3,
    [8] = 6,
}

RoomPlanner.LINK_LIMITS = {
    [1] = 0,
    [2] = 0,
    [3] = 0,
    [4] = 0,
    [5] = 2,
    [6] = 3,
    [7] = 4,
    [8] = 6,
}

---@param x integer
---@param y integer
---@return integer
function RoomPlanner.tileKey(x, y)
    return x * 50 + y
end

---@param x integer
---@param y integer
---@return boolean
function RoomPlanner.inBuildBounds(x, y)
    return x >= 2 and x <= 47 and y >= 2 and y <= 47
end

---@param x1 integer
---@param y1 integer
---@param x2 integer
---@param y2 integer
---@return integer
function RoomPlanner.chebyshev(x1, y1, x2, y2)
    return math.max(math.abs(x1 - x2), math.abs(y1 - y2))
end

---@param room? Room
---@return table<string, any>|nil
function RoomPlanner.getRoomMemory(room)
    if not room then
        return nil
    end
    if screeps.Memory and room.name then
        if not screeps.Memory.rooms then
            screeps.Memory.rooms = {}
        end
        if not screeps.Memory.rooms[room.name] then
            screeps.Memory.rooms[room.name] = room.memory or {}
        end
        return screeps.Memory.rooms[room.name]
    end
    return room.memory
end

---Return the room's primary owned Spawn (preferring Spawn1 if present), if any.
---@param room Room
---@return StructureSpawn|nil
function RoomPlanner.getPrimarySpawn(room)
    local spawns = room:find(screeps.FIND.MY_SPAWNS) ---@type StructureSpawn[]
    if not spawns or #spawns == 0 then
        return nil
    end
    for _, sp in ipairs(spawns) do
        if sp.name == "Spawn1" then
            return sp
        end
    end
    return spawns[1]
end

---@param posList PlanCoord[]
---@param targetPos PlanCoord|RoomPosition
---@param maxRange integer
---@return boolean
function RoomPlanner.hasPosNear(posList, targetPos, maxRange)
    for _, p in ipairs(posList) do
        if RoomPlanner.chebyshev(p.x, p.y, targetPos.x, targetPos.y) <= maxRange then
            return true
        end
    end
    return false
end

---Construct a RoomPlanner bound to an owned Room.
---@param room Room
---@return RoomPlanner
function RoomPlanner.new(room)
    return setmetatable({
        room = room,
        terrain = room and room:getTerrain() or nil,
        spawn = room and RoomPlanner.getPrimarySpawn(room) or nil,
        state = nil,
        memory = RoomPlanner.getRoomMemory(room),
    }, RoomPlanner)
end

---Build occupancy maps and structure/site counts for the room.
---@return RoomScanState
function RoomPlanner:scanRoomState()
    local solid = {}
    local hasRoad = {}
    local hasRampart = {}
    local counts = {}
    local containers = {}
    local links = {}
    local keyStructures = {}
    local storagePos = nil

    ---@param pos? RoomPosition
    ---@param stype? StructureType
    local function record(pos, stype)
        if not pos or not stype then
            return
        end
        local key = RoomPlanner.tileKey(pos.x, pos.y)
        counts[stype] = (counts[stype] or 0) + 1

        if stype == screeps.STRUCTURE.ROAD then
            hasRoad[key] = true
        elseif stype == screeps.STRUCTURE.RAMPART then
            hasRampart[key] = true
        else
            solid[key] = stype
        end

        if stype == screeps.STRUCTURE.CONTAINER then
            table.insert(containers, pos)
        elseif stype == screeps.STRUCTURE.LINK then
            table.insert(links, pos)
        elseif stype == screeps.STRUCTURE.STORAGE then
            storagePos = pos
            table.insert(keyStructures, pos)
        elseif stype == screeps.STRUCTURE.SPAWN or stype == screeps.STRUCTURE.TOWER then
            table.insert(keyStructures, pos)
        end
    end

    local structs = self.room:find(screeps.FIND.STRUCTURES) or {} ---@type Structure[]
    for _, s in ipairs(structs) do
        record(s.pos, s.structureType)
    end

    local sites = self.room:find(screeps.FIND.CONSTRUCTION_SITES) or {} ---@type ConstructionSite[]
    for _, s in ipairs(sites) do
        record(s.pos, s.structureType)
    end

    ---@type RoomScanState
    local state = {
        solid = solid,
        hasRoad = hasRoad,
        hasRampart = hasRampart,
        counts = counts,
        containers = containers,
        links = links,
        keyStructures = keyStructures,
        storagePos = storagePos,
    }
    self.state = state
    return state
end

---Find the best open walkable tile within [minRange, maxRange] of targetPos, closest to towardsPos.
---@param state { solid: table<integer, any> }
---@param targetPos PlanCoord|RoomPosition
---@param towardsPos? PlanCoord|RoomPosition
---@param minRange integer
---@param maxRange integer
---@return integer|nil x
---@return integer|nil y
function RoomPlanner:findBestTileNear(state, targetPos, towardsPos, minRange, maxRange)
    local terrain = self.terrain
    if not terrain then
        return nil, nil
    end

    local bestX, bestY = nil, nil
    local bestScore = math.maxinteger

    for dx = -maxRange, maxRange do
        for dy = -maxRange, maxRange do
            local dist = math.max(math.abs(dx), math.abs(dy))
            if dist >= minRange and dist <= maxRange then
                local x = targetPos.x + dx
                local y = targetPos.y + dy
                if RoomPlanner.inBuildBounds(x, y) then
                    local key = RoomPlanner.tileKey(x, y)
                    if not state.solid[key] and terrain:get(x, y) ~= 1 then
                        local score = dist * 10
                        if towardsPos then
                            score = score + RoomPlanner.chebyshev(x, y, towardsPos.x, towardsPos.y)
                        end
                        if terrain:get(x, y) == 2 then
                            score = score + 1
                        end
                        if score < bestScore then
                            bestScore = score
                            bestX = x
                            bestY = y
                        end
                    end
                end
            end
        end
    end

    return bestX, bestY
end

---Place up to `needed` structures in a checkerboard pattern around `origin`.
---@param origin PlanCoord|RoomPosition
---@param structureType StructureType
---@param needed integer
---@param minRadius integer
---@param maxRadius integer
---@param checkerboardOnly boolean
---@return integer
function RoomPlanner:placeCheckerboardNear(origin, structureType, needed, minRadius, maxRadius, checkerboardOnly)
    local placed = 0
    local terrain = self.terrain
    local state = self.state
    if needed <= 0 or not terrain or not state then
        return 0
    end

    for r = minRadius, maxRadius do
        for dx = -r, r do
            for dy = -r, r do
                if math.max(math.abs(dx), math.abs(dy)) == r then
                    local okPattern = (not checkerboardOnly) or ((math.abs(dx + dy) % 2) == 0)
                    if okPattern then
                        local x = origin.x + dx
                        local y = origin.y + dy
                        if RoomPlanner.inBuildBounds(x, y) then
                            local key = RoomPlanner.tileKey(x, y)
                            if not state.solid[key] and terrain:get(x, y) ~= 1 then
                                if self.room:createConstructionSite(x, y, structureType) == screeps.OK then
                                    state.solid[key] = structureType
                                    state.counts[structureType] = (state.counts[structureType] or 0) + 1
                                    placed = placed + 1
                                    if placed >= needed then
                                        return placed
                                    end
                                end
                            end
                        end
                    end
                end
            end
        end
    end

    return placed
end

---Shortest road path from startPos to adjacent of endPos (uses native PathFinder.search when available).
---@param startPos PlanCoord|RoomPosition
---@param endPos PlanCoord|RoomPosition
---@return PlanCoord[]
function RoomPlanner:findRoadPath(startPos, endPos)
    if screeps.PathFinder and screeps.PathFinder.search and startPos and endPos then
        local res = screeps.PathFinder.search(startPos, { pos = endPos, range = 1 }, {
            plainCost = 2,
            swampCost = 4,
            maxOps = 2000,
        })
        if res and res.path and #res.path > 0 then
            ---@type PlanCoord[]
            local path = {}
            for i = 1, #res.path do
                local step = res.path[i]
                if step and step.x and step.y then
                    table.insert(path, { x = step.x, y = step.y })
                end
            end
            return path
        end
    end

    local terrain = self.terrain
    if not terrain then
        return {}
    end

    local sx, sy = startPos.x, startPos.y
    local tx, ty = endPos.x, endPos.y

    local startKey = RoomPlanner.tileKey(sx, sy)
    local queue = { startKey }
    local head = 1
    local parent = {}
    parent[startKey] = -1

    local goalKey = nil
    local dirs <const> = {
        { 0, -1 }, { 1, 0 }, { 0, 1 }, { -1, 0 },
        { 1, -1 }, { 1, 1 }, { -1, 1 }, { -1, -1 },
    }

    while head <= #queue do
        local curr = queue[head]
        head = head + 1

        local cx = curr // 50
        local cy = curr - cx * 50

        if RoomPlanner.chebyshev(cx, cy, tx, ty) <= 1 then
            goalKey = curr
            break
        end

        for _, d in ipairs(dirs) do
            local nx = cx + d[1]
            local ny = cy + d[2]
            if nx >= 1 and nx <= 48 and ny >= 1 and ny <= 48 then
                local nk = RoomPlanner.tileKey(nx, ny)
                if parent[nk] == nil and terrain:get(nx, ny) ~= 1 then
                    parent[nk] = curr
                    table.insert(queue, nk)
                end
            end
        end
    end

    if not goalKey then
        return {}
    end

    ---@type PlanCoord[]
    local rev = {}
    local k = goalKey
    while k ~= -1 and k ~= startKey do
        local px = k // 50
        local py = k - px * 50
        table.insert(rev, { x = px, y = py })
        k = parent[k]
    end

    ---@type PlanCoord[]
    local path = {}
    for i = #rev, 1, -1 do
        table.insert(path, rev[i])
    end
    return path
end

---Plan source containers and controller container (RCL >= 2).
---@param sources Source[]
function RoomPlanner:planContainers(sources)
    local state = self.state
    local spawn = self.spawn
    if not state or not spawn or not spawn.pos then
        return
    end

    for _, src in ipairs(sources) do
        if src.pos and not RoomPlanner.hasPosNear(state.containers, src.pos, 1) then
            local cx, cy = self:findBestTileNear(state, src.pos, spawn.pos, 1, 1)
            if cx and cy then
                if self.room:createConstructionSite(cx, cy, screeps.STRUCTURE.CONTAINER) == screeps.OK then
                    local key = RoomPlanner.tileKey(cx, cy)
                    state.solid[key] = screeps.STRUCTURE.CONTAINER
                    table.insert(state.containers, { x = cx, y = cy })
                end
            end
        end
    end

    if self.room.controller and self.room.controller.pos then
        if not RoomPlanner.hasPosNear(state.containers, self.room.controller.pos, 2) then
            local cx, cy = self:findBestTileNear(state, self.room.controller.pos, spawn.pos, 1, 2)
            if cx and cy then
                if self.room:createConstructionSite(cx, cy, screeps.STRUCTURE.CONTAINER) == screeps.OK then
                    local key = RoomPlanner.tileKey(cx, cy)
                    state.solid[key] = screeps.STRUCTURE.CONTAINER
                    table.insert(state.containers, { x = cx, y = cy })
                end
            end
        end
    end
end

---Plan Storage at RCL >= 4 near Spawn1.
function RoomPlanner:planStorage()
    local state = self.state
    local spawn = self.spawn
    if not state or not spawn or not spawn.pos then
        return
    end
    if (state.counts[screeps.STRUCTURE.STORAGE] or 0) >= 1 then
        return
    end
    local towards = (self.room.controller and self.room.controller.pos) or nil
    local sx, sy = self:findBestTileNear(state, spawn.pos, towards, 2, 4)
    if sx and sy then
        if self.room:createConstructionSite(sx, sy, screeps.STRUCTURE.STORAGE) == screeps.OK then
            local key = RoomPlanner.tileKey(sx, sy)
            state.solid[key] = screeps.STRUCTURE.STORAGE
            state.counts[screeps.STRUCTURE.STORAGE] = 1
            state.storagePos = { x = sx, y = sy }
            table.insert(state.keyStructures, state.storagePos)
        end
    end
end

---Plan Links at RCL >= 5 (hub link near storage/spawn, controller link, source links).
---@param sources Source[]
---@param rcl integer
function RoomPlanner:planLinks(sources, rcl)
    local state = self.state
    local spawn = self.spawn
    if not state or not spawn or not spawn.pos then
        return
    end
    local limit = RoomPlanner.LINK_LIMITS[rcl] or 0
    local current = state.counts[screeps.STRUCTURE.LINK] or 0
    if current >= limit then
        return
    end

    ---@param anchorPos? PlanCoord|RoomPosition
    ---@param towardsPos? PlanCoord|RoomPosition
    ---@param maxRange integer
    local function placeLinkNear(anchorPos, towardsPos, maxRange)
        if current >= limit or not anchorPos then
            return
        end
        if RoomPlanner.hasPosNear(state.links, anchorPos, maxRange) then
            return
        end
        local lx, ly = self:findBestTileNear(state, anchorPos, towardsPos, 1, maxRange)
        if lx and ly then
            if self.room:createConstructionSite(lx, ly, screeps.STRUCTURE.LINK) == screeps.OK then
                local key = RoomPlanner.tileKey(lx, ly)
                state.solid[key] = screeps.STRUCTURE.LINK
                current = current + 1
                state.counts[screeps.STRUCTURE.LINK] = current
                table.insert(state.links, { x = lx, y = ly })
            end
        end
    end

    -- 1. Hub link near storage (or spawn).
    local hubAnchor = state.storagePos or spawn.pos
    placeLinkNear(hubAnchor, spawn.pos, 2)

    -- 2. Controller link.
    if self.room.controller and self.room.controller.pos then
        placeLinkNear(self.room.controller.pos, hubAnchor, 2)
    end

    -- 3. Source links.
    for _, src in ipairs(sources) do
        if src.pos then
            placeLinkNear(src.pos, hubAnchor, 2)
        end
    end
end

---Plan Ramparts protecting Spawn, Storage, and Towers (RCL >= 2).
function RoomPlanner:planRamparts()
    local state = self.state
    if not state then
        return
    end
    local placed = 0
    for _, pos in ipairs(state.keyStructures) do
        if placed >= MAX_RAMPART_SITES_PER_RUN then
            break
        end
        local key = RoomPlanner.tileKey(pos.x, pos.y)
        if not state.hasRampart[key] then
            if self.room:createConstructionSite(pos.x, pos.y, screeps.STRUCTURE.RAMPART) == screeps.OK then
                state.hasRampart[key] = true
                placed = placed + 1
            end
        end
    end
end

---Plan BFS-routed roads connecting Spawn to Sources, Controller, and Storage.
---@param sources Source[]
function RoomPlanner:planRoads(sources)
    local state = self.state
    local spawn = self.spawn
    local terrain = self.terrain
    if not state or not spawn or not spawn.pos or not terrain then
        return
    end

    ---@type (PlanCoord|RoomPosition)[]
    local targets = {}
    for _, src in ipairs(sources) do
        if src.pos then
            table.insert(targets, src.pos)
        end
    end
    if self.room.controller and self.room.controller.pos then
        table.insert(targets, self.room.controller.pos)
    end
    if state.storagePos then
        table.insert(targets, state.storagePos)
    end

    local roadsPlaced = 0
    for _, targetPos in ipairs(targets) do
        if roadsPlaced >= MAX_ROAD_SITES_PER_RUN then
            break
        end
        local path = self:findRoadPath(spawn.pos, targetPos)
        for _, step in ipairs(path) do
            if roadsPlaced >= MAX_ROAD_SITES_PER_RUN then
                break
            end
            local key = RoomPlanner.tileKey(step.x, step.y)
            if not state.hasRoad[key] and not state.solid[key] and terrain:get(step.x, step.y) ~= 1 then
                if self.room:createConstructionSite(step.x, step.y, screeps.STRUCTURE.ROAD) == screeps.OK then
                    state.hasRoad[key] = true
                    roadsPlaced = roadsPlaced + 1
                end
            end
        end
    end
end

---Build and cache the full RCL 1-8 room blueprint in room.memory.plan.
---@return RoomPlan|nil
function RoomPlanner:ensurePlan()
    local rmem = self.memory
    local spawn = self.spawn
    local terrain = self.terrain
    if not rmem or not spawn or not spawn.pos or not terrain then
        return nil
    end

    local existing = rmem.plan
    if existing and existing.spawnX == spawn.pos.x and existing.spawnY == spawn.pos.y
        and type(existing.extensions) == "table" and type(existing.bays) == "table" then
        return existing
    end

    local sx, sy = spawn.pos.x, spawn.pos.y
    local sources = self.room:find(screeps.FIND.SOURCES) or {} ---@type Source[]
    local minerals = self.room:find(screeps.FIND.MINERALS) or {} ---@type Mineral[]
    local ctrlPos = self.room.controller and self.room.controller.pos or nil

    ---@type RoomPlan
    local plan = {
        spawnX = sx,
        spawnY = sy,
        bays = {},
        containers = {},
        extensions = {},
        towers = {},
        storage = nil,
        extractor = nil,
        mineralContainer = nil,
        links = {},
        ramparts = {},
        perimeter = {},
        roads = {},
        lastTick = screeps.Game.time or 0,
        rcl = (self.room.controller and self.room.controller.level) or 1,
    }

    local reserved = {}
    reserved[RoomPlanner.tileKey(sx, sy)] = screeps.STRUCTURE.SPAWN
    local vState = { solid = reserved }

    -- 1. Source containers + controller container.
    for _, src in ipairs(sources) do
        if src.pos then
            local cx, cy = self:findBestTileNear(vState, src.pos, spawn.pos, 1, 1)
            if cx and cy then
                reserved[RoomPlanner.tileKey(cx, cy)] = screeps.STRUCTURE.CONTAINER
                table.insert(plan.containers, { x = cx, y = cy })
            end
        end
    end
    if ctrlPos then
        local cx, cy = self:findBestTileNear(vState, ctrlPos, spawn.pos, 1, 2)
        if cx and cy then
            reserved[RoomPlanner.tileKey(cx, cy)] = screeps.STRUCTURE.CONTAINER
            table.insert(plan.containers, { x = cx, y = cy })
        end
    end

    -- 2. Storage near spawn towards controller.
    local stx, sty = self:findBestTileNear(vState, spawn.pos, ctrlPos, 2, 4)
    if stx and sty then
        reserved[RoomPlanner.tileKey(stx, sty)] = screeps.STRUCTURE.STORAGE
        plan.storage = { x = stx, y = sty }
    end

    -- 3. Up to 6 towers near spawn (checkerboard first, fallback open tiles).
    ---@param list PlanCoord[]
    ---@param maxCount integer
    ---@param minR integer
    ---@param maxR integer
    ---@param checkerOnly boolean
    ---@param avoidAnchors boolean
    local function collectCheckerboard(list, maxCount, minR, maxR, checkerOnly, avoidAnchors)
        for r = minR, maxR do
            for dx = -r, r do
                for dy = -r, r do
                    if #list >= maxCount then
                        return
                    end
                    if math.max(math.abs(dx), math.abs(dy)) == r then
                        local okPattern = (not checkerOnly) or ((math.abs(dx + dy) % 2) == 0)
                        if okPattern then
                            local x = sx + dx
                            local y = sy + dy
                            if RoomPlanner.inBuildBounds(x, y) then
                                local key = RoomPlanner.tileKey(x, y)
                                if not reserved[key] and terrain:get(x, y) ~= 1 then
                                    local blocked = false
                                    if avoidAnchors then
                                        if ctrlPos and RoomPlanner.chebyshev(x, y, ctrlPos.x, ctrlPos.y) <= 1 then
                                            blocked = true
                                        end
                                        if not blocked then
                                            for _, src in ipairs(sources) do
                                                if src.pos and RoomPlanner.chebyshev(x, y, src.pos.x, src.pos.y) <= 1 then
                                                    blocked = true
                                                    break
                                                end
                                            end
                                        end
                                    end
                                    if not blocked then
                                        reserved[key] = true
                                        table.insert(list, { x = x, y = y })
                                    end
                                end
                            end
                        end
                    end
                end
            end
        end
    end

    collectCheckerboard(plan.towers, 6, 2, 6, true, true)
    if #plan.towers < 6 then
        collectCheckerboard(plan.towers, 6, 2, 7, false, true)
    end

    -- 4. Links: hub link near storage/spawn, controller link, source links.
    local hubAnchor = plan.storage or spawn.pos
    ---@param anchorPos? PlanCoord|RoomPosition
    ---@param towardsPos? PlanCoord|RoomPosition
    ---@param maxRange integer
    local function addPlannedLink(anchorPos, towardsPos, maxRange)
        if #plan.links >= 6 or not anchorPos then
            return
        end
        local lx, ly = self:findBestTileNear(vState, anchorPos, towardsPos, 1, maxRange)
        if lx and ly then
            reserved[RoomPlanner.tileKey(lx, ly)] = screeps.STRUCTURE.LINK
            table.insert(plan.links, { x = lx, y = ly })
        end
    end
    addPlannedLink(hubAnchor, spawn.pos, 2)
    if ctrlPos then
        addPlannedLink(ctrlPos, hubAnchor, 2)
    end
    for _, src in ipairs(sources) do
        if src.pos then
            addPlannedLink(src.pos, hubAnchor, 2)
        end
    end

    -- 4b. Mineral extractor + container (built at RCL >= 6).
    local mineral = minerals[1]
    if mineral and mineral.pos then
        plan.extractor = { x = mineral.pos.x, y = mineral.pos.y }
        reserved[RoomPlanner.tileKey(mineral.pos.x, mineral.pos.y)] = screeps.STRUCTURE.EXTRACTOR
        local mcx, mcy = self:findBestTileNear(vState, mineral.pos, hubAnchor, 1, 1)
        if mcx and mcy then
            plan.mineralContainer = { x = mcx, y = mcy }
            reserved[RoomPlanner.tileKey(mcx, mcy)] = screeps.STRUCTURE.CONTAINER
        end
    end

    -- 5. HiveMind 3x3 Extension Bays around spawn (hauler stands at center, fills up to 7 extensions),
    --    falling back to checkerboard extensions for any remaining slots up to 60.
    local bayRoadTiles = {} ---@type table<integer, boolean>
    local function isAnchorNear(x, y)
        if ctrlPos and RoomPlanner.chebyshev(x, y, ctrlPos.x, ctrlPos.y) <= 1 then
            return true
        end
        for _, src in ipairs(sources) do
            if src.pos and RoomPlanner.chebyshev(x, y, src.pos.x, src.pos.y) <= 1 then
                return true
            end
        end
        if mineral and mineral.pos and RoomPlanner.chebyshev(x, y, mineral.pos.x, mineral.pos.y) <= 1 then
            return true
        end
        return false
    end

    for r = 3, 12 do
        if #plan.extensions >= 60 then
            break
        end
        for dx = -r, r do
            for dy = -r, r do
                if #plan.extensions >= 60 then
                    break
                end
                if math.max(math.abs(dx), math.abs(dy)) == r and (dx % 3 == 0) and (dy % 3 == 0) then
                    local bx, by = sx + dx, sy + dy
                    if bx >= 5 and bx <= 44 and by >= 5 and by <= 44 then
                        local cKey = RoomPlanner.tileKey(bx, by)
                        if not reserved[cKey] and not bayRoadTiles[cKey] and terrain:get(bx, by) ~= 1 and not isAnchorNear(bx, by) then
                            local freeNeighbors = {} ---@type PlanCoord[]
                            local bestEntryIdx = nil
                            local bestEntryDist = math.maxinteger
                            for ndx = -1, 1 do
                                for ndy = -1, 1 do
                                    if ndx ~= 0 or ndy ~= 0 then
                                        local nx, ny = bx + ndx, by + ndy
                                        local nKey = RoomPlanner.tileKey(nx, ny)
                                        if RoomPlanner.inBuildBounds(nx, ny) and not reserved[nKey] and not bayRoadTiles[nKey]
                                            and terrain:get(nx, ny) ~= 1 and not isAnchorNear(nx, ny) then
                                            table.insert(freeNeighbors, { x = nx, y = ny })
                                            local d = RoomPlanner.chebyshev(nx, ny, sx, sy)
                                            if d < bestEntryDist then
                                                bestEntryDist = d
                                                bestEntryIdx = #freeNeighbors
                                            end
                                        end
                                    end
                                end
                            end
                            if #freeNeighbors >= 6 and bestEntryIdx then
                                bayRoadTiles[cKey] = true
                                table.insert(plan.bays, { x = bx, y = by })
                                local entry = freeNeighbors[bestEntryIdx]
                                bayRoadTiles[RoomPlanner.tileKey(entry.x, entry.y)] = true
                                for idx, nb in ipairs(freeNeighbors) do
                                    if idx ~= bestEntryIdx and #plan.extensions < 60 then
                                        reserved[RoomPlanner.tileKey(nb.x, nb.y)] = screeps.STRUCTURE.EXTENSION
                                        table.insert(plan.extensions, nb)
                                    end
                                end
                            end
                        end
                    end
                end
            end
        end
    end
    if #plan.extensions < 60 then
        collectCheckerboard(plan.extensions, 60, 2, 12, true, true)
    end

    -- 6. Ramparts over Spawn, Storage, and planned Towers.
    table.insert(plan.ramparts, { x = sx, y = sy })
    if plan.storage then
        table.insert(plan.ramparts, { x = plan.storage.x, y = plan.storage.y })
    end
    for _, pt in ipairs(plan.towers) do
        table.insert(plan.ramparts, { x = pt.x, y = pt.y })
    end

    -- 6b. Exit-chokepoint perimeter fortifications (wall + rampart gates at distance 2 from exits).
    local perimSeen = {} ---@type table<integer, boolean>
    local function addPerim(x, y, stype)
        if not RoomPlanner.inBuildBounds(x, y) then
            return
        end
        local k = RoomPlanner.tileKey(x, y)
        if perimSeen[k] or reserved[k] or terrain:get(x, y) == 1 then
            return
        end
        perimSeen[k] = true
        table.insert(plan.perimeter, { x = x, y = y, stype = stype })
    end
    local function fortifyEdge(isHorizontal, edgeCoord, innerCoord)
        local runStart = nil
        for i = 1, 48 do
            local ex, ey = isHorizontal and i or edgeCoord, isHorizontal and edgeCoord or i
            local open = terrain:get(ex, ey) ~= 1
            if open and not runStart then
                runStart = i
            elseif (not open or i == 48) and runStart then
                local runEnd = open and i or (i - 1)
                local mid = (runStart + runEnd) // 2
                local lo = math.max(2, runStart - 2)
                local hi = math.min(47, runEnd + 2)
                for p = lo, hi do
                    local px = isHorizontal and p or innerCoord
                    local py = isHorizontal and innerCoord or p
                    local stype = (math.abs(p - mid) <= 1) and screeps.STRUCTURE.RAMPART or screeps.STRUCTURE.WALL
                    addPerim(px, py, stype)
                end
                runStart = nil
            end
        end
    end
    fortifyEdge(true, 0, 2)
    fortifyEdge(true, 49, 47)
    fortifyEdge(false, 0, 2)
    fortifyEdge(false, 49, 47)

    -- 7. BFS-routed roads connecting Spawn to Sources, Controller, Storage, Mineral, and Extension Bays.
    local roadSeen = {}
    for k in pairs(bayRoadTiles) do
        local rx, ry = k % 50, k // 50
        if not roadSeen[k] and not reserved[k] and terrain:get(rx, ry) ~= 1 then
            roadSeen[k] = true
            table.insert(plan.roads, { x = rx, y = ry })
        end
    end
    ---@type (PlanCoord|RoomPosition)[]
    local targets = {}
    for _, src in ipairs(sources) do
        if src.pos then
            table.insert(targets, src.pos)
        end
    end
    if ctrlPos then
        table.insert(targets, ctrlPos)
    end
    if plan.storage then
        table.insert(targets, plan.storage)
    end
    if plan.mineralContainer then
        table.insert(targets, plan.mineralContainer)
    end
    for _, bay in ipairs(plan.bays) do
        table.insert(targets, bay)
    end

    for _, targetPos in ipairs(targets) do
        local path = self:findRoadPath(spawn.pos, targetPos)
        for _, step in ipairs(path) do
            local key = RoomPlanner.tileKey(step.x, step.y)
            if not roadSeen[key] and not reserved[key] and terrain:get(step.x, step.y) ~= 1 then
                roadSeen[key] = true
                table.insert(plan.roads, { x = step.x, y = step.y })
            end
        end
    end

    rmem.plan = plan
    return plan
end

---Plan structures and place construction sites for this room at the given RCL.
---@param rcl integer
function RoomPlanner:plan(rcl)
    local room = self.room
    local spawn = self.spawn
    local terrain = self.terrain
    if not room or not spawn or not spawn.pos or not terrain then
        return
    end

    local plan = self:ensurePlan()
    if plan then
        plan.lastTick = screeps.Game.time or 0
        plan.rcl = rcl
    end

    if rcl < 2 then
        return
    end

    local state = self:scanRoomState()
    local sources = room:find(screeps.FIND.SOURCES) or {} ---@type Source[]

    -- 1. Source and controller containers (RCL >= 2).
    if plan and plan.containers and #plan.containers > 0 then
        for _, pt in ipairs(plan.containers) do
            local key = RoomPlanner.tileKey(pt.x, pt.y)
            if not state.solid[key] then
                if room:createConstructionSite(pt.x, pt.y, screeps.STRUCTURE.CONTAINER) == screeps.OK then
                    state.solid[key] = screeps.STRUCTURE.CONTAINER
                    table.insert(state.containers, { x = pt.x, y = pt.y })
                end
            end
        end
    else
        self:planContainers(sources)
    end

    -- 2. Checkerboard extensions scaling RCL 2-8.
    local maxExt = RoomPlanner.EXTENSION_LIMITS[rcl] or 0
    local curExt = state.counts[screeps.STRUCTURE.EXTENSION] or 0
    if curExt < maxExt then
        local toPlace = math.min(MAX_EXT_SITES_PER_RUN, maxExt - curExt)
        local placed = 0
        if plan and plan.extensions then
            for _, pt in ipairs(plan.extensions) do
                if placed >= toPlace then
                    break
                end
                local key = RoomPlanner.tileKey(pt.x, pt.y)
                if not state.solid[key] and terrain:get(pt.x, pt.y) ~= 1 then
                    if room:createConstructionSite(pt.x, pt.y, screeps.STRUCTURE.EXTENSION) == screeps.OK then
                        state.solid[key] = screeps.STRUCTURE.EXTENSION
                        state.counts[screeps.STRUCTURE.EXTENSION] = (state.counts[screeps.STRUCTURE.EXTENSION] or 0) + 1
                        placed = placed + 1
                    end
                end
            end
        end
        if placed < toPlace then
            self:placeCheckerboardNear(spawn.pos, screeps.STRUCTURE.EXTENSION, toPlace - placed, 2, 12, true)
        end
    end

    -- 3. Towers scaling RCL 3-8.
    local maxTowers = RoomPlanner.TOWER_LIMITS[rcl] or 0
    local curTowers = state.counts[screeps.STRUCTURE.TOWER] or 0
    if curTowers < maxTowers then
        local needed = maxTowers - curTowers
        local placed = 0
        if plan and plan.towers then
            for _, pt in ipairs(plan.towers) do
                if placed >= needed then
                    break
                end
                local key = RoomPlanner.tileKey(pt.x, pt.y)
                if not state.solid[key] and terrain:get(pt.x, pt.y) ~= 1 then
                    if room:createConstructionSite(pt.x, pt.y, screeps.STRUCTURE.TOWER) == screeps.OK then
                        state.solid[key] = screeps.STRUCTURE.TOWER
                        state.counts[screeps.STRUCTURE.TOWER] = (state.counts[screeps.STRUCTURE.TOWER] or 0) + 1
                        table.insert(state.keyStructures, { x = pt.x, y = pt.y })
                        placed = placed + 1
                    end
                end
            end
        end
        if placed < needed then
            local extra = self:placeCheckerboardNear(spawn.pos, screeps.STRUCTURE.TOWER, needed - placed, 2, 6, true)
            placed = placed + extra
            if placed < needed then
                self:placeCheckerboardNear(spawn.pos, screeps.STRUCTURE.TOWER, needed - placed, 2, 7, false)
            end
        end
    end

    -- 4. Storage at RCL >= 4.
    if rcl >= 4 then
        if plan and plan.storage and (state.counts[screeps.STRUCTURE.STORAGE] or 0) < 1 then
            local key = RoomPlanner.tileKey(plan.storage.x, plan.storage.y)
            if not state.solid[key] then
                if room:createConstructionSite(plan.storage.x, plan.storage.y, screeps.STRUCTURE.STORAGE) == screeps.OK then
                    state.solid[key] = screeps.STRUCTURE.STORAGE
                    state.counts[screeps.STRUCTURE.STORAGE] = 1
                    state.storagePos = { x = plan.storage.x, y = plan.storage.y }
                    table.insert(state.keyStructures, state.storagePos)
                end
            end
        else
            self:planStorage()
        end
    end

    -- 5. Links at RCL >= 5.
    if rcl >= 5 then
        local linkLimit = RoomPlanner.LINK_LIMITS[rcl] or 0
        local curLinks = state.counts[screeps.STRUCTURE.LINK] or 0
        if plan and plan.links and curLinks < linkLimit then
            for _, pt in ipairs(plan.links) do
                if curLinks >= linkLimit then
                    break
                end
                local key = RoomPlanner.tileKey(pt.x, pt.y)
                if not state.solid[key] then
                    if room:createConstructionSite(pt.x, pt.y, screeps.STRUCTURE.LINK) == screeps.OK then
                        state.solid[key] = screeps.STRUCTURE.LINK
                        curLinks = curLinks + 1
                        state.counts[screeps.STRUCTURE.LINK] = curLinks
                        table.insert(state.links, { x = pt.x, y = pt.y })
                    end
                end
            end
        else
            self:planLinks(sources, rcl)
        end
    end

    -- 5b. Mineral Extractor & container at RCL >= 6.
    if rcl >= 6 and plan then
        if plan.extractor and (state.counts[screeps.STRUCTURE.EXTRACTOR] or 0) < 1 then
            local key = RoomPlanner.tileKey(plan.extractor.x, plan.extractor.y)
            if state.solid[key] ~= screeps.STRUCTURE.EXTRACTOR then
                if room:createConstructionSite(plan.extractor.x, plan.extractor.y, screeps.STRUCTURE.EXTRACTOR) == screeps.OK then
                    state.solid[key] = screeps.STRUCTURE.EXTRACTOR
                    state.counts[screeps.STRUCTURE.EXTRACTOR] = 1
                end
            end
        end
        if plan.mineralContainer then
            local key = RoomPlanner.tileKey(plan.mineralContainer.x, plan.mineralContainer.y)
            if not state.solid[key] then
                if room:createConstructionSite(plan.mineralContainer.x, plan.mineralContainer.y, screeps.STRUCTURE.CONTAINER) == screeps.OK then
                    state.solid[key] = screeps.STRUCTURE.CONTAINER
                    table.insert(state.containers, { x = plan.mineralContainer.x, y = plan.mineralContainer.y })
                end
            end
        end
    end

    -- 6. Ramparts over key structures at RCL >= 2 (protects Spawn1 from early raids).
    if rcl >= 2 then
        self:planRamparts()
    end

    -- 6b. Exit-chokepoint perimeter fortifications at RCL >= 4.
    if rcl >= 4 and plan and plan.perimeter and #plan.perimeter > 0 then
        local perimPlaced = 0
        for _, slot in ipairs(plan.perimeter) do
            if perimPlaced >= MAX_PERIMETER_SITES_PER_RUN then
                break
            end
            local key = RoomPlanner.tileKey(slot.x, slot.y)
            local already = (slot.stype == screeps.STRUCTURE.RAMPART and state.hasRampart[key])
                or (slot.stype == screeps.STRUCTURE.WALL and state.solid[key])
            if not already and terrain:get(slot.x, slot.y) ~= 1 then
                if room:createConstructionSite(slot.x, slot.y, slot.stype) == screeps.OK then
                    if slot.stype == screeps.STRUCTURE.RAMPART then
                        state.hasRampart[key] = true
                    else
                        state.solid[key] = slot.stype
                    end
                    perimPlaced = perimPlaced + 1
                end
            end
        end
    end

    -- 7. BFS-routed roads connecting Spawn, Sources, Controller, Storage.
    if plan and plan.roads and #plan.roads > 0 then
        local roadsPlaced = 0
        for _, step in ipairs(plan.roads) do
            if roadsPlaced >= MAX_ROAD_SITES_PER_RUN then
                break
            end
            local key = RoomPlanner.tileKey(step.x, step.y)
            if not state.hasRoad[key] and not state.solid[key] and terrain:get(step.x, step.y) ~= 1 then
                if room:createConstructionSite(step.x, step.y, screeps.STRUCTURE.ROAD) == screeps.OK then
                    state.hasRoad[key] = true
                    roadsPlaced = roadsPlaced + 1
                end
            end
        end
    else
        self:planRoads(sources)
    end
end

---Render translucent RoomVisual blueprints for unbuilt planned slots from cached room.memory.plan.
function RoomPlanner:drawVisuals()
    local room = self.room
    if not room or not room.visual then
        return
    end
    local rmem = self.memory
    local plan = rmem and rmem.plan
    if not plan then
        return
    end

    local vis = room.visual
    local solid = {}
    local hasRoad = {}
    local hasRampart = {}

    ---@param pos? RoomPosition
    ---@param stype? StructureType
    local function markOccupied(pos, stype)
        if not pos or not stype then
            return
        end
        local key = RoomPlanner.tileKey(pos.x, pos.y)
        if stype == screeps.STRUCTURE.ROAD then
            hasRoad[key] = true
        elseif stype == screeps.STRUCTURE.RAMPART then
            hasRampart[key] = true
        else
            solid[key] = true
        end
    end

    local structs = room:find(screeps.FIND.STRUCTURES) or {} ---@type Structure[]
    for _, s in ipairs(structs) do
        markOccupied(s.pos, s.structureType)
    end
    local sites = room:find(screeps.FIND.CONSTRUCTION_SITES) or {} ---@type ConstructionSite[]
    for _, s in ipairs(sites) do
        markOccupied(s.pos, s.structureType)
    end

    ---@param x number
    ---@param y number
    ---@param half number
    ---@param style table<string, any>
    local function drawRectOrCircle(x, y, half, style)
        if vis.rect then
            vis:rect(x - half, y - half, half * 2, half * 2, style)
        elseif vis.circle then
            vis:circle(x, y, {
                radius = half,
                fill = style.fill,
                stroke = style.stroke,
                opacity = style.opacity,
            })
        end
    end

    -- Planned roads: faint dot.
    if type(plan.roads) == "table" and vis.circle then
        local roadStyle = { radius = 0.14, fill = "#90a4ae", opacity = 0.28 }
        for _, pt in ipairs(plan.roads) do
            if type(pt) == "table" and pt.x and pt.y then
                local key = RoomPlanner.tileKey(pt.x, pt.y)
                if not hasRoad[key] and not solid[key] then
                    vis:circle(pt.x, pt.y, roadStyle)
                end
            end
        end
    end

    -- Planned ramparts: faint green circle.
    if type(plan.ramparts) == "table" and vis.circle then
        local rampartStyle = { radius = 0.46, fill = "transparent", stroke = "#66bb6a", opacity = 0.3 }
        for _, pt in ipairs(plan.ramparts) do
            if type(pt) == "table" and pt.x and pt.y then
                local key = RoomPlanner.tileKey(pt.x, pt.y)
                if not hasRampart[key] then
                    vis:circle(pt.x, pt.y, rampartStyle)
                end
            end
        end
    end

    -- Planned extensions: translucent gold circle.
    if type(plan.extensions) == "table" and vis.circle then
        local extStyle = { radius = 0.34, fill = "transparent", stroke = "#ffd54f", opacity = 0.45 }
        for _, pt in ipairs(plan.extensions) do
            if type(pt) == "table" and pt.x and pt.y then
                local key = RoomPlanner.tileKey(pt.x, pt.y)
                if not solid[key] then
                    vis:circle(pt.x, pt.y, extStyle)
                end
            end
        end
    end

    -- Planned containers: translucent amber rect.
    if type(plan.containers) == "table" then
        local contStyle = { fill = "transparent", stroke = "#ffb74d", opacity = 0.45 }
        for _, pt in ipairs(plan.containers) do
            if type(pt) == "table" and pt.x and pt.y then
                local key = RoomPlanner.tileKey(pt.x, pt.y)
                if not solid[key] then
                    drawRectOrCircle(pt.x, pt.y, 0.3, contStyle)
                end
            end
        end
    end

    -- Planned storage & links: translucent cyan rect.
    local cyanStyle = { fill = "transparent", stroke = "#4fc3f7", opacity = 0.5 }
    if type(plan.storage) == "table" and plan.storage.x and plan.storage.y then
        local key = RoomPlanner.tileKey(plan.storage.x, plan.storage.y)
        if not solid[key] then
            drawRectOrCircle(plan.storage.x, plan.storage.y, 0.36, cyanStyle)
        end
    end
    if type(plan.links) == "table" then
        for _, pt in ipairs(plan.links) do
            if type(pt) == "table" and pt.x and pt.y then
                local key = RoomPlanner.tileKey(pt.x, pt.y)
                if not solid[key] then
                    drawRectOrCircle(pt.x, pt.y, 0.3, cyanStyle)
                end
            end
        end
    end

    -- Planned towers: translucent coral/red circle.
    if type(plan.towers) == "table" and vis.circle then
        local towerStyle = { radius = 0.42, fill = "transparent", stroke = "#ff7043", opacity = 0.55 }
        for _, pt in ipairs(plan.towers) do
            if type(pt) == "table" and pt.x and pt.y then
                local key = RoomPlanner.tileKey(pt.x, pt.y)
                if not solid[key] then
                    vis:circle(pt.x, pt.y, towerStyle)
                end
            end
        end
    end
end

---Plan containers and roads inside an unowned remote mining room toward homeRoomName.
---@param room Room
---@param homeRoomName string
function RoomPlanner.planRemoteRoom(room, homeRoomName)
    if not room or not homeRoomName then
        return
    end
    local terrain = room:getTerrain()
    if not terrain then
        return
    end
    local sites = room:find(screeps.FIND.MY_CONSTRUCTION_SITES) or {} ---@type ConstructionSite[]
    if #sites >= MAX_REMOTE_SITES then
        return
    end

    local planner = RoomPlanner.new(room)
    local state = planner:scanRoomState()
    local sources = room:find(screeps.FIND.SOURCES) or {} ---@type Source[]
    if #sources == 0 then
        return
    end

    -- Locate the closest exit tile back toward homeRoomName as the road anchor.
    local exitDir = room:findExitTo(homeRoomName)
    local exitAnchor = { x = 25, y = 25 } ---@type PlanCoord
    if type(exitDir) == "number" and exitDir > 0 then
        local exits = room:find(exitDir) or {} ---@type RoomPosition[]
        if #exits > 0 then
            local mid = exits[(#exits + 1) // 2]
            exitAnchor = { x = mid.x, y = mid.y }
        end
    end

    local placed = 0
    local containerAnchors = {} ---@type PlanCoord[]
    for _, src in ipairs(sources) do
        if src.pos then
            if RoomPlanner.hasPosNear(state.containers, src.pos, 1) then
                table.insert(containerAnchors, { x = src.pos.x, y = src.pos.y })
            else
                local cx, cy = planner:findBestTileNear(state, src.pos, exitAnchor, 1, 1)
                if cx and cy and (#sites + placed) < MAX_REMOTE_SITES then
                    if room:createConstructionSite(cx, cy, screeps.STRUCTURE.CONTAINER) == screeps.OK then
                        state.solid[RoomPlanner.tileKey(cx, cy)] = screeps.STRUCTURE.CONTAINER
                        table.insert(state.containers, { x = cx, y = cy })
                        table.insert(containerAnchors, { x = cx, y = cy })
                        placed = placed + 1
                    end
                end
            end
        end
    end

    -- Route roads from each remote source container to the home exit.
    local roadsPlaced = 0
    for _, anchor in ipairs(containerAnchors) do
        if roadsPlaced >= 4 or (#sites + placed) >= MAX_REMOTE_SITES then
            break
        end
        local path = planner:findRoadPath(anchor, exitAnchor)
        for _, step in ipairs(path) do
            if roadsPlaced >= 4 or (#sites + placed) >= MAX_REMOTE_SITES then
                break
            end
            local key = RoomPlanner.tileKey(step.x, step.y)
            if RoomPlanner.inBuildBounds(step.x, step.y) and not state.hasRoad[key] and not state.solid[key] and terrain:get(step.x, step.y) ~= 1 then
                if room:createConstructionSite(step.x, step.y, screeps.STRUCTURE.ROAD) == screeps.OK then
                    state.hasRoad[key] = true
                    roadsPlaced = roadsPlaced + 1
                    placed = placed + 1
                end
            end
        end
    end
end

---Static convenience entry point: plan structures for a room at the given RCL.
---@param room Room
---@param rcl integer
function RoomPlanner.planRoom(room, rcl)
    if not room then
        return
    end
    RoomPlanner.new(room):plan(rcl)
end

---Static convenience entry point: render blueprint overlays for a room.
---@param room Room
function RoomPlanner.drawPlanVisuals(room)
    if not room then
        return
    end
    RoomPlanner.new(room):drawVisuals()
end

return RoomPlanner
