-- roles.lua: Object-oriented colony creep role behaviors for Screeps.
-- Roles: harvester, miner, hauler, upgrader, builder, defender, scout, remoteHarvester, remoteHauler, remoteBuilder, mineralHarvester, assault, dismantler, healer.

local screeps <const> = require("screeps")
local RoomPlanner <const> = require("planner")

---@class RoomIntelEntry
---@field owner? string
---@field sources integer
---@field hostiles integer
---@field exits? table<string, string>
---@field lastSeen integer

---@class DeliveryOptions
---@field includeControllerContainer? boolean
---@field includeStorage? boolean
---@field includeContainers? boolean

---@class CreepAgent
---@field creep Creep
---@field room Room
---@field memory table<string, any>
local CreepAgent = {}
CreepAgent.__index = CreepAgent

---@type table<integer, FindConstant>
local EXIT_DIRS <const> = {
    [1] = screeps.FIND.EXIT_TOP,
    [3] = screeps.FIND.EXIT_RIGHT,
    [5] = screeps.FIND.EXIT_BOTTOM,
    [7] = screeps.FIND.EXIT_LEFT,
}

local SCOUT_INTEL_TTL <const> = 1500
local HOSTILE_INTEL_TTL <const> = 250

---Construct a CreepAgent bound to a specific owned Creep.
---@param creep Creep
---@return CreepAgent
function CreepAgent.new(creep)
    return setmetatable({
        creep = creep,
        room = creep.room,
        memory = creep.memory,
    }, CreepAgent)
end

---Return true if the creep has at least one active (hits > 0) body part of `partType`.
---@param creep? Creep
---@param partType BodyPartType
---@return boolean
function CreepAgent.hasActivePart(creep, partType)
    if not creep or not creep.body then
        return false
    end
    for _, part in ipairs(creep.body) do
        local pt = (type(part) == "table" and part.type) or part
        local hits = (type(part) == "table" and part.hits) or nil
        if pt == partType and (hits == nil or hits > 0) then
            return true
        end
    end
    return false
end

---@param val number
---@param lo number
---@param hi number
---@return number
function CreepAgent.clamp(val, lo, hi)
    if val < lo then
        return lo
    end
    if val > hi then
        return hi
    end
    return val
end

---@param pos? RoomPosition
---@return boolean
function CreepAgent.isOnEdge(pos)
    if not pos then
        return false
    end
    return pos.x <= 0 or pos.x >= 49 or pos.y <= 0 or pos.y >= 49
end

---@param room? Room
---@param targetRoom? string
---@return FindConstant|nil
function CreepAgent.resolveExitDir(room, targetRoom)
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

---Record visible room intelligence (owner, source count, hostiles, tick) into Memory.roomIntel.
---@param room? Room
function CreepAgent.recordRoomIntel(room)
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

    local sources = room:find(screeps.FIND.SOURCES) or {} ---@type Source[]
    local hostiles = room:find(screeps.FIND.HOSTILE_CREEPS) or {} ---@type Creep[]
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

    local exits = nil
    if screeps.Game.map and screeps.Game.map.describeExits then
        local rawExits = screeps.Game.map.describeExits(room.name)
        if type(rawExits) == "table" then
            exits = rawExits
        end
    end

    screeps.Memory.roomIntel[room.name] = {
        sources = #sources,
        owner = owner,
        hostiles = #hostiles,
        exits = exits,
        lastSeen = now,
    }
end

---@param store? Store
---@return integer
function CreepAgent.getUsedEnergy(store)
    if not store then
        return 0
    end
    return store:getUsedCapacity(screeps.RESOURCE.ENERGY) or 0
end

---@param store? Store
---@return integer
function CreepAgent.getFreeEnergy(store)
    if not store then
        return 0
    end
    return store:getFreeCapacity(screeps.RESOURCE.ENERGY) or 0
end

---@generic T
---@param pos RoomPosition
---@param list? T[]
---@return T|nil
function CreepAgent.findClosest(pos, list)
    if not list or #list == 0 then
        return nil
    end
    return pos:findClosestByRange(list)
end

---@param room Room
---@return StructureStorage|nil
function CreepAgent.findRoomStorage(room)
    local myStructs = room:find(screeps.FIND.MY_STRUCTURES) ---@type Structure[]
    if not myStructs then
        return nil
    end
    for _, s in ipairs(myStructs) do
        if s.structureType == screeps.STRUCTURE.STORAGE then
            return s --[[@as StructureStorage]]
        end
    end
    return nil
end

---@param room Room
---@param anchorPos? RoomPosition
---@param maxRange integer
---@param minEnergy? integer
---@return StructureContainer[]
function CreepAgent.findContainersNear(room, anchorPos, maxRange, minEnergy)
    if not anchorPos then
        return {}
    end
    local structs = room:find(screeps.FIND.STRUCTURES) or {} ---@type Structure[]
    ---@type StructureContainer[]
    local matches = {}
    for _, s in ipairs(structs) do
        if s.structureType == screeps.STRUCTURE.CONTAINER and s.pos then
            if anchorPos:getRangeTo(s) <= maxRange then
                if minEnergy == nil or CreepAgent.getUsedEnergy(s.store) >= minEnergy then
                    table.insert(matches, s --[[@as StructureContainer]])
                end
            end
        end
    end
    return matches
end

---@param room Room
---@param minEnergy? integer
---@return StructureContainer[]
function CreepAgent.findSourceContainers(room, minEnergy)
    local sources = room:find(screeps.FIND.SOURCES) or {} ---@type Source[]
    local structs = room:find(screeps.FIND.STRUCTURES) or {} ---@type Structure[]
    ---@type StructureContainer[]
    local matches = {}

    for _, s in ipairs(structs) do
        if s.structureType == screeps.STRUCTURE.CONTAINER and s.pos then
            if minEnergy == nil or CreepAgent.getUsedEnergy(s.store) >= minEnergy then
                for _, src in ipairs(sources) do
                    if src.pos and src.pos:getRangeTo(s) <= 2 then
                        table.insert(matches, s --[[@as StructureContainer]])
                        break
                    end
                end
            end
        end
    end
    return matches
end

---@param room Room
---@return boolean
function CreepAgent.roomNeedsCriticalEnergy(room)
    local myStructures = room:find(screeps.FIND.MY_STRUCTURES) or {} ---@type Structure[]
    for _, s in ipairs(myStructures) do
        if s.structureType == screeps.STRUCTURE.SPAWN or s.structureType == screeps.STRUCTURE.EXTENSION then
            if CreepAgent.getFreeEnergy(s.store) > 0 then
                return true
            end
        elseif s.structureType == screeps.STRUCTURE.TOWER then
            if CreepAgent.getFreeEnergy(s.store) > 100 then
                return true
            end
        end
    end
    return false
end

---Construction site priority (lower number = higher priority).
---@param stype StructureType
---@return integer
function CreepAgent.sitePriority(stype)
    if stype == screeps.STRUCTURE.SPAWN or stype == screeps.STRUCTURE.EXTENSION or stype == screeps.STRUCTURE.TOWER then
        return 1
    elseif stype == screeps.STRUCTURE.CONTAINER or stype == screeps.STRUCTURE.STORAGE or stype == screeps.STRUCTURE.LINK then
        return 2
    elseif stype == screeps.STRUCTURE.ROAD then
        return 3
    elseif stype == screeps.STRUCTURE.RAMPART then
        return 4
    end
    return 5
end

---Pick the next adjacent connected room of `homeRoomName` needing intel refresh.
---@param homeRoomName? string
---@return string|nil
function CreepAgent.pickNextScoutRoom(homeRoomName)
    if not homeRoomName or not screeps.Game.map or not screeps.Game.map.describeExits then
        return nil
    end
    local exits = screeps.Game.map.describeExits(homeRoomName)
    if type(exits) ~= "table" then
        return nil
    end
    ---@type string[]
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
    -- Prioritize 1-hop neighbors first.
    for _, rName in ipairs(candidates) do
        local info = intelMap[rName]
        local seen = (info and info.lastSeen) or -9999
        local ttl = (info and (info.hostiles or 0) > 0) and HOSTILE_INTEL_TTL or SCOUT_INTEL_TTL
        if (not info) or (now - seen > ttl) then
            if seen < oldestSeen then
                oldestSeen = seen
                bestRoom = rName
            end
        end
    end
    if bestRoom then
        return bestRoom
    end

    -- Multi-hop (2-hop) scouting: check neighbors of known 1-hop rooms.
    local hop2Seen = { [homeRoomName] = true } ---@type table<string, boolean>
    for _, rName in ipairs(candidates) do
        hop2Seen[rName] = true
    end
    local hop2Candidates = {} ---@type string[]
    for _, rName in ipairs(candidates) do
        local info = intelMap[rName]
        if info and type(info.exits) == "table" then
            for _, nextName in pairs(info.exits) do
                if type(nextName) == "string" and nextName ~= "" and not hop2Seen[nextName] then
                    hop2Seen[nextName] = true
                    table.insert(hop2Candidates, nextName)
                end
            end
        end
    end
    table.sort(hop2Candidates)
    for _, rName in ipairs(hop2Candidates) do
        local info = intelMap[rName]
        local seen = (info and info.lastSeen) or -9999
        local ttl = (info and (info.hostiles or 0) > 0) and HOSTILE_INTEL_TTL or SCOUT_INTEL_TTL
        if (not info) or (now - seen > ttl) then
            if seen < oldestSeen then
                oldestSeen = seen
                bestRoom = rName
            end
        end
    end
    return bestRoom
end

---Inspect staging assault wave in homeRoom and return (stagedList, hasHealer).
---@param room Room
---@return Creep[] staged
---@return boolean hasHealer
function CreepAgent.inspectAssaultWave(room)
    ---@type Creep[]
    local staged = {}
    local hasHealer = false
    local myCreeps = room:find(screeps.FIND.MY_CREEPS) or {} ---@type Creep[]
    for _, c in ipairs(myCreeps) do
        if not c.spawning and c.memory then
            local r = c.memory.role
            if (r == "assault" or r == "dismantler" or r == "healer") and c.memory.state ~= "assault" then
                table.insert(staged, c)
                if r == "healer" then
                    hasHealer = true
                end
            end
        end
    end
    return staged, hasHealer
end

---@return boolean
function CreepAgent:stepOffEdge()
    local creep = self.creep
    if not creep or not creep.pos then
        return false
    end
    local x, y = creep.pos.x, creep.pos.y
    if x <= 0 then
        creep:moveTo(2, CreepAgent.clamp(y, 2, 47))
        return true
    elseif x >= 49 then
        creep:moveTo(47, CreepAgent.clamp(y, 2, 47))
        return true
    elseif y <= 0 then
        creep:moveTo(CreepAgent.clamp(x, 2, 47), 2)
        return true
    elseif y >= 49 then
        creep:moveTo(CreepAgent.clamp(x, 2, 47), 47)
        return true
    end
    return false
end

---@param targetRoom? string
---@param fallbackDir? FindConstant
---@return boolean
function CreepAgent:moveAcrossExit(targetRoom, fallbackDir)
    local creep = self.creep
    local room = self.room
    if not room then
        return false
    end
    local exitDir = nil
    if targetRoom then
        exitDir = CreepAgent.resolveExitDir(room, targetRoom)
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

---@param target? RoomObject
---@param color string
function CreepAgent:drawTargetLine(target, color)
    local creep = self.creep
    if not target or not target.pos or not self.room or not self.room.visual then
        return
    end
    self.room.visual:line(
        creep.pos.x,
        creep.pos.y,
        target.pos.x,
        target.pos.y,
        { color = color, width = 0.08, opacity = 0.35 }
    )
end

function CreepAgent:updateWorkingState()
    local used = CreepAgent.getUsedEnergy(self.creep.store)
    local free = CreepAgent.getFreeEnergy(self.creep.store)

    if self.memory.working and used == 0 then
        self.memory.working = false
        self.memory.fromStorage = false
    elseif (not self.memory.working) and free == 0 then
        self.memory.working = true
    end
end

---@return Source|nil
function CreepAgent:findNearestSource()
    local sources = self.room:find(screeps.FIND.SOURCES) ---@type Source[]
    if not sources or #sources == 0 then
        return nil
    end

    ---@type Source[]
    local active = {}
    for _, src in ipairs(sources) do
        if src.energy == nil or src.energy > 0 then
            table.insert(active, src)
        end
    end

    if #active > 0 then
        return CreepAgent.findClosest(self.creep.pos, active)
    end
    return CreepAgent.findClosest(self.creep.pos, sources)
end

---@return Resource|nil
function CreepAgent:findDroppedEnergy()
    local dropped = self.room:find(screeps.FIND.DROPPED_RESOURCES) ---@type Resource[]
    if not dropped or #dropped == 0 then
        return nil
    end

    ---@type Resource[]
    local pile = {}
    ---@type Resource[]
    local small = {}
    for _, res in ipairs(dropped) do
        if res.resourceType == nil or res.resourceType == screeps.RESOURCE.ENERGY then
            if res.amount == nil or res.amount >= 25 then
                table.insert(pile, res)
            else
                table.insert(small, res)
            end
        end
    end

    if #pile > 0 then
        return CreepAgent.findClosest(self.creep.pos, pile)
    end
    return CreepAgent.findClosest(self.creep.pos, small)
end

---@return Tombstone|nil
function CreepAgent:findEnergyTombstone()
    if not screeps.FIND.TOMBSTONES then
        return nil
    end
    local tombs = self.room:find(screeps.FIND.TOMBSTONES) ---@type Tombstone[]
    if not tombs or #tombs == 0 then
        return nil
    end
    ---@type Tombstone[]
    local rich = {}
    for _, t in ipairs(tombs) do
        if CreepAgent.getUsedEnergy(t.store) > 0 then
            table.insert(rich, t)
        end
    end
    return CreepAgent.findClosest(self.creep.pos, rich)
end

---@return StructureContainer|nil
function CreepAgent:findAnyEnergyContainer()
    local structures = self.room:find(screeps.FIND.STRUCTURES) ---@type Structure[]
    if not structures or #structures == 0 then
        return nil
    end

    ---@type StructureContainer[]
    local rich = {}
    ---@type StructureContainer[]
    local any = {}
    for _, s in ipairs(structures) do
        if s.structureType == screeps.STRUCTURE.CONTAINER then
            local energy = CreepAgent.getUsedEnergy(s.store)
            if energy >= 50 then
                table.insert(rich, s --[[@as StructureContainer]])
            elseif energy > 0 then
                table.insert(any, s --[[@as StructureContainer]])
            end
        end
    end

    if #rich > 0 then
        return CreepAgent.findClosest(self.creep.pos, rich)
    end
    return CreepAgent.findClosest(self.creep.pos, any)
end

---Find highest-priority structure needing energy (spawns/extensions -> towers -> optional storage/containers).
---@param opts? DeliveryOptions
---@return Structure|nil
function CreepAgent:findEnergyDeliveryTarget(opts)
    opts = opts or {}
    local creep = self.creep
    local room = self.room
    local myStructures = room:find(screeps.FIND.MY_STRUCTURES) ---@type Structure[]
    ---@type Structure[]
    local spawnOrExt = {}
    ---@type Structure[]
    local towers = {}
    ---@type StructureStorage|nil
    local storage = nil

    if myStructures then
        for _, s in ipairs(myStructures) do
            if s.structureType == screeps.STRUCTURE.SPAWN or s.structureType == screeps.STRUCTURE.EXTENSION then
                if CreepAgent.getFreeEnergy(s.store) > 0 then
                    table.insert(spawnOrExt, s)
                end
            elseif s.structureType == screeps.STRUCTURE.TOWER then
                if CreepAgent.getFreeEnergy(s.store) > 50 then
                    table.insert(towers, s)
                end
            elseif s.structureType == screeps.STRUCTURE.STORAGE then
                storage = s --[[@as StructureStorage]]
            end
        end
    end

    if #spawnOrExt > 0 then
        return CreepAgent.findClosest(creep.pos, spawnOrExt)
    end
    if #towers > 0 then
        return CreepAgent.findClosest(creep.pos, towers)
    end

    if opts.includeControllerContainer and room.controller and room.controller.pos then
        local ctrlContainers = CreepAgent.findContainersNear(room, room.controller.pos, 3, nil)
        ---@type StructureContainer[]
        local needyCtrl = {}
        for _, c in ipairs(ctrlContainers) do
            if CreepAgent.getFreeEnergy(c.store) >= 50 then
                table.insert(needyCtrl, c)
            end
        end
        if #needyCtrl > 0 then
            return CreepAgent.findClosest(creep.pos, needyCtrl)
        end
    end

    if opts.includeStorage and storage and (not self.memory.fromStorage) and CreepAgent.getFreeEnergy(storage.store) > 0 then
        return storage
    end

    if opts.includeContainers then
        local structures = room:find(screeps.FIND.STRUCTURES) or {} ---@type Structure[]
        ---@type StructureContainer[]
        local containers = {}
        for _, s in ipairs(structures) do
            if s.structureType == screeps.STRUCTURE.CONTAINER and CreepAgent.getFreeEnergy(s.store) > 0 then
                table.insert(containers, s --[[@as StructureContainer]])
            end
        end
        if #containers > 0 then
            return CreepAgent.findClosest(creep.pos, containers)
        end
    end

    return nil
end

---@return boolean
function CreepAgent:upgradeRoomController()
    local ctrl = self.room.controller
    if not ctrl then
        return false
    end
    self:drawTargetLine(ctrl, "#7e57c2")
    if self.creep:upgradeController(ctrl) == screeps.ERR.NOT_IN_RANGE then
        self.creep:moveTo(ctrl)
    end
    return true
end

---Generic energy acquisition for workers (dropped -> storage -> container -> source).
---@param preferControllerSupply boolean
---@return boolean
function CreepAgent:acquireWorkerEnergy(preferControllerSupply)
    local creep = self.creep
    local room = self.room
    local ctrl = room.controller

    -- Upgraders prefer controller link or controller container first.
    if preferControllerSupply and ctrl and ctrl.pos then
        local myStructs = room:find(screeps.FIND.MY_STRUCTURES) or {} ---@type Structure[]
        for _, s in ipairs(myStructs) do
            if s.structureType == screeps.STRUCTURE.LINK and s.pos and ctrl.pos:getRangeTo(s) <= 3 then
                if CreepAgent.getUsedEnergy(s.store) > 0 then
                    self:drawTargetLine(s, "#b388ff")
                    if creep:withdraw(s, screeps.RESOURCE.ENERGY) == screeps.ERR.NOT_IN_RANGE then
                        creep:moveTo(s)
                    end
                    return true
                end
            end
        end

        local ctrlContainers = CreepAgent.findContainersNear(room, ctrl.pos, 3, 1)
        local c = CreepAgent.findClosest(creep.pos, ctrlContainers)
        if c then
            self:drawTargetLine(c, "#ffca28")
            if creep:withdraw(c, screeps.RESOURCE.ENERGY) == screeps.ERR.NOT_IN_RANGE then
                creep:moveTo(c)
            end
            return true
        end
    end

    local storage = CreepAgent.findRoomStorage(room)
    if storage and CreepAgent.getUsedEnergy(storage.store) >= 100 then
        self:drawTargetLine(storage, "#ffd54f")
        if creep:withdraw(storage, screeps.RESOURCE.ENERGY) == screeps.ERR.NOT_IN_RANGE then
            creep:moveTo(storage)
        end
        return true
    end

    local dropped = self:findDroppedEnergy()
    if dropped then
        self:drawTargetLine(dropped, "#ffd54f")
        if creep:pickup(dropped) == screeps.ERR.NOT_IN_RANGE then
            creep:moveTo(dropped)
        end
        return true
    end

    local container = self:findAnyEnergyContainer()
    if container then
        self:drawTargetLine(container, "#ffca28")
        if creep:withdraw(container, screeps.RESOURCE.ENERGY) == screeps.ERR.NOT_IN_RANGE then
            creep:moveTo(container)
        end
        return true
    end

    local source = self:findNearestSource()
    if source then
        self:drawTargetLine(source, "#ffe082")
        if creep:harvest(source) == screeps.ERR.NOT_IN_RANGE then
            creep:moveTo(source)
        end
        return true
    end

    return false
end

---1. Harvester: bootstrap / fallback mobile harvester.
function CreepAgent:harvester()
    self:updateWorkingState()

    if not self.memory.working then
        local source = self:findNearestSource()
        if source then
            self:drawTargetLine(source, "#ffe082")
            if self.creep:harvest(source) == screeps.ERR.NOT_IN_RANGE then
                self.creep:moveTo(source)
            end
        end
        return
    end

    local target = self:findEnergyDeliveryTarget({
        includeStorage = true,
        includeContainers = true,
    })
    if target then
        self:drawTargetLine(target, "#ffeb3b")
        if self.creep:transfer(target, screeps.RESOURCE.ENERGY) == screeps.ERR.NOT_IN_RANGE then
            self.creep:moveTo(target)
        end
        return
    end

    self:upgradeRoomController()
end

---Assign a miner to the source in its room with the fewest assigned miners.
---@return Source|nil
function CreepAgent:resolveMinerSource()
    if self.memory.sourceId and screeps.Game.getObjectById then
        local existing = screeps.Game.getObjectById(self.memory.sourceId) --[[@as Source|nil]]
        if existing then
            return existing
        end
        self.memory.sourceId = nil
    end

    local sources = self.room:find(screeps.FIND.SOURCES) or {} ---@type Source[]
    if #sources == 0 then
        return nil
    end

    ---@type table<string, integer>
    local assignedCounts = {}
    for _, src in ipairs(sources) do
        assignedCounts[src.id] = 0
    end

    local myCreeps = self.room:find(screeps.FIND.MY_CREEPS) or {} ---@type Creep[]
    for _, c in ipairs(myCreeps) do
        if c.id ~= self.creep.id and c.memory and c.memory.role == "miner" and c.memory.sourceId then
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

    self.memory.sourceId = chosen.id
    return chosen
end

---2. Miner: high-WORK static source miner that sits on a source container and harvests continuously.
function CreepAgent:miner()
    local creep = self.creep
    local source = self:resolveMinerSource()
    if not source then
        return
    end

    -- Locate a container adjacent to the assigned source.
    local containers = CreepAgent.findContainersNear(self.room, source.pos, 1, nil)
    local container = containers[1]

    if container and container.pos then
        if creep.pos.x ~= container.pos.x or creep.pos.y ~= container.pos.y then
            creep:moveTo(container)
            return
        end
    end

    -- If carrying energy and an adjacent source link has free capacity, dump into the link.
    if CreepAgent.getUsedEnergy(creep.store) > 0 then
        local myStructs = self.room:find(screeps.FIND.MY_STRUCTURES) or {} ---@type Structure[]
        for _, s in ipairs(myStructs) do
            if s.structureType == screeps.STRUCTURE.LINK and s.pos and creep.pos:getRangeTo(s) <= 1 then
                if CreepAgent.getFreeEnergy(s.store) > 0 then
                    self:drawTargetLine(s, "#b388ff")
                    creep:transfer(s, screeps.RESOURCE.ENERGY)
                    break
                end
            end
        end
    end

    self:drawTargetLine(source, "#ffd54f")
    if creep:harvest(source) == screeps.ERR.NOT_IN_RANGE then
        creep:moveTo(source)
    end
end

---3. Hauler: collects dropped energy, loots tombstones, withdraws from source containers/storage,
---and delivers to spawns, extensions, towers, controller container, and storage.
function CreepAgent:hauler()
    local creep = self.creep
    local room = self.room
    self:updateWorkingState()

    if not self.memory.working then
        -- 1. Dropped energy.
        local dropped = self:findDroppedEnergy()
        if dropped then
            self:drawTargetLine(dropped, "#ffd54f")
            if creep:pickup(dropped) == screeps.ERR.NOT_IN_RANGE then
                creep:moveTo(dropped)
            end
            return
        end

        -- 2. Tombstones with energy.
        local tomb = self:findEnergyTombstone()
        if tomb then
            self:drawTargetLine(tomb, "#ff8a65")
            if creep:withdraw(tomb, screeps.RESOURCE.ENERGY) == screeps.ERR.NOT_IN_RANGE then
                creep:moveTo(tomb)
            end
            return
        end

        -- 3. Source containers with energy.
        local srcContainers = CreepAgent.findSourceContainers(room, 50)
        if #srcContainers == 0 then
            srcContainers = CreepAgent.findSourceContainers(room, 1)
        end
        local srcContainer = CreepAgent.findClosest(creep.pos, srcContainers)
        if srcContainer then
            self:drawTargetLine(srcContainer, "#ffca28")
            if creep:withdraw(srcContainer, screeps.RESOURCE.ENERGY) == screeps.ERR.NOT_IN_RANGE then
                creep:moveTo(srcContainer)
            end
            return
        end

        -- 4. Storage (when spawns, extensions, or towers need energy).
        if CreepAgent.roomNeedsCriticalEnergy(room) then
            local storage = CreepAgent.findRoomStorage(room)
            if storage and CreepAgent.getUsedEnergy(storage.store) > 0 then
                self.memory.fromStorage = true
                self:drawTargetLine(storage, "#26c6da")
                if creep:withdraw(storage, screeps.RESOURCE.ENERGY) == screeps.ERR.NOT_IN_RANGE then
                    creep:moveTo(storage)
                end
                return
            end
        end

        -- 5. Fallback: any container with energy when critical structures need refilling.
        if CreepAgent.roomNeedsCriticalEnergy(room) then
            local anyC = self:findAnyEnergyContainer()
            if anyC then
                self:drawTargetLine(anyC, "#ffca28")
                if creep:withdraw(anyC, screeps.RESOURCE.ENERGY) == screeps.ERR.NOT_IN_RANGE then
                    creep:moveTo(anyC)
                end
                return
            end
        end

        if CreepAgent.getUsedEnergy(creep.store) > 0 then
            self.memory.working = true
        end
        return
    end

    -- Delivering energy.
    local target = self:findEnergyDeliveryTarget({
        includeControllerContainer = true,
        includeStorage = true,
    })
    if target then
        self:drawTargetLine(target, "#26c6da")
        if creep:transfer(target, screeps.RESOURCE.ENERGY) == screeps.ERR.NOT_IN_RANGE then
            creep:moveTo(target)
        end
        return
    end

    -- Fallback: supply an upgrader or builder with free energy capacity.
    local myCreeps = room:find(screeps.FIND.MY_CREEPS) or {} ---@type Creep[]
    ---@type Creep[]
    local needy = {}
    for _, c in ipairs(myCreeps) do
        if c.id ~= creep.id and c.memory then
            local r = c.memory.role
            if (r == "upgrader" or r == "builder") and CreepAgent.getFreeEnergy(c.store) > 0 then
                table.insert(needy, c)
            end
        end
    end

    local recipient = CreepAgent.findClosest(creep.pos, needy)
    if recipient then
        self:drawTargetLine(recipient, "#80deea")
        if creep:transfer(recipient, screeps.RESOURCE.ENERGY) == screeps.ERR.NOT_IN_RANGE then
            creep:moveTo(recipient)
        end
        return
    end

    local spawns = room:find(screeps.FIND.MY_SPAWNS) ---@type StructureSpawn[]
    local spawn = CreepAgent.findClosest(creep.pos, spawns)
    if spawn and creep.pos:getRangeTo(spawn) > 3 then
        creep:moveTo(spawn)
    end
end

---4. Upgrader: withdraws from controller link/container/storage or harvests, upgrades controller.
function CreepAgent:upgrader()
    self:updateWorkingState()

    if not self.memory.working then
        self:acquireWorkerEnergy(true)
        return
    end

    self:upgradeRoomController()
end

---5. Builder: builds prioritized sites and repairs damaged structures & ramparts.
function CreepAgent:builder()
    local creep = self.creep
    local room = self.room
    self:updateWorkingState()

    if not self.memory.working then
        self:acquireWorkerEnergy(false)
        return
    end

    -- Priority 1: build construction sites by tier, closest within highest tier.
    local sites = room:find(screeps.FIND.CONSTRUCTION_SITES) ---@type ConstructionSite[]
    if sites and #sites > 0 then
        local bestTier = 99
        for _, site in ipairs(sites) do
            local tier = CreepAgent.sitePriority(site.structureType)
            if tier < bestTier then
                bestTier = tier
            end
        end

        ---@type ConstructionSite[]
        local tierSites = {}
        for _, site in ipairs(sites) do
            if CreepAgent.sitePriority(site.structureType) == bestTier then
                table.insert(tierSites, site)
            end
        end

        local targetSite = CreepAgent.findClosest(creep.pos, tierSites)
        if targetSite then
            self:drawTargetLine(targetSite, "#66bb6a")
            if creep:build(targetSite) == screeps.ERR.NOT_IN_RANGE then
                creep:moveTo(targetSite)
            end
            return
        end
    end

    -- Priority 2: repair damaged structures (regular structures < 80%, ramparts up to RCL cap).
    local rcl = (room.controller and room.controller.level) or 1
    local rampartCap = math.min(50000, rcl * 10000)

    local structures = room:find(screeps.FIND.STRUCTURES) or {} ---@type Structure[]
    local worstRegular = nil
    local worstRatio = 0.8
    local worstRampart = nil
    local lowestRampartHits = rampartCap

    for _, s in ipairs(structures) do
        if s.hits and s.hitsMax and s.hitsMax > 0 then
            if s.structureType == screeps.STRUCTURE.RAMPART then
                if s.hits < lowestRampartHits and s.hits < s.hitsMax then
                    lowestRampartHits = s.hits
                    worstRampart = s
                end
            elseif s.structureType ~= screeps.STRUCTURE.WALL then
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
        self:drawTargetLine(repairTarget, "#ffa726")
        if creep:repair(repairTarget) == screeps.ERR.NOT_IN_RANGE then
            creep:moveTo(repairTarget)
        end
        return
    end

    -- Priority 3: upgrade controller when nothing needs building or repairing.
    self:upgradeRoomController()
end

---Find a friendly rampart near the defender that can still engage `target` without stepping on border exits.
---@param target? RoomObject
---@param maxEngageRange integer
---@return Structure|nil
function CreepAgent:findCombatRampart(target, maxEngageRange)
    if not target or not target.pos then
        return nil
    end
    local myStructs = self.room:find(screeps.FIND.MY_STRUCTURES) or {} ---@type Structure[]
    local best = nil
    local bestDist = math.maxinteger
    for _, s in ipairs(myStructs) do
        if s.structureType == screeps.STRUCTURE.RAMPART and s.pos and not CreepAgent.isOnEdge(s.pos) then
            local distToTarget = s.pos:getRangeTo(target)
            if distToTarget <= maxEngageRange then
                local distToCreep = self.creep.pos:getRangeTo(s)
                if distToCreep <= 3 and distToCreep < bestDist then
                    bestDist = distToCreep
                    best = s
                end
            end
        end
    end
    return best
end

---6. Defender: home-bound, border-safe, rampart-aware combat defender.
function CreepAgent:defender()
    local creep = self.creep
    local room = self.room
    local homeRoom = (self.memory and self.memory.room) or (room and room.name)
    if self.memory and not self.memory.room and homeRoom then
        self.memory.room = homeRoom
    end

    -- Never abandon home room: immediately return if pushed across an exit.
    if homeRoom and room and room.name ~= homeRoom then
        self:moveAcrossExit(homeRoom, nil)
        return
    end

    -- Step off border exit tiles immediately so we never bounce out of homeRoom.
    if CreepAgent.isOnEdge(creep.pos) then
        self:stepOffEdge()
        return
    end

    local hostiles = room:find(screeps.FIND.HOSTILE_CREEPS) or {} ---@type Creep[]
    if #hostiles > 0 then
        -- Prefer interior hostiles (not standing on exit border tiles) when available.
        ---@type Creep[]
        local interiorHostiles = {}
        for _, h in ipairs(hostiles) do
            if h.pos and not CreepAgent.isOnEdge(h.pos) then
                table.insert(interiorHostiles, h)
            end
        end
        local target = CreepAgent.findClosest(creep.pos, (#interiorHostiles > 0 and interiorHostiles) or hostiles)
        if target then
            self:drawTargetLine(target, "#ef5350")
            local range = creep.pos:getRangeTo(target)
            local hasMelee = CreepAgent.hasActivePart(creep, screeps.PART.ATTACK)
            local hasRanged = CreepAgent.hasActivePart(creep, screeps.PART.RANGED_ATTACK)

            if hasRanged and range <= 3 then
                creep:rangedAttack(target)
            end

            -- Rampart-aware positioning: hold or step onto a nearby rampart in weapon range.
            local engageRange = (hasMelee and 1) or 3
            local rampart = self:findCombatRampart(target, engageRange)
            if rampart and rampart.pos then
                if creep.pos.x ~= rampart.pos.x or creep.pos.y ~= rampart.pos.y then
                    creep:moveTo(rampart)
                end
                if hasMelee and range <= 1 then
                    creep:attack(target)
                end
                return
            end

            -- Compute border-safe chase coordinate (never step onto x==0, x==49, y==0, y==49).
            local chaseX = CreepAgent.clamp(target.pos.x, 1, 48)
            local chaseY = CreepAgent.clamp(target.pos.y, 1, 48)

            if hasMelee then
                if creep:attack(target) == screeps.ERR.NOT_IN_RANGE then
                    if CreepAgent.isOnEdge(target.pos) then
                        creep:moveTo(chaseX, chaseY)
                    else
                        creep:moveTo(target)
                    end
                end
            elseif hasRanged and range > 3 then
                if CreepAgent.isOnEdge(target.pos) then
                    creep:moveTo(chaseX, chaseY)
                else
                    creep:moveTo(target)
                end
            end
            return
        end
    end

    -- Patrol near primary spawn / north-east perimeter (staying inside x=2..47, y=2..47).
    local spawns = room:find(screeps.FIND.MY_SPAWNS) ---@type StructureSpawn[]
    local spawn = CreepAgent.findClosest(creep.pos, spawns)
    if spawn and spawn.pos then
        local px = CreepAgent.clamp(spawn.pos.x + 3, 2, 47)
        local py = CreepAgent.clamp(spawn.pos.y - 3, 2, 47)
        if creep.pos:getRangeTo(px, py) > 2 then
            creep:moveTo(px, py)
        end
    else
        local px = CreepAgent.clamp(creep.pos.x, 2, 47)
        local py = CreepAgent.clamp(creep.pos.y, 2, 47)
        if creep.pos.x ~= px or creep.pos.y ~= py then
            creep:moveTo(px, py)
        end
    end
end

---7. Scout: fast 1-MOVE room intel explorer visiting adjacent connected rooms.
function CreepAgent:scout()
    local creep = self.creep
    local room = self.room
    local homeRoom = (self.memory and self.memory.room) or (room and room.name)
    if self.memory and not self.memory.room and homeRoom then
        self.memory.room = homeRoom
    end

    if room.name == homeRoom then
        self.memory.scoutTicks = 0
        local targetRoom = self.memory.targetRoom
        if targetRoom then
            local info = screeps.Memory and screeps.Memory.roomIntel and screeps.Memory.roomIntel[targetRoom]
            local now = screeps.Game.time or 0
            local ttl = (info and (info.hostiles or 0) > 0) and HOSTILE_INTEL_TTL or SCOUT_INTEL_TTL
            if info and info.lastSeen and (now - info.lastSeen) <= ttl then
                targetRoom = nil
            end
        end
        if not targetRoom then
            targetRoom = CreepAgent.pickNextScoutRoom(homeRoom)
            self.memory.targetRoom = targetRoom
        end

        if targetRoom then
            self:moveAcrossExit(targetRoom, nil)
            return
        end

        -- All adjacent rooms recently scouted: hold near spawn inside safe interior.
        if CreepAgent.isOnEdge(creep.pos) then
            self:stepOffEdge()
            return
        end
        local spawns = room:find(screeps.FIND.MY_SPAWNS) ---@type StructureSpawn[]
        local spawn = CreepAgent.findClosest(creep.pos, spawns)
        if spawn and spawn.pos and creep.pos:getRangeTo(spawn) > 4 then
            creep:moveTo(spawn)
        end
        return
    end

    -- Inside an adjacent room: intel is already recorded by CreepAgent.run.
    if CreepAgent.isOnEdge(creep.pos) then
        self:stepOffEdge()
        return
    end

    local hostiles = room:find(screeps.FIND.HOSTILE_CREEPS) or {} ---@type Creep[]
    if #hostiles > 0 then
        -- Hostiles spotted: intel recorded, retreat back home immediately.
        self.memory.targetRoom = nil
        self:moveAcrossExit(homeRoom, nil)
        return
    end

    self.memory.scoutTicks = (self.memory.scoutTicks or 0) + 1
    if self.memory.scoutTicks < 4 then
        if creep.pos:getRangeTo(25, 25) > 8 then
            creep:moveTo(25, 25)
        end
    else
        self.memory.targetRoom = nil
        self:moveAcrossExit(homeRoom, nil)
    end
end

---Assign a remoteHarvester to the remote source with the fewest assigned remoteHarvesters.
---@return Source|nil
function CreepAgent:resolveRemoteSource()
    if self.memory.sourceId and screeps.Game.getObjectById then
        local existing = screeps.Game.getObjectById(self.memory.sourceId) --[[@as Source|nil]]
        if existing and existing.room and existing.room.name == self.room.name then
            return existing
        end
        self.memory.sourceId = nil
    end

    local sources = self.room:find(screeps.FIND.SOURCES) or {} ---@type Source[]
    if #sources == 0 then
        return nil
    end

    ---@type table<string, integer>
    local assignedCounts = {}
    for _, src in ipairs(sources) do
        assignedCounts[src.id] = 0
    end

    local myCreeps = self.room:find(screeps.FIND.MY_CREEPS) or {} ---@type Creep[]
    for _, c in ipairs(myCreeps) do
        if c.id ~= self.creep.id and c.memory and c.memory.role == "remoteHarvester" and c.memory.sourceId then
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

    self.memory.sourceId = chosen.id
    return chosen
end

---8. Remote Harvester: travels to targetRoom, harvests source, drops/transfers energy, flees if hostiles appear.
function CreepAgent:remoteHarvester()
    local creep = self.creep
    local room = self.room
    local homeRoom = (self.memory and self.memory.room) or (room and room.name)
    if self.memory and not self.memory.room and homeRoom then
        self.memory.room = homeRoom
    end

    local rmem = screeps.Memory and screeps.Memory.rooms and screeps.Memory.rooms[homeRoom]
    if room.name == homeRoom and rmem then
        self.memory.targetRoom = rmem.remoteRoom
    elseif not self.memory.targetRoom and rmem then
        self.memory.targetRoom = rmem.remoteRoom
    end
    local targetRoom = self.memory.targetRoom

    -- Flee back home if hostiles appear in any non-home room.
    if room.name ~= homeRoom then
        local hostiles = room:find(screeps.FIND.HOSTILE_CREEPS) or {} ---@type Creep[]
        if #hostiles > 0 then
            self:moveAcrossExit(homeRoom, nil)
            return
        end
    end

    if not targetRoom then
        if room.name ~= homeRoom then
            self:moveAcrossExit(homeRoom, nil)
        else
            local spawns = room:find(screeps.FIND.MY_SPAWNS) ---@type StructureSpawn[]
            local spawn = CreepAgent.findClosest(creep.pos, spawns)
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
                local spawns = room:find(screeps.FIND.MY_SPAWNS) ---@type StructureSpawn[]
                local spawn = CreepAgent.findClosest(creep.pos, spawns)
                if spawn and spawn.pos and creep.pos:getRangeTo(spawn) > 4 then
                    creep:moveTo(spawn)
                end
                return
            end
        elseif CreepAgent.isOnEdge(creep.pos) then
            self:stepOffEdge()
            return
        end
        self:moveAcrossExit(targetRoom, nil)
        return
    end

    -- Inside targetRoom: step off edge and harvest.
    if CreepAgent.isOnEdge(creep.pos) then
        self:stepOffEdge()
        return
    end

    local source = self:resolveRemoteSource()
    if not source then
        return
    end

    self:drawTargetLine(source, "#26c6da")
    if creep:harvest(source) == screeps.ERR.NOT_IN_RANGE then
        creep:moveTo(source)
        return
    end

    -- Offload energy to adjacent container, adjacent remoteHauler, or drop when full.
    local used = CreepAgent.getUsedEnergy(creep.store)
    local free = CreepAgent.getFreeEnergy(creep.store)
    if used > 0 then
        local containers = CreepAgent.findContainersNear(room, creep.pos, 1, nil)
        for _, c in ipairs(containers) do
            if CreepAgent.getFreeEnergy(c.store) > 0 then
                creep:transfer(c, screeps.RESOURCE.ENERGY)
                return
            end
        end

        local myCreeps = room:find(screeps.FIND.MY_CREEPS) or {} ---@type Creep[]
        for _, c in ipairs(myCreeps) do
            if c.id ~= creep.id and c.memory and c.memory.role == "remoteHauler" and c.pos then
                if creep.pos:getRangeTo(c) <= 1 and CreepAgent.getFreeEnergy(c.store) > 0 then
                    creep:transfer(c, screeps.RESOURCE.ENERGY)
                    return
                end
            end
        end

        if free == 0 and creep.drop then
            creep:drop(screeps.RESOURCE.ENERGY)
        end
    end
end

---9. Remote Hauler: fast 1:1 CARRY/MOVE hauler ferrying energy from targetRoom back to homeRoom.
function CreepAgent:remoteHauler()
    local creep = self.creep
    local room = self.room
    local homeRoom = (self.memory and self.memory.room) or (room and room.name)
    if self.memory and not self.memory.room and homeRoom then
        self.memory.room = homeRoom
    end

    local rmem = screeps.Memory and screeps.Memory.rooms and screeps.Memory.rooms[homeRoom]
    if room.name == homeRoom and rmem then
        self.memory.targetRoom = rmem.remoteRoom
    elseif not self.memory.targetRoom and rmem then
        self.memory.targetRoom = rmem.remoteRoom
    end
    local targetRoom = self.memory.targetRoom

    self:updateWorkingState()

    -- Flee back home immediately if hostiles appear in a non-home room.
    if room.name ~= homeRoom then
        local hostiles = room:find(screeps.FIND.HOSTILE_CREEPS) or {} ---@type Creep[]
        if #hostiles > 0 then
            self:moveAcrossExit(homeRoom, nil)
            return
        end
    end

    if not self.memory.working then
        -- Collecting energy in targetRoom.
        if not targetRoom then
            if CreepAgent.getUsedEnergy(creep.store) > 0 then
                self.memory.working = true
            elseif room.name ~= homeRoom then
                self:moveAcrossExit(homeRoom, nil)
            end
            return
        end

        if room.name ~= targetRoom then
            if room.name == homeRoom then
                local rIntel = screeps.Memory and screeps.Memory.roomIntel and screeps.Memory.roomIntel[targetRoom]
                if rIntel and (rIntel.hostiles or 0) > 0 then
                    local spawns = room:find(screeps.FIND.MY_SPAWNS) ---@type StructureSpawn[]
                    local spawn = CreepAgent.findClosest(creep.pos, spawns)
                    if spawn and spawn.pos and creep.pos:getRangeTo(spawn) > 4 then
                        creep:moveTo(spawn)
                    end
                    return
                end
            elseif CreepAgent.isOnEdge(creep.pos) then
                self:stepOffEdge()
                return
            end
            self:moveAcrossExit(targetRoom, nil)
            return
        end

        if CreepAgent.isOnEdge(creep.pos) then
            self:stepOffEdge()
            return
        end

        -- 1. Dropped energy in remote room.
        local dropped = self:findDroppedEnergy()
        if dropped then
            self:drawTargetLine(dropped, "#26c6da")
            if creep:pickup(dropped) == screeps.ERR.NOT_IN_RANGE then
                creep:moveTo(dropped)
            end
            return
        end

        -- 2. Tombstones with energy.
        local tomb = self:findEnergyTombstone()
        if tomb then
            self:drawTargetLine(tomb, "#ff8a65")
            if creep:withdraw(tomb, screeps.RESOURCE.ENERGY) == screeps.ERR.NOT_IN_RANGE then
                creep:moveTo(tomb)
            end
            return
        end

        -- 3. Remote containers with energy.
        local container = self:findAnyEnergyContainer()
        if container then
            self:drawTargetLine(container, "#26c6da")
            if creep:withdraw(container, screeps.RESOURCE.ENERGY) == screeps.ERR.NOT_IN_RANGE then
                creep:moveTo(container)
            end
            return
        end

        -- 4. If already carrying partial energy and nothing left to gather, head home.
        if CreepAgent.getUsedEnergy(creep.store) >= 50 then
            self.memory.working = true
            return
        end

        -- Otherwise stage near a remoteHarvester or source waiting for drops.
        local myCreeps = room:find(screeps.FIND.MY_CREEPS) or {} ---@type Creep[]
        ---@type Creep[]
        local harvesters = {}
        for _, c in ipairs(myCreeps) do
            if c.memory and c.memory.role == "remoteHarvester" then
                table.insert(harvesters, c)
            end
        end
        local anchor = CreepAgent.findClosest(creep.pos, harvesters) or self:findNearestSource()
        if anchor and anchor.pos and creep.pos:getRangeTo(anchor) > 2 then
            creep:moveTo(anchor)
        end
        return
    end

    -- Delivering energy back to homeRoom.
    if room.name ~= homeRoom then
        self:moveAcrossExit(homeRoom, nil)
        return
    end

    if CreepAgent.isOnEdge(creep.pos) then
        self:stepOffEdge()
        return
    end

    local target = self:findEnergyDeliveryTarget({
        includeControllerContainer = true,
        includeStorage = true,
    })
    if target then
        self:drawTargetLine(target, "#26c6da")
        if creep:transfer(target, screeps.RESOURCE.ENERGY) == screeps.ERR.NOT_IN_RANGE then
            creep:moveTo(target)
        end
        return
    end

    -- Fallback: hand off to an upgrader or builder needing energy.
    local myCreeps = room:find(screeps.FIND.MY_CREEPS) or {} ---@type Creep[]
    ---@type Creep[]
    local needy = {}
    for _, c in ipairs(myCreeps) do
        if c.id ~= creep.id and c.memory then
            local r = c.memory.role
            if (r == "upgrader" or r == "builder") and CreepAgent.getFreeEnergy(c.store) > 0 then
                table.insert(needy, c)
            end
        end
    end
    local recipient = CreepAgent.findClosest(creep.pos, needy)
    if recipient then
        self:drawTargetLine(recipient, "#80deea")
        if creep:transfer(recipient, screeps.RESOURCE.ENERGY) == screeps.ERR.NOT_IN_RANGE then
            creep:moveTo(recipient)
        end
        return
    end

    -- Last resort: drop near primary spawn so haulers/upgraders can use it.
    local spawns = room:find(screeps.FIND.MY_SPAWNS) ---@type StructureSpawn[]
    local spawn = CreepAgent.findClosest(creep.pos, spawns)
    if spawn and spawn.pos then
        if creep.pos:getRangeTo(spawn) > 2 then
            creep:moveTo(spawn)
        elseif creep.drop and CreepAgent.getUsedEnergy(creep.store) > 0 then
            creep:drop(screeps.RESOURCE.ENERGY)
        end
    end
end

---Shared border-rally & cross-room assault transit for Hivemind counter-raids.
---@return boolean readyToEngage
function CreepAgent:handleAssaultTransit()
    local creep = self.creep
    local room = self.room
    local homeRoom = (self.memory and self.memory.room) or (room and room.name)
    local rmem = homeRoom and screeps.Memory and screeps.Memory.rooms and screeps.Memory.rooms[homeRoom]
    local targetRoom = rmem and rmem.attackTarget

    if not targetRoom then
        if self.memory then
            self.memory.targetRoom = nil
        end
        if homeRoom and room and room.name ~= homeRoom then
            self:moveAcrossExit(homeRoom, nil)
            return false
        end
        return false
    end
    self.memory.targetRoom = targetRoom

    if room.name == targetRoom then
        self.memory.state = "assault"
        if CreepAgent.isOnEdge(creep.pos) then
            self:stepOffEdge()
            return false
        end
        return true
    end

    if room.name == homeRoom then
        -- Defend home immediately if enemy raiders are inside!
        local homeHostiles = room:find(screeps.FIND.HOSTILE_CREEPS) or {} ---@type Creep[]
        if #homeHostiles > 0 then
            return true
        end

        local exitDir = room.findExitTo and room:findExitTo(targetRoom)
        if type(exitDir) ~= "number" or exitDir <= 0 then
            return false
        end

        -- Rally near border exit until full wave (2 bruisers + 1-2 healers) is ready!
        if self.memory.state ~= "assault" then
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
            local staged, hasHealer = CreepAgent.inspectAssaultWave(room)
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

        self:moveAcrossExit(targetRoom, exitDir)
        return false
    end

    if CreepAgent.isOnEdge(creep.pos) then
        self:stepOffEdge()
        return false
    end
    self:moveAcrossExit(targetRoom, nil)
    return false
end

---Shared focus-fire target selection for Hivemind assault waves.
---@return Creep|Structure|nil
function CreepAgent:selectAssaultTarget()
    local creep = self.creep
    local room = self.room
    local now = screeps.Game.time or 0
    if screeps.Memory and screeps.Memory.assaultFocusTick == now and screeps.Memory.assaultFocusRoom == room.name and screeps.Memory.assaultFocusId then
        local cached = screeps.Game.getObjectById and screeps.Game.getObjectById(screeps.Memory.assaultFocusId) --[[@as Creep|Structure|nil]]
        if cached and cached.room and cached.room.name == room.name then
            return cached
        end
    end

    local chosen = nil
    local hostiles = room:find(screeps.FIND.HOSTILE_CREEPS) or {} ---@type Creep[]
    if #hostiles > 0 then
        local bestScore = math.mininteger
        for _, h in ipairs(hostiles) do
            local d = creep.pos:getRangeTo(h)
            local hpFrac = (h.hits and h.hitsMax and h.hitsMax > 0) and (h.hits / h.hitsMax) or 1
            local s = 100 - d * 2 + (1 - hpFrac) * 45
            if h.body then
                for _, p in ipairs(h.body) do
                    local pt = (type(p) == "table" and p.type) or p
                    if pt == screeps.PART.HEAL then
                        s = s + 60
                        break
                    elseif pt == screeps.PART.ATTACK or pt == screeps.PART.RANGED_ATTACK then
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
        local hStructs = room:find(screeps.FIND.HOSTILE_STRUCTURES) or {} ---@type Structure[]
        ---@type Structure[]
        local towers = {}
        ---@type Structure[]
        local spawns = {}
        ---@type Structure[]
        local others = {}
        for _, s in ipairs(hStructs) do
            if s.structureType == screeps.STRUCTURE.TOWER then
                table.insert(towers, s)
            elseif s.structureType == screeps.STRUCTURE.SPAWN then
                table.insert(spawns, s)
            elseif s.structureType ~= screeps.STRUCTURE.CONTROLLER then
                table.insert(others, s)
            end
        end
        if #towers > 0 then
            chosen = CreepAgent.findClosest(creep.pos, towers)
        elseif #spawns > 0 then
            chosen = CreepAgent.findClosest(creep.pos, spawns)
        elseif #others > 0 then
            chosen = CreepAgent.findClosest(creep.pos, others)
        end
    end

    if chosen and chosen.id and screeps.Memory then
        screeps.Memory.assaultFocusTick = now
        screeps.Memory.assaultFocusRoom = room.name
        screeps.Memory.assaultFocusId = chosen.id
    end
    return chosen
end

---10. Assault Bruiser: coordinated counter-raid frontline attacker.
function CreepAgent:assault()
    local creep = self.creep
    if not self:handleAssaultTransit() then
        return
    end
    -- Pull back to friendly healer if critically low HP (< 25%).
    if creep.hits and creep.hitsMax and creep.hits < creep.hitsMax * 0.25 then
        local myCreeps = self.room:find(screeps.FIND.MY_CREEPS) or {} ---@type Creep[]
        for _, ally in ipairs(myCreeps) do
            if ally.id ~= creep.id and ally.memory and ally.memory.role == "healer" then
                creep:moveTo(ally)
                return
            end
        end
    end
    local target = self:selectAssaultTarget()
    if not target then
        if creep.pos:getRangeTo(25, 25) > 5 then
            creep:moveTo(25, 25)
        end
        return
    end
    self:drawTargetLine(target, "#ff5252")
    if creep:attack(target) == screeps.ERR.NOT_IN_RANGE then
        creep:moveTo(target)
    end
end

---11. Siege Dismantler: breaches enemy towers, spawns, extensions, and ramparts (50 dmg/WORK).
function CreepAgent:dismantler()
    local creep = self.creep
    if not self:handleAssaultTransit() then
        return
    end
    -- Pull back toward a friendly healer when critically damaged (< 30% HP).
    if creep.hits and creep.hitsMax and creep.hits < creep.hitsMax * 0.30 then
        local myCreeps = self.room:find(screeps.FIND.MY_CREEPS) or {} ---@type Creep[]
        for _, ally in ipairs(myCreeps) do
            if ally.id ~= creep.id and ally.memory and ally.memory.role == "healer" then
                creep:moveTo(ally)
                return
            end
        end
    end

    -- Prioritize hostile structures: Tower -> Spawn -> Extension -> Storage -> Rampart -> other.
    local structs = self.room:find(screeps.FIND.HOSTILE_STRUCTURES) or {} ---@type Structure[]
    local best = nil ---@type Structure|nil
    local bestScore = -1
    for _, s in ipairs(structs) do
        if s.structureType ~= screeps.STRUCTURE.CONTROLLER and s.hits and s.hits > 0 then
            local prio = 20
            if s.structureType == screeps.STRUCTURE.TOWER then
                prio = 100
            elseif s.structureType == screeps.STRUCTURE.SPAWN then
                prio = 90
            elseif s.structureType == screeps.STRUCTURE.EXTENSION then
                prio = 65
            elseif s.structureType == screeps.STRUCTURE.STORAGE then
                prio = 55
            elseif s.structureType == screeps.STRUCTURE.RAMPART then
                prio = 40
            end
            local d = creep.pos:getRangeTo(s)
            local score = prio * 10 - d
            if score > bestScore then
                bestScore = score
                best = s
            end
        end
    end

    if not best then
        if creep.pos:getRangeTo(25, 25) > 5 then
            creep:moveTo(25, 25)
        end
        return
    end
    self:drawTargetLine(best, "#ff9800")
    if creep:dismantle(best) == screeps.ERR.NOT_IN_RANGE then
        creep:moveTo(best)
    end
end

---12. Remote Builder: plans and builds containers and roads in the assigned remote mining room.
function CreepAgent:remoteBuilder()
    local creep = self.creep
    local room = self.room
    local homeRoom = self.memory.homeRoom or room.name
    self.memory.homeRoom = homeRoom
    local rmem = screeps.Memory and screeps.Memory.rooms and screeps.Memory.rooms[homeRoom]
    if room.name == homeRoom then
        self.memory.targetRoom = rmem and rmem.remoteRoom or nil
    end
    local targetRoom = self.memory.targetRoom
    if not targetRoom then
        self:builder()
        return
    end

    if room.name ~= targetRoom then
        self:moveAcrossExit(targetRoom, nil)
        return
    end
    if self:stepOffEdge() then
        return
    end

    -- Evacuate if hostiles appear in the remote room.
    local hostiles = room:find(screeps.FIND.HOSTILE_CREEPS) or {} ---@type Creep[]
    if #hostiles > 0 then
        self:moveAcrossExit(homeRoom, nil)
        return
    end

    -- Periodically lay down remote source containers and roads back toward homeRoom.
    if ((screeps.Game.time or 0) % 25) == 0 then
        RoomPlanner.planRemoteRoom(room, homeRoom)
    end

    self:updateWorkingState()
    if self.memory.working then
        local sites = room:find(screeps.FIND.MY_CONSTRUCTION_SITES) or {} ---@type ConstructionSite[]
        if #sites > 0 then
            local target = CreepAgent.findClosest(creep.pos, sites)
            if target then
                self:drawTargetLine(target, "#ffd54f")
                if creep:build(target) == screeps.ERR.NOT_IN_RANGE then
                    creep:moveTo(target)
                end
                return
            end
        end
        -- Maintain remote containers and roads.
        local structs = room:find(screeps.FIND.STRUCTURES) or {} ---@type Structure[]
        local damaged = nil ---@type Structure|nil
        local lowestRatio = 0.85
        for _, s in ipairs(structs) do
            if (s.structureType == screeps.STRUCTURE.CONTAINER or s.structureType == screeps.STRUCTURE.ROAD)
                and s.hits and s.hitsMax and s.hitsMax > 0 then
                local ratio = s.hits / s.hitsMax
                if ratio < lowestRatio then
                    lowestRatio = ratio
                    damaged = s
                end
            end
        end
        if damaged then
            self:drawTargetLine(damaged, "#80cbc4")
            if creep:repair(damaged) == screeps.ERR.NOT_IN_RANGE then
                creep:moveTo(damaged)
            end
            return
        end
        -- Nothing to build/repair: return home to help build locally.
        self:moveAcrossExit(homeRoom, nil)
    else
        -- Harvest or withdraw locally in the remote room.
        local dropped = self:findDroppedEnergy()
        if dropped then
            if creep:pickup(dropped) == screeps.ERR.NOT_IN_RANGE then
                creep:moveTo(dropped)
            end
            return
        end
        local src = self:findNearestSource()
        if src then
            if creep:harvest(src) == screeps.ERR.NOT_IN_RANGE then
                creep:moveTo(src)
            end
        end
    end
end

---13. Mineral Harvester: harvests room minerals at RCL 6+ (with Extractor) and deposits into Storage.
function CreepAgent:mineralHarvester()
    local creep = self.creep
    local room = self.room
    local minerals = room:find(screeps.FIND.MINERALS) or {} ---@type Mineral[]
    local mineral = minerals[1]
    local storage = CreepAgent.findRoomStorage(room)
    if not mineral or not storage then
        return
    end
    local resType = mineral.mineralType
    local carried = (creep.store and resType and creep.store:getUsedCapacity(resType)) or 0
    local free = (creep.store and creep.store:getFreeCapacity()) or 0

    if carried > 0 and (free == 0 or (mineral.mineralAmount or 0) <= 0) then
        self:drawTargetLine(storage, "#ce93d8")
        if creep:transfer(storage, resType) == screeps.ERR.NOT_IN_RANGE then
            creep:moveTo(storage)
        end
        return
    end

    if (mineral.mineralAmount or 0) > 0 then
        self:drawTargetLine(mineral, "#ab47bc")
        if creep:harvest(mineral) == screeps.ERR.NOT_IN_RANGE then
            creep:moveTo(mineral)
        end
    elseif carried > 0 then
        if creep:transfer(storage, resType) == screeps.ERR.NOT_IN_RANGE then
            creep:moveTo(storage)
        end
    end
end

---14. Combat Healer: hugs frontline assault/dismantler creeps and sustains the wave under tower fire.
function CreepAgent:healer()
    local creep = self.creep
    if creep.hits and creep.hitsMax and creep.hits < creep.hitsMax then
        creep:heal(creep)
    end
    if not self:handleAssaultTransit() then
        return
    end
    local myCreeps = self.room:find(screeps.FIND.MY_CREEPS) or {} ---@type Creep[]
    local wounded = nil
    local lowestRatio = 1.0
    ---@type Creep[]
    local frontline = {}
    for _, ally in ipairs(myCreeps) do
        if ally.id ~= creep.id then
            if ally.hits and ally.hitsMax and ally.hitsMax > 0 and ally.hits < ally.hitsMax then
                local r = ally.hits / ally.hitsMax
                if r < lowestRatio then
                    lowestRatio = r
                    wounded = ally
                end
            end
            if ally.memory and (ally.memory.role == "dismantler" or ally.memory.role == "assault") then
                table.insert(frontline, ally)
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
    if #frontline > 0 then
        local lead = CreepAgent.findClosest(creep.pos, frontline)
        if lead then
            if creep.pos:getRangeTo(lead) <= 1 then
                creep:heal(lead) -- pre-heal frontline siege/assault creep
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

---@type table<string, fun(self: CreepAgent)>
local ROLE_HANDLERS <const> = {
    harvester = CreepAgent.harvester,
    miner = CreepAgent.miner,
    hauler = CreepAgent.hauler,
    upgrader = CreepAgent.upgrader,
    builder = CreepAgent.builder,
    defender = CreepAgent.defender,
    scout = CreepAgent.scout,
    remoteHarvester = CreepAgent.remoteHarvester,
    remoteHauler = CreepAgent.remoteHauler,
    remoteBuilder = CreepAgent.remoteBuilder,
    mineralHarvester = CreepAgent.mineralHarvester,
    assault = CreepAgent.assault,
    dismantler = CreepAgent.dismantler,
    healer = CreepAgent.healer,
}

---Execute this creep's role behavior for the current tick.
function CreepAgent:execute()
    local role = self.memory.role or "harvester"
    local handler = ROLE_HANDLERS[role] or CreepAgent.harvester
    handler(self)
end

---Static dispatcher for running any creep according to creep.memory.role.
---@param creep? Creep
function CreepAgent.run(creep)
    if not creep or creep.spawning or not creep.memory then
        return
    end
    if creep.room then
        CreepAgent.recordRoomIntel(creep.room)
    end
    CreepAgent.new(creep):execute()
end

return CreepAgent
