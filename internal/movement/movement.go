// Package movement builds and checks pairs movements: who sits where, against whom, on which boards,
// round by round. A movement is plain data (Seats), so one from a file or BridgeMate is checked the
// same way as one built here.
package movement

import (
	"fmt"
	"sort"
)

// Seat is one table in one round: pair NS against pair EW on board set Set. All numbers are 1-based;
// pair 0 is nobody (the other pair sits out).
type Seat struct {
	Round, Table int
	NS, EW       int
	Set          int
}

type Kind string

const (
	Mitchell     Kind = "mitchell"
	SkipMitchell Kind = "skip-mitchell"
	Howell       Kind = "howell"
	// HowellDuplicated is a round robin where every table plays the same board set in a round:
	// it needs as many copies of the boards as tables (a dealing machine), and exists for any size.
	HowellDuplicated Kind = "howell-duplicated"
	// HowellShared is a small Howell (4-7 pairs) for one set of boards: tables share a set in a round,
	// passing its boards between them. A Howell of 4 or 6 pairs can't give every table its own set (there
	// is no Room square of side 3 or 5); for 6 pairs at least one round has all three tables on one set.
	HowellShared Kind = "howell-shared"
)

// Movement is a complete seating plan.
type Movement struct {
	Kind   Kind
	Pairs  int // pair ids are 1..Pairs
	Tables int
	Rounds int
	Sets   int // board sets 1..Sets; set s holds boards (s-1)*bpr+1 .. s*bpr
	// TwoFields: a Mitchell's NS pairs (1..Tables) and EW pairs (Tables+1..2*Tables) are ranked
	// separately unless arrow switches or a one-field factoring make it one.
	TwoFields bool
	// Duplicated: several tables may play the same set in the same round (copies of the boards).
	Duplicated bool
	// Shared: tables may share a set in a round (one copy, its boards passed between them).
	Shared bool
	// Phantom is the pair id that stands for a sit-out (an odd number of pairs), 0 if none.
	Phantom int
	Seats   []Seat
}

// Boards of set s with bpr boards per round.
func Boards(s, bpr int) []int {
	b := make([]int, bpr)
	for i := range b {
		b[i] = (s-1)*bpr + i + 1
	}
	return b
}

// Round returns the seats of round r, by table.
func (m Movement) Round(r int) []Seat {
	var out []Seat
	for _, s := range m.Seats {
		if s.Round == r {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Table < out[j].Table })
	return out
}

// Met is every pair of opponents in the movement, phantom meetings excluded.
func (m Movement) Met() map[[2]int]bool {
	met := map[[2]int]bool{}
	for _, s := range m.Seats {
		if s.NS == 0 || s.EW == 0 || s.NS == m.Phantom || s.EW == m.Phantom {
			continue
		}
		met[key(s.NS, s.EW)] = true
	}
	return met
}

func key(a, b int) [2]int {
	if a > b {
		a, b = b, a
	}
	return [2]int{a, b}
}

// Next is where the boards of table t go after round r: the tables that play its set in round r+1.
// None means the set rests on the relay stand (or the session is over); several, a duplicated
// movement's copies.
func (m Movement) Next(r, t int) []int {
	set := 0
	for _, s := range m.Seats {
		if s.Round == r && s.Table == t {
			set = s.Set
		}
	}
	var out []int
	for _, s := range m.Seats {
		if s.Round == r+1 && s.Set == set {
			out = append(out, s.Table)
		}
	}
	sort.Ints(out)
	return out
}

// SharedWith is the other table that plays the same set as table t in round r, 0 if none.
func (m Movement) SharedWith(r, t int) int {
	set := 0
	for _, s := range m.Seats {
		if s.Round == r && s.Table == t {
			set = s.Set
		}
	}
	for _, s := range m.Seats {
		if s.Round == r && s.Set == set && s.Table != t {
			return s.Table
		}
	}
	return 0
}

// Validate checks that the movement is playable: nobody sits twice in a round, no two pairs meet
// twice, no pair plays a set twice, and (unless boards are duplicated) no set is at two tables at once.
func (m Movement) Validate() error {
	type rp struct{ r, p int }
	type rs struct{ r, s int }
	type ps struct{ p, s int }
	type rt struct{ r, t int }
	seated, tablesOn, played, tables := map[rp]bool{}, map[rs]int{}, map[ps]bool{}, map[rt]bool{}
	met := map[[2]int]int{}
	for _, s := range m.Seats {
		if s.Round < 1 || s.Round > m.Rounds || s.Table < 1 || s.Table > m.Tables || s.Set < 1 || s.Set > m.Sets {
			return fmt.Errorf("round %d table %d: out of range (set %d)", s.Round, s.Table, s.Set)
		}
		if tables[rt{s.Round, s.Table}] {
			return fmt.Errorf("round %d: table %d used twice", s.Round, s.Table)
		}
		tables[rt{s.Round, s.Table}] = true
		if s.NS == s.EW {
			return fmt.Errorf("round %d table %d: pair %d against itself", s.Round, s.Table, s.NS)
		}
		for _, p := range []int{s.NS, s.EW} {
			if p == 0 {
				continue
			}
			if p < 0 || p > m.Pairs {
				return fmt.Errorf("round %d table %d: no pair %d", s.Round, s.Table, p)
			}
			if seated[rp{s.Round, p}] {
				return fmt.Errorf("round %d: pair %d seated twice", s.Round, p)
			}
			seated[rp{s.Round, p}] = true
			if played[ps{p, s.Set}] {
				return fmt.Errorf("pair %d plays set %d twice (round %d)", p, s.Set, s.Round)
			}
			played[ps{p, s.Set}] = true
		}
		if !m.Duplicated {
			n := tablesOn[rs{s.Round, s.Set}] + 1
			tablesOn[rs{s.Round, s.Set}] = n
			if n > 1 && !m.Shared {
				return fmt.Errorf("round %d: set %d at %d tables", s.Round, s.Set, n)
			}
		}
		if s.NS != 0 && s.EW != 0 {
			if met[key(s.NS, s.EW)]++; met[key(s.NS, s.EW)] > 1 {
				return fmt.Errorf("pairs %d and %d meet twice", s.NS, s.EW)
			}
		}
	}
	return nil
}
