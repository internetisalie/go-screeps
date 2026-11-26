-- planner.lua: Full RCL 1-8 automated room planner for Screeps.
-- Compatible with standard Lua 5.1 / gopher-lua.

local M = {}

local STRUCT_STORAGE = STRUCTURE_STORAGE or "storage"
local STRUCT_LINK = STRUCTURE_LINK or "link"
local STRUCT_RAMPART = STRUCTURE_RAMPART or "rampart"

local MAX_EXT_SITES_PER_RUN = 5
local MAX_ROAD_SITES_PER_RUN = 6
local MAX_RAMPART_SITES_PER_RUN = 2

M.EXTENSION_LIMITS = {
    [1] = 0,
    [2] = 5,
    [3] = 10,
    [4] = 20,
    [5] = 30,
    [6] = 40,
    [7] = 50,
    [8] = 60,
}

M.TOWER_LIMITS = {
    [1] = 0,
    [2] = 0,
    [3] = 1,
    [4] = 1,
    [5] = 2,
    [6] = 2,
    [7] = 3,
    [8] = 6,
}

M.LINK_LIMITS = {
    [1] = 0,
    [2] = 0,
    [3] = 0,
    [4] = 0,
    [5] = 2,
    [6] = 3,
    [7] = 4,
    [8] = 6,
}

local function tileKey(x, y)
    return x * 50 + y
end

local function inBuildBounds(x, y)
    return x >= 2 and x <= 47 and y >= 2 and y <= 47
end

local function chebyshev(x1, y1, x2, y2)
    return math.max(math.abs(x1 - x2), math.abs(y1 - y2))
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

-- Find the primary spawn for a room (preferring Spawn1 if present).
function M.getPrimarySpawn(room)
    local spawns = room:find(FIND_MY_SPAWNS)
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

-- Build occupancy maps and structure/site counts for the room.
local function scanRoomState(room)
    local solid = {}
    local hasRoad = {}
    local hasRampart = {}
    local counts = {}
    local containers = {}
    local links = {}
    local keyStructures = {}
    local storagePos = nil

    local function record(pos, stype)
        if not pos or not stype then
            return
        end
        local key = tileKey(pos.x, pos.y)
        counts[stype] = (counts[stype] or 0) + 1

        if stype == STRUCTURE_ROAD then
            hasRoad[key] = true
        elseif stype == STRUCT_RAMPART then
            hasRampart[key] = true
        else
            solid[key] = stype
        end

        if stype == STRUCTURE_CONTAINER then
            table.insert(containers, pos)
        elseif stype == STRUCT_LINK then
            table.insert(links, pos)
        elseif stype == STRUCT_STORAGE then
            storagePos = pos
            table.insert(keyStructures, pos)
        elseif stype == STRUCTURE_SPAWN or stype == STRUCTURE_TOWER then
            table.insert(keyStructures, pos)
        end
    end

    local structs = room:find(FIND_STRUCTURES) or {}
    for _, s in ipairs(structs) do
        record(s.pos, s.structureType)
    end

    local sites = room:find(FIND_CONSTRUCTION_SITES) or {}
    for _, s in ipairs(sites) do
        record(s.pos, s.structureType)
    end

    return {
        solid = solid,
        hasRoad = hasRoad,
        hasRampart = hasRampart,
        counts = counts,
        containers = containers,
        links = links,
        keyStructures = keyStructures,
        storagePos = storagePos,
    }
end

local function hasPosNear(posList, targetPos, maxRange)
    for _, p in ipairs(posList) do
        if chebyshev(p.x, p.y, targetPos.x, targetPos.y) <= maxRange then
            return true
        end
    end
    return false
end

-- Find the best open walkable tile within [minRange, maxRange] of targetPos, closest to towardsPos.
local function findBestTileNear(terrain, state, targetPos, towardsPos, minRange, maxRange)
    local bestX, bestY = nil, nil
    local bestScore = 999999

    for dx = -maxRange, maxRange do
        for dy = -maxRange, maxRange do
            local dist = math.max(math.abs(dx), math.abs(dy))
            if dist >= minRange and dist <= maxRange then
                local x = targetPos.x + dx
                local y = targetPos.y + dy
                if inBuildBounds(x, y) then
                    local key = tileKey(x, y)
                    if not state.solid[key] and terrain:get(x, y) ~= 1 then
                        local score = dist * 10
                        if towardsPos then
                            score = score + chebyshev(x, y, towardsPos.x, towardsPos.y)
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

-- Place up to `needed` structures in a checkerboard pattern around `origin`.
local function placeCheckerboardNear(room, terrain, state, origin, structureType, needed, minRadius, maxRadius, checkerboardOnly)
    local placed = 0
    if needed <= 0 then
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
                        if inBuildBounds(x, y) then
                            local key = tileKey(x, y)
                            if not state.solid[key] and terrain:get(x, y) ~= 1 then
                                if room:createConstructionSite(x, y, structureType) == OK then
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

-- Shortest road path from startPos to adjacent of endPos (uses native PathFinder.search when available).
local function findRoadPath(terrain, startPos, endPos)
    if PathFinder and PathFinder.search and startPos and endPos then
        local res = PathFinder.search(startPos, { pos = endPos, range = 1 }, {
            plainCost = 2,
            swampCost = 4,
            maxOps = 2000,
        })
        if res and res.path and #res.path > 0 then
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

    local sx, sy = startPos.x, startPos.y
    local tx, ty = endPos.x, endPos.y

    local startKey = tileKey(sx, sy)
    local queue = { startKey }
    local head = 1
    local parent = {}
    parent[startKey] = -1

    local goalKey = nil
    local dirs = {
        { 0, -1 }, { 1, 0 }, { 0, 1 }, { -1, 0 },
        { 1, -1 }, { 1, 1 }, { -1, 1 }, { -1, -1 },
    }

    while head <= #queue do
        local curr = queue[head]
        head = head + 1

        local cx = math.floor(curr / 50)
        local cy = curr - cx * 50

        if chebyshev(cx, cy, tx, ty) <= 1 then
            goalKey = curr
            break
        end

        for _, d in ipairs(dirs) do
            local nx = cx + d[1]
            local ny = cy + d[2]
            if nx >= 1 and nx <= 48 and ny >= 1 and ny <= 48 then
                local nk = tileKey(nx, ny)
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

    local rev = {}
    local k = goalKey
    while k ~= -1 and k ~= startKey do
        local px = math.floor(k / 50)
        local py = k - px * 50
        table.insert(rev, { x = px, y = py })
        k = parent[k]
    end

    local path = {}
    for i = #rev, 1, -1 do
        table.insert(path, rev[i])
    end
    return path
end

-- Plan source containers and controller container (RCL >= 2).
local function planContainers(room, terrain, state, spawn, sources)
    for _, src in ipairs(sources) do
        if src.pos and not hasPosNear(state.containers, src.pos, 1) then
            local cx, cy = findBestTileNear(terrain, state, src.pos, spawn.pos, 1, 1)
            if cx and cy then
                if room:createConstructionSite(cx, cy, STRUCTURE_CONTAINER) == OK then
                    local key = tileKey(cx, cy)
                    state.solid[key] = STRUCTURE_CONTAINER
                    table.insert(state.containers, { x = cx, y = cy })
                end
            end
        end
    end

    if room.controller and room.controller.pos then
        if not hasPosNear(state.containers, room.controller.pos, 2) then
            local cx, cy = findBestTileNear(terrain, state, room.controller.pos, spawn.pos, 1, 2)
            if cx and cy then
                if room:createConstructionSite(cx, cy, STRUCTURE_CONTAINER) == OK then
                    local key = tileKey(cx, cy)
                    state.solid[key] = STRUCTURE_CONTAINER
                    table.insert(state.containers, { x = cx, y = cy })
                end
            end
        end
    end
end

-- Plan Storage at RCL >= 4 near Spawn1.
local function planStorage(room, terrain, state, spawn)
    if (state.counts[STRUCT_STORAGE] or 0) >= 1 then
        return
    end
    local towards = (room.controller and room.controller.pos) or nil
    local sx, sy = findBestTileNear(terrain, state, spawn.pos, towards, 2, 4)
    if sx and sy then
        if room:createConstructionSite(sx, sy, STRUCT_STORAGE) == OK then
            local key = tileKey(sx, sy)
            state.solid[key] = STRUCT_STORAGE
            state.counts[STRUCT_STORAGE] = 1
            state.storagePos = { x = sx, y = sy }
            table.insert(state.keyStructures, state.storagePos)
        end
    end
end

-- Plan Links at RCL >= 5 (hub link near storage/spawn, controller link, source links).
local function planLinks(room, terrain, state, spawn, sources, rcl)
    local limit = M.LINK_LIMITS[rcl] or 0
    local current = state.counts[STRUCT_LINK] or 0
    if current >= limit then
        return
    end

    local function placeLinkNear(anchorPos, towardsPos, maxRange)
        if current >= limit or not anchorPos then
            return
        end
        if hasPosNear(state.links, anchorPos, maxRange) then
            return
        end
        local lx, ly = findBestTileNear(terrain, state, anchorPos, towardsPos, 1, maxRange)
        if lx and ly then
            if room:createConstructionSite(lx, ly, STRUCT_LINK) == OK then
                local key = tileKey(lx, ly)
                state.solid[key] = STRUCT_LINK
                current = current + 1
                state.counts[STRUCT_LINK] = current
                table.insert(state.links, { x = lx, y = ly })
            end
        end
    end

    -- 1. Hub link near storage (or spawn).
    local hubAnchor = state.storagePos or spawn.pos
    placeLinkNear(hubAnchor, spawn.pos, 2)

    -- 2. Controller link.
    if room.controller and room.controller.pos then
        placeLinkNear(room.controller.pos, hubAnchor, 2)
    end

    -- 3. Source links.
    for _, src in ipairs(sources) do
        if src.pos then
            placeLinkNear(src.pos, hubAnchor, 2)
        end
    end
end

-- Plan Ramparts protecting Spawn, Storage, and Towers (RCL >= 3).
local function planRamparts(room, state)
    local placed = 0
    for _, pos in ipairs(state.keyStructures) do
        if placed >= MAX_RAMPART_SITES_PER_RUN then
            break
        end
        local key = tileKey(pos.x, pos.y)
        if not state.hasRampart[key] then
            if room:createConstructionSite(pos.x, pos.y, STRUCT_RAMPART) == OK then
                state.hasRampart[key] = true
                placed = placed + 1
            end
        end
    end
end

-- Plan BFS-routed roads connecting Spawn to Sources, Controller, and Storage.
local function planRoads(room, terrain, state, spawn, sources)
    local targets = {}
    for _, src in ipairs(sources) do
        if src.pos then
            table.insert(targets, src.pos)
        end
    end
    if room.controller and room.controller.pos then
        table.insert(targets, room.controller.pos)
    end
    if state.storagePos then
        table.insert(targets, state.storagePos)
    end

    local roadsPlaced = 0
    for _, targetPos in ipairs(targets) do
        if roadsPlaced >= MAX_ROAD_SITES_PER_RUN then
            break
        end
        local path = findRoadPath(terrain, spawn.pos, targetPos)
        for _, step in ipairs(path) do
            if roadsPlaced >= MAX_ROAD_SITES_PER_RUN then
                break
            end
            local key = tileKey(step.x, step.y)
            if not state.hasRoad[key] and not state.solid[key] and terrain:get(step.x, step.y) ~= 1 then
                if room:createConstructionSite(step.x, step.y, STRUCTURE_ROAD) == OK then
                    state.hasRoad[key] = true
                    roadsPlaced = roadsPlaced + 1
                end
            end
        end
    end
end

-- Build and cache the full RCL 1-8 room blueprint in room.memory.plan.
local function ensurePlan(room, spawn, terrain)
    local rmem = getRoomMemory(room)
    if not rmem then
        return nil
    end
    local existing = rmem.plan
    if existing and existing.spawnX == spawn.pos.x and existing.spawnY == spawn.pos.y and type(existing.extensions) == "table" then
        return existing
    end

    local sx, sy = spawn.pos.x, spawn.pos.y
    local sources = room:find(FIND_SOURCES) or {}
    local ctrlPos = room.controller and room.controller.pos or nil

    local plan = {
        spawnX = sx,
        spawnY = sy,
        containers = {},
        extensions = {},
        towers = {},
        storage = nil,
        links = {},
        ramparts = {},
        roads = {},
        lastTick = Game.time or 0,
        rcl = (room.controller and room.controller.level) or 1,
    }

    local reserved = {}
    reserved[tileKey(sx, sy)] = STRUCTURE_SPAWN
    local vState = { solid = reserved }

    -- 1. Source containers + controller container.
    for _, src in ipairs(sources) do
        if src.pos then
            local cx, cy = findBestTileNear(terrain, vState, src.pos, spawn.pos, 1, 1)
            if cx and cy then
                reserved[tileKey(cx, cy)] = STRUCTURE_CONTAINER
                table.insert(plan.containers, { x = cx, y = cy })
            end
        end
    end
    if ctrlPos then
        local cx, cy = findBestTileNear(terrain, vState, ctrlPos, spawn.pos, 1, 2)
        if cx and cy then
            reserved[tileKey(cx, cy)] = STRUCTURE_CONTAINER
            table.insert(plan.containers, { x = cx, y = cy })
        end
    end

    -- 2. Storage near spawn towards controller.
    local stx, sty = findBestTileNear(terrain, vState, spawn.pos, ctrlPos, 2, 4)
    if stx and sty then
        reserved[tileKey(stx, sty)] = STRUCT_STORAGE
        plan.storage = { x = stx, y = sty }
    end

    -- 3. Up to 6 towers near spawn (checkerboard first, fallback open tiles).
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
                            if inBuildBounds(x, y) then
                                local key = tileKey(x, y)
                                if not reserved[key] and terrain:get(x, y) ~= 1 then
                                    local blocked = false
                                    if avoidAnchors then
                                        if ctrlPos and chebyshev(x, y, ctrlPos.x, ctrlPos.y) <= 1 then
                                            blocked = true
                                        end
                                        if not blocked then
                                            for _, src in ipairs(sources) do
                                                if src.pos and chebyshev(x, y, src.pos.x, src.pos.y) <= 1 then
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
    local function addPlannedLink(anchorPos, towardsPos, maxRange)
        if #plan.links >= 6 or not anchorPos then
            return
        end
        local lx, ly = findBestTileNear(terrain, vState, anchorPos, towardsPos, 1, maxRange)
        if lx and ly then
            reserved[tileKey(lx, ly)] = STRUCT_LINK
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

    -- 5. Up to 60 checkerboard extensions around spawn.
    collectCheckerboard(plan.extensions, 60, 2, 12, true, true)

    -- 6. Ramparts over Spawn, Storage, and planned Towers.
    table.insert(plan.ramparts, { x = sx, y = sy })
    if plan.storage then
        table.insert(plan.ramparts, { x = plan.storage.x, y = plan.storage.y })
    end
    for _, pt in ipairs(plan.towers) do
        table.insert(plan.ramparts, { x = pt.x, y = pt.y })
    end

    -- 7. BFS-routed roads connecting Spawn to Sources, Controller, and Storage.
    local roadSeen = {}
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

    for _, targetPos in ipairs(targets) do
        local path = findRoadPath(terrain, spawn.pos, targetPos)
        for _, step in ipairs(path) do
            local key = tileKey(step.x, step.y)
            if not roadSeen[key] and not reserved[key] and terrain:get(step.x, step.y) ~= 1 then
                roadSeen[key] = true
                table.insert(plan.roads, { x = step.x, y = step.y })
            end
        end
    end

    rmem.plan = plan
    return plan
end

-- Main room planning entry point (call periodically, e.g. every 25 ticks).
function M.planRoom(room, rcl)
    if not room then
        return
    end

    local spawn = M.getPrimarySpawn(room)
    if not spawn or not spawn.pos then
        return
    end

    local terrain = room:getTerrain()
    if not terrain then
        return
    end

    local plan = ensurePlan(room, spawn, terrain)
    if plan then
        plan.lastTick = Game.time or 0
        plan.rcl = rcl
    end

    if rcl < 2 then
        return
    end

    local state = scanRoomState(room)
    local sources = room:find(FIND_SOURCES) or {}

    -- 1. Source and controller containers (RCL >= 2).
    if plan and plan.containers and #plan.containers > 0 then
        for _, pt in ipairs(plan.containers) do
            local key = tileKey(pt.x, pt.y)
            if not state.solid[key] then
                if room:createConstructionSite(pt.x, pt.y, STRUCTURE_CONTAINER) == OK then
                    state.solid[key] = STRUCTURE_CONTAINER
                    table.insert(state.containers, { x = pt.x, y = pt.y })
                end
            end
        end
    else
        planContainers(room, terrain, state, spawn, sources)
    end

    -- 2. Checkerboard extensions scaling RCL 2-8.
    local maxExt = M.EXTENSION_LIMITS[rcl] or 0
    local curExt = state.counts[STRUCTURE_EXTENSION] or 0
    if curExt < maxExt then
        local toPlace = math.min(MAX_EXT_SITES_PER_RUN, maxExt - curExt)
        local placed = 0
        if plan and plan.extensions then
            for _, pt in ipairs(plan.extensions) do
                if placed >= toPlace then
                    break
                end
                local key = tileKey(pt.x, pt.y)
                if not state.solid[key] and terrain:get(pt.x, pt.y) ~= 1 then
                    if room:createConstructionSite(pt.x, pt.y, STRUCTURE_EXTENSION) == OK then
                        state.solid[key] = STRUCTURE_EXTENSION
                        state.counts[STRUCTURE_EXTENSION] = (state.counts[STRUCTURE_EXTENSION] or 0) + 1
                        placed = placed + 1
                    end
                end
            end
        end
        if placed < toPlace then
            placeCheckerboardNear(room, terrain, state, spawn.pos, STRUCTURE_EXTENSION, toPlace - placed, 2, 12, true)
        end
    end

    -- 3. Towers scaling RCL 3-8.
    local maxTowers = M.TOWER_LIMITS[rcl] or 0
    local curTowers = state.counts[STRUCTURE_TOWER] or 0
    if curTowers < maxTowers then
        local needed = maxTowers - curTowers
        local placed = 0
        if plan and plan.towers then
            for _, pt in ipairs(plan.towers) do
                if placed >= needed then
                    break
                end
                local key = tileKey(pt.x, pt.y)
                if not state.solid[key] and terrain:get(pt.x, pt.y) ~= 1 then
                    if room:createConstructionSite(pt.x, pt.y, STRUCTURE_TOWER) == OK then
                        state.solid[key] = STRUCTURE_TOWER
                        state.counts[STRUCTURE_TOWER] = (state.counts[STRUCTURE_TOWER] or 0) + 1
                        table.insert(state.keyStructures, { x = pt.x, y = pt.y })
                        placed = placed + 1
                    end
                end
            end
        end
        if placed < needed then
            local extra = placeCheckerboardNear(room, terrain, state, spawn.pos, STRUCTURE_TOWER, needed - placed, 2, 6, true)
            placed = placed + extra
            if placed < needed then
                placeCheckerboardNear(room, terrain, state, spawn.pos, STRUCTURE_TOWER, needed - placed, 2, 7, false)
            end
        end
    end

    -- 4. Storage at RCL >= 4.
    if rcl >= 4 then
        if plan and plan.storage and (state.counts[STRUCT_STORAGE] or 0) < 1 then
            local key = tileKey(plan.storage.x, plan.storage.y)
            if not state.solid[key] then
                if room:createConstructionSite(plan.storage.x, plan.storage.y, STRUCT_STORAGE) == OK then
                    state.solid[key] = STRUCT_STORAGE
                    state.counts[STRUCT_STORAGE] = 1
                    state.storagePos = { x = plan.storage.x, y = plan.storage.y }
                    table.insert(state.keyStructures, state.storagePos)
                end
            end
        else
            planStorage(room, terrain, state, spawn)
        end
    end

    -- 5. Links at RCL >= 5.
    if rcl >= 5 then
        local linkLimit = M.LINK_LIMITS[rcl] or 0
        local curLinks = state.counts[STRUCT_LINK] or 0
        if plan and plan.links and curLinks < linkLimit then
            for _, pt in ipairs(plan.links) do
                if curLinks >= linkLimit then
                    break
                end
                local key = tileKey(pt.x, pt.y)
                if not state.solid[key] then
                    if room:createConstructionSite(pt.x, pt.y, STRUCT_LINK) == OK then
                        state.solid[key] = STRUCT_LINK
                        curLinks = curLinks + 1
                        state.counts[STRUCT_LINK] = curLinks
                        table.insert(state.links, { x = pt.x, y = pt.y })
                    end
                end
            end
        else
            planLinks(room, terrain, state, spawn, sources, rcl)
        end
    end

    -- 6. Ramparts over key structures at RCL >= 2 (protects Spawn1 from early raids).
    if rcl >= 2 then
        planRamparts(room, state)
    end

    -- 7. BFS-routed roads connecting Spawn, Sources, Controller, Storage.
    if plan and plan.roads and #plan.roads > 0 then
        local roadsPlaced = 0
        for _, step in ipairs(plan.roads) do
            if roadsPlaced >= MAX_ROAD_SITES_PER_RUN then
                break
            end
            local key = tileKey(step.x, step.y)
            if not state.hasRoad[key] and not state.solid[key] and terrain:get(step.x, step.y) ~= 1 then
                if room:createConstructionSite(step.x, step.y, STRUCTURE_ROAD) == OK then
                    state.hasRoad[key] = true
                    roadsPlaced = roadsPlaced + 1
                end
            end
        end
    else
        planRoads(room, terrain, state, spawn, sources)
    end
end

-- Draw translucent blueprint overlays for unbuilt planned slots from cached room.memory.plan.
function M.drawPlanVisuals(room)
    if not room or not room.visual then
        return
    end
    local rmem = getRoomMemory(room)
    local plan = rmem and rmem.plan
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
        local key = tileKey(pos.x, pos.y)
        if stype == STRUCTURE_ROAD then
            hasRoad[key] = true
        elseif stype == STRUCT_RAMPART then
            hasRampart[key] = true
        else
            solid[key] = true
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
    if type(plan.roads) == "table" and vis.circle then
        local roadStyle = { radius = 0.14, fill = "#90a4ae", opacity = 0.28 }
        for _, pt in ipairs(plan.roads) do
            if type(pt) == "table" and pt.x and pt.y then
                local key = tileKey(pt.x, pt.y)
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
                local key = tileKey(pt.x, pt.y)
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
                local key = tileKey(pt.x, pt.y)
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
                local key = tileKey(pt.x, pt.y)
                if not solid[key] then
                    drawRectOrCircle(pt.x, pt.y, 0.3, contStyle)
                end
            end
        end
    end

    -- Planned storage & links: translucent cyan rect.
    local cyanStyle = { fill = "transparent", stroke = "#4fc3f7", opacity = 0.5 }
    if type(plan.storage) == "table" and plan.storage.x and plan.storage.y then
        local key = tileKey(plan.storage.x, plan.storage.y)
        if not solid[key] then
            drawRectOrCircle(plan.storage.x, plan.storage.y, 0.36, cyanStyle)
        end
    end
    if type(plan.links) == "table" then
        for _, pt in ipairs(plan.links) do
            if type(pt) == "table" and pt.x and pt.y then
                local key = tileKey(pt.x, pt.y)
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
                local key = tileKey(pt.x, pt.y)
                if not solid[key] then
                    vis:circle(pt.x, pt.y, towerStyle)
                end
            end
        end
    end
end

return M
