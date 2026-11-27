package movement

import "testing"

func TestLayouts(t *testing.T) {
	for _, tc := range []struct {
		pairs int
		want  string
	}{
		{60, "A:15 + B:15 tables, 13 rounds × 2 = 26 boards, deal 2 × boards 1-30, each board played 26 times"},
		{56, "A:14 + B:14 tables, 13 rounds × 2 = 26 boards, deal 2 × boards 1-28, each board played 26 times"},
		{52, "A:13 + B:13 tables, 13 rounds × 2 = 26 boards, deal 2 × boards 1-26, each board played 26 times"},
		{40, "A:10 + B:10 tables, 9 rounds × 3 = 27 boards, deal 2 × boards 1-30, each board played 18 times"},
	} {
		ls := Layouts(tc.pairs, 26, 2, 0, 0)
		if len(ls) == 0 || ls[0].String() != tc.want {
			t.Errorf("%d pairs: %v\n want %s", tc.pairs, ls[0], tc.want)
		}
	}
	// 57 pairs: the odd pair out sits out in section A
	l := Layouts(57, 26, 2, 0, 0)[0]
	if l.Sections[0].Pairs != 29 || l.Sections[1].Pairs != 28 {
		t.Errorf("57 pairs: %+v", l.Sections)
	}
	// what was dealt for 60 seats 56 the same way
	if l := Layouts(56, 26, 2, 30, 2); len(l) == 0 || l[0].BoardsPerCopy != 28 {
		t.Errorf("56 pairs within 2 × 30: %v", l)
	}
}

func TestAdvise(t *testing.T) {
	// fixed 26 boards: 2 × boards 1-30 seats 50-60 pairs (50: 13 + 13 tables, a pair sitting out in each)
	ds := Advise(40, 60, 26, 0)
	if len(ds) == 0 || ds[0].Copies != 2 || ds[0].BoardsPerCopy != 30 || ds[0].Down != 50 {
		t.Fatalf("40-60 pairs: %+v", ds)
	}
	for p, l := range ds[0].ByPairs {
		if l.Boards != 26 || l.BoardsPerCopy > 30 {
			t.Errorf("%d pairs: %s", p, l)
		}
	}
	// 40-49 need sections over 16 tables or boards over 32: not offered
	if _, ok := ds[0].ByPairs[44]; ok {
		t.Error("44 pairs seated")
	}
	// with ±2 boards the same deal reaches further down
	if wide := Advise(40, 60, 26, 2); wide[0].Down >= 50 {
		t.Errorf("±2: down to %d", wide[0].Down)
	}
}

func TestSitOutsPerSection(t *testing.T) {
	l := Layouts(50, 26, 0, 0, 0)[0]
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
		// tables 1 and 2 share a set every round; one set rests; the rest move down a table
		down, stand := 0, 0
		for r := 1; r <= m.Rounds; r++ {
			if m.SharedWith(r, 1) != 2 {
				t.Fatalf("%d tables, round %d: table 1 shares with %d", tables, r, m.SharedWith(r, 1))
			}
			if r == m.Rounds {
				continue
			}
			for tb := 2; tb <= tables; tb++ {
				switch next := m.Next(r, tb); {
				case len(next) == 0:
					stand++
				case next[0] == tb-1 || tb == 2 && next[0] == 1:
					down++
				}
			}
		}
		if stand != m.Rounds-1 {
			t.Errorf("%d tables: %d sets on the stand over the rounds, want one a round", tables, stand)
		}
	}
	// 12 tables, 24 boards = 12 rounds × 2: a relay section, one duplicated set
	l := Layouts(24, 24, 0, 0, 0)[0]
	if l.Sections[0].Kind != RelayMitchell || l.Duplicates != 1 || l.Deal() != 26 {
		t.Errorf("24 pairs, 24 boards: %s %+v, deal %d", l, l.Sections, l.Deal())
	}
}

func TestBuildSections(t *testing.T) {
	m, err := Layouts(56, 26, 2, 0, 0)[0].Build()
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
	m, err = Layouts(57, 26, 2, 0, 0)[0].Build()
	if err != nil || len(m.Phantoms) != 1 || m.Label(m.Phantoms[0])[0] != 'A' {
		t.Errorf("57 pairs: %v, sit-outs %v", err, m.Phantoms)
	}
}
