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
	ds := Advise(40, 60, 26, 2)
	if len(ds) < 2 {
		t.Fatalf("40-60 pairs: %+v", ds)
	}
	// the smallest seats the expected 60, and down to 51 (2 × 13 tables, one pair sitting out): 2 × boards 1-30
	if d := ds[0]; d.Copies != 2 || d.BoardsPerCopy != 30 || d.Down != 51 {
		t.Errorf("first deal: %d × %d down to %d", d.Copies, d.BoardsPerCopy, d.Down)
	}
	// the last covers the whole range, with more boards
	last := ds[len(ds)-1]
	if last.Down != 40 || last.Total() <= ds[0].Total() {
		t.Errorf("last deal: %d × %d down to %d", last.Copies, last.BoardsPerCopy, last.Down)
	}
	for _, d := range ds {
		for p, l := range d.ByPairs {
			if l.Boards < 24 || l.Boards > 28 || l.BoardsPerCopy > d.BoardsPerCopy || l.Copies > d.Copies {
				t.Errorf("%d × %d, %d pairs: %s", d.Copies, d.BoardsPerCopy, p, l)
			}
		}
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
	if err != nil || m.Phantom == 0 || m.Label(m.Phantom)[0] != 'A' {
		t.Errorf("57 pairs: %v, phantom %d", err, m.Phantom)
	}
}
