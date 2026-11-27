-- main.lua: Object-oriented Aggressive Raider Screeps colony AI entry point.
-- Modules: colony.lua, economy.lua, planner.lua, combat.lua.

local screeps <const> = require("screeps")

local RaiderColony <const> = require("colony")
local EconomyAgent <const> = require("economy")
local CombatAgent <const> = require("combat")

---@class RaiderBot
---@field game Game
---@field memory Memory
local RaiderBot = {}
RaiderBot.__index = RaiderBot

---Construct a RaiderBot tick runner bound to Game and Memory.
---@param game Game
---@param memory Memory
---@return RaiderBot
function RaiderBot.new(game, memory)
    return setmetatable({
        game = game,
        memory = memory,
    }, RaiderBot)
end

---Remove stale Memory.creeps entries and initialize top-level Raider Memory tables.
function RaiderBot:cleanupMemory()
    local game = self.game
    local memory = self.memory
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
function RaiderBot:adoptUnassignedCreeps()
    local game = self.game
    if not game.creeps then
        return
    end
    for _, creep in pairs(game.creeps) do
        if creep.memory and creep.memory.role == nil then
            local attack, ranged, heal, work, carry = 0, 0, 0, 0, 0
            if creep.body then
                for _, part in ipairs(creep.body) do
                    local pt = (type(part) == "table" and part.type) or part
                    if pt == screeps.PART.ATTACK then
                        attack = attack + 1
                    elseif pt == screeps.PART.RANGED_ATTACK then
                        ranged = ranged + 1
                    elseif pt == screeps.PART.HEAL then
                        heal = heal + 1
                    elseif pt == screeps.PART.WORK then
                        work = work + 1
                    elseif pt == screeps.PART.CARRY then
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

---Execute one full tick of the Raider colony AI.
function RaiderBot:run()
    self:cleanupMemory()
    self:adoptUnassignedCreeps()

    local game = self.game
    local globalCounts = RaiderColony.countGlobalRoles()

    if game.rooms then
        for _, room in pairs(game.rooms) do
            local isMyRoom = (room.controller and room.controller.my)
                or (#(room:find(screeps.FIND.MY_SPAWNS) or {}) > 0)
            if isMyRoom then
                RaiderColony.new(room, globalCounts):run()
            else
                CombatAgent.updateIntel(room)
            end
        end
    end

    if game.creeps then
        for _, creep in pairs(game.creeps) do
            local role = creep.memory and creep.memory.role
            if role == "harvester" or role == "hauler" or role == "upgrader" or role == "builder" then
                EconomyAgent.run(creep)
            else
                CombatAgent.run(creep)
            end
        end
    end
end

return {
    ---Main tick entry point invoked every tick by the Screeps Lua engine.
    ---@param game Game
    ---@param memory Memory
    loop = function(game, memory)
        RaiderBot.new(game, memory):run()
    end,
}
