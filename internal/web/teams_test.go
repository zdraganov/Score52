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

// A Swiss teams event from the home page to the file for bridge.bg: 5 teams (a bye), boards of both
// rooms, a correction, standings.
func TestTeamsSwiss(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	c := &client{t: t, base: ts.URL, http: &http.Client{}, token: s.token}

	_, ev := c.post("/events", url.Values{"title": {"Отборно"}, "date": {"2026-12-05"}, "kind": {"teams"}})
	for i := 1; i <= 5; i++ {
		v := url.Values{"name": {fmt.Sprint("Отбор ", i)}}
		for k := 1; k <= 4; k++ {
			v.Set(fmt.Sprint("p", k), fmt.Sprintf("Играч %d-%d", i, k))
		}
		c.post(ev+"/teams", v)
	}
	home, _ := c.get("/")
	if !strings.Contains(home, "5 отбора") {
		t.Error("the home page doesn't count teams")
	}
	page, _ := c.post(ev+"/teams/start", url.Values{"format": {"swiss"}, "boards": {"2"}, "bye_vp": {"12"}})
	if !strings.Contains(page, "Чифтосай рунд 1") {
		t.Fatal("no pairing after the start")
	}
	page, _ = c.post(ev+"/teams/pair", url.Values{"boards": {"2"}})
	links := regexp.MustCompile(`href="(/e/[\w-]+/r/1/m/\d+)"`).FindAllStringSubmatch(page, -1)
	if len(links) != 4 || !strings.Contains(page, "почива") { // two matches, two links each, one bye
		t.Fatalf("round 1: %d match links", len(links))
	}
	// match 1: open 4S= by N (+420 on board 1, none vul), closed 4S-1 by S (-50): home +470 = 10 IMPs;
	// board 2 adjusted 3:0
	m1 := links[0][1]
	page, _ = c.post(m1, url.Values{"oc0": {"4s"}, "od0": {"N"}, "or0": {"="}, "cc0": {"4S"}, "cd0": {"S"}, "cr0": {"-1"},
		"ah1": {"3"}, "aa1": {"0"}})
	if !strings.Contains(page, "13 : 0 IMP") || !strings.Contains(page, "VP") {
		t.Fatalf("match 1 total:\n%s", page[strings.Index(page, "<h1>"):strings.Index(page, "<form")])
	}
	// a correction: board 1 closed made too, 0 IMPs; the match is recorded again
	page, _ = c.post(m1, url.Values{"oc0": {"4S"}, "od0": {"N"}, "or0": {"="}, "cc0": {"4S"}, "cd0": {"S"}, "cr0": {"="},
		"ah1": {"3"}, "aa1": {"0"}})
	if !strings.Contains(page, "3 : 0 IMP") {
		t.Fatal("the correction didn't change the total")
	}
	// a bad row is shown, not saved
	page, _ = c.post(links[2][1], url.Values{"oc0": {"4S"}, "od0": {"X"}, "or0": {"="}})
	if !strings.Contains(page, "разиграващият") {
		t.Error("a bad declarer passed")
	}
	evPage, _ := c.get(ev)
	if strings.Contains(evPage, "Чифтосай рунд 2") {
		t.Error("round 2 can be paired before round 1 is in")
	}
	c.post(links[2][1], url.Values{"oc0": {"3NT"}, "od0": {"S"}, "or0": {"+1"}, "cc0": {"PASS"}, "oc1": {"PASS"}, "cc1": {"PASS"}})
	evPage, _ = c.get(ev)
	if !strings.Contains(evPage, "Чифтосай рунд 2") {
		t.Fatal("round 1 is in but round 2 can't be paired")
	}
	st, _ := c.get(ev + "/standings")
	if !strings.Contains(st, "Отбор") || !strings.Contains(st, "<b>12</b>") { // the bye's 12 VP
		t.Errorf("standings:\n%s", st[strings.Index(st, "<tbody>"):])
	}
	xml, _ := c.get(ev + "/teams/usebio.xml")
	if !strings.Contains(xml, "SWISS_TEAMS") || strings.Count(xml, "<MATCH>") != 2 {
		t.Errorf("usebio:\n%s", xml)
	}
}

// teamsClient starts a teams event of n teams, started with 2 boards a match.
func teamsClient(t *testing.T, n int, format string) (*client, string, func()) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	c := &client{t: t, base: ts.URL, http: &http.Client{}, token: s.token}
	_, ev := c.post("/events", url.Values{"title": {"Отборно"}, "date": {"2026-12-05"}, "kind": {"teams"}})
	for i := 1; i <= n; i++ {
		v := url.Values{"name": {fmt.Sprint("Т", i)}}
		for k := 1; k <= 4; k++ {
			v.Set(fmt.Sprint("p", k), fmt.Sprintf("И %d-%d", i, k))
		}
		c.post(ev+"/teams", v)
	}
	c.post(ev+"/teams/start", url.Values{"format": {format}, "boards": {"2"}, "bye_vp": {"12"}})
	return c, ev, func() { ts.Close(); s.Close() }
}

// playRound pairs a round and plays every match in it: the home team wins 10 IMPs on board 1, except
// the matches listed in tie, all passed (0:0).
func playRound(c *client, ev string, form url.Values, tie map[int]bool) []string {
	c.t.Helper()
	page, _ := c.post(ev+"/teams/pair", form)
	round := regexp.MustCompile(`id="r(\d+)"`).FindAllStringSubmatch(page, -1)
	r := round[len(round)-1][1]
	var links []string
	seen := map[string]bool{}
	for _, m := range regexp.MustCompile(`href="(/e/[\w-]+/r/`+r+`/m/\d+)"`).FindAllStringSubmatch(page, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			links = append(links, m[1])
		}
	}
	for i, l := range links {
		v := url.Values{"oc0": {"4S"}, "od0": {"N"}, "or0": {"="}, "cc0": {"4S"}, "cd0": {"S"}, "cr0": {"-1"}}
		if tie[i] {
			v = url.Values{"oc0": {"PASS"}, "cc0": {"PASS"}}
		}
		page, _ := c.get(l)
		for b := 1; b < len(regexp.MustCompile(`name="oc\d+"`).FindAllString(page, -1)); b++ {
			v.Set(fmt.Sprint("oc", b), "PASS")
			v.Set(fmt.Sprint("cc", b), "PASS")
		}
		c.post(l, v)
	}
	return links
}

func TestTeamsGroupsAndKnockout(t *testing.T) {
	c, ev, done := teamsClient(t, 8, "swiss")
	defer done()
	playRound(c, ev, url.Values{"boards": {"2"}}, nil)
	playRound(c, ev, url.Values{"boards": {"2"}}, nil)
	page, _ := c.get(ev)
	if !strings.Contains(page, "Разделяне на групи") {
		t.Fatal("no split after round 2")
	}
	// A: the top 4 knockout; B: the rest, Swiss, carrying half their VPs
	c.post(ev+"/teams/split", url.Values{"from": {""}, "name1": {"A"}, "count1": {"4"}, "format1": {"knockout"}, "carry1": {"none"},
		"name2": {"B"}, "format2": {"swiss"}, "carry2": {"all"}, "pct2": {"50"}})
	st, _ := c.get(ev + "/standings")
	if !strings.Contains(st, "Група A") || !strings.Contains(st, "Група B") || !strings.Contains(st, "Пренесено") {
		t.Fatal("standings don't show the groups")
	}
	// semis: A plays 4 boards; its first match ends level
	links := playRound(c, ev, url.Values{"boards": {"2"}, "boards0": {"4"}}, map[int]bool{0: true})
	semi := links[0]
	page, _ = c.get(semi)
	if !strings.Contains(page, "Директна елиминация") || !strings.Contains(page, "Допълнителни раздавания") || strings.Count(page, `name="oc`) != 4 {
		t.Fatal("the tied semi-final doesn't offer extra boards (or doesn't play 4 boards)")
	}
	if p, _ := c.get(ev); strings.Contains(p, "Чифтосай рунд 4") {
		t.Fatal("paired past a tie")
	}
	// carry-over 0:2 would decide it, but the director rules it 0:0 and the teams play 2 extra boards: away +3
	c.post(semi+"/carry", url.Values{"carry_home": {"0"}, "carry_away": {"0"}})
	page, _ = c.post(semi+"/playoff", url.Values{"boards": {"2"}, "imps_home": {"0"}, "imps_away": {"3"}})
	if !strings.Contains(page, "Победител") {
		t.Fatal("the extra boards didn't decide the match")
	}
	// the final in A, round 4 of B
	playRound(c, ev, url.Values{"boards": {"2"}, "boards0": {"4"}}, nil) // final and B
	page, _ = c.get(ev)
	if !strings.Contains(page, "Чифтосай рунд 5") {
		t.Fatal("can't pair after the final")
	}
	links = playRound(c, ev, url.Values{"boards": {"2"}}, nil)
	for _, l := range links {
		mp, _ := c.get(l)
		if strings.Contains(mp, "Директна елиминация") {
			t.Error("the finished knockout was paired again")
		}
	}
	st, _ = c.get(ev + "/standings")
	if !strings.Contains(st, "отпадна в рунд 4") || !strings.Contains(st, "в играта") {
		t.Errorf("knockout ranking:\n%s", st)
	}
	xml, _ := c.get(ev + "/teams/usebio.xml")
	if !strings.Contains(xml, "<GROUP>A</GROUP>") && !strings.Contains(xml, `GROUP="A"`) {
		t.Errorf("no groups in the file:\n%s", xml[:min(len(xml), 1500)])
	}
}

func TestTeamsExchange(t *testing.T) {
	c, ev, done := teamsClient(t, 8, "swiss")
	defer done()
	playRound(c, ev, url.Values{"boards": {"2"}}, nil)
	c.post(ev+"/teams/split", url.Values{"from": {""}, "name1": {"A"}, "count1": {"4"}, "format1": {"round-robin"}, "carry1": {"all"},
		"name2": {"B"}, "format2": {"round-robin"}, "carry2": {"all"}})
	playRound(c, ev, url.Values{"boards": {"2"}}, nil)
	before, _ := c.get(ev + "/standings")
	page, _ := c.post(ev+"/teams/exchange", url.Values{"n": {"1"}, "upper": {"A"}, "lower": {"B"}, "carry": {"none"}})
	if !strings.Contains(page, "Група A") {
		t.Fatal("exchange failed")
	}
	after, _ := c.get(ev + "/standings")
	if before == after {
		t.Error("the exchange changed nothing")
	}
	// the movers start from 0 in their new groups: a 0 VP row in each group
	if strings.Count(after, `<td class="num"><b>0</b></td>`) < 2 {
		t.Errorf("movers with no carry:\n%s", after)
	}
	// a bad split is an error page, not a crash
	r, _ := c.http.PostForm(c.base+ev+"/teams/split", url.Values{"t": {c.token}, "from": {"A"}, "name1": {"A"}, "name2": {"A"}})
	if r.StatusCode != http.StatusBadRequest {
		t.Errorf("a split into two groups called A: %d", r.StatusCode)
	}
}

// Matches play whatever boards their table got: the director types the numbers (or BridgeMate brings
// them), a renumbered board moves with its results, and both rooms are compared board by board.
func TestTeamsBoardNumbers(t *testing.T) {
	c, ev, done := teamsClient(t, 4, "swiss")
	defer done()
	page, _ := c.post(ev+"/teams/pair", url.Values{"boards": {"2"}})
	if !strings.Contains(page, "2 раздавания на мач") {
		t.Error("the round doesn't say how many boards a match plays")
	}
	m1 := regexp.MustCompile(`href="(/e/[\w-]+/r/1/m/\d+)"`).FindStringSubmatch(page)[1]
	mp, _ := c.get(m1)
	if !strings.Contains(mp, `name="b0" value="1"`) || !strings.Contains(mp, `name="b1" value="2"`) {
		t.Fatal("an empty match isn't numbered 1, 2")
	}
	// the table played boards 17 and 18: board 17 (none vul) 4S= +420 against 4S-1 -50, 10 IMPs
	mp, _ = c.post(m1, url.Values{"b0": {"17"}, "oc0": {"4S"}, "od0": {"N"}, "or0": {"="}, "cc0": {"4S"}, "cd0": {"S"}, "cr0": {"-1"},
		"b1": {"18"}, "oc1": {"PASS"}, "cc1": {"PASS"}})
	if !strings.Contains(mp, `name="b0" value="17"`) || !strings.Contains(mp, "10 : 0 IMP") || !strings.Contains(mp, " VP") {
		t.Fatalf("boards 17-18: %s", mp[strings.Index(mp, "<h1>"):strings.Index(mp, "<form")])
	}
	// 18 was really 25: the board moves, the match stays complete
	mp, _ = c.post(m1, url.Values{"b0": {"17"}, "oc0": {"4S"}, "od0": {"N"}, "or0": {"="}, "cc0": {"4S"}, "cd0": {"S"}, "cr0": {"-1"},
		"b1": {"25"}, "oc1": {"PASS"}, "cc1": {"PASS"}})
	if !strings.Contains(mp, `name="b1" value="25"`) || strings.Contains(mp, `value="18"`) || !strings.Contains(mp, " VP") {
		t.Error("board 18 wasn't renumbered 25")
	}
	// the same number twice is refused
	mp, _ = c.post(m1, url.Values{"b0": {"17"}, "b1": {"17"}})
	if !strings.Contains(mp, "раздаване 17 е два пъти") {
		t.Error("two rows with board 17")
	}
}
