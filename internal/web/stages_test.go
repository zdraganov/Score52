package web

import (
	"fmt"
	"math"
	"math/rand/v2"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/zdraganov/Score52/internal/pairs"
)

// fill enters a random result at every table of every board of a session.
func fill(c *client, sess string, rng *rand.Rand) {
	c.t.Helper()
	page, _ := c.get(sess)
	for _, m := range regexp.MustCompile(`/b/(\d+)"`).FindAllStringSubmatch(page, -1) {
		trav, _ := c.get(sess + "/b/" + m[1])
		rows := len(regexp.MustCompile(`name="c\d+"`).FindAllString(trav, -1))
		v := url.Values{"go": {"stay"}}
		for k := 0; k < rows; k++ {
			ks := fmt.Sprint(k)
			v.Set("c"+ks, []string{"4S", "3NT", "2H", "5DX", "1NT"}[rng.IntN(5)])
			v.Set("d"+ks, string("NESW"[rng.IntN(4)]))
			v.Set("r"+ks, []string{"=", "+1", "-1", "-2"}[rng.IntN(4)])
		}
		c.post(sess+"/b/"+m[1], v)
	}
}

func TestStagesFlow(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	jar, _ := cookiejar.New(nil)
	c := &client{t: t, base: ts.URL, http: &http.Client{Jar: jar}, token: s.token}
	rng := rand.New(rand.NewPCG(5, 2))

	_, ev := c.post("/events", url.Values{"title": {"Двусесиен турнир"}, "date": {"2026-11-28"}})
	for i := 1; i <= 12; i++ {
		c.post(ev+"/pairs", url.Values{"name1": {fmt.Sprintf("Играч %dа", i)}, "name2": {fmt.Sprintf("Играч %dб", i)}})
	}
	// two qualifying sessions: 12 pairs, a 6-table skip Mitchell of 5 rounds
	_, s1 := c.post(ev+"/sessions", url.Values{"kind": {"skip-mitchell"}, "tables": {"6"}, "rounds": {"5"}, "bpr": {"3"}, "method": {"mp"}})
	fill(c, s1, rng)
	page, _ := c.get(ev + "/new-session")
	if !strings.Contains(page, "разместват") {
		t.Error("second session does not say it mixes")
	}
	_, s2 := c.post(ev+"/sessions", url.Values{"kind": {"skip-mitchell"}, "tables": {"6"}, "rounds": {"5"}, "bpr": {"3"}, "method": {"mp"}})
	if s2 != ev+"/s/s2" {
		t.Fatalf("second session at %s", s2)
	}
	fill(c, s2, rng)

	// the second session mixes: fewer repeated meetings than seating everyone as before
	st, _ := s.open(strings.TrimPrefix(ev, "/e/"))
	sessions, _ := st.Sessions()
	h := pairs.NewHistory()
	h.Add(sessions[0].Movement, ints(sessions[0].Seating))
	same := pairs.Repeats(sessions[1].Movement, ints(sessions[0].Seating), h)
	got := pairs.Repeats(sessions[1].Movement, ints(sessions[1].Seating), h)
	if got >= same {
		t.Errorf("session 2 repeats %d meetings; seating as before would repeat %d", got, same)
	}

	// finals: A the top 6, B the rest, 25% carry-over
	stand, _ := c.get(ev + "/standings")
	if !strings.Contains(stand, "Сесия 2") || !strings.Contains(stand, "Финал A") {
		t.Fatal("standings before finals")
	}
	q, carry, _ := qualifying(st, sessions)
	cut := 6 // a cut with no tie on it: random results can leave two pairs level
	for q.Rows[cut-1].Total.Total == q.Rows[cut].Total.Total {
		cut++
	}
	tie := -1
	for i := 4; i+1 < len(q.Rows); i++ {
		if q.Rows[i-1].Total.Total == q.Rows[i].Total.Total {
			tie = i
		}
	}
	if tie > 0 {
		r, _ := c.http.PostForm(ts.URL+ev+"/finals", url.Values{"t": {s.token}, "nA": {fmt.Sprint(tie)}, "carry": {"25"}})
		if r.StatusCode != 400 {
			t.Errorf("a tie on the cut after %d was accepted", tie)
		}
	}
	c.post(ev+"/finals", url.Values{"nA": {fmt.Sprint(cut)}, "nB": {"0"}, "carry": {"25"}})
	pl, _ := loadPlan(st)
	if len(pl.Finals) != 2 || len(pl.Finals[0].Entries) != cut || len(pl.Finals[1].Entries) != 12-cut || pl.Finals[0].Entries[0] != q.Rows[0].Entry {
		t.Fatalf("plan: %+v", pl)
	}

	// final A: one session, a 3-table Howell... whatever is offered first
	page, _ = c.get(ev + "/new-session?stage=A&min=10&max=30")
	if !strings.Contains(page, "Финал A: сесия 1") {
		t.Fatalf("final A session page:\n%s", page)
	}
	m := regexp.MustCompile(`name="kind" value="([^"]+)"[\s\S]*?name="tables" value="(\d+)"[\s\S]*?name="rounds" value="(\d+)"[\s\S]*?name="bpr" value="(\d+)"`).FindStringSubmatch(page)
	_, fa := c.post(ev+"/sessions", url.Values{"stage": {"A"}, "kind": {m[1]}, "tables": {m[2]}, "rounds": {m[3]}, "bpr": {m[4]}, "method": {"mp"}})
	if fa != ev+"/s/fA1" {
		t.Fatalf("final A session at %s", fa)
	}
	fill(c, fa, rng)

	// final standings: 25% qualifying + 75% final, recomputed by hand for the winner
	sessions, _ = st.Sessions()
	fr, err := stageStandings(st, sessions, "A", carry, 0.25, pl.Finals[0].Entries)
	if err != nil || len(fr.Rows) != cut {
		t.Fatalf("final A: %v %+v", err, fr)
	}
	w := fr.Rows[0]
	if want := 0.25*carry[int(w.Entry)] + 0.75**w.PerSession[0]; math.Abs(w.Total.Total-want) > 1e-9 {
		t.Errorf("winner's total %v, want %v", w.Total.Total, want)
	}
	stand, _ = c.get(ev + "/standings")
	if !strings.Contains(stand, "пренасяне 25%") || strings.Contains(stand, "Раздели") {
		t.Error("standings after the final started")
	}
	// the final's file goes to bridge.bg as .f1, carry-over in, with its boards (one session)
	r, _ := c.http.Get(ts.URL + ev + "/stage/A/usebio.xml")
	if cd := r.Header.Get("Content-Disposition"); !strings.Contains(cd, `.f1.xml"`) {
		t.Errorf("final file name: %s", cd)
	}
	xml, _ := c.get(ev + "/stage/A/usebio.xml")
	if n := strings.Count(xml, "<PAIR>"); n != cut || !strings.Contains(xml, "<BOARD>") || !strings.Contains(xml, fmt.Sprintf("<PERCENTAGE>%.2f</PERCENTAGE>", w.Total.Total)) {
		t.Errorf("final A usebio: %d pairs\n%s", n, xml[:min(len(xml), 1500)])
	}
	// planning again is refused once a final has a session
	r, _ = c.http.PostForm(ts.URL+ev+"/finals", url.Values{"t": {s.token}, "nA": {"4"}, "carry": {"0"}})
	if r.StatusCode != 400 {
		t.Errorf("re-planning after the final started: %d", r.StatusCode)
	}
}

func ints(v []int64) []int {
	out := make([]int, len(v))
	for i, x := range v {
		out[i] = int(x)
	}
	return out
}
