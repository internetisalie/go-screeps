-- economy.lua: Object-oriented EconomyAgent for the Raider colony.
-- Roles: harvester, hauler, builder, upgrader.

local screeps <const> = require("screeps")

---@type table<StructureType, integer>
local SITE_PRIORITY <const> = {
    [screeps.STRUCTURE.TOWER or "tower"] = 1,
    [screeps.STRUCTURE.EXTENSION or "extension"] = 2,
    [screeps.STRUCTURE.CONTAINER or "container"] = 3,
    [screeps.STRUCTURE.STORAGE or "storage"] = 4,
    [screeps.STRUCTURE.RAMPART or "rampart"] = 5,
    [screeps.STRUCTURE.ROAD or "road"] = 6,
}

---@class EconomyRoomCache
---@field haulerCount integer|nil
---@field [string] any

local _cacheTick = -1
---@type table<string, EconomyRoomCache>
local _roomCache = {}

---@class EconomyAgent
---@field creep Creep
---@field room Room
---@field memory table<string, any>
local EconomyAgent = {}
EconomyAgent.__index = EconomyAgent

---@param room Room
---@return EconomyRoomCache
function EconomyAgent.getRoomCache(room)
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

---@param room Room
---@param key string
---@param findConst FindConstant
---@return any[]
function EconomyAgent.cachedFind(room, key, findConst)
    local c = EconomyAgent.getRoomCache(room)
    local v = c[key]
    if v == nil then
        v = room:find(findConst) or {}
        c[key] = v
    end
    return v
end

---@param store Store|nil
---@return integer
function EconomyAgent.getUsedEnergy(store)
    if not store or not store.getUsedCapacity then
        return 0
    end
    return store:getUsedCapacity(screeps.RESOURCE.ENERGY) or 0
end

---@param store Store|nil
---@return integer
function EconomyAgent.getFreeEnergy(store)
    if not store or not store.getFreeCapacity then
        return 0
    end
    return store:getFreeCapacity(screeps.RESOURCE.ENERGY) or 0
end

---Build a harvester body: fast mobile worker during bootstrap (no hauler),
---or high-WORK dedicated source miner once haulers are running.
---@param budget integer
---@param hasHauler boolean
---@return BodyPartType[]|nil
function EconomyAgent.buildHarvesterBody(budget, hasHauler)
    if budget < 200 then
        return nil
    end
    if not hasHauler then
        if budget >= 250 then
            return { screeps.PART.WORK, screeps.PART.CARRY, screeps.PART.MOVE, screeps.PART.MOVE } -- 250: 1 tile/tick on plains for fast bootstrap
        end
        return { screeps.PART.WORK, screeps.PART.CARRY, screeps.PART.MOVE } -- 200
    end
    if budget < 300 then
        return { screeps.PART.WORK, screeps.PART.CARRY, screeps.PART.MOVE }
    end

    ---@type BodyPartType[]
    local body = {}
    local cost = 0

    -- Scale WORK up to 5 (full 10 energy/tick source drain).
    local maxWork = math.min(5, (budget - 100) // 100)
    if maxWork < 2 then
        maxWork = 2
    end
    for _ = 1, maxWork do
        table.insert(body, screeps.PART.WORK)
        cost = cost + 100
    end

    if cost + 50 <= budget then
        table.insert(body, screeps.PART.CARRY)
        cost = cost + 50
    end

    local desiredMove = math.max(1, math.min(3, math.ceil(maxWork / 2)))
    for _ = 1, desiredMove do
        if cost + 50 <= budget then
            table.insert(body, screeps.PART.MOVE)
            cost = cost + 50
        end
    end

    return body
end

---Build a fast 1:1 CARRY/MOVE hauler body (never slows down off-road).
---@param budget integer
---@return BodyPartType[]|nil
function EconomyAgent.buildHaulerBody(budget)
    if budget < 150 then
        if budget >= 100 then
            return { screeps.PART.CARRY, screeps.PART.MOVE }
        end
        return nil
    end

    ---@type BodyPartType[]
    local body = {}
    local cost = 0
    while cost + 100 <= budget and #body + 2 <= 16 do
        table.insert(body, screeps.PART.CARRY)
        table.insert(body, screeps.PART.MOVE)
        cost = cost + 100
    end
    if cost + 50 <= budget and #body + 1 <= 16 then
        table.insert(body, screeps.PART.CARRY)
    end
    return body
end

---Build a balanced WORK/CARRY/MOVE body for builders and upgraders.
---@param budget integer
---@return BodyPartType[]|nil
function EconomyAgent.buildWorkerBody(budget)
    if budget < 200 then
        return nil
    end
    ---@type BodyPartType[]
    local body = { screeps.PART.WORK, screeps.PART.CARRY, screeps.PART.MOVE }
    local cost = 200
    while cost + 200 <= budget and #body + 3 <= 15 do
        table.insert(body, screeps.PART.WORK)
        table.insert(body, screeps.PART.CARRY)
        table.insert(body, screeps.PART.MOVE)
        cost = cost + 200
    end
    if cost + 100 <= budget and #body + 2 <= 15 then
        table.insert(body, screeps.PART.CARRY)
        table.insert(body, screeps.PART.MOVE)
    end
    return body
end

---Count haulers currently alive in a room.
---@param room Room
---@return integer
function EconomyAgent.countRoomHaulers(room)
    local c = EconomyAgent.getRoomCache(room)
    if c.haulerCount ~= nil then
        return c.haulerCount
    end
    local count = 0
    local myCreeps = EconomyAgent.cachedFind(room, "myCreeps", screeps.FIND.MY_CREEPS) ---@type Creep[]
    for _, cr in ipairs(myCreeps) do
        if cr.memory and cr.memory.role == "hauler" then
            count = count + 1
        end
    end
    c.haulerCount = count
    return count
end

---Find a container adjacent to a position.
---@param room Room
---@param pos RoomPosition|nil
---@return Structure|nil
function EconomyAgent.findAdjacentContainer(room, pos)
    if not pos then
        return nil
    end
    local structs = EconomyAgent.cachedFind(room, "structures", screeps.FIND.STRUCTURES) ---@type Structure[]
    for _, s in ipairs(structs) do
        if s.structureType == screeps.STRUCTURE.CONTAINER and s.pos and pos:getRangeTo(s) <= 2 then
            return s
        end
    end
    return nil
end

---Construct an EconomyAgent bound to a Creep.
---@param creep Creep
---@return EconomyAgent
function EconomyAgent.new(creep)
    return setmetatable({
        creep = creep,
        room = creep.room,
        memory = creep.memory,
    }, EconomyAgent)
end

---Assign this harvester to the source with fewest harvesters currently assigned.
---@return Source|nil
function EconomyAgent:getAssignedSource()
    local creep = self.creep
    local room = self.room
    if self.memory.sourceId and screeps.Game.getObjectById then
        local existing = screeps.Game.getObjectById(self.memory.sourceId) ---@type Source|nil
        if existing and existing.room and existing.room.name == room.name then
            return existing
        end
    end

    local sources = room:find(screeps.FIND.SOURCES) or {} ---@type Source[]
    if #sources == 0 then
        return nil
    end

    ---@type table<string, integer>
    local assigned = {}
    for _, s in ipairs(sources) do
        assigned[s.id] = 0
    end
    local myCreeps = room:find(screeps.FIND.MY_CREEPS) or {} ---@type Creep[]
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

    self.memory.sourceId = best.id
    return best
end

---Find spawn/extension/tower/storage needing energy delivery.
---@param includeTowers boolean
---@param includeStorage boolean
---@return Structure|nil
function EconomyAgent:findRefillTarget(includeTowers, includeStorage)
    local creep = self.creep
    local myStructs = EconomyAgent.cachedFind(self.room, "myStructures", screeps.FIND.MY_STRUCTURES) ---@type Structure[]

    ---@type Structure[]
    local primary = {}
    ---@type Structure[]
    local towers = {}
    ---@type Structure|nil
    local storage = nil

    for _, s in ipairs(myStructs) do
        if s.structureType == screeps.STRUCTURE.SPAWN or s.structureType == screeps.STRUCTURE.EXTENSION then
            if EconomyAgent.getFreeEnergy(s.store) > 0 then
                table.insert(primary, s)
            end
        elseif includeTowers and s.structureType == screeps.STRUCTURE.TOWER then
            local used = EconomyAgent.getUsedEnergy(s.store)
            local free = EconomyAgent.getFreeEnergy(s.store)
            if free > 0 and used < 750 then
                table.insert(towers, s)
            end
        elseif includeStorage and s.structureType == screeps.STRUCTURE.STORAGE then
            if EconomyAgent.getFreeEnergy(s.store) > 0 then
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

---Gather energy for haulers, builders, and upgraders.
---@param allowHarvest boolean
---@param allowStorage boolean
---@return boolean
function EconomyAgent:acquireEnergy(allowHarvest, allowStorage)
    local creep = self.creep
    local room = self.room

    -- 1. Dropped energy resources (highest efficiency: zero decay waste).
    local dropped = EconomyAgent.cachedFind(room, "dropped", screeps.FIND.DROPPED_RESOURCES) ---@type Resource[]
    ---@type Resource[]
    local goodDrops = {}
    for _, r in ipairs(dropped) do
        if r.resourceType == screeps.RESOURCE.ENERGY and (r.amount or 0) >= 25 then
            table.insert(goodDrops, r)
        end
    end
    if #goodDrops > 0 then
        local target = creep.pos:findClosestByRange(goodDrops) or goodDrops[1]
        if creep:pickup(target) == screeps.ERR.NOT_IN_RANGE then
            creep:moveTo(target)
        end
        return true
    end

    -- 2. Tombstones with energy.
    if screeps.FIND.TOMBSTONES then
        local tombs = EconomyAgent.cachedFind(room, "tombs", screeps.FIND.TOMBSTONES) ---@type Tombstone[]
        ---@type Tombstone[]
        local goodTombs = {}
        for _, t in ipairs(tombs) do
            if EconomyAgent.getUsedEnergy(t.store) >= 20 then
                table.insert(goodTombs, t)
            end
        end
        if #goodTombs > 0 then
            local target = creep.pos:findClosestByRange(goodTombs) or goodTombs[1]
            if creep:withdraw(target, screeps.RESOURCE.ENERGY) == screeps.ERR.NOT_IN_RANGE then
                creep:moveTo(target)
            end
            return true
        end
    end

    -- 3. Source containers with energy.
    local structs = EconomyAgent.cachedFind(room, "structures", screeps.FIND.STRUCTURES) ---@type Structure[]
    ---@type Structure[]
    local containers = {}
    ---@type Structure|nil
    local storage = nil
    for _, s in ipairs(structs) do
        if s.structureType == screeps.STRUCTURE.CONTAINER and EconomyAgent.getUsedEnergy(s.store) >= 50 then
            table.insert(containers, s)
        elseif allowStorage and s.structureType == screeps.STRUCTURE.STORAGE and EconomyAgent.getUsedEnergy(s.store) >= 100 then
            storage = s
        end
    end
    if #containers > 0 then
        local target = creep.pos:findClosestByRange(containers) or containers[1]
        if creep:withdraw(target, screeps.RESOURCE.ENERGY) == screeps.ERR.NOT_IN_RANGE then
            creep:moveTo(target)
        end
        return true
    end

    -- 4. Storage (when allowed).
    if storage then
        if creep:withdraw(storage, screeps.RESOURCE.ENERGY) == screeps.ERR.NOT_IN_RANGE then
            creep:moveTo(storage)
        end
        return true
    end

    -- 5. Direct harvest fallback for workers when containers aren't built yet.
    if allowHarvest then
        local sources = room:find(screeps.FIND.SOURCES_ACTIVE) or room:find(screeps.FIND.SOURCES) or {} ---@type Source[]
        if #sources > 0 then
            local src = creep.pos:findClosestByRange(sources) or sources[1]
            if creep:harvest(src) == screeps.ERR.NOT_IN_RANGE then
                creep:moveTo(src)
            end
            return true
        end
    end

    return false
end

---Role: Harvester (dedicated source miner; self-delivers when no haulers exist).
function EconomyAgent:runHarvester()
    local creep = self.creep
    local room = self.room
    local source = self:getAssignedSource()
    if not source then
        return
    end

    local haulers = EconomyAgent.countRoomHaulers(room)
    local container = EconomyAgent.findAdjacentContainer(room, source.pos)
    local used = EconomyAgent.getUsedEnergy(creep.store)
    local free = EconomyAgent.getFreeEnergy(creep.store)

    -- If haulers exist, act as a dedicated static/drop miner!
    if haulers > 0 then
        if container and container.pos then
            if creep.pos:getRangeTo(container) > 0 and creep.pos:getRangeTo(source) > 1 then
                creep:moveTo(container)
                return
            end
        end

        if free > 0 then
            if creep:harvest(source) == screeps.ERR.NOT_IN_RANGE then
                creep:moveTo(source)
            end
        elseif container and EconomyAgent.getFreeEnergy(container.store) > 0 then
            if creep:transfer(container, screeps.RESOURCE.ENERGY) == screeps.ERR.NOT_IN_RANGE then
                creep:moveTo(container)
            end
        elseif creep.drop then
            creep:drop(screeps.RESOURCE.ENERGY)
        end
        return
    end

    -- Bootstrap mode (no haulers yet): harvest and deliver directly to spawn/extensions.
    if self.memory.delivering and used == 0 then
        self.memory.delivering = false
    elseif not self.memory.delivering and free == 0 then
        self.memory.delivering = true
    end

    if not self.memory.delivering then
        if creep:harvest(source) == screeps.ERR.NOT_IN_RANGE then
            creep:moveTo(source)
        end
    else
        local target = self:findRefillTarget(true, false)
        if target then
            if creep:transfer(target, screeps.RESOURCE.ENERGY) == screeps.ERR.NOT_IN_RANGE then
                creep:moveTo(target)
            end
        elseif room.controller and room.controller.my then
            if creep:upgradeController(room.controller) == screeps.ERR.NOT_IN_RANGE then
                creep:moveTo(room.controller)
            end
        end
    end
end

---Role: Hauler (fast energy pipeline from sources/drops -> spawn/extensions/towers/storage).
function EconomyAgent:runHauler()
    local creep = self.creep
    local room = self.room
    local used = EconomyAgent.getUsedEnergy(creep.store)
    local free = EconomyAgent.getFreeEnergy(creep.store)

    if self.memory.delivering and used == 0 then
        self.memory.delivering = false
    elseif not self.memory.delivering and (free == 0 or (used >= 50 and self:findRefillTarget(false, false) ~= nil)) then
        self.memory.delivering = true
    end

    if self.memory.delivering then
        local target = self:findRefillTarget(true, true)
        if target then
            if creep:transfer(target, screeps.RESOURCE.ENERGY) == screeps.ERR.NOT_IN_RANGE then
                creep:moveTo(target)
            end
            return
        end
        -- If everything is full, feed an upgrader creep near the controller.
        local myCreeps = EconomyAgent.cachedFind(room, "myCreeps", screeps.FIND.MY_CREEPS) ---@type Creep[]
        ---@type Creep[]
        local upgraders = {}
        for _, c in ipairs(myCreeps) do
            if c.memory and c.memory.role == "upgrader" and EconomyAgent.getFreeEnergy(c.store) >= 25 then
                table.insert(upgraders, c)
            end
        end
        if #upgraders > 0 then
            local u = creep.pos:findClosestByRange(upgraders) or upgraders[1]
            if creep:transfer(u, screeps.RESOURCE.ENERGY) == screeps.ERR.NOT_IN_RANGE then
                creep:moveTo(u)
            end
        end
    else
        -- Only withdraw from storage if spawn/extensions/towers need refilling.
        local needRefill = self:findRefillTarget(true, false) ~= nil
        local got = self:acquireEnergy(false, needRefill)
        if not got then
            -- Park near a harvester to catch dropped energy.
            local myCreeps = EconomyAgent.cachedFind(room, "myCreeps", screeps.FIND.MY_CREEPS) ---@type Creep[]
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

---Role: Builder (fast construction + critical repair + upgrade fallback).
function EconomyAgent:runBuilder()
    local creep = self.creep
    local room = self.room
    local used = EconomyAgent.getUsedEnergy(creep.store)
    local free = EconomyAgent.getFreeEnergy(creep.store)

    if self.memory.working and used == 0 then
        self.memory.working = false
    elseif not self.memory.working and free == 0 then
        self.memory.working = true
    end

    if not self.memory.working then
        self:acquireEnergy(true, true)
        return
    end

    -- 1. Urgent repairs (critical ramparts, damaged towers/spawns/containers).
    local structs = room:find(screeps.FIND.STRUCTURES) or {} ---@type Structure[]
    ---@type Structure|nil
    local urgent = nil
    local urgentScore = math.maxinteger
    for _, s in ipairs(structs) do
        if s.hits and s.hitsMax and s.hitsMax > 0 then
            if s.structureType == screeps.STRUCTURE.RAMPART and s.hits < 5000 then
                if s.hits < urgentScore then
                    urgentScore = s.hits
                    urgent = s
                end
            elseif (s.structureType == screeps.STRUCTURE.CONTAINER or s.structureType == screeps.STRUCTURE.TOWER or s.structureType == screeps.STRUCTURE.SPAWN) and s.hits < s.hitsMax * 0.65 then
                if s.hits < urgentScore then
                    urgentScore = s.hits
                    urgent = s
                end
            end
        end
    end
    if urgent then
        if creep:repair(urgent) == screeps.ERR.NOT_IN_RANGE then
            creep:moveTo(urgent)
        end
        return
    end

    -- 2. Prioritized construction sites (Tower > Extension > Container > Storage > Rampart > Road).
    local sites = room:find(screeps.FIND.MY_CONSTRUCTION_SITES) or room:find(screeps.FIND.CONSTRUCTION_SITES) or {} ---@type ConstructionSite[]
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
        if creep:build(bestSite) == screeps.ERR.NOT_IN_RANGE then
            creep:moveTo(bestSite)
        end
        return
    end

    -- 3. General maintenance repairs.
    ---@type Structure[]
    local repairable = {}
    for _, s in ipairs(structs) do
        if s.hits and s.hitsMax and s.hitsMax > 0 then
            if s.structureType == screeps.STRUCTURE.WALL or s.structureType == screeps.STRUCTURE.RAMPART then
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
        if creep:repair(target) == screeps.ERR.NOT_IN_RANGE then
            creep:moveTo(target)
        end
        return
    end

    -- 4. Fallback: help upgrade controller so builders are never idle.
    if room.controller and room.controller.my then
        if creep:upgradeController(room.controller) == screeps.ERR.NOT_IN_RANGE then
            creep:moveTo(room.controller)
        end
    end
end

---Role: Upgrader (fast RCL progression).
function EconomyAgent:runUpgrader()
    local creep = self.creep
    local room = self.room
    local used = EconomyAgent.getUsedEnergy(creep.store)
    local free = EconomyAgent.getFreeEnergy(creep.store)

    if self.memory.working and used == 0 then
        self.memory.working = false
    elseif not self.memory.working and free == 0 then
        self.memory.working = true
    end

    if not self.memory.working then
        self:acquireEnergy(true, true)
    else
        local ctrl = room.controller
        if ctrl and ctrl.my then
            if creep:upgradeController(ctrl) == screeps.ERR.NOT_IN_RANGE then
                creep:moveTo(ctrl)
            end
        end
    end
end

---Dispatch this economy creep to its assigned role behavior.
function EconomyAgent:execute()
    local role = self.memory.role
    if role == "harvester" then
        self:runHarvester()
    elseif role == "hauler" then
        self:runHauler()
    elseif role == "builder" then
        self:runBuilder()
    elseif role == "upgrader" then
        self:runUpgrader()
    end
end

---Static convenience entry point to run an economy creep for one tick.
---@param creep Creep|nil
function EconomyAgent.run(creep)
    if not creep or creep.spawning or not creep.memory then
        return
    end
    EconomyAgent.new(creep):execute()
end

return EconomyAgent
