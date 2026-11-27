-- planner.lua: Compact, aggressive base layout planner for the Raider colony.
-- Places source containers, fast roads, tight checkerboard extensions around spawn,
-- defensive tower(s) + ramparts at RCL 3+, and storage at RCL 4+.
-- Idiomatic Lua 5.4 (arnodel/golua) with Lunar/LuaLS annotations.

---@class RaiderPlanner
local M = {}

local MAX_ACTIVE_SITES = 6

local EXTENSION_LIMITS = {
    [1] = 0,
    [2] = 5,
    [3] = 10,
    [4] = 20,
    [5] = 30,
    [6] = 40,
    [7] = 50,
    [8] = 60,
}

local TOWER_LIMITS = {
    [1] = 0,
    [2] = 0,
    [3] = 1,
    [4] = 1,
    [5] = 2,
    [6] = 2,
    [7] = 3,
    [8] = 6,
}

local function tileKey(x, y)
    return tostring(x) .. ":" .. tostring(y)
end

local function fastKey(x, y)
    return x * 50 + y
end

local function getRoomMemory(room)
    if not room then
        return nil
    end
    if Memory and room.name then
        if not Memory.rooms then
            Memory.rooms = {}
        end
        if not Memory.rooms[room.name] then
            Memory.rooms[room.name] = room.memory or {}
        end
        return Memory.rooms[room.name]
    end
    return room.memory
end

local function isWalkable(terrain, x, y)
    if x < 2 or x > 47 or y < 2 or y > 47 then
        return false
    end
    if not terrain or not terrain.get then
        return true
    end
    local t = terrain:get(x, y)
    return t ~= (TERRAIN_MASK_WALL or 1)
end

function M.getPrimarySpawn(room)
    if not room or not room.find then
        return nil
    end
    local spawns = room:find(FIND_MY_SPAWNS)
    if spawns and #spawns > 0 then
        return spawns[1]
    end
    return nil
end

function M.getSourceContainers(room)
    local containers = {}
    if not room or not room.find then
        return containers
    end
    local sources = room:find(FIND_SOURCES) or {}
    local structs = room:find(FIND_STRUCTURES) or {}
    for _, s in ipairs(structs) do
        if s.structureType == STRUCTURE_CONTAINER and s.pos then
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

local function buildOccupancyMap(room)
    local occ = {}
    local counts = {}

    local structs = room:find(FIND_STRUCTURES) or {}
    for _, s in ipairs(structs) do
        if s.pos then
            local k = tileKey(s.pos.x, s.pos.y)
            if not occ[k] then
                occ[k] = {}
            end
            occ[k][s.structureType] = true
            counts[s.structureType] = (counts[s.structureType] or 0) + 1
        end
    end

    local sites = room:find(FIND_CONSTRUCTION_SITES) or {}
    local activeSites = 0
    for _, site in ipairs(sites) do
        activeSites = activeSites + 1
        if site.pos then
            local k = tileKey(site.pos.x, site.pos.y)
            if not occ[k] then
                occ[k] = {}
            end
            occ[k][site.structureType] = true
            counts[site.structureType] = (counts[site.structureType] or 0) + 1
        end
    end

    return occ, counts, activeSites
end

local function hasBlockingStructure(occ, x, y, allowRoad)
    local cell = occ[tileKey(x, y)]
    if not cell then
        return false
    end
    for stype, _ in pairs(cell) do
        if stype == STRUCTURE_RAMPART then
            -- Ramparts can coexist with other structures.
        elseif allowRoad and stype == STRUCTURE_ROAD then
            -- Roads can share a tile with containers if needed.
        else
            return true
        end
    end
    return false
end

local function hasStructureType(occ, x, y, stype)
    local cell = occ[tileKey(x, y)]
    return cell ~= nil and cell[stype] == true
end

local function tryPlace(room, occ, counts, activeSites, x, y, stype, allowRoad)
    if activeSites >= MAX_ACTIVE_SITES then
        return activeSites, false
    end
    if x < 2 or x > 47 or y < 2 or y > 47 then
        return activeSites, false
    end
    if hasStructureType(occ, x, y, stype) then
        return activeSites, false
    end
    if stype ~= STRUCTURE_RAMPART and stype ~= STRUCTURE_ROAD then
        if hasBlockingStructure(occ, x, y, allowRoad) then
            return activeSites, false
        end
    elseif stype == STRUCTURE_ROAD then
        if hasBlockingStructure(occ, x, y, true) then
            return activeSites, false
        end
    end

    local res = room:createConstructionSite(x, y, stype)
    if res == OK then
        local k = tileKey(x, y)
        if not occ[k] then
            occ[k] = {}
        end
        occ[k][stype] = true
        counts[stype] = (counts[stype] or 0) + 1
        return activeSites + 1, true
    end
    return activeSites, false
end

-- Find the best walkable tile adjacent to a source, closest to spawn.
local function findSourceContainerTile(terrain, srcPos, spawnPos)
    local bestX, bestY = nil, nil
    local bestDist = 999
    for dx = -1, 1 do
        for dy = -1, 1 do
            if not (dx == 0 and dy == 0) then
                local tx = srcPos.x + dx
                local ty = srcPos.y + dy
                if isWalkable(terrain, tx, ty) then
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

-- Generate a simple road path from origin to target using PathFinder (or direct stepping fallback).
local function computeRoadPath(room, terrain, fromPos, toPos, range)
    local path = {}
    if PathFinder and PathFinder.search and fromPos and toPos then
        local res = PathFinder.search(fromPos, { pos = toPos, range = range or 1 }, {
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
        if isWalkable(terrain, nx, ny) then
            cx, cy = nx, ny
            table.insert(path, { x = cx, y = cy })
        elseif dx ~= 0 and isWalkable(terrain, cx + dx, cy) then
            cx = cx + dx
            table.insert(path, { x = cx, y = cy })
        elseif dy ~= 0 and isWalkable(terrain, cx, cy + dy) then
            cy = cy + dy
            table.insert(path, { x = cx, y = cy })
        else
            break
        end
    end
    return path
end

-- Build and cache the compact checkerboard layout in room.memory.raiderPlan.
local function ensurePlan(room, spawn)
    local rmem = getRoomMemory(room)
    if not rmem then
        return nil
    end
    local plan = rmem.raiderPlan
    if plan and plan.spawnX == spawn.pos.x and plan.spawnY == spawn.pos.y then
        return plan
    end

    local terrain = room:getTerrain()
    local sx, sy = spawn.pos.x, spawn.pos.y
    local sources = room:find(FIND_SOURCES) or {}

    plan = {
        spawnX = sx,
        spawnY = sy,
        containers = {},
        towers = {},
        storage = nil,
        extensions = {},
        ramparts = {},
        roads = {},
    }

    local reserved = {}
    reserved[tileKey(sx, sy)] = true

    -- 1. Source containers (closest walkable adjacent tile to spawn).
    for _, src in ipairs(sources) do
        if src.pos then
            local cx, cy = findSourceContainerTile(terrain, src.pos, spawn.pos)
            if cx and cy then
                table.insert(plan.containers, { x = cx, y = cy })
                reserved[tileKey(cx, cy)] = true
            end
        end
    end

    -- 2. Tower positions: tight ring around spawn (distance 2, cardinal/diagonal).
    local towerCandidates = {
        { sx, sy - 2 }, { sx + 2, sy }, { sx, sy + 2 }, { sx - 2, sy },
        { sx + 2, sy - 2 }, { sx - 2, sy + 2 }, { sx - 2, sy - 2 }, { sx + 2, sy + 2 },
    }
    for _, pt in ipairs(towerCandidates) do
        local tx, ty = pt[1], pt[2]
        if isWalkable(terrain, tx, ty) and not reserved[tileKey(tx, ty)] then
            table.insert(plan.towers, { x = tx, y = ty })
            reserved[tileKey(tx, ty)] = true
            if #plan.towers >= 6 then
                break
            end
        end
    end

    -- 3. Storage position: close to spawn (radius 2-3), off-checkerboard or clear tile.
    local storageCandidates = {
        { sx + 1, sy + 2 }, { sx - 1, sy + 2 }, { sx + 2, sy + 1 }, { sx - 2, sy + 1 },
        { sx + 1, sy - 2 }, { sx - 1, sy - 2 }, { sx + 2, sy - 1 }, { sx - 2, sy - 1 },
        { sx, sy + 3 }, { sx, sy - 3 }, { sx + 3, sy }, { sx - 3, sy },
    }
    for _, pt in ipairs(storageCandidates) do
        local tx, ty = pt[1], pt[2]
        if isWalkable(terrain, tx, ty) and not reserved[tileKey(tx, ty)] then
            plan.storage = { x = tx, y = ty }
            reserved[tileKey(tx, ty)] = true
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
                        local k = tileKey(tx, ty)
                        if isWalkable(terrain, tx, ty) and not reserved[k] then
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
    local roadSeen = {}
    local function addRoadPoints(pts)
        for _, p in ipairs(pts) do
            local k = tileKey(p.x, p.y)
            if not roadSeen[k] and not (p.x == sx and p.y == sy) then
                roadSeen[k] = true
                table.insert(plan.roads, { x = p.x, y = p.y })
            end
        end
    end

    for _, src in ipairs(sources) do
        if src.pos then
            addRoadPoints(computeRoadPath(room, terrain, spawn.pos, src.pos, 1))
        end
    end
    if room.controller and room.controller.pos then
        addRoadPoints(computeRoadPath(room, terrain, spawn.pos, room.controller.pos, 2))
    end

    -- 6. Ramparts on primary spawn and planned towers.
    table.insert(plan.ramparts, { x = sx, y = sy })
    for _, pt in ipairs(plan.towers) do
        table.insert(plan.ramparts, { x = pt.x, y = pt.y })
    end

    rmem.raiderPlan = plan
    return plan
end

function M.planRoom(room, rcl)
    if not room or not room.createConstructionSite then
        return
    end
    local spawn = M.getPrimarySpawn(room)
    if not spawn or not spawn.pos then
        return
    end

    rcl = rcl or (room.controller and room.controller.level) or 1
    local plan = ensurePlan(room, spawn)
    if not plan then
        return
    end

    local occ, counts, activeSites = buildOccupancyMap(room)
    if activeSites >= MAX_ACTIVE_SITES then
        return
    end

    -- Priority 1: Tower at RCL 3+ (critical for aggressive colony defense).
    local towerLimit = TOWER_LIMITS[rcl] or 0
    if towerLimit > 0 and (counts[STRUCTURE_TOWER] or 0) < towerLimit then
        for _, pt in ipairs(plan.towers) do
            if (counts[STRUCTURE_TOWER] or 0) >= towerLimit or activeSites >= MAX_ACTIVE_SITES then
                break
            end
            activeSites = tryPlace(room, occ, counts, activeSites, pt.x, pt.y, STRUCTURE_TOWER, true)
        end
    end

    -- Priority 2: Checkerboard extensions at RCL 2+ (rapid energy capacity ramp to 550 / 800+).
    local extLimit = EXTENSION_LIMITS[rcl] or 0
    if extLimit > 0 and (counts[STRUCTURE_EXTENSION] or 0) < extLimit then
        for _, pt in ipairs(plan.extensions) do
            if (counts[STRUCTURE_EXTENSION] or 0) >= extLimit or activeSites >= MAX_ACTIVE_SITES then
                break
            end
            activeSites = tryPlace(room, occ, counts, activeSites, pt.x, pt.y, STRUCTURE_EXTENSION, true)
        end
    end

    -- Priority 3: Source containers (up to 2, built early for fast hauler pipeline).
    if (counts[STRUCTURE_CONTAINER] or 0) < #plan.containers then
        for _, pt in ipairs(plan.containers) do
            if activeSites >= MAX_ACTIVE_SITES then
                break
            end
            activeSites = tryPlace(room, occ, counts, activeSites, pt.x, pt.y, STRUCTURE_CONTAINER, true)
        end
    end

    -- Priority 4: Storage at RCL 4+.
    if rcl >= 4 and plan.storage and (counts[STRUCTURE_STORAGE] or 0) < 1 then
        activeSites = tryPlace(room, occ, counts, activeSites, plan.storage.x, plan.storage.y, STRUCTURE_STORAGE, true)
    end

    -- Priority 5: Ramparts on primary spawn and built towers at RCL 3+.
    if rcl >= 3 and activeSites < MAX_ACTIVE_SITES then
        activeSites = tryPlace(room, occ, counts, activeSites, spawn.pos.x, spawn.pos.y, STRUCTURE_RAMPART, true)
        local myStructs = room:find(FIND_MY_STRUCTURES) or {}
        for _, s in ipairs(myStructs) do
            if activeSites >= MAX_ACTIVE_SITES then
                break
            end
            if s.structureType == STRUCTURE_TOWER and s.pos then
                activeSites = tryPlace(room, occ, counts, activeSites, s.pos.x, s.pos.y, STRUCTURE_RAMPART, true)
            end
        end
    end

    -- Priority 6: Roads from spawn to sources and controller (starting RCL 2 so RCL 1 stays lean).
    if rcl >= 2 and activeSites < MAX_ACTIVE_SITES then
        for _, pt in ipairs(plan.roads) do
            if activeSites >= MAX_ACTIVE_SITES then
                break
            end
            activeSites = tryPlace(room, occ, counts, activeSites, pt.x, pt.y, STRUCTURE_ROAD, true)
        end
    end
end

-- Draw translucent crimson/amber/rose blueprint overlays for unbuilt planned slots from cached room.memory.raiderPlan.
function M.drawPlanVisuals(room)
    if not room or not room.visual then
        return
    end
    local rmem = getRoomMemory(room)
    local plan = rmem and rmem.raiderPlan
    if not plan then
        return
    end

    local vis = room.visual
    local solid = {}
    local hasRoad = {}
    local hasRampart = {}

    local function markOccupied(pos, stype)
        if not pos or not stype then
            return
        end
        local k = fastKey(pos.x, pos.y)
        if stype == STRUCTURE_ROAD then
            hasRoad[k] = true
        elseif stype == STRUCTURE_RAMPART then
            hasRampart[k] = true
        else
            solid[k] = true
        end
    end

    local structs = room:find(FIND_STRUCTURES) or {}
    for _, s in ipairs(structs) do
        markOccupied(s.pos, s.structureType)
    end
    local sites = room:find(FIND_CONSTRUCTION_SITES) or {}
    for _, s in ipairs(sites) do
        markOccupied(s.pos, s.structureType)
    end

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
    if plan.roads and vis.circle then
        local roadStyle = { radius = 0.14, fill = "#90a4ae", opacity = 0.28 }
        for _, pt in ipairs(plan.roads) do
            local k = fastKey(pt.x, pt.y)
            if not hasRoad[k] and not solid[k] then
                vis:circle(pt.x, pt.y, roadStyle)
            end
        end
    end

    -- Planned ramparts: faint rose circle.
    if vis.circle then
        local rampartStyle = { radius = 0.46, fill = "transparent", stroke = "#ef9a9a", opacity = 0.3 }
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
            local k = fastKey(pt.x, pt.y)
            if not hasRampart[k] then
                vis:circle(pt.x, pt.y, rampartStyle)
            end
        end
    end

    -- Planned extensions: translucent coral-orange circle.
    if plan.extensions and vis.circle then
        local extStyle = { radius = 0.34, fill = "transparent", stroke = "#ff8a65", opacity = 0.45 }
        for _, pt in ipairs(plan.extensions) do
            local k = fastKey(pt.x, pt.y)
            if not solid[k] then
                vis:circle(pt.x, pt.y, extStyle)
            end
        end
    end

    -- Planned containers & storage: translucent amber rect.
    if plan.containers then
        local contStyle = { fill = "transparent", stroke = "#ffab40", opacity = 0.45 }
        for _, pt in ipairs(plan.containers) do
            local k = fastKey(pt.x, pt.y)
            if not solid[k] then
                drawRectOrCircle(pt.x, pt.y, 0.3, contStyle)
            end
        end
    end
    if plan.storage then
        local k = fastKey(plan.storage.x, plan.storage.y)
        if not solid[k] then
            drawRectOrCircle(plan.storage.x, plan.storage.y, 0.36, {
                fill = "transparent",
                stroke = "#ffab40",
                opacity = 0.5,
            })
        end
    end

    -- Planned towers: translucent crimson circle.
    if plan.towers and vis.circle then
        local towerStyle = { radius = 0.42, fill = "transparent", stroke = "#ff5252", opacity = 0.55 }
        for _, pt in ipairs(plan.towers) do
            local k = fastKey(pt.x, pt.y)
            if not solid[k] then
                vis:circle(pt.x, pt.y, towerStyle)
            end
        end
    end
end

return M
