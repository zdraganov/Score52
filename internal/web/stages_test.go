package web

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
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
	if !strings.Contains(stand, "общо = 25% квалификация + 75% финал") || !strings.Contains(stand, `<span class="weight">75%</span>`) || strings.Contains(stand, "Раздели") {
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
	// a second session of final A: mixed again, and the final's % covers both sessions
	page, _ = c.get(ev + "/new-session?stage=A&min=10&max=30")
	if !strings.Contains(page, "Финал A: сесия 2") || !strings.Contains(page, "разместват") {
		t.Fatal("second final session page")
	}
	m = regexp.MustCompile(`name="kind" value="([^"]+)"[\s\S]*?name="tables" value="(\d+)"[\s\S]*?name="rounds" value="(\d+)"[\s\S]*?name="bpr" value="(\d+)"`).FindStringSubmatch(page)
	_, fa2 := c.post(ev+"/sessions", url.Values{"stage": {"A"}, "kind": {m[1]}, "tables": {m[2]}, "rounds": {m[3]}, "bpr": {m[4]}, "method": {"mp"}})
	if fa2 != ev+"/s/fA2" {
		t.Fatalf("second final session at %s", fa2)
	}
	fill(c, fa2, rng)
	sessions, _ = st.Sessions()
	fr, _ = stageStandings(st, sessions, "A", carry, 0.25, pl.Finals[0].Entries)
	for _, row := range fr.Rows {
		a, b := *row.PerSession[0], *row.PerSession[1]
		if row.Total.Score < math.Min(a, b)-1e-9 || row.Total.Score > math.Max(a, b)+1e-9 {
			t.Errorf("%s: final %v is not between its sessions %v and %v", row.Names, row.Total.Score, a, b)
		}
		if want := 0.25*carry[int(row.Entry)] + 0.75*row.Total.Score; math.Abs(row.Total.Total-want) > 1e-9 {
			t.Errorf("%s: total %v, want %v", row.Names, row.Total.Total, want)
		}
	}
	xml, _ = c.get(ev + "/stage/A/usebio.xml")
	if strings.Contains(xml, "<BOARD>") || strings.Count(xml, "<PAIR>") != cut {
		t.Error("a two-session final's file should hold the ranking only")
	}

	// the files for bridge.bg, by slot
	file := func(path string) (string, string) {
		t.Helper()
		r, err := c.http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		_, name, _ := strings.Cut(r.Header.Get("Content-Disposition"), `filename="`)
		name = strings.TrimSuffix(name, `"`)
		return name[strings.Index(name, "."):], string(b)
	}
	base := strings.TrimPrefix(ev, "/e/")
	for _, f := range []struct{ path, name string }{
		{s1 + "/usebio.xml", ".s1.xml"}, {s2 + "/usebio.xml", ".s2.xml"}, {ev + "/stage/q/usebio.xml", ".s2.xml"},
		{fa + "/usebio.xml", ".f1s1.xml"}, {fa2 + "/usebio.xml", ".f1s2.xml"}, {ev + "/stage/A/usebio.xml", ".f1.xml"},
	} {
		if name, _ := file(f.path); name != f.name {
			t.Errorf("%s: file %s%s, want %s", f.path, base, name, f.name)
		}
	}
	// session 2's file holds the qualifying so far (both sessions) and session 2 alone beside it
	_, q2 := file(s2 + "/usebio.xml")
	if !strings.Contains(q2, fmt.Sprintf("<PERCENTAGE>%.2f</PERCENTAGE>", q.Rows[0].Total.Total)) || !strings.Contains(q2, "<SESSION_PERCENTAGE>") ||
		!strings.Contains(q2, "<BOARD>") || strings.Contains(q2, "<CARRY_OVER>") {
		t.Errorf("session 2 file:\n%s", q2[:min(len(q2), 1200)])
	}
	// a final session's file: the final so far with the carry-over, and its boards
	_, f2 := file(fa2 + "/usebio.xml")
	if !strings.Contains(f2, "<CARRY_OVER>") || !strings.Contains(f2, "<SESSION_PERCENTAGE>") || !strings.Contains(f2, "<BOARD>") {
		t.Errorf("final session 2 file:\n%s", f2[:min(len(f2), 1200)])
	}
	// bridge.bg reads them as Magic Contest's: "Сесия | Общо" and "Финал | Пренесено | Общо"
	if rows := siteRanking(t, q2); rows != nil {
		if rows[0]["SessionPercentage"] == nil || rows[0]["NrBoardsSession"] == nil || rows[0]["ScoreCarryOver"] != nil {
			t.Errorf("site reads session 2 as %v", rows[0])
		}
	}
	_, fw := file(ev + "/stage/A/usebio.xml")
	if rows := siteRanking(t, fw); rows != nil {
		w := fr.Rows[0]
		carried := 0.25 * w.Total.Carry
		if rows[0]["ScoreCarryOver"] != fmt.Sprintf("%.2f", carried) || rows[0]["SessionScoreMP"] != fmt.Sprintf("%.2f", w.Total.Total-carried) ||
			rows[0]["TotalScoreMP"] != fmt.Sprintf("%.2f", w.Total.Total) || rows[0]["NrBoardsSession"] != nil {
			t.Errorf("site reads final A's winner as %v", rows[0])
		}
		// with bridge.bg#244 the site shows the plain results under their weights
		if siteKnows("CARRY_WEIGHT") {
			if rows[0]["CarryWeight"] != "0.25" || rows[0]["QualifyingPercentage"] != fmt.Sprintf("%.2f", w.Total.Carry) ||
				rows[0]["FinalPercentage"] != fmt.Sprintf("%.2f", w.Total.Score) {
				t.Errorf("site reads final A's plain results as %v", rows[0])
			}
		}
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

// siteRanking is bridge.bg's UsebioParser's ranking of a file: its repository next to this one, or
// BRIDGEBG_DIR (a checkout with the session and carry-over fields). nil when it can't run.
func siteRanking(t *testing.T, xml string) []map[string]any {
	t.Helper()
	site := os.Getenv("BRIDGEBG_DIR")
	if site == "" {
		site = "../../../bridge.bg"
	}
	dir, _ := filepath.Abs(filepath.Join(site, "next/src/Results"))
	if _, err := os.Stat(dir); err != nil {
		t.Log("no bridge.bg repository: not checked against the site")
		return nil
	}
	if _, err := exec.LookPath("php"); err != nil {
		t.Log("no php: not checked against the site")
		return nil
	}
	src, err := os.ReadFile(filepath.Join(dir, "UsebioParser.php"))
	if err != nil || !strings.Contains(string(src), "CARRY_OVER") {
		t.Log("this bridge.bg doesn't read session results yet: set BRIDGEBG_DIR to a newer checkout")
		return nil
	}
	f := filepath.Join(t.TempDir(), "1.xml")
	os.WriteFile(f, []byte(xml), 0o644)
	out, err := exec.Command("php", "-r", `require $argv[1]."/PbnParser.php"; require $argv[1]."/UsebioParser.php";
		echo json_encode((new App\Results\UsebioParser())->parse($argv[2])["ranking"] ?? null);`, dir, f).CombinedOutput()
	var rows []map[string]any
	if err != nil || json.Unmarshal(out, &rows) != nil || len(rows) == 0 {
		t.Fatalf("site parser: %v\n%s", err, out)
	}
	return rows
}

// siteKnows: the bridge.bg checkout's USEBIO reader knows a tag.
func siteKnows(tag string) bool {
	site := os.Getenv("BRIDGEBG_DIR")
	if site == "" {
		site = "../../../bridge.bg"
	}
	src, err := os.ReadFile(filepath.Join(site, "next/src/Results/UsebioParser.php"))
	return err == nil && strings.Contains(string(src), tag)
}
