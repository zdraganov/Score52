// Package store keeps an event in a SQLite file: one file per event, so a backup is a copy of the file
// and the director can carry it on a stick. The engine packages hold no state of their own beyond what
// is loaded from here.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	"github.com/zdraganov/Score52/internal/movement"
	"github.com/zdraganov/Score52/internal/teams"

	_ "modernc.org/sqlite"
)

type Store struct{ db *sql.DB }

// Open opens (or creates) an event file and brings its schema up to date.
func Open(path string) (*Store, error) {
	q := url.Values{}
	for _, p := range []string{"foreign_keys(1)", "journal_mode(WAL)", "busy_timeout(5000)", "synchronous(NORMAL)"} {
		q.Add("_pragma", p)
	}
	db, err := sql.Open("sqlite", "file:"+path+"?"+q.Encode())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // one writer; SQLite serialises anyway, this keeps "database is locked" away
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	var v int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		return err
	}
	if v > len(migrations) {
		return fmt.Errorf("the file is from a newer version of the program (schema %d, this one knows %d)", v, len(migrations))
	}
	for i := v; i < len(migrations); i++ {
		err := s.tx(func(tx *sql.Tx) error {
			if _, err := tx.Exec(migrations[i]); err != nil {
				return err
			}
			_, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, i+1))
			return err
		})
		if err != nil {
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
	}
	return nil
}

func (s *Store) tx(f func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	if err := f(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// --- meta ---

// Event-wide settings live in meta under these keys (and any others the UI needs).
const (
	MetaKind      = "kind" // pairs, teams
	MetaTitle     = "title"
	MetaDate      = "date" // YYYY-MM-DD
	MetaBridgeBG  = "bridgebg_tournament"
	MetaPairsPlan = "pairs_plan" // JSON: stages, finals, carry-over
)

func (s *Store) SetMeta(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// Meta is a setting, "" when unset.
func (s *Store) Meta(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// --- players and entries ---

type Player struct {
	ID           int64
	Name         string
	FederationID string
	Club         string
}

type Entry struct {
	ID      int64
	Name    string
	Seed    int
	Players []Player
}

func (s *Store) AddPlayer(p Player) (int64, error) {
	r, err := s.db.Exec(`INSERT INTO players (name, federation_id, club) VALUES (?, ?, ?)`, p.Name, p.FederationID, p.Club)
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}

// AddEntry adds a pair or a team with its players (already added), in seat order.
func (s *Store) AddEntry(name string, seed int, players []int64) (int64, error) {
	var id int64
	err := s.tx(func(tx *sql.Tx) error {
		r, err := tx.Exec(`INSERT INTO entries (name, seed) VALUES (?, ?)`, name, seed)
		if err != nil {
			return err
		}
		if id, err = r.LastInsertId(); err != nil {
			return err
		}
		for i, p := range players {
			if _, err := tx.Exec(`INSERT INTO entry_players (entry_id, seat, player_id) VALUES (?, ?, ?)`, id, i+1, p); err != nil {
				return err
			}
		}
		return nil
	})
	return id, err
}

// DeleteEntry removes a pair or team and its players. Refused once a session seats it.
func (s *Store) DeleteEntry(id int64) error {
	sessions, err := s.Sessions()
	if err != nil {
		return err
	}
	for _, x := range sessions {
		for _, e := range x.Seating {
			if e == id {
				return fmt.Errorf("the entry plays in session %s", x.Slot)
			}
		}
	}
	return s.tx(func(tx *sql.Tx) error {
		rows, err := tx.Query(`SELECT player_id FROM entry_players WHERE entry_id = ?`, id)
		if err != nil {
			return err
		}
		var players []int64
		for rows.Next() {
			var p int64
			if err := rows.Scan(&p); err != nil {
				rows.Close()
				return err
			}
			players = append(players, p)
		}
		rows.Close()
		if _, err := tx.Exec(`DELETE FROM entries WHERE id = ?`, id); err != nil {
			return err
		}
		for _, p := range players {
			if _, err := tx.Exec(`DELETE FROM players WHERE id = ? AND NOT EXISTS (SELECT 1 FROM entry_players WHERE player_id = ?)`, p, p); err != nil {
				return err
			}
		}
		return nil
	})
}

// Entries are all pairs or teams with their players, by id.
func (s *Store) Entries() ([]Entry, error) {
	rows, err := s.db.Query(`SELECT e.id, e.name, e.seed, p.id, p.name, p.federation_id, p.club
		FROM entries e LEFT JOIN entry_players ep ON ep.entry_id = e.id LEFT JOIN players p ON p.id = ep.player_id
		ORDER BY e.id, ep.seat`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		var e Entry
		var pid sql.NullInt64
		var pn, pf, pc sql.NullString
		if err := rows.Scan(&e.ID, &e.Name, &e.Seed, &pid, &pn, &pf, &pc); err != nil {
			return nil, err
		}
		if len(out) == 0 || out[len(out)-1].ID != e.ID {
			out = append(out, e)
		}
		if pid.Valid {
			last := &out[len(out)-1]
			last.Players = append(last.Players, Player{ID: pid.Int64, Name: pn.String, FederationID: pf.String, Club: pc.String})
		}
	}
	return out, rows.Err()
}

// --- pairs sessions ---

type Session struct {
	ID             int64
	Slot           string // s1, f1
	Name           string
	Scoring        string // mp, cross-imp, butler
	BoardsPerRound int
	FirstBoard     int
	Movement       movement.Movement
	Seating        []int64 // entry id at movement number i+1; 0 for the phantom
	Status         string
	Stage          string // '' qualifying, 'A', 'B'... a final
}

// SaveSession inserts a session, or replaces the one in the same slot (keeping its results).
func (s *Store) SaveSession(x Session) (int64, error) {
	if err := x.Movement.Validate(); err != nil {
		return 0, fmt.Errorf("session %s: %w", x.Slot, err)
	}
	if len(x.Seating) != x.Movement.Pairs {
		return 0, fmt.Errorf("session %s: %d seats for a movement of %d pairs", x.Slot, len(x.Seating), x.Movement.Pairs)
	}
	m, _ := json.Marshal(x.Movement)
	seat, _ := json.Marshal(x.Seating)
	if x.FirstBoard == 0 {
		x.FirstBoard = 1
	}
	if x.Status == "" {
		x.Status = "planned"
	}
	var id int64
	err := s.db.QueryRow(`INSERT INTO sessions (slot, name, scoring, boards_per_round, first_board, movement, seating, status, stage)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (slot) DO UPDATE SET name = excluded.name, scoring = excluded.scoring, boards_per_round = excluded.boards_per_round,
			first_board = excluded.first_board, movement = excluded.movement, seating = excluded.seating, status = excluded.status,
			stage = excluded.stage
		RETURNING id`, x.Slot, x.Name, x.Scoring, x.BoardsPerRound, x.FirstBoard, string(m), string(seat), x.Status, x.Stage).Scan(&id)
	return id, err
}

func (s *Store) Sessions() ([]Session, error) {
	rows, err := s.db.Query(`SELECT id, slot, name, scoring, boards_per_round, first_board, movement, seating, status, stage FROM sessions ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var x Session
		var m, seat string
		if err := rows.Scan(&x.ID, &x.Slot, &x.Name, &x.Scoring, &x.BoardsPerRound, &x.FirstBoard, &m, &seat, &x.Status, &x.Stage); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(m), &x.Movement); err != nil {
			return nil, fmt.Errorf("session %s movement: %w", x.Slot, err)
		}
		if err := json.Unmarshal([]byte(seat), &x.Seating); err != nil {
			return nil, fmt.Errorf("session %s seating: %w", x.Slot, err)
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// --- results ---

type Result struct {
	Board, Round, Table int
	NS, EW              int // movement pair numbers
	Contract            string
	Declarer, Lead      string
	Tricks, Score       int
	AdjNS, AdjEW        *float64 // an artificial adjusted score instead of a played one
	Source              string
	UpdatedAt           string
}

// PutResult enters or corrects a traveller line; a correction leaves the old one in the history.
func (s *Store) PutResult(session int64, r Result) error {
	if r.Source == "" {
		r.Source = "manual"
	}
	_, err := s.db.Exec(`INSERT INTO results (session_id, board, round, tbl, ns, ew, contract, declarer, lead, tricks, score, adj_ns, adj_ew, source)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (session_id, board, ns, ew) DO UPDATE SET round = excluded.round, tbl = excluded.tbl, contract = excluded.contract,
			declarer = excluded.declarer, lead = excluded.lead, tricks = excluded.tricks, score = excluded.score,
			adj_ns = excluded.adj_ns, adj_ew = excluded.adj_ew, source = excluded.source,
			updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`,
		session, r.Board, r.Round, r.Table, r.NS, r.EW, r.Contract, r.Declarer, r.Lead, r.Tricks, r.Score, r.AdjNS, r.AdjEW, r.Source)
	return err
}

func (s *Store) DeleteResult(session int64, board, ns, ew int) error {
	_, err := s.db.Exec(`DELETE FROM results WHERE session_id = ? AND board = ? AND ns = ? AND ew = ?`, session, board, ns, ew)
	return err
}

// Results are a session's traveller lines by board, then table.
func (s *Store) Results(session int64) ([]Result, error) {
	rows, err := s.db.Query(`SELECT board, round, tbl, ns, ew, contract, declarer, lead, tricks, score, adj_ns, adj_ew, source, updated_at
		FROM results WHERE session_id = ? ORDER BY board, tbl`, session)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Result
	for rows.Next() {
		var r Result
		if err := rows.Scan(&r.Board, &r.Round, &r.Table, &r.NS, &r.EW, &r.Contract, &r.Declarer, &r.Lead, &r.Tricks, &r.Score,
			&r.AdjNS, &r.AdjEW, &r.Source, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type Change struct {
	Board, NS, EW int
	Action        string // update, delete
	Old           string // the line before, as JSON
	At            string
}

// History is what was changed in a session's results, newest first.
func (s *Store) History(session int64) ([]Change, error) {
	rows, err := s.db.Query(`SELECT board, ns, ew, action, old, at FROM result_history WHERE session_id = ? ORDER BY id DESC`, session)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Change
	for rows.Next() {
		var c Change
		if err := rows.Scan(&c.Board, &c.NS, &c.EW, &c.Action, &c.Old, &c.At); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// --- teams ---

// SaveTeams writes the ops of e's log the file does not have yet. The log only grows: a file holding
// more ops than e is another event, and is refused.
func (s *Store) SaveTeams(e *teams.Event) error {
	return s.tx(func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRow(`SELECT count(*) FROM team_ops`).Scan(&n); err != nil {
			return err
		}
		if n > len(e.Log) {
			return fmt.Errorf("the file holds %d operations, the event only %d", n, len(e.Log))
		}
		for _, op := range e.Log[n:] {
			b, err := json.Marshal(op)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO team_ops (op) VALUES (?)`, string(b)); err != nil {
				return err
			}
		}
		return nil
	})
}

// LoadTeams rebuilds the teams event from its log; nil when the file has none yet.
func (s *Store) LoadTeams() (*teams.Event, error) {
	rows, err := s.db.Query(`SELECT op FROM team_ops ORDER BY seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var log []teams.Op
	for rows.Next() {
		var b string
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		var op teams.Op
		if err := json.Unmarshal([]byte(b), &op); err != nil {
			return nil, err
		}
		log = append(log, op)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(log) == 0 {
		return nil, nil
	}
	return teams.Replay(log)
}
