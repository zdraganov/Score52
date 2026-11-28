package bridgemate

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// The .bws is reached through database/sql: on Windows the 32-bit Access ODBC driver (open.go); in
// the tests SQLite, which takes the same [bracketed] SQL. Values always go as parameters: Cyrillic
// written into the SQL text itself does not survive the Access driver as Unicode.

// columnsAdded are the columns newer BCS versions expect that Bridgemate's template lacks.
var columnsAdded = []struct{ table, column, typ string }{
	{"PlayerNumbers", "EntrySource", "SHORT"},
	{"PlayerNumbers", "Round", "SHORT"},
}

func hasColumn(db *sql.DB, table, column string) bool {
	rows, err := db.Query(fmt.Sprintf("SELECT [%s] FROM [%s] WHERE 1=0", column, table))
	if err != nil {
		return false
	}
	rows.Close()
	return true
}

// Write puts a setup into a .bws made from the template: the client computer, the session,
// sections, tables, rounds, the names at the tables and the names by number (PlayerNames).
// Section, Tables and RoundData are emptied first, so a changed movement is written the same way
// (BCS then needs UpdateFromRound, see docs).
func Write(db *sql.DB, s Setup, computer string) error {
	for _, c := range columnsAdded {
		if !hasColumn(db, c.table, c.column) {
			if _, err := db.Exec(fmt.Sprintf("ALTER TABLE [%s] ADD COLUMN [%s] %s", c.table, c.column, c.typ)); err != nil {
				return fmt.Errorf("adding %s.%s: %w", c.table, c.column, err)
			}
		}
	}

	var client int
	err := db.QueryRow("SELECT [ID] FROM [Clients] WHERE [Computer] = ?", computer).Scan(&client)
	if err == sql.ErrNoRows {
		if _, err := db.Exec("INSERT INTO [Clients] ([Computer]) VALUES (?)", computer); err != nil {
			return fmt.Errorf("clients: %w", err)
		}
		err = db.QueryRow("SELECT [ID] FROM [Clients] WHERE [Computer] = ?", computer).Scan(&client)
	}
	if err != nil {
		return fmt.Errorf("clients: %w", err)
	}

	// keep a GUID BCS gave the session earlier
	var guid sql.NullString
	db.QueryRow("SELECT [GUID] FROM [Session] WHERE [ID] = ?", s.Session.ID).Scan(&guid)
	if s.Session.GUID == "" && guid.Valid {
		s.Session.GUID = guid.String
	}
	for _, t := range []string{"Session", "Section", "Tables", "RoundData", "PlayerNumbers"} {
		if _, err := db.Exec("DELETE FROM [" + t + "]"); err != nil {
			return fmt.Errorf("clearing %s: %w", t, err)
		}
	}
	date := s.Session.Date
	if date.IsZero() {
		date = time.Now()
	}
	day := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.Local)
	var guidArg any
	if s.Session.GUID != "" {
		guidArg = s.Session.GUID
	}
	if _, err := db.Exec("INSERT INTO [Session] ([ID], [Name], [Date], [GUID], [Status]) VALUES (?, ?, ?, ?, 0)",
		s.Session.ID, s.Session.Name, day, guidArg); err != nil {
		return fmt.Errorf("session: %w", err)
	}
	for _, x := range s.Sections {
		if _, err := db.Exec("INSERT INTO [Section] ([ID], [Letter], [Tables], [MissingPair], [EWMoveBeforePlay], [Session], [ScoringType], [Winners]) VALUES (?, ?, ?, ?, 0, ?, ?, ?)",
			x.ID, x.Letter, x.Tables, x.MissingPair, x.Session, int(x.Scoring), x.Winners); err != nil {
			return fmt.Errorf("section %s: %w", x.Letter, err)
		}
	}
	for _, t := range s.Tables {
		if _, err := db.Exec("INSERT INTO [Tables] ([Section], [Table], [ComputerID], [Status], [LogOnOff], [UpdateFromRound], [Group]) VALUES (?, ?, ?, 0, 2, 0, ?)",
			t.Section, t.Table, client, t.Group); err != nil {
			return fmt.Errorf("table %d: %w", t.Table, err)
		}
	}
	for _, r := range s.Rounds { // CustomBoards left out: the template refuses ''
		if _, err := db.Exec("INSERT INTO [RoundData] ([Section], [Table], [Round], [NSPair], [EWPair], [LowBoard], [HighBoard]) VALUES (?, ?, ?, ?, ?, ?, ?)",
			r.Section, r.Table, r.Round, r.NS, r.EW, r.LowBoard, r.HighBoard); err != nil {
			return fmt.Errorf("round %d table %d: %w", r.Round, r.Table, err)
		}
	}
	for _, p := range s.Players {
		if _, err := db.Exec("INSERT INTO [PlayerNumbers] ([Section], [Table], [Direction], [Number], [Name], [Round], [Updated], [EntrySource]) VALUES (?, ?, ?, ?, ?, 0, ?, 0)",
			p.Section, p.Table, p.Direction, p.Number, p.Name, true); err != nil {
			return fmt.Errorf("player %s: %w", p.Number, err)
		}
	}
	if len(s.Settings) > 0 {
		if _, err := db.Exec("DELETE FROM [Settings]"); err != nil {
			return fmt.Errorf("clearing Settings: %w", err)
		}
		cols, marks, vals := []string{"[Section]"}, []string{"?"}, []any{0}
		for _, x := range s.Settings {
			if hasColumn(db, "Settings", x.Column) {
				cols, marks, vals = append(cols, "["+x.Column+"]"), append(marks, "?"), append(vals, x.Value)
			}
		}
		q := fmt.Sprintf("INSERT INTO [Settings] (%s) VALUES (%s)", strings.Join(cols, ", "), strings.Join(marks, ", "))
		for _, x := range s.Sections {
			vals[0] = x.ID
			if _, err := db.Exec(q, vals...); err != nil {
				return fmt.Errorf("settings of section %s: %w", x.Letter, err)
			}
		}
	}
	if hasColumn(db, "PlayerNames", "strID") {
		if _, err := db.Exec("DELETE FROM [PlayerNames]"); err != nil {
			return fmt.Errorf("clearing PlayerNames: %w", err)
		}
	}
	for _, n := range s.Names {
		var id any
		if n.ID > 0 {
			id = n.ID
		}
		if _, err := db.Exec("INSERT INTO [PlayerNames] ([ID], [strID], [Name]) VALUES (?, ?, ?)", id, n.StrID, n.Name); err != nil {
			return fmt.Errorf("player name %s: %w", n.StrID, err)
		}
	}
	return nil
}

// ReadReceived reads every ReceivedData row. A session has a few thousand at most, and reading them
// all each time also catches results the TD corrected in BCS itself.
func ReadReceived(db *sql.DB) ([]Received, error) {
	rows, err := db.Query("SELECT [ID], [Section], [Table], [Round], [Board], [PairNS], [PairEW], [Declarer], [NS/EW], [Contract], [Result], [LeadCard], [Remarks], [TimeLog], [Erased] FROM [ReceivedData]")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Received
	for rows.Next() {
		var r Received
		var section, table, round, board, ns, ew, decl sql.NullInt64
		var dir, contract, result, lead, remarks sql.NullString
		var logged sql.NullTime
		var erased sql.NullBool
		if err := rows.Scan(&r.ID, &section, &table, &round, &board, &ns, &ew, &decl, &dir, &contract, &result, &lead, &remarks, &logged, &erased); err != nil {
			return nil, err
		}
		r.Section, r.Table, r.Round, r.Board = int(section.Int64), int(table.Int64), int(round.Int64), int(board.Int64)
		r.PairNS, r.PairEW, r.Declarer = int(ns.Int64), int(ew.Int64), int(decl.Int64)
		r.Direction, r.Contract, r.Result = strings.TrimSpace(dir.String), contract.String, result.String
		r.LeadCard, r.Remarks, r.Logged, r.Erased = lead.String, remarks.String, logged.Time, erased.Bool
		out = append(out, r)
	}
	return out, rows.Err()
}
