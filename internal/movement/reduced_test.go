package movement

import "testing"

// 40 pairs, 13 rounds of 2 boards on boards 1-32: a reduced Howell on 16 sets, 20 tables, so 4 tables a
// round share a set; every pair meets 13 others and plays each set at most once (Validate).
func TestReducedHowell(t *testing.T) {
	for _, tc := range []struct{ pairs, rounds, sets, dups int }{
		{40, 13, 16, 4}, {36, 13, 16, 2}, {34, 13, 16, 1}, {32, 13, 16, 0}, {14, 13, 13, 0}, {45, 13, 16, 7}, {24, 13, 16, 0},
	} {
		m, err := NewReducedHowell(tc.pairs, tc.rounds, tc.sets)
		if err != nil {
			t.Fatalf("%d pairs: %v", tc.pairs, err)
		}
		if m.Rounds != tc.rounds || m.Sets > tc.sets || m.Duplicates() != tc.dups {
			t.Errorf("%d pairs: %d rounds, %d sets, %d duplicates; want %d duplicates", tc.pairs, m.Rounds, m.Sets, m.Duplicates(), tc.dups)
		}
		if got := len(m.Met()); m.Phantom == 0 && got != tc.pairs/2*tc.rounds {
			t.Errorf("%d pairs: %d meetings", tc.pairs, got)
		}
	}
	// 14 pairs over 13 rounds is the complete Howell: one set a table, boards down a table
	if m, _ := NewReducedHowell(14, 13, 16); m.Shared || m.Sets != 13 {
		t.Errorf("14 pairs: %+v", m.Kind)
	}
}
