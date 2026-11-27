package movement

import (
	"fmt"
	"math/rand/v2"
	"sort"
)

// NewReducedHowell is a Howell for `pairs` pairs over `rounds` rounds on at most maxSets sets: the first
// rounds of the complete Howell's round robin, every pair meeting a new pair each round, one field.
//
// Played complete (rounds = pairs-1) on one set per table, it is the Room square Howell when maxSets
// allows. Reduced, its tables can outnumber the sets: then sets are assigned by search so that no pair
// plays a set twice and no set is at more than two tables in a round (the director duplicates those),
// as few as can be.
func NewReducedHowell(pairs, rounds, maxSets int) (Movement, error) {
	full, err := NewHowell(pairs, rounds, false)
	if err != nil {
		return Movement{}, err
	}
	n := full.Pairs // with the phantom
	if rounds == n-1 && full.Sets <= maxSets && !full.Shared {
		return full, nil
	}
	if rounds > maxSets {
		return Movement{}, fmt.Errorf("%d rounds need %d sets, at most %d", rounds, rounds, maxSets)
	}
	// as many sets as tables when the boards allow (nobody shares), at least one a round; a set more when
	// that leaves the search no room, up to what the boards allow
	for sets := max(min(maxSets, full.Tables), rounds); sets <= maxSets; sets++ {
		m := full
		m.Seats = append([]Seat(nil), full.Seats...)
		m.Kind, m.Sets, m.Shared, m.Duplicated = Howell, sets, sets < full.Tables, false
		if assignSets(&m, sets, 2) {
			return m, m.Validate()
		}
	}
	return Movement{}, fmt.Errorf("no sets for a reduced Howell of %d pairs over %d rounds within %d sets", pairs, rounds, maxSets)
}

// assignSets gives every seat a set in 1..sets so that no pair plays a set twice and no set is at more
// than cap tables in a round. That is colouring the meetings (each pair's in different colours, with
// at most cap of a colour in a round); with sets above the 14 colours a 13-round field needs at most,
// local search finds one: start anywhere within the round caps, then keep moving a meeting that clashes
// to the set (or swapping it with the meeting of the round on that set) that clashes least, until
// nothing does. Deterministic: a fixed seed. A sit-out's table gets a set too, which nobody plays.
func assignSets(m *Movement, sets, cap int) bool {
	if sets*cap < m.Tables {
		return false
	}
	if m.Tables <= sets {
		cap = 1 // a set for every table: nobody shares
	}
	rng := rand.New(rand.NewPCG(52, uint64(m.Pairs*100+m.Rounds)))
	n := len(m.Seats)
	pair := func(p int) bool { return p != 0 && !m.IsPhantom(p) }
	color := make([]int, n)
	count := map[[2]int]int{}     // pair, set -> meetings
	onRound := map[[2]int][]int{} // round, set -> seats
	for r := 1; r <= m.Rounds; r++ {
		k := 0
		for i, s := range m.Seats {
			if s.Round == r {
				color[i] = k%sets + 1
				k++
			}
		}
	}
	add := func(i, c, d int) {
		s := m.Seats[i]
		for _, p := range []int{s.NS, s.EW} {
			if pair(p) {
				count[[2]int{p, c}] += d
			}
		}
	}
	for i, s := range m.Seats {
		add(i, color[i], 1)
		onRound[[2]int{s.Round, color[i]}] = append(onRound[[2]int{s.Round, color[i]}], i)
	}
	// clash of seat i if it had set c: how many of its pairs' other meetings are on c
	clash := func(i, c int) int {
		s, v := m.Seats[i], 0
		for _, p := range []int{s.NS, s.EW} {
			if pair(p) {
				v += count[[2]int{p, c}]
				if c == color[i] {
					v-- // itself
				}
			}
		}
		return v
	}
	move := func(i, c int) {
		s := m.Seats[i]
		old := color[i]
		add(i, old, -1)
		lst := onRound[[2]int{s.Round, old}]
		for k, j := range lst {
			if j == i {
				onRound[[2]int{s.Round, old}] = append(lst[:k:k], lst[k+1:]...)
				break
			}
		}
		color[i] = c
		add(i, c, 1)
		onRound[[2]int{s.Round, c}] = append(onRound[[2]int{s.Round, c}], i)
	}
	// sharing costs too: a round needs at least tables-sets sets at two tables, any more is a copy too many
	tablesIn := map[int]int{}
	for _, s := range m.Seats {
		tablesIn[s.Round]++
	}
	excess := func() int {
		e := 0
		for r := 1; r <= m.Rounds; r++ {
			doubled := 0
			for c := 1; c <= sets; c++ {
				if len(onRound[[2]int{r, c}]) > 1 {
					doubled++
				}
			}
			e += doubled - max(0, tablesIn[r]-sets)
		}
		return e
	}
	for step := 0; step < 400000; step++ {
		var bad []int
		for i := range m.Seats {
			if clash(i, color[i]) > 0 {
				bad = append(bad, i)
			}
		}
		if len(bad) == 0 {
			if excess() > 0 && step < 300000 {
				// valid but sharing more than needed: move a table off a doubled set to a free one
				var spare []int
				for i, s := range m.Seats {
					if len(onRound[[2]int{s.Round, color[i]}]) > 1 {
						spare = append(spare, i)
					}
				}
				bad = spare
			} else {
				for i := range m.Seats {
					m.Seats[i].Set = color[i]
				}
				return true
			}
		}
		i := bad[rng.IntN(len(bad))]
		r, cur := m.Seats[i].Round, color[i]
		bestD, bestC, bestJ := 1<<30, 0, -1
		for c := 1; c <= sets; c++ {
			if c == cur {
				continue
			}
			if len(onRound[[2]int{r, c}]) < cap { // a free place on set c
				d := 4 * (clash(i, c) - clash(i, cur))
				if len(onRound[[2]int{r, c}]) == 1 {
					d++ // c becomes shared
				}
				if len(onRound[[2]int{r, cur}]) == 2 {
					d-- // cur stops being shared
				}
				if d < bestD || d == bestD && rng.IntN(2) == 0 {
					bestD, bestC, bestJ = d, c, -1
				}
				continue
			}
			for _, j := range onRound[[2]int{r, c}] { // swap with a table on set c
				d := 4 * (clash(i, c) - clash(i, cur) + clash(j, cur) - clash(j, c))
				if d < bestD || d == bestD && rng.IntN(2) == 0 {
					bestD, bestC, bestJ = d, c, j
				}
			}
		}
		if bestC == 0 || bestD > 0 && rng.IntN(10) > 0 { // mostly downhill, sometimes a step up to get out
			continue
		}
		if bestJ >= 0 {
			move(bestJ, cur)
		}
		move(i, bestC)
	}
	return false
}

// SideSets are, by section, the sets some round has at two tables: each needs a second copy, on an empty
// table, for the table that shares it (the BBF TD's way).
func (m Movement) SideSets() map[int][]int {
	out := map[int][]int{}
	seen := map[[2]int]bool{}
	for r := 1; r <= m.Rounds; r++ {
		on := map[[2]int]int{} // section, set
		for _, s := range m.Round(r) {
			k := [2]int{s.Section, s.Set}
			if on[k]++; on[k] == 2 && !seen[k] {
				seen[k] = true
				out[s.Section] = append(out[s.Section], s.Set)
			}
		}
	}
	for k := range out {
		sort.Ints(out[k])
	}
	return out
}

// Duplicates is how many sets a round needs twice at most (tables sharing a set): the extra copies.
func (m Movement) Duplicates() int {
	most := 0
	for r := 1; r <= m.Rounds; r++ {
		on := map[[2]int]int{} // section, set
		extra := 0
		for _, s := range m.Round(r) {
			k := [2]int{s.Section, s.Set}
			if on[k]++; on[k] > 1 {
				extra++
			}
		}
		most = max(most, extra)
	}
	return most
}
