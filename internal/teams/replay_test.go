package teams

import (
	"encoding/json"
	"math/rand/v2"
	"reflect"
	"testing"
)

func roundTrip(t *testing.T, e *Event) *Event {
	t.Helper()
	b, err := json.Marshal(e.Log)
	if err != nil {
		t.Fatal(err)
	}
	var log []Op
	if err := json.Unmarshal(b, &log); err != nil {
		t.Fatal(err)
	}
	r, err := Replay(log)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func sameState(t *testing.T, a, b *Event) {
	t.Helper()
	if !reflect.DeepEqual(a.Matches, b.Matches) || a.Round != b.Round {
		t.Fatal("matches differ after replay")
	}
	for _, g := range a.Groups {
		if !reflect.DeepEqual(a.Standings(g.Name), b.Standings(g.Name)) {
			t.Errorf("group %q: standings differ after replay", g.Name)
		}
	}
}

// The whole Swiss -> close A -> exchange -> knockout path survives saving and loading, and the replayed
// event pairs the next round exactly as the live one does.
func TestReplay(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 8))
	e := mustNew(t, field(20), Swiss)
	for r := 0; r < 6; r++ {
		play(t, e, rng)
	}
	if err := e.Split("", []GroupSpec{{Name: "A", Count: 8, Format: RoundRobin, Carry: Carry{Kind: CarryHeadToHead}}, {Name: "B", Format: Swiss, Carry: Carry{Kind: CarryAll, Fraction: 0.5}}}); err != nil {
		t.Fatal(err)
	}
	play(t, e, rng)
	play(t, e, rng)
	if err := e.Exchange("A", "B", 2, Carry{Kind: CarryAll, Fraction: 0.8}); err != nil {
		t.Fatal(err)
	}
	play(t, e, rng)
	if err := e.Split("A", []GroupSpec{{Name: "A", Count: 4, Format: Knockout}, {Name: "A2", Format: Swiss, Carry: Carry{Kind: CarryAll}}}); err != nil {
		t.Fatal(err)
	}
	play(t, e, rng)

	r := roundTrip(t, e)
	sameState(t, e, r)
	m1, err1 := e.PairRound(12)
	m2, err2 := r.PairRound(12)
	if err1 != nil || err2 != nil || !reflect.DeepEqual(m1, m2) {
		t.Fatalf("next round differs: %v %v\n%v\n%v", err1, err2, m1, m2)
	}
}

// A round-2 score corrected after the split reaches the group's carry-over, but does not move a team
// to another group.
func TestCorrectionReachesCarry(t *testing.T) {
	rng := rand.New(rand.NewPCG(9, 10))
	e := mustNew(t, field(10), Swiss)
	for r := 0; r < 4; r++ {
		play(t, e, rng)
	}
	if err := e.Split("", []GroupSpec{{Name: "A", Count: 4, Format: Swiss, Carry: Carry{Kind: CarryAll}}, {Name: "B", Format: Swiss, Carry: Carry{Kind: CarryAll}}}); err != nil {
		t.Fatal(err)
	}
	play(t, e, rng)
	var m Match
	for _, x := range e.Matches {
		if x.Round == 2 && e.groupOf(x.Home) == "B" {
			m = x
			break
		}
	}
	before := carryOfTeam(e, "B", m.Home)
	if err := e.Record(2, m.Home, m.Away, m.IMPsHome+40, m.IMPsAway); err != nil {
		t.Fatal(err)
	}
	after := carryOfTeam(e, "B", m.Home)
	if after <= before {
		t.Errorf("carry %v -> %v after a better round-2 score", before, after)
	}
	if e.groupOf(m.Home) != "B" {
		t.Error("the correction moved a team between groups")
	}
	sameState(t, e, roundTrip(t, e))
}

func carryOfTeam(e *Event, g string, id TeamID) float64 {
	for _, s := range e.Standings(g) {
		if s.Team == id {
			return s.Carry
		}
	}
	return -1
}

func TestReplayRejectsBadLog(t *testing.T) {
	if _, err := Replay([]Op{{Kind: OpRecord}}); err == nil {
		t.Error("log without start accepted")
	}
	e := mustNew(t, field(4), Swiss)
	log := append(e.Log, Op{Kind: OpSplit, Group: "", Groups: []SplitGroup{{Name: "A", Teams: []TeamID{1, 2}}}})
	if _, err := Replay(log); err == nil {
		t.Error("split leaving teams out accepted")
	}
}
