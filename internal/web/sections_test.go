package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// 50-60 pairs expected for 26 boards: deal 2 × boards 1-30; 56 come: two sections of 14 tables, one
// field.
func TestDealAndSections(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	c := &client{t: t, base: ts.URL, http: &http.Client{}, token: s.token}

	_, ev := c.post("/events", url.Values{"title": {"Голям турнир"}, "date": {"2026-11-30"}})
	page, _ := c.get(ev + "/deal?from=50&to=60&boards=26")
	if !strings.Contains(page, "2 × раздавания 1–30") || !strings.Contains(page, "стигат за 50–60 двойки") || strings.Contains(page, `name="tol"`) {
		t.Fatalf("deal options:\n%s", page[strings.Index(page, "<main>"):])
	}
	c.post(ev+"/deal", url.Values{"from": {"50"}, "to": {"60"}, "boards": {"26"}, "copies": {"2"}, "per_copy": {"30"}})

	for i := 1; i <= 56; i++ {
		c.post(ev+"/pairs", url.Values{"name1": {fmt.Sprintf("Играч %dа", i)}, "name2": {fmt.Sprintf("Играч %dб", i)}})
	}
	page, _ = c.get(ev + "/new-session")
	if !strings.Contains(page, "На секции") || !strings.Contains(page, "A: 14 маси · B: 14 маси") || !strings.Contains(page, "2 × раздавания 1–28") {
		t.Fatalf("new session:\n%s", page[strings.Index(page, "<main>"):min(len(page), strings.Index(page, "<main>")+3000)])
	}
	m := regexp.MustCompile(`name="sections" value="([\d,]+)"[\s\S]*?name="rounds" value="(\d+)"[\s\S]*?name="bpr" value="(\d+)"`).FindStringSubmatch(page)
	if m[1] != "14,14" || m[2] != "13" || m[3] != "2" {
		t.Fatalf("recommended layout %v", m[1:])
	}
	plan, sess := c.post(ev+"/sessions", url.Values{"kind": {"sections"}, "sections": {m[1]}, "rounds": {m[2]}, "bpr": {m[3]}, "method": {"mp"}})
	for _, want := range []string{"2 секции", "Секция A", "Секция B", "Маса B14", "→ маса B13"} {
		if !strings.Contains(plan, want) {
			t.Errorf("plan lacks %q", want)
		}
	}

	// board 1 is played 13 times in each section: 26 results scored together
	trav, _ := c.get(sess + "/b/1")
	rows := len(regexp.MustCompile(`name="c\d+"`).FindAllString(trav, -1))
	if rows != 26 || !strings.Contains(trav, "<b>B") {
		t.Fatalf("board 1 has %d rows", rows)
	}
	v := url.Values{"go": {"stay"}}
	for k := 0; k < rows; k++ {
		v.Set(fmt.Sprintf("c%d", k), []string{"4S", "3NT"}[k%2])
		v.Set(fmt.Sprintf("d%d", k), "N")
		v.Set(fmt.Sprintf("r%d", k), "=")
	}
	saved, _ := c.post(sess+"/b/1", v)
	// 13 at 420 and 13 at 400 (board 1, nobody vulnerable), compared across both sections: 4S beats 13
	// and ties 12 of 25 = 76%, 3NT ties 12 = 24%
	if !strings.Contains(saved, ">76<") || !strings.Contains(saved, ">24<") {
		t.Errorf("board 1 not scored across the field:\n%s", saved[strings.Index(saved, "<tbody>"):strings.Index(saved, "</tbody>")])
	}
	res, _ := c.get(sess + "/results")
	if !strings.Contains(res, "<td>B") || !strings.Contains(res, "Север – Юг") {
		t.Error("results don't show section labels or lines")
	}
}

// 50 pairs: 13 + 13 tables with a pair sitting out in each section; the plan shows both.
func TestSectionsWithSitOuts(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	c := &client{t: t, base: ts.URL, http: &http.Client{}, token: s.token}
	_, ev := c.post("/events", url.Values{"title": {"50 двойки"}, "date": {"2026-11-30"}})
	c.post(ev+"/deal", url.Values{"from": {"50"}, "to": {"60"}, "boards": {"26"}, "copies": {"2"}, "per_copy": {"30"}})
	for i := 1; i <= 50; i++ {
		c.post(ev+"/pairs", url.Values{"name1": {fmt.Sprint("А", i)}, "name2": {fmt.Sprint("Б", i)}})
	}
	page, _ := c.get(ev + "/new-session")
	if !strings.Contains(page, "A: 13 маси (една двойка почива) · B: 13 маси (една двойка почива)") {
		t.Fatalf("50 pairs:\n%s", page[strings.Index(page, "<main>"):min(len(page), strings.Index(page, "<main>")+2500)])
	}
	plan, _ := c.post(ev+"/sessions", url.Values{"kind": {"sections"}, "sections": {"13,13"}, "rounds": {"13"}, "bpr": {"2"}, "method": {"mp"}})
	if strings.Count(plan, "(почива)") != 26 { // a sit-out at one table of each section, every round
		t.Errorf("%d sit-outs in the plan, want 26", strings.Count(plan, "(почива)"))
	}
}
