package movement

import "fmt"

// MitchellOptions tune a Mitchell.
type MitchellOptions struct {
	// ArrowSwitch lists rounds in which every table turns its boards: the stationary pair plays the
	// EW cards. Switching about a quarter of the rounds lets the two lines be ranked as one field.
	ArrowSwitch []int
}

// NewMitchell builds a Mitchell for `tables` tables over `rounds` rounds. NS pairs 1..T stay put,
// EW pairs T+1..2T move up a table each round and the boards move down one. With an odd number of
// tables every round can be played (rounds <= T); with an even number the EW pairs skip a table after
// half the rounds (a skip Mitchell), which allows at most T-1 rounds without a relay.
func NewMitchell(tables, rounds int, opt MitchellOptions) (Movement, error) {
	if tables < 3 {
		return Movement{}, fmt.Errorf("a Mitchell needs at least 3 tables")
	}
	maxRounds := tables
	kind := Mitchell
	if tables%2 == 0 {
		maxRounds, kind = tables-1, SkipMitchell
	}
	if rounds < 1 || rounds > maxRounds {
		return Movement{}, fmt.Errorf("a %s on %d tables plays 1..%d rounds, not %d", kind, tables, maxRounds, rounds)
	}
	switched := map[int]bool{}
	for _, r := range opt.ArrowSwitch {
		switched[r] = true
	}
	m := Movement{Kind: kind, Pairs: 2 * tables, Tables: tables, Rounds: rounds, Sets: tables, TwoFields: len(opt.ArrowSwitch) == 0}
	for r := 0; r < rounds; r++ {
		shift := r
		if kind == SkipMitchell && r >= tables/2 {
			shift++
		}
		for t := 0; t < tables; t++ {
			ns := t + 1
			ew := tables + mod(t-shift, tables) + 1
			if switched[r+1] {
				ns, ew = ew, ns
			}
			m.Seats = append(m.Seats, Seat{Round: r + 1, Table: t + 1, NS: ns, EW: ew, Set: mod(t+r, tables) + 1})
		}
	}
	return m, m.Validate()
}

func mod(a, n int) int { return ((a % n) + n) % n }
