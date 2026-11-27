-- economy.lua: Lean, fast-bootstrapping economy for the Raider colony.
-- Roles: harvester, hauler, builder, upgrader.
-- Idiomatic Lua 5.4 (arnodel/golua) with Lunar/LuaLS annotations.

local screeps <const> = require("screeps")
local ERR <const> = screeps.ERR
local FIND <const> = screeps.FIND
local PART <const> = screeps.PART
local RESOURCE <const> = screeps.RESOURCE
local STRUCTURE <const> = screeps.STRUCTURE

---@class RaiderEconomy
local M = {}

local _cacheTick = -1
local _roomCache = {}

local function getRoomCache(room)
    local t = screeps.Game.time or 0
    if t ~= _cacheTick then
        _cacheTick = t
        _roomCache = {}
    end
    local name = room.name or ""
    local c = _roomCache[name]
    if not c then
        c = {}
        _roomCache[name] = c
    end
    return c
end

local function cachedFind(room, key, findConst)
    local c = getRoomCache(room)
    local v = c[key]
    if v == nil then
        v = room:find(findConst) or {}
        c[key] = v
    end
    return v
end

local SITE_PRIORITY = {
    [STRUCTURE.TOWER or "tower"] = 1,
    [STRUCTURE.EXTENSION or "extension"] = 2,
    [STRUCTURE.CONTAINER or "container"] = 3,
    [STRUCTURE.STORAGE or "storage"] = 4,
    [STRUCTURE.RAMPART or "rampart"] = 5,
    [STRUCTURE.ROAD or "road"] = 6,
}

local function getUsedEnergy(store)
    if not store or not store.getUsedCapacity then
        return 0
    end
    return store:getUsedCapacity(RESOURCE.ENERGY) or 0
end

local function getFreeEnergy(store)
    if not store or not store.getFreeCapacity then
        return 0
    end
    return store:getFreeCapacity(RESOURCE.ENERGY) or 0
end

-- Build a harvester body: fast mobile worker during bootstrap (no hauler),
-- or high-WORK dedicated source miner once haulers are running.
function M.buildHarvesterBody(budget, hasHauler)
    if budget < 200 then
        return nil
    end
    if not hasHauler then
        if budget >= 250 then
            return { PART.WORK, PART.CARRY, PART.MOVE, PART.MOVE } -- 250: 1 tile/tick on plains for fast bootstrap
        end
        return { PART.WORK, PART.CARRY, PART.MOVE } -- 200
    end
    if budget < 300 then
        return { PART.WORK, PART.CARRY, PART.MOVE }
    end

    local body = {}
    local cost = 0

    -- Scale WORK up to 5 (full 10 energy/tick source drain).
    local maxWork = math.min(5, math.floor((budget - 100) / 100))
    if maxWork < 2 then
        maxWork = 2
    end
    for _ = 1, maxWork do
        table.insert(body, PART.WORK)
        cost = cost + 100
    end

    if cost + 50 <= budget then
        table.insert(body, PART.CARRY)
        cost = cost + 50
    end

    local desiredMove = math.max(1, math.min(3, math.ceil(maxWork / 2)))
    for _ = 1, desiredMove do
        if cost + 50 <= budget then
            table.insert(body, PART.MOVE)
            cost = cost + 50
        end
    end

    return body
end

-- Build a fast 1:1 CARRY/MOVE hauler body (never slows down off-road).
function M.buildHaulerBody(budget)
    if budget < 150 then
        if budget >= 100 then
            return { PART.CARRY, PART.MOVE }
        end
        return nil
    end

    local body = {}
    local cost = 0
    while cost + 100 <= budget and #body + 2 <= 16 do
        table.insert(body, PART.CARRY)
        table.insert(body, PART.MOVE)
        cost = cost + 100
    end
    if cost + 50 <= budget and #body + 1 <= 16 then
        table.insert(body, PART.CARRY)
    end
    return body
end

-- Build a balanced WORK/CARRY/MOVE body for builders and upgraders.
function M.buildWorkerBody(budget)
    if budget < 200 then
        return nil
    end
    local body = { PART.WORK, PART.CARRY, PART.MOVE }
    local cost = 200
    while cost + 200 <= budget and #body + 3 <= 15 do
        table.insert(body, PART.WORK)
        table.insert(body, PART.CARRY)
        table.insert(body, PART.MOVE)
        cost = cost + 200
    end
    if cost + 100 <= budget and #body + 2 <= 15 then
        table.insert(body, PART.CARRY)
        table.insert(body, PART.MOVE)
    end
    return body
end

-- Count haulers currently alive in the creep's room.
local function countRoomHaulers(room)
    local c = getRoomCache(room)
    if c.haulerCount ~= nil then
        return c.haulerCount
    end
    local count = 0
    local myCreeps = cachedFind(room, "myCreeps", FIND.MY_CREEPS)
    for _, cr in ipairs(myCreeps) do
        if cr.memory and cr.memory.role == "hauler" then
            count = count + 1
        end
    end
    c.haulerCount = count
    return count
end

-- Assign a harvester to the source with fewest harvesters currently assigned.
local function getAssignedSource(creep)
    local room = creep.room
    if creep.memory.sourceId and screeps.Game.getObjectById then
        local existing = screeps.Game.getObjectById(creep.memory.sourceId)
        if existing and existing.room and existing.room.name == room.name then
            return existing
        end
    end

    local sources = room:find(FIND.SOURCES) or {}
    if #sources == 0 then
        return nil
    end

    local assigned = {}
    for _, s in ipairs(sources) do
        assigned[s.id] = 0
    end
    local myCreeps = room:find(FIND.MY_CREEPS) or {}
    for _, other in ipairs(myCreeps) do
        if other.id ~= creep.id and other.memory and other.memory.role == "harvester" and other.memory.sourceId then
            if assigned[other.memory.sourceId] ~= nil then
                assigned[other.memory.sourceId] = assigned[other.memory.sourceId] + 1
            end
        end
    end

    local best = sources[1]
    local bestCount = assigned[best.id] or 0
    for i = 2, #sources do
        local s = sources[i]
        local c = assigned[s.id] or 0
        if c < bestCount then
            bestCount = c
            best = s
        end
    end

    creep.memory.sourceId = best.id
    return best
end

-- Find a container adjacent to a source.
local function findAdjacentContainer(room, pos)
    if not pos then
        return nil
    end
    local structs = cachedFind(room, "structures", FIND.STRUCTURES)
    for _, s in ipairs(structs) do
        if s.structureType == STRUCTURE.CONTAINER and s.pos and pos:getRangeTo(s) <= 2 then
            return s
        end
    end
    return nil
end

-- Find spawn/extension/tower needing energy delivery.
local function findRefillTarget(creep, includeTowers, includeStorage)
    local room = creep.room
    local myStructs = cachedFind(room, "myStructures", FIND.MY_STRUCTURES)

    local primary = {}
    local towers = {}
    local storage = nil

    for _, s in ipairs(myStructs) do
        if s.structureType == STRUCTURE.SPAWN or s.structureType == STRUCTURE.EXTENSION then
            if getFreeEnergy(s.store) > 0 then
                table.insert(primary, s)
            end
        elseif includeTowers and s.structureType == STRUCTURE.TOWER then
            local used = getUsedEnergy(s.store)
            local free = getFreeEnergy(s.store)
            if free > 0 and used < 750 then
                table.insert(towers, s)
            end
        elseif includeStorage and s.structureType == STRUCTURE.STORAGE then
            if getFreeEnergy(s.store) > 0 then
                storage = s
            end
        end
    end

    if #primary > 0 then
        return creep.pos:findClosestByRange(primary) or primary[1]
    end
    if #towers > 0 then
        return creep.pos:findClosestByRange(towers) or towers[1]
    end
    return storage
end

-- Gather energy for haulers, builders, and upgraders.
local function acquireEnergy(creep, allowHarvest, allowStorage)
    local room = creep.room

    -- 1. Dropped energy resources (highest efficiency: zero decay waste).
    local dropped = cachedFind(room, "dropped", FIND.DROPPED_RESOURCES)
    local goodDrops = {}
    for _, r in ipairs(dropped) do
        if r.resourceType == RESOURCE.ENERGY and (r.amount or 0) >= 25 then
            table.insert(goodDrops, r)
        end
    end
    if #goodDrops > 0 then
        local target = creep.pos:findClosestByRange(goodDrops) or goodDrops[1]
        if creep:pickup(target) == ERR.NOT_IN_RANGE then
            creep:moveTo(target)
        end
        return true
    end

    -- 2. Tombstones with energy.
    if FIND.TOMBSTONES then
        local tombs = cachedFind(room, "tombs", FIND.TOMBSTONES)
        local goodTombs = {}
        for _, t in ipairs(tombs) do
            if getUsedEnergy(t.store) >= 20 then
                table.insert(goodTombs, t)
            end
        end
        if #goodTombs > 0 then
            local target = creep.pos:findClosestByRange(goodTombs) or goodTombs[1]
            if creep:withdraw(target, RESOURCE.ENERGY) == ERR.NOT_IN_RANGE then
                creep:moveTo(target)
            end
            return true
        end
    end

    -- 3. Source containers with energy.
    local structs = cachedFind(room, "structures", FIND.STRUCTURES)
    local containers = {}
    local storage = nil
    for _, s in ipairs(structs) do
        if s.structureType == STRUCTURE.CONTAINER and getUsedEnergy(s.store) >= 50 then
            table.insert(containers, s)
        elseif allowStorage and s.structureType == STRUCTURE.STORAGE and getUsedEnergy(s.store) >= 100 then
            storage = s
        end
    end
    if #containers > 0 then
        local target = creep.pos:findClosestByRange(containers) or containers[1]
        if creep:withdraw(target, RESOURCE.ENERGY) == ERR.NOT_IN_RANGE then
            creep:moveTo(target)
        end
        return true
    end

    -- 4. Storage (when allowed).
    if storage then
        if creep:withdraw(storage, RESOURCE.ENERGY) == ERR.NOT_IN_RANGE then
            creep:moveTo(storage)
        end
        return true
    end

    -- 5. Direct harvest fallback for workers when containers aren't built yet.
    if allowHarvest then
        local sources = room:find(FIND.SOURCES_ACTIVE) or room:find(FIND.SOURCES) or {}
        if #sources > 0 then
            local src = creep.pos:findClosestByRange(sources) or sources[1]
            if creep:harvest(src) == ERR.NOT_IN_RANGE then
                creep:moveTo(src)
            end
            return true
        end
    end

    return false
end

-- Role: Harvester (dedicated source miner; self-delivers when no haulers exist).
local function runHarvester(creep)
    local room = creep.room
    local source = getAssignedSource(creep)
    if not source then
        return
    end

    local haulers = countRoomHaulers(room)
    local container = findAdjacentContainer(room, source.pos)
    local used = getUsedEnergy(creep.store)
    local free = getFreeEnergy(creep.store)

    -- If haulers exist, act as a dedicated static/drop miner!
    if haulers > 0 then
        if container and container.pos then
            if creep.pos:getRangeTo(container) > 0 and creep.pos:getRangeTo(source) > 1 then
                creep:moveTo(container)
                return
            end
        end

        if free > 0 then
            if creep:harvest(source) == ERR.NOT_IN_RANGE then
                creep:moveTo(source)
            end
        elseif container and getFreeEnergy(container.store) > 0 then
            if creep:transfer(container, RESOURCE.ENERGY) == ERR.NOT_IN_RANGE then
                creep:moveTo(container)
            end
        elseif creep.drop then
            creep:drop(RESOURCE.ENERGY)
        end
        return
    end

    -- Bootstrap mode (no haulers yet): harvest and deliver directly to spawn/extensions.
    if creep.memory.delivering and used == 0 then
        creep.memory.delivering = false
    elseif not creep.memory.delivering and free == 0 then
        creep.memory.delivering = true
    end

    if not creep.memory.delivering then
        if creep:harvest(source) == ERR.NOT_IN_RANGE then
            creep:moveTo(source)
        end
    else
        local target = findRefillTarget(creep, true, false)
        if target then
            if creep:transfer(target, RESOURCE.ENERGY) == ERR.NOT_IN_RANGE then
                creep:moveTo(target)
            end
        elseif room.controller and room.controller.my then
            if creep:upgradeController(room.controller) == ERR.NOT_IN_RANGE then
                creep:moveTo(room.controller)
            end
        end
    end
end

-- Role: Hauler (fast energy pipeline from sources/drops -> spawn/extensions/towers/storage).
local function runHauler(creep)
    local room = creep.room
    local used = getUsedEnergy(creep.store)
    local free = getFreeEnergy(creep.store)

    if creep.memory.delivering and used == 0 then
        creep.memory.delivering = false
    elseif not creep.memory.delivering and (free == 0 or (used >= 50 and findRefillTarget(creep, false, false) ~= nil)) then
        creep.memory.delivering = true
    end

    if creep.memory.delivering then
        local target = findRefillTarget(creep, true, true)
        if target then
            if creep:transfer(target, RESOURCE.ENERGY) == ERR.NOT_IN_RANGE then
                creep:moveTo(target)
            end
            return
        end
        -- If everything is full, feed an upgrader creep near the controller.
        local myCreeps = cachedFind(room, "myCreeps", FIND.MY_CREEPS)
        local upgraders = {}
        for _, c in ipairs(myCreeps) do
            if c.memory and c.memory.role == "upgrader" and getFreeEnergy(c.store) >= 25 then
                table.insert(upgraders, c)
            end
        end
        if #upgraders > 0 then
            local u = creep.pos:findClosestByRange(upgraders) or upgraders[1]
            if creep:transfer(u, RESOURCE.ENERGY) == ERR.NOT_IN_RANGE then
                creep:moveTo(u)
            end
        end
    else
        -- Only withdraw from storage if spawn/extensions/towers need refilling.
        local needRefill = findRefillTarget(creep, true, false) ~= nil
        local got = acquireEnergy(creep, false, needRefill)
        if not got then
            -- Park near a harvester to catch dropped energy.
            local myCreeps = cachedFind(room, "myCreeps", FIND.MY_CREEPS)
            for _, c in ipairs(myCreeps) do
                if c.memory and c.memory.role == "harvester" and c.pos then
                    if creep.pos:getRangeTo(c) > 2 then
                        creep:moveTo(c)
                    end
                    break
                end
            end
        end
    end
end

-- Role: Builder (fast construction + critical repair + upgrade fallback).
local function runBuilder(creep)
    local room = creep.room
    local used = getUsedEnergy(creep.store)
    local free = getFreeEnergy(creep.store)

    if creep.memory.working and used == 0 then
        creep.memory.working = false
    elseif not creep.memory.working and free == 0 then
        creep.memory.working = true
    end

    if not creep.memory.working then
        acquireEnergy(creep, true, true)
        return
    end

    -- 1. Urgent repairs (critical ramparts, damaged towers/spawns/containers).
    local structs = room:find(FIND.STRUCTURES) or {}
    local urgent = nil
    local urgentScore = 9999999
    for _, s in ipairs(structs) do
        if s.hits and s.hitsMax and s.hitsMax > 0 then
            if s.structureType == STRUCTURE.RAMPART and s.hits < 5000 then
                if s.hits < urgentScore then
                    urgentScore = s.hits
                    urgent = s
                end
            elseif (s.structureType == STRUCTURE.CONTAINER or s.structureType == STRUCTURE.TOWER or s.structureType == STRUCTURE.SPAWN) and s.hits < s.hitsMax * 0.65 then
                if s.hits < urgentScore then
                    urgentScore = s.hits
                    urgent = s
                end
            end
        end
    end
    if urgent then
        if creep:repair(urgent) == ERR.NOT_IN_RANGE then
            creep:moveTo(urgent)
        end
        return
    end

    -- 2. Prioritized construction sites (Tower > Extension > Container > Storage > Rampart > Road).
    local sites = room:find(FIND.MY_CONSTRUCTION_SITES) or room:find(FIND.CONSTRUCTION_SITES) or {}
    if #sites > 0 then
        local bestSite = sites[1]
        local bestPrio = SITE_PRIORITY[bestSite.structureType] or 99
        local bestDist = creep.pos:getRangeTo(bestSite)
        for i = 2, #sites do
            local s = sites[i]
            local prio = SITE_PRIORITY[s.structureType] or 99
            local dist = creep.pos:getRangeTo(s)
            if prio < bestPrio or (prio == bestPrio and dist < bestDist) then
                bestSite = s
                bestPrio = prio
                bestDist = dist
            end
        end
        if creep:build(bestSite) == ERR.NOT_IN_RANGE then
            creep:moveTo(bestSite)
        end
        return
    end

    -- 3. General maintenance repairs.
    local repairable = {}
    for _, s in ipairs(structs) do
        if s.hits and s.hitsMax and s.hitsMax > 0 then
            if s.structureType == STRUCTURE.WALL or s.structureType == STRUCTURE.RAMPART then
                if s.hits < 25000 then
                    table.insert(repairable, s)
                end
            elseif s.hits < s.hitsMax * 0.8 then
                table.insert(repairable, s)
            end
        end
    end
    if #repairable > 0 then
        local target = creep.pos:findClosestByRange(repairable) or repairable[1]
        if creep:repair(target) == ERR.NOT_IN_RANGE then
            creep:moveTo(target)
        end
        return
    end

    -- 4. Fallback: help upgrade controller so builders are never idle.
    if room.controller and room.controller.my then
        if creep:upgradeController(room.controller) == ERR.NOT_IN_RANGE then
            creep:moveTo(room.controller)
        end
    end
end

-- Role: Upgrader (fast RCL progression).
local function runUpgrader(creep)
    local room = creep.room
    local used = getUsedEnergy(creep.store)
    local free = getFreeEnergy(creep.store)

    if creep.memory.working and used == 0 then
        creep.memory.working = false
    elseif not creep.memory.working and free == 0 then
        creep.memory.working = true
    end

    if not creep.memory.working then
        acquireEnergy(creep, true, true)
    else
        local ctrl = room.controller
        if ctrl and ctrl.my then
            if creep:upgradeController(ctrl) == ERR.NOT_IN_RANGE then
                creep:moveTo(ctrl)
            end
        end
    end
end

function M.run(creep)
    if not creep or creep.spawning or not creep.memory then
        return
    end
    local role = creep.memory.role
    if role == "harvester" then
        runHarvester(creep)
    elseif role == "hauler" then
        runHauler(creep)
    elseif role == "builder" then
        runBuilder(creep)
    elseif role == "upgrader" then
        runUpgrader(creep)
    end
end

return M
