package movement

import "fmt"

// WebMitchell and AppendixMitchell are the BBF TD's movements for fields a plain Mitchell doesn't fit
// (as Magic Contest offers them; checked row for row against its Bridgemate files for 16 tables and
// 13+5 tables over 13 rounds).
const (
	WebMitchell      Kind = "web-mitchell"
	AppendixMitchell Kind = "appendix-mitchell"
)

// scrambleRounds are the rounds a scrambled movement of 13 rounds switches NS and EW in, so the two
// lines are scored as one field (Magic Contest's pattern); other lengths are played unscrambled.
func scrambleRounds(rounds int) map[int]bool {
	if rounds != 13 {
		return nil
	}
	return map[int]bool{4: true, 7: true, 8: true}
}

// NewWebMitchell is a Web Mitchell on an even number of tables over `rounds` rounds on `rounds` sets
// (boards 1..rounds×bpr): NS pairs 1..T stay put, EW pairs T+1..2T move up a table each round; the
// first half of the tables plays set table+round-1 (boards move down a table), the second half
// T+1-table-round (boards move up), so table t and table T+1-t... mirrored tables play the same set:
// two copies of the boards. Scrambled (13 rounds), rounds 4, 7 and 8 switch NS and EW at every table but
// table 1 and table T/2+1, and the field is one.
func NewWebMitchell(tables, rounds int, scrambled bool) (Movement, error) {
	if tables < 6 || tables%2 == 1 || rounds < 1 || rounds > tables {
		return Movement{}, fmt.Errorf("a Web Mitchell is on an even number of tables, at most that many rounds")
	}
	sw := map[int]bool{}
	if scrambled {
		sw = scrambleRounds(rounds)
	}
	m := Movement{Kind: WebMitchell, Pairs: 2 * tables, Tables: tables, Rounds: rounds, Sets: rounds, Duplicated: true,
		TwoFields: len(sw) == 0}
	for r := 1; r <= rounds; r++ {
		for t := 1; t <= tables; t++ {
			var s int
			if t <= tables/2 {
				s = mod(t+r-2, rounds) + 1
			} else {
				s = mod(tables+1-t-r-1, rounds) + 1
			}
			ns, ew := t, tables+1+mod(t-r, tables)
			if sw[r] && t != 1 && t != tables/2+1 {
				ns, ew = ew, ns
			}
			m.Seats = append(m.Seats, Seat{Round: r, Table: t, NS: ns, EW: ew, Set: s})
		}
	}
	return m, m.Validate()
}

// NewAppendixMitchell is an Appendix Mitchell: a Mitchell of `main` tables (odd) over `main` rounds and
// an appendix of `app` tables. Pairs:
//   - 1..main travel NS, two positions a round round tables app+1..main and then the appendix;
//   - main+1..2·main are the Mitchell's EW, a table up each round round tables 1..main;
//   - 2·main+1..2·main+app sit NS at tables 1..app; the next app sit EW at the appendix tables.
//
// The Mitchell's boards move down a table; appendix table k plays the same set as table k (a second copy
// of those boards). Scrambled (13 rounds): rounds 4 and 7 switch every table, 8, 9 and 13 the appendix,
// 10-12 the Mitchell.
func NewAppendixMitchell(main, app int, scrambled bool) (Movement, error) {
	if main%2 == 0 || main < 5 || app < 1 || app >= main {
		return Movement{}, fmt.Errorf("an Appendix Mitchell needs an odd Mitchell and a smaller appendix")
	}
	rounds := main
	tables := main + app
	swAll, swApp, swMain := map[int]bool{}, map[int]bool{}, map[int]bool{}
	if scrambled && rounds == 13 {
		swAll = map[int]bool{4: true, 7: true}
		swApp = map[int]bool{8: true, 9: true, 13: true}
		swMain = map[int]bool{10: true, 11: true, 12: true}
	}
	m := Movement{Kind: AppendixMitchell, Pairs: 2 * tables, Tables: tables, Rounds: rounds, Sets: main, Duplicated: true,
		TwoFields: len(swAll) == 0}
	for r := 0; r < rounds; r++ {
		for t := 1; t <= tables; t++ {
			var ns, ew, s int
			if t <= main {
				s = mod(t-1+r, main) + 1
				ew = main + 1 + mod(t-1-r, main)
				if t <= app {
					ns = 2*main + t
				} else {
					ns = 1 + mod(t-app-1+app-2*r, main)
				}
			} else {
				k := t - main
				s = mod(k-1+r, main) + 1
				ns = 1 + mod(main-app+k-1+app-2*r, main)
				ew = 2*main + app + k
			}
			if swAll[r+1] || swApp[r+1] && t > main || swMain[r+1] && t <= main {
				ns, ew = ew, ns
			}
			m.Seats = append(m.Seats, Seat{Round: r + 1, Table: t, NS: ns, EW: ew, Set: s})
		}
	}
	return m, m.Validate()
}
