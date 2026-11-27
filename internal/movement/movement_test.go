package movement

import "testing"

func TestMitchell(t *testing.T) {
	for tables := 3; tables <= 20; tables++ {
		max := tables
		if tables%2 == 0 {
			max = tables - 1
		}
		m, err := NewMitchell(tables, max, MitchellOptions{})
		if err != nil {
			t.Fatalf("%d tables: %v", tables, err)
		}
		// every NS pair meets every EW pair it can
		if got := len(m.Met()); got != tables*max {
			t.Errorf("%d tables: %d meetings", tables, got)
		}
		if _, err := NewMitchell(tables, max+1, MitchellOptions{}); err == nil {
			t.Errorf("%d tables: %d rounds accepted", tables, max+1)
		}
	}
	if _, err := NewMitchell(7, 7, MitchellOptions{ArrowSwitch: []int{6, 7}}); err != nil {
		t.Fatal(err)
	}
}

func TestHowell(t *testing.T) {
	for pairs := 3; pairs <= 40; pairs++ {
		n := pairs + pairs%2
		m, err := NewHowell(pairs, n-1, false)
		if err != nil {
			t.Fatalf("%d pairs: %v", pairs, err)
		}
		if got, want := len(m.Met()), n*(n-1)/2; m.Phantom == 0 && got != want {
			t.Errorf("%d pairs: %d meetings, want %d", pairs, got, want)
		}
		if n >= 8 && m.Duplicated {
			t.Errorf("%d pairs: expected shared boards", pairs)
		}
		if _, err := NewHowell(pairs, 3, true); err != nil {
			t.Errorf("%d pairs reduced duplicated: %v", pairs, err)
		}
	}
}

func TestValidateCatches(t *testing.T) {
	m, _ := NewMitchell(5, 5, MitchellOptions{})
	m.Seats[6].EW = m.Seats[5].EW // pair seated twice in round 2
	if m.Validate() == nil {
		t.Error("broken movement validated")
	}
}

func TestOptions(t *testing.T) {
	o := Options(18, 24, 28, 1) // 9 tables: complete Mitchell 9x3 = 27
	if len(o) == 0 || o[0].Kind != Mitchell || o[0].Boards != 27 || !o[0].Complete {
		t.Errorf("18 pairs: %+v", o[0])
	}
	o = Options(8, 21, 28, 1) // 4 tables: Howell 7 rounds
	if len(o) == 0 || o[0].Kind != Howell || !o[0].Complete {
		t.Errorf("8 pairs: %+v", o)
	}
}

// With one set, the boards of a Howell move down one table each round, or rest on the relay stand.
func TestHowellBoardsMoveDown(t *testing.T) {
	for pairs := 8; pairs <= 40; pairs += 2 {
		m, err := NewHowell(pairs, pairs-1, false)
		if err != nil {
			t.Fatal(err)
		}
		relays := 0
		for r := 1; r < m.Rounds; r++ {
			for tb := 1; tb <= m.Tables; tb++ {
				switch next := m.Next(r, tb); {
				case len(next) == 0:
					relays++
				case len(next) > 1 || next[0] != tb-1:
					t.Fatalf("%d pairs, round %d: table %d's boards go to %v", pairs, r, tb, next)
				}
			}
		}
		if relays == 0 {
			t.Errorf("%d pairs: no relay stand used", pairs)
		}
		// table 1 plays set 1 in round 1
		if m.Round(1)[0].Set != 1 {
			t.Errorf("%d pairs: table 1 starts on set %d", pairs, m.Round(1)[0].Set)
		}
	}
}

func TestMitchellBoardsMoveDown(t *testing.T) {
	m, _ := NewMitchell(7, 7, MitchellOptions{})
	for tb := 1; tb <= 7; tb++ {
		want := tb - 1
		if want == 0 {
			want = 7
		}
		if next := m.Next(1, tb); len(next) != 1 || next[0] != want {
			t.Errorf("table %d: boards go to %v", tb, next)
		}
	}
}

// One set for 4-7 pairs: tables share, as little as possible.
func TestSharedHowell(t *testing.T) {
	for pairs, wantCrowded := range map[int]int{4: 3, 5: 1, 6: 1, 7: 0} {
		m, err := NewHowell(pairs, pairs+pairs%2-1, false)
		if err != nil {
			t.Fatalf("%d pairs: %v", pairs, err)
		}
		if pairs < 7 && (m.Kind != HowellShared || !m.Shared) {
			t.Fatalf("%d pairs: %s", pairs, m.Kind)
		}
		crowded := 0 // rounds with a set at every table (4 pairs: both tables, always)
		for r := 1; r <= m.Rounds; r++ {
			on := map[int]int{}
			for _, s := range m.Round(r) {
				on[s.Set]++
			}
			for _, n := range on {
				if n == m.Tables && m.Tables > 1 && pairs < 7 {
					crowded++
				}
			}
		}
		if pairs < 7 && crowded != wantCrowded {
			t.Errorf("%d pairs: %d rounds with one set at every table, want %d", pairs, crowded, wantCrowded)
		}
	}
	if o := Options(6, 15, 25, 1); o[0].Kind != HowellShared {
		t.Errorf("6 pairs, one set: %+v", o[0])
	}
	if o := Options(6, 15, 25, 3); o[0].Kind != HowellDuplicated {
		t.Errorf("6 pairs, three copies: %+v", o[0])
	}
}
