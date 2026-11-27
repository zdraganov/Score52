package movement

import (
	"fmt"
	"sort"
	"strings"
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
	// Duplicates: sets to copy once more for relay-and-stand sections (the first and last table share one)
	Duplicates int
	SitOuts    int    // sections with a pair sitting out each round
	Type       string // TypeSections, TypeHowell, TypeHowells, TypeMixed
	// SideSets: sets that need a second copy on an empty table, for the tables sharing a set in a round
	// (the BBF TD's way: the sharing table takes its set from there). An estimate here; the built
	// movement's SideSets is exact.
	SideSets int
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

// Layout types: Mitchell sections, one Howell, or a Mitchell section and a Howell section.
const (
	TypeSections = "sections"
	TypeHowell   = "howell"
	TypeHowells  = "howells"
	TypeMixed    = "mixed"
)

// Layouts lists the ways to seat `pairs` pairs for `boards` boards (±tol) in an event of `sessions`
// sessions, best first. With an even number of pairs, layouts with a pair sitting out come last. The federation's TD's preference decides the kind: a single session plays
// Mitchell sections when they fit, else one reduced Howell (one field); an event of 2-3 sessions plays one
// big Mitchell section and a smaller Howell section, so the next session can seat pairs against new
// opponents. Within a kind: boards closest to the target, each board played the most times across the
// field, fewer sit-outs, fewer duplicated sets, fewer sections, fewer moves. maxDeal > 0 keeps to boards
// per copy within what was dealt, and maxCopies > 0 to that many copies.
func Layouts(pairs, boards, tol, maxDeal, maxCopies, sessions int) []Layout {
	need := (pairs + 1) / 2
	var out []Layout
	keep := func(l Layout, ok bool) {
		if ok && l.BoardsPerCopy <= MaxBoard && (maxDeal == 0 || l.BoardsPerCopy <= maxDeal) && (maxCopies == 0 || l.Copies <= maxCopies) {
			out = append(out, l)
		}
	}
	for bpr := 2; bpr <= 5; bpr++ {
		for r := (boards - tol + bpr - 1) / bpr; r*bpr <= boards+tol; r++ {
			// Mitchell sections, up to one pair sitting out in each
			for k := 1; k <= need/MinSectionTables+1 && k <= 26; k++ {
				for tables := need; 2*tables-pairs <= k; tables++ {
					sizes := split(tables, k)
					if sizes[k-1] < MinSectionTables && k > 1 || sizes[0] > MaxSectionTables {
						continue
					}
					if k == 1 && sizes[0] != r && sizes[0] != r+1-r%2 { // the TD's: one section is a complete Mitchell
						continue
					}
					var secs []Section
					left := 2*tables - pairs
					for _, t := range sizes {
						p := 2 * t
						if left > 0 {
							p, left = p-1, left-1
						}
						secs = append(secs, Section{Tables: t, Pairs: p, Kind: Mitchell})
					}
					keep(assemble(secs, r, bpr))
				}
			}
			// one Howell for the whole field
			keep(assemble([]Section{{Tables: need, Pairs: pairs, Kind: Howell}}, r, bpr))
			// two Howell sections, the field halved, or nearly so with an even number in each (nobody sits out)
			for _, a := range []int{pairs - pairs/2, pairs/2 + pairs/2%2} {
				if b := pairs - a; b >= r+1 && a >= b {
					keep(assemble([]Section{{Tables: (a + 1) / 2, Pairs: a, Kind: Howell}, {Tables: (b + 1) / 2, Pairs: b, Kind: Howell}}, r, bpr))
				}
			}
			// a complete Mitchell section (the TD's: as many tables as rounds) and a Howell section for the rest
			for t := max(r, 3); t <= r+1-r%2; t++ {
				for _, mp := range []int{2 * t} { // a full Mitchell: with a pair sitting out, two Howells instead (TD)
					if rest := pairs - mp; rest >= 4 && rest < mp { // the Mitchell is the bigger one
						keep(assemble([]Section{{Tables: t, Pairs: mp, Kind: Mitchell}, {Tables: (rest + 1) / 2, Pairs: rest, Kind: Howell}}, r, bpr))
					}
				}
			}
		}
	}
	rank := func(l Layout) int { // the TD's order of the kinds
		order := map[string]int{TypeSections: 0, TypeMixed: 1, TypeHowells: 2, TypeHowell: 3}
		if sessions >= 2 && l.Type == TypeMixed && l.SideSets == 0 {
			return -1 // several sessions: a Mitchell and a Howell, for new opponents next time
		}
		return order[l.Type]
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if da, db := abs(a.Boards-boards), abs(b.Boards-boards); da != db {
			return da < db
		}
		// the TD's: with an even number of pairs nobody sits out, whatever the kind
		if sa, sb := pairs%2 == 0 && a.SitOuts > 0, pairs%2 == 0 && b.SitOuts > 0; sa != sb {
			return sb
		}
		if ra, rb := rank(a), rank(b); ra != rb {
			return ra < rb
		}
		if a.SitOuts != b.SitOuts {
			return a.SitOuts < b.SitOuts
		}
		if a.SideSets != b.SideSets {
			return a.SideSets < b.SideSets
		}
		if a.Duplicates != b.Duplicates {
			return a.Duplicates < b.Duplicates
		}
		if a.Type == TypeMixed && b.Type == TypeMixed && a.Sections[0].Pairs != b.Sections[0].Pairs {
			return a.Sections[0].Pairs > b.Sections[0].Pairs // the TD's: a big Mitchell, a small Howell
		}
		if a.Played != b.Played {
			return a.Played > b.Played
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

// assemble makes a layout of sections (Kind Mitchell for any Mitchell, Howell) for rounds × bpr boards:
// a Mitchell's variant (plain, skip, relay and stand) from its tables and rounds, a Howell's sets from
// the boards (up to MaxBoard; sharing where it has more tables than sets), the boards to deal, the
// duplicated sets, the sit-outs, and how many times a board is played across the field.
func assemble(secs []Section, rounds, bpr int) (Layout, bool) {
	l := Layout{Rounds: rounds, BoardsPerRound: bpr, Boards: rounds * bpr, Copies: len(secs), Complete: true, Type: TypeSections}
	maxSets := MaxBoard / bpr
	hasH, hasM := false, false
	for i, x := range secs {
		x.Letter = string(rune('A' + i))
		sets := x.Tables
		if x.Kind == Howell {
			hasH = true
			n := x.Pairs + x.Pairs%2
			if x.Pairs < 4 || rounds > n-1 {
				return l, false
			}
			x.Tables = n / 2
			sets = max(min(maxSets, x.Tables), rounds)
			l.Duplicates += max(0, x.Tables-sets)
			l.SideSets += min(sets, max(0, x.Tables-sets)*rounds) // a shared set a table and round, at most every set
			l.Complete = l.Complete && rounds == n-1
			l.Played += rounds * x.Tables / sets
			if x.Pairs%2 == 1 {
				l.SitOuts++
			}
		} else {
			hasM = true
			if x.Pairs != 2*x.Tables && x.Pairs != 2*x.Tables-1 {
				return l, false
			}
			// odd tables: a Mitchell, every round; even: skip Mitchell up to T-1 rounds, relay and stand for all T
			max := x.Tables
			switch {
			case x.Tables%2 == 1:
				x.Kind = Mitchell
			case rounds <= x.Tables-1:
				x.Kind, max = SkipMitchell, x.Tables-1
			default:
				x.Kind = RelayMitchell
				l.Duplicates++
				l.SideSets += rounds // the shared set changes every round
			}
			if x.Tables < 3 || rounds > max {
				return l, false
			}
			l.Complete = l.Complete && rounds == max
			l.Played += rounds
			if x.Pairs < 2*x.Tables {
				l.SitOuts++
			}
		}
		l.BoardsPerCopy = max2(l.BoardsPerCopy, sets*bpr)
		l.Sections = append(l.Sections, x)
	}
	switch {
	case hasH && hasM:
		l.Type = TypeMixed
	case hasH && len(l.Sections) > 1:
		l.Type = TypeHowells
	case hasH:
		l.Type = TypeHowell
	}
	return l, true
}

// Spec is a layout's sections as text, to rebuild it from a form: "M13/26,H7/14" (kind, tables, pairs).
func (l Layout) Spec() string {
	var out []string
	for _, x := range l.Sections {
		k := "M"
		if x.Kind == Howell {
			k = "H"
		}
		out = append(out, fmt.Sprintf("%s%d/%d", k, x.Tables, x.Pairs))
	}
	return strings.Join(out, ",")
}

// LayoutOf rebuilds the layout of a Spec for a session of rounds × bpr boards.
func LayoutOf(spec string, rounds, bpr int) (Layout, error) {
	var secs []Section
	for _, f := range strings.Split(spec, ",") {
		var k string
		var t, p int
		if len(f) < 4 {
			return Layout{}, fmt.Errorf("bad section %q", f)
		}
		k, f = f[:1], f[1:]
		if _, err := fmt.Sscanf(f, "%d/%d", &t, &p); err != nil || (k != "M" && k != "H") {
			return Layout{}, fmt.Errorf("bad section %q", k+f)
		}
		kind := Mitchell
		if k == "H" {
			kind = Howell
		}
		secs = append(secs, Section{Tables: t, Pairs: p, Kind: kind})
	}
	if len(secs) == 0 || len(secs) > 26 {
		return Layout{}, fmt.Errorf("1-26 sections")
	}
	l, ok := assemble(secs, rounds, bpr)
	if !ok {
		return Layout{}, fmt.Errorf("the sections %s don't play %d rounds", spec, rounds)
	}
	return l, nil
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

// DealAdvice is what to deal before the pairs are known: Copies boxes of boards 1..MaxBoard (the
// federation deals whole boxes), and Duplicates: the most tables that share a set in a round. ByPairs is
// the layout each number of pairs would play; a missing number isn't covered.
type DealAdvice struct {
	Copies        int
	BoardsPerCopy int // MaxBoard: a whole box
	Duplicates    int // the most tables sharing a set in a round, for any number of pairs covered
	BoardsPerSet  int // to count the duplicates in boards
	ByPairs       map[int]Layout
	// Down is the fewest pairs it covers from the top of the range without a gap
	Down int
}

// Total is the boards in the boxes.
func (d DealAdvice) Total() int { return d.Copies * d.BoardsPerCopy }

// Advise lists the deals worth making for from..to pairs and `boards` boards (±tol) in an event of
// `sessions` sessions. For every number of pairs it takes the layout the TD would pick (Layouts' first)
// and what that needs: boxes of 1..MaxBoard and tables sharing a set. The first deal is what the expected
// maximum needs, each next one what it takes to go further down the range, up to all of it.
func Advise(from, to, boards, tol, sessions int) []DealAdvice {
	var out []DealAdvice
	cur := DealAdvice{BoardsPerCopy: MaxBoard, ByPairs: map[int]Layout{}, Down: to + 1}
	for p := to; p >= from; p-- {
		ls := Layouts(p, boards, tol, 0, 0, sessions)
		if len(ls) == 0 {
			break // a gap: nothing below it is covered without one
		}
		l := ls[0]
		if l.Copies > cur.Copies || l.Duplicates > cur.Duplicates {
			if cur.Copies > 0 {
				out = append(out, cur)
			}
			next := DealAdvice{Copies: max(cur.Copies, l.Copies), BoardsPerCopy: MaxBoard,
				Duplicates: max(cur.Duplicates, l.Duplicates), BoardsPerSet: l.BoardsPerRound, ByPairs: map[int]Layout{}}
			for q, x := range cur.ByPairs {
				next.ByPairs[q] = x
			}
			cur = next
		}
		cur.ByPairs[p] = l
		cur.Down = p
		if cur.BoardsPerSet == 0 {
			cur.BoardsPerSet = l.BoardsPerRound
		}
	}
	if cur.Copies > 0 {
		out = append(out, cur)
	}
	return out
}

// tables), the odd pair out sitting out in section A.
func (l Layout) Build() (Movement, error) {
	var ms []Movement
	var letters []string
	for _, x := range l.Sections {
		if x.Kind == Howell {
			m, err := NewReducedHowell(x.Pairs, l.Rounds, MaxBoard/l.BoardsPerRound)
			if err != nil {
				return Movement{}, fmt.Errorf("section %s: %w", x.Letter, err)
			}
			ms = append(ms, m)
			letters = append(letters, x.Letter)
			continue
		}
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
