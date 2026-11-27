-- main.lua: Object-oriented Aggressive Raider Screeps colony AI entry point.
-- Modules: colony.lua, economy.lua, planner.lua, combat.lua.
-- Compatible with Lua 5.1 (gopher-lua) and Lunar/LuaLS annotations.

local RaiderColony = require("colony")
local economy = require("economy")
local combat = require("combat")

---Remove stale Memory.creeps entries and initialize top-level Raider Memory tables.
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

---Adopt any owned creep missing creep.memory.role.
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
function loop()
    cleanupMemory()
    adoptUnassignedCreeps()

    local globalCounts = RaiderColony.countGlobalRoles()

    if Game.rooms then
        for _, room in pairs(Game.rooms) do
            combat.updateIntel(room)
            local isMyRoom = (room.controller and room.controller.my)
                or (#(room:find(FIND_MY_SPAWNS) or {}) > 0)
            if isMyRoom then
                RaiderColony.new(room, globalCounts):run()
            end
        end
    end

    if Game.creeps then
        for _, creep in pairs(Game.creeps) do
            local role = creep.memory and creep.memory.role
            if role == "harvester" or role == "hauler" or role == "upgrader" or role == "builder" then
                economy.run(creep)
            else
                combat.run(creep)
            end
        end
    end
end
