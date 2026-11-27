package engine

import "container/heap"

// CostMatrix stores per-tile pathfinding cost overrides for a 50x50 room.
// A value of 0 uses the default terrain/structure cost; 255 marks a tile impassable.
type CostMatrix struct {
	Bits [RoomSize * RoomSize]uint8
}

// NewCostMatrix returns a zero-initialized CostMatrix.
func NewCostMatrix() *CostMatrix {
	return &CostMatrix{}
}

// Set assigns a movement cost (0..255) to tile (x, y).
func (cm *CostMatrix) Set(x, y int, cost uint8) {
	if !InBounds(x, y) {
		return
	}
	cm.Bits[y*RoomSize+x] = cost
}

// Get returns the movement cost override at (x, y), or 255 if out of bounds.
func (cm *CostMatrix) Get(x, y int) uint8 {
	if !InBounds(x, y) {
		return 255
	}
	return cm.Bits[y*RoomSize+x]
}

// Clone returns a deep copy of the CostMatrix.
func (cm *CostMatrix) Clone() *CostMatrix {
	cp := *cm
	return &cp
}

// Goal specifies a target tile and acceptable Chebyshev range for pathfinding.
type Goal struct {
	X     int
	Y     int
	Range int
}

// PathOpts configures SearchPath behavior.
type PathOpts struct {
	PlainCost    uint8
	SwampCost    uint8
	MaxOps       int
	IgnoreCreeps bool
	IgnoreRoads  bool
	CostMatrix   *CostMatrix
	Flee         bool
	MoverUser    string
}

// PathStep represents one step along a computed path.
type PathStep struct {
	X         int
	Y         int
	DX        int
	DY        int
	Direction int
}

// PathResult is returned by SearchPath.
type PathResult struct {
	Path       []PathStep
	Ops        int
	Cost       int
	Incomplete bool
}

type astarNode struct {
	idx int
	g   int
	f   int
	tie int
}

type astarHeap []astarNode

func (h astarHeap) Len() int { return len(h) }
func (h astarHeap) Less(i, j int) bool {
	if h[i].f != h[j].f {
		return h[i].f < h[j].f
	}
	if h[i].tie != h[j].tie {
		return h[i].tie < h[j].tie
	}
	if h[i].g != h[j].g {
		return h[i].g > h[j].g
	}
	return h[i].idx < h[j].idx
}
func (h astarHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *astarHeap) Push(x any)   { *h = append(*h, x.(astarNode)) }
func (h *astarHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

// SearchPath runs A* over a 50x50 room grid from (startX, startY) to the nearest Goal.
func SearchPath(room *Room, startX, startY int, goals []Goal, opts PathOpts) PathResult {
	if room == nil || !InBounds(startX, startY) || len(goals) == 0 {
		return PathResult{Incomplete: true}
	}

	plainCost := opts.PlainCost
	if plainCost == 0 {
		plainCost = 2
	}
	swampCost := opts.SwampCost
	if swampCost == 0 {
		swampCost = 10
	}
	maxOps := opts.MaxOps
	if maxOps <= 0 {
		maxOps = 2000
	}

	// Precompute tile costs for fast lookup during A*.
	var tileCost [RoomSize * RoomSize]uint8
	for i := 0; i < RoomSize*RoomSize; i++ {
		switch room.Terrain[i] {
		case TERRAIN_MASK_WALL:
			tileCost[i] = 255
		case TERRAIN_MASK_SWAMP:
			tileCost[i] = swampCost
		default:
			tileCost[i] = plainCost
		}
	}

	// Overlay entities (roads and obstacles).
	for _, e := range room.Entities {
		if !InBounds(e.X, e.Y) {
			continue
		}
		idx := e.Y*RoomSize + e.X
		if e.Type == STRUCTURE_ROAD {
			if !opts.IgnoreRoads && tileCost[idx] < 255 {
				tileCost[idx] = 1
			}
			continue
		}
		if e.Type == TYPE_CREEP && opts.IgnoreCreeps {
			continue
		}
		if e.IsObstacle(opts.MoverUser) {
			tileCost[idx] = 255
		}
	}

	// Apply explicit CostMatrix overrides last.
	if opts.CostMatrix != nil {
		for i := 0; i < RoomSize*RoomSize; i++ {
			if c := opts.CostMatrix.Bits[i]; c != 0 {
				tileCost[i] = c
			}
		}
	}

	// Minimum possible step cost for admissible heuristic.
	minStepCost := int(plainCost)
	if int(swampCost) < minStepCost {
		minStepCost = int(swampCost)
	}
	if !opts.IgnoreRoads && minStepCost > 1 {
		minStepCost = 1
	}
	if opts.CostMatrix != nil && minStepCost > 1 {
		for i := 0; i < RoomSize*RoomSize; i++ {
			if c := opts.CostMatrix.Bits[i]; c > 0 && c < 255 && int(c) < minStepCost {
				minStepCost = int(c)
				if minStepCost == 1 {
					break
				}
			}
		}
	}
	if minStepCost < 1 {
		minStepCost = 1
	}

	heuristic := func(x, y int) int {
		if opts.Flee {
			// Want to reach at least g.Range away from every goal.
			rem := 0
			for _, g := range goals {
				d := ChebyshevDist(x, y, g.X, g.Y)
				if d < g.Range && g.Range-d > rem {
					rem = g.Range - d
				}
			}
			return rem * minStepCost
		}
		best := 1 << 30
		for _, g := range goals {
			d := ChebyshevDist(x, y, g.X, g.Y) - g.Range
			if d < 0 {
				d = 0
			}
			if d < best {
				best = d
			}
		}
		return best * minStepCost
	}

	isGoal := func(x, y int) bool {
		if opts.Flee {
			for _, g := range goals {
				if ChebyshevDist(x, y, g.X, g.Y) < g.Range {
					return false
				}
			}
			return true
		}
		for _, g := range goals {
			if ChebyshevDist(x, y, g.X, g.Y) <= g.Range {
				return true
			}
		}
		return false
	}

	if isGoal(startX, startY) {
		return PathResult{}
	}

	const inf = 1 << 29
	var gScore [RoomSize * RoomSize]int
	var parent [RoomSize * RoomSize]int
	var closed [RoomSize * RoomSize]bool
	for i := range gScore {
		gScore[i] = inf
		parent[i] = -1
	}

	startIdx := startY*RoomSize + startX
	gScore[startIdx] = 0
	startH := heuristic(startX, startY)

	tieBreak := func(x, y int) int {
		best := 1 << 30
		for _, g := range goals {
			d := abs(x-g.X) + abs(y-g.Y)
			if d < best {
				best = d
			}
		}
		if opts.Flee {
			return -best
		}
		return best
	}

	open := &astarHeap{{idx: startIdx, g: 0, f: startH, tie: tieBreak(startX, startY)}}
	heap.Init(open)

	bestIdx := startIdx
	bestH := startH
	ops := 0

	for open.Len() > 0 && ops < maxOps {
		cur := heap.Pop(open).(astarNode)
		if closed[cur.idx] {
			continue
		}
		closed[cur.idx] = true
		ops++

		cx, cy := cur.idx%RoomSize, cur.idx/RoomSize
		if cur.idx != startIdx && isGoal(cx, cy) {
			return PathResult{
				Path:       reconstructPath(startIdx, cur.idx, parent[:]),
				Ops:        ops,
				Cost:       cur.g,
				Incomplete: false,
			}
		}

		if h := heuristic(cx, cy); h < bestH {
			bestH = h
			bestIdx = cur.idx
		}

		for d := TOP; d <= TOP_LEFT; d++ {
			nx, ny := cx+DirDX[d], cy+DirDY[d]
			if !InBounds(nx, ny) {
				continue
			}
			nIdx := ny*RoomSize + nx
			if closed[nIdx] {
				continue
			}
			cost := tileCost[nIdx]
			if cost == 255 {
				continue
			}
			ng := cur.g + int(cost)
			if ng < gScore[nIdx] {
				gScore[nIdx] = ng
				parent[nIdx] = cur.idx
				nf := ng + heuristic(nx, ny)
				heap.Push(open, astarNode{idx: nIdx, g: ng, f: nf, tie: tieBreak(nx, ny)})
			}
		}
	}

	// Goal not reached within maxOps or unreachable: return best partial path.
	return PathResult{
		Path:       reconstructPath(startIdx, bestIdx, parent[:]),
		Ops:        ops,
		Cost:       gScore[bestIdx],
		Incomplete: true,
	}
}

func reconstructPath(startIdx, endIdx int, parent []int) []PathStep {
	if endIdx == startIdx || endIdx < 0 {
		return nil
	}
	var rev []int
	for cur := endIdx; cur != startIdx && cur >= 0; cur = parent[cur] {
		rev = append(rev, cur)
	}
	if len(rev) == 0 {
		return nil
	}
	steps := make([]PathStep, 0, len(rev))
	px, py := startIdx%RoomSize, startIdx/RoomSize
	for i := len(rev) - 1; i >= 0; i-- {
		nx, ny := rev[i]%RoomSize, rev[i]/RoomSize
		dx, dy := nx-px, ny-py
		steps = append(steps, PathStep{
			X:         nx,
			Y:         ny,
			DX:        dx,
			DY:        dy,
			Direction: DirectionTo(px, py, nx, ny),
		})
		px, py = nx, ny
	}
	return steps
}

// FindDirection returns the next movement direction (1..8) from (fromX, fromY) toward (toX, toY),
// or 0 if already at the destination (or adjacent to an impassable destination) or if no path exists.
func FindDirection(room *Room, fromX, fromY, toX, toY int, ignoreCreeps bool) int {
	return FindDirectionFor(room, fromX, fromY, toX, toY, ignoreCreeps, "")
}

// FindDirectionFor returns the next movement direction (1..8) from (fromX, fromY) toward (toX, toY)
// for moverUser (respecting hostile Ramparts), or 0 if already at the destination (or adjacent to
// an impassable destination) or if no path exists.
func FindDirectionFor(room *Room, fromX, fromY, toX, toY int, ignoreCreeps bool, moverUser string) int {
	if room == nil || !InBounds(fromX, fromY) || !InBounds(toX, toY) {
		return 0
	}
	if fromX == toX && fromY == toY {
		return 0
	}
	rng := 0
	if !room.IsWalkableFor(toX, toY, moverUser, ignoreCreeps) {
		rng = 1
	}
	res := SearchPath(room, fromX, fromY, []Goal{{X: toX, Y: toY, Range: rng}}, PathOpts{
		IgnoreCreeps: ignoreCreeps,
		MoverUser:    moverUser,
	})
	if len(res.Path) == 0 {
		return 0
	}
	return res.Path[0].Direction
}
