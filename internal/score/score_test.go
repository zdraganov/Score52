package score

import (
	"math"
	"testing"
)

func TestScore(t *testing.T) {
	cases := []struct {
		c      string
		vul    bool
		tricks int
		want   int
	}{
		{"PASS", false, 0, 0},
		{"1C", false, 7, 70},
		{"1NT", false, 7, 90},
		{"2S", false, 9, 140},
		{"3NT", false, 9, 400},
		{"3NT", true, 9, 600},
		{"4H", true, 11, 650},
		{"5D", false, 11, 400},
		{"6S", true, 12, 1430},
		{"7NT", true, 13, 2220},
		{"1NTX", false, 7, 180},
		{"1CX", false, 7, 140},
		{"2HX", false, 8, 470},  // doubled into game
		{"1CXX", false, 7, 230}, // 80 + 50 + 100
		{"1CX", true, 8, 340},   // 40 + 50 + 50 + 200
		{"4S", false, 9, -50},
		{"4S", true, 7, -300},
		{"4SX", false, 9, -100},
		{"4SX", false, 7, -500},
		{"4SX", false, 6, -800},
		{"4SX", true, 9, -200},
		{"4SX", true, 7, -800},
		{"4SXX", true, 7, -1600},
		{"7NTX", false, 0, -3500},
	}
	for _, tc := range cases {
		c, err := ParseContract(tc.c)
		if err != nil {
			t.Fatal(err)
		}
		if got := Score(c, tc.vul, tc.tricks); got != tc.want {
			t.Errorf("%s vul=%v %d tricks: got %d want %d", tc.c, tc.vul, tc.tricks, got, tc.want)
		}
	}
}

func TestIMP(t *testing.T) {
	for d, want := range map[int]int{0: 0, 10: 0, 20: 1, 40: 1, 50: 2, 420: 9, 430: 10, 620: 12, -620: -12, 3990: 23, 5000: 24} {
		if got := IMP(d); got != want {
			t.Errorf("IMP(%d) = %d want %d", d, got, want)
		}
	}
}

func TestMatchpoints(t *testing.T) {
	r := []Result{
		{NS: 1, EW: 11, Played: true, Score: 420},
		{NS: 2, EW: 12, Played: true, Score: 420},
		{NS: 3, EW: 13, Played: true, Score: 450},
		{NS: 4, EW: 14, Played: true, Score: -50},
	}
	mp := Matchpoints(r, 4)
	want := []float64{3, 3, 6, 0} // top 6
	for i := range mp {
		if mp[i].MPNS != want[i] {
			t.Errorf("pair %d: %v want %v", mp[i].NS, mp[i].MPNS, want[i])
		}
	}
	if mp[2].PctNS != 100 || mp[3].PctEW != 100 {
		t.Errorf("pct: %+v", mp)
	}

	// Played 3 times out of 4 with an adjusted 60/40: Neuberg on the others
	r[3] = Result{NS: 4, EW: 14, AdjNS: 60, AdjEW: 40}
	mp = Matchpoints(r, 4)
	// raw 1,1,4 on top 4 -> (mp+1)*4/3-1 = 1.667, 1.667, 5.667 on top 6
	if math.Abs(mp[0].MPNS-5.0/3) > 1e-9 || math.Abs(mp[2].MPNS-17.0/3) > 1e-9 {
		t.Errorf("neuberg: %+v", mp)
	}
	if mp[3].PctNS != 60 || mp[3].PctEW != 40 {
		t.Errorf("adjusted: %+v", mp[3])
	}
}

func TestButler(t *testing.T) {
	r := []Result{{Played: true, Score: 420}, {Played: true, Score: 450}, {Played: true, Score: 420}, {Played: true, Score: -50}, {Played: true, Score: 1430}}
	out, datum := Butler(r, 0.1)
	if datum != 430 { // drops -50 and 1430: mean 430
		t.Fatalf("datum %d", datum)
	}
	if out[1].IMPNS != 1 || out[4].IMPNS != 14 {
		t.Errorf("%+v", out)
	}
}

func TestWBF20(t *testing.T) {
	for _, tc := range []struct {
		imps   float64
		boards int
		win    float64
	}{{0, 16, 10}, {1, 16, 10.31}, {60, 16, 20}, {100, 16, 20}, {30, 16, 16.73}} {
		w, l := WBF20{}.VP(tc.imps, tc.boards)
		if w != tc.win || math.Abs(w+l-20) > 1e-9 {
			t.Errorf("%v imps / %d boards: %v-%v want %v", tc.imps, tc.boards, w, l, tc.win)
		}
	}
	if a, b := MatchVP(WBF20{}, -1, 16); a != 9.69 || b != 10.31 {
		t.Errorf("loser first: %v %v", a, b)
	}
}

func TestVulnerability(t *testing.T) {
	if BoardVulnerability(1) != None || BoardVulnerability(2) != NS || BoardVulnerability(16) != EW || BoardVulnerability(17) != None {
		t.Error("vulnerability cycle")
	}
}
