package movement

import "sort"

// Option is one way to run a pairs session.
type Option struct {
	Kind           Kind
	Tables, Rounds int
	BoardsPerRound int
	Boards         int
	// Complete: everybody meets everybody they can (every round of the Mitchell or Howell).
	Complete bool
	Note     string
}

// Options lists the movements that fit `pairs` pairs and a session of minBoards..maxBoards boards with
// `copies` copies of the boards, best first. The federation's preference: a Mitchell when there are
// enough tables (complete if the number of tables is odd), a Howell (complete, or reduced) otherwise.
// Everything is playable with one copy; a small Howell (4-7 pairs) then shares boards between tables,
// and with a copy for every table it gives each table its own instead.
func Options(pairs, minBoards, maxBoards, copies int) []Option {
	var out []Option
	tables := (pairs + 1) / 2
	for bpr := 2; bpr <= 5; bpr++ {
		if tables >= 5 {
			maxR := tables
			kind := Mitchell
			if tables%2 == 0 {
				maxR, kind = tables-1, SkipMitchell
			}
			for r := maxR; r >= 1; r-- {
				if b := r * bpr; b >= minBoards && b <= maxBoards {
					out = append(out, Option{Kind: kind, Tables: tables, Rounds: r, BoardsPerRound: bpr, Boards: b, Complete: r == maxR})
				}
			}
		}
		n := pairs + pairs%2
		for r := n - 1; r >= 1; r-- {
			if b := r * bpr; b >= minBoards && b <= maxBoards {
				kind := Howell
				if n < 8 {
					kind = HowellShared
					if copies >= n/2 {
						kind = HowellDuplicated
					}
				}
				out = append(out, Option{Kind: kind, Tables: n / 2, Rounds: r, BoardsPerRound: bpr, Boards: b, Complete: r == n-1})
			}
		}
	}
	rank := func(o Option) int {
		s := 0
		if !o.Complete {
			s += 10
		}
		switch o.Kind {
		case Mitchell:
		case SkipMitchell:
			s += 1
		case Howell, HowellDuplicated:
			s += 2
		default:
			s += 3
		}
		if o.BoardsPerRound < 3 { // too much moving
			s += 4
		}
		return s
	}
	sort.SliceStable(out, func(i, j int) bool { return rank(out[i]) < rank(out[j]) })
	return out
}
