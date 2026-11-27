package movement

import "testing"

func TestLayouts(t *testing.T) {
	for _, tc := range []struct {
		pairs int
		want  string
	}{
		{60, "M15/30,M15/30"}, {56, "M14/28,M14/28"}, {52, "M13/26,M13/26"}, {50, "M13/26,H12/24"},
		{48, "M13/26,H11/22"}, {40, "M13/26,H7/14"}, {38, "H10/20,H9/18"}, {36, "A18/36"}, {34, "H9/18,H8/16"},
		{32, "W16/32"}, {30, "H8/16,H7/14"}, {26, "M13/26"},
		{51, "M13/25,M13/26"}, {35, "H9/18,H9/17"},
	} {
		ls := Layouts(tc.pairs, 26, 0, 0, 0, 1)
		if len(ls) == 0 || ls[0].Spec() != tc.want || ls[0].Boards != 26 {
			t.Errorf("%d pairs, one session: %v, want %s", tc.pairs, ls[0].Spec(), tc.want)
		}
	}
	// 57 pairs: the odd pair out sits out in section A
	l := Layouts(57, 26, 0, 0, 0, 1)[0]
	if l.Sections[0].Pairs != 29 || l.Sections[1].Pairs != 28 {
		t.Errorf("57 pairs: %+v", l.Sections)
	}
}

// Before the day, 26-60 pairs expected, one session of 26 boards: two boxes seat them all (Mitchell
// sections, a Mitchell and a Howell, two Howells), nobody sharing a set.
func TestAdvise(t *testing.T) {
	ds := Advise(26, 60, 26, 0, 1)
	if len(ds) != 1 || ds[0].Down != 26 || ds[0].Copies != 2 || ds[0].Duplicates != 0 {
		t.Errorf("deals: %+v", ds)
	}
	for _, d := range ds {
		for p, l := range d.ByPairs {
			if l.Boards != 26 || l.Copies > d.Copies || l.Duplicates > d.Duplicates {
				t.Errorf("%d pairs: %s", p, l)
			}
		}
	}
	// two sessions: the same boxes
	if two := Advise(40, 60, 26, 0, 2); two[len(two)-1].Down != 40 || two[len(two)-1].Duplicates != 0 {
		t.Errorf("two sessions: %+v", two)
	}
}

func TestSitOutsPerSection(t *testing.T) {
	l, _ := LayoutOf("M13/25,M13/25", 13, 2) // two sections with a pair sitting out in each
	if l.SitOuts != 2 || l.Sections[0].Pairs != 25 || l.Sections[1].Pairs != 25 {
		t.Fatalf("50 pairs: %s, %+v", l, l.Sections)
	}
	m, err := l.Build()
	if err != nil || len(m.Phantoms) != 2 || m.Label(m.Phantoms[1])[0] != 'B' {
		t.Fatalf("50 pairs: %v %v", err, m.Phantoms)
	}
	ns, ew := m.Lines()
	if len(ns)+len(ew) != 50 {
		t.Errorf("lines hold %d pairs", len(ns)+len(ew))
	}
	if b := m.Section(1); b.Phantom == 0 || b.Validate() != nil {
		t.Errorf("section B alone: phantom %d", b.Phantom)
	}
}

func TestRelayMitchell(t *testing.T) {
	for tables := 4; tables <= 30; tables += 2 {
		m, err := NewMitchell(tables, tables, MitchellOptions{Relay: true})
		if err != nil {
			t.Fatalf("%d tables: %v", tables, err)
		}
		if got := len(m.Met()); got != tables*tables {
			t.Errorf("%d tables: %d meetings, want %d", tables, got, tables*tables)
		}
		// the first and the last table share a set every round; one set rests; the rest move down a table
		down, stand := 0, 0
		for r := 1; r <= m.Rounds; r++ {
			if m.SharedWith(r, 1) != tables {
				t.Fatalf("%d tables, round %d: table 1 shares with %d", tables, r, m.SharedWith(r, 1))
			}
			if r == m.Rounds {
				continue
			}
			for tb := 2; tb <= tables; tb++ {
				switch next := m.Next(r, tb); {
				case len(next) == 0:
					stand++
				case next[0] == tb-1:
					down++
				}
			}
		}
		if stand != m.Rounds-1 {
			t.Errorf("%d tables: %d sets on the stand over the rounds, want one a round", tables, stand)
		}
		// the stand is in the middle: table T/2+1's boards come from it, table T/2's go... down to T/2-1
		if next := m.Next(1, tables/2+1); len(next) != 0 {
			t.Errorf("%d tables: boards of table %d go to %v, want the stand", tables, tables/2+1, next)
		}
	}
	// 12 tables, 24 boards = 12 rounds × 2: a relay section, one duplicated set
	l := Layouts(24, 24, 0, 0, 0, 1)[0]
	if l.Sections[0].Kind != RelayMitchell || l.Duplicates != 1 || l.Deal() != 26 {
		t.Errorf("24 pairs, 24 boards: %s %+v, deal %d", l, l.Sections, l.Deal())
	}
}

func TestBuildSections(t *testing.T) {
	m, err := Layouts(56, 26, 2, 0, 0, 1)[0].Build()
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Sections) != 2 || m.Tables != 28 || m.Pairs != 56 || m.Rounds != 13 || !m.TwoFields {
		t.Fatalf("%+v", m.Sections)
	}
	ns, ew := m.Lines()
	if len(ns) != 28 || len(ew) != 28 {
		t.Errorf("lines %d/%d", len(ns), len(ew))
	}
	// boards stay in their section: B1's boards go to B14 (the section's last table), never to A
	for r := 1; r < m.Rounds; r++ {
		for tb := 1; tb <= m.Tables; tb++ {
			for _, n := range m.Next(r, tb) {
				if m.sectionOf(n) != m.sectionOf(tb) {
					t.Fatalf("round %d: boards of %s go to %s", r, m.TableLabel(tb), m.TableLabel(n))
				}
			}
		}
	}
	if got := m.TableLabel(m.Next(1, 15)[0]); got != "B14" {
		t.Errorf("B1's boards go to %s", got)
	}
	if m.Label(29) != "B1" || m.Label(56) != "B28" || m.TableLabel(15) != "B1" {
		t.Errorf("labels %s %s %s", m.Label(29), m.Label(56), m.TableLabel(15))
	}
	// a section on its own is a plain skip Mitchell again (for Bridgemate)
	b := m.Section(1)
	if b.Tables != 14 || b.Pairs != 28 || b.Validate() != nil || b.Seats[0].NS != 1 {
		t.Errorf("section B: %+v", b.Seats[0])
	}
	// each set is played 13 times in each section: 26 across the field
	plays := map[int]int{}
	for _, s := range m.Seats {
		plays[s.Set]++
	}
	for set, n := range plays {
		if n != 26 {
			t.Errorf("set %d played %d times", set, n)
		}
	}
	// 57 pairs: the sit-out is in section A only
	m, err = Layouts(57, 26, 2, 0, 0, 1)[0].Build()
	if err != nil || len(m.Phantoms) != 1 || m.Label(m.Phantoms[0])[0] != 'A' {
		t.Errorf("57 pairs: %v, sit-outs %v", err, m.Phantoms)
	}
}

// The number of sessions decides the kind, as the BBF's TD prefers.
func TestLayoutsBySessions(t *testing.T) {
	for _, tc := range []struct {
		pairs, sessions int
		typ, spec       string
		dups            int
	}{
		{56, 1, TypeSections, "M14/28,M14/28", 0}, // fits Mitchell sections
		{40, 1, TypeMixed, "M13/26,H7/14", 0},     // a complete Mitchell of 13 tables and a complete Howell of 14
		{50, 2, TypeMixed, "M13/26,H12/24", 0},    // several sessions: Mitchell and Howell, for new opponents
		{56, 2, TypeSections, "M14/28,M14/28", 0}, // ... unless the Howell would need the side table
		{30, 3, TypeHowells, "H8/16,H7/14", 0},    // the TD's: two Howells, nobody sitting out
	} {
		ls := Layouts(tc.pairs, 26, 0, 0, 0, tc.sessions)
		if len(ls) == 0 {
			t.Fatalf("%d pairs, %d sessions: nothing", tc.pairs, tc.sessions)
		}
		l := ls[0]
		if l.Type != tc.typ || tc.spec != "" && l.Spec() != tc.spec || l.Duplicates != tc.dups {
			t.Errorf("%d pairs, %d sessions: %s %s (%d duplicated); want %s %s (%d)", tc.pairs, tc.sessions, l.Type, l.Spec(), l.Duplicates, tc.typ, tc.spec, tc.dups)
		}
		m, err := l.Build()
		if err != nil {
			t.Fatalf("%d pairs: %v", tc.pairs, err)
		}
		if l.Type == TypeMixed && m.TwoFields {
			t.Errorf("%d pairs: a mixed layout ranks as one field", tc.pairs)
		}
		// rebuilt from its spec, as the form does
		if r, err := LayoutOf(l.Spec(), l.Rounds, l.BoardsPerRound); err != nil || r.Spec() != l.Spec() {
			t.Errorf("%s: %v", l.Spec(), err)
		}
	}
}
