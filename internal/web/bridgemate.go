package web

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/zdraganov/Score52/internal/bridgemate"
	"github.com/zdraganov/Score52/internal/movement"
	"github.com/zdraganov/Score52/internal/pairs"
	"github.com/zdraganov/Score52/internal/store"
	"github.com/zdraganov/Score52/internal/teams"
)

// BwsHost is where BridgeMate files are made and read: on Windows, the template, the Access driver and
// Bridgemate Control Software (BCS). Without one (another system) the pages say BridgeMate needs Windows.
type BwsHost interface {
	Create(path string) error // a new .bws from the template
	Open(path string) (*sql.DB, error)
	Computer() (string, error) // the name BCS knows this PC by
	Start(path string) error   // BCS on the file: reset the server, upload, read results
}

// SetBridgemate connects the server to BridgeMate.
func (s *Server) SetBridgemate(h BwsHost) { s.bws = h }

// roundBws is a teams round's BridgeMate file, next to the event's.
func (s *Server) roundBws(id string, round int) string {
	return filepath.Join(s.dir, fmt.Sprintf("%s-r%d.bws", id, round))
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// roundOf loads a started event and one of its rounds.
func (s *Server) roundOf(w http.ResponseWriter, r *http.Request) (*store.Store, page, *teams.Event, roundView, bool) {
	st, p, ok := s.eventPage(w, r)
	if !ok {
		return nil, p, nil, roundView{}, false
	}
	if s.bws == nil {
		s.fail(w, http.StatusBadRequest, errors.New("BridgeMate работи само под Windows, с инсталиран Bridgemate Control Software"))
		return nil, p, nil, roundView{}, false
	}
	ev, err := st.LoadTeams()
	if err != nil || ev == nil {
		s.fail(w, http.StatusBadRequest, errors.New("състезанието не е започнало"))
		return nil, p, nil, roundView{}, false
	}
	entries, _ := st.Entries()
	rounds, err := roundViews(st, ev, entries)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return nil, p, nil, roundView{}, false
	}
	round, _ := strconv.Atoi(r.PathValue("round"))
	for _, rv := range rounds {
		if rv.Round == round {
			return st, p, ev, rv, true
		}
	}
	s.fail(w, http.StatusNotFound, errors.New("няма такъв рунд"))
	return nil, p, nil, roundView{}, false
}

// bwsRound writes the round into its BridgeMate file, with every player of the event by number, and
// when asked starts BCS on it.
func (s *Server) bwsRound(w http.ResponseWriter, r *http.Request) {
	st, p, ev, rv, ok := s.roundOf(w, r)
	if !ok {
		return
	}
	var ms []bridgemate.TeamMatch
	for _, m := range rv.Matches {
		if !m.Bye() { // each team's name on its seats in both rooms
			h, a := bridgemate.Player{Name: m.HomeName}, bridgemate.Player{Name: m.AwayName}
			ms = append(ms, bridgemate.TeamMatch{Table: m.Table, Home: ev.Teams[m.Home].Seed, Away: ev.Teams[m.Away].Seed,
				Open: [4]bridgemate.Player{h, h, a, a}, Closed: [4]bridgemate.Player{a, a, h, h}})
		}
	}
	date, _ := st.Meta(store.MetaDate)
	d, _ := time.Parse("2006-01-02", date)
	// the tables key in their first board: any of 1..32 (more when a match plays more)
	setup, err := bridgemate.TeamsSetup(fmt.Sprintf("%s – рунд %d", p.Event, rv.Round), d, rv.Round, max(32, rv.Boards), ms)
	if err != nil {
		s.fail(w, http.StatusBadRequest, fmt.Errorf("файлът за BridgeMate не става: %w", err))
		return
	}
	setup.AddNames(eventNames(st)...)
	msg, err := s.writeBws(s.roundBws(p.EventID, rv.Round), setup, r.PostFormValue("start") != "")
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/e/%s?bm=%s#r%d", p.EventID, msg, rv.Round), http.StatusSeeOther)
}

// bwsResults takes the round's results from its BridgeMate file into the matches: section A is the
// open room, B the closed one, the table the match. A board the director entered or corrected here
// stays as it is; a result erased on the Bridgemate goes. Complete matches are recorded.
func (s *Server) bwsResults(w http.ResponseWriter, r *http.Request) {
	st, p, ev, rv, ok := s.roundOf(w, r)
	if !ok {
		return
	}
	boards, bad, err := s.readBws(s.roundBws(p.EventID, rv.Round))
	if err != nil {
		s.fail(w, http.StatusBadRequest, err)
		return
	}
	byTable := map[int]matchView{}
	for _, m := range rv.Matches {
		if !m.Bye() {
			byTable[m.Table] = m
		}
	}
	have, err := st.TeamBoards(rv.Round)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	type key struct {
		home, away int
		room       string
		board      int
	}
	mine := map[key]store.TeamBoard{}
	for _, b := range have {
		mine[key{b.Home, b.Away, b.Room, b.Board}] = b
	}
	rooms := map[int]string{bridgemate.SectionOpen: store.RoomOpen, bridgemate.SectionClosed: store.RoomClosed}
	var taken, kept, pending int
	current := map[key]bool{}
	for _, b := range boards {
		m, ok := byTable[b.Table]
		room := rooms[b.Section]
		if !ok || room == "" || b.Received.Board < 1 {
			pending++ // not a table of this round's matches
			continue
		}
		k := key{int(m.Home), int(m.Away), room, b.Received.Board}
		if b.Status != bridgemate.Played {
			pending++ // adjusted, not played or the TD's to decide: entered by hand
			continue
		}
		current[k] = true
		tb := store.TeamBoard{Round: rv.Round, Home: k.home, Away: k.away, Room: room, Board: k.board, Contract: b.Contract.String(),
			Tricks: b.Tricks, Score: b.ScoreNS, Source: "bridgemate", Lead: b.LeadCard}
		tb.Declarer = declarer(b)
		if old, ok := mine[k]; ok && old.Source != "bridgemate" {
			if old.Score != tb.Score || old.Contract != tb.Contract {
				kept++
			}
			continue
		}
		if err := st.PutTeamBoard(tb); err != nil {
			s.fail(w, http.StatusInternalServerError, err)
			return
		}
		taken++
	}
	for k, b := range mine { // erased on the Bridgemate
		if b.Source == "bridgemate" && !current[k] {
			if err := st.DeleteTeamBoard(rv.Round, k.home, k.away, k.room, k.board); err != nil {
				s.fail(w, http.StatusInternalServerError, err)
				return
			}
		}
	}
	all, err := st.TeamBoards(rv.Round)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	for _, m := range rv.Matches {
		if m.Bye() {
			continue
		}
		md := &matchData{Round: rv.Round, Table: m.Table, View: m, Match: m.Match}
		md.fill(all)
		if err := recordMatch(st, ev, md); err != nil {
			s.fail(w, http.StatusBadRequest, err)
			return
		}
	}
	q := url.Values{"bm": {"read"}, "taken": {strconv.Itoa(taken)}, "kept": {strconv.Itoa(kept)}, "pending": {strconv.Itoa(pending + len(bad))}}
	http.Redirect(w, r, fmt.Sprintf("/e/%s?%s#r%d", p.EventID, q.Encode(), rv.Round), http.StatusSeeOther)
}

// --- pairs sessions ---

// sessionBws is a pairs session's BridgeMate file, next to the event's.
func (s *Server) sessionBws(id, slot string) string {
	return filepath.Join(s.dir, fmt.Sprintf("%s-%s.bws", id, slot))
}

// bmPlayer is a player as the Bridgemates show them.
func bmPlayer(p store.Player) bridgemate.Player {
	return bridgemate.Player{Number: strings.TrimSpace(p.FederationID), Name: p.Name}
}

// eventNames are every player of the event, for PlayerNames: whoever sits down enters their number
// and sees their name, wherever the movement or the line-up puts them.
func eventNames(st *store.Store) []bridgemate.Player {
	entries, _ := st.Entries()
	var out []bridgemate.Player
	for _, e := range entries {
		for _, p := range e.Players {
			out = append(out, bmPlayer(p))
		}
	}
	return out
}

// writeBws makes path from the template the first time (results already in it stay), writes the
// setup and, when asked, starts BCS on it. It answers "file" or "started" for the page's note.
func (s *Server) writeBws(path string, setup bridgemate.Setup, start bool) (string, error) {
	if !fileExists(path) {
		if err := s.bws.Create(path); err != nil {
			return "", fmt.Errorf("файлът за BridgeMate: %w", err)
		}
	}
	db, err := s.bws.Open(path)
	if err != nil {
		return "", err
	}
	defer db.Close()
	pc, err := s.bws.Computer()
	if err == nil {
		err = bridgemate.Write(db, setup, pc)
	}
	if err != nil {
		return "", fmt.Errorf("файлът за BridgeMate: %w", err)
	}
	if !start {
		return "file", nil
	}
	if err := s.bws.Start(path); err != nil {
		return "", fmt.Errorf("BCS не тръгна: %w", err)
	}
	return "started", nil
}

// readBws reads a file's current results.
func (s *Server) readBws(path string) ([]bridgemate.Board, []error, error) {
	if !fileExists(path) {
		return nil, nil, errors.New("още няма файл за BridgeMate")
	}
	db, err := s.bws.Open(path)
	if err != nil {
		return nil, nil, err
	}
	rows, err := bridgemate.ReadReceived(db)
	db.Close()
	if err != nil {
		return nil, nil, fmt.Errorf("резултатите от BridgeMate: %w", err)
	}
	boards, bad := bridgemate.Current(rows)
	return boards, bad, nil
}

// declarer is the seat that played the board: BCS's letter, or N/E for the side.
func declarer(b bridgemate.Board) string {
	if b.Contract.Level == 0 {
		return ""
	}
	if d := strings.ToUpper(strings.TrimSpace(b.Direction)); len(d) == 1 && strings.Contains("NESW", d) {
		return d
	}
	if b.DeclarerNS {
		return "N"
	}
	return "E"
}

// bmNote is what the page says after a BridgeMate action (?bm=...).
func bmNote(q url.Values, path string) string {
	switch q.Get("bm") {
	case "file":
		return "Файлът за BridgeMate е готов: " + path
	case "started":
		return "BCS е пуснат на файла: " + path
	case "read":
		return fmt.Sprintf("От BridgeMate: %s резултата взети; %s поправени тук остават както са; %s чакат съдията (присъдени, неизиграни или нечетими).",
			q.Get("taken"), q.Get("kept"), q.Get("pending"))
	}
	return ""
}

func (s *Server) sessionForBws(w http.ResponseWriter, r *http.Request) (*store.Store, page, *sessionData, bool) {
	if s.bws == nil {
		s.fail(w, http.StatusBadRequest, errors.New("BridgeMate работи само под Windows, с инсталиран Bridgemate Control Software"))
		return nil, page{}, nil, false
	}
	return s.loadSession(w, r)
}

// bwsSession writes the session into its BridgeMate file: its sections as BCS sections, the names at
// the first-round tables and every player of the event by number.
func (s *Server) bwsSession(w http.ResponseWriter, r *http.Request) {
	st, p, d, ok := s.sessionForBws(w, r)
	if !ok {
		return
	}
	entries, _ := st.Entries()
	byID := map[int64]store.Entry{}
	for _, e := range entries {
		byID[e.ID] = e
	}
	players := map[int][2]bridgemate.Player{}
	for i, id := range d.Seating {
		if e, ok := byID[id]; ok && id != 0 && len(e.Players) == 2 {
			players[i+1] = [2]bridgemate.Player{bmPlayer(e.Players[0]), bmPlayer(e.Players[1])}
		}
	}
	date, _ := st.Meta(store.MetaDate)
	day, _ := time.Parse("2006-01-02", date)
	setup, err := bridgemate.SessionSetup(p.Event+" – "+d.Name, day, d.Movement, d.BoardsPerRound, d.FirstBoard, pairs.Method(d.Scoring), players)
	if err != nil {
		s.fail(w, http.StatusBadRequest, fmt.Errorf("файлът за BridgeMate не става: %w", err))
		return
	}
	setup.AddNames(eventNames(st)...)
	msg, err := s.writeBws(s.sessionBws(p.EventID, d.Slot), setup, r.PostFormValue("start") != "")
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/e/%s/s/%s?bm=%s", p.EventID, d.Slot, msg), http.StatusSeeOther)
}

// bwsSessionResults takes the session's results from its BridgeMate file into the travellers. BCS's
// section and table give the movement's table, the round its seat: the pairs are the movement's.
// A line the director entered or corrected here stays; a result erased on the Bridgemate goes.
func (s *Server) bwsSessionResults(w http.ResponseWriter, r *http.Request) {
	st, p, d, ok := s.sessionForBws(w, r)
	if !ok {
		return
	}
	boards, bad, err := s.readBws(s.sessionBws(p.EventID, d.Slot))
	if err != nil {
		s.fail(w, http.StatusBadRequest, err)
		return
	}
	m := d.Movement
	secs := bridgemate.SessionSections(m)
	seat := map[[2]int]movement.Seat{}
	for _, x := range m.Seats {
		seat[[2]int{x.Round, x.Table}] = x
	}
	type key struct{ board, ns, ew int }
	mine := map[key]store.Result{}
	for _, res := range d.Results {
		mine[key{res.Board, res.NS, res.EW}] = res
	}
	mp := d.Scoring == string(pairs.MethodMP)
	var taken, kept, pending int
	current := map[key]bool{}
	for _, b := range boards {
		var x movement.Seat
		ok := b.Section >= 1 && b.Section <= len(secs) && b.Table >= 1 && b.Table <= secs[b.Section-1].Tables
		if ok {
			x, ok = seat[[2]int{b.Received.Round, secs[b.Section-1].FirstTable + b.Table - 1}]
		}
		in := false
		if ok && !m.IsPhantom(x.NS) && !m.IsPhantom(x.EW) {
			for _, n := range boardsOf(x, d.BoardsPerRound, d.FirstBoard) {
				in = in || n == b.Received.Board
			}
		}
		if !in {
			pending++ // not a board of this table in that round
			continue
		}
		k := key{b.Received.Board, x.NS, x.EW}
		res := store.Result{Board: k.board, Round: x.Round, Table: x.Table, NS: x.NS, EW: x.EW, Source: "bridgemate"}
		switch {
		case b.Status == bridgemate.Played:
			res.Contract, res.Declarer, res.Lead, res.Tricks, res.Score = b.Contract.String(), declarer(b), b.LeadCard, b.Tricks, b.ScoreNS
		case b.Status == bridgemate.Adjusted && mp:
			ns, ew := b.AdjNS, b.AdjEW
			res.AdjNS, res.AdjEW = &ns, &ew
		default:
			pending++ // not played, the TD's to decide, or an adjusted score in IMPs: entered by hand
			continue
		}
		current[k] = true
		if old, ok := mine[k]; ok {
			if old.Source != "bridgemate" {
				if !sameResult(old, res) {
					kept++
				}
				continue
			}
			if sameResult(old, res) && old.Lead == res.Lead {
				continue
			}
		}
		if err := st.PutResult(d.ID, res); err != nil {
			s.fail(w, http.StatusInternalServerError, err)
			return
		}
		taken++
	}
	for k, res := range mine { // erased on the Bridgemate
		if res.Source == "bridgemate" && !current[k] {
			if err := st.DeleteResult(d.ID, k.board, k.ns, k.ew); err != nil {
				s.fail(w, http.StatusInternalServerError, err)
				return
			}
		}
	}
	q := url.Values{"bm": {"read"}, "taken": {strconv.Itoa(taken)}, "kept": {strconv.Itoa(kept)}, "pending": {strconv.Itoa(pending + len(bad))}}
	http.Redirect(w, r, fmt.Sprintf("/e/%s/s/%s?%s", p.EventID, d.Slot, q.Encode()), http.StatusSeeOther)
}

// sameResult: two lines with the same score (the lead aside).
func sameResult(a, b store.Result) bool {
	adj := func(x, y *float64) bool { return x == nil && y == nil || x != nil && y != nil && *x == *y }
	return a.Contract == b.Contract && a.Declarer == b.Declarer && a.Tricks == b.Tricks && a.Score == b.Score &&
		adj(a.AdjNS, b.AdjNS) && adj(a.AdjEW, b.AdjEW)
}
