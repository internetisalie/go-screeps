-- planner.lua: Compact, aggressive base layout planner for the Raider colony.
-- Places source containers, fast roads, tight checkerboard extensions around spawn,
-- defensive tower(s) + ramparts at RCL 3+, and storage at RCL 4+.

local screeps <const> = require("screeps")

local MAX_ACTIVE_SITES <const> = 6

---@type table<integer, integer>
local EXTENSION_LIMITS <const> = {
    [1] = 0,
    [2] = 5,
    [3] = 10,
    [4] = 20,
    [5] = 30,
    [6] = 40,
    [7] = 50,
    [8] = 60,
}

---@type table<integer, integer>
local TOWER_LIMITS <const> = {
    [1] = 0,
    [2] = 0,
    [3] = 1,
    [4] = 1,
    [5] = 2,
    [6] = 2,
    [7] = 3,
    [8] = 6,
}

---@class RaiderPlanPoint
---@field x integer
---@field y integer

---@class RaiderPlan
---@field spawnX integer
---@field spawnY integer
---@field containers RaiderPlanPoint[]
---@field towers RaiderPlanPoint[]
---@field storage RaiderPlanPoint|nil
---@field extensions RaiderPlanPoint[]
---@field ramparts RaiderPlanPoint[]
---@field roads RaiderPlanPoint[]

---@class RaiderPlanner
---@field room Room
---@field terrain RoomTerrain|nil
---@field spawn StructureSpawn|nil
---@field occ table<string, table<StructureType, boolean>>
---@field counts table<StructureType, integer>
---@field activeSites integer
---@field memory table<string, any>|nil
local RaiderPlanner = {}
RaiderPlanner.__index = RaiderPlanner

---@param x integer
---@param y integer
---@return string
function RaiderPlanner.tileKey(x, y)
    return tostring(x) .. ":" .. tostring(y)
end

---@param x integer
---@param y integer
---@return integer
function RaiderPlanner.fastKey(x, y)
    return x * 50 + y
end

---@param room Room|nil
---@return table<string, any>|nil
function RaiderPlanner.getRoomMemory(room)
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

---@param room Room|nil
---@return StructureSpawn|nil
function RaiderPlanner.getPrimarySpawn(room)
    if not room or not room.find then
        return nil
    end
    local spawns = room:find(screeps.FIND.MY_SPAWNS) ---@type StructureSpawn[]|nil
    if spawns and #spawns > 0 then
        return spawns[1]
    end
    return nil
end

---@param room Room|nil
---@return Structure[]
function RaiderPlanner.getSourceContainers(room)
    ---@type Structure[]
    local containers = {}
    if not room or not room.find then
        return containers
    end
    local sources = room:find(screeps.FIND.SOURCES) or {} ---@type Source[]
    local structs = room:find(screeps.FIND.STRUCTURES) or {} ---@type Structure[]
    for _, s in ipairs(structs) do
        if s.structureType == screeps.STRUCTURE.CONTAINER and s.pos then
            for _, src in ipairs(sources) do
                if src.pos and src.pos:getRangeTo(s) <= 2 then
                    table.insert(containers, s)
                    break
                end
            end
        end
    end
    return containers
end

---Mark a tile as occupied in blueprint overlay lookup sets.
---@param solid table<integer, boolean>
---@param hasRoad table<integer, boolean>
---@param hasRampart table<integer, boolean>
---@param pos RoomPosition|nil
---@param stype StructureType|nil
function RaiderPlanner.markOccupied(solid, hasRoad, hasRampart, pos, stype)
    if not pos or not stype then
        return
    end
    local k = RaiderPlanner.fastKey(pos.x, pos.y)
    if stype == screeps.STRUCTURE.ROAD then
        hasRoad[k] = true
    elseif stype == screeps.STRUCTURE.RAMPART then
        hasRampart[k] = true
    else
        solid[k] = true
    end
end

---Draw a centered rectangle or circle fallback on a RoomVisual.
---@param vis RoomVisual
---@param x integer
---@param y integer
---@param half number
---@param style table<string, any>
function RaiderPlanner.drawRectOrCircle(vis, x, y, half, style)
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

---Construct a RaiderPlanner bound to a Room.
---@param room Room
---@return RaiderPlanner
function RaiderPlanner.new(room)
    local terrain = (room and room.getTerrain) and room:getTerrain() or nil
    return setmetatable({
        room = room,
        terrain = terrain,
        spawn = RaiderPlanner.getPrimarySpawn(room),
        occ = {},
        counts = {},
        activeSites = 0,
        memory = RaiderPlanner.getRoomMemory(room),
    }, RaiderPlanner)
end

---@param x integer
---@param y integer
---@return boolean
function RaiderPlanner:isWalkable(x, y)
    if x < 2 or x > 47 or y < 2 or y > 47 then
        return false
    end
    if not self.terrain or not self.terrain.get then
        return true
    end
    local t = self.terrain:get(x, y)
    return t ~= (screeps.TERRAIN.WALL or 1)
end

---Populate occupancy map, structure counts, and active construction site count.
function RaiderPlanner:buildOccupancyMap()
    ---@type table<string, table<StructureType, boolean>>
    local occ = {}
    ---@type table<StructureType, integer>
    local counts = {}

    local structs = self.room:find(screeps.FIND.STRUCTURES) or {} ---@type Structure[]
    for _, s in ipairs(structs) do
        if s.pos then
            local k = RaiderPlanner.tileKey(s.pos.x, s.pos.y)
            if not occ[k] then
                occ[k] = {}
            end
            occ[k][s.structureType] = true
            counts[s.structureType] = (counts[s.structureType] or 0) + 1
        end
    end

    local sites = self.room:find(screeps.FIND.CONSTRUCTION_SITES) or {} ---@type ConstructionSite[]
    local activeSites = 0
    for _, site in ipairs(sites) do
        activeSites = activeSites + 1
        if site.pos then
            local k = RaiderPlanner.tileKey(site.pos.x, site.pos.y)
            if not occ[k] then
                occ[k] = {}
            end
            occ[k][site.structureType] = true
            counts[site.structureType] = (counts[site.structureType] or 0) + 1
        end
    end

    self.occ = occ
    self.counts = counts
    self.activeSites = activeSites
end

---@param x integer
---@param y integer
---@param allowRoad boolean
---@return boolean
function RaiderPlanner:hasBlockingStructure(x, y, allowRoad)
    local cell = self.occ[RaiderPlanner.tileKey(x, y)]
    if not cell then
        return false
    end
    for stype, _ in pairs(cell) do
        if stype == screeps.STRUCTURE.RAMPART then
            -- Ramparts can coexist with other structures.
        elseif allowRoad and stype == screeps.STRUCTURE.ROAD then
            -- Roads can share a tile with containers if needed.
        else
            return true
        end
    end
    return false
end

---@param x integer
---@param y integer
---@param stype StructureType
---@return boolean
function RaiderPlanner:hasStructureType(x, y, stype)
    local cell = self.occ[RaiderPlanner.tileKey(x, y)]
    return cell ~= nil and cell[stype] == true
end

---@param x integer
---@param y integer
---@param stype StructureType
---@param allowRoad boolean
---@return boolean
function RaiderPlanner:tryPlace(x, y, stype, allowRoad)
    if self.activeSites >= MAX_ACTIVE_SITES then
        return false
    end
    if x < 2 or x > 47 or y < 2 or y > 47 then
        return false
    end
    if self:hasStructureType(x, y, stype) then
        return false
    end
    if stype ~= screeps.STRUCTURE.RAMPART and stype ~= screeps.STRUCTURE.ROAD then
        if self:hasBlockingStructure(x, y, allowRoad) then
            return false
        end
    elseif stype == screeps.STRUCTURE.ROAD then
        if self:hasBlockingStructure(x, y, true) then
            return false
        end
    end

    local res = self.room:createConstructionSite(x, y, stype)
    if res == screeps.OK then
        local k = RaiderPlanner.tileKey(x, y)
        if not self.occ[k] then
            self.occ[k] = {}
        end
        self.occ[k][stype] = true
        self.counts[stype] = (self.counts[stype] or 0) + 1
        self.activeSites = self.activeSites + 1
        return true
    end
    return false
end

---Find the best walkable tile adjacent to a source, closest to spawn.
---@param srcPos RoomPosition
---@param spawnPos RoomPosition
---@return integer|nil, integer|nil
function RaiderPlanner:findSourceContainerTile(srcPos, spawnPos)
    local bestX, bestY = nil, nil
    local bestDist = math.maxinteger
    for dx = -1, 1 do
        for dy = -1, 1 do
            if not (dx == 0 and dy == 0) then
                local tx = srcPos.x + dx
                local ty = srcPos.y + dy
                if self:isWalkable(tx, ty) then
                    local d = math.max(math.abs(tx - spawnPos.x), math.abs(ty - spawnPos.y))
                    if d < bestDist then
                        bestDist = d
                        bestX = tx
                        bestY = ty
                    end
                end
            end
        end
    end
    return bestX, bestY
end

---Generate a simple road path from origin to target using PathFinder (or direct stepping fallback).
---@param fromPos RoomPosition
---@param toPos RoomPosition
---@param range integer|nil
---@return RaiderPlanPoint[]
function RaiderPlanner:computeRoadPath(fromPos, toPos, range)
    ---@type RaiderPlanPoint[]
    local path = {}
    if screeps.PathFinder and screeps.PathFinder.search and fromPos and toPos then
        local res = screeps.PathFinder.search(fromPos, { pos = toPos, range = range or 1 }, {
            plainCost = 2,
            swampCost = 4,
            maxOps = 1000,
        })
        if res and res.path and #res.path > 0 then
            for _, step in ipairs(res.path) do
                table.insert(path, { x = step.x, y = step.y })
            end
            return path
        end
    end

    -- Fallback: greedy diagonal stepping toward target.
    local cx, cy = fromPos.x, fromPos.y
    local guard = 0
    while math.max(math.abs(cx - toPos.x), math.abs(cy - toPos.y)) > (range or 1) and guard < 60 do
        guard = guard + 1
        local dx = 0
        local dy = 0
        if toPos.x > cx then
            dx = 1
        elseif toPos.x < cx then
            dx = -1
        end
        if toPos.y > cy then
            dy = 1
        elseif toPos.y < cy then
            dy = -1
        end
        local nx, ny = cx + dx, cy + dy
        if self:isWalkable(nx, ny) then
            cx, cy = nx, ny
            table.insert(path, { x = cx, y = cy })
        elseif dx ~= 0 and self:isWalkable(cx + dx, cy) then
            cx = cx + dx
            table.insert(path, { x = cx, y = cy })
        elseif dy ~= 0 and self:isWalkable(cx, cy + dy) then
            cy = cy + dy
            table.insert(path, { x = cx, y = cy })
        else
            break
        end
    end
    return path
end

---Append deduplicated road points to a plan, skipping the spawn tile.
---@param plan RaiderPlan
---@param roadSeen table<string, boolean>
---@param sx integer
---@param sy integer
---@param pts RaiderPlanPoint[]
function RaiderPlanner:addRoadPoints(plan, roadSeen, sx, sy, pts)
    for _, p in ipairs(pts) do
        local k = RaiderPlanner.tileKey(p.x, p.y)
        if not roadSeen[k] and not (p.x == sx and p.y == sy) then
            roadSeen[k] = true
            table.insert(plan.roads, { x = p.x, y = p.y })
        end
    end
end

---Build and cache the compact checkerboard layout in room.memory.raiderPlan.
---@return RaiderPlan|nil
function RaiderPlanner:ensurePlan()
    local rmem = self.memory
    local spawn = self.spawn
    if not rmem or not spawn or not spawn.pos then
        return nil
    end

    local existing = rmem.raiderPlan ---@type RaiderPlan|nil
    if existing and existing.spawnX == spawn.pos.x and existing.spawnY == spawn.pos.y then
        return existing
    end

    local room = self.room
    local sx, sy = spawn.pos.x, spawn.pos.y
    local sources = room:find(screeps.FIND.SOURCES) or {} ---@type Source[]

    ---@type RaiderPlan
    local plan = {
        spawnX = sx,
        spawnY = sy,
        containers = {},
        towers = {},
        storage = nil,
        extensions = {},
        ramparts = {},
        roads = {},
    }

    ---@type table<string, boolean>
    local reserved = {}
    reserved[RaiderPlanner.tileKey(sx, sy)] = true

    -- 1. Source containers (closest walkable adjacent tile to spawn).
    for _, src in ipairs(sources) do
        if src.pos then
            local cx, cy = self:findSourceContainerTile(src.pos, spawn.pos)
            if cx and cy then
                table.insert(plan.containers, { x = cx, y = cy })
                reserved[RaiderPlanner.tileKey(cx, cy)] = true
            end
        end
    end

    -- 2. Tower positions: tight ring around spawn (distance 2, cardinal/diagonal).
    local towerCandidates <const> = {
        { sx, sy - 2 }, { sx + 2, sy }, { sx, sy + 2 }, { sx - 2, sy },
        { sx + 2, sy - 2 }, { sx - 2, sy + 2 }, { sx - 2, sy - 2 }, { sx + 2, sy + 2 },
    }
    for _, pt in ipairs(towerCandidates) do
        local tx, ty = pt[1], pt[2]
        local k = RaiderPlanner.tileKey(tx, ty)
        if self:isWalkable(tx, ty) and not reserved[k] then
            table.insert(plan.towers, { x = tx, y = ty })
            reserved[k] = true
            if #plan.towers >= 6 then
                break
            end
        end
    end

    -- 3. Storage position: close to spawn (radius 2-3), off-checkerboard or clear tile.
    local storageCandidates <const> = {
        { sx + 1, sy + 2 }, { sx - 1, sy + 2 }, { sx + 2, sy + 1 }, { sx - 2, sy + 1 },
        { sx + 1, sy - 2 }, { sx - 1, sy - 2 }, { sx + 2, sy - 1 }, { sx - 2, sy - 1 },
        { sx, sy + 3 }, { sx, sy - 3 }, { sx + 3, sy }, { sx - 3, sy },
    }
    for _, pt in ipairs(storageCandidates) do
        local tx, ty = pt[1], pt[2]
        local k = RaiderPlanner.tileKey(tx, ty)
        if self:isWalkable(tx, ty) and not reserved[k] then
            plan.storage = { x = tx, y = ty }
            reserved[k] = true
            break
        end
    end

    -- 4. Compact checkerboard extensions around spawn (radius 2..6).
    -- Checkerboard parity: (dx + dy) % 2 == 0 guarantees walkable alleys for fast haulers.
    for radius = 2, 6 do
        for dx = -radius, radius do
            for dy = -radius, radius do
                if math.max(math.abs(dx), math.abs(dy)) == radius then
                    if ((dx + dy) % 2) == 0 then
                        local tx, ty = sx + dx, sy + dy
                        local k = RaiderPlanner.tileKey(tx, ty)
                        if self:isWalkable(tx, ty) and not reserved[k] then
                            -- Avoid blocking controller or source immediate tiles.
                            local tooClose = false
                            if room.controller and room.controller.pos then
                                if math.max(math.abs(tx - room.controller.pos.x), math.abs(ty - room.controller.pos.y)) <= 1 then
                                    tooClose = true
                                end
                            end
                            if not tooClose then
                                for _, src in ipairs(sources) do
                                    if src.pos and math.max(math.abs(tx - src.pos.x), math.abs(ty - src.pos.y)) <= 1 then
                                        tooClose = true
                                        break
                                    end
                                end
                            end
                            if not tooClose then
                                table.insert(plan.extensions, { x = tx, y = ty })
                                reserved[k] = true
                            end
                        end
                    end
                end
            end
        end
    end

    -- 5. Roads from spawn to sources and controller.
    ---@type table<string, boolean>
    local roadSeen = {}
    for _, src in ipairs(sources) do
        if src.pos then
            self:addRoadPoints(plan, roadSeen, sx, sy, self:computeRoadPath(spawn.pos, src.pos, 1))
        end
    end
    if room.controller and room.controller.pos then
        self:addRoadPoints(plan, roadSeen, sx, sy, self:computeRoadPath(spawn.pos, room.controller.pos, 2))
    end

    -- 6. Ramparts on primary spawn and planned towers.
    table.insert(plan.ramparts, { x = sx, y = sy })
    for _, pt in ipairs(plan.towers) do
        table.insert(plan.ramparts, { x = pt.x, y = pt.y })
    end

    rmem.raiderPlan = plan
    return plan
end

---Place construction sites for the room according to priority order and RCL limits.
---@param rcl integer|nil
function RaiderPlanner:plan(rcl)
    local room = self.room
    if not room or not room.createConstructionSite then
        return
    end
    local spawn = self.spawn
    if not spawn or not spawn.pos then
        return
    end

    local level = rcl or (room.controller and room.controller.level) or 1
    local plan = self:ensurePlan()
    if not plan then
        return
    end

    self:buildOccupancyMap()
    if self.activeSites >= MAX_ACTIVE_SITES then
        return
    end

    -- Priority 1: Tower at RCL 3+ (critical for aggressive colony defense).
    local towerLimit = TOWER_LIMITS[level] or 0
    if towerLimit > 0 and (self.counts[screeps.STRUCTURE.TOWER] or 0) < towerLimit then
        for _, pt in ipairs(plan.towers) do
            if (self.counts[screeps.STRUCTURE.TOWER] or 0) >= towerLimit or self.activeSites >= MAX_ACTIVE_SITES then
                break
            end
            self:tryPlace(pt.x, pt.y, screeps.STRUCTURE.TOWER, true)
        end
    end

    -- Priority 2: Checkerboard extensions at RCL 2+ (rapid energy capacity ramp to 550 / 800+).
    local extLimit = EXTENSION_LIMITS[level] or 0
    if extLimit > 0 and (self.counts[screeps.STRUCTURE.EXTENSION] or 0) < extLimit then
        for _, pt in ipairs(plan.extensions) do
            if (self.counts[screeps.STRUCTURE.EXTENSION] or 0) >= extLimit or self.activeSites >= MAX_ACTIVE_SITES then
                break
            end
            self:tryPlace(pt.x, pt.y, screeps.STRUCTURE.EXTENSION, true)
        end
    end

    -- Priority 3: Source containers (up to 2, built early for fast hauler pipeline).
    if (self.counts[screeps.STRUCTURE.CONTAINER] or 0) < #plan.containers then
        for _, pt in ipairs(plan.containers) do
            if self.activeSites >= MAX_ACTIVE_SITES then
                break
            end
            self:tryPlace(pt.x, pt.y, screeps.STRUCTURE.CONTAINER, true)
        end
    end

    -- Priority 4: Storage at RCL 4+.
    if level >= 4 and plan.storage and (self.counts[screeps.STRUCTURE.STORAGE] or 0) < 1 then
        self:tryPlace(plan.storage.x, plan.storage.y, screeps.STRUCTURE.STORAGE, true)
    end

    -- Priority 5: Ramparts on primary spawn and built towers at RCL 3+.
    if level >= 3 and self.activeSites < MAX_ACTIVE_SITES then
        self:tryPlace(spawn.pos.x, spawn.pos.y, screeps.STRUCTURE.RAMPART, true)
        local myStructs = room:find(screeps.FIND.MY_STRUCTURES) or {} ---@type Structure[]
        for _, s in ipairs(myStructs) do
            if self.activeSites >= MAX_ACTIVE_SITES then
                break
            end
            if s.structureType == screeps.STRUCTURE.TOWER and s.pos then
                self:tryPlace(s.pos.x, s.pos.y, screeps.STRUCTURE.RAMPART, true)
            end
        end
    end

    -- Priority 6: Roads from spawn to sources and controller (starting RCL 2 so RCL 1 stays lean).
    if level >= 2 and self.activeSites < MAX_ACTIVE_SITES then
        for _, pt in ipairs(plan.roads) do
            if self.activeSites >= MAX_ACTIVE_SITES then
                break
            end
            self:tryPlace(pt.x, pt.y, screeps.STRUCTURE.ROAD, true)
        end
    end
end

---Draw translucent crimson/amber/rose blueprint overlays for unbuilt planned slots from cached room.memory.raiderPlan.
function RaiderPlanner:drawVisuals()
    local room = self.room
    if not room or not room.visual then
        return
    end
    local plan = self.memory and self.memory.raiderPlan ---@type RaiderPlan|nil
    if not plan then
        return
    end

    local vis = room.visual
    ---@type table<integer, boolean>
    local solid = {}
    ---@type table<integer, boolean>
    local hasRoad = {}
    ---@type table<integer, boolean>
    local hasRampart = {}

    local structs = room:find(screeps.FIND.STRUCTURES) or {} ---@type Structure[]
    for _, s in ipairs(structs) do
        RaiderPlanner.markOccupied(solid, hasRoad, hasRampart, s.pos, s.structureType)
    end
    local sites = room:find(screeps.FIND.CONSTRUCTION_SITES) or {} ---@type ConstructionSite[]
    for _, s in ipairs(sites) do
        RaiderPlanner.markOccupied(solid, hasRoad, hasRampart, s.pos, s.structureType)
    end

    -- Planned roads: faint dot.
    if plan.roads and vis.circle then
        local roadStyle <const> = { radius = 0.14, fill = "#90a4ae", opacity = 0.28 }
        for _, pt in ipairs(plan.roads) do
            local k = RaiderPlanner.fastKey(pt.x, pt.y)
            if not hasRoad[k] and not solid[k] then
                vis:circle(pt.x, pt.y, roadStyle)
            end
        end
    end

    -- Planned ramparts: faint rose circle.
    if vis.circle then
        local rampartStyle <const> = { radius = 0.46, fill = "transparent", stroke = "#ef9a9a", opacity = 0.3 }
        local ramparts = plan.ramparts
        if not ramparts then
            ramparts = {}
            if plan.spawnX and plan.spawnY then
                table.insert(ramparts, { x = plan.spawnX, y = plan.spawnY })
            end
            if plan.towers then
                for _, pt in ipairs(plan.towers) do
                    table.insert(ramparts, pt)
                end
            end
        end
        for _, pt in ipairs(ramparts) do
            local k = RaiderPlanner.fastKey(pt.x, pt.y)
            if not hasRampart[k] then
                vis:circle(pt.x, pt.y, rampartStyle)
            end
        end
    end

    -- Planned extensions: translucent coral-orange circle.
    if plan.extensions and vis.circle then
        local extStyle <const> = { radius = 0.34, fill = "transparent", stroke = "#ff8a65", opacity = 0.45 }
        for _, pt in ipairs(plan.extensions) do
            local k = RaiderPlanner.fastKey(pt.x, pt.y)
            if not solid[k] then
                vis:circle(pt.x, pt.y, extStyle)
            end
        end
    end

    -- Planned containers & storage: translucent amber rect.
    if plan.containers then
        local contStyle <const> = { fill = "transparent", stroke = "#ffab40", opacity = 0.45 }
        for _, pt in ipairs(plan.containers) do
            local k = RaiderPlanner.fastKey(pt.x, pt.y)
            if not solid[k] then
                RaiderPlanner.drawRectOrCircle(vis, pt.x, pt.y, 0.3, contStyle)
            end
        end
    end
    if plan.storage then
        local k = RaiderPlanner.fastKey(plan.storage.x, plan.storage.y)
        if not solid[k] then
            RaiderPlanner.drawRectOrCircle(vis, plan.storage.x, plan.storage.y, 0.36, {
                fill = "transparent",
                stroke = "#ffab40",
                opacity = 0.5,
            })
        end
    end

    -- Planned towers: translucent crimson circle.
    if plan.towers and vis.circle then
        local towerStyle <const> = { radius = 0.42, fill = "transparent", stroke = "#ff5252", opacity = 0.55 }
        for _, pt in ipairs(plan.towers) do
            local k = RaiderPlanner.fastKey(pt.x, pt.y)
            if not solid[k] then
                vis:circle(pt.x, pt.y, towerStyle)
            end
        end
    end
end

---Static convenience entry point to plan a room for a given RCL.
---@param room Room
---@param rcl integer|nil
function RaiderPlanner.planRoom(room, rcl)
    RaiderPlanner.new(room):plan(rcl)
end

---Static convenience entry point to draw blueprint visuals for a room.
---@param room Room
function RaiderPlanner.drawPlanVisuals(room)
    RaiderPlanner.new(room):drawVisuals()
end

return RaiderPlanner
