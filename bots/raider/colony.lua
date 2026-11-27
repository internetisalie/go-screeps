-- colony.lua: Object-oriented RaiderColony manager for an owned Raider room.
-- Coordinates home defense, economy, scouting, synchronized raid wave spawning, planning, and HUD visuals.
-- Idiomatic Lua 5.4 (arnodel/golua) with Lunar/LuaLS annotations.

local screeps <const> = require("screeps")
local OK <const> = screeps.OK
local FIND <const> = screeps.FIND
local PART <const> = screeps.PART
local COST <const> = screeps.COST

local economy <const> = require("economy")
local planner <const> = require("planner")
local combat <const> = require("combat")

local PLANNER_INTERVAL <const> = 20

---@class RaiderRoleCounts
---@field harvester integer
---@field hauler integer
---@field upgrader integer
---@field builder integer
---@field defender integer
---@field scout integer
---@field brawler integer
---@field skirmisher integer
---@field medic integer
---@field total integer

---@class RaiderColony
---@field room Room
---@field rcl integer
---@field memory table<string, any>
---@field roomCounts RaiderRoleCounts
---@field globalCounts RaiderRoleCounts
---@field threatLevel integer
---@field hostiles Creep[]
local RaiderColony = {}
RaiderColony.__index = RaiderColony

---@return RaiderRoleCounts
local function makeRoleCounter()
    return {
        harvester = 0,
        hauler = 0,
        upgrader = 0,
        builder = 0,
        defender = 0,
        scout = 0,
        brawler = 0,
        skirmisher = 0,
        medic = 0,
        total = 0,
    }
end

---@param body BodyPartType[]
---@return integer
local function bodyCost(body)
    local total = 0
    for _, part in ipairs(body) do
        total = total + (COST[part] or 0)
    end
    return total
end

---@param role string
---@return integer
local function minRoleCost(role)
    if role == "scout" then
        return 50
    elseif role == "hauler" then
        return 150
    elseif role == "defender" then
        return 190
    elseif role == "brawler" then
        return 260
    elseif role == "skirmisher" then
        return 200
    elseif role == "medic" then
        return 300
    end
    return 200
end

---Count all owned creeps globally by role.
---@return RaiderRoleCounts
function RaiderColony.countGlobalRoles()
    local counts = makeRoleCounter()
    if not screeps.Game.creeps then
        return counts
    end
    for _, creep in pairs(screeps.Game.creeps) do
        counts.total = counts.total + 1
        local role = (creep.memory and creep.memory.role) or "harvester"
        if counts[role] ~= nil then
            counts[role] = counts[role] + 1
        end
    end
    return counts
end

---Construct a RaiderColony manager bound to an owned Room.
---@param room Room
---@param globalCounts RaiderRoleCounts
---@return RaiderColony
function RaiderColony.new(room, globalCounts)
    if not screeps.Memory.rooms then
        screeps.Memory.rooms = {}
    end
    if not screeps.Memory.rooms[room.name] then
        screeps.Memory.rooms[room.name] = room.memory or {}
    end
    return setmetatable({
        room = room,
        rcl = (room.controller and room.controller.level) or 0,
        memory = screeps.Memory.rooms[room.name],
        roomCounts = makeRoleCounter(),
        globalCounts = globalCounts,
        threatLevel = 0,
        hostiles = {},
    }, RaiderColony)
end

---@return RaiderRoleCounts
function RaiderColony:countRoomRoles()
    local counts = makeRoleCounter()
    local myCreeps = self.room:find(FIND.MY_CREEPS) or {} ---@type Creep[]
    for _, creep in ipairs(myCreeps) do
        counts.total = counts.total + 1
        local role = (creep.memory and creep.memory.role) or "harvester"
        if counts[role] ~= nil then
            counts[role] = counts[role] + 1
        end
    end
    self.roomCounts = counts
    return counts
end

---@return boolean
function RaiderColony:needsScout()
    if self.globalCounts.scout > 0 then
        return false
    end
    local adj = combat.getAdjacentRooms(self.room)
    if #adj == 0 then
        return false
    end
    if not screeps.Memory.raidTarget then
        return true
    end
    local info = screeps.Memory.scoutedRooms and screeps.Memory.scoutedRooms[screeps.Memory.raidTarget]
    if not info or ((screeps.Game.time or 0) - (info.lastSeen or 0)) > 250 then
        return true
    end
    return false
end

---Manage spawning for economy, home defense, scouting, and synchronized raid waves.
function RaiderColony:manageSpawning()
    local room = self.room
    local rmem = self.memory
    local roomCounts = self.roomCounts
    local globalCounts = self.globalCounts
    local rcl = self.rcl

    local energyAvail = room.energyAvailable or 0
    local energyCap = math.max(energyAvail, room.energyCapacityAvailable or energyAvail)

    local sources = room:find(FIND.SOURCES) or {} ---@type Source[]
    local sites = room:find(FIND.MY_CONSTRUCTION_SITES) or room:find(FIND.CONSTRUCTION_SITES) or {} ---@type ConstructionSite[]
    local sourceCount = math.max(1, #sources)

    local wantHarvesters = math.max(2, math.min(3, sourceCount))
    local wantHaulers = (rcl >= 2 or sourceCount >= 2) and 2 or 1
    local wantUpgraders = (rcl >= 2) and 2 or 1
    local wantBuilders = (#sites >= 4) and 2 or ((#sites > 0 or rcl >= 2) and 1 or 0)
    local wantDefenders = (self.threatLevel > 0) and math.min(3, math.max(1, math.ceil(#self.hostiles / 2))) or 0
    local wantScout = self:needsScout() and 1 or 0

    local ecoHealthy = (roomCounts.harvester >= 2 and energyCap >= 300)
    local wantBrawlers, wantSkirmishers, wantMedics = 0, 0, 0
    if ecoHealthy and self.threatLevel == 0 and screeps.Memory.raidTarget then
        wantBrawlers = (energyCap >= 800) and 3 or 2
        wantSkirmishers = (energyCap >= 550) and 2 or 1
        if energyCap >= 800 then
            wantMedics = 2
        elseif energyCap >= 550 then
            wantMedics = 1
        end
    end

    rmem.desiredRoles = {
        harvester = wantHarvesters,
        hauler = wantHaulers,
        upgrader = wantUpgraders,
        builder = wantBuilders,
        defender = wantDefenders,
        scout = wantScout,
        brawler = wantBrawlers,
        skirmisher = wantSkirmishers,
        medic = wantMedics,
    }

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

    local roleToSpawn = nil
    local budget = energyCap

    if roomCounts.harvester == 0 then
        roleToSpawn = "harvester"
        budget = energyAvail
    elseif roomCounts.defender < wantDefenders then
        roleToSpawn = "defender"
        if roomCounts.defender == 0 and energyAvail >= 190 then
            budget = math.max(energyAvail, math.min(energyCap, 550))
        end
    elseif roomCounts.harvester < wantHarvesters then
        roleToSpawn = "harvester"
        if roomCounts.harvester < 2 and roomCounts.hauler == 0 then
            budget = math.min(energyAvail, 250)
        end
    elseif roomCounts.hauler < wantHaulers then
        roleToSpawn = "hauler"
        if roomCounts.hauler == 0 and energyAvail >= 150 then
            budget = math.min(energyAvail, 300)
        end
    elseif roomCounts.upgrader < wantUpgraders then
        roleToSpawn = "upgrader"
    elseif roomCounts.builder < wantBuilders then
        roleToSpawn = "builder"
    elseif wantScout > 0 then
        roleToSpawn = "scout"
        budget = 50
    elseif globalCounts.brawler < wantBrawlers and globalCounts.brawler <= globalCounts.skirmisher then
        roleToSpawn = "brawler"
    elseif globalCounts.skirmisher < wantSkirmishers then
        roleToSpawn = "skirmisher"
    elseif globalCounts.medic < wantMedics then
        roleToSpawn = "medic"
    elseif globalCounts.brawler < wantBrawlers then
        roleToSpawn = "brawler"
    end

    if not roleToSpawn then
        rmem.nextSpawn = "idle (economy full)"
        return
    end

    ---@type BodyPartType[]|nil
    local body = nil
    if roleToSpawn == "harvester" then
        body = economy.buildHarvesterBody(budget, roomCounts.hauler > 0)
    elseif roleToSpawn == "hauler" then
        body = economy.buildHaulerBody(budget)
    elseif roleToSpawn == "upgrader" or roleToSpawn == "builder" then
        body = economy.buildWorkerBody(budget)
    elseif roleToSpawn == "defender" then
        body = combat.buildDefenderBody(budget)
    elseif roleToSpawn == "scout" then
        body = { PART.MOVE }
    elseif roleToSpawn == "brawler" then
        body = combat.buildBrawlerBody(budget)
    elseif roleToSpawn == "skirmisher" then
        body = combat.buildSkirmisherBody(budget)
    elseif roleToSpawn == "medic" then
        body = combat.buildMedicBody(budget)
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

    local name = "R_" .. roleToSpawn .. "_" .. tostring(screeps.Game.time or 0)
    local res = spawn:spawnCreep(body, name, {
        memory = {
            role = roleToSpawn,
            homeRoom = room.name,
            working = false,
            delivering = false,
            state = "rally",
            rallyWait = 0,
        },
    })
    rmem.nextSpawn = (res == OK) and ("spawning " .. roleToSpawn) or ("waiting to spawn " .. roleToSpawn)
end

---Draw crimson/amber tactical RoomVisual HUD for this Raider home room.
function RaiderColony:drawVisuals()
    local room = self.room
    local vis = room.visual
    if not vis then
        return
    end

    planner.drawPlanVisuals(room)

    local spawn = planner.getPrimarySpawn(room)
    if spawn and spawn.pos then
        local ringColor = (self.threatLevel > 0) and "#ff1744" or (spawn.spawning and "#ffab40" or "#ff5252")
        vis:circle(spawn.pos.x, spawn.pos.y, {
            radius = 0.85,
            fill = "transparent",
            stroke = ringColor,
            strokeWidth = 0.14,
            opacity = 0.85,
        })
    end

    if room.controller and room.controller.pos then
        vis:circle(room.controller.pos.x, room.controller.pos.y, {
            radius = 0.8,
            fill = "transparent",
            stroke = "#ffab40",
            opacity = 0.8,
        })
    end

    local dotX = 2
    for _ = 1, math.min(self.rcl, 8) do
        vis:circle(dotX, 1.5, {
            radius = 0.24,
            fill = "#ffab40",
            stroke = "#3e2723",
            opacity = 0.9,
        })
        dotX = dotX + 0.68
    end

    if self.threatLevel > 0 then
        vis:circle(dotX + 0.5, 1.5, {
            radius = 0.3,
            fill = "#ff1744",
            stroke = "#ffffff",
            opacity = 0.95,
        })
    end

    local roleColors = {
        { count = self.roomCounts.harvester, color = "#ffeb3b" },
        { count = self.roomCounts.hauler, color = "#ffab40" },
        { count = self.roomCounts.upgrader, color = "#ce93d8" },
        { count = self.roomCounts.builder, color = "#81c784" },
        { count = self.roomCounts.defender, color = "#ff5252" },
        { count = self.globalCounts.brawler, color = "#d50000" },
        { count = self.globalCounts.skirmisher, color = "#ff6d00" },
        { count = self.globalCounts.medic, color = "#69f0ae" },
        { count = self.globalCounts.scout, color = "#80deea" },
    }
    local creepX = 2
    for _, entry in ipairs(roleColors) do
        for _ = 1, math.min(entry.count, 10) do
            vis:circle(creepX, 2.5, {
                radius = 0.2,
                fill = entry.color,
                opacity = 0.88,
            })
            creepX = creepX + 0.52
        end
    end
end

---Run one full tick for this owned Raider colony.
function RaiderColony:run()
    self:countRoomRoles()
    self.threatLevel, self.hostiles = combat.assessThreat(self.room)
    combat.runTowers(self.room, self.hostiles)
    self:manageSpawning()

    if (not self.memory.plan) or (screeps.Game.time and (screeps.Game.time % PLANNER_INTERVAL == 0)) then
        planner.planRoom(self.room, self.rcl)
    end

    self:drawVisuals()
end

return RaiderColony
