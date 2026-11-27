package engine

// Screeps return/error codes.
const (
	OK                        = 0
	ERR_NOT_OWNER             = -1
	ERR_NO_PATH               = -2
	ERR_NAME_EXISTS           = -3
	ERR_BUSY                  = -4
	ERR_NOT_FOUND             = -5
	ERR_NOT_ENOUGH_ENERGY     = -6
	ERR_NOT_ENOUGH_RESOURCES  = -6
	ERR_NOT_ENOUGH_EXTENSIONS = -6
	ERR_INVALID_TARGET        = -7
	ERR_FULL                  = -8
	ERR_NOT_IN_RANGE          = -9
	ERR_INVALID_ARGS          = -10
	ERR_TIRED                 = -11
	ERR_NO_BODYPART           = -12
	ERR_RCL_NOT_ENOUGH        = -14
	ERR_GCL_NOT_ENOUGH        = -15
)

// Room dimensions and terrain bitmasks.
const (
	RoomSize           = 50
	TERRAIN_MASK_PLAIN = 0
	TERRAIN_MASK_WALL  = 1
	TERRAIN_MASK_SWAMP = 2
)

// Movement directions (1..8, clockwise starting from TOP).
const (
	TOP          = 1
	TOP_RIGHT    = 2
	RIGHT        = 3
	BOTTOM_RIGHT = 4
	BOTTOM       = 5
	BOTTOM_LEFT  = 6
	LEFT         = 7
	TOP_LEFT     = 8
)

// DirDX and DirDY map direction (1..8) to (dx, dy) tile offsets.
var (
	DirDX = [9]int{0, 0, 1, 1, 1, 0, -1, -1, -1}
	DirDY = [9]int{0, -1, -1, 0, 1, 1, 1, 0, -1}
)

// Body part constants.
const (
	MOVE          = "move"
	WORK          = "work"
	CARRY         = "carry"
	ATTACK        = "attack"
	RANGED_ATTACK = "ranged_attack"
	TOUGH         = "tough"
	HEAL          = "heal"
	CLAIM         = "claim"
)

// BODYPART_COST maps each body part type to its energy cost.
var BODYPART_COST = map[string]int{
	MOVE:          50,
	WORK:          100,
	CARRY:         50,
	ATTACK:        80,
	RANGED_ATTACK: 150,
	TOUGH:         10,
	HEAL:          250,
	CLAIM:         600,
}

// Creep action and lifecycle constants.
const (
	MAX_CREEP_SIZE           = 50
	CREEP_LIFE_TIME          = 1500
	CREEP_CLAIM_LIFE_TIME    = 600
	CREEP_SPAWN_TIME         = 3
	CARRY_CAPACITY           = 50
	HARVEST_POWER            = 2
	HARVEST_MINERAL_POWER    = 1
	REPAIR_POWER             = 100
	DISMANTLE_POWER          = 50
	BUILD_POWER              = 5
	ATTACK_POWER             = 30
	UPGRADE_CONTROLLER_POWER = 1
	RANGED_ATTACK_POWER      = 10
	HEAL_POWER               = 12
	RANGED_HEAL_POWER        = 4
	REPAIR_COST              = 0.01
	DISMANTLE_COST           = 0.005
)

// Resource constants.
const (
	RESOURCE_ENERGY    = "energy"
	RESOURCE_HYDROGEN  = "H"
	RESOURCE_OXYGEN    = "O"
	RESOURCE_UTRIUM    = "U"
	RESOURCE_LEMERGIUM = "L"
	RESOURCE_KEANIUM   = "K"
	RESOURCE_ZYNTHIUM  = "Z"
	RESOURCE_CATALYST  = "X"
)

// Structure type constants.
const (
	STRUCTURE_SPAWN      = "spawn"
	STRUCTURE_EXTENSION  = "extension"
	STRUCTURE_ROAD       = "road"
	STRUCTURE_WALL       = "constructedWall"
	STRUCTURE_RAMPART    = "rampart"
	STRUCTURE_LINK       = "link"
	STRUCTURE_STORAGE    = "storage"
	STRUCTURE_TOWER      = "tower"
	STRUCTURE_CONTAINER  = "container"
	STRUCTURE_CONTROLLER = "controller"
	STRUCTURE_EXTRACTOR  = "extractor"
)

// Non-structure entity type constants.
const (
	TYPE_CREEP             = "creep"
	TYPE_SOURCE            = "source"
	TYPE_MINERAL           = "mineral"
	TYPE_CONSTRUCTION_SITE = "constructionSite"
	TYPE_RESOURCE          = "resource"
	TYPE_TOMBSTONE         = "tombstone"
)

// Source and mineral constants.
const (
	SOURCE_ENERGY_CAPACITY         = 3000
	SOURCE_ENERGY_NEUTRAL_CAPACITY = 1500
	SOURCE_ENERGY_REGEN            = 300
	MINERAL_DENSITY_AMOUNT         = 70000
)

// Structure capacity, power, and decay constants.
const (
	SPAWN_ENERGY_START        = 300
	SPAWN_ENERGY_CAPACITY     = 300
	STORAGE_CAPACITY          = 1000000
	CONTAINER_CAPACITY        = 2000
	LINK_CAPACITY             = 800
	LINK_COOLDOWN             = 1
	LINK_LOSS_RATIO           = 0.03
	TOWER_CAPACITY            = 1000
	TOWER_ENERGY_COST         = 10
	TOWER_POWER_ATTACK        = 600
	TOWER_POWER_HEAL          = 400
	TOWER_POWER_REPAIR        = 800
	TOWER_OPTIMAL_RANGE       = 5
	TOWER_FALLOFF_RANGE       = 20
	TOWER_FALLOFF             = 0.75
	ROAD_DECAY_AMOUNT         = 100
	ROAD_DECAY_TIME           = 1000
	CONTAINER_DECAY           = 5000
	CONTAINER_DECAY_TIME      = 100
	CONTAINER_DECAY_TIME_OWN  = 500
	RAMPART_DECAY_AMOUNT      = 300
	RAMPART_DECAY_TIME        = 100
	TOMBSTONE_DECAY_PER_PART  = 5
	ENERGY_DECAY              = 1000
	CONSTRUCTION_SITE_MAX_NUM         = 100
	SAFE_MODE_DURATION                = 20000
	SAFE_MODE_COOLDOWN                = 50000
	CONTROLLER_RESERVE                = 1
	CONTROLLER_RESERVE_MAX            = 5000
	CONTROLLER_CLAIM_DOWNGRADE        = 300
	CONTROLLER_ATTACK_BLOCKED_UPGRADE = 1000
	GCL_POW                           = 2.4
	GCL_MULTIPLY                      = 50000 // Local-sim GCL scale (GCL 2 at 50k upgrade energy)
)

// RangedMassAttackPower maps Chebyshev distance (1..3) to AoE damage per RANGED_ATTACK part.
var RangedMassAttackPower = map[int]int{
	1: 10,
	2: 4,
	3: 1,
}

// FIND_* constants matching Screeps Room.find() API.
const (
	FIND_EXIT_TOP                  = 1
	FIND_EXIT_RIGHT                = 3
	FIND_EXIT_BOTTOM               = 5
	FIND_EXIT_LEFT                 = 7
	FIND_EXIT                      = 10
	FIND_CREEPS                    = 101
	FIND_MY_CREEPS                 = 102
	FIND_HOSTILE_CREEPS            = 103
	FIND_SOURCES_ACTIVE            = 104
	FIND_SOURCES                   = 105
	FIND_DROPPED_RESOURCES         = 106
	FIND_STRUCTURES                = 107
	FIND_MY_STRUCTURES             = 108
	FIND_HOSTILE_STRUCTURES        = 109
	FIND_FLAGS                     = 110
	FIND_CONSTRUCTION_SITES        = 111
	FIND_MY_SPAWNS                 = 112
	FIND_HOSTILE_SPAWNS            = 113
	FIND_MY_CONSTRUCTION_SITES     = 114
	FIND_HOSTILE_CONSTRUCTION_SITES = 115
	FIND_MINERALS                  = 116
	FIND_TOMBSTONES                = 118
)

// CONTROLLER_LEVELS maps current RCL (1..7) to the upgrade progress required for the next level.
var CONTROLLER_LEVELS = map[int]int{
	1: 200,
	2: 45000,
	3: 135000,
	4: 405000,
	5: 1215000,
	6: 3645000,
	7: 10935000,
}

// CONTROLLER_DOWNGRADE maps RCL (1..8) to downgrade timer ticks.
var CONTROLLER_DOWNGRADE = map[int]int{
	1: 20000,
	2: 10000,
	3: 20000,
	4: 40000,
	5: 80000,
	6: 120000,
	7: 150000,
	8: 200000,
}

// EXTENSION_ENERGY_CAPACITY maps RCL (0..8) to energy capacity per extension.
var EXTENSION_ENERGY_CAPACITY = map[int]int{
	0: 50,
	1: 50,
	2: 50,
	3: 50,
	4: 50,
	5: 50,
	6: 50,
	7: 100,
	8: 200,
}

// CONTROLLER_STRUCTURES maps structure type -> RCL -> maximum allowed count.
var CONTROLLER_STRUCTURES = map[string]map[int]int{
	STRUCTURE_SPAWN:     {0: 0, 1: 1, 2: 1, 3: 1, 4: 1, 5: 1, 6: 1, 7: 2, 8: 3},
	STRUCTURE_EXTENSION: {0: 0, 1: 0, 2: 5, 3: 10, 4: 20, 5: 30, 6: 40, 7: 50, 8: 60},
	STRUCTURE_LINK:      {0: 0, 1: 0, 2: 0, 3: 0, 4: 0, 5: 2, 6: 3, 7: 4, 8: 6},
	STRUCTURE_ROAD:      {0: 2500, 1: 2500, 2: 2500, 3: 2500, 4: 2500, 5: 2500, 6: 2500, 7: 2500, 8: 2500},
	STRUCTURE_WALL:      {0: 0, 1: 0, 2: 2500, 3: 2500, 4: 2500, 5: 2500, 6: 2500, 7: 2500, 8: 2500},
	STRUCTURE_RAMPART:   {0: 0, 1: 0, 2: 2500, 3: 2500, 4: 2500, 5: 2500, 6: 2500, 7: 2500, 8: 2500},
	STRUCTURE_STORAGE:   {0: 0, 1: 0, 2: 0, 3: 0, 4: 1, 5: 1, 6: 1, 7: 1, 8: 1},
	STRUCTURE_TOWER:     {0: 0, 1: 0, 2: 0, 3: 1, 4: 1, 5: 2, 6: 2, 7: 3, 8: 6},
	STRUCTURE_CONTAINER: {0: 5, 1: 5, 2: 5, 3: 5, 4: 5, 5: 5, 6: 5, 7: 5, 8: 5},
	STRUCTURE_EXTRACTOR: {0: 0, 1: 0, 2: 0, 3: 0, 4: 0, 5: 0, 6: 1, 7: 1, 8: 1},
}

// CONSTRUCTION_COST maps structure type to energy required to construct it.
var CONSTRUCTION_COST = map[string]int{
	STRUCTURE_SPAWN:     15000,
	STRUCTURE_EXTENSION: 3000,
	STRUCTURE_ROAD:      300,
	STRUCTURE_WALL:      1,
	STRUCTURE_RAMPART:   1,
	STRUCTURE_LINK:      5000,
	STRUCTURE_STORAGE:   30000,
	STRUCTURE_TOWER:     5000,
	STRUCTURE_CONTAINER: 5000,
	STRUCTURE_EXTRACTOR: 5000,
}

// STRUCTURE_HITS maps structure type to initial hits when constructed.
var STRUCTURE_HITS = map[string]int{
	STRUCTURE_SPAWN:     5000,
	STRUCTURE_EXTENSION: 1000,
	STRUCTURE_ROAD:      5000,
	STRUCTURE_WALL:      1,
	STRUCTURE_RAMPART:   1,
	STRUCTURE_LINK:      1000,
	STRUCTURE_STORAGE:   10000,
	STRUCTURE_TOWER:     3000,
	STRUCTURE_CONTAINER: 250000,
	STRUCTURE_EXTRACTOR: 500,
}

// STRUCTURE_HITS_MAX maps structure type to maximum hits.
var STRUCTURE_HITS_MAX = map[string]int{
	STRUCTURE_SPAWN:     5000,
	STRUCTURE_EXTENSION: 1000,
	STRUCTURE_ROAD:      5000,
	STRUCTURE_WALL:      300000000,
	STRUCTURE_RAMPART:   300000000,
	STRUCTURE_LINK:      1000,
	STRUCTURE_STORAGE:   10000,
	STRUCTURE_TOWER:     3000,
	STRUCTURE_CONTAINER: 250000,
	STRUCTURE_EXTRACTOR: 500,
}

// BodyCost returns the total energy cost of a creep body, or -1 if invalid.
func BodyCost(body []string) int {
	if len(body) == 0 || len(body) > MAX_CREEP_SIZE {
		return -1
	}
	total := 0
	for _, p := range body {
		c, ok := BODYPART_COST[p]
		if !ok {
			return -1
		}
		total += c
	}
	return total
}

// ChebyshevDist returns the Screeps tile range max(|ax-bx|, |ay-by|).
func ChebyshevDist(ax, ay, bx, by int) int {
	dx := ax - bx
	if dx < 0 {
		dx = -dx
	}
	dy := ay - by
	if dy < 0 {
		dy = -dy
	}
	if dx > dy {
		return dx
	}
	return dy
}

// DirectionTo returns the Screeps direction (1..8) from (fx, fy) toward (tx, ty), or 0 if same tile.
func DirectionTo(fx, fy, tx, ty int) int {
	dx := tx - fx
	dy := ty - fy
	if dx == 0 && dy == 0 {
		return 0
	}
	if dx > 0 {
		dx = 1
	} else if dx < 0 {
		dx = -1
	}
	if dy > 0 {
		dy = 1
	} else if dy < 0 {
		dy = -1
	}
	for d := TOP; d <= TOP_LEFT; d++ {
		if DirDX[d] == dx && DirDY[d] == dy {
			return d
		}
	}
	return 0
}

// InBounds reports whether (x, y) is inside the 50x50 room grid.
func InBounds(x, y int) bool {
	return x >= 0 && x < RoomSize && y >= 0 && y < RoomSize
}
