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
	o := Options(18, 24, 28) // 9 tables: complete Mitchell 9x3 = 27
	if len(o) == 0 || o[0].Kind != Mitchell || o[0].Boards != 27 || !o[0].Complete {
		t.Errorf("18 pairs: %+v", o[0])
	}
	o = Options(8, 21, 28) // 4 tables: Howell 7 rounds
	if len(o) == 0 || o[0].Kind != Howell || !o[0].Complete {
		t.Errorf("8 pairs: %+v", o)
	}
}
