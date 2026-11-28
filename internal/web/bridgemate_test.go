package web

import (
	"database/sql"
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
	page, _ := c.post(ev+"/teams/pair", url.Values{"boards": {"2"}, "first": {"5"}})
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
	var sections, tables int
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
	// match 2, board 5 open only, then a result entered and erased
	add(1, 2, 5, 0, 0, "E", "3 NT", "+1", false)
	add(2, 2, 5, 0, 0, "E", "3 NT", "=", false)
	add(2, 2, 5, 0, 0, "E", "3 NT", "=", true)
	page, _ = c.post(ev+"/r/1/bridgemate/results", url.Values{})
	if !strings.Contains(page, "5 резултата взети") {
		t.Fatalf("results: %s", regexp.MustCompile(`(?s)<p class="alert">.*?</p>`).FindString(page))
	}
	m1 := regexp.MustCompile(`href="(/e/[\w-]+/r/1/m/\d+)"`).FindStringSubmatch(page)[1]
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
	page, _ := c.post(ev+"/teams/pair", url.Values{"boards": {"2"}, "first": {"1"}})
	if !strings.Contains(page, "само под Windows") || strings.Contains(page, "Създай файла за BridgeMate") {
		t.Error("BridgeMate offered without a host")
	}
}
