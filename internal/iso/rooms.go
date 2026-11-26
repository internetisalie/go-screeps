package iso

import (
	"strconv"
	"strings"
)

// ParseRoomCoords parses a Screeps room name (e.g. "W9N7", "E0S0") into continuous
// world coordinates (wx, wy) where W0 (-1) is adjacent to E0 (0) and N0 (-1) is adjacent to S0 (0).
func ParseRoomCoords(name string) (wx, wy int, ok bool) {
	s := strings.ToUpper(strings.TrimSpace(name))
	if len(s) < 4 {
		return 0, 0, false
	}
	ew := s[0]
	if ew != 'W' && ew != 'E' {
		return 0, 0, false
	}
	nsIdx := strings.IndexAny(s[1:], "NS")
	if nsIdx <= 0 {
		return 0, 0, false
	}
	nsIdx++ // index in s
	if nsIdx >= len(s)-1 {
		return 0, 0, false
	}
	ns := s[nsIdx]

	xStr := s[1:nsIdx]
	yStr := s[nsIdx+1:]
	if !allDigits(xStr) || !allDigits(yStr) {
		return 0, 0, false
	}
	xVal, err1 := strconv.Atoi(xStr)
	yVal, err2 := strconv.Atoi(yStr)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}

	if ew == 'W' {
		wx = -(xVal + 1)
	} else {
		wx = xVal
	}
	if ns == 'N' {
		wy = -(yVal + 1)
	} else {
		wy = yVal
	}
	return wx, wy, true
}

func allDigits(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// FormatRoomCoords converts continuous world coordinates (wx, wy) back into a Screeps room name.
func FormatRoomCoords(wx, wy int) string {
	var b strings.Builder
	if wx < 0 {
		b.WriteByte('W')
		b.WriteString(strconv.Itoa(-wx - 1))
	} else {
		b.WriteByte('E')
		b.WriteString(strconv.Itoa(wx))
	}
	if wy < 0 {
		b.WriteByte('N')
		b.WriteString(strconv.Itoa(-wy - 1))
	} else {
		b.WriteByte('S')
		b.WriteString(strconv.Itoa(wy))
	}
	return b.String()
}

// NeighborRoomNames returns all room names within Chebyshev distance radius of center
// (e.g. radius=1 returns the 3x3 grid of 9 rooms).
func NeighborRoomNames(center string, radius int) []string {
	cwx, cwy, ok := ParseRoomCoords(center)
	if !ok || radius < 0 {
		return nil
	}
	side := 2*radius + 1
	out := make([]string, 0, side*side)
	for dy := -radius; dy <= radius; dy++ {
		for dx := -radius; dx <= radius; dx++ {
			out = append(out, FormatRoomCoords(cwx+dx, cwy+dy))
		}
	}
	return out
}
