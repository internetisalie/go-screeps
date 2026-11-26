-- defense.lua: Threat tracking, coordinated tower defense, and defender body builder.
-- Compatible with standard Lua 5.1 / gopher-lua.

local M = {}

local STRUCT_RAMPART = STRUCTURE_RAMPART or "rampart"
local STRUCT_WALL = STRUCTURE_WALL or "constructedWall"

local function getUsedEnergy(store)
    if not store then
        return 0
    end
    return store:getUsedCapacity(RESOURCE_ENERGY) or 0
end

local function ensureRoomMemory(roomName)
    if not Memory then
        return nil
    end
    if not Memory.rooms then
        Memory.rooms = {}
    end
    if not Memory.rooms[roomName] then
        Memory.rooms[roomName] = {}
    end
    return Memory.rooms[roomName]
end

local PART_STRENGTH = {
    attack = 30,
    ranged_attack = 10,
    heal = 12,
    work = 50,
    claim = 15,
}

-- Compute Hivemind-style part-weighted combat strength for a hostile creep.
local function scoreHostileCreep(creep)
    if not creep or not creep.body then
        return 0
    end
    local score = 0
    for _, part in ipairs(creep.body) do
        local ptype = part
        local active = true
        if type(part) == "table" then
            ptype = part.type
            if part.hits ~= nil and part.hits <= 0 then
                active = false
            end
        end
        if active and ptype then
            if ptype == ATTACK or ptype == "attack" then
                score = score + PART_STRENGTH.attack
            elseif ptype == RANGED_ATTACK or ptype == "ranged_attack" then
                score = score + PART_STRENGTH.ranged_attack
            elseif ptype == HEAL or ptype == "heal" then
                score = score + PART_STRENGTH.heal
            elseif ptype == WORK or ptype == "work" then
                score = score + PART_STRENGTH.work
            elseif ptype == CLAIM or ptype == "claim" then
                score = score + PART_STRENGTH.claim
            end
        end
    end
    return score
end

-- Assess hostile presence and record threat level & part-weighted strength in Memory.rooms[room.name].
-- Returns: threatLevel (0=none, 1=weak <=60, 2=normal <=200, 3=strong >200), hostiles array.
function M.assessThreat(room)
    local hostiles = room:find(FIND_HOSTILE_CREEPS) or {}
    local count = #hostiles
    local enemyStrength = 0
    local level = 0

    if count > 0 then
        for _, c in ipairs(hostiles) do
            enemyStrength = enemyStrength + scoreHostileCreep(c)
        end

        if enemyStrength > 200 then
            level = 3
        elseif enemyStrength > 60 then
            level = 2
        else
            level = 1
        end
    end

    local rmem = room.name and ensureRoomMemory(room.name)
    if rmem then
        local prevLastSeen = (rmem.threat and rmem.threat.lastSeen) or 0
        rmem.threat = {
            level = level,
            count = count,
            strength = enemyStrength,
            lastSeen = (count > 0 and (Game.time or 0)) or prevLastSeen,
        }
    end

    return level, hostiles
end

-- Determine desired number of defender creeps for the room based on threat level and hostile count (up to 4).
function M.desiredDefenders(threatLevel, hostiles)
    if threatLevel == 0 or not hostiles or #hostiles == 0 then
        return 0
    end
    local count = #hostiles
    if threatLevel >= 3 then
        return math.min(4, math.max(3, count))
    elseif threatLevel == 2 then
        return math.min(3, math.max(2, count))
    end
    return math.min(2, math.max(1, count))
end

-- Build a combat defender body scaled to available energy budget.
-- Uses TOUGH (10), ATTACK (80), RANGED_ATTACK (150), MOVE (50).
function M.buildDefenderBody(budget)
    if budget < 130 then
        return nil
    end
    if budget < 190 then
        return { ATTACK, MOVE }
    end

    local body = {}
    local cost = 0

    local function addParts(parts, partCost)
        if cost + partCost <= budget and (#body + #parts) <= 16 then
            for _, p in ipairs(parts) do
                table.insert(body, p)
            end
            cost = cost + partCost
            return true
        end
        return false
    end

    -- Lead with a high-DPS melee bruiser package scaled for early RCL 2-3 raid defense.
    if budget >= 510 then
        addParts({ TOUGH, TOUGH, ATTACK, ATTACK, ATTACK, MOVE, MOVE, MOVE, MOVE, MOVE }, 510)
    elseif budget >= 320 then
        addParts({ TOUGH, ATTACK, ATTACK, MOVE, MOVE, MOVE }, 320)
    else
        addParts({ TOUGH, ATTACK, MOVE, MOVE }, 190)
    end

    -- Scale up alternating melee and ranged packages.
    local addRanged = false
    while #body <= 12 do
        local added
        if addRanged then
            added = addParts({ RANGED_ATTACK, MOVE }, 200)
        else
            added = addParts({ TOUGH, ATTACK, MOVE, MOVE }, 190)
        end
        if not added then
            if not addParts({ ATTACK, MOVE }, 130) then
                break
            end
        end
        addRanged = not addRanged
    end

    -- Sort TOUGH parts to the front so they absorb damage first.
    local ordered = {}
    for _, p in ipairs(body) do
        if p == TOUGH then
            table.insert(ordered, p)
        end
    end
    for _, p in ipairs(body) do
        if p ~= TOUGH then
            table.insert(ordered, p)
        end
    end

    return ordered
end

-- Build a heavy offensive assault bruiser body (TOUGH + ATTACK + 1:1 MOVE, up to 24 parts).
function M.buildAssaultBody(budget)
    if budget < 320 then
        return nil
    end
    local body = {}
    local cost = 0
    local toughCount = math.min(4, math.floor(budget / 280))
    for _ = 1, toughCount do
        if cost + 60 <= budget then
            table.insert(body, TOUGH)
            cost = cost + 10
        end
    end
    local attackCount = 0
    while cost + 130 <= budget and (#body + attackCount + 2) <= 24 do
        table.insert(body, ATTACK)
        cost = cost + 80
        attackCount = attackCount + 1
        cost = cost + 50
    end
    cost = cost - (attackCount * 50)
    if attackCount == 0 then
        return { TOUGH, ATTACK, ATTACK, MOVE, MOVE }
    end
    local moveCount = math.min(24 - #body, math.max(attackCount + math.floor(toughCount / 2), math.floor((budget - cost) / 50)))
    for _ = 1, moveCount do
        if cost + 50 <= budget then
            table.insert(body, MOVE)
            cost = cost + 50
        end
    end
    return body
end

-- Build a combat healer body (HEAL + 1:1 MOVE, up to 20 parts) for assault squads.
function M.buildHealerBody(budget)
    if budget < 300 then
        return nil
    end
    local body = {}
    local cost = 0
    local healCount = 0
    while cost + 300 <= budget and (#body + healCount + 2) <= 20 do
        table.insert(body, HEAL)
        cost = cost + 250
        healCount = healCount + 1
        cost = cost + 50
    end
    cost = cost - (healCount * 50)
    if healCount == 0 then
        return { HEAL, MOVE }
    end
    local moveCount = math.min(20 - #body, math.max(healCount, math.floor((budget - cost) / 50)))
    for _ = 1, moveCount do
        if cost + 50 <= budget then
            table.insert(body, MOVE)
            cost = cost + 50
        end
    end
    return body
end

-- Pick a single focus-fire target (lowest HP, tie-broken by closeness to anchor).
local function selectFocusHostile(hostiles, anchorPos)
    local best = nil
    local bestScore = 999999999

    for _, h in ipairs(hostiles) do
        local hp = h.hits or 100
        local dist = 0
        if anchorPos and h.pos then
            dist = anchorPos:getRangeTo(h) or 0
        end
        -- Strongly prefer damaged/weakest hostile, with distance tie-breaker.
        local score = hp + dist * 25
        if score < bestScore then
            bestScore = score
            best = h
        end
    end

    return best
end

-- Select the most critical structure for tower repair.
local function selectTowerRepairTarget(room)
    local structures = room:find(FIND_STRUCTURES)
    if not structures or #structures == 0 then
        return nil
    end

    local criticalRampart = nil
    local lowestRampartHits = 15000
    local worstRegular = nil
    local worstRegularRatio = 0.8

    for _, s in ipairs(structures) do
        if s.hits and s.hitsMax and s.hitsMax > 0 then
            if s.structureType == STRUCT_RAMPART then
                -- Keep ramparts above emergency decay threshold.
                if s.hits < lowestRampartHits and s.hits < s.hitsMax then
                    lowestRampartHits = s.hits
                    criticalRampart = s
                end
            elseif s.structureType ~= STRUCT_WALL then
                local ratio = s.hits / s.hitsMax
                if ratio < worstRegularRatio then
                    worstRegularRatio = ratio
                    worstRegular = s
                end
            end
        end
    end

    if criticalRampart and criticalRampart.hits < 5000 then
        return criticalRampart
    end
    if worstRegular and worstRegular.hits < worstRegular.hitsMax * 0.5 then
        return worstRegular
    end
    if criticalRampart then
        return criticalRampart
    end
    return worstRegular
end

-- Run coordinated tower actions: focus-fire hostiles -> heal friendlies -> repair critical structures.
function M.runTowers(room, hostiles)
    local myStructures = room:find(FIND_MY_STRUCTURES)
    if not myStructures or #myStructures == 0 then
        return
    end

    local towers = {}
    for _, s in ipairs(myStructures) do
        if s.structureType == STRUCTURE_TOWER then
            table.insert(towers, s)
        end
    end
    if #towers == 0 then
        return
    end

    -- 1. Coordinated focus-fire on hostiles.
    if hostiles and #hostiles > 0 then
        local spawns = room:find(FIND_MY_SPAWNS)
        local anchor = (spawns and spawns[1] and spawns[1].pos) or towers[1].pos
        local primaryTarget = selectFocusHostile(hostiles, anchor)

        if primaryTarget then
            for _, tower in ipairs(towers) do
                tower:attack(primaryTarget)
                if room.visual and primaryTarget.pos then
                    room.visual:line(tower.pos.x, tower.pos.y, primaryTarget.pos.x, primaryTarget.pos.y, {
                        color = "#ef5350",
                        width = 0.16,
                        opacity = 0.85,
                    })
                end
            end
        end
        return
    end

    -- 2. Heal most injured friendly creep.
    local myCreeps = room:find(FIND_MY_CREEPS) or {}
    local mostInjured = nil
    local lowestRatio = 1.0
    for _, c in ipairs(myCreeps) do
        if c.hits and c.hitsMax and c.hitsMax > 0 and c.hits < c.hitsMax then
            local ratio = c.hits / c.hitsMax
            if ratio < lowestRatio then
                lowestRatio = ratio
                mostInjured = c
            end
        end
    end

    if mostInjured then
        for _, tower in ipairs(towers) do
            tower:heal(mostInjured)
            if room.visual and mostInjured.pos then
                room.visual:line(tower.pos.x, tower.pos.y, mostInjured.pos.x, mostInjured.pos.y, {
                    color = "#66bb6a",
                    width = 0.12,
                    opacity = 0.75,
                })
            end
        end
        return
    end

    -- 3. Repair critical ramparts, roads, containers, and structures (keeping energy reserve for defense).
    local repairTarget = selectTowerRepairTarget(room)
    if repairTarget then
        for _, tower in ipairs(towers) do
            if getUsedEnergy(tower.store) > 300 then
                tower:repair(repairTarget)
                if room.visual and repairTarget.pos then
                    room.visual:line(tower.pos.x, tower.pos.y, repairTarget.pos.x, repairTarget.pos.y, {
                        color = "#ffa726",
                        width = 0.1,
                        opacity = 0.5,
                    })
                end
            end
        end
    end
end

-- Activate Safe Mode (Hivemind RoomDefense trigger) if Spawn is damaged or threatened with no defenders.
function M.checkSafeMode(room, threatLevel, hostiles, defenderCount)
    if not room or threatLevel == 0 or not hostiles or #hostiles == 0 then
        return false
    end
    local ctrl = room.controller
    if not ctrl or not ctrl.my or not ctrl.activateSafeMode then
        return false
    end
    if ctrl.safeMode and ctrl.safeMode > 0 then
        return false
    end
    if ctrl.safeModeCooldown and ctrl.safeModeCooldown > 0 then
        return false
    end
    if (ctrl.safeModeAvailable or 0) <= 0 then
        return false
    end

    local spawns = room:find(FIND_MY_SPAWNS) or {}
    local spawn = spawns[1]
    if not spawn then
        return false
    end

    local needSafeMode = false
    if spawn.hits and spawn.hitsMax and spawn.hits < spawn.hitsMax then
        needSafeMode = true
    elseif (defenderCount or 0) == 0 and spawn.pos then
        for _, h in ipairs(hostiles) do
            if h.pos and spawn.pos:getRangeTo(h) <= 4 then
                needSafeMode = true
                break
            end
        end
    end

    if needSafeMode then
        return ctrl:activateSafeMode() == OK
    end
    return false
end

return M
