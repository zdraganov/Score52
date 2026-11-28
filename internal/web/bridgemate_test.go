package web

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/zdraganov/Score52/internal/bridgemate/bwstest"
)

// fakeHost is BridgeMate without Windows: SQLite files with the template's tables; Start only notes it.
type fakeHost struct{ started []string }

func (h *fakeHost) Create(path string) error          { return bwstest.Create(path) }
func (h *fakeHost) Open(path string) (*sql.DB, error) { return bwstest.Open(path) }
func (h *fakeHost) Computer() (string, error)         { return "TD-PC", nil }
func (h *fakeHost) Start(path string) error           { h.started = append(h.started, path); return nil }

func TestTeamsBridgemate(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	host := &fakeHost{}
	s.SetBridgemate(host)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	c := &client{t: t, base: ts.URL, http: &http.Client{}, token: s.token}
	_, ev := c.post("/events", url.Values{"title": {"Отборно"}, "date": {"2026-12-05"}, "kind": {"teams"}})
	for i := 1; i <= 4; i++ {
		v := url.Values{"name": {"Отбор " + string(rune('0'+i))}}
		for k := 1; k <= 4; k++ {
			v.Set("p"+string(rune('0'+k)), "Играч")
		}
		c.post(ev+"/teams", v)
	}
	c.post(ev+"/teams/start", url.Values{"format": {"swiss"}, "boards": {"2"}, "bye_vp": {"12"}})
	page, _ := c.post(ev+"/teams/pair", url.Values{"boards": {"2"}})
	if !strings.Contains(page, "Създай файла за BridgeMate") {
		t.Fatal("no BridgeMate on the round")
	}
	page, _ = c.post(ev+"/r/1/bridgemate", url.Values{"start": {"1"}})
	bws := filepath.Join(dir, strings.TrimPrefix(ev, "/e/")+"-r1.bws")
	if _, err := os.Stat(bws); err != nil || len(host.started) != 1 || !strings.Contains(page, "BCS е пуснат") {
		t.Fatalf("file %v, started %v", err, host.started)
	}
	db, _ := bwstest.Open(bws)
	defer db.Close()
	var sections, tables, manual int
	db.QueryRow("SELECT count(*) FROM [Settings] WHERE [BM2FirstBoardManually]").Scan(&manual)
	if manual != 2 {
		t.Errorf("%d sections where the tables key in their first board", manual)
	}
	var low, high int
	db.QueryRow("SELECT min([LowBoard]), max([HighBoard]) FROM [RoundData] WHERE [NSPair] > 0").Scan(&low, &high)
	if low != 1 || high != 32 {
		t.Errorf("tables get boards %d-%d, want 1-32", low, high)
	}
	db.QueryRow("SELECT count(*) FROM [Section] WHERE [ScoringType] = 4").Scan(&sections)
	db.QueryRow("SELECT count(*) FROM [Tables]").Scan(&tables)
	if sections != 2 || tables != 4 {
		t.Fatalf("%d team sections, %d tables", sections, tables)
	}
	// table 1's teams, as the file has them
	var homeNo, awayNo int
	db.QueryRow("SELECT [NSPair], [EWPair] FROM [RoundData] WHERE [Section] = 1 AND [Table] = 1").Scan(&homeNo, &awayNo)
	add := func(sec, table, board int, ns, ew int, decl, contract, result string, erased bool) {
		t.Helper()
		if _, err := db.Exec("INSERT INTO [ReceivedData] ([Section],[Table],[Round],[Board],[PairNS],[PairEW],[Declarer],[NS/EW],[Contract],[Result],[Erased]) VALUES (?,?,1,?,?,?,?,?,?,?,?)",
			sec, table, board, ns, ew, ns, decl, contract, result, erased); err != nil {
			t.Fatal(err)
		}
	}
	// match 1, boards 5 and 6 in both rooms: board 5 (NS vul) open 4S= +620, closed 4S-1 -100: 12 IMPs home
	add(1, 1, 5, homeNo, awayNo, "N", "4 S", "=", false)
	add(2, 1, 5, awayNo, homeNo, "S", "4 S", "-1", false)
	add(1, 1, 6, homeNo, awayNo, "", "PASS", "", false)
	add(2, 1, 6, awayNo, homeNo, "", "PASS", "", false)
	// match 2 plays other boards: 25 open only (the closed room entered it, then erased it)
	add(1, 2, 25, 0, 0, "E", "3 NT", "+1", false)
	add(2, 2, 25, 0, 0, "E", "3 NT", "=", false)
	add(2, 2, 25, 0, 0, "E", "3 NT", "=", true)
	page, _ = c.post(ev+"/r/1/bridgemate/results", url.Values{})
	if !strings.Contains(page, "5 резултата взети") {
		t.Fatalf("results: %s", regexp.MustCompile(`(?s)<p class="alert">.*?</p>`).FindString(page))
	}
	ms := regexp.MustCompile(`href="(/e/[\w-]+/r/1/m/\d+)"`).FindAllStringSubmatch(page, -1)
	m1 := ms[0][1]
	if m2, _ := c.get(ms[len(ms)-1][1]); !strings.Contains(m2, `name="b0" value="25"`) {
		t.Error("match 2's board 25 isn't on its page")
	}
	mp, _ := c.get(m1)
	if !strings.Contains(mp, "12 : 0 IMP") || !strings.Contains(mp, " VP") {
		t.Fatalf("match 1 from BridgeMate: %s", mp[strings.Index(mp, "<h1>"):strings.Index(mp, "<form")])
	}
	// the TD corrects board 5 closed room here (4S= too: 0 IMPs); BridgeMate doesn't overwrite it
	c.post(m1, url.Values{"oc0": {"4S"}, "od0": {"N"}, "or0": {"="}, "cc0": {"4S"}, "cd0": {"S"}, "cr0": {"="}, "oc1": {"PASS"}, "cc1": {"PASS"}})
	page, _ = c.post(ev+"/r/1/bridgemate/results", url.Values{})
	if !strings.Contains(page, "1 поправени тук остават") {
		t.Errorf("the correction wasn't kept: %s", regexp.MustCompile(`(?s)<p class="alert">.*?</p>`).FindString(page))
	}
	mp, _ = c.get(m1)
	if !strings.Contains(mp, "0 : 0 IMP") {
		t.Error("BridgeMate overwrote the director's correction")
	}
	// an erased result: the board goes when it's erased on the Bridgemate
	add(1, 1, 6, homeNo, awayNo, "", "", "", true)
	c.post(ev+"/r/1/bridgemate/results", url.Values{})
	mp, _ = c.get(m1)
	if strings.Count(mp, `value="PASS"`) != 1 {
		t.Errorf("the erased board 6 (open room) is still there: %d PASS", strings.Count(mp, `value="PASS"`))
	}
}

func TestBridgemateNeedsWindows(t *testing.T) {
	c, ev, done := teamsClient(t, 4, "swiss")
	defer done()
	page, _ := c.post(ev+"/teams/pair", url.Values{"boards": {"2"}})
	if !strings.Contains(page, "само под Windows") || strings.Contains(page, "Създай файла за BridgeMate") {
		t.Error("BridgeMate offered without a host")
	}
}

// A pairs session in two sections: one file, BCS's sections A and B numbered from 1 each, the names
// at the first-round tables and every player by number; results come back into the travellers.
func TestSessionBridgemate(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	host := &fakeHost{}
	s.SetBridgemate(host)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	c := &client{t: t, base: ts.URL, http: &http.Client{}, token: s.token}
	_, ev := c.post("/events", url.Values{"title": {"51 двойки"}, "date": {"2026-11-30"}})
	for i := 1; i <= 51; i++ {
		c.post(ev+"/pairs", url.Values{"name1": {fmt.Sprint("Първи ", i)}, "ibn1": {fmt.Sprint(1000 + 2*i)},
			"name2": {fmt.Sprint("Втори ", i)}, "ibn2": {fmt.Sprint(1001 + 2*i)}})
	}
	plan, sess := c.post(ev+"/sessions", url.Values{"kind": {"sections"}, "sections": {"M13/25,M13/26"}, "rounds": {"13"}, "bpr": {"2"}, "method": {"mp"}})
	if !strings.Contains(plan, "Създай файла за BridgeMate") {
		t.Fatal("no BridgeMate on the session")
	}
	page, _ := c.post(sess+"/bridgemate", url.Values{"start": {"1"}})
	bws := filepath.Join(dir, strings.TrimPrefix(ev, "/e/")+"-s1.bws")
	if len(host.started) != 1 || host.started[0] != bws || !strings.Contains(page, "BCS е пуснат") {
		t.Fatalf("started %v", host.started)
	}
	db, _ := bwstest.Open(bws)
	defer db.Close()
	count := func(q string, args ...any) (n int) {
		t.Helper()
		if err := db.QueryRow(q, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count("SELECT count(*) FROM [Section]"); n != 2 {
		t.Fatalf("%d sections", n)
	}
	var letter string
	var missing, tables int
	db.QueryRow("SELECT [Letter], [MissingPair], [Tables] FROM [Section] WHERE [ID] = 1").Scan(&letter, &missing, &tables)
	if letter != "A" || missing == 0 || tables != 13 {
		t.Errorf("section A: %s, missing pair %d, %d tables", letter, missing, tables)
	}
	if n := count("SELECT max([Table]) FROM [Tables] WHERE [Section] = 2"); n != 13 {
		t.Errorf("section B's tables go up to %d", n)
	}
	if n := count("SELECT max([NSPair]) + max([EWPair]) FROM [RoundData] WHERE [Section] = 2"); n > 52 {
		t.Errorf("section B's pairs aren't numbered from 1 (%d)", n)
	}
	if n := count("SELECT count(*) FROM [PlayerNumbers]"); n != 102 {
		t.Errorf("%d names at the tables, want 102", n)
	}
	if n := count("SELECT count(*) FROM [PlayerNames] WHERE [ID] = 1003 AND [strID] = '1003' AND [Name] = 'Втори 1'"); n != 1 {
		t.Error("no PlayerNames row for 1003")
	}
	if n := count("SELECT count(*) FROM [PlayerNames]"); n != 102 {
		t.Errorf("%d names by number", n)
	}

	// section B, table 1, round 1: its pairs and first board
	var ns, ew, low int
	db.QueryRow("SELECT [NSPair], [EWPair], [LowBoard] FROM [RoundData] WHERE [Section] = 2 AND [Table] = 1 AND [Round] = 1").Scan(&ns, &ew, &low)
	add := func(board int, decl, contract, result, remarks string, erased bool) {
		t.Helper()
		if _, err := db.Exec("INSERT INTO [ReceivedData] ([Section],[Table],[Round],[Board],[PairNS],[PairEW],[Declarer],[NS/EW],[Contract],[Result],[LeadCard],[Remarks],[Erased]) VALUES (2,1,1,?,?,?,?,?,?,?,'SA',?,?)",
			board, ns, ew, ns, decl, contract, result, remarks, erased); err != nil {
			t.Fatal(err)
		}
	}
	read := func(want string) {
		t.Helper()
		page, _ := c.post(sess+"/bridgemate/results", url.Values{})
		if !strings.Contains(page, want) {
			t.Fatalf("want %q: %s", want, regexp.MustCompile(`(?s)<p class="alert">.*?</p>`).FindString(page))
		}
	}
	add(low, "N", "4 S", "=", "", false)
	add(low+1, "", "", "", "60-40", false)
	add(low+1, "", "", "", "Not played", false) // re-entered: the latest counts
	read("1 резултата взети; 0 поправени тук остават както са; 1 чакат")
	trav := fmt.Sprintf("%s/b/%d", sess, low)
	tp, _ := c.get(trav)
	if !strings.Contains(tp, `value="4S"`) {
		t.Fatal("4S from BridgeMate isn't on the traveller")
	}
	// saving the traveller unchanged leaves the line BridgeMate's: a new result from the table replaces it
	form := url.Values{}
	for _, m := range regexp.MustCompile(`name="([cr]\d+)" value="([^"]*)"`).FindAllStringSubmatch(tp, -1) {
		form.Set(m[1], m[2])
	}
	i := regexp.MustCompile(`name="c(\d+)" value="4S"`).FindStringSubmatch(tp)[1]
	form.Set("d"+i, "N")
	c.post(trav, form)
	add(low, "N", "4 S", "+1", "", false)
	read("1 резултата взети; 0 поправени")
	// the director's correction stays
	form.Set("r"+i, "-1")
	c.post(trav, form)
	add(low, "N", "4 S", "+2", "", false)
	read("0 резултата взети; 1 поправени тук остават")
	if tp, _ = c.get(trav); !strings.Contains(tp, `value="-1"`) {
		t.Error("BridgeMate overwrote the correction")
	}
	// erased on the Bridgemate: a line from BridgeMate goes
	form.Set("c"+i, "")
	form.Set("r"+i, "")
	form.Del("d" + i)
	c.post(trav, form) // the director clears it: BridgeMate gives it back
	read("1 резултата взети")
	add(low, "", "", "", "", true)
	read("0 резултата взети")
	if tp, _ = c.get(trav); strings.Contains(tp, `value="4S"`) {
		t.Error("the erased result is still on the traveller")
	}
}
