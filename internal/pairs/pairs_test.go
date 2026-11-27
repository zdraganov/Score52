package pairs

import (
	"testing"

	"bridge.bg/scorer/internal/movement"
)

// Two sessions of a 9-table Mitchell: the second should have no repeated opponents and mix the lines.
func TestSecondSessionMixes(t *testing.T) {
	m, err := movement.NewMitchell(9, 9, movement.MitchellOptions{})
	if err != nil {
		t.Fatal(err)
	}
	first := make([]int, 18)
	for i := range first {
		first[i] = i + 1
	}
	h := NewHistory()
	h.Add(m, first)
	second := Seat(m, first, h, DefaultWeights, 1)
	if got := sortedCopy(second); got[0] != 1 || got[17] != 18 {
		t.Fatalf("not a permutation: %v", second)
	}
	// A complete Mitchell meets every pair of the other line. If the new NS line holds a pairs of the old
	// NS line, a^2 + (9-a)^2 meetings repeat, so 41 (a = 4 or 5) is the least possible, against 81 for
	// seating everyone as before.
	if r := Repeats(m, second, h); r != 41 {
		t.Errorf("%d repeated meetings, the optimum is 41", r)
	}
	ns := map[int]bool{}
	for _, p := range second[:9] {
		ns[p] = true
	}
	same := 0
	for p := 1; p <= 9; p++ {
		if ns[p] {
			same++
		}
	}
	if same != 4 && same != 5 {
		t.Errorf("%d of the first NS line are NS together again", same)
	}
}

func TestHowellSeatingAvoidsRepeats(t *testing.T) {
	// 14 pairs, two reduced Howells of 6 rounds: 6+6 < 13 opponents, so no repeats are needed
	m, err := movement.NewHowell(14, 6, false)
	if err != nil {
		t.Fatal(err)
	}
	first := make([]int, 14)
	for i := range first {
		first[i] = i + 1
	}
	h := NewHistory()
	h.Add(m, first)
	second := Seat(m, first, h, DefaultWeights, 7)
	if r := Repeats(m, second, h); r != 0 {
		t.Errorf("%d repeats", r)
	}
}

func TestQualifyWithCarry(t *testing.T) {
	q := Totals(MP, [][]SessionScore{
		{{Pair: 1, Score: 120, Top: 200}, {Pair: 2, Score: 110, Top: 200}, {Pair: 3, Score: 90, Top: 200}, {Pair: 4, Score: 80, Top: 200}},
	}, nil, Carry{})
	finals, carry, err := Qualify(q, []Final{{Name: "A", Count: 2}, {Name: "B"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(finals["A"]) != 2 || finals["A"][0] != 1 || len(finals["B"]) != 2 {
		t.Fatalf("%v", finals)
	}
	// final A: pair 2 wins the final 60-40 and the 25% carry-over (60 vs 55) does not save pair 1
	f := Totals(MP, [][]SessionScore{{{Pair: 1, Score: 40, Top: 100}, {Pair: 2, Score: 60, Top: 100}}}, carry, Carry{Fraction: 0.25})
	if f[0].Pair != 2 || f[0].Total != 0.25*55+0.75*60 {
		t.Errorf("%+v", f)
	}
}
