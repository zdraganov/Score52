package movement

import (
	"fmt"
	"sort"
)

// A Layout is one way to seat a whole field in one session: the tables split into sections of as equal a
// size as can be, every section a Mitchell of the same rounds and boards per round, all on the same deals,
// so a board is scored across the field (every section's results on it together).
//
// In a Mitchell every table has a set in every round, so a section of T tables needs T sets: the boards
// to deal are the largest section's tables × boards per round, once per section.
type Layout struct {
	Sections       []Section
	Rounds         int
	BoardsPerRound int
	Boards         int // each pair plays
	BoardsPerCopy  int // boards 1..N to deal for each section
	Copies         int // one per section
	Played         int // times each board is played across the field: its comparisons
	Complete       bool
	// Duplicates: sets to copy once more for relay-and-stand sections (tables 1 and 2 share one)
	Duplicates int
	SitOuts    int // sections with a pair sitting out each round
}

// Section is one section of a layout.
type Section struct {
	Letter string
	Tables int
	Pairs  int // 2×Tables, or one less with a sit-out
	Kind   Kind
}

// Deal is the boards to deal: Copies copies of boards 1..BoardsPerCopy, and a duplicate of a set for
// each relay section.
func (l Layout) Deal() int { return l.Copies*l.BoardsPerCopy + l.Duplicates*l.BoardsPerRound }

func (l Layout) String() string {
	var s string
	for i, x := range l.Sections {
		if i > 0 {
			s += " + "
		}
		s += fmt.Sprintf("%s:%d", x.Letter, x.Tables)
	}
	return fmt.Sprintf("%s tables, %d rounds × %d = %d boards, deal %d × boards 1-%d, each board played %d times",
		s, l.Rounds, l.BoardsPerRound, l.Boards, l.Copies, l.BoardsPerCopy, l.Played)
}

// Section sizes a layout keeps to: smaller ones meet too few opponents. A section needs a set per table,
// and boards go up to MaxBoard (the federation's boxes are 1-32), which bounds its size too.
const (
	MinSectionTables = 7
	MaxSectionTables = 16
	MaxBoard         = 32
)

// Layouts lists the ways to seat `pairs` pairs for about `boards` boards (within ±tol), best first: the
// number of boards closest to the target; then each board played the most times across the field (its
// comparisons: sections × rounds); then the fewest sections; then fewer moves. maxDeal > 0 keeps to
// layouts whose boards per copy fit what was dealt, and maxCopies > 0 to that many copies.
func Layouts(pairs, boards, tol, maxDeal, maxCopies int) []Layout {
	need := (pairs + 1) / 2
	var out []Layout
	for k := 1; k <= need/MinSectionTables+1 && k <= 26; k++ {
		if maxCopies > 0 && k > maxCopies {
			break
		}
		// up to one pair sitting out in each section: the tables may run to (pairs + k) / 2
		for tables := need; 2*tables-pairs <= k; tables++ {
			sizes := split(tables, k)
			if sizes[k-1] < MinSectionTables && k > 1 || sizes[0] > MaxSectionTables {
				continue
			}
			for bpr := 2; bpr <= 5; bpr++ {
				for r := (boards - tol + bpr - 1) / bpr; r*bpr <= boards+tol; r++ {
					l, ok := layout(pairs, sizes, r, bpr)
					if ok && l.BoardsPerCopy <= MaxBoard && (maxDeal == 0 || l.BoardsPerCopy <= maxDeal) {
						out = append(out, l)
					}
				}
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if da, db := abs(a.Boards-boards), abs(b.Boards-boards); da != db {
			return da < db
		}
		if a.Played != b.Played {
			return a.Played > b.Played
		}
		if a.SitOuts != b.SitOuts {
			return a.SitOuts < b.SitOuts
		}
		if a.Duplicates != b.Duplicates {
			return a.Duplicates < b.Duplicates
		}
		if len(a.Sections) != len(b.Sections) {
			return len(a.Sections) < len(b.Sections)
		}
		if a.Complete != b.Complete {
			return a.Complete
		}
		return a.BoardsPerRound > b.BoardsPerRound
	})
	return out
}

// split divides n tables into k sections, the larger ones first.
func split(n, k int) []int {
	out := make([]int, k)
	for i := range out {
		out[i] = n / k
		if i < n%k {
			out[i]++
		}
	}
	return out
}

func layout(pairs int, sizes []int, rounds, bpr int) (Layout, bool) {
	l := Layout{Rounds: rounds, BoardsPerRound: bpr, Boards: rounds * bpr, Copies: len(sizes), Complete: true}
	out := 2*sum(sizes) - pairs // sit-outs: the first sections get one each
	if out < 0 || out > len(sizes) {
		return l, false
	}
	for i, t := range sizes {
		// odd tables: a Mitchell, every round; even: skip Mitchell up to T-1 rounds, relay and stand for all T
		kind, max := Mitchell, t
		switch {
		case t%2 == 1:
		case rounds <= t-1:
			kind, max = SkipMitchell, t-1
		default:
			kind = RelayMitchell
			l.Duplicates++
		}
		if t < 3 || rounds > max {
			return l, false
		}
		l.Complete = l.Complete && rounds == max
		p := 2 * t
		if i < out {
			p--
			l.SitOuts++
		}
		l.Sections = append(l.Sections, Section{Letter: string(rune('A' + i)), Tables: t, Pairs: p, Kind: kind})
		l.BoardsPerCopy = max2(l.BoardsPerCopy, t*bpr)
	}
	l.Played = len(sizes) * rounds
	return l, true
}

func sum(v []int) int {
	s := 0
	for _, x := range v {
		s += x
	}
	return s
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func max2(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// DealAdvice is a deal to make before the pairs are known: Copies copies of boards 1..BoardsPerCopy,
// and the layout each number of pairs would play with it.
type DealAdvice struct {
	Copies        int
	BoardsPerCopy int
	ByPairs       map[int]Layout // missing: can't seat that many within the boards asked for
	// Down is the fewest pairs it covers from the top of the range without a gap
	Down int
}

// Total is the boards to deal.
func (d DealAdvice) Total() int { return d.Copies * d.BoardsPerCopy }

// Advise lists the deals worth making for from..to pairs and about `boards` boards (±tol): the smallest
// that seats the expected maximum, then each larger one that covers more of the range from the top
// down, up to the one covering all of it. The TD weighs boards to deal against the risk of fewer pairs.
func Advise(from, to, boards, tol int) []DealAdvice {
	type deal struct{ c, n int }
	cands := map[deal]bool{}
	for p := from; p <= to; p++ {
		for _, l := range Layouts(p, boards, tol, 0, 0) {
			cands[deal{l.Copies, l.BoardsPerCopy}] = true
		}
	}
	var all []DealAdvice
	for d := range cands {
		a := DealAdvice{Copies: d.c, BoardsPerCopy: d.n, ByPairs: map[int]Layout{}, Down: to + 1}
		gap := false
		for p := to; p >= from; p-- {
			if ls := Layouts(p, boards, tol, d.n, d.c); len(ls) > 0 {
				a.ByPairs[p] = ls[0]
				if !gap {
					a.Down = p
				}
			} else {
				gap = true
			}
		}
		if a.Down <= to {
			all = append(all, a)
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Total() != all[j].Total() {
			return all[i].Total() < all[j].Total()
		}
		if all[i].Down != all[j].Down {
			return all[i].Down < all[j].Down
		}
		return all[i].Copies < all[j].Copies
	})
	// keep each deal that reaches further down than every smaller one
	var out []DealAdvice
	for _, a := range all {
		if len(out) == 0 || a.Down < out[len(out)-1].Down {
			out = append(out, a)
		}
	}
	return out
}

// Build makes the layout's combined movement: a Mitchell per section (skip Mitchell on an even number of
// tables), the odd pair out sitting out in section A.
func (l Layout) Build() (Movement, error) {
	var ms []Movement
	var letters []string
	for _, x := range l.Sections {
		m, err := NewMitchell(x.Tables, l.Rounds, MitchellOptions{Relay: x.Kind == RelayMitchell})
		if err != nil {
			return Movement{}, fmt.Errorf("section %s: %w", x.Letter, err)
		}
		if x.Pairs == m.Pairs-1 {
			m.Phantom = m.Pairs
		} else if x.Pairs != m.Pairs {
			return Movement{}, fmt.Errorf("section %s: %d pairs on %d tables", x.Letter, x.Pairs, x.Tables)
		}
		ms = append(ms, m)
		letters = append(letters, x.Letter)
	}
	if len(ms) == 1 {
		return ms[0], ms[0].Validate()
	}
	return Combine(letters, ms...)
}

// LayoutOf is the layout of `pairs` pairs in sections of these sizes (tables), for a session of
// rounds × bpr boards: what a director picked from Layouts, rebuilt from its numbers.
func LayoutOf(pairs int, sizes []int, rounds, bpr int) (Layout, error) {
	if len(sizes) == 0 || len(sizes) > 26 {
		return Layout{}, fmt.Errorf("1-26 sections")
	}
	l, ok := layout(pairs, sizes, rounds, bpr)
	if !ok {
		return Layout{}, fmt.Errorf("%d pairs don't fit sections of %v tables for %d rounds", pairs, sizes, rounds)
	}
	return l, nil
}
