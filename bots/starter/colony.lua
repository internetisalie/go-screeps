-- colony.lua: Object-oriented Colony manager for an owned Screeps room.
-- Coordinates defense, links, spawning, remote mining, counter-assaults, planning, and HUD visuals.
-- Idiomatic Lua 5.4 (arnodel/golua) with Lunar/LuaLS annotations.

local screeps <const> = require("screeps")
local OK <const> = screeps.OK
local FIND <const> = screeps.FIND
local PART <const> = screeps.PART
local COST <const> = screeps.COST
local RESOURCE <const> = screeps.RESOURCE
local STRUCTURE <const> = screeps.STRUCTURE

local planner <const> = require("planner")
local DefenseManager <const> = require("defense")

local PLANNER_INTERVAL <const> = 25
local SCOUT_INTEL_TTL <const> = 1500

---@type table<integer, FindConstant>
local EXIT_DIRS <const> = {
    [1] = FIND.EXIT_TOP,
    [3] = FIND.EXIT_RIGHT,
    [5] = FIND.EXIT_BOTTOM,
    [7] = FIND.EXIT_LEFT,
}

---@type table<string, integer>
local PRESPAWN_TTL <const> = {
    miner = 80,
    hauler = 80,
    harvester = 80,
    remoteHarvester = 140,
    remoteHauler = 120,
}

---@class RoleCounts
---@field harvester integer
---@field miner integer
---@field hauler integer
---@field upgrader integer
---@field builder integer
---@field defender integer
---@field assault integer
---@field healer integer
---@field scout integer
---@field remoteHarvester integer
---@field remoteHauler integer
---@field total? integer

---@class Colony
---@field room Room
---@field rcl integer
---@field memory table<string, any>
---@field defense DefenseManager
---@field counts RoleCounts
local Colony = {}
Colony.__index = Colony

---@param store? Store
---@return integer
local function getUsedEnergy(store)
    if not store then
        return 0
    end
    return store:getUsedCapacity(RESOURCE.ENERGY) or 0
end

---@param store? Store
---@return integer
local function getFreeEnergy(store)
    if not store then
        return 0
    end
    return store:getFreeCapacity(RESOURCE.ENERGY) or 0
end

---@type table<BodyPartType, integer>
local FALLBACK_PART_COST = {
    work = 100,
    carry = 50,
    move = 50,
    attack = 80,
    ranged_attack = 150,
    heal = 250,
    claim = 600,
    tough = 10,
}

---@param body BodyPartType[]
---@return integer
local function bodyCost(body)
    local costs = COST or FALLBACK_PART_COST
    local total = 0
    for _, part in ipairs(body) do
        total = total + (costs[part] or 0)
    end
    return total
end

---@param budget integer
---@return BodyPartType[]|nil
local function buildWorkerBody(budget)
    if budget < 200 then
        return nil
    end
    ---@type BodyPartType[]
    local body = { PART.WORK, PART.CARRY, PART.MOVE }
    local cost = 200
    while cost + 200 <= budget and #body + 3 <= 18 do
        table.insert(body, PART.WORK)
        table.insert(body, PART.CARRY)
        table.insert(body, PART.MOVE)
        cost = cost + 200
    end
    if cost + 100 <= budget and #body + 2 <= 18 then
        table.insert(body, PART.CARRY)
        table.insert(body, PART.MOVE)
    end
    return body
end

---@param budget integer
---@return BodyPartType[]|nil
local function buildMinerBody(budget)
    if budget < 300 then
        return nil
    end
    local workParts = math.max(2, math.min(5, (budget - 100) // 100))
    ---@type BodyPartType[]
    local body = {}
    local cost = 0
    for _ = 1, workParts do
        table.insert(body, PART.WORK)
        cost = cost + 100
    end
    if cost + 50 <= budget then
        table.insert(body, PART.CARRY)
        cost = cost + 50
    end
    local moveParts = (cost + 100 <= budget) and 2 or 1
    for _ = 1, moveParts do
        if cost + 50 <= budget then
            table.insert(body, PART.MOVE)
            cost = cost + 50
        end
    end
    return body
end

---@param budget integer
---@return BodyPartType[]|nil
local function buildHaulerBody(budget)
    if budget < 150 then
        return nil
    end
    ---@type BodyPartType[]
    local body = { PART.CARRY, PART.CARRY, PART.MOVE }
    local cost = 150
    while cost + 150 <= budget and #body + 3 <= 18 do
        table.insert(body, PART.CARRY)
        table.insert(body, PART.CARRY)
        table.insert(body, PART.MOVE)
        cost = cost + 150
    end
    if cost + 100 <= budget and #body + 2 <= 18 then
        table.insert(body, PART.CARRY)
        table.insert(body, PART.MOVE)
    end
    return body
end

---@param budget integer
---@return BodyPartType[]|nil
local function buildScoutBody(budget)
    if budget < 50 then
        return nil
    end
    return { PART.MOVE }
end

---@param budget integer
---@return BodyPartType[]|nil
local function buildRemoteHarvesterBody(budget)
    if budget < 400 then
        return nil
    end
    if budget >= 550 then
        return { PART.WORK, PART.WORK, PART.WORK, PART.CARRY, PART.MOVE, PART.MOVE, PART.MOVE, PART.MOVE }
    end
    return { PART.WORK, PART.WORK, PART.CARRY, PART.MOVE, PART.MOVE, PART.MOVE }
end

---@param budget integer
---@return BodyPartType[]|nil
local function buildRemoteHaulerBody(budget)
    if budget < 200 then
        return nil
    end
    local pairsCount = math.max(2, math.min(5, math.min(budget, 500) // 100))
    ---@type BodyPartType[]
    local body = {}
    for _ = 1, pairsCount do
        table.insert(body, PART.CARRY)
    end
    for _ = 1, pairsCount do
        table.insert(body, PART.MOVE)
    end
    return body
end

---@param role string
---@return integer
local function minRoleCost(role)
    if role == "miner" then
        return 300
    elseif role == "hauler" then
        return 150
    elseif role == "defender" then
        return 190
    elseif role == "assault" then
        return 320
    elseif role == "healer" then
        return 300
    elseif role == "scout" then
        return 50
    elseif role == "remoteHarvester" then
        return 400
    elseif role == "remoteHauler" then
        return 200
    end
    return 200
end

---@param roomName string
---@return string[]
local function getAdjacentRoomNames(roomName)
    ---@type string[]
    local result = {}
    if not roomName or not screeps.Game.map or not screeps.Game.map.describeExits then
        return result
    end
    local exits = screeps.Game.map.describeExits(roomName)
    if type(exits) ~= "table" then
        return result
    end
    for _, adjName in pairs(exits) do
        if type(adjName) == "string" and adjName ~= "" then
            table.insert(result, adjName)
        end
    end
    table.sort(result)
    return result
end

---Create a Colony controller for an owned Room.
---@param room Room
---@return Colony
function Colony.new(room)
    if not screeps.Memory.rooms then
        screeps.Memory.rooms = {}
    end
    if not screeps.Memory.rooms[room.name] then
        screeps.Memory.rooms[room.name] = room.memory or {}
    end
    local self = setmetatable({
        room = room,
        rcl = (room.controller and room.controller.level) or 0,
        memory = screeps.Memory.rooms[room.name],
        defense = DefenseManager.new(room),
        counts = {
            harvester = 0,
            miner = 0,
            hauler = 0,
            upgrader = 0,
            builder = 0,
            defender = 0,
            assault = 0,
            healer = 0,
            scout = 0,
            remoteHarvester = 0,
            remoteHauler = 0,
            total = 0,
        },
    }, Colony)
    return self
end

---Count active (non-expiring) creeps belonging to this colony by role.
---@return RoleCounts
function Colony:countRoles()
    ---@type RoleCounts
    local counts = {
        harvester = 0,
        miner = 0,
        hauler = 0,
        upgrader = 0,
        builder = 0,
        defender = 0,
        assault = 0,
        healer = 0,
        scout = 0,
        remoteHarvester = 0,
        remoteHauler = 0,
        total = 0,
    }
    if screeps.Game.creeps then
        for _, creep in pairs(screeps.Game.creeps) do
            local home = (creep.memory and creep.memory.room) or (creep.room and creep.room.name)
            if home == self.room.name then
                counts.total = counts.total + 1
                local role = (creep.memory and creep.memory.role) or "harvester"
                local lead = PRESPAWN_TTL[role]
                local expiring = lead and (not creep.spawning) and creep.ticksToLive and creep.ticksToLive <= lead
                if counts[role] ~= nil and not expiring then
                    counts[role] = counts[role] + 1
                end
            end
        end
        self.counts = counts
        return counts
    end

    local myCreeps = self.room:find(FIND.MY_CREEPS) or {} ---@type Creep[]
    for _, creep in ipairs(myCreeps) do
        counts.total = counts.total + 1
        local role = (creep.memory and creep.memory.role) or "harvester"
        if counts[role] ~= nil then
            counts[role] = counts[role] + 1
        end
    end
    self.counts = counts
    return counts
end

---@return string|nil
function Colony:findRoomNeedingScout()
    local adj = getAdjacentRoomNames(self.room.name)
    if #adj == 0 then
        return nil
    end
    local now = screeps.Game.time or 0
    local intelMap = (screeps.Memory and screeps.Memory.roomIntel) or {}
    local best = nil
    local oldestSeen = now + 1
    for _, rName in ipairs(adj) do
        local info = intelMap[rName]
        local seen = (info and info.lastSeen) or -9999
        if (not info) or (now - seen > SCOUT_INTEL_TTL) then
            if seen < oldestSeen then
                oldestSeen = seen
                best = rName
            end
        end
    end
    return best
end

---Select up to 1 adjacent unowned, safe room with sources for remote mining.
---@return string|nil
function Colony:updateRemoteRoom()
    local adj = getAdjacentRoomNames(self.room.name)
    if #adj == 0 then
        self.memory.remoteRoom = nil
        return nil
    end
    local intelMap = (screeps.Memory and screeps.Memory.roomIntel) or {}
    local chosen = nil
    local bestSources = 0
    for _, rName in ipairs(adj) do
        local info = intelMap[rName]
        if info and (info.owner == nil or info.owner == "") and (info.hostiles or 0) == 0 and (info.sources or 0) > 0 then
            if info.sources > bestSources then
                bestSources = info.sources
                chosen = rName
            end
        end
    end
    self.memory.remoteRoom = chosen
    return chosen
end

---Identify an adjacent hostile-owned room for coordinated counter-assault waves.
---@return string|nil
function Colony:updateAttackTarget()
    local myOwner = (self.room.controller and self.room.controller.owner and self.room.controller.owner.username) or nil
    local adj = getAdjacentRoomNames(self.room.name)
    local intelMap = (screeps.Memory and screeps.Memory.roomIntel) or {}
    local enemyRoom = nil
    for _, rName in ipairs(adj) do
        local info = intelMap[rName]
        if info and info.owner and info.owner ~= "" and info.owner ~= myOwner then
            enemyRoom = rName
            break
        end
    end
    self.memory.attackTarget = enemyRoom
    return enemyRoom
end

---@param sources Source[]
---@return integer
function Colony:countSourceContainers(sources)
    local structs = self.room:find(FIND.STRUCTURES) or {} ---@type Structure[]
    local ready = 0
    for _, src in ipairs(sources) do
        if src.pos then
            for _, s in ipairs(structs) do
                if s.structureType == STRUCTURE.CONTAINER and s.pos and src.pos:getRangeTo(s) <= 1 then
                    ready = ready + 1
                    break
                end
            end
        end
    end
    return ready
end

---Compute desired role targets for this colony.
---@param energyCap integer
---@return RoleCounts targets
---@return string|nil scoutTarget
function Colony:computeTargetRoster(energyCap)
    local room = self.room
    local rcl = self.rcl
    local sources = room:find(FIND.SOURCES) or {} ---@type Source[]
    local sites = room:find(FIND.CONSTRUCTION_SITES) or {} ---@type ConstructionSite[]
    local structures = room:find(FIND.STRUCTURES) or {} ---@type Structure[]
    local dropped = room:find(FIND.DROPPED_RESOURCES) or {} ---@type Resource[]

    local sourceCount = math.max(1, #sources)
    local srcContainers = self:countSourceContainers(sources)
    local useStaticMiners = (srcContainers > 0 and energyCap >= 550)

    local hasDamaged = false
    for _, s in ipairs(structures) do
        if s.hits and s.hitsMax and s.hitsMax > 0 and s.hits < s.hitsMax * 0.8 then
            hasDamaged = true
            break
        end
    end

    ---@type RoleCounts
    local targets = {
        harvester = 0,
        miner = 0,
        hauler = 0,
        upgrader = 1,
        builder = 0,
        defender = self.defense:desiredDefenders(),
        assault = 0,
        healer = 0,
        scout = 0,
        remoteHarvester = 0,
        remoteHauler = 0,
    }

    if useStaticMiners then
        targets.miner = math.min(sourceCount, srcContainers)
        if srcContainers < sourceCount then
            targets.harvester = 1
        end
        targets.hauler = math.max(2, targets.miner)
    else
        targets.harvester = math.min(3, math.max(2, sourceCount))
        if rcl >= 2 or #dropped > 0 then
            targets.hauler = 1
        end
    end

    if rcl >= 2 then
        targets.upgrader = 2
    end

    if #sites >= 5 then
        targets.builder = 2
    elseif #sites > 0 or hasDamaged or rcl >= 2 then
        targets.builder = 1
    end

    if targets.defender == 0 and rcl >= 2 then
        if energyCap >= 500 then
            targets.defender = 2
        elseif energyCap >= 400 then
            targets.defender = 1
        end
    end

    local scoutTarget = nil
    if rcl >= 2 and (self.counts.harvester + self.counts.miner) >= 1 and self.counts.hauler >= 1 then
        scoutTarget = self:findRoomNeedingScout()
        if scoutTarget then
            targets.scout = 1
        end
    end

    if rcl >= 2 and energyCap >= 450 and self.defense.threatLevel == 0 and self.memory.remoteRoom then
        local rIntel = screeps.Memory and screeps.Memory.roomIntel and screeps.Memory.roomIntel[self.memory.remoteRoom]
        local remoteSources = (rIntel and rIntel.sources) or 1
        targets.remoteHarvester = (energyCap >= 550 and remoteSources >= 2) and 2 or 1
        targets.remoteHauler = (targets.remoteHarvester >= 2) and 2 or 1
    end

    self:updateAttackTarget()
    if rcl >= 3 and energyCap >= 550 and self.defense.threatLevel == 0 and self.memory.attackTarget then
        targets.assault = 2
        targets.healer = (energyCap >= 800) and 2 or 1
    end

    return targets, scoutTarget
end

---Manage creep spawning for this colony with bootstrap, defense, economy, remote, and assault priorities.
function Colony:manageSpawning()
    local room = self.room
    local rmem = self.memory
    self:updateRemoteRoom()

    local energyAvail = room.energyAvailable or 0
    local energyCap = math.max(energyAvail, room.energyCapacityAvailable or energyAvail)
    local targets, scoutTarget = self:computeTargetRoster(energyCap)
    rmem.desiredRoles = targets

    local spawn = planner.getPrimarySpawn(room)
    if not spawn then
        rmem.nextSpawn = "no spawn"
        return
    end
    if spawn.spawning then
        local sname = (type(spawn.spawning) == "table" and spawn.spawning.name) or nil
        rmem.nextSpawn = sname and ("spawning " .. tostring(sname)) or "spawning"
        return
    end

    local counts = self.counts
    local roleToSpawn = nil
    local budget = energyCap

    if (counts.harvester + counts.miner) == 0 then
        roleToSpawn = "harvester"
        budget = energyAvail
    elseif counts.miner > 0 and counts.hauler == 0 then
        roleToSpawn = "hauler"
        budget = energyAvail
    elseif self.defense.threatLevel > 0 and counts.defender < targets.defender then
        roleToSpawn = "defender"
        if counts.defender == 0 then
            if energyAvail >= 320 then
                budget = energyAvail
            elseif (counts.harvester + counts.miner) == 0 and energyAvail >= 190 then
                budget = energyAvail
            else
                budget = math.min(energyCap, 320)
            end
        end
    elseif counts.miner < targets.miner then
        roleToSpawn = "miner"
    elseif counts.harvester < targets.harvester then
        roleToSpawn = "harvester"
    elseif counts.hauler < targets.hauler then
        roleToSpawn = "hauler"
    elseif counts.upgrader < targets.upgrader then
        roleToSpawn = "upgrader"
    elseif counts.builder < targets.builder then
        roleToSpawn = "builder"
    elseif counts.defender < targets.defender then
        roleToSpawn = "defender"
    elseif counts.scout < targets.scout then
        roleToSpawn = "scout"
    elseif counts.remoteHarvester < targets.remoteHarvester then
        roleToSpawn = "remoteHarvester"
    elseif counts.remoteHauler < targets.remoteHauler then
        roleToSpawn = "remoteHauler"
    elseif counts.healer < targets.healer then
        roleToSpawn = "healer"
    elseif counts.assault < targets.assault then
        roleToSpawn = "assault"
    end

    if not roleToSpawn then
        rmem.nextSpawn = "idle (economy full)"
        return
    end

    ---@type BodyPartType[]|nil
    local body
    if roleToSpawn == "miner" then
        body = buildMinerBody(budget)
    elseif roleToSpawn == "hauler" then
        body = buildHaulerBody(budget)
    elseif roleToSpawn == "defender" then
        body = DefenseManager.buildDefenderBody(budget)
    elseif roleToSpawn == "assault" then
        body = DefenseManager.buildAssaultBody(budget)
    elseif roleToSpawn == "healer" then
        body = DefenseManager.buildHealerBody(budget)
    elseif roleToSpawn == "scout" then
        body = buildScoutBody(budget)
    elseif roleToSpawn == "remoteHarvester" then
        body = buildRemoteHarvesterBody(budget)
    elseif roleToSpawn == "remoteHauler" then
        body = buildRemoteHaulerBody(budget)
    else
        body = buildWorkerBody(budget)
    end

    if not body then
        rmem.nextSpawn = string.format("saving for %s (%d/%d)", roleToSpawn, energyAvail, minRoleCost(roleToSpawn))
        return
    end

    local cost = bodyCost(body)
    if energyAvail < cost then
        rmem.nextSpawn = string.format("saving for %s (%d/%d)", roleToSpawn, energyAvail, cost)
        return
    end

    local creepMem = {
        role = roleToSpawn,
        room = room.name,
        working = false,
    }
    if roleToSpawn == "scout" and scoutTarget then
        creepMem.targetRoom = scoutTarget
    elseif (roleToSpawn == "remoteHarvester" or roleToSpawn == "remoteHauler") and rmem.remoteRoom then
        creepMem.targetRoom = rmem.remoteRoom
    elseif (roleToSpawn == "assault" or roleToSpawn == "healer") and rmem.attackTarget then
        creepMem.targetRoom = rmem.attackTarget
    end

    local name = roleToSpawn .. "_" .. tostring(screeps.Game.time or 0)
    local res = spawn:spawnCreep(body, name, { memory = creepMem })
    rmem.nextSpawn = (res == OK) and ("spawning " .. roleToSpawn) or ("waiting to spawn " .. roleToSpawn)
end

---Execute automated link energy transfers (source links -> controller link / hub link).
function Colony:runLinks()
    local room = self.room
    local myStructs = room:find(FIND.MY_STRUCTURES) ---@type Structure[]
    if not myStructs or #myStructs == 0 then
        return
    end

    ---@type StructureLink[]
    local links = {}
    ---@type StructureStorage|nil
    local storage = nil
    for _, s in ipairs(myStructs) do
        if s.structureType == STRUCTURE.LINK then
            table.insert(links, s --[[@as StructureLink]])
        elseif s.structureType == STRUCTURE.STORAGE then
            storage = s --[[@as StructureStorage]]
        end
    end
    if #links < 2 then
        return
    end

    local ctrl = room.controller
    local sources = room:find(FIND.SOURCES) or {} ---@type Source[]
    ---@type StructureLink|nil
    local ctrlLink = nil
    ---@type StructureLink|nil
    local hubLink = nil
    ---@type StructureLink[]
    local sourceLinks = {}

    for _, link in ipairs(links) do
        local isSourceLink = false
        for _, src in ipairs(sources) do
            if src.pos and link.pos and src.pos:getRangeTo(link) <= 3 then
                isSourceLink = true
                break
            end
        end
        if ctrl and ctrl.pos and link.pos and ctrl.pos:getRangeTo(link) <= 3 then
            ctrlLink = link
        elseif isSourceLink then
            table.insert(sourceLinks, link)
        elseif storage and storage.pos and link.pos and storage.pos:getRangeTo(link) <= 3 then
            hubLink = link
        elseif not hubLink then
            hubLink = link
        end
    end

    ---@param sender? StructureLink
    ---@param receiver? StructureLink
    ---@return boolean
    local function tryTransfer(sender, receiver)
        if not sender or not receiver or sender.id == receiver.id or not sender.transferEnergy then
            return false
        end
        if (sender.cooldown or 0) > 0 then
            return false
        end
        if getUsedEnergy(sender.store) < 400 or getFreeEnergy(receiver.store) < 200 then
            return false
        end
        if sender:transferEnergy(receiver) == OK then
            if room.visual and sender.pos and receiver.pos then
                room.visual:line(sender.pos.x, sender.pos.y, receiver.pos.x, receiver.pos.y, {
                    color = "#b388ff",
                    width = 0.14,
                    opacity = 0.7,
                })
            end
            return true
        end
        return false
    end

    for _, sLink in ipairs(sourceLinks) do
        if not tryTransfer(sLink, ctrlLink) then
            tryTransfer(sLink, hubLink)
        end
    end
    if hubLink and ctrlLink then
        tryTransfer(hubLink, ctrlLink)
    end
end

---@param targetRoom string
---@return FindConstant|nil
function Colony:getExitDirForVisual(targetRoom)
    local room = self.room
    if not targetRoom or room.name == targetRoom then
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

---Draw RoomVisual overlays (plan blueprint, spawn/controller rings, remote line, telemetry dots).
function Colony:drawVisuals()
    local room = self.room
    local vis = room.visual
    if not vis then
        return
    end

    planner.drawPlanVisuals(room)

    local spawn = planner.getPrimarySpawn(room)
    if spawn and spawn.pos then
        local spawnColor = "#66bb6a"
        if self.defense.threatLevel > 0 then
            spawnColor = "#ef5350"
        elseif spawn.spawning then
            spawnColor = "#ffca28"
        end
        vis:circle(spawn.pos.x, spawn.pos.y, {
            radius = 0.8,
            fill = "transparent",
            stroke = spawnColor,
            opacity = 0.8,
        })
    end

    if room.controller and room.controller.pos then
        vis:circle(room.controller.pos.x, room.controller.pos.y, {
            radius = 0.8,
            fill = "transparent",
            stroke = "#7e57c2",
            opacity = 0.75,
        })
    end

    local remoteRoom = self.memory.remoteRoom
    if remoteRoom and spawn and spawn.pos then
        local exitDir = self:getExitDirForVisual(remoteRoom)
        if exitDir then
            local exitTiles = room:find(exitDir) or {}
            if #exitTiles > 0 then
                local mid = exitTiles[math.max(1, (#exitTiles + 1) // 2)]
                if mid and mid.x and mid.y then
                    vis:line(spawn.pos.x, spawn.pos.y, mid.x, mid.y, {
                        color = "#00e5ff",
                        width = 0.08,
                        opacity = 0.45,
                        lineStyle = "dashed",
                    })
                    vis:circle(mid.x, mid.y, {
                        radius = 0.45,
                        fill = "transparent",
                        stroke = "#00e5ff",
                        opacity = 0.6,
                    })
                end
            end
        end
    end

    local dotX = 2
    for _ = 1, math.min(self.rcl, 8) do
        vis:circle(dotX, 1.5, {
            radius = 0.24,
            fill = "#7e57c2",
            stroke = "#ffffff",
            opacity = 0.85,
        })
        dotX = dotX + 0.68
    end

    if self.defense.threatLevel > 0 then
        vis:circle(dotX + 0.5, 1.5, {
            radius = 0.3,
            fill = "#ef5350",
            stroke = "#ffffff",
            opacity = 0.9,
        })
    end

    local roleColors = {
        { count = self.counts.miner, color = "#ffa726" },
        { count = self.counts.harvester, color = "#ffeb3b" },
        { count = self.counts.hauler, color = "#26c6da" },
        { count = self.counts.upgrader, color = "#7e57c2" },
        { count = self.counts.builder, color = "#66bb6a" },
        { count = self.counts.defender, color = "#ef5350" },
        { count = self.counts.assault, color = "#ff5252" },
        { count = self.counts.healer, color = "#69f0ae" },
        { count = self.counts.scout, color = "#80deea" },
        { count = self.counts.remoteHarvester, color = "#00e5ff" },
        { count = self.counts.remoteHauler, color = "#00b8d4" },
    }
    local creepX = 2
    for _, entry in ipairs(roleColors) do
        for _ = 1, math.min(entry.count, 10) do
            vis:circle(creepX, 2.5, {
                radius = 0.2,
                fill = entry.color,
                opacity = 0.85,
            })
            creepX = creepX + 0.52
        end
    end
end

---Run one full tick for this owned colony.
function Colony:run()
    self:countRoles()
    self.defense:assessThreat()
    self.defense:checkSafeMode(self.counts.defender)
    self.defense:runTowers()
    self:runLinks()
    self:manageSpawning()

    if (not self.memory.plan) or (screeps.Game.time and (screeps.Game.time % PLANNER_INTERVAL == 0)) then
        planner.planRoom(self.room, self.rcl)
    end

    self:drawVisuals()
end

return Colony
