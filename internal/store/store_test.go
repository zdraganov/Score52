package store

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/zdraganov/Score52/internal/movement"
	"github.com/zdraganov/Score52/internal/pairs"
	"github.com/zdraganov/Score52/internal/score"
	"github.com/zdraganov/Score52/internal/teams"
	"github.com/zdraganov/Score52/internal/usebio"
)

func open(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "event.score52")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return s, path
}

func TestMigrateTwice(t *testing.T) {
	s, path := open(t)
	s.SetMeta(MetaTitle, "Купа София")
	s.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if v, _ := s.Meta(MetaTitle); v != "Купа София" {
		t.Errorf("meta after reopen: %q", v)
	}
	if v, _ := s.Meta("nothing"); v != "" {
		t.Errorf("unset meta: %q", v)
	}
}

func TestNewerFileRefused(t *testing.T) {
	s, path := open(t)
	s.db.Exec(`PRAGMA user_version = 99`)
	s.Close()
	if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "newer version") {
		t.Errorf("newer schema: %v", err)
	}
}

// A 5-table Mitchell from entries to the file bridge.bg reads: save, reopen, score, export.
func TestPairsSessionEndToEnd(t *testing.T) {
	s, path := open(t)
	var ids []int64
	for i := 1; i <= 10; i++ {
		a, _ := s.AddPlayer(Player{Name: fmt.Sprintf("Играч %dА", i), FederationID: fmt.Sprint(1000 + 2*i)})
		b, _ := s.AddPlayer(Player{Name: fmt.Sprintf("Играч %dБ", i), FederationID: fmt.Sprint(1001 + 2*i)})
		id, err := s.AddEntry("", i, []int64{a, b})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	m, err := movement.NewMitchell(5, 5, movement.MitchellOptions{})
	if err != nil {
		t.Fatal(err)
	}
	seating := make([]int64, m.Pairs)
	copy(seating, ids)
	sid, err := s.SaveSession(Session{Slot: "s1", Name: "Сесия 1", Scoring: string(pairs.MethodMP), BoardsPerRound: 3, Movement: m, Seating: seating})
	if err != nil {
		t.Fatal(err)
	}

	rng := rand.New(rand.NewPCG(1, 1))
	four, _ := score.ParseContract("4S")
	for _, seat := range m.Seats {
		for _, b := range movement.Boards(seat.Set, 3) {
			tricks := 8 + rng.IntN(5)
			r := Result{Board: b, Round: seat.Round, Table: seat.Table, NS: seat.NS, EW: seat.EW, Contract: "4S", Declarer: "N",
				Tricks: tricks, Score: score.NSScore(b, four, true, tricks), Source: "bridgemate"}
			if err := s.PutResult(sid, r); err != nil {
				t.Fatal(err)
			}
		}
	}
	// the director corrects one line and gives another an adjusted 60/40
	first := m.Seats[0]
	s.PutResult(sid, Result{Board: 1, Round: 1, Table: 1, NS: first.NS, EW: first.EW, Contract: "3NT", Declarer: "S", Tricks: 9, Score: 400})
	sixty, forty := 60.0, 40.0
	s.PutResult(sid, Result{Board: 2, Round: 1, Table: 1, NS: first.NS, EW: first.EW, AdjNS: &sixty, AdjEW: &forty})
	s.Close()

	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	sess, err := s.Sessions()
	if err != nil || len(sess) != 1 {
		t.Fatalf("sessions: %v %v", sess, err)
	}
	x := sess[0]
	if h, _ := s.History(x.ID); len(h) != 2 || h[0].Board != 2 || !strings.Contains(h[1].Old, `"contract":"4S"`) {
		t.Errorf("history: %+v", h)
	}
	rows, err := s.Results(x.ID)
	if err != nil || len(rows) != 75 {
		t.Fatalf("results: %d %v", len(rows), err)
	}
	var lines []pairs.Line
	for _, r := range rows {
		l := pairs.Line{Board: r.Board, NS: r.NS, EW: r.EW, Played: r.AdjNS == nil, Score: r.Score}
		if r.AdjNS != nil {
			l.AdjNS, l.AdjEW = *r.AdjNS, *r.AdjEW
		}
		lines = append(lines, l)
	}
	res, err := pairs.ScoreSession(x.Movement, x.BoardsPerRound, x.FirstBoard, pairs.Method(x.Scoring), 0, lines)
	if err != nil {
		t.Fatal(err)
	}
	if tt := res.Totals[1]; tt.Boards != 15 || tt.Top != 15*8 {
		t.Errorf("pair 1 totals: %+v", tt)
	}

	// export: ranking by percentage, travellers with the session's pair numbers
	entries, _ := s.Entries()
	byID := map[int64]Entry{}
	for _, e := range entries {
		byID[e.ID] = e
	}
	ev := usebio.PairsEvent{Title: "Сесия 1", Method: usebio.MatchPoints}
	for num := 1; num <= x.Movement.Pairs; num++ {
		tot := res.Totals[num]
		p := usebio.Pair{Number: num, Score: tot.Score, Percent: 100 * tot.Score / tot.Top, Boards: tot.Boards}
		if num <= 5 {
			p.Direction = "NS"
		} else {
			p.Direction = "EW"
		}
		for _, pl := range byID[x.Seating[num-1]].Players {
			p.Players = append(p.Players, usebio.Player{Name: pl.Name, FederationID: pl.FederationID})
		}
		ev.Pairs = append(ev.Pairs, p)
	}
	sort.Slice(ev.Pairs, func(i, j int) bool { return ev.Pairs[i].Percent > ev.Pairs[j].Percent })
	for i := range ev.Pairs {
		ev.Pairs[i].Place = i + 1
	}
	byBoard := map[int]*usebio.Board{}
	for _, l := range res.Lines {
		if byBoard[l.Board] == nil {
			byBoard[l.Board] = &usebio.Board{Number: l.Board}
		}
		byBoard[l.Board].Lines = append(byBoard[l.Board].Lines, usebio.Line{NS: l.NS, EW: l.EW, Played: true, NSPoints: l.NSPoints, EWPoints: l.EWPoints})
	}
	for b := 1; b <= 15; b++ {
		ev.Boards = append(ev.Boards, *byBoard[b])
	}
	var buf bytes.Buffer
	if err := usebio.WritePairs(&buf, ev); err != nil {
		t.Fatal(err)
	}
	siteParses(t, buf.Bytes(), 10)
}

// siteParses runs bridge.bg's UsebioParser on the file, when the site's repository is next to this one.
func siteParses(t *testing.T, xml []byte, rows int) {
	t.Helper()
	site := os.Getenv("BRIDGEBG_DIR")
	if site == "" {
		site = "../../../bridge.bg"
	}
	dir, _ := filepath.Abs(filepath.Join(site, "next/src/Results"))
	if _, err := os.Stat(dir); err != nil {
		t.Skip("no bridge.bg repository")
	}
	if _, err := exec.LookPath("php"); err != nil {
		t.Skip("no php")
	}
	f := filepath.Join(t.TempDir(), "1.s1.xml")
	os.WriteFile(f, xml, 0o644)
	out, err := exec.Command("php", "-r", `require $argv[1]."/PbnParser.php"; require $argv[1]."/UsebioParser.php";
		$v = (new App\Results\UsebioParser())->parse($argv[2]); echo $v === null ? "null" : count($v["ranking"]);`, dir, f).CombinedOutput()
	if err != nil || string(out) != fmt.Sprint(rows) {
		t.Errorf("site parser: %s %v", out, err)
	}
}

func TestTeamsSaveLoad(t *testing.T) {
	s, path := open(t)
	var ts []teams.Team
	for i := 1; i <= 8; i++ {
		ts = append(ts, teams.Team{ID: teams.TeamID(i), Name: fmt.Sprintf("Отбор %d", i), Seed: i})
	}
	e, err := teams.New(ts, teams.Swiss, "wbf20", 12)
	if err != nil {
		t.Fatal(err)
	}
	for r := 0; r < 3; r++ {
		ms, err := e.PairRound(10)
		if err != nil {
			t.Fatal(err)
		}
		for i, m := range ms {
			e.Record(m.Round, m.Home, m.Away, float64(10+i*3), float64(20-i))
		}
		if err := s.SaveTeams(e); err != nil { // saved after every round, as the UI will
			t.Fatal(err)
		}
	}
	e.Split("", []teams.GroupSpec{{Name: "A", Count: 4, Format: teams.Knockout}, {Name: "B", Format: teams.Swiss, Carry: teams.Carry{Kind: teams.CarryAll}}})
	s.SaveTeams(e)
	s.Close()

	s, _ = Open(path)
	defer s.Close()
	got, err := s.LoadTeams()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Log) != len(e.Log) || fmt.Sprint(got.Standings("B")) != fmt.Sprint(e.Standings("B")) {
		t.Errorf("loaded event differs")
	}
	// another event's (shorter) log can't overwrite the file
	other, _ := teams.New(ts, teams.Swiss, "wbf20", 12)
	if err := s.SaveTeams(other); err == nil {
		t.Error("shorter log accepted")
	}
}

func TestTeamBoards(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.score52"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	b := TeamBoard{Round: 1, Home: 1, Away: 2, Room: RoomOpen, Board: 3, Contract: "4S", Declarer: "N", Tricks: 10, Score: 620}
	for _, x := range []TeamBoard{b, b} { // the same twice: no history
		if err := s.PutTeamBoard(x); err != nil {
			t.Fatal(err)
		}
	}
	b.Tricks, b.Score = 9, -100
	if err := s.PutTeamBoard(b); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteTeamBoard(1, 1, 2, RoomOpen, 3); err != nil {
		t.Fatal(err)
	}
	var n int
	s.db.QueryRow(`SELECT count(*) FROM team_board_history`).Scan(&n)
	got, _ := s.TeamBoards(0)
	if n != 2 || len(got) != 0 {
		t.Errorf("history %d (want 2: the correction and the delete), %d boards left", n, len(got))
	}
}
