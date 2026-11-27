package bridgemate

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bridge.bg/scorer/internal/movement"
	"bridge.bg/scorer/internal/pairs"
	"bridge.bg/scorer/internal/score"

	_ "modernc.org/sqlite"
)

// fakeBws is the template's tables as SQLite: the same names and [bracketed] SQL, so Write and
// ReadReceived run unchanged. PlayerNumbers lacks EntrySource and Round, as the template does.
func fakeBws(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "x.bws"))
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		"CREATE TABLE [Clients] ([ID] INTEGER PRIMARY KEY AUTOINCREMENT, [Computer] TEXT)",
		"CREATE TABLE [Session] ([ID] INTEGER, [Name] TEXT, [Date] DATETIME, [Time] DATETIME, [GUID] TEXT, [Status] INTEGER, [ShowInApp] BOOLEAN, [PairsMoveAcrossField] BOOLEAN, [EWReturnHome] BOOLEAN)",
		"CREATE TABLE [Section] ([ID] INTEGER, [Letter] TEXT, [Tables] INTEGER, [MissingPair] INTEGER, [EWMoveBeforePlay] INTEGER, [Session] INTEGER, [ScoringType] INTEGER, [Winners] INTEGER)",
		"CREATE TABLE [Tables] ([Section] INTEGER, [Table] INTEGER, [ComputerID] INTEGER, [Status] INTEGER, [LogOnOff] INTEGER, [CurrentRound] INTEGER, [CurrentBoard] INTEGER, [UpdateFromRound] INTEGER, [Group] INTEGER)",
		"CREATE TABLE [RoundData] ([Section] INTEGER, [Table] INTEGER, [Round] INTEGER, [NSPair] INTEGER, [EWPair] INTEGER, [LowBoard] INTEGER, [HighBoard] INTEGER, [CustomBoards] TEXT CHECK ([CustomBoards] IS NULL OR [CustomBoards] <> ''))",
		"CREATE TABLE [PlayerNumbers] ([Section] INTEGER, [Table] INTEGER, [Direction] TEXT, [Number] TEXT, [Name] TEXT, [Updated] BOOLEAN, [TimeLog] DATETIME, [Processed] BOOLEAN)",
		"CREATE TABLE [ReceivedData] ([ID] INTEGER PRIMARY KEY AUTOINCREMENT, [Section] INTEGER, [Table] INTEGER, [Round] INTEGER, [Board] INTEGER, [PairNS] INTEGER, [PairEW] INTEGER, [Declarer] INTEGER, [NS/EW] TEXT, [Contract] TEXT, [Result] TEXT, [LeadCard] TEXT, [Remarks] TEXT, [DateLog] DATETIME, [TimeLog] DATETIME, [Processed] BOOLEAN, [Erased] BOOLEAN, [custom_ProcessedMC] BOOLEAN)",
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
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
