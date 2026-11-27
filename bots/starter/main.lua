-- main.lua: Object-oriented Screeps autonomous colony AI entry point.
-- Modules: colony.lua, defense.lua, planner.lua, roles.lua.
-- Idiomatic Lua 5.4 (arnodel/golua) with Lunar/LuaLS annotations.

local Colony <const> = require("colony")
local roles <const> = require("roles")

---Remove stale Memory.creeps entries and initialize top-level Memory tables.
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

---Adopt any owned creep missing creep.memory.role (e.g. migrated from another bot).
local function adoptUnassignedCreeps()
    if not Game.creeps then
        return
    end
    for _, creep in pairs(Game.creeps) do
        if creep.memory and creep.memory.role == nil then
            local attack, ranged, heal, work, carry, move = 0, 0, 0, 0, 0, 0
            if creep.body then
                for _, part in ipairs(creep.body) do
                    local ptype = (type(part) == "table" and part.type) or part
                    if ptype == ATTACK then
                        attack = attack + 1
                    elseif ptype == RANGED_ATTACK then
                        ranged = ranged + 1
                    elseif ptype == HEAL then
                        heal = heal + 1
                    elseif ptype == WORK then
                        work = work + 1
                    elseif ptype == CARRY then
                        carry = carry + 1
                    elseif ptype == MOVE then
                        move = move + 1
                    end
                end
            end

            local role = "harvester"
            if heal > 0 then
                role = "healer"
            elseif attack > 0 or ranged > 0 then
                role = "defender"
            elseif work == 0 and carry > 0 then
                role = "hauler"
            elseif work >= 3 and move <= 2 then
                role = "miner"
            elseif creep.name and string.sub(creep.name, 1, 2) == "B_" then
                role = "builder"
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

---Main tick entry point invoked every tick by the Screeps Lua engine.
function loop()
    cleanupMemory()
    adoptUnassignedCreeps()

    if Game.rooms then
        for _, room in pairs(Game.rooms) do
            roles.recordRoomIntel(room)
            local isMyRoom = (room.controller and room.controller.my)
                or (#(room:find(FIND_MY_SPAWNS) or {}) > 0)
            if isMyRoom then
                Colony.new(room):run()
            end
        end
    end

    if Game.creeps then
        for _, creep in pairs(Game.creeps) do
            roles.run(creep)
        end
    end
end
