-- main.lua: Comprehensive Screeps autonomous colony AI entry point.
-- Modules: planner.lua, defense.lua, roles.lua.
-- Compatible with standard Lua 5.1 / gopher-lua.

local roles = require("roles")
local planner = require("planner")
local defense = require("defense")

local PLANNER_INTERVAL = 25
local SCOUT_INTEL_TTL = 1500
local STRUCT_LINK = STRUCTURE_LINK or "link"
local STRUCT_STORAGE = STRUCTURE_STORAGE or "storage"

local EXIT_DIRS = {
    [1] = FIND_EXIT_TOP or 1,
    [3] = FIND_EXIT_RIGHT or 3,
    [5] = FIND_EXIT_BOTTOM or 5,
    [7] = FIND_EXIT_LEFT or 7,
}

local function getUsedEnergy(store)
    if not store then
        return 0
    end
    return store:getUsedCapacity(RESOURCE_ENERGY) or 0
end

local function getFreeEnergy(store)
    if not store then
        return 0
    end
    return store:getFreeCapacity(RESOURCE_ENERGY) or 0
end

-- Remove stale entries for dead creeps and initialize room memory table.
local function cleanupMemory()
    if not Memory.creeps then
        Memory.creeps = {}
    else
        for name, _ in pairs(Memory.creeps) do
            if not Game.creeps[name] then
                Memory.creeps[name] = nil
            end
        end
    end
    if not Memory.rooms then
        Memory.rooms = {}
    end
    if not Memory.roomIntel then
        Memory.roomIntel = {}
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

-- Build a balanced mobile worker body (WORK, CARRY, MOVE) scaled to energy budget.
local function buildWorkerBody(budget)
    if budget < 200 then
        return nil
    end
    local body = { WORK, CARRY, MOVE }
    local cost = 200

    while cost + 200 <= budget and #body + 3 <= 18 do
        table.insert(body, WORK)
        table.insert(body, CARRY)
        table.insert(body, MOVE)
        cost = cost + 200
    end

    if cost + 100 <= budget and #body + 2 <= 18 then
        table.insert(body, CARRY)
        table.insert(body, MOVE)
    end

    return body
end

-- Build a high-WORK static miner body (up to 5 WORK, 1 CARRY, 1-2 MOVE).
local function buildMinerBody(budget)
    if budget < 300 then
        return nil
    end

    -- 5 WORK harvests 10 energy/tick (full 3000-energy source regen rate).
    local workParts = math.min(5, math.floor((budget - 100) / 100))
    if workParts < 2 then
        workParts = 2
    end

    local body = {}
    local cost = 0
    for _ = 1, workParts do
        table.insert(body, WORK)
        cost = cost + 100
    end

    if cost + 50 <= budget then
        table.insert(body, CARRY)
        cost = cost + 50
    end

    local moveParts = 1
    if cost + 100 <= budget then
        moveParts = 2
    end
    for _ = 1, moveParts do
        if cost + 50 <= budget then
            table.insert(body, MOVE)
            cost = cost + 50
        end
    end

    return body
end

-- Build a hauler body (CARRY, CARRY, MOVE) scaled to energy budget.
local function buildHaulerBody(budget)
    if budget < 150 then
        return nil
    end
    local body = { CARRY, CARRY, MOVE }
    local cost = 150

    while cost + 150 <= budget and #body + 3 <= 18 do
        table.insert(body, CARRY)
        table.insert(body, CARRY)
        table.insert(body, MOVE)
        cost = cost + 150
    end

    if cost + 100 <= budget and #body + 2 <= 18 then
        table.insert(body, CARRY)
        table.insert(body, MOVE)
    end

    return body
end

-- Build a cheap 1-MOVE scout body (50 energy).
local function buildScoutBody(budget)
    if budget < 50 then
        return nil
    end
    return { MOVE }
end

-- Build a mobile remote harvester body (400 or 550 energy, 1:1 move on plains).
local function buildRemoteHarvesterBody(budget)
    if budget < 400 then
        return nil
    end
    if budget >= 550 then
        return { WORK, WORK, WORK, CARRY, MOVE, MOVE, MOVE, MOVE }
    end
    return { WORK, WORK, CARRY, MOVE, MOVE, MOVE }
end

-- Build a fast 1:1 CARRY/MOVE remote hauler body (200 to 500 energy).
local function buildRemoteHaulerBody(budget)
    if budget < 200 then
        return nil
    end
    local capped = math.min(budget, 500)
    local pairsCount = math.min(5, math.floor(capped / 100))
    if pairsCount < 2 then
        pairsCount = 2
    end
    local body = {}
    for _ = 1, pairsCount do
        table.insert(body, CARRY)
    end
    for _ = 1, pairsCount do
        table.insert(body, MOVE)
    end
    return body
end

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

-- Return sorted list of adjacent connected room names using Game.map.describeExits.
local function getAdjacentRoomNames(roomName)
    local result = {}
    if not roomName or not Game.map or not Game.map.describeExits then
        return result
    end
    local exits = Game.map.describeExits(roomName)
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

-- Return an adjacent room name that has not been scouted in the last SCOUT_INTEL_TTL ticks, or nil.
local function findRoomNeedingScout(homeRoomName)
    local adj = getAdjacentRoomNames(homeRoomName)
    if #adj == 0 then
        return nil
    end
    local now = Game.time or 0
    local intelMap = (Memory and Memory.roomIntel) or {}
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

-- Pick up to 1 adjacent unowned, safe room (owner == nil, hostiles == 0, sources > 0) as rmem.remoteRoom.
local function updateRemoteRoomSelection(room, rmem)
    if not room or not room.name or not rmem then
        return nil
    end
    local adj = getAdjacentRoomNames(room.name)
    if #adj == 0 then
        rmem.remoteRoom = nil
        return nil
    end
    local intelMap = (Memory and Memory.roomIntel) or {}
    local bestRoom = nil
    local bestSources = 0
    for _, rName in ipairs(adj) do
        local info = intelMap[rName]
        if info and info.owner == nil and (info.hostiles or 0) == 0 and (info.sources or 0) > 0 then
            if info.sources > bestSources then
                bestSources = info.sources
                bestRoom = rName
            end
        end
    end
    rmem.remoteRoom = bestRoom
    return bestRoom
end

-- Count owned creeps per role belonging to a home room (including creeps operating in remote rooms).
local PRESPAWN_TTL = {
    defender = 120,
    miner = 100,
    hauler = 80,
    harvester = 80,
    remoteHarvester = 140,
    remoteHauler = 120,
}

local function countRoles(room)
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
    if Game.creeps then
        for _, creep in pairs(Game.creeps) do
            local home = (creep.memory and creep.memory.room) or (creep.room and creep.room.name)
            if home == room.name then
                counts.total = counts.total + 1
                local role = (creep.memory and creep.memory.role) or "harvester"
                local lead = PRESPAWN_TTL[role]
                local expiring = lead and (not creep.spawning) and creep.ticksToLive and creep.ticksToLive <= lead
                if counts[role] ~= nil and not expiring then
                    counts[role] = counts[role] + 1
                end
            end
        end
        return counts
    end

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

-- Count how many sources have an adjacent built container.
local function countSourceContainers(room, sources)
    local structs = room:find(FIND_STRUCTURES) or {}
    local ready = 0
    for _, src in ipairs(sources) do
        if src.pos then
            for _, s in ipairs(structs) do
                if s.structureType == STRUCTURE_CONTAINER and s.pos and src.pos:getRangeTo(s) <= 1 then
                    ready = ready + 1
                    break
                end
            end
        end
    end
    return ready
end

-- Compute desired creep counts for a room based on RCL, infrastructure, threat level, and remote intel.
local function getTargetRoster(room, rcl, energyCap, threatLevel, hostiles, counts, rmem)
    local sources = room:find(FIND_SOURCES) or {}
    local sites = room:find(FIND_CONSTRUCTION_SITES) or {}
    local structures = room:find(FIND_STRUCTURES) or {}
    local dropped = room:find(FIND_DROPPED_RESOURCES) or {}

    local sourceCount = math.max(1, #sources)
    local srcContainers = countSourceContainers(room, sources)
    local useStaticMiners = (srcContainers > 0 and energyCap >= 550)

    local hasDamaged = false
    for _, s in ipairs(structures) do
        if s.hits and s.hitsMax and s.hitsMax > 0 and s.hits < s.hitsMax * 0.8 then
            hasDamaged = true
            break
        end
    end

    local targets = {
        harvester = 0,
        miner = 0,
        hauler = 0,
        upgrader = 1,
        builder = 0,
        defender = defense.desiredDefenders(threatLevel, hostiles),
        assault = 0,
        healer = 0,
        scout = 0,
        remoteHarvester = 0,
        remoteHauler = 0,
    }

    if useStaticMiners then
        targets.miner = math.min(sourceCount, srcContainers)
        -- Keep 1 mobile harvester if any source lacks a container yet.
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
    if rcl >= 4 and energyCap >= 1300 then
        targets.upgrader = 2
    end

    if #sites >= 5 then
        targets.builder = 2
    elseif #sites > 0 or hasDamaged or rcl >= 2 then
        targets.builder = 1
    end

    -- Keep standing garrison defenders once RCL 2+ economy is online so raids are met immediately.
    if targets.defender == 0 and rcl >= 2 then
        if energyCap >= 500 then
            targets.defender = 2
        elseif energyCap >= 400 then
            targets.defender = 1
        end
    end

    -- Room intel scout: once RCL >= 2 and home has at least 1 harvester/miner + 1 hauler,
    -- spawn 1 scout if any adjacent connected room hasn't been scouted in the last 1500 ticks.
    local scoutTarget = nil
    if rcl >= 2 and counts and (counts.harvester + counts.miner) >= 1 and counts.hauler >= 1 then
        scoutTarget = findRoomNeedingScout(room.name)
        if scoutTarget then
            targets.scout = 1
        end
    end

    -- Remote mining: once RCL >= 2, energyCap >= 450, remoteRoom is discovered, and home is safe.
    if rcl >= 2 and energyCap >= 450 and threatLevel == 0 and rmem and rmem.remoteRoom then
        local rIntel = Memory and Memory.roomIntel and Memory.roomIntel[rmem.remoteRoom]
        local remoteSources = (rIntel and rIntel.sources) or 1
        targets.remoteHarvester = 1
        if energyCap >= 550 and remoteSources >= 2 then
            targets.remoteHarvester = 2
        end
        targets.remoteHauler = 1
        if targets.remoteHarvester >= 2 then
            targets.remoteHauler = 2
        end
    end

    -- Proactive counter-assault waves against hostile-owned adjacent rooms once RCL >= 3 and economy is healthy!
    if rmem then
        local myOwner = (room.controller and room.controller.owner and room.controller.owner.username) or nil
        local adj = getAdjacentRoomNames(room.name)
        local enemyRoom = nil
        local intelMap = (Memory and Memory.roomIntel) or {}
        for _, rName in ipairs(adj) do
            local info = intelMap[rName]
            if info and info.owner and info.owner ~= "" and info.owner ~= myOwner then
                enemyRoom = rName
                break
            end
        end
        rmem.attackTarget = enemyRoom
    end
    if rcl >= 3 and energyCap >= 550 and threatLevel == 0 and rmem and rmem.attackTarget then
        targets.assault = 2
        targets.healer = (energyCap >= 800) and 2 or 1
    end

    return targets, scoutTarget
end

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

-- Manage creep spawning for a room with bootstrap, defense, and remote mining prioritization.
local function manageSpawning(room, counts, rcl, threatLevel, hostiles)
    local rmem = ensureRoomMemory(room)
    updateRemoteRoomSelection(room, rmem)

    local energyAvail = room.energyAvailable or 0
    local energyCap = room.energyCapacityAvailable or energyAvail
    if energyCap < energyAvail then
        energyCap = energyAvail
    end

    local targets, scoutTarget = getTargetRoster(room, rcl, energyCap, threatLevel, hostiles, counts, rmem)
    if rmem then
        rmem.desiredRoles = {
            harvester = targets.harvester,
            miner = targets.miner,
            hauler = targets.hauler,
            upgrader = targets.upgrader,
            builder = targets.builder,
            defender = targets.defender,
            assault = targets.assault,
            healer = targets.healer,
            scout = targets.scout,
            remoteHarvester = targets.remoteHarvester,
            remoteHauler = targets.remoteHauler,
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

    -- 1. Emergency economy bootstrap: never let the colony stall without energy income.
    if (counts.harvester + counts.miner) == 0 then
        roleToSpawn = "harvester"
        budget = energyAvail
    -- 2. Emergency hauler recovery when miners are running but no haulers exist.
    elseif counts.miner > 0 and counts.hauler == 0 then
        roleToSpawn = "hauler"
        budget = energyAvail
    -- 3. Active defense priority when hostiles are present.
    elseif threatLevel > 0 and counts.defender < targets.defender then
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
    -- 4. Standard home economy roster.
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
    -- 5. Standing garrison defender once core home economy is staffed.
    elseif counts.defender < targets.defender then
        roleToSpawn = "defender"
    -- 6. Scouting adjacent rooms for intel (lower priority than home economy + garrison).
    elseif counts.scout < targets.scout then
        roleToSpawn = "scout"
    -- 7. Remote mining operations (lower priority so home never starves).
    elseif counts.remoteHarvester < targets.remoteHarvester then
        roleToSpawn = "remoteHarvester"
    elseif counts.remoteHauler < targets.remoteHauler then
        roleToSpawn = "remoteHauler"
    -- 8. Coordinated counter-assault wave (healers paired with heavy bruisers).
    elseif counts.healer < targets.healer then
        roleToSpawn = "healer"
    elseif counts.assault < targets.assault then
        roleToSpawn = "assault"
    end

    if not roleToSpawn then
        if rmem then
            rmem.nextSpawn = "idle (economy full)"
        end
        return
    end

    local body
    if roleToSpawn == "miner" then
        body = buildMinerBody(budget)
    elseif roleToSpawn == "hauler" then
        body = buildHaulerBody(budget)
    elseif roleToSpawn == "defender" then
        body = defense.buildDefenderBody(budget)
    elseif roleToSpawn == "assault" then
        body = defense.buildAssaultBody(budget)
    elseif roleToSpawn == "healer" then
        body = defense.buildHealerBody(budget)
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

    local creepMem = {
        role = roleToSpawn,
        room = room.name,
        working = false,
    }
    if roleToSpawn == "scout" and scoutTarget then
        creepMem.targetRoom = scoutTarget
    elseif (roleToSpawn == "remoteHarvester" or roleToSpawn == "remoteHauler") and rmem and rmem.remoteRoom then
        creepMem.targetRoom = rmem.remoteRoom
    elseif (roleToSpawn == "assault" or roleToSpawn == "healer") and rmem and rmem.attackTarget then
        creepMem.targetRoom = rmem.attackTarget
    end

    local name = roleToSpawn .. "_" .. tostring(Game.time or 0)
    local res = spawn:spawnCreep(body, name, {
        memory = creepMem,
    })
    if rmem then
        if res == OK then
            rmem.nextSpawn = "spawning " .. roleToSpawn
        else
            rmem.nextSpawn = "waiting to spawn " .. roleToSpawn
        end
    end
end

-- Execute automated link energy transfers (source links -> controller link / hub link).
local function runLinks(room)
    local myStructs = room:find(FIND_MY_STRUCTURES)
    if not myStructs or #myStructs == 0 then
        return
    end

    local links = {}
    local storage = nil
    for _, s in ipairs(myStructs) do
        if s.structureType == STRUCT_LINK then
            table.insert(links, s)
        elseif s.structureType == STRUCT_STORAGE then
            storage = s
        end
    end

    if #links < 2 then
        return
    end

    local ctrl = room.controller
    local sources = room:find(FIND_SOURCES) or {}

    local ctrlLink = nil
    local hubLink = nil
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

    local function tryTransfer(sender, receiver)
        if not sender or not receiver or sender.id == receiver.id then
            return false
        end
        if not sender.transferEnergy then
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

    -- Source links feed controller link first, then hub/storage link.
    for _, sLink in ipairs(sourceLinks) do
        if not tryTransfer(sLink, ctrlLink) then
            tryTransfer(sLink, hubLink)
        end
    end

    -- Hub link tops up controller link if hub has surplus.
    if hubLink and ctrlLink then
        tryTransfer(hubLink, ctrlLink)
    end
end

-- Resolve exit direction from `room` toward `targetRoom` for visual overlays.
local function getExitDirForVisual(room, targetRoom)
    if not room or not targetRoom or room.name == targetRoom then
        return nil
    end
    if room.findExitTo then
        local dir = room:findExitTo(targetRoom)
        if type(dir) == "number" and dir > 0 then
            return dir
        end
    end
    if Game.map and Game.map.describeExits then
        local exits = Game.map.describeExits(room.name)
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

-- Draw clean RoomVisual debug overlays (rings, threat alerts, remote line, telemetry dots, optional HUD text).
local function drawRoomVisuals(room, counts, rcl, threatLevel)
    local vis = room.visual
    if not vis then
        return
    end

    planner.drawPlanVisuals(room)

    local spawn = planner.getPrimarySpawn(room)
    if spawn and spawn.pos then
        local spawnColor = "#66bb6a"
        if threatLevel > 0 then
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

    -- Subtle cyan dashed/thin visual line toward active remote mining room exit.
    local rmem = Memory and Memory.rooms and Memory.rooms[room.name]
    local remoteRoom = rmem and rmem.remoteRoom
    if remoteRoom and spawn and spawn.pos then
        local exitDir = getExitDirForVisual(room, remoteRoom)
        if exitDir then
            local exitTiles = room:find(exitDir) or {}
            if #exitTiles > 0 then
                local mid = exitTiles[math.max(1, math.floor((#exitTiles + 1) / 2))]
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

    -- Top-left RCL dots (purple) and threat indicator (red).
    local dotX = 2
    for _ = 1, math.min(rcl, 8) do
        vis:circle(dotX, 1.5, {
            radius = 0.24,
            fill = "#7e57c2",
            stroke = "#ffffff",
            opacity = 0.85,
        })
        dotX = dotX + 0.68
    end

    if threatLevel > 0 then
        vis:circle(dotX + 0.5, 1.5, {
            radius = 0.3,
            fill = "#ef5350",
            stroke = "#ffffff",
            opacity = 0.9,
        })
    end

    -- Creep role count dots.
    local roleColors = {
        { count = counts.miner, color = "#ffa726" },
        { count = counts.harvester, color = "#ffeb3b" },
        { count = counts.hauler, color = "#26c6da" },
        { count = counts.upgrader, color = "#7e57c2" },
        { count = counts.builder, color = "#66bb6a" },
        { count = counts.defender, color = "#ef5350" },
        { count = counts.scout or 0, color = "#80deea" },
        { count = counts.remoteHarvester or 0, color = "#00e5ff" },
        { count = counts.remoteHauler or 0, color = "#00b8d4" },
    }
    local creepX = 2
    for _, entry in ipairs(roleColors) do
        for _ = 1, math.min(entry.count, 10) do
            vis:circle(creepX, 2.5, {
                radius = 0.21,
                fill = entry.color,
                stroke = "#222222",
                opacity = 0.85,
            })
            creepX = creepX + 0.62
        end
    end

    if vis.text then
        local remTag = ""
        if remoteRoom then
            remTag = string.format(
                " | REM:%s(%d/%d)",
                tostring(remoteRoom),
                counts.remoteHarvester or 0,
                counts.remoteHauler or 0
            )
        end
        local hud = string.format(
            "RCL %d | E:%d/%d | M:%d H:%d Ha:%d U:%d B:%d D:%d%s%s",
            rcl,
            room.energyAvailable or 0,
            room.energyCapacityAvailable or 0,
            counts.miner,
            counts.harvester,
            counts.hauler,
            counts.upgrader,
            counts.builder,
            counts.defender,
            remTag,
            (threatLevel > 0 and (" | THREAT:" .. tostring(threatLevel)) or "")
        )
        vis:text(hud, 2, 3.8, { color = "#ffffff", opacity = 0.85 })
    end
end

-- Adopt any owned creep missing creep.memory.role (e.g. migrated from another bot).
local function adoptUnassignedCreeps()
    if not Game.creeps then
        return
    end
    for _, creep in pairs(Game.creeps) do
        if creep.memory and creep.memory.role == nil then
            local attack = 0
            local ranged = 0
            local work = 0
            local carry = 0
            local move = 0
            if creep.body then
                for _, part in ipairs(creep.body) do
                    local ptype = (type(part) == "table" and part.type) or part
                    if ptype == ATTACK or ptype == "attack" then
                        attack = attack + 1
                    elseif ptype == RANGED_ATTACK or ptype == "ranged_attack" then
                        ranged = ranged + 1
                    elseif ptype == WORK or ptype == "work" then
                        work = work + 1
                    elseif ptype == CARRY or ptype == "carry" then
                        carry = carry + 1
                    elseif ptype == MOVE or ptype == "move" then
                        move = move + 1
                    end
                end
            end

            local role
            if attack > 0 or ranged > 0 then
                role = "defender"
            elseif work == 0 and carry > 0 then
                role = "hauler"
            elseif work >= 3 and move <= 2 then
                role = "miner"
            elseif creep.name and string.sub(creep.name, 1, 2) == "B_" then
                role = "builder"
            else
                role = "harvester"
            end

            creep.memory.role = role
            if creep.memory.room == nil and creep.room then
                creep.memory.room = creep.room.name
            end
            if creep.memory.working == nil then
                creep.memory.working = false
            end
        end
    end
end

-- Main tick entry point invoked every tick by the Screeps Lua engine.
function loop()
    cleanupMemory()
    adoptUnassignedCreeps()

    if Game.rooms then
        for _, room in pairs(Game.rooms) do
            roles.recordRoomIntel(room)

            local isMyRoom = (room.controller and room.controller.my)
                or (#(room:find(FIND_MY_SPAWNS) or {}) > 0)
            if isMyRoom then
                local rcl = (room.controller and room.controller.level) or 0
                local counts = countRoles(room)
                local threatLevel, hostiles = defense.assessThreat(room)

                defense.checkSafeMode(room, threatLevel, hostiles, counts.defender)
                defense.runTowers(room, hostiles)
                runLinks(room)
                manageSpawning(room, counts, rcl, threatLevel, hostiles)

                local rmem = ensureRoomMemory(room)
                if (rmem and not rmem.plan) or (Game.time and (Game.time % PLANNER_INTERVAL == 0)) then
                    planner.planRoom(room, rcl)
                end

                drawRoomVisuals(room, counts, rcl, threatLevel)
            end
        end
    end

    if Game.creeps then
        for _, creep in pairs(Game.creeps) do
            roles.run(creep)
        end
    end
end
