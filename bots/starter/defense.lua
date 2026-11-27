-- defense.lua: Object-oriented room threat tracking, tower focus-fire, Safe Mode, and combat body builders.
-- Idiomatic Lua 5.4 (arnodel/golua) with Lunar/LuaLS annotations.

local screeps <const> = require("screeps")
local OK <const> = screeps.OK
local FIND <const> = screeps.FIND
local PART <const> = screeps.PART
local RESOURCE <const> = screeps.RESOURCE
local STRUCTURE <const> = screeps.STRUCTURE

---@class ThreatState
---@field level integer 0=none, 1=weak (<=60), 2=normal (<=200), 3=strong (>200)
---@field count integer
---@field strength integer
---@field lastSeen integer

---@class DefenseManager
---@field room Room
---@field threatLevel integer
---@field enemyStrength integer
---@field hostiles Creep[]
local DefenseManager = {}
DefenseManager.__index = DefenseManager

---@type table<BodyPartType, integer>
local PART_STRENGTH <const> = {
    attack = 30,
    ranged_attack = 10,
    heal = 12,
    work = 50,
    claim = 15,
}

---@param store? Store
---@return integer
local function getUsedEnergy(store)
    if not store then
        return 0
    end
    return store:getUsedCapacity(RESOURCE.ENERGY) or 0
end

---@param roomName string
---@return table<string, any>|nil
local function ensureRoomMemory(roomName)
    if not screeps.Memory then
        return nil
    end
    if not screeps.Memory.rooms then
        screeps.Memory.rooms = {}
    end
    if not screeps.Memory.rooms[roomName] then
        screeps.Memory.rooms[roomName] = {}
    end
    return screeps.Memory.rooms[roomName]
end

---Compute Hivemind-style part-weighted combat strength for a hostile creep.
---@param creep Creep
---@return integer
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
            local w = PART_STRENGTH[ptype]
            if w then
                score = score + w
            end
        end
    end
    return score
end

---Pick a single focus-fire target (lowest HP, tie-broken by closeness to anchor).
---@param hostiles Creep[]
---@param anchorPos? RoomPosition
---@return Creep|nil
local function selectFocusHostile(hostiles, anchorPos)
    local best = nil
    local bestScore = math.maxinteger

    for _, h in ipairs(hostiles) do
        local hp = h.hits or 100
        local dist = 0
        if anchorPos and h.pos then
            dist = anchorPos:getRangeTo(h) or 0
        end
        local score = hp + dist * 25
        if score < bestScore then
            bestScore = score
            best = h
        end
    end

    return best
end

---Select the most critical structure for tower repair.
---@param room Room
---@return Structure|nil
local function selectTowerRepairTarget(room)
    local structures = room:find(FIND.STRUCTURES) ---@type Structure[]
    if not structures or #structures == 0 then
        return nil
    end

    local criticalRampart = nil
    local lowestRampartHits = 15000
    local worstRegular = nil
    local worstRegularRatio = 0.8

    for _, s in ipairs(structures) do
        if s.hits and s.hitsMax and s.hitsMax > 0 then
            if s.structureType == STRUCTURE.RAMPART then
                if s.hits < lowestRampartHits and s.hits < s.hitsMax then
                    lowestRampartHits = s.hits
                    criticalRampart = s
                end
            elseif s.structureType ~= STRUCTURE.WALL then
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

---Construct a DefenseManager bound to a specific owned Room.
---@param room Room
---@return DefenseManager
function DefenseManager.new(room)
    return setmetatable({
        room = room,
        threatLevel = 0,
        enemyStrength = 0,
        hostiles = {},
    }, DefenseManager)
end

---Assess hostile presence and record threat state in Memory.rooms[room.name].
---@return integer threatLevel
---@return Creep[] hostiles
function DefenseManager:assessThreat()
    local room = self.room
    local hostiles = room:find(FIND.HOSTILE_CREEPS) or {} ---@type Creep[]
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

    self.threatLevel = level
    self.enemyStrength = enemyStrength
    self.hostiles = hostiles

    local rmem = room.name and ensureRoomMemory(room.name)
    if rmem then
        local prevLastSeen = (rmem.threat and rmem.threat.lastSeen) or 0
        rmem.threat = {
            level = level,
            count = count,
            strength = enemyStrength,
            lastSeen = (count > 0 and (screeps.Game.time or 0)) or prevLastSeen,
        }
    end

    return level, hostiles
end

---Determine desired number of defender creeps for the room based on assessed threat (up to 4).
---@return integer
function DefenseManager:desiredDefenders()
    if self.threatLevel == 0 or #self.hostiles == 0 then
        return 0
    end
    local count = #self.hostiles
    if self.threatLevel >= 3 then
        return math.min(4, math.max(3, count))
    elseif self.threatLevel == 2 then
        return math.min(3, math.max(2, count))
    end
    return math.min(2, math.max(1, count))
end

---Run coordinated tower actions: focus-fire hostiles -> heal friendlies -> repair critical structures.
function DefenseManager:runTowers()
    local room = self.room
    local myStructures = room:find(FIND.MY_STRUCTURES) ---@type Structure[]
    if not myStructures or #myStructures == 0 then
        return
    end

    ---@type StructureTower[]
    local towers = {}
    for _, s in ipairs(myStructures) do
        if s.structureType == STRUCTURE.TOWER then
            table.insert(towers, s --[[@as StructureTower]])
        end
    end
    if #towers == 0 then
        return
    end

    -- 1. Coordinated focus-fire on hostiles.
    if #self.hostiles > 0 then
        local spawns = room:find(FIND.MY_SPAWNS) ---@type StructureSpawn[]
        local anchor = (spawns and spawns[1] and spawns[1].pos) or towers[1].pos
        local primaryTarget = selectFocusHostile(self.hostiles, anchor)

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
    local myCreeps = room:find(FIND.MY_CREEPS) or {} ---@type Creep[]
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

    -- 3. Repair critical structures while keeping an energy reserve for defense.
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

---Activate Safe Mode if the Spawn is damaged or threatened with no active defenders.
---@param defenderCount integer
---@return boolean
function DefenseManager:checkSafeMode(defenderCount)
    local room = self.room
    if not room or self.threatLevel == 0 or #self.hostiles == 0 then
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

    local spawns = room:find(FIND.MY_SPAWNS) or {} ---@type StructureSpawn[]
    local spawn = spawns[1]
    if not spawn then
        return false
    end

    local needSafeMode = false
    if spawn.hits and spawn.hitsMax and spawn.hits < spawn.hitsMax then
        needSafeMode = true
    elseif (defenderCount or 0) == 0 and spawn.pos then
        for _, h in ipairs(self.hostiles) do
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

---Build a combat defender body scaled to available energy budget.
---@param budget integer
---@return BodyPartType[]|nil
function DefenseManager.buildDefenderBody(budget)
    if budget < 130 then
        return nil
    end
    if budget < 190 then
        return { PART.ATTACK, PART.MOVE }
    end

    ---@type BodyPartType[]
    local body = {}
    local cost = 0

    ---@param parts BodyPartType[]
    ---@param partCost integer
    ---@return boolean
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

    if budget >= 510 then
        addParts({ PART.TOUGH, PART.TOUGH, PART.ATTACK, PART.ATTACK, PART.ATTACK, PART.MOVE, PART.MOVE, PART.MOVE, PART.MOVE, PART.MOVE }, 510)
    elseif budget >= 320 then
        addParts({ PART.TOUGH, PART.ATTACK, PART.ATTACK, PART.MOVE, PART.MOVE, PART.MOVE }, 320)
    else
        addParts({ PART.TOUGH, PART.ATTACK, PART.MOVE, PART.MOVE }, 190)
    end

    local addRanged = false
    while #body <= 12 do
        local added
        if addRanged then
            added = addParts({ PART.RANGED_ATTACK, PART.MOVE }, 200)
        else
            added = addParts({ PART.TOUGH, PART.ATTACK, PART.MOVE, PART.MOVE }, 190)
        end
        if not added then
            if not addParts({ PART.ATTACK, PART.MOVE }, 130) then
                break
            end
        end
        addRanged = not addRanged
    end

    ---@type BodyPartType[]
    local ordered = {}
    for _, p in ipairs(body) do
        if p == PART.TOUGH then
            table.insert(ordered, p)
        end
    end
    for _, p in ipairs(body) do
        if p ~= PART.TOUGH then
            table.insert(ordered, p)
        end
    end

    return ordered
end

---Build a heavy offensive assault bruiser body (TOUGH + ATTACK + 1:1 MOVE, up to 24 parts).
---@param budget integer
---@return BodyPartType[]|nil
function DefenseManager.buildAssaultBody(budget)
    if budget < 320 then
        return nil
    end
    ---@type BodyPartType[]
    local body = {}
    local cost = 0
    local toughCount = math.min(4, budget // 280)
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
        cost = cost + 50
    end
    cost = cost - (attackCount * 50)
    if attackCount == 0 then
        return { PART.TOUGH, PART.ATTACK, PART.ATTACK, PART.MOVE, PART.MOVE }
    end
    local moveCount = math.min(24 - #body, math.max(attackCount + (toughCount // 2), (budget - cost) // 50))
    for _ = 1, moveCount do
        if cost + 50 <= budget then
            table.insert(body, PART.MOVE)
            cost = cost + 50
        end
    end
    return body
end

---Build a combat healer body (HEAL + 1:1 MOVE, up to 20 parts) for assault squads.
---@param budget integer
---@return BodyPartType[]|nil
function DefenseManager.buildHealerBody(budget)
    if budget < 300 then
        return nil
    end
    ---@type BodyPartType[]
    local body = {}
    local cost = 0
    local healCount = 0
    while cost + 300 <= budget and (#body + healCount + 2) <= 20 do
        table.insert(body, PART.HEAL)
        cost = cost + 250
        healCount = healCount + 1
        cost = cost + 50
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

return DefenseManager
