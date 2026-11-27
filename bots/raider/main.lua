-- main.lua: Object-oriented Aggressive Raider Screeps colony AI entry point.
-- Modules: colony.lua, economy.lua, planner.lua, combat.lua.
-- Idiomatic Lua 5.4 (arnodel/golua) with Lunar/LuaLS annotations.

local screeps <const> = require("screeps")
local FIND <const> = screeps.FIND
local PART <const> = screeps.PART

local RaiderColony <const> = require("colony")
local economy <const> = require("economy")
local combat <const> = require("combat")

---Remove stale Memory.creeps entries and initialize top-level Raider Memory tables.
---@param game Game
---@param memory Memory
local function cleanupMemory(game, memory)
    if not memory.creeps then
        memory.creeps = {}
    else
        for name, _ in pairs(memory.creeps) do
            if not game.creeps or not game.creeps[name] then
                memory.creeps[name] = nil
            end
        end
    end
    if not memory.rooms then
        memory.rooms = {}
    end
    if not memory.scoutedRooms then
        memory.scoutedRooms = {}
    end
end

---Adopt any owned creep missing creep.memory.role.
---@param game Game
local function adoptUnassignedCreeps(game)
    if not game.creeps then
        return
    end
    for _, creep in pairs(game.creeps) do
        if creep.memory and creep.memory.role == nil then
            local attack, ranged, heal, work, carry = 0, 0, 0, 0, 0
            if creep.body then
                for _, part in ipairs(creep.body) do
                    local pt = (type(part) == "table" and part.type) or part
                    if pt == PART.ATTACK then
                        attack = attack + 1
                    elseif pt == PART.RANGED_ATTACK then
                        ranged = ranged + 1
                    elseif pt == PART.HEAL then
                        heal = heal + 1
                    elseif pt == PART.WORK then
                        work = work + 1
                    elseif pt == PART.CARRY then
                        carry = carry + 1
                    end
                end
            end

            local role = "harvester"
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
            end

            creep.memory.role = role
            if creep.room then
                creep.memory.homeRoom = creep.memory.homeRoom or creep.room.name
            end
        end
    end
end

---Main tick entry point invoked every tick by the Screeps Lua engine.
---@param game Game
---@param memory Memory
local function loop(game, memory)
    cleanupMemory(game, memory)
    adoptUnassignedCreeps(game)

    local globalCounts = RaiderColony.countGlobalRoles()

    if game.rooms then
        for _, room in pairs(game.rooms) do
            combat.updateIntel(room)
            local isMyRoom = (room.controller and room.controller.my)
                or (#(room:find(FIND.MY_SPAWNS) or {}) > 0)
            if isMyRoom then
                RaiderColony.new(room, globalCounts):run()
            end
        end
    end

    if game.creeps then
        for _, creep in pairs(game.creeps) do
            local role = creep.memory and creep.memory.role
            if role == "harvester" or role == "hauler" or role == "upgrader" or role == "builder" then
                economy.run(creep)
            else
                combat.run(creep)
            end
        end
    end
end

return {
    loop = loop,
}
