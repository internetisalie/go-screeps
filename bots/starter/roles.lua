-- roles.lua: Full colony creep role behaviors for Screeps.
-- Roles: harvester, miner, hauler, upgrader, builder, defender, scout, remoteHarvester, remoteHauler, assault, healer.
-- Idiomatic Lua 5.4 (arnodel/golua) with Lunar/LuaLS annotations.

local screeps <const> = require("screeps")
local ERR <const> = screeps.ERR
local FIND <const> = screeps.FIND
local PART <const> = screeps.PART
local RESOURCE <const> = screeps.RESOURCE
local STRUCTURE <const> = screeps.STRUCTURE

---@class RoomIntelEntry
---@field owner? string
---@field sources integer
---@field hostiles integer
---@field lastSeen integer

---@class StarterRoles
local M = {}

local STRUCT_STORAGE = STRUCTURE.STORAGE
local STRUCT_LINK = STRUCTURE.LINK
local STRUCT_RAMPART = STRUCTURE.RAMPART
local STRUCT_WALL = STRUCTURE.WALL

---@type table<integer, FindConstant>
local EXIT_DIRS = {
    [1] = FIND.EXIT_TOP,
    [3] = FIND.EXIT_RIGHT,
    [5] = FIND.EXIT_BOTTOM,
    [7] = FIND.EXIT_LEFT,
}

local SCOUT_INTEL_TTL = 1500

local function clamp(val, lo, hi)
    if val < lo then
        return lo
    end
    if val > hi then
        return hi
    end
    return val
end

local function isOnEdge(pos)
    if not pos then
        return false
    end
    return pos.x <= 0 or pos.x >= 49 or pos.y <= 0 or pos.y >= 49
end

local function stepOffEdge(creep)
    if not creep or not creep.pos then
        return false
    end
    local x, y = creep.pos.x, creep.pos.y
    if x <= 0 then
        creep:moveTo(2, clamp(y, 2, 47))
        return true
    elseif x >= 49 then
        creep:moveTo(47, clamp(y, 2, 47))
        return true
    elseif y <= 0 then
        creep:moveTo(clamp(x, 2, 47), 2)
        return true
    elseif y >= 49 then
        creep:moveTo(clamp(x, 2, 47), 47)
        return true
    end
    return false
end

local function resolveExitDir(room, targetRoom)
    if not room or not targetRoom or room.name == targetRoom then
        return nil
    end
    if room.findExitTo then
        local dir = room:findExitTo(targetRoom)
        if type(dir) == "number" and dir > 0 then
            return dir
        end
    end
    if screeps.Game.map and screeps.Game.map.describeExits then
        local exits = screeps.Game.map.describeExits(room.name)
        if type(exits) == "table" then
            for k, adjName in pairs(exits) do
                if adjName == targetRoom then
                    local dirNum = tonumber(k)
                    if dirNum and EXIT_DIRS[dirNum] then
                        return EXIT_DIRS[dirNum]
                    end
                end
            end
        end
    end
    return nil
end

local function moveAcrossExit(creep, targetRoom, fallbackDir)
    local room = creep.room
    if not room then
        return false
    end
    local exitDir = nil
    if targetRoom then
        exitDir = resolveExitDir(room, targetRoom)
    end
    if not exitDir then
        exitDir = fallbackDir
    end
    if not exitDir then
        return false
    end
    local exitTiles = room:find(exitDir) or {}
    if #exitTiles == 0 then
        return false
    end
    local exitPos = creep.pos:findClosestByRange(exitTiles) or exitTiles[1]
    creep:moveTo(exitPos, { range = 0 })
    return true
end

-- Record room intel into Memory.roomIntel[room.name] (deduplicated per tick).
---Record visible room intelligence (owner, source count, hostiles, tick) into Memory.roomIntel.
---@param room Room
function M.recordRoomIntel(room)
    if not room or not room.name or not screeps.Memory then
        return
    end
    if not screeps.Memory.roomIntel then
        screeps.Memory.roomIntel = {}
    end
    local now = screeps.Game.time or 0
    local existing = screeps.Memory.roomIntel[room.name]
    if existing and existing.lastSeen == now then
        return
    end

    local sources = room:find(FIND.SOURCES) or {}
    local hostiles = room:find(FIND.HOSTILE_CREEPS) or {}
    local owner = nil
    local ctrl = room.controller
    if ctrl then
        if ctrl.owner and ctrl.owner.username then
            owner = ctrl.owner.username
        elseif ctrl.owner and type(ctrl.owner) == "string" then
            owner = ctrl.owner
        elseif ctrl.my then
            owner = "self"
        end
    end

    screeps.Memory.roomIntel[room.name] = {
        sources = #sources,
        owner = owner,
        hostiles = #hostiles,
        lastSeen = now,
    }
end

local function getUsedEnergy(store)
    if not store then
        return 0
    end
    return store:getUsedCapacity(RESOURCE.ENERGY) or 0
end

local function getFreeEnergy(store)
    if not store then
        return 0
    end
    return store:getFreeCapacity(RESOURCE.ENERGY) or 0
end

local function drawTargetLine(creep, target, color)
    if not target or not target.pos or not creep.room or not creep.room.visual then
        return
    end
    creep.room.visual:line(
        creep.pos.x,
        creep.pos.y,
        target.pos.x,
        target.pos.y,
        { color = color, width = 0.08, opacity = 0.35 }
    )
end

local function findClosest(pos, list)
    if not list or #list == 0 then
        return nil
    end
    return pos:findClosestByRange(list)
end

local function updateWorkingState(creep)
    local used = getUsedEnergy(creep.store)
    local free = getFreeEnergy(creep.store)

    if creep.memory.working and used == 0 then
        creep.memory.working = false
        creep.memory.fromStorage = false
    elseif (not creep.memory.working) and free == 0 then
        creep.memory.working = true
    end
end

local function findNearestSource(creep)
    local sources = creep.room:find(FIND.SOURCES)
    if not sources or #sources == 0 then
        return nil
    end

    local active = {}
    for _, src in ipairs(sources) do
        if src.energy == nil or src.energy > 0 then
            table.insert(active, src)
        end
    end

    if #active > 0 then
        return findClosest(creep.pos, active)
    end
    return findClosest(creep.pos, sources)
end

local function findDroppedEnergy(creep)
    local dropped = creep.room:find(FIND.DROPPED_RESOURCES)
    if not dropped or #dropped == 0 then
        return nil
    end

    local pile = {}
    local small = {}
    for _, res in ipairs(dropped) do
        if res.resourceType == nil or res.resourceType == RESOURCE.ENERGY then
            if res.amount == nil or res.amount >= 25 then
                table.insert(pile, res)
            else
                table.insert(small, res)
            end
        end
    end

    if #pile > 0 then
        return findClosest(creep.pos, pile)
    end
    return findClosest(creep.pos, small)
end

local function findEnergyTombstone(creep)
    if not FIND.TOMBSTONES then
        return nil
    end
    local tombs = creep.room:find(FIND.TOMBSTONES)
    if not tombs or #tombs == 0 then
        return nil
    end
    local rich = {}
    for _, t in ipairs(tombs) do
        if getUsedEnergy(t.store) > 0 then
            table.insert(rich, t)
        end
    end
    return findClosest(creep.pos, rich)
end

local function findRoomStorage(room)
    local myStructs = room:find(FIND.MY_STRUCTURES)
    if not myStructs then
        return nil
    end
    for _, s in ipairs(myStructs) do
        if s.structureType == STRUCT_STORAGE then
            return s
        end
    end
    return nil
end

local function findContainersNear(room, anchorPos, maxRange, minEnergy)
    if not anchorPos then
        return {}
    end
    local structs = room:find(FIND.STRUCTURES) or {}
    local matches = {}
    for _, s in ipairs(structs) do
        if s.structureType == STRUCTURE.CONTAINER and s.pos then
            if anchorPos:getRangeTo(s) <= maxRange then
                if minEnergy == nil or getUsedEnergy(s.store) >= minEnergy then
                    table.insert(matches, s)
                end
            end
        end
    end
    return matches
end

local function findSourceContainers(room, minEnergy)
    local sources = room:find(FIND.SOURCES) or {}
    local structs = room:find(FIND.STRUCTURES) or {}
    local matches = {}

    for _, s in ipairs(structs) do
        if s.structureType == STRUCTURE.CONTAINER and s.pos then
            if minEnergy == nil or getUsedEnergy(s.store) >= minEnergy then
                for _, src in ipairs(sources) do
                    if src.pos and src.pos:getRangeTo(s) <= 2 then
                        table.insert(matches, s)
                        break
                    end
                end
            end
        end
    end
    return matches
end

local function findAnyEnergyContainer(creep)
    local structures = creep.room:find(FIND.STRUCTURES)
    if not structures or #structures == 0 then
        return nil
    end

    local rich = {}
    local any = {}
    for _, s in ipairs(structures) do
        if s.structureType == STRUCTURE.CONTAINER then
            local energy = getUsedEnergy(s.store)
            if energy >= 50 then
                table.insert(rich, s)
            elseif energy > 0 then
                table.insert(any, s)
            end
        end
    end

    if #rich > 0 then
        return findClosest(creep.pos, rich)
    end
    return findClosest(creep.pos, any)
end

-- Find highest-priority structure needing energy (spawns/extensions -> towers -> optional storage/containers).
local function findEnergyDeliveryTarget(creep, opts)
    opts = opts or {}
    local myStructures = creep.room:find(FIND.MY_STRUCTURES)
    local spawnOrExt = {}
    local towers = {}
    local storage = nil

    if myStructures then
        for _, s in ipairs(myStructures) do
            if s.structureType == STRUCTURE.SPAWN or s.structureType == STRUCTURE.EXTENSION then
                if getFreeEnergy(s.store) > 0 then
                    table.insert(spawnOrExt, s)
                end
            elseif s.structureType == STRUCTURE.TOWER then
                if getFreeEnergy(s.store) > 50 then
                    table.insert(towers, s)
                end
            elseif s.structureType == STRUCT_STORAGE then
                storage = s
            end
        end
    end

    if #spawnOrExt > 0 then
        return findClosest(creep.pos, spawnOrExt)
    end
    if #towers > 0 then
        return findClosest(creep.pos, towers)
    end

    if opts.includeControllerContainer and creep.room.controller and creep.room.controller.pos then
        local ctrlContainers = findContainersNear(creep.room, creep.room.controller.pos, 3, nil)
        local needyCtrl = {}
        for _, c in ipairs(ctrlContainers) do
            if getFreeEnergy(c.store) >= 50 then
                table.insert(needyCtrl, c)
            end
        end
        if #needyCtrl > 0 then
            return findClosest(creep.pos, needyCtrl)
        end
    end

    if opts.includeStorage and storage and (not creep.memory.fromStorage) and getFreeEnergy(storage.store) > 0 then
        return storage
    end

    if opts.includeContainers then
        local structures = creep.room:find(FIND.STRUCTURES) or {}
        local containers = {}
        for _, s in ipairs(structures) do
            if s.structureType == STRUCTURE.CONTAINER and getFreeEnergy(s.store) > 0 then
                table.insert(containers, s)
            end
        end
        if #containers > 0 then
            return findClosest(creep.pos, containers)
        end
    end

    return nil
end

local function roomNeedsCriticalEnergy(room)
    local myStructures = room:find(FIND.MY_STRUCTURES) or {}
    for _, s in ipairs(myStructures) do
        if s.structureType == STRUCTURE.SPAWN or s.structureType == STRUCTURE.EXTENSION then
            if getFreeEnergy(s.store) > 0 then
                return true
            end
        elseif s.structureType == STRUCTURE.TOWER then
            if getFreeEnergy(s.store) > 100 then
                return true
            end
        end
    end
    return false
end

local function upgradeRoomController(creep)
    local ctrl = creep.room.controller
    if not ctrl then
        return false
    end
    drawTargetLine(creep, ctrl, "#7e57c2")
    if creep:upgradeController(ctrl) == ERR.NOT_IN_RANGE then
        creep:moveTo(ctrl)
    end
    return true
end

-- Generic energy acquisition for workers (dropped -> storage -> container -> source).
local function acquireWorkerEnergy(creep, preferControllerSupply)
    local ctrl = creep.room.controller

    -- Upgraders prefer controller link or controller container first.
    if preferControllerSupply and ctrl and ctrl.pos then
        local myStructs = creep.room:find(FIND.MY_STRUCTURES) or {}
        for _, s in ipairs(myStructs) do
            if s.structureType == STRUCT_LINK and s.pos and ctrl.pos:getRangeTo(s) <= 3 then
                if getUsedEnergy(s.store) > 0 then
                    drawTargetLine(creep, s, "#b388ff")
                    if creep:withdraw(s, RESOURCE.ENERGY) == ERR.NOT_IN_RANGE then
                        creep:moveTo(s)
                    end
                    return true
                end
            end
        end

        local ctrlContainers = findContainersNear(creep.room, ctrl.pos, 3, 1)
        local c = findClosest(creep.pos, ctrlContainers)
        if c then
            drawTargetLine(creep, c, "#ffca28")
            if creep:withdraw(c, RESOURCE.ENERGY) == ERR.NOT_IN_RANGE then
                creep:moveTo(c)
            end
            return true
        end
    end

    local storage = findRoomStorage(creep.room)
    if storage and getUsedEnergy(storage.store) >= 100 then
        drawTargetLine(creep, storage, "#ffd54f")
        if creep:withdraw(storage, RESOURCE.ENERGY) == ERR.NOT_IN_RANGE then
            creep:moveTo(storage)
        end
        return true
    end

    local dropped = findDroppedEnergy(creep)
    if dropped then
        drawTargetLine(creep, dropped, "#ffd54f")
        if creep:pickup(dropped) == ERR.NOT_IN_RANGE then
            creep:moveTo(dropped)
        end
        return true
    end

    local container = findAnyEnergyContainer(creep)
    if container then
        drawTargetLine(creep, container, "#ffca28")
        if creep:withdraw(container, RESOURCE.ENERGY) == ERR.NOT_IN_RANGE then
            creep:moveTo(container)
        end
        return true
    end

    local source = findNearestSource(creep)
    if source then
        drawTargetLine(creep, source, "#ffe082")
        if creep:harvest(source) == ERR.NOT_IN_RANGE then
            creep:moveTo(source)
        end
        return true
    end

    return false
end

-- 1. Harvester: bootstrap / fallback mobile harvester.
function M.harvester(creep)
    updateWorkingState(creep)

    if not creep.memory.working then
        local source = findNearestSource(creep)
        if source then
            drawTargetLine(creep, source, "#ffe082")
            if creep:harvest(source) == ERR.NOT_IN_RANGE then
                creep:moveTo(source)
            end
        end
        return
    end

    local target = findEnergyDeliveryTarget(creep, {
        includeStorage = true,
        includeContainers = true,
    })
    if target then
        drawTargetLine(creep, target, "#ffeb3b")
        if creep:transfer(target, RESOURCE.ENERGY) == ERR.NOT_IN_RANGE then
            creep:moveTo(target)
        end
        return
    end

    upgradeRoomController(creep)
end

-- Assign a miner to the source in its room with the fewest assigned miners.
local function resolveMinerSource(creep)
    if creep.memory.sourceId and screeps.Game.getObjectById then
        local existing = screeps.Game.getObjectById(creep.memory.sourceId)
        if existing then
            return existing
        end
        creep.memory.sourceId = nil
    end

    local sources = creep.room:find(FIND.SOURCES) or {}
    if #sources == 0 then
        return nil
    end

    local assignedCounts = {}
    for _, src in ipairs(sources) do
        assignedCounts[src.id] = 0
    end

    local myCreeps = creep.room:find(FIND.MY_CREEPS) or {}
    for _, c in ipairs(myCreeps) do
        if c.id ~= creep.id and c.memory and c.memory.role == "miner" and c.memory.sourceId then
            if assignedCounts[c.memory.sourceId] ~= nil then
                assignedCounts[c.memory.sourceId] = assignedCounts[c.memory.sourceId] + 1
            end
        end
    end

    local chosen = sources[1]
    local minCount = assignedCounts[chosen.id] or 0
    for _, src in ipairs(sources) do
        local cnt = assignedCounts[src.id] or 0
        if cnt < minCount then
            minCount = cnt
            chosen = src
        end
    end

    creep.memory.sourceId = chosen.id
    return chosen
end

-- 2. Miner: high-WORK static source miner that sits on a source container and harvests continuously.
function M.miner(creep)
    local source = resolveMinerSource(creep)
    if not source then
        return
    end

    -- Locate a container adjacent to the assigned source.
    local containers = findContainersNear(creep.room, source.pos, 1, nil)
    local container = containers[1]

    if container and container.pos then
        if creep.pos.x ~= container.pos.x or creep.pos.y ~= container.pos.y then
            creep:moveTo(container)
            return
        end
    end

    -- If carrying energy and an adjacent source link has free capacity, dump into the link.
    if getUsedEnergy(creep.store) > 0 then
        local myStructs = creep.room:find(FIND.MY_STRUCTURES) or {}
        for _, s in ipairs(myStructs) do
            if s.structureType == STRUCT_LINK and s.pos and creep.pos:getRangeTo(s) <= 1 then
                if getFreeEnergy(s.store) > 0 then
                    drawTargetLine(creep, s, "#b388ff")
                    creep:transfer(s, RESOURCE.ENERGY)
                    break
                end
            end
        end
    end

    drawTargetLine(creep, source, "#ffd54f")
    if creep:harvest(source) == ERR.NOT_IN_RANGE then
        creep:moveTo(source)
    end
end

-- 3. Hauler: collects dropped energy, loots tombstones, withdraws from source containers/storage,
-- and delivers to spawns, extensions, towers, controller container, and storage.
function M.hauler(creep)
    updateWorkingState(creep)

    if not creep.memory.working then
        -- 1. Dropped energy.
        local dropped = findDroppedEnergy(creep)
        if dropped then
            drawTargetLine(creep, dropped, "#ffd54f")
            if creep:pickup(dropped) == ERR.NOT_IN_RANGE then
                creep:moveTo(dropped)
            end
            return
        end

        -- 2. Tombstones with energy.
        local tomb = findEnergyTombstone(creep)
        if tomb then
            drawTargetLine(creep, tomb, "#ff8a65")
            if creep:withdraw(tomb, RESOURCE.ENERGY) == ERR.NOT_IN_RANGE then
                creep:moveTo(tomb)
            end
            return
        end

        -- 3. Source containers with energy.
        local srcContainers = findSourceContainers(creep.room, 50)
        if #srcContainers == 0 then
            srcContainers = findSourceContainers(creep.room, 1)
        end
        local srcContainer = findClosest(creep.pos, srcContainers)
        if srcContainer then
            drawTargetLine(creep, srcContainer, "#ffca28")
            if creep:withdraw(srcContainer, RESOURCE.ENERGY) == ERR.NOT_IN_RANGE then
                creep:moveTo(srcContainer)
            end
            return
        end

        -- 4. Storage (when spawns, extensions, or towers need energy).
        if roomNeedsCriticalEnergy(creep.room) then
            local storage = findRoomStorage(creep.room)
            if storage and getUsedEnergy(storage.store) > 0 then
                creep.memory.fromStorage = true
                drawTargetLine(creep, storage, "#26c6da")
                if creep:withdraw(storage, RESOURCE.ENERGY) == ERR.NOT_IN_RANGE then
                    creep:moveTo(storage)
                end
                return
            end
        end

        -- 5. Fallback: any container with energy when critical structures need refilling.
        if roomNeedsCriticalEnergy(creep.room) then
            local anyC = findAnyEnergyContainer(creep)
            if anyC then
                drawTargetLine(creep, anyC, "#ffca28")
                if creep:withdraw(anyC, RESOURCE.ENERGY) == ERR.NOT_IN_RANGE then
                    creep:moveTo(anyC)
                end
                return
            end
        end

        if getUsedEnergy(creep.store) > 0 then
            creep.memory.working = true
        end
        return
    end

    -- Delivering energy.
    local target = findEnergyDeliveryTarget(creep, {
        includeControllerContainer = true,
        includeStorage = true,
    })
    if target then
        drawTargetLine(creep, target, "#26c6da")
        if creep:transfer(target, RESOURCE.ENERGY) == ERR.NOT_IN_RANGE then
            creep:moveTo(target)
        end
        return
    end

    -- Fallback: supply an upgrader or builder with free energy capacity.
    local myCreeps = creep.room:find(FIND.MY_CREEPS) or {}
    local needy = {}
    for _, c in ipairs(myCreeps) do
        if c.id ~= creep.id and c.memory then
            local r = c.memory.role
            if (r == "upgrader" or r == "builder") and getFreeEnergy(c.store) > 0 then
                table.insert(needy, c)
            end
        end
    end

    local recipient = findClosest(creep.pos, needy)
    if recipient then
        drawTargetLine(creep, recipient, "#80deea")
        if creep:transfer(recipient, RESOURCE.ENERGY) == ERR.NOT_IN_RANGE then
            creep:moveTo(recipient)
        end
        return
    end

    local spawns = creep.room:find(FIND.MY_SPAWNS)
    local spawn = findClosest(creep.pos, spawns)
    if spawn and creep.pos:getRangeTo(spawn) > 3 then
        creep:moveTo(spawn)
    end
end

-- 4. Upgrader: withdraws from controller link/container/storage or harvests, upgrades controller.
function M.upgrader(creep)
    updateWorkingState(creep)

    if not creep.memory.working then
        acquireWorkerEnergy(creep, true)
        return
    end

    upgradeRoomController(creep)
end

-- Construction site priority (lower number = higher priority).
local function sitePriority(stype)
    if stype == STRUCTURE.SPAWN or stype == STRUCTURE.EXTENSION or stype == STRUCTURE.TOWER then
        return 1
    elseif stype == STRUCTURE.CONTAINER or stype == STRUCT_STORAGE or stype == STRUCT_LINK then
        return 2
    elseif stype == STRUCTURE.ROAD then
        return 3
    elseif stype == STRUCT_RAMPART then
        return 4
    end
    return 5
end

-- 5. Builder: builds prioritized sites and repairs damaged structures & ramparts.
function M.builder(creep)
    updateWorkingState(creep)

    if not creep.memory.working then
        acquireWorkerEnergy(creep, false)
        return
    end

    -- Priority 1: build construction sites by tier, closest within highest tier.
    local sites = creep.room:find(FIND.CONSTRUCTION_SITES)
    if sites and #sites > 0 then
        local bestTier = 99
        for _, site in ipairs(sites) do
            local tier = sitePriority(site.structureType)
            if tier < bestTier then
                bestTier = tier
            end
        end

        local tierSites = {}
        for _, site in ipairs(sites) do
            if sitePriority(site.structureType) == bestTier then
                table.insert(tierSites, site)
            end
        end

        local targetSite = findClosest(creep.pos, tierSites)
        if targetSite then
            drawTargetLine(creep, targetSite, "#66bb6a")
            if creep:build(targetSite) == ERR.NOT_IN_RANGE then
                creep:moveTo(targetSite)
            end
            return
        end
    end

    -- Priority 2: repair damaged structures (regular structures < 80%, ramparts up to RCL cap).
    local rcl = (creep.room.controller and creep.room.controller.level) or 1
    local rampartCap = math.min(50000, rcl * 10000)

    local structures = creep.room:find(FIND.STRUCTURES) or {}
    local worstRegular = nil
    local worstRatio = 0.8
    local worstRampart = nil
    local lowestRampartHits = rampartCap

    for _, s in ipairs(structures) do
        if s.hits and s.hitsMax and s.hitsMax > 0 then
            if s.structureType == STRUCT_RAMPART then
                if s.hits < lowestRampartHits and s.hits < s.hitsMax then
                    lowestRampartHits = s.hits
                    worstRampart = s
                end
            elseif s.structureType ~= STRUCT_WALL then
                local ratio = s.hits / s.hitsMax
                if ratio < worstRatio then
                    worstRatio = ratio
                    worstRegular = s
                end
            end
        end
    end

    local repairTarget = worstRegular or worstRampart
    if repairTarget then
        drawTargetLine(creep, repairTarget, "#ffa726")
        if creep:repair(repairTarget) == ERR.NOT_IN_RANGE then
            creep:moveTo(repairTarget)
        end
        return
    end

    -- Priority 3: upgrade controller when nothing needs building or repairing.
    upgradeRoomController(creep)
end

-- Find a friendly rampart near the defender that can still engage `target` without stepping on border exits.
local function findCombatRampart(creep, target, maxEngageRange)
    if not target or not target.pos then
        return nil
    end
    local myStructs = creep.room:find(FIND.MY_STRUCTURES) or {}
    local best = nil
    local bestDist = 999
    for _, s in ipairs(myStructs) do
        if s.structureType == STRUCT_RAMPART and s.pos and not isOnEdge(s.pos) then
            local distToTarget = s.pos:getRangeTo(target)
            if distToTarget <= maxEngageRange then
                local distToCreep = creep.pos:getRangeTo(s)
                if distToCreep <= 3 and distToCreep < bestDist then
                    bestDist = distToCreep
                    best = s
                end
            end
        end
    end
    return best
end

-- 6. Defender: home-bound, border-safe, rampart-aware combat defender.
function M.defender(creep)
    local room = creep.room
    local homeRoom = (creep.memory and creep.memory.room) or (room and room.name)
    if creep.memory and not creep.memory.room and homeRoom then
        creep.memory.room = homeRoom
    end

    -- Never abandon home room: immediately return if pushed across an exit.
    if homeRoom and room and room.name ~= homeRoom then
        moveAcrossExit(creep, homeRoom, nil)
        return
    end

    -- Step off border exit tiles immediately so we never bounce out of homeRoom.
    if isOnEdge(creep.pos) then
        stepOffEdge(creep)
        return
    end

    local hostiles = room:find(FIND.HOSTILE_CREEPS) or {}
    if #hostiles > 0 then
        -- Prefer interior hostiles (not standing on exit border tiles) when available.
        local interiorHostiles = {}
        for _, h in ipairs(hostiles) do
            if h.pos and not isOnEdge(h.pos) then
                table.insert(interiorHostiles, h)
            end
        end
        local target = findClosest(creep.pos, (#interiorHostiles > 0 and interiorHostiles) or hostiles)
        if target then
            drawTargetLine(creep, target, "#ef5350")
            local range = creep.pos:getRangeTo(target)

            if creep.rangedAttack and range <= 3 then
                creep:rangedAttack(target)
            end

            -- Rampart-aware positioning: hold or step onto a nearby rampart in weapon range.
            local engageRange = (creep.attack and 1) or 3
            local rampart = findCombatRampart(creep, target, engageRange)
            if rampart and rampart.pos then
                if creep.pos.x ~= rampart.pos.x or creep.pos.y ~= rampart.pos.y then
                    creep:moveTo(rampart)
                end
                if creep.attack and range <= 1 then
                    creep:attack(target)
                end
                return
            end

            -- Compute border-safe chase coordinate (never step onto x==0, x==49, y==0, y==49).
            local chaseX = clamp(target.pos.x, 1, 48)
            local chaseY = clamp(target.pos.y, 1, 48)

            if creep.attack then
                if creep:attack(target) == ERR.NOT_IN_RANGE then
                    if isOnEdge(target.pos) then
                        creep:moveTo(chaseX, chaseY)
                    else
                        creep:moveTo(target)
                    end
                end
            elseif range > 3 then
                if isOnEdge(target.pos) then
                    creep:moveTo(chaseX, chaseY)
                else
                    creep:moveTo(target)
                end
            end
            return
        end
    end

    -- Patrol near primary spawn / north-east perimeter (staying inside x=2..47, y=2..47).
    local spawns = room:find(FIND.MY_SPAWNS)
    local spawn = findClosest(creep.pos, spawns)
    if spawn and spawn.pos then
        local px = clamp(spawn.pos.x + 3, 2, 47)
        local py = clamp(spawn.pos.y - 3, 2, 47)
        if creep.pos:getRangeTo(px, py) > 2 then
            creep:moveTo(px, py)
        end
    else
        local px = clamp(creep.pos.x, 2, 47)
        local py = clamp(creep.pos.y, 2, 47)
        if creep.pos.x ~= px or creep.pos.y ~= py then
            creep:moveTo(px, py)
        end
    end
end

-- Pick the next adjacent connected room of `homeRoomName` needing intel refresh.
local function pickNextScoutRoom(homeRoomName)
    if not homeRoomName or not screeps.Game.map or not screeps.Game.map.describeExits then
        return nil
    end
    local exits = screeps.Game.map.describeExits(homeRoomName)
    if type(exits) ~= "table" then
        return nil
    end
    local candidates = {}
    for _, adjName in pairs(exits) do
        if type(adjName) == "string" and adjName ~= "" then
            table.insert(candidates, adjName)
        end
    end
    table.sort(candidates)

    local now = screeps.Game.time or 0
    local intelMap = (screeps.Memory and screeps.Memory.roomIntel) or {}
    local bestRoom = nil
    local oldestSeen = now + 1
    for _, rName in ipairs(candidates) do
        local info = intelMap[rName]
        local seen = (info and info.lastSeen) or -9999
        if (not info) or (now - seen > SCOUT_INTEL_TTL) then
            if seen < oldestSeen then
                oldestSeen = seen
                bestRoom = rName
            end
        end
    end
    return bestRoom
end

-- 7. Scout: fast 1-MOVE room intel explorer visiting adjacent connected rooms.
function M.scout(creep)
    local room = creep.room
    local homeRoom = (creep.memory and creep.memory.room) or (room and room.name)
    if creep.memory and not creep.memory.room and homeRoom then
        creep.memory.room = homeRoom
    end

    if room.name == homeRoom then
        creep.memory.scoutTicks = 0
        local targetRoom = creep.memory.targetRoom
        if targetRoom then
            local info = screeps.Memory and screeps.Memory.roomIntel and screeps.Memory.roomIntel[targetRoom]
            local now = screeps.Game.time or 0
            if info and info.lastSeen and (now - info.lastSeen) <= SCOUT_INTEL_TTL then
                targetRoom = nil
            end
        end
        if not targetRoom then
            targetRoom = pickNextScoutRoom(homeRoom)
            creep.memory.targetRoom = targetRoom
        end

        if targetRoom then
            moveAcrossExit(creep, targetRoom, nil)
            return
        end

        -- All adjacent rooms recently scouted: hold near spawn inside safe interior.
        if isOnEdge(creep.pos) then
            stepOffEdge(creep)
            return
        end
        local spawns = room:find(FIND.MY_SPAWNS)
        local spawn = findClosest(creep.pos, spawns)
        if spawn and spawn.pos and creep.pos:getRangeTo(spawn) > 4 then
            creep:moveTo(spawn)
        end
        return
    end

    -- Inside an adjacent room: intel is already recorded by M.run.
    if isOnEdge(creep.pos) then
        stepOffEdge(creep)
        return
    end

    local hostiles = room:find(FIND.HOSTILE_CREEPS) or {}
    if #hostiles > 0 then
        -- Hostiles spotted: intel recorded, retreat back home immediately.
        creep.memory.targetRoom = nil
        moveAcrossExit(creep, homeRoom, nil)
        return
    end

    creep.memory.scoutTicks = (creep.memory.scoutTicks or 0) + 1
    if creep.memory.scoutTicks < 4 then
        if creep.pos:getRangeTo(25, 25) > 8 then
            creep:moveTo(25, 25)
        end
    else
        creep.memory.targetRoom = nil
        moveAcrossExit(creep, homeRoom, nil)
    end
end

-- Assign a remoteHarvester to the remote source with the fewest assigned remoteHarvesters.
local function resolveRemoteSource(creep)
    if creep.memory.sourceId and screeps.Game.getObjectById then
        local existing = screeps.Game.getObjectById(creep.memory.sourceId)
        if existing and existing.room and existing.room.name == creep.room.name then
            return existing
        end
        creep.memory.sourceId = nil
    end

    local sources = creep.room:find(FIND.SOURCES) or {}
    if #sources == 0 then
        return nil
    end

    local assignedCounts = {}
    for _, src in ipairs(sources) do
        assignedCounts[src.id] = 0
    end

    local myCreeps = creep.room:find(FIND.MY_CREEPS) or {}
    for _, c in ipairs(myCreeps) do
        if c.id ~= creep.id and c.memory and c.memory.role == "remoteHarvester" and c.memory.sourceId then
            if assignedCounts[c.memory.sourceId] ~= nil then
                assignedCounts[c.memory.sourceId] = assignedCounts[c.memory.sourceId] + 1
            end
        end
    end

    local chosen = sources[1]
    local minCount = assignedCounts[chosen.id] or 0
    for _, src in ipairs(sources) do
        local cnt = assignedCounts[src.id] or 0
        if cnt < minCount then
            minCount = cnt
            chosen = src
        end
    end

    creep.memory.sourceId = chosen.id
    return chosen
end

-- 8. Remote Harvester: travels to targetRoom, harvests source, drops/transfers energy, flees if hostiles appear.
function M.remoteHarvester(creep)
    local room = creep.room
    local homeRoom = (creep.memory and creep.memory.room) or (room and room.name)
    if creep.memory and not creep.memory.room and homeRoom then
        creep.memory.room = homeRoom
    end

    local targetRoom = creep.memory.targetRoom
    if not targetRoom and screeps.Memory and screeps.Memory.rooms and screeps.Memory.rooms[homeRoom] then
        targetRoom = screeps.Memory.rooms[homeRoom].remoteRoom
        creep.memory.targetRoom = targetRoom
    end

    -- Flee back home if hostiles appear in any non-home room.
    if room.name ~= homeRoom then
        local hostiles = room:find(FIND.HOSTILE_CREEPS) or {}
        if #hostiles > 0 then
            moveAcrossExit(creep, homeRoom, nil)
            return
        end
    end

    if not targetRoom then
        if room.name ~= homeRoom then
            moveAcrossExit(creep, homeRoom, nil)
        else
            local spawns = room:find(FIND.MY_SPAWNS)
            local spawn = findClosest(creep.pos, spawns)
            if spawn and spawn.pos and creep.pos:getRangeTo(spawn) > 4 then
                creep:moveTo(spawn)
            end
        end
        return
    end

    -- Transit to targetRoom (stay home if remote room is currently known to have hostiles).
    if room.name ~= targetRoom then
        if room.name == homeRoom then
            local rIntel = screeps.Memory and screeps.Memory.roomIntel and screeps.Memory.roomIntel[targetRoom]
            if rIntel and (rIntel.hostiles or 0) > 0 then
                local spawns = room:find(FIND.MY_SPAWNS)
                local spawn = findClosest(creep.pos, spawns)
                if spawn and spawn.pos and creep.pos:getRangeTo(spawn) > 4 then
                    creep:moveTo(spawn)
                end
                return
            end
        elseif isOnEdge(creep.pos) then
            stepOffEdge(creep)
            return
        end
        moveAcrossExit(creep, targetRoom, nil)
        return
    end

    -- Inside targetRoom: step off edge and harvest.
    if isOnEdge(creep.pos) then
        stepOffEdge(creep)
        return
    end

    local source = resolveRemoteSource(creep)
    if not source then
        return
    end

    drawTargetLine(creep, source, "#26c6da")
    if creep:harvest(source) == ERR.NOT_IN_RANGE then
        creep:moveTo(source)
        return
    end

    -- Offload energy to adjacent container, adjacent remoteHauler, or drop when full.
    local used = getUsedEnergy(creep.store)
    local free = getFreeEnergy(creep.store)
    if used > 0 then
        local containers = findContainersNear(room, creep.pos, 1, nil)
        for _, c in ipairs(containers) do
            if getFreeEnergy(c.store) > 0 then
                creep:transfer(c, RESOURCE.ENERGY)
                return
            end
        end

        local myCreeps = room:find(FIND.MY_CREEPS) or {}
        for _, c in ipairs(myCreeps) do
            if c.id ~= creep.id and c.memory and c.memory.role == "remoteHauler" and c.pos then
                if creep.pos:getRangeTo(c) <= 1 and getFreeEnergy(c.store) > 0 then
                    creep:transfer(c, RESOURCE.ENERGY)
                    return
                end
            end
        end

        if free == 0 and creep.drop then
            creep:drop(RESOURCE.ENERGY)
        end
    end
end

-- 9. Remote Hauler: fast 1:1 CARRY/MOVE hauler ferrying energy from targetRoom back to homeRoom.
function M.remoteHauler(creep)
    local room = creep.room
    local homeRoom = (creep.memory and creep.memory.room) or (room and room.name)
    if creep.memory and not creep.memory.room and homeRoom then
        creep.memory.room = homeRoom
    end

    local targetRoom = creep.memory.targetRoom
    if not targetRoom and screeps.Memory and screeps.Memory.rooms and screeps.Memory.rooms[homeRoom] then
        targetRoom = screeps.Memory.rooms[homeRoom].remoteRoom
        creep.memory.targetRoom = targetRoom
    end

    updateWorkingState(creep)

    -- Flee back home immediately if hostiles appear in a non-home room.
    if room.name ~= homeRoom then
        local hostiles = room:find(FIND.HOSTILE_CREEPS) or {}
        if #hostiles > 0 then
            moveAcrossExit(creep, homeRoom, nil)
            return
        end
    end

    if not creep.memory.working then
        -- Collecting energy in targetRoom.
        if not targetRoom then
            if getUsedEnergy(creep.store) > 0 then
                creep.memory.working = true
            elseif room.name ~= homeRoom then
                moveAcrossExit(creep, homeRoom, nil)
            end
            return
        end

        if room.name ~= targetRoom then
            if room.name == homeRoom then
                local rIntel = screeps.Memory and screeps.Memory.roomIntel and screeps.Memory.roomIntel[targetRoom]
                if rIntel and (rIntel.hostiles or 0) > 0 then
                    local spawns = room:find(FIND.MY_SPAWNS)
                    local spawn = findClosest(creep.pos, spawns)
                    if spawn and spawn.pos and creep.pos:getRangeTo(spawn) > 4 then
                        creep:moveTo(spawn)
                    end
                    return
                end
            elseif isOnEdge(creep.pos) then
                stepOffEdge(creep)
                return
            end
            moveAcrossExit(creep, targetRoom, nil)
            return
        end

        if isOnEdge(creep.pos) then
            stepOffEdge(creep)
            return
        end

        -- 1. Dropped energy in remote room.
        local dropped = findDroppedEnergy(creep)
        if dropped then
            drawTargetLine(creep, dropped, "#26c6da")
            if creep:pickup(dropped) == ERR.NOT_IN_RANGE then
                creep:moveTo(dropped)
            end
            return
        end

        -- 2. Tombstones with energy.
        local tomb = findEnergyTombstone(creep)
        if tomb then
            drawTargetLine(creep, tomb, "#ff8a65")
            if creep:withdraw(tomb, RESOURCE.ENERGY) == ERR.NOT_IN_RANGE then
                creep:moveTo(tomb)
            end
            return
        end

        -- 3. Remote containers with energy.
        local container = findAnyEnergyContainer(creep)
        if container then
            drawTargetLine(creep, container, "#26c6da")
            if creep:withdraw(container, RESOURCE.ENERGY) == ERR.NOT_IN_RANGE then
                creep:moveTo(container)
            end
            return
        end

        -- 4. If already carrying partial energy and nothing left to gather, head home.
        if getUsedEnergy(creep.store) >= 50 then
            creep.memory.working = true
            return
        end

        -- Otherwise stage near a remoteHarvester or source waiting for drops.
        local myCreeps = room:find(FIND.MY_CREEPS) or {}
        local harvesters = {}
        for _, c in ipairs(myCreeps) do
            if c.memory and c.memory.role == "remoteHarvester" then
                table.insert(harvesters, c)
            end
        end
        local anchor = findClosest(creep.pos, harvesters) or findNearestSource(creep)
        if anchor and anchor.pos and creep.pos:getRangeTo(anchor) > 2 then
            creep:moveTo(anchor)
        end
        return
    end

    -- Delivering energy back to homeRoom.
    if room.name ~= homeRoom then
        moveAcrossExit(creep, homeRoom, nil)
        return
    end

    if isOnEdge(creep.pos) then
        stepOffEdge(creep)
        return
    end

    local target = findEnergyDeliveryTarget(creep, {
        includeControllerContainer = true,
        includeStorage = true,
    })
    if target then
        drawTargetLine(creep, target, "#26c6da")
        if creep:transfer(target, RESOURCE.ENERGY) == ERR.NOT_IN_RANGE then
            creep:moveTo(target)
        end
        return
    end

    -- Fallback: hand off to an upgrader or builder needing energy.
    local myCreeps = room:find(FIND.MY_CREEPS) or {}
    local needy = {}
    for _, c in ipairs(myCreeps) do
        if c.id ~= creep.id and c.memory then
            local r = c.memory.role
            if (r == "upgrader" or r == "builder") and getFreeEnergy(c.store) > 0 then
                table.insert(needy, c)
            end
        end
    end
    local recipient = findClosest(creep.pos, needy)
    if recipient then
        drawTargetLine(creep, recipient, "#80deea")
        if creep:transfer(recipient, RESOURCE.ENERGY) == ERR.NOT_IN_RANGE then
            creep:moveTo(recipient)
        end
        return
    end

    -- Last resort: drop near primary spawn so haulers/upgraders can use it.
    local spawns = room:find(FIND.MY_SPAWNS)
    local spawn = findClosest(creep.pos, spawns)
    if spawn and spawn.pos then
        if creep.pos:getRangeTo(spawn) > 2 then
            creep:moveTo(spawn)
        elseif creep.drop and getUsedEnergy(creep.store) > 0 then
            creep:drop(RESOURCE.ENERGY)
        end
    end
end

-- Helper: inspect staging assault wave in homeRoom and return (stagedList, hasHealer).
local function inspectAssaultWave(room)
    local staged = {}
    local hasHealer = false
    local myCreeps = room:find(FIND.MY_CREEPS) or {}
    for _, c in ipairs(myCreeps) do
        if c.memory then
            local r = c.memory.role
            if (r == "assault" or r == "healer") and c.memory.state ~= "assault" then
                table.insert(staged, c)
                if r == "healer" then
                    hasHealer = true
                end
            end
        end
    end
    return staged, hasHealer
end

-- Helper: shared border-rally & cross-room assault transit for Hivemind counter-raids.
local function handleAssaultTransit(creep)
    local room = creep.room
    local homeRoom = (creep.memory and creep.memory.room) or (room and room.name)
    local rmem = homeRoom and screeps.Memory and screeps.Memory.rooms and screeps.Memory.rooms[homeRoom]
    local targetRoom = (creep.memory and creep.memory.targetRoom) or (rmem and rmem.attackTarget)

    if not targetRoom then
        if homeRoom and room and room.name ~= homeRoom then
            moveAcrossExit(creep, homeRoom, nil)
            return false
        end
        return false
    end
    creep.memory.targetRoom = targetRoom

    if room.name == targetRoom then
        creep.memory.state = "assault"
        if isOnEdge(creep.pos) then
            stepOffEdge(creep)
            return false
        end
        return true
    end

    if room.name == homeRoom then
        -- Defend home immediately if enemy raiders are inside!
        local homeHostiles = room:find(FIND.HOSTILE_CREEPS) or {}
        if #homeHostiles > 0 then
            return true
        end

        local exitDir = room.findExitTo and room:findExitTo(targetRoom)
        if type(exitDir) ~= "number" or exitDir <= 0 then
            return false
        end

        -- Rally near border exit until full wave (2 bruisers + 1-2 healers) is ready!
        if creep.memory.state ~= "assault" then
            local rx, ry = 25, 25
            if exitDir == 1 then
                rx, ry = 25, 4
            elseif exitDir == 3 then
                rx, ry = 45, 25
            elseif exitDir == 5 then
                rx, ry = 25, 45
            elseif exitDir == 7 then
                rx, ry = 4, 25
            end
            local staged, hasHealer = inspectAssaultWave(room)
            local energyCap = room.energyCapacityAvailable or 300
            local waveSize = (energyCap >= 800) and 4 or 3
            if rmem then
                if rmem.assaultGatherStart == nil or #staged == 0 then
                    rmem.assaultGatherStart = screeps.Game.time or 0
                end
            end
            local waited = (screeps.Game.time or 0) - ((rmem and rmem.assaultGatherStart) or (screeps.Game.time or 0))

            if room.visual then
                room.visual:circle(rx, ry, {
                    radius = 0.85,
                    fill = "transparent",
                    stroke = "#42a5f5",
                    opacity = 0.75,
                })
            end

            if (#staged >= waveSize and hasHealer) or (waited >= 220 and #staged >= 2) then
                for _, m in ipairs(staged) do
                    m.memory.state = "assault"
                end
                if rmem then
                    rmem.assaultGatherStart = nil
                end
            else
                if creep.pos:getRangeTo(rx, ry) > 2 then
                    creep:moveTo(rx, ry)
                end
                return false
            end
        end

        moveAcrossExit(creep, targetRoom, exitDir)
        return false
    end

    if isOnEdge(creep.pos) then
        stepOffEdge(creep)
        return false
    end
    moveAcrossExit(creep, targetRoom, nil)
    return false
end

-- Shared focus-fire target selection for Hivemind assault waves.
local function selectAssaultTarget(creep)
    local room = creep.room
    local now = screeps.Game.time or 0
    if screeps.Memory and screeps.Memory.assaultFocusTick == now and screeps.Memory.assaultFocusRoom == room.name and screeps.Memory.assaultFocusId then
        local cached = screeps.Game.getObjectById and screeps.Game.getObjectById(screeps.Memory.assaultFocusId)
        if cached and cached.room and cached.room.name == room.name then
            return cached
        end
    end

    local chosen = nil
    local hostiles = room:find(FIND.HOSTILE_CREEPS) or {}
    if #hostiles > 0 then
        local bestScore = math.mininteger
        for _, h in ipairs(hostiles) do
            local d = creep.pos:getRangeTo(h)
            local hpFrac = (h.hits and h.hitsMax and h.hitsMax > 0) and (h.hits / h.hitsMax) or 1
            local s = 100 - d * 2 + (1 - hpFrac) * 45
            if h.body then
                for _, p in ipairs(h.body) do
                    local pt = (type(p) == "table" and p.type) or p
                    if pt == PART.HEAL then
                        s = s + 60
                        break
                    elseif pt == PART.ATTACK or pt == PART.RANGED_ATTACK then
                        s = s + 35
                        break
                    end
                end
            end
            if s > bestScore then
                bestScore = s
                chosen = h
            end
        end
    end

    if not chosen then
        local hStructs = room:find(FIND.HOSTILE_STRUCTURES) or {}
        local towers, spawns, others = {}, {}, {}
        for _, s in ipairs(hStructs) do
            if s.structureType == STRUCTURE.TOWER then
                table.insert(towers, s)
            elseif s.structureType == STRUCTURE.SPAWN then
                table.insert(spawns, s)
            elseif s.structureType ~= STRUCTURE.CONTROLLER then
                table.insert(others, s)
            end
        end
        if #towers > 0 then
            chosen = findClosest(creep.pos, towers)
        elseif #spawns > 0 then
            chosen = findClosest(creep.pos, spawns)
        elseif #others > 0 then
            chosen = findClosest(creep.pos, others)
        end
    end

    if chosen and chosen.id and screeps.Memory then
        screeps.Memory.assaultFocusTick = now
        screeps.Memory.assaultFocusRoom = room.name
        screeps.Memory.assaultFocusId = chosen.id
    end
    return chosen
end

-- 10. Assault Bruiser: coordinated counter-raid frontline attacker.
function M.assault(creep)
    if not handleAssaultTransit(creep) then
        return
    end
    -- Pull back to friendly healer if critically low HP (< 25%).
    if creep.hits and creep.hitsMax and creep.hits < creep.hitsMax * 0.25 then
        local myCreeps = creep.room:find(FIND.MY_CREEPS) or {}
        for _, ally in ipairs(myCreeps) do
            if ally.id ~= creep.id and ally.memory and ally.memory.role == "healer" then
                creep:moveTo(ally)
                return
            end
        end
    end
    local target = selectAssaultTarget(creep)
    if not target then
        if creep.pos:getRangeTo(25, 25) > 5 then
            creep:moveTo(25, 25)
        end
        return
    end
    drawTargetLine(creep, target, "#ff5252")
    if creep:attack(target) == ERR.NOT_IN_RANGE then
        creep:moveTo(target)
    end
end

-- 11. Combat Healer: hugs frontline assault bruisers and sustains the wave under tower fire.
function M.healer(creep)
    if creep.hits and creep.hitsMax and creep.hits < creep.hitsMax then
        creep:heal(creep)
    end
    if not handleAssaultTransit(creep) then
        return
    end
    local myCreeps = creep.room:find(FIND.MY_CREEPS) or {}
    local wounded = nil
    local lowestRatio = 1.0
    local bruisers = {}
    for _, ally in ipairs(myCreeps) do
        if ally.id ~= creep.id then
            if ally.hits and ally.hitsMax and ally.hitsMax > 0 and ally.hits < ally.hitsMax then
                local r = ally.hits / ally.hitsMax
                if r < lowestRatio then
                    lowestRatio = r
                    wounded = ally
                end
            end
            if ally.memory and ally.memory.role == "assault" then
                table.insert(bruisers, ally)
            end
        end
    end
    if wounded then
        local d = creep.pos:getRangeTo(wounded)
        if d <= 1 then
            creep:heal(wounded)
        elseif d <= 3 then
            creep:rangedHeal(wounded)
            creep:moveTo(wounded)
        else
            creep:moveTo(wounded)
        end
        return
    end
    if #bruisers > 0 then
        local lead = findClosest(creep.pos, bruisers)
        if lead then
            if creep.pos:getRangeTo(lead) <= 1 then
                creep:heal(lead) -- pre-heal frontline bruiser
            else
                creep:moveTo(lead)
            end
        end
        return
    end
    if creep.pos:getRangeTo(25, 25) > 5 then
        creep:moveTo(25, 25)
    end
end

---Dispatcher for running any creep according to creep.memory.role.
---@param creep Creep
function M.run(creep)
    if not creep or not creep.memory then
        return
    end
    if creep.room then
        M.recordRoomIntel(creep.room)
    end
    local role = creep.memory.role or "harvester"
    local fn = M[role]
    if fn then
        fn(creep)
    else
        M.harvester(creep)
    end
end

return M
