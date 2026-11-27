-- main.lua: Object-oriented Screeps autonomous colony AI entry point.
-- Modules: colony.lua, defense.lua, planner.lua, roles.lua.

local screeps <const> = require("screeps")

local Colony <const> = require("colony")
local CreepAgent <const> = require("roles")

---@class Hivemind
---@field game Game
---@field memory Memory
local Hivemind = {}
Hivemind.__index = Hivemind

---Construct a Hivemind tick coordinator bound to Game and Memory.
---@param game Game
---@param memory Memory
---@return Hivemind
function Hivemind.new(game, memory)
    return setmetatable({
        game = game,
        memory = memory,
    }, Hivemind)
end

---Remove stale Memory.creeps entries and initialize top-level Memory tables.
function Hivemind:cleanupMemory()
    local memory = self.memory
    if not memory.creeps then
        memory.creeps = {}
    else
        for name, _ in pairs(memory.creeps) do
            if not self.game.creeps[name] then
                memory.creeps[name] = nil
            end
        end
    end
    if not memory.rooms then
        memory.rooms = {}
    end
    if not memory.roomIntel then
        memory.roomIntel = {}
    end
end

---Adopt any owned creep missing creep.memory.role (e.g. migrated from another bot).
function Hivemind:adoptUnassignedCreeps()
    if not self.game.creeps then
        return
    end
    for _, creep in pairs(self.game.creeps) do
        if creep.memory and creep.memory.role == nil then
            local attack, ranged, heal, work, carry, move = 0, 0, 0, 0, 0, 0
            if creep.body then
                for _, part in ipairs(creep.body) do
                    local ptype = (type(part) == "table" and part.type) or part
                    if ptype == screeps.PART.ATTACK then
                        attack = attack + 1
                    elseif ptype == screeps.PART.RANGED_ATTACK then
                        ranged = ranged + 1
                    elseif ptype == screeps.PART.HEAL then
                        heal = heal + 1
                    elseif ptype == screeps.PART.WORK then
                        work = work + 1
                    elseif ptype == screeps.PART.CARRY then
                        carry = carry + 1
                    elseif ptype == screeps.PART.MOVE then
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

---Execute one full tick across all owned colonies and creeps.
function Hivemind:run()
    self:cleanupMemory()
    self:adoptUnassignedCreeps()

    if self.game.rooms then
        for _, room in pairs(self.game.rooms) do
            CreepAgent.recordRoomIntel(room)
            local isMyRoom = (room.controller and room.controller.my)
                or (#(room:find(screeps.FIND.MY_SPAWNS) or {}) > 0)
            if isMyRoom then
                Colony.new(room):run()
            end
        end
    end

    if self.game.creeps then
        for _, creep in pairs(self.game.creeps) do
            CreepAgent.run(creep)
        end
    end
end

---Main tick entry point invoked every tick by the Screeps Lua engine.
---@param game Game
---@param memory Memory
function Hivemind.loop(game, memory)
    Hivemind.new(game, memory):run()
end

return {
    loop = Hivemind.loop,
}
