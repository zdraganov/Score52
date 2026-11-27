package movement

import (
	"fmt"
	"sort"
)

// NewHowell builds a Howell for `pairs` pairs (an odd number gets a phantom) over `rounds` rounds:
// all n-1 rounds for a complete Howell, fewer for a reduced one, where each pair meets that many
// of the others.
//
// With one set of boards (duplicated=false): from 8 pairs on the movement comes from a cyclic Room
// square, every table with its own set, numbered so the boards move down one table (or to the relay
// stand); 4 and 6 pairs have no such square and share boards between two tables (HowellShared).
// duplicated=true: a round robin where every table plays the same set, for copies of the boards.
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
	switch {
	case duplicated:
		m = roundRobin(s, rounds)
	case s >= 7:
		starter, adder := cyclicRoom(s)
		if adder == nil {
			return Movement{}, fmt.Errorf("no shared-board Howell found for %d pairs", pairs)
		}
		m = roomHowell(s, rounds, starter, adder)
	default:
		var ok bool
		if m, ok = sharedHowell(s, rounds); !ok {
			return Movement{}, fmt.Errorf("no Howell with shared boards found for %d pairs", pairs)
		}
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

// roomHowell lays the Room square out as a movement: the starter's pair i plays at a table with its
// column a_i, symbol v is pair v+1 and ∞ is pair s+1 (it stays put, NS and EW in turn).
//
// A table with column a plays set a+r in round r, so a set passes from the table with column a to the
// one with column a-1. The tables are numbered by column, starting after a gap, so the boards move down
// one table, or to the relay stand where a column is missing; sets are numbered so that table 1 plays
// set 1 in round 1.
func roomHowell(s, rounds int, starter [][2]int, a []int) Movement {
	used := map[int]bool{}
	for _, c := range a {
		used[c] = true
	}
	start := a[0]
	for _, c := range a {
		if !used[mod(c-1, s)] {
			start = c
			break
		}
	}
	order := make([]int, len(a)) // starter index (0 = ∞) by table, in column order from start
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(x, y int) bool { return mod(a[order[x]]-start, s) < mod(a[order[y]]-start, s) })
	tableOf := make([]int, len(a))
	for t, i := range order {
		tableOf[i] = t + 1
	}
	set := func(c int) int { return mod(c-start, s) + 1 }

	m := Movement{Kind: Howell, Pairs: s + 1, Tables: len(a), Rounds: rounds, Sets: s}
	for r := 0; r < rounds; r++ {
		ns, ew := s+1, r+1
		if r%2 == 1 {
			ns, ew = ew, ns
		}
		m.Seats = append(m.Seats, Seat{Round: r + 1, Table: tableOf[0], NS: ns, EW: ew, Set: set(a[0] + r)})
		for i, p := range starter {
			m.Seats = append(m.Seats, Seat{Round: r + 1, Table: tableOf[i+1], NS: mod(p[0]+r, s) + 1, EW: mod(p[1]+r, s) + 1, Set: set(a[i+1] + r)})
		}
	}
	sort.Slice(m.Seats, func(x, y int) bool {
		return m.Seats[x].Round < m.Seats[y].Round || m.Seats[x].Round == m.Seats[y].Round && m.Seats[x].Table < m.Seats[y].Table
	})
	return m
}

// sharedHowell is a small Howell for one set of boards: the circle method's round robin, with sets
// found by search so that each pair plays each set once and tables share as little as possible: no set
// at more than two tables in a round, except in as few rounds as can be (one for 6 pairs, where all
// three tables then pass one set around).
func sharedHowell(s, rounds int) (Movement, bool) {
	k := s / 2
	type match struct{ a, b int }
	var ms [][]match // by round
	for r := 0; r < rounds; r++ {
		row := []match{{s, r}}
		for i := 1; i <= k; i++ {
			row = append(row, match{mod(r+i, s), mod(r-i, s)})
		}
		ms = append(ms, row)
	}
	for crowded := 0; crowded <= rounds; crowded++ { // rounds with a set at 3+ tables
		sets := make([][]int, rounds)
		for r := range sets {
			sets[r] = make([]int, k+1)
		}
		seen := map[[2]int]bool{} // pair, set
		var try func(r, i int, onTable map[int]int, left int) bool
		try = func(r, i int, onTable map[int]int, left int) bool {
			if r == rounds {
				return true
			}
			if i == k+1 {
				for _, n := range onTable {
					if n > 2 {
						left--
						break
					}
				}
				return left >= 0 && try(r+1, 0, map[int]int{}, left)
			}
			mt := ms[r][i]
			for c := 0; c < s; c++ {
				if onTable[c] >= 2 && left == 0 || seen[[2]int{mt.a, c}] || seen[[2]int{mt.b, c}] {
					continue
				}
				onTable[c]++
				seen[[2]int{mt.a, c}], seen[[2]int{mt.b, c}] = true, true
				sets[r][i] = c
				if try(r, i+1, onTable, left) {
					return true
				}
				onTable[c]--
				delete(seen, [2]int{mt.a, c})
				delete(seen, [2]int{mt.b, c})
			}
			return false
		}
		if !try(0, 0, map[int]int{}, crowded) {
			continue
		}
		m := Movement{Kind: HowellShared, Pairs: s + 1, Tables: k + 1, Rounds: rounds, Sets: s, Shared: true}
		for r := 0; r < rounds; r++ {
			for i, mt := range ms[r] {
				ns, ew := mt.a+1, mt.b+1
				if i == 0 && r%2 == 1 {
					ns, ew = ew, ns
				}
				m.Seats = append(m.Seats, Seat{Round: r + 1, Table: i + 1, NS: ns, EW: ew, Set: sets[r][i] + 1})
			}
		}
		return m, true
	}
	return Movement{}, false
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
