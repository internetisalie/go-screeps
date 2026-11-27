-- combat.lua: Home defense, tower control, cross-room scouting, and synchronized raiding squads.
-- Roles: defender, scout, brawler, skirmisher, medic.
-- Idiomatic Lua 5.4 (arnodel/golua) with Lunar/LuaLS annotations.

local screeps <const> = require("screeps")
local OK <const> = screeps.OK
local ERR <const> = screeps.ERR
local TERRAIN <const> = screeps.TERRAIN
local FIND <const> = screeps.FIND
local PART <const> = screeps.PART
local RESOURCE <const> = screeps.RESOURCE
local STRUCTURE <const> = screeps.STRUCTURE

---@class RaiderCombat
local M = {}

---@type table<integer, FindConstant>
local EXIT_DIRS <const> = {
    [1] = FIND.EXIT_TOP,
    [3] = FIND.EXIT_RIGHT,
    [5] = FIND.EXIT_BOTTOM,
    [7] = FIND.EXIT_LEFT,
}

local function getUsedEnergy(store)
    if not store or not store.getUsedCapacity then
        return 0
    end
    return store:getUsedCapacity(RESOURCE.ENERGY) or 0
end

local function hasPart(creep, partType)
    if not creep or not creep.body then
        return false
    end
    for _, p in ipairs(creep.body) do
        local pt = (type(p) == "table" and p.type) or p
        local hits = (type(p) == "table" and p.hits) or 100
        if pt == partType and hits > 0 then
            return true
        end
    end
    return false
end

local function countPart(creep, partType)
    if not creep or not creep.body then
        return 0
    end
    local n = 0
    for _, p in ipairs(creep.body) do
        local pt = (type(p) == "table" and p.type) or p
        local hits = (type(p) == "table" and p.hits) or 100
        if pt == partType and hits > 0 then
            n = n + 1
        end
    end
    return n
end

local function isOnEdge(pos)
    if not pos then
        return false
    end
    return pos.x <= 0 or pos.x >= 49 or pos.y <= 0 or pos.y >= 49
end

-- Step 1 tile inward off room border to avoid bouncing back across exits.
local function stepOffEdge(creep)
    if not creep or not creep.pos then
        return false
    end
    local x, y = creep.pos.x, creep.pos.y
    if x <= 0 then
        creep:moveTo(2, math.max(2, math.min(47, y)))
        return true
    elseif x >= 49 then
        creep:moveTo(47, math.max(2, math.min(47, y)))
        return true
    elseif y <= 0 then
        creep:moveTo(math.max(2, math.min(47, x)), 2)
        return true
    elseif y >= 49 then
        creep:moveTo(math.max(2, math.min(47, x)), 47)
        return true
    end
    return false
end

-- Step away from a threat position (for kiting / retreating).
local function stepAwayFrom(creep, threatPos)
    if not creep or not creep.pos or not threatPos then
        return
    end
    local terrain = creep.room:getTerrain()
    local bestX, bestY = nil, nil
    local bestDist = creep.pos:getRangeTo(threatPos)
    for dx = -1, 1 do
        for dy = -1, 1 do
            if not (dx == 0 and dy == 0) then
                local nx = creep.pos.x + dx
                local ny = creep.pos.y + dy
                if nx >= 2 and nx <= 47 and ny >= 2 and ny <= 47 then
                    local walkable = true
                    if terrain and terrain.get then
                        walkable = (terrain:get(nx, ny) ~= (TERRAIN.WALL or 1))
                    end
                    if walkable then
                        local d = math.max(math.abs(nx - threatPos.x), math.abs(ny - threatPos.y))
                        if d > bestDist then
                            bestDist = d
                            bestX = nx
                            bestY = ny
                        end
                    end
                end
            end
        end
    end
    if bestX and bestY then
        creep:moveTo(bestX, bestY)
    else
        creep:moveTo(25, 25)
    end
end

-- --- Body Builders ---

function M.buildDefenderBody(budget)
    if budget < 190 then
        return nil
    end
    if budget >= 550 then
        return { PART.TOUGH, PART.TOUGH, PART.ATTACK, PART.ATTACK, PART.RANGED_ATTACK, PART.MOVE, PART.MOVE, PART.MOVE, PART.MOVE } -- 530
    elseif budget >= 300 then
        return { PART.TOUGH, PART.TOUGH, PART.ATTACK, PART.ATTACK, PART.MOVE, PART.MOVE } -- 280
    end
    return { PART.TOUGH, PART.ATTACK, PART.MOVE, PART.MOVE } -- 190
end

-- Frontline melee brawler: TOUGH + ATTACK + 1:1 MOVE for relentless pursuit.
function M.buildBrawlerBody(budget)
    if budget < 260 then
        return nil
    end
    if budget < 400 then
        if budget >= 280 then
            return { PART.TOUGH, PART.TOUGH, PART.ATTACK, PART.ATTACK, PART.MOVE, PART.MOVE } -- 280
        end
        return { PART.ATTACK, PART.ATTACK, PART.MOVE, PART.MOVE } -- 260
    end

    local body = {}
    local cost = 0
    -- Front-load TOUGH armor (up to 4).
    local toughCount = math.min(4, budget // 250)
    for _ = 1, toughCount do
        if cost + 60 <= budget then
            table.insert(body, PART.TOUGH)
            cost = cost + 10
        end
    end

    local attackCount = 0
    while cost + 130 <= budget and (#body + attackCount + 2) <= 24 do
        table.insert(body, PART.ATTACK)
        cost = cost + 80
        attackCount = attackCount + 1
        -- Reserve 50 for paired MOVE.
        cost = cost + 50
    end
    -- Reclaim reserved move budget and append MOVE parts.
    cost = cost - (attackCount * 50)
    local moveCount = math.min(24 - #body, math.max(attackCount, (budget - cost) // 50))
    for _ = 1, moveCount do
        if cost + 50 <= budget then
            table.insert(body, PART.MOVE)
            cost = cost + 50
        end
    end
    return body
end

-- Ranged skirmisher: RANGED_ATTACK + fast MOVE for kiting.
function M.buildSkirmisherBody(budget)
    if budget < 250 then
        return nil
    end
    if budget < 400 then
        if budget >= 260 then
            return { PART.TOUGH, PART.RANGED_ATTACK, PART.MOVE, PART.MOVE } -- 260
        end
        return { PART.RANGED_ATTACK, PART.MOVE, PART.MOVE } -- 250
    end

    local body = {}
    local cost = 0
    local rangedCount = 0
    while cost + 200 <= budget and (#body + rangedCount + 2) <= 22 do
        table.insert(body, PART.RANGED_ATTACK)
        cost = cost + 150
        rangedCount = rangedCount + 1
        cost = cost + 50 -- reserve paired MOVE
    end
    cost = cost - (rangedCount * 50)
    if cost + 60 <= budget and #body + rangedCount + 2 <= 22 then
        table.insert(body, 1, PART.TOUGH)
        cost = cost + 10
    end
    local moveCount = math.min(22 - #body, math.max(rangedCount, (budget - cost) // 50))
    for _ = 1, moveCount do
        if cost + 50 <= budget then
            table.insert(body, PART.MOVE)
            cost = cost + 50
        end
    end
    return body
end

-- Combat medic: HEAL + MOVE (scales up to 6+ HEAL parts at RCL 4+ to out-heal tower fire).
function M.buildMedicBody(budget)
    if budget < 300 then
        return nil
    end
    local body = {}
    local cost = 0
    local healCount = 0
    while cost + 300 <= budget and (#body + healCount + 2) <= 20 do
        table.insert(body, PART.HEAL)
        cost = cost + 250
        healCount = healCount + 1
        cost = cost + 50 -- reserve paired MOVE
    end
    cost = cost - (healCount * 50)
    if healCount == 0 then
        return { PART.HEAL, PART.MOVE }
    end
    local moveCount = math.min(20 - #body, math.max(healCount, (budget - cost) // 50))
    for _ = 1, moveCount do
        if cost + 50 <= budget then
            table.insert(body, PART.MOVE)
            cost = cost + 50
        end
    end
    return body
end

-- --- Threat Assessment & Home Towers ---

function M.assessThreat(room)
    local hostiles = room:find(FIND.HOSTILE_CREEPS) or {}
    local threatLevel = 0
    for _, h in ipairs(hostiles) do
        threatLevel = threatLevel + 1
        threatLevel = threatLevel + countPart(h, PART.ATTACK) * 2
        threatLevel = threatLevel + countPart(h, PART.RANGED_ATTACK) * 2
        threatLevel = threatLevel + countPart(h, PART.HEAL) * 3
    end
    if room.memory then
        room.memory.threat = {
            level = threatLevel,
            count = #hostiles,
            tick = screeps.Game.time or 0,
        }
    end
    return threatLevel, hostiles
end

function M.runTowers(room, hostiles)
    local myStructs = room:find(FIND.MY_STRUCTURES) or {}
    local towers = {}
    for _, s in ipairs(myStructs) do
        if s.structureType == STRUCTURE.TOWER and getUsedEnergy(s.store) >= 10 then
            table.insert(towers, s)
        end
    end
    if #towers == 0 then
        return
    end

    hostiles = hostiles or room:find(FIND.HOSTILE_CREEPS) or {}
    local myCreeps = room:find(FIND.MY_CREEPS) or {}
    local wounded = nil
    local lowestRatio = 1.0
    for _, c in ipairs(myCreeps) do
        if c.hits and c.hitsMax and c.hitsMax > 0 and c.hits < c.hitsMax then
            local ratio = c.hits / c.hitsMax
            if ratio < lowestRatio then
                lowestRatio = ratio
                wounded = c
            end
        end
    end

    for _, tower in ipairs(towers) do
        if #hostiles > 0 then
            -- Focus-fire closest hostile (preferring healers if close).
            local target = tower.pos:findClosestByRange(hostiles) or hostiles[1]
            if tower:attack(target) == OK and room.visual and tower.pos and target.pos then
                room.visual:line(tower.pos.x, tower.pos.y, target.pos.x, target.pos.y, {
                    color = "#ff5252",
                    width = 0.18,
                    opacity = 0.85,
                })
            end
        elseif wounded then
            if tower:heal(wounded) == OK and room.visual and tower.pos and wounded.pos then
                room.visual:line(tower.pos.x, tower.pos.y, wounded.pos.x, wounded.pos.y, {
                    color = "#69f0ae",
                    width = 0.15,
                    opacity = 0.75,
                })
            end
        elseif getUsedEnergy(tower.store) > 500 then
            -- Maintain critical ramparts and structures when energy is plentiful.
            local structs = room:find(FIND.STRUCTURES) or {}
            local repairTarget = nil
            local bestHits = 9999999
            for _, s in ipairs(structs) do
                if s.hits and s.hitsMax and s.hitsMax > 0 then
                    if s.structureType == STRUCTURE.RAMPART and s.hits < 3000 and s.hits < bestHits then
                        bestHits = s.hits
                        repairTarget = s
                    elseif (s.structureType == STRUCTURE.CONTAINER or s.structureType == STRUCTURE.ROAD) and s.hits < s.hitsMax * 0.5 and s.hits < bestHits then
                        bestHits = s.hits
                        repairTarget = s
                    end
                end
            end
            if repairTarget then
                tower:repair(repairTarget)
            end
        end
    end
end

-- --- Map & Cross-Room Helpers ---

-- Returns list of { dir = exitConst, roomName = name } for rooms connected to `room`.
function M.getAdjacentRooms(room)
    local result = {}
    if not room or not room.name then
        return result
    end

    if screeps.Game.map and screeps.Game.map.describeExits then
        local exits = screeps.Game.map.describeExits(room.name)
        if type(exits) == "table" then
            for k, adjName in pairs(exits) do
                local dirNum = tonumber(k)
                if dirNum and EXIT_DIRS[dirNum] and type(adjName) == "string" and adjName ~= "" then
                    table.insert(result, { dir = EXIT_DIRS[dirNum], roomName = adjName })
                end
            end
        end
        if #result > 0 then
            table.sort(result, function(a, b) return a.roomName < b.roomName end)
            return result
        end
    end

    -- Fallback when Game.map.describeExits is unavailable: check which exit directions have tiles.
    local orderedDirs = {
        FIND.EXIT_TOP or 1,
        FIND.EXIT_RIGHT or 3,
        FIND.EXIT_BOTTOM or 5,
        FIND.EXIT_LEFT or 7,
    }
    for _, dir in ipairs(orderedDirs) do
        local tiles = room:find(dir)
        if tiles and #tiles > 0 then
            table.insert(result, { dir = dir, roomName = nil })
        end
    end
    return result
end

-- Resolve exit direction from `room` toward `targetRoom`.
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

-- Pick a rally point 4 tiles inward from the midpoint of an exit.
local function getRallyPoint(room, exitDir)
    local tiles = room:find(exitDir) or {}
    if #tiles == 0 then
        return 25, 25
    end
    local mid = tiles[math.max(1, math.floor((#tiles + 1) / 2))]
    local rx, ry = mid.x, mid.y
    if rx <= 1 then
        rx = 5
    elseif rx >= 48 then
        rx = 44
    end
    if ry <= 1 then
        ry = 5
    elseif ry >= 48 then
        ry = 44
    end
    return rx, ry
end

-- Move creep across a room exit toward `targetRoom` (or fallback `exitDir`).
local function moveAcrossExit(creep, targetRoom, fallbackDir)
    local room = creep.room
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

-- Scan a non-home room and record hostile intel in Memory.scoutedRooms / Memory.raidTarget.
function M.updateIntel(room)
    if not room or not room.name then
        return
    end
    if not screeps.Memory.scoutedRooms then
        screeps.Memory.scoutedRooms = {}
    end

    local hostileSpawns = room:find(FIND.HOSTILE_SPAWNS) or {}
    local hostileCreeps = room:find(FIND.HOSTILE_CREEPS) or {}
    local hostileStructs = room:find(FIND.HOSTILE_STRUCTURES) or {}
    local ctrl = room.controller

    local hostileCtrl = false
    local ownerName = ""
    if ctrl and ctrl.owner and not ctrl.my then
        hostileCtrl = true
        ownerName = ctrl.owner.username or "enemy"
    end

    local score = (#hostileSpawns * 100) + (#hostileCreeps * 20) + (#hostileStructs * 10)
    if hostileCtrl then
        score = score + 50
    end

    screeps.Memory.scoutedRooms[room.name] = {
        lastSeen = screeps.Game.time or 0,
        score = score,
        hasSpawn = (#hostileSpawns > 0),
        hasHostiles = (score > 0),
        hostileCreeps = #hostileCreeps,
        owner = ownerName,
    }

    -- Select the highest-priority discovered enemy room as Memory.raidTarget.
    local bestRoom = nil
    local bestScore = 0
    for rName, info in pairs(screeps.Memory.scoutedRooms) do
        if info and info.hasHostiles and (info.score or 0) > bestScore then
            bestScore = info.score
            bestRoom = rName
        end
    end

    if bestRoom then
        screeps.Memory.raidTarget = bestRoom
    elseif screeps.Memory.raidTarget == room.name and score == 0 then
        screeps.Memory.raidTarget = nil
    end
end

-- Choose which adjacent room a scout should explore next.
function M.pickNextScoutTarget(homeRoom)
    if not homeRoom then
        return nil, nil
    end
    local adj = M.getAdjacentRooms(homeRoom)
    if #adj == 0 then
        return nil, nil
    end
    if not screeps.Memory.scoutedRooms then
        screeps.Memory.scoutedRooms = {}
    end

    local best = nil
    local oldestTick = (screeps.Game.time or 0) + 1
    for _, entry in ipairs(adj) do
        if entry.roomName then
            local info = screeps.Memory.scoutedRooms[entry.roomName]
            local seen = (info and info.lastSeen) or -9999
            if seen < oldestTick then
                oldestTick = seen
                best = entry
            end
        elseif not best then
            best = entry
        end
    end

    if best then
        return best.roomName, best.dir
    end
    return nil, nil
end

-- --- Combat Roles ---

-- Role: Home Defender.
local function runDefender(creep)
    local room = creep.room
    local homeRoom = creep.memory.homeRoom or room.name
    if room.name ~= homeRoom then
        moveAcrossExit(creep, homeRoom, nil)
        return
    end

    local hostiles = room:find(FIND.HOSTILE_CREEPS) or {}
    if #hostiles > 0 then
        local target = creep.pos:findClosestByRange(hostiles) or hostiles[1]
        local dist = creep.pos:getRangeTo(target)
        local hasMelee = hasPart(creep, PART.ATTACK)
        local hasRanged = hasPart(creep, PART.RANGED_ATTACK)

        if hasRanged and dist <= 3 then
            creep:rangedAttack(target)
        end
        if hasMelee then
            if creep:attack(target) == ERR.NOT_IN_RANGE then
                creep:moveTo(target)
            end
        elseif hasRanged then
            if dist <= 2 then
                stepAwayFrom(creep, target.pos)
            elseif dist > 3 then
                creep:moveTo(target)
            end
        end
        return
    end

    -- Patrol near raid exit or spawn when peaceful.
    if screeps.Memory.raidTarget then
        local exitDir = resolveExitDir(room, screeps.Memory.raidTarget)
        if exitDir then
            local rx, ry = getRallyPoint(room, exitDir)
            if creep.pos:getRangeTo(rx, ry) > 2 then
                creep:moveTo(rx, ry)
            end
            return
        end
    end

    local spawns = room:find(FIND.MY_SPAWNS) or {}
    if #spawns > 0 and creep.pos:getRangeTo(spawns[1]) > 4 then
        creep:moveTo(spawns[1], { range = 3 })
    end
end

-- Role: Fast Cheap Scout ({ MOVE }).
local function runScout(creep)
    local room = creep.room
    local homeRoom = creep.memory.homeRoom or room.name
    creep.memory.homeRoom = homeRoom

    if room.name == homeRoom then
        -- Pick next room to scout.
        if not creep.memory.targetRoom and not creep.memory.targetDir then
            local tRoom, tDir = M.pickNextScoutTarget(room)
            creep.memory.targetRoom = tRoom
            creep.memory.targetDir = tDir
        end
        if creep.memory.targetRoom or creep.memory.targetDir then
            moveAcrossExit(creep, creep.memory.targetRoom, creep.memory.targetDir)
        end
        return
    end

    -- Inside a remote room: step off edge, update intel, avoid hostiles, then cycle back.
    M.updateIntel(room)

    if isOnEdge(creep.pos) then
        stepOffEdge(creep)
        return
    end

    local hostiles = room:find(FIND.HOSTILE_CREEPS) or {}
    if #hostiles > 0 then
        local nearest = creep.pos:findClosestByRange(hostiles)
        if nearest and creep.pos:getRangeTo(nearest) <= 5 then
            stepAwayFrom(creep, nearest.pos)
            return
        end
    end

    creep.memory.exploreTicks = (creep.memory.exploreTicks or 0) + 1
    if creep.memory.exploreTicks < 12 then
        if creep.pos:getRangeTo(25, 25) > 6 then
            creep:moveTo(25, 25)
        end
    else
        -- Done scouting this room; return home to pick next adjacent room.
        creep.memory.exploreTicks = 0
        creep.memory.targetRoom = nil
        creep.memory.targetDir = nil
        moveAcrossExit(creep, homeRoom, nil)
    end
end

-- Choose a shared squad focus-fire target in a raid room (computed once per tick so all raiders focus-fire!).
local function selectRaidTarget(creep)
    local room = creep.room
    local now = screeps.Game.time or 0

    -- Reuse shared squad focus target if still alive in this room on the same tick.
    if screeps.Memory.raidFocusTick == now and screeps.Memory.raidFocusRoom == room.name and screeps.Memory.raidFocusId then
        local cached = screeps.Game.getObjectById and screeps.Game.getObjectById(screeps.Memory.raidFocusId)
        if cached and cached.room and cached.room.name == room.name then
            return cached, screeps.Memory.raidFocusKind or "creep"
        end
    end

    local chosen, kind = nil, nil

    -- 1. Hostile combat creeps & healers first (kill enemy healers & defenders together!).
    local hostiles = room:find(FIND.HOSTILE_CREEPS) or {}
    if #hostiles > 0 then
        local best = nil
        local bestScore = math.mininteger
        for _, h in ipairs(hostiles) do
            local d = creep.pos:getRangeTo(h)
            local hpFrac = (h.hits and h.hitsMax and h.hitsMax > 0) and (h.hits / h.hitsMax) or 1
            local s = 100 - (d * 2) + (1 - hpFrac) * 45
            if hasPart(h, PART.HEAL) then
                s = s + 65
            elseif hasPart(h, PART.ATTACK) or hasPart(h, PART.RANGED_ATTACK) then
                s = s + 40
            end
            if s > bestScore then
                bestScore = s
                best = h
            end
        end
        if best then
            chosen, kind = best, "creep"
        end
    end

    -- 2. Hostile towers (disable room defense!), then spawns, then other structures.
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
            chosen, kind = creep.pos:findClosestByRange(towers) or towers[1], "structure"
        elseif #spawns > 0 then
            chosen, kind = creep.pos:findClosestByRange(spawns) or spawns[1], "structure"
        elseif #others > 0 then
            chosen, kind = creep.pos:findClosestByRange(others) or others[1], "structure"
        end
    end

    -- 3. Hostile construction sites (stomp them!).
    if not chosen and FIND.HOSTILE_CONSTRUCTION_SITES then
        local hSites = room:find(FIND.HOSTILE_CONSTRUCTION_SITES) or {}
        if #hSites > 0 then
            chosen, kind = creep.pos:findClosestByRange(hSites) or hSites[1], "site"
        end
    end

    if chosen and chosen.id then
        screeps.Memory.raidFocusTick = now
        screeps.Memory.raidFocusRoom = room.name
        screeps.Memory.raidFocusId = chosen.id
        screeps.Memory.raidFocusKind = kind
    end
    return chosen, kind
end

-- Count raiders currently rallying in `room` (and whether a medic is present).
local function inspectStagingSquad(room)
    local staged = {}
    local hasMedic = false
    local myCreeps = room:find(FIND.MY_CREEPS) or {}
    for _, c in ipairs(myCreeps) do
        if c.memory then
            local r = c.memory.role
            if (r == "brawler" or r == "skirmisher" or r == "medic") and c.memory.state ~= "raid" then
                table.insert(staged, c)
                if r == "medic" then
                    hasMedic = true
                end
            end
        end
    end
    return staged, hasMedic
end

-- Shared synchronized wave rally & cross-room transit logic for raiders (brawler, skirmisher, medic).
-- Prevents 1-by-1 trickling by holding newly spawned raiders at the exit until a full wave launches together!
local function handleRaidTransit(creep)
    local room = creep.room
    local homeRoom = creep.memory.homeRoom or room.name
    creep.memory.homeRoom = homeRoom

    local targetRoom = screeps.Memory.raidTarget or creep.memory.targetRoom
    if not targetRoom then
        if room.name ~= homeRoom then
            moveAcrossExit(creep, homeRoom, nil)
            return false
        end
        local hostiles = room:find(FIND.HOSTILE_CREEPS) or {}
        if #hostiles > 0 then
            return true
        end
        local spawns = room:find(FIND.MY_SPAWNS) or {}
        if #spawns > 0 and creep.pos:getRangeTo(spawns[1]) > 5 then
            creep:moveTo(spawns[1], { range = 4 })
        end
        return false
    end

    creep.memory.targetRoom = targetRoom

    -- If already inside the target room, step off edge and fight!
    if room.name == targetRoom then
        creep.memory.state = "raid"
        if isOnEdge(creep.pos) then
            stepOffEdge(creep)
            return false
        end
        return true
    end

    -- In home room: fight intruders immediately if home is under attack!
    if room.name == homeRoom then
        local homeHostiles = room:find(FIND.HOSTILE_CREEPS) or {}
        if #homeHostiles > 0 then
            return true
        end

        local exitDir = resolveExitDir(room, targetRoom)
        if not exitDir then
            return false
        end

        -- Synchronized wave staging: hold at rally point until full wave is ready, then launch ALL together!
        if creep.memory.state ~= "raid" then
            local rx, ry = getRallyPoint(room, exitDir)
            local staged, hasMedic = inspectStagingSquad(room)
            local energyCap = room.energyCapacityAvailable or 300
            local needMedic = (energyCap >= 550)
            local waveTarget = (energyCap >= 800) and 5 or ((energyCap >= 550) and 4 or 3)

            if screeps.Memory.raidGatherStart == nil or #staged == 0 then
                screeps.Memory.raidGatherStart = screeps.Game.time or 0
            end
            local waited = (screeps.Game.time or 0) - (screeps.Memory.raidGatherStart or (screeps.Game.time or 0))

            if room.visual then
                room.visual:circle(rx, ry, {
                    radius = 0.85,
                    fill = "transparent",
                    stroke = "#ff5252",
                    opacity = 0.75,
                })
            end

            local waveReady = (#staged >= waveTarget and (not needMedic or hasMedic))
                or (waited >= 220 and #staged >= 2)
            if waveReady then
                for _, member in ipairs(staged) do
                    member.memory.state = "raid"
                end
                screeps.Memory.raidGatherStart = nil
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

    -- In an intermediate room: keep moving toward targetRoom.
    if isOnEdge(creep.pos) then
        stepOffEdge(creep)
        return false
    end
    moveAcrossExit(creep, targetRoom, nil)
    return false
end

-- Retreat/kite behavior when HP < 25%.
local function handleLowHP(creep)
    if not creep.hits or not creep.hitsMax or creep.hitsMax <= 0 then
        return false
    end
    if creep.hits >= creep.hitsMax * 0.25 then
        return false
    end

    local room = creep.room
    -- If a friendly medic is in the room, pull back toward the medic!
    local myCreeps = room:find(FIND.MY_CREEPS) or {}
    local medic = nil
    for _, c in ipairs(myCreeps) do
        if c.id ~= creep.id and c.memory and c.memory.role == "medic" then
            medic = c
            break
        end
    end

    -- Skirmishers still fire while retreating.
    if hasPart(creep, PART.RANGED_ATTACK) then
        local hostiles = room:find(FIND.HOSTILE_CREEPS) or {}
        if #hostiles > 0 then
            local nearest = creep.pos:findClosestByRange(hostiles)
            if nearest and creep.pos:getRangeTo(nearest) <= 3 then
                creep:rangedAttack(nearest)
            end
        end
    end

    if medic and medic.pos then
        creep:moveTo(medic)
        return true
    end

    local homeRoom = creep.memory.homeRoom
    if homeRoom and room.name ~= homeRoom then
        if moveAcrossExit(creep, homeRoom, nil) then
            return true
        end
    end

    local hostiles = room:find(FIND.HOSTILE_CREEPS) or {}
    if #hostiles > 0 then
        local nearest = creep.pos:findClosestByRange(hostiles)
        if nearest and nearest.pos then
            stepAwayFrom(creep, nearest.pos)
            return true
        end
    end

    return false
end

-- Role: Frontline Brawler (melee).
local function runBrawler(creep)
    if not handleRaidTransit(creep) then
        return
    end
    if handleLowHP(creep) then
        return
    end

    local target, kind = selectRaidTarget(creep)
    if not target then
        -- Room cleared! Patrol center.
        if creep.pos:getRangeTo(25, 25) > 5 then
            creep:moveTo(25, 25)
        end
        return
    end

    if kind == "site" then
        creep:moveTo(target, { range = 0 })
        return
    end

    if creep:attack(target) == ERR.NOT_IN_RANGE then
        creep:moveTo(target)
    end
end

-- Role: Ranged Skirmisher (kiter).
local function runSkirmisher(creep)
    if not handleRaidTransit(creep) then
        return
    end
    if handleLowHP(creep) then
        return
    end

    local target, kind = selectRaidTarget(creep)
    if not target then
        if creep.pos:getRangeTo(25, 25) > 5 then
            creep:moveTo(25, 25)
        end
        return
    end

    if kind == "site" then
        creep:moveTo(target, { range = 0 })
        return
    end

    local dist = creep.pos:getRangeTo(target)
    if dist <= 3 then
        creep:rangedAttack(target)
    end

    -- Kite melee hostiles at range 3!
    if kind == "creep" and hasPart(target, PART.ATTACK) and dist <= 2 then
        stepAwayFrom(creep, target.pos)
    elseif dist > 3 then
        creep:moveTo(target)
    elseif kind == "structure" and dist > 2 then
        creep:moveTo(target)
    end
end

-- Role: Squad Medic (healer).
local function runMedic(creep)
    -- Self-heal if damaged.
    if creep.hits and creep.hitsMax and creep.hits < creep.hitsMax then
        creep:heal(creep)
    end

    if not handleRaidTransit(creep) then
        return
    end

    local room = creep.room
    local myCreeps = room:find(FIND.MY_CREEPS) or {}

    -- Find most wounded friendly creep in the room.
    local wounded = nil
    local lowestRatio = 1.0
    local fighters = {}
    for _, ally in ipairs(myCreeps) do
        if ally.id ~= creep.id then
            if ally.hits and ally.hitsMax and ally.hitsMax > 0 and ally.hits < ally.hitsMax then
                local ratio = ally.hits / ally.hitsMax
                if ratio < lowestRatio then
                    lowestRatio = ratio
                    wounded = ally
                end
            end
            if ally.memory and (ally.memory.role == "brawler" or ally.memory.role == "skirmisher") then
                table.insert(fighters, ally)
            end
        end
    end

    if wounded then
        local dist = creep.pos:getRangeTo(wounded)
        if dist <= 1 then
            creep:heal(wounded)
        elseif dist <= 3 then
            creep:rangedHeal(wounded)
            creep:moveTo(wounded)
        else
            creep:moveTo(wounded)
        end
        return
    end

    -- Follow closest fighter at range 2 so we stay safe and ready to heal.
    if #fighters > 0 then
        local lead = creep.pos:findClosestByRange(fighters) or fighters[1]
        if creep.pos:getRangeTo(lead) > 2 then
            creep:moveTo(lead)
        end
        return
    end

    -- Stay clear of hostiles if solo.
    local hostiles = room:find(FIND.HOSTILE_CREEPS) or {}
    if #hostiles > 0 then
        local nearest = creep.pos:findClosestByRange(hostiles)
        if nearest and creep.pos:getRangeTo(nearest) <= 4 then
            stepAwayFrom(creep, nearest.pos)
            return
        end
    end

    if creep.pos:getRangeTo(25, 25) > 5 then
        creep:moveTo(25, 25)
    end
end

function M.run(creep)
    if not creep or creep.spawning or not creep.memory then
        return
    end
    local role = creep.memory.role
    if role == "defender" then
        runDefender(creep)
    elseif role == "scout" then
        runScout(creep)
    elseif role == "brawler" then
        runBrawler(creep)
    elseif role == "skirmisher" then
        runSkirmisher(creep)
    elseif role == "medic" then
        runMedic(creep)
    end
end

return M
