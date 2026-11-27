package teams

import (
	"fmt"
	"math/rand/v2"
	"testing"
)

func field(n int) []Team {
	var ts []Team
	for i := 1; i <= n; i++ {
		ts = append(ts, Team{ID: TeamID(i), Name: fmt.Sprintf("T%d", i), Seed: i})
	}
	return ts
}

// play pairs a round and fills in random results, stronger seeds doing a little better.
func play(t *testing.T, e *Event, rng *rand.Rand) []Match {
	t.Helper()
	ms, err := e.PairRound(12)
	if err != nil {
		t.Fatalf("round %d: %v", e.Round+1, err)
	}
	for _, m := range ms {
		if m.Bye() {
			continue
		}
		h := rng.Float64()*40 + float64(40-e.Teams[m.Home].Seed)/4
		a := rng.Float64()*40 + float64(40-e.Teams[m.Away].Seed)/4
		h, a = float64(int(h)), float64(int(a))
		if h == a {
			h++
		}
		if err := e.Record(m.Round, m.Home, m.Away, h, a); err != nil {
			t.Fatal(err)
		}
	}
	return ms
}

func noRepeats(t *testing.T, e *Event, upTo int) {
	t.Helper()
	seen := map[[2]TeamID]int{}
	for _, m := range e.Matches {
		if m.Round <= upTo && !m.Bye() {
			k := tkey(m.Home, m.Away)
			if r, ok := seen[k]; ok {
				t.Errorf("%v met in rounds %d and %d", k, r, m.Round)
			}
			seen[k] = m.Round
		}
	}
}

// 7 rounds Swiss, 2 rounds Danish, then A (top 8) knockout and B Swiss.
func TestSwissDanishKnockout(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	e := mustNew(t, field(31), Swiss)
	for r := 0; r < 7; r++ {
		play(t, e, rng)
	}
	noRepeats(t, e, 7)
	if err := e.SetFormat("", Danish); err != nil {
		t.Fatal(err)
	}
	for r := 0; r < 2; r++ {
		ms := play(t, e, rng)
		st := e.Standings("")
		_ = st
		if len(ms) != 16 {
			t.Fatalf("danish round: %d matches", len(ms))
		}
	}
	top := e.Standings("")[:8]
	if err := e.Split("", []GroupSpec{{Name: "A", Count: 8, Format: Knockout}, {Name: "B", Format: Swiss, Carry: Carry{Kind: CarryAll}}}); err != nil {
		t.Fatal(err)
	}
	ko := play(t, e, rng)
	if ko[0].Home != top[0].Team || ko[0].Away != top[7].Team || ko[1].Home != top[3].Team {
		t.Errorf("bracket: %+v", ko[:2])
	}
	play(t, e, rng) // SF
	final := play(t, e, rng)
	n := 0
	for _, m := range final {
		if m.Group == "A" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("final: %d A matches", n)
	}
	if got := len(e.Members("B")); got != 23 {
		t.Errorf("B has %d teams", got)
	}
	b := e.Standings("B")
	if b[0].Carry == 0 || b[0].Played != 3 {
		t.Errorf("B carry/played: %+v", b[0])
	}
}

// Close A with 10 teams after round 8; after round 10 the last two of A and the first two of B swap.
func TestCloseAndExchange(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	e := mustNew(t, field(24), Swiss)
	for r := 0; r < 8; r++ {
		play(t, e, rng)
	}
	noRepeats(t, e, 8)
	if err := e.Split("", []GroupSpec{{Name: "A", Count: 10, Format: Swiss, Carry: Carry{Kind: CarryAll}}, {Name: "B", Format: Swiss, Carry: Carry{Kind: CarryAll}}}); err != nil {
		t.Fatal(err)
	}
	for r := 0; r < 2; r++ {
		for _, m := range play(t, e, rng) {
			ga, gb := e.groupOf(m.Home), e.groupOf(m.Away)
			if m.Away != 0 && ga != gb {
				t.Errorf("cross-group match %v", m)
			}
		}
	}
	a, b := e.Standings("A"), e.Standings("B")
	vpBefore := map[TeamID]float64{a[0].Team: a[0].VP, a[8].Team: a[8].VP, b[0].Team: b[0].VP}
	down := []TeamID{a[8].Team, a[9].Team}
	up := []TeamID{b[0].Team, b[1].Team}
	if err := e.Exchange("A", "B", 2, Carry{Kind: CarryAll, Fraction: 0.5}); err != nil {
		t.Fatal(err)
	}
	for _, id := range down {
		if e.groupOf(id) != "B" {
			t.Errorf("team %d not relegated", id)
		}
	}
	for _, id := range up {
		if e.groupOf(id) != "A" {
			t.Errorf("team %d not promoted", id)
		}
	}
	// movers bring half their score, the teams that stay keep theirs
	for _, st := range append(e.Standings("A"), e.Standings("B")...) {
		if v, ok := vpBefore[st.Team]; ok {
			want := v
			if st.Team != a[0].Team {
				want = round2(v / 2)
			}
			if st.VP != want {
				t.Errorf("team %d: %v after the exchange, want %v", st.Team, st.VP, want)
			}
		}
	}
	play(t, e, rng)
	if len(e.Members("A")) != 10 {
		t.Error("A size")
	}
}

func TestHeadToHeadCarry(t *testing.T) {
	rng := rand.New(rand.NewPCG(5, 6))
	e := mustNew(t, field(8), RoundRobin)
	for r := 0; r < 7; r++ {
		play(t, e, rng)
	}
	noRepeats(t, e, 7)
	if err := e.Split("", []GroupSpec{{Name: "A", Count: 4, Format: RoundRobin, Carry: Carry{Kind: CarryHeadToHead}}, {Name: "B", Format: RoundRobin, Carry: Carry{Kind: CarryNone}}}); err != nil {
		t.Fatal(err)
	}
	for _, s := range e.Standings("A") {
		var want float64
		for _, m := range e.Matches {
			if m.Home == s.Team && e.groupOf(m.Away) == "A" {
				want += m.VPHome
			} else if m.Away == s.Team && e.groupOf(m.Home) == "A" {
				want += m.VPAway
			}
		}
		if round2(want) != s.Carry {
			t.Errorf("team %d carry %v want %v", s.Team, s.Carry, want)
		}
	}
	for _, s := range e.Standings("B") {
		if s.VP != 0 {
			t.Errorf("B carry none: %+v", s)
		}
	}
}

func TestBracket(t *testing.T) {
	if fmt.Sprint(bracket(8)) != "[1 8 4 5 2 7 3 6]" {
		t.Error(bracket(8))
	}
}

func (e *Event) groupOf(id TeamID) string {
	for _, g := range e.Groups {
		if g.members[id] != nil && !e.eliminated(g, id) {
			return g.Name
		}
	}
	return "?"
}

func mustNew(t *testing.T, teams []Team, f Format) *Event {
	t.Helper()
	e, err := New(teams, f, "wbf20", 12)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestKnockoutCarryAndPlayoff(t *testing.T) {
	e := mustNew(t, field(4), Knockout)
	ms, err := e.PairRound(16) // 1-4, 2-3
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SetCarry(1, ms[0].Home, ms[0].Away, 3, 0); err != nil {
		t.Fatal(err)
	}
	e.Record(1, ms[0].Home, ms[0].Away, 40, 43) // level after the carry of 3
	e.Record(1, ms[1].Home, ms[1].Away, 30, 20)
	if _, err := e.PairRound(16); err == nil {
		t.Fatal("paired past a tied match")
	}
	if err := e.AddPlayoff(1, ms[0].Home, ms[0].Away, 1, 0, 0); err != nil {
		t.Fatal(err)
	}
	e.AddPlayoff(1, ms[0].Home, ms[0].Away, 1, 0, 5) // away wins on the second extra board
	final, err := e.PairRound(16)
	if err != nil {
		t.Fatal(err)
	}
	if final[0].Home != ms[0].Away || final[0].Away != ms[1].Home {
		t.Errorf("final %+v", final[0])
	}
}
