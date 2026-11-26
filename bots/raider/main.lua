-- main.lua: Aggressive Raider Screeps colony AI entry point.
-- Fast-economy + cross-room scouting & raiding squads (brawlers, skirmishers, medics).
-- Modules: economy.lua, planner.lua, combat.lua.
-- Strict Lua 5.1 (gopher-lua compatible).

local economy = require("economy")
local planner = require("planner")
local combat = require("combat")

local PLANNER_INTERVAL = 20

local function bodyCost(body)
    local total = 0
    for _, part in ipairs(body) do
        if part == WORK then
            total = total + 100
        elseif part == CARRY or part == MOVE then
            total = total + 50
        elseif part == ATTACK then
            total = total + 80
        elseif part == RANGED_ATTACK then
            total = total + 150
        elseif part == HEAL then
            total = total + 250
        elseif part == CLAIM then
            total = total + 600
        elseif part == TOUGH then
            total = total + 10
        end
    end
    return total
end

local function cleanupMemory()
    if not Memory.creeps then
        Memory.creeps = {}
    else
        for name, _ in pairs(Memory.creeps) do
            if not Game.creeps or not Game.creeps[name] then
                Memory.creeps[name] = nil
            end
        end
    end
    if not Memory.rooms then
        Memory.rooms = {}
    end
    if not Memory.scoutedRooms then
        Memory.scoutedRooms = {}
    end
end

local function ensureRoomMemory(room)
    if not room or not room.name then
        return nil
    end
    if not Memory.rooms then
        Memory.rooms = {}
    end
    if not Memory.rooms[room.name] then
        Memory.rooms[room.name] = room.memory or {}
    end
    return Memory.rooms[room.name]
end

local function adoptUnassignedCreeps()
    if not Game.creeps then
        return
    end
    for _, creep in pairs(Game.creeps) do
        if creep.memory and creep.memory.role == nil then
            local attack, ranged, heal, work, carry = 0, 0, 0, 0, 0
            if creep.body then
                for _, part in ipairs(creep.body) do
                    local pt = (type(part) == "table" and part.type) or part
                    if pt == ATTACK then
                        attack = attack + 1
                    elseif pt == RANGED_ATTACK then
                        ranged = ranged + 1
                    elseif pt == HEAL then
                        heal = heal + 1
                    elseif pt == WORK then
                        work = work + 1
                    elseif pt == CARRY then
                        carry = carry + 1
                    end
                end
            end

            local role
            if heal > 0 then
                role = "medic"
            elseif ranged > 0 then
                role = "skirmisher"
            elseif attack > 0 then
                role = "brawler"
            elseif work == 0 and carry == 0 then
                role = "scout"
            elseif work == 0 and carry > 0 then
                role = "hauler"
            else
                role = "harvester"
            end

            creep.memory.role = role
            if creep.room then
                creep.memory.homeRoom = creep.memory.homeRoom or creep.room.name
            end
        end
    end
end

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

local function countGlobalRoles()
    local counts = makeRoleCounter()
    if not Game.creeps then
        return counts
    end
    for _, creep in pairs(Game.creeps) do
        counts.total = counts.total + 1
        local role = (creep.memory and creep.memory.role) or "harvester"
        if counts[role] ~= nil then
            counts[role] = counts[role] + 1
        end
    end
    return counts
end

local function countRoomRoles(room)
    local counts = makeRoleCounter()
    local myCreeps = room:find(FIND_MY_CREEPS) or {}
    for _, creep in ipairs(myCreeps) do
        counts.total = counts.total + 1
        local role = (creep.memory and creep.memory.role) or "harvester"
        if counts[role] ~= nil then
            counts[role] = counts[role] + 1
        end
    end
    return counts
end

local function needsScout(homeRoom, globalCounts)
    if globalCounts.scout > 0 then
        return false
    end
    local adj = combat.getAdjacentRooms(homeRoom)
    if #adj == 0 then
        return false
    end
    if not Memory.raidTarget then
        return true
    end
    -- Re-scout periodically if target intel is older than 250 ticks.
    local info = Memory.scoutedRooms and Memory.scoutedRooms[Memory.raidTarget]
    if not info or ((Game.time or 0) - (info.lastSeen or 0)) > 250 then
        return true
    end
    return false
end

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

local function manageSpawning(room, roomCounts, globalCounts, rcl, threatLevel, hostiles)
    local rmem = ensureRoomMemory(room)

    local energyAvail = room.energyAvailable or 0
    local energyCap = room.energyCapacityAvailable or energyAvail
    if energyCap < energyAvail then
        energyCap = energyAvail
    end

    local sources = room:find(FIND_SOURCES) or {}
    local sites = room:find(FIND_MY_CONSTRUCTION_SITES) or room:find(FIND_CONSTRUCTION_SITES) or {}
    local sourceCount = math.max(1, #sources)

    -- Economic targets: lean, aggressive RCL 2 -> RCL 3 ramp.
    local wantHarvesters = math.max(2, math.min(3, sourceCount))
    local wantHaulers = 1
    if rcl >= 2 or sourceCount >= 2 then
        wantHaulers = 2
    end
    local wantUpgraders = 1
    if rcl >= 2 then
        wantUpgraders = 2
    end
    local wantBuilders = 0
    if #sites >= 4 then
        wantBuilders = 2
    elseif #sites > 0 or rcl >= 2 then
        wantBuilders = 1
    end

    local wantDefenders = 0
    if threatLevel > 0 then
        wantDefenders = math.min(3, math.max(1, math.ceil(#hostiles / 2)))
    end

    local wantScout = needsScout(room, globalCounts) and 1 or 0

    -- Raid squad targets (active once economy is healthy and energyCap >= 300).
    local ecoHealthy = (roomCounts.harvester >= 2 and energyCap >= 300)
    local wantBrawlers = 0
    local wantSkirmishers = 0
    local wantMedics = 0
    if ecoHealthy and threatLevel == 0 and Memory.raidTarget then
        wantBrawlers = (energyCap >= 800) and 3 or 2
        wantSkirmishers = (energyCap >= 550) and 2 or 1
        if energyCap >= 800 then
            wantMedics = 2
        elseif energyCap >= 550 then
            wantMedics = 1
        end
    end

    if rmem then
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
    end

    local spawn = planner.getPrimarySpawn(room)
    if not spawn then
        if rmem then
            rmem.nextSpawn = "no spawn"
        end
        return
    end
    if spawn.spawning then
        if rmem then
            local sname = (type(spawn.spawning) == "table" and spawn.spawning.name) or nil
            if sname then
                rmem.nextSpawn = "spawning " .. tostring(sname)
            else
                rmem.nextSpawn = "spawning"
            end
        end
        return
    end

    local roleToSpawn = nil
    local budget = energyCap

    -- 1. Emergency recovery when no harvesters exist.
    if roomCounts.harvester == 0 then
        roleToSpawn = "harvester"
        budget = energyAvail
    -- 2. Emergency home defense.
    elseif roomCounts.defender < wantDefenders then
        roleToSpawn = "defender"
        if roomCounts.defender == 0 and energyAvail >= 190 then
            budget = math.max(energyAvail, math.min(energyCap, 550))
        end
    -- 3. Core economy: harvesters -> haulers -> upgraders -> builders.
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
    -- 4. Cheap scout to discover rival colonies in adjacent rooms.
    elseif wantScout > 0 then
        roleToSpawn = "scout"
        budget = 50
    -- 5. Raiding squad composition (balanced brawler + skirmisher + medic).
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
        if rmem then
            rmem.nextSpawn = "idle (economy full)"
        end
        return
    end

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
        body = { MOVE }
    elseif roleToSpawn == "brawler" then
        body = combat.buildBrawlerBody(budget)
    elseif roleToSpawn == "skirmisher" then
        body = combat.buildSkirmisherBody(budget)
    elseif roleToSpawn == "medic" then
        body = combat.buildMedicBody(budget)
    end

    if not body then
        if rmem then
            rmem.nextSpawn = string.format("saving for %s (%d/%d)", roleToSpawn, energyAvail, minRoleCost(roleToSpawn))
        end
        return
    end

    local cost = bodyCost(body)
    if energyAvail < cost then
        if rmem then
            rmem.nextSpawn = string.format("saving for %s (%d/%d)", roleToSpawn, energyAvail, cost)
        end
        return
    end

    local name = "R_" .. roleToSpawn .. "_" .. tostring(Game.time or 0)
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
    if rmem then
        if res == OK then
            rmem.nextSpawn = "spawning " .. roleToSpawn
        else
            rmem.nextSpawn = "waiting to spawn " .. roleToSpawn
        end
    end
end

-- Crimson/amber tactical RoomVisual HUD for home room.
local function drawHomeVisuals(room, roomCounts, globalCounts, rcl, threatLevel)
    local vis = room.visual
    if not vis then
        return
    end

    planner.drawPlanVisuals(room)

    local spawn = planner.getPrimarySpawn(room)
    if spawn and spawn.pos then
        local ringColor = (threatLevel > 0) and "#ff1744" or (spawn.spawning and "#ffab40" or "#ff5252")
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

    -- Top-left RCL pips in amber.
    local dotX = 2
    for _ = 1, math.min(rcl, 8) do
        vis:circle(dotX, 1.5, {
            radius = 0.24,
            fill = "#ffab40",
            stroke = "#3e2723",
            opacity = 0.9,
        })
        dotX = dotX + 0.68
    end

    if threatLevel > 0 then
        vis:circle(dotX + 0.5, 1.5, {
            radius = 0.32,
            fill = "#ff1744",
            stroke = "#ffffff",
            opacity = 0.95,
        })
    end

    -- Role count pips in crimson/amber palette.
    local rolePips = {
        { count = roomCounts.harvester, color = "#ffab40" },
        { count = roomCounts.hauler, color = "#ffd54f" },
        { count = roomCounts.upgrader, color = "#ff8a65" },
        { count = roomCounts.builder, color = "#ffb74d" },
        { count = roomCounts.defender, color = "#ff5252" },
        { count = globalCounts.brawler, color = "#d50000" },
        { count = globalCounts.skirmisher, color = "#ff6e40" },
        { count = globalCounts.medic, color = "#69f0ae" },
        { count = globalCounts.scout, color = "#ffe082" },
    }
    local creepX = 2
    for _, entry in ipairs(rolePips) do
        for _ = 1, math.min(entry.count, 8) do
            vis:circle(creepX, 2.5, {
                radius = 0.21,
                fill = entry.color,
                stroke = "#212121",
                opacity = 0.9,
            })
            creepX = creepX + 0.62
        end
    end

    if vis.text then
        local raidCount = globalCounts.brawler + globalCounts.skirmisher + globalCounts.medic
        local targetLabel = Memory.raidTarget or "SCOUTING"
        local hud = string.format(
            "[RAIDER] RCL %d | E:%d/%d | H:%d Ha:%d U:%d B:%d | RAID:%d -> %s",
            rcl,
            room.energyAvailable or 0,
            room.energyCapacityAvailable or 0,
            roomCounts.harvester,
            roomCounts.hauler,
            roomCounts.upgrader,
            roomCounts.builder,
            raidCount,
            targetLabel
        )
        vis:text(hud, 2, 3.8, { color = "#ffab40", opacity = 0.9 })
    end
end

-- Crimson target overlay in remote/enemy raid rooms.
local function drawRaidRoomVisuals(room)
    local vis = room.visual
    if not vis then
        return
    end
    local hSpawns = room:find(FIND_HOSTILE_SPAWNS) or {}
    for _, sp in ipairs(hSpawns) do
        if sp.pos then
            vis:circle(sp.pos.x, sp.pos.y, {
                radius = 1.0,
                fill = "transparent",
                stroke = "#ff5252",
                strokeWidth = 0.16,
                opacity = 0.9,
            })
        end
    end
    local hCreeps = room:find(FIND_HOSTILE_CREEPS) or {}
    for _, hc in ipairs(hCreeps) do
        if hc.pos then
            vis:circle(hc.pos.x, hc.pos.y, {
                radius = 0.55,
                fill = "transparent",
                stroke = "#ffab40",
                opacity = 0.8,
            })
        end
    end
    if vis.text then
        vis:text("[RAID ZONE] " .. tostring(room.name), 2, 2.0, { color = "#ff5252", opacity = 0.9 })
    end
end

function loop()
    cleanupMemory()
    adoptUnassignedCreeps()

    local globalCounts = countGlobalRoles()
    local activeRaiders = globalCounts.brawler + globalCounts.skirmisher + globalCounts.medic
    if activeRaiders == 0 then
        Memory.raidActive = false
    end

    if Game.rooms then
        for _, room in pairs(Game.rooms) do
            local spawn = planner.getPrimarySpawn(room)
            if spawn then
                -- Home colony room.
                local rcl = (room.controller and room.controller.level) or 1
                local roomCounts = countRoomRoles(room)
                local threatLevel, hostiles = combat.assessThreat(room)

                combat.runTowers(room, hostiles)
                manageSpawning(room, roomCounts, globalCounts, rcl, threatLevel, hostiles)

                local rmem = ensureRoomMemory(room)
                if (rmem and not rmem.raiderPlan) or (Game.time and (Game.time % PLANNER_INTERVAL == 0)) then
                    planner.planRoom(room, rcl)
                end

                drawHomeVisuals(room, roomCounts, globalCounts, rcl, threatLevel)
            else
                -- Remote / scouted / raid room.
                combat.updateIntel(room)
                drawRaidRoomVisuals(room)
            end
        end
    end

    if Game.creeps then
        for _, creep in pairs(Game.creeps) do
            local role = creep.memory and creep.memory.role
            if role == "harvester" or role == "hauler" or role == "builder" or role == "upgrader" then
                economy.run(creep)
            else
                combat.run(creep)
            end
        end
    end
end
