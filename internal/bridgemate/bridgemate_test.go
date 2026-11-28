package bridgemate

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zdraganov/Score52/internal/movement"
	"github.com/zdraganov/Score52/internal/pairs"
	"github.com/zdraganov/Score52/internal/score"

	"github.com/zdraganov/Score52/internal/bridgemate/bwstest"
)

// fakeBws is the template's tables as SQLite (bwstest), so Write and ReadReceived run unchanged.
func fakeBws(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "x.bws")
	if err := bwstest.Create(path); err != nil {
		t.Fatal(err)
	}
	db, err := bwstest.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func mitchellSetup(t *testing.T) (movement.Movement, Setup) {
	t.Helper()
	m, err := movement.NewMitchell(5, 5, movement.MitchellOptions{})
	if err != nil {
		t.Fatal(err)
	}
	players := map[int][2]Player{
		1: {{"1001", "Иван Иванов"}, {"1002", "Петър Константинов-Стоянов"}},
		6: {{"1011", "Мария Георгиева"}, {"1012", "Анна Димитрова"}},
	}
	s, err := PairsSetup("Купа на федерацията 2026 - първа сесия, двойки", time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC),
		PairsSection{Letter: "A", Movement: m, BoardsPerRound: 3, Scoring: pairs.MethodMP, Players: players})
	if err != nil {
		t.Fatal(err)
	}
	return m, s
}

func TestPairsSetup(t *testing.T) {
	_, s := mitchellSetup(t)
	if len([]rune(s.Session.Name)) != 40 {
		t.Errorf("session name not clipped to 40: %q", s.Session.Name)
	}
	sec := s.Sections[0]
	if sec.Tables != 5 || sec.Winners != 2 || sec.Scoring != ScoringMP || sec.MissingPair != 0 {
		t.Errorf("section: %+v", sec)
	}
	if len(s.Rounds) != 25 || len(s.Tables) != 5 {
		t.Fatalf("%d rounds, %d tables", len(s.Rounds), len(s.Tables))
	}
	r := s.Rounds[0] // table 1 round 1: NS 1 v EW 6 on boards 1-3
	if r.NS != 1 || r.EW != 6 || r.LowBoard != 1 || r.HighBoard != 3 {
		t.Errorf("first round: %+v", r)
	}
	if len(s.Players) != 4 || s.Players[1].Name != "Петър Константинов" || s.Players[2].Direction != "E" {
		t.Errorf("players: %+v", s.Players)
	}
}

func TestPhantomAndLimits(t *testing.T) {
	m, _ := movement.NewHowell(9, 9, false) // 9 pairs: a phantom
	s, err := PairsSetup("x", time.Time{}, PairsSection{Letter: "A", Movement: m, BoardsPerRound: 3})
	if err != nil {
		t.Fatal(err)
	}
	if s.Sections[0].MissingPair != 10 || s.Sections[0].Winners != 1 {
		t.Errorf("phantom: %+v", s.Sections[0])
	}
	big, _ := movement.NewHowell(14, 13, false)
	if _, err := PairsSetup("x", time.Time{}, PairsSection{Letter: "A", Movement: big, BoardsPerRound: 10}); err == nil ||
		!strings.Contains(err.Error(), "127") {
		t.Errorf("130 boards accepted: %v", err)
	}
}

func TestWriteAndRead(t *testing.T) {
	db := fakeBws(t)
	m, s := mitchellSetup(t)
	if err := Write(db, s, "MAGIC-VM"); err != nil {
		t.Fatal(err)
	}
	// rewriting (a changed movement) replaces, keeps the client and the session's GUID
	db.Exec("UPDATE [Session] SET [GUID] = 'D9E6F8EE3BD04B03A395CD7E8E1F8219'")
	s.Session.GUID = ""
	if err := Write(db, s, "MAGIC-VM"); err != nil {
		t.Fatal(err)
	}
	var clients, rounds int
	var guid, name string
	db.QueryRow("SELECT COUNT(*) FROM [Clients]").Scan(&clients)
	db.QueryRow("SELECT COUNT(*) FROM [RoundData]").Scan(&rounds)
	db.QueryRow("SELECT [GUID] FROM [Session]").Scan(&guid)
	db.QueryRow("SELECT [Name] FROM [PlayerNumbers] WHERE [Number] = '1001'").Scan(&name)
	if clients != 1 || rounds != 25 || guid != "D9E6F8EE3BD04B03A395CD7E8E1F8219" || name != "Иван Иванов" {
		t.Errorf("clients %d, rounds %d, guid %q, name %q", clients, rounds, guid, name)
	}
	if !hasColumn(db, "PlayerNumbers", "EntrySource") {
		t.Error("EntrySource not added")
	}

	// results as BCS writes them: table 1 round 1 (pairs 1 v 6, boards 1-3)
	ins := func(table, round, board, ns, ew, decl int, dir, contract, result, remarks string, erased bool) {
		t.Helper()
		if _, err := db.Exec("INSERT INTO [ReceivedData] ([Section], [Table], [Round], [Board], [PairNS], [PairEW], [Declarer], [NS/EW], [Contract], [Result], [LeadCard], [Remarks], [Erased]) VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?, ?)",
			table, round, board, ns, ew, decl, dir, contract, result, remarks, erased); err != nil {
			t.Fatal(err)
		}
	}
	ins(1, 1, 1, 1, 6, 1, "S", "6 NT", "=", "", false)     // NS 990 (board 1 none vul)
	ins(1, 1, 2, 1, 6, 6, "W", "4 S", "+1", "", false)     // EW 450 (board 2 NS vul)
	ins(1, 1, 2, 1, 6, 6, "W", "4 S", "=", "", false)      // corrected: EW 420
	ins(1, 1, 3, 1, 6, 1, "N", "3 NT", "-2", "", false)    // entered, then erased
	ins(1, 1, 3, 1, 6, 1, "N", "3 NT", "-2", "", true)     //
	ins(2, 1, 4, 2, 7, 0, "", "", "", "60%-40%", false)    // adjusted
	ins(3, 1, 7, 3, 8, 0, "", "", "", "Not played", false) // TD to settle
	ins(4, 1, 10, 4, 9, 4, "N", "1 C xx", "-1", "", false) // NS -400 (board 10 all vul, XX down 1)
	ins(5, 1, 13, 5, 10, 10, "E", "PASS", "", "", false)   // passed out

	rows, err := ReadReceived(db)
	if err != nil {
		t.Fatal(err)
	}
	boards, errs := Current(rows)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	got := map[int]Board{}
	for _, b := range boards {
		got[b.Received.Board] = b
	}
	if len(boards) != 6 || got[3].Received.Board != 0 {
		t.Fatalf("%d boards (board 3 erased): %+v", len(boards), boards)
	}
	if got[1].ScoreNS != 990 || got[2].ScoreNS != -420 || got[10].ScoreNS != -400 || got[13].ScoreNS != 0 {
		t.Errorf("scores: 1:%d 2:%d 10:%d 13:%d", got[1].ScoreNS, got[2].ScoreNS, got[10].ScoreNS, got[13].ScoreNS)
	}
	if got[4].Status != Adjusted || got[4].AdjNS != 60 || got[7].Status != NotPlayed {
		t.Errorf("adjusted / not played: %+v %+v", got[4], got[7])
	}

	// the lines score as a session
	var lines []pairs.Line
	for _, b := range boards {
		if l, ok := b.Line(); ok {
			lines = append(lines, l)
		}
	}
	res, err := pairs.ScoreSession(m, 3, 1, pairs.MethodMP, 0, lines)
	if err != nil {
		t.Fatal(err)
	}
	if res.Totals[1].Boards != 2 { // boards 1 and 2 of pair 1; board 3 erased
		t.Errorf("pair 1: %+v", res.Totals[1])
	}
}

func TestParseErrors(t *testing.T) {
	for _, r := range []Received{
		{Contract: "7 NT", Result: "+1", Direction: "N"},
		{Contract: "4 S", Result: "", Direction: "N"},
		{Contract: "4 Q", Result: "=", Direction: "N"},
		{Contract: "4 S", Result: "=", Declarer: 99, PairNS: 1, PairEW: 2},
		{Remarks: "something"},
	} {
		if _, err := Parse(r); err == nil {
			t.Errorf("accepted %+v", r)
		}
	}
	// declarer from the pair number when the seat is missing
	b, err := Parse(Received{Board: 1, Contract: "4 S", Result: "=", Declarer: 2, PairNS: 1, PairEW: 2})
	if err != nil || b.DeclarerNS || b.ScoreNS != -score.Score(score.Contract{Level: 4, Strain: score.Spades}, false, 10) {
		t.Errorf("%+v %v", b, err)
	}
}

// A teams round as Magic Contest lays it out: A open room, B closed room, Group = match; written,
// then results of both rooms read back by section and table.
func TestTeamsRound(t *testing.T) {
	ms := []TeamMatch{
		{Table: 1, Home: 3, Away: 5, Boards: 8, Open: [4]Player{{"101", "Иван Петров"}, {"102", "Петя"}, {"", "Гост 1"}, {"", "Гост 2"}}},
		{Table: 2, Home: 1, Away: 4, Boards: 8},
	}
	s, err := TeamsSetup("Отборно, рунд 2", time.Date(2026, 12, 5, 0, 0, 0, 0, time.UTC), 9, ms)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Sections) != 2 || s.Sections[0].Scoring != ScoringTeams || len(s.Tables) != 4 || len(s.Players) != 4 {
		t.Fatalf("setup: %+v", s)
	}
	for _, r := range s.Rounds {
		if r.Round != 1 || r.LowBoard != 9 || r.HighBoard != 16 {
			t.Errorf("round row %+v", r)
		}
	}
	// match 1: open room NS = home 3, closed room NS = away 5
	if s.Rounds[0].NS != 3 || s.Rounds[0].EW != 5 || s.Rounds[1].Section != SectionClosed || s.Rounds[1].NS != 5 || s.Rounds[1].EW != 3 {
		t.Errorf("rooms: %+v %+v", s.Rounds[0], s.Rounds[1])
	}
	db := fakeBws(t)
	if err := Write(db, s, "PC"); err != nil {
		t.Fatal(err)
	}
	var groups int
	db.QueryRow("SELECT count(DISTINCT [Group]) FROM [Tables]").Scan(&groups)
	if groups != 2 {
		t.Errorf("%d groups", groups)
	}
	for _, q := range []string{
		"INSERT INTO [ReceivedData] ([Section],[Table],[Round],[Board],[PairNS],[PairEW],[Declarer],[NS/EW],[Contract],[Result],[Erased]) VALUES (1,1,1,9,3,5,3,'N','4 S','=',0)",
		"INSERT INTO [ReceivedData] ([Section],[Table],[Round],[Board],[PairNS],[PairEW],[Declarer],[NS/EW],[Contract],[Result],[Erased]) VALUES (2,1,1,9,5,3,5,'S','4 S','-1',0)",
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := ReadReceived(db)
	if err != nil {
		t.Fatal(err)
	}
	boards, errs := Current(rows)
	if len(errs) > 0 || len(boards) != 2 {
		t.Fatalf("%d boards, errors %v", len(boards), errs)
	}
	// board 9 (EW vulnerable): open +420 for the home team NS, closed -50 for NS = the away team
	got := map[int]int{}
	for _, b := range boards {
		got[b.Section] = b.ScoreNS
	}
	if got[SectionOpen] != 420 || got[SectionClosed] != -50 {
		t.Errorf("scores %v", got)
	}
}
