package movement

import "fmt"

// NewHowell builds a Howell for `pairs` pairs (an odd number gets a phantom) over `rounds` rounds:
// all n-1 rounds for a complete Howell, fewer for a reduced one, where each pair meets that many
// of the others.
//
// From 8 pairs on the movement comes from a cyclic Room square, so the board sets are shared and
// only one copy of the boards is needed: every round each table moves its set on by one and the
// pairs move by a fixed pattern. A Room square does not exist for 4 or 6 pairs; they (and
// duplicated=true) get a round robin in which all tables play the same set (duplicated boards).
func NewHowell(pairs, rounds int, duplicated bool) (Movement, error) {
	phantom := 0
	if pairs%2 == 1 {
		pairs++
		phantom = pairs
	}
	if pairs < 4 {
		return Movement{}, fmt.Errorf("a Howell needs at least 3 pairs")
	}
	s := pairs - 1
	if rounds < 1 || rounds > s {
		return Movement{}, fmt.Errorf("a Howell for %d pairs plays 1..%d rounds, not %d", pairs, s, rounds)
	}
	var m Movement
	if !duplicated && s >= 7 {
		starter, adder := cyclicRoom(s)
		if adder == nil {
			return Movement{}, fmt.Errorf("no shared-board Howell found for %d pairs", pairs)
		}
		m = roomHowell(s, rounds, starter, adder)
	} else {
		m = roundRobin(s, rounds)
	}
	m.Phantom = phantom
	return m, m.Validate()
}

// cyclicRoom finds a cyclic Room square of odd side s over Z_s and ∞: a starter (a split of
// Z_s\{0} into pairs {x,y} whose differences ±(x-y) cover every nonzero element once) and an adder
// (distinct columns a_0..a_k such that -a_0 and x-a_i, y-a_i are all distinct). Row r then holds the
// pair {x_i+r, y_i+r} in column a_i+r and {∞, r} in column a_0+r: each row and column holds every
// symbol once, and every two symbols share exactly one cell. The patterned starter {i,-i} is tried
// first (it gives the most regular movement), then every other starter.
func cyclicRoom(s int) (starter [][2]int, adder []int) {
	k := s / 2
	pat := make([][2]int, k)
	for i := range pat {
		pat[i] = [2]int{i + 1, s - i - 1}
	}
	if a := findAdder(s, pat); a != nil {
		return pat, a
	}
	var found [][2]int
	var a []int
	var rec func(rem []int, pairs [][2]int, diffs map[int]bool) bool
	rec = func(rem []int, pairs [][2]int, diffs map[int]bool) bool {
		if len(rem) == 0 {
			if a = findAdder(s, pairs); a != nil {
				found = append([][2]int(nil), pairs...)
				return true
			}
			return false
		}
		x := rem[0]
		for _, y := range rem[1:] {
			d1, d2 := mod(x-y, s), mod(y-x, s)
			if diffs[d1] || diffs[d2] {
				continue
			}
			var r []int
			for _, e := range rem {
				if e != x && e != y {
					r = append(r, e)
				}
			}
			diffs[d1], diffs[d2] = true, true
			if rec(r, append(pairs, [2]int{x, y}), diffs) {
				return true
			}
			delete(diffs, d1)
			delete(diffs, d2)
		}
		return false
	}
	rem := make([]int, s-1)
	for i := range rem {
		rem[i] = i + 1
	}
	if rec(rem, nil, map[int]bool{}) {
		return found, a
	}
	return nil, nil
}

// findAdder searches the adder of a starter (see cyclicRoom); a[0] is the column of {∞,0}.
func findAdder(s int, starter [][2]int) []int {
	a := make([]int, len(starter)+1)
	usedA := make([]bool, s)
	usedV := make([]bool, s)
	var try func(i int) bool
	try = func(i int) bool {
		if i > len(starter) {
			return true
		}
		for c := 0; c < s; c++ {
			if usedA[c] {
				continue
			}
			var vs []int
			if i == 0 {
				vs = []int{mod(-c, s)}
			} else {
				vs = []int{mod(starter[i-1][0]-c, s), mod(starter[i-1][1]-c, s)}
			}
			free := true
			for _, v := range vs {
				free = free && !usedV[v]
			}
			if !free || (len(vs) == 2 && vs[0] == vs[1]) {
				continue
			}
			usedA[c] = true
			for _, v := range vs {
				usedV[v] = true
			}
			a[i] = c
			if try(i + 1) {
				return true
			}
			usedA[c] = false
			for _, v := range vs {
				usedV[v] = false
			}
		}
		return false
	}
	if !try(0) {
		return nil
	}
	return a
}

// roomHowell lays the Room square out as a movement: table i+1 holds the starter's pair i, symbol v
// is pair v+1 and ∞ is pair s+1 (it stays at table 1, NS and EW in turn).
func roomHowell(s, rounds int, starter [][2]int, a []int) Movement {
	m := Movement{Kind: Howell, Pairs: s + 1, Tables: len(starter) + 1, Rounds: rounds, Sets: s}
	for r := 0; r < rounds; r++ {
		ns, ew := s+1, r+1
		if r%2 == 1 {
			ns, ew = ew, ns
		}
		m.Seats = append(m.Seats, Seat{Round: r + 1, Table: 1, NS: ns, EW: ew, Set: mod(a[0]+r, s) + 1})
		for i, p := range starter {
			m.Seats = append(m.Seats, Seat{Round: r + 1, Table: i + 2, NS: mod(p[0]+r, s) + 1, EW: mod(p[1]+r, s) + 1, Set: mod(a[i+1]+r, s) + 1})
		}
	}
	return m
}

// roundRobin is the circle method: pair s+1 stays at table 1 against pair r, the others pair off
// around it; every table plays set r in round r.
func roundRobin(s, rounds int) Movement {
	k := s / 2
	m := Movement{Kind: HowellDuplicated, Pairs: s + 1, Tables: k + 1, Rounds: rounds, Sets: rounds, Duplicated: true}
	for r := 0; r < rounds; r++ {
		ns, ew := s+1, r+1
		if r%2 == 1 {
			ns, ew = ew, ns
		}
		m.Seats = append(m.Seats, Seat{Round: r + 1, Table: 1, NS: ns, EW: ew, Set: r + 1})
		for i := 1; i <= k; i++ {
			m.Seats = append(m.Seats, Seat{Round: r + 1, Table: i + 1, NS: mod(r+i, s) + 1, EW: mod(r-i, s) + 1, Set: r + 1})
		}
	}
	return m
}
