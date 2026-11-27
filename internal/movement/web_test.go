package movement

import "testing"

func TestWebMitchell(t *testing.T) {
	m, err := NewWebMitchell(16, 13, true)
	if err != nil {
		t.Fatal(err)
	}
	// as Magic Contest's: round 1 table 9 plays boards of set 7, table 16 set 13; round 2 table 1 NS 1 v EW 32
	r1 := m.Round(1)
	if r1[8].Set != 7 || r1[15].Set != 13 || m.Round(2)[0].EW != 32 {
		t.Errorf("round 1: %+v %+v; round 2 table 1: %+v", r1[8], r1[15], m.Round(2)[0])
	}
	// scrambled in round 4, not at tables 1 and 9
	r4 := m.Round(4)
	if r4[0].NS != 1 || r4[8].NS != 9 || r4[1].NS == 2 {
		t.Errorf("round 4 switches: %+v %+v %+v", r4[0], r4[8], r4[1])
	}
	if len(m.Met()) != 16*13 {
		t.Errorf("%d meetings", len(m.Met()))
	}
	for _, tables := range []int{14, 16, 18, 20, 22, 24, 26} {
		if _, err := NewWebMitchell(tables, 13, true); err != nil {
			t.Errorf("%d tables: %v", tables, err)
		}
	}
}

func TestAppendixMitchell(t *testing.T) {
	m, err := NewAppendixMitchell(13, 5, true)
	if err != nil {
		t.Fatal(err)
	}
	// as Magic Contest's (its numbers: travelling NS 1-13, EW 21-33, fixed NS 34-38, fixed EW 39-43):
	// round 1 table 14 is travelling NS 1 v fixed EW 39 on set 1; round 2 table 6 is NS 4 v EW 25 on set 7
	r1, r2 := m.Round(1), m.Round(2)
	if r1[13].NS != 1 || r1[13].EW != 2*13+5+1 || r1[13].Set != 1 || r2[5].NS != 4 || r2[5].EW != 13+5 || r2[5].Set != 7 {
		t.Errorf("round 1 table 14: %+v; round 2 table 6: %+v", r1[13], r2[5])
	}
	for app := 1; app <= 6; app++ {
		if _, err := NewAppendixMitchell(13, app, true); err != nil {
			t.Errorf("13+%d: %v", app, err)
		}
	}
}
