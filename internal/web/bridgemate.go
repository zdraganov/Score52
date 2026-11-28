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

// bwsRound writes the round into its BridgeMate file (made from the template the first time; results
// already in it stay) and, when asked, starts BCS on it.
func (s *Server) bwsRound(w http.ResponseWriter, r *http.Request) {
	st, p, ev, rv, ok := s.roundOf(w, r)
	if !ok {
		return
	}
	var ms []bridgemate.TeamMatch
	for _, m := range rv.Matches {
		if !m.Bye() {
			ms = append(ms, bridgemate.TeamMatch{Table: m.Table, Home: ev.Teams[m.Home].Seed, Away: ev.Teams[m.Away].Seed, Boards: m.Boards})
		}
	}
	date, _ := st.Meta(store.MetaDate)
	d, _ := time.Parse("2006-01-02", date)
	setup, err := bridgemate.TeamsSetup(fmt.Sprintf("%s – рунд %d", p.Event, rv.Round), d, rv.FirstBoard, ms)
	if err != nil {
		s.fail(w, http.StatusBadRequest, fmt.Errorf("файлът за BridgeMate не става: %w", err))
		return
	}
	path := s.roundBws(p.EventID, rv.Round)
	if !fileExists(path) {
		if err := s.bws.Create(path); err != nil {
			s.fail(w, http.StatusInternalServerError, fmt.Errorf("файлът за BridgeMate: %w", err))
			return
		}
	}
	db, err := s.bws.Open(path)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	defer db.Close()
	pc, err := s.bws.Computer()
	if err == nil {
		err = bridgemate.Write(db, setup, pc)
	}
	if err != nil {
		s.fail(w, http.StatusInternalServerError, fmt.Errorf("файлът за BridgeMate: %w", err))
		return
	}
	msg := "file"
	if r.PostFormValue("start") != "" {
		if err := s.bws.Start(path); err != nil {
			s.fail(w, http.StatusInternalServerError, fmt.Errorf("BCS не тръгна: %w", err))
			return
		}
		msg = "started"
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
	path := s.roundBws(p.EventID, rv.Round)
	if !fileExists(path) {
		s.fail(w, http.StatusBadRequest, errors.New("рундът още няма файл за BridgeMate"))
		return
	}
	db, err := s.bws.Open(path)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	rows, err := bridgemate.ReadReceived(db)
	db.Close()
	if err != nil {
		s.fail(w, http.StatusInternalServerError, fmt.Errorf("резултатите от BridgeMate: %w", err))
		return
	}
	boards, bad := bridgemate.Current(rows)
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
		if !ok || room == "" || b.Received.Board < rv.FirstBoard || b.Received.Board >= rv.FirstBoard+m.Boards {
			pending++ // not a board of this round's matches
			continue
		}
		k := key{int(m.Home), int(m.Away), room, b.Received.Board}
		current[k] = true
		if b.Status != bridgemate.Played {
			pending++ // adjusted, not played or the TD's to decide: entered by hand
			continue
		}
		tb := store.TeamBoard{Round: rv.Round, Home: k.home, Away: k.away, Room: room, Board: k.board, Contract: b.Contract.String(),
			Tricks: b.Tricks, Score: b.ScoreNS, Source: "bridgemate", Lead: b.LeadCard}
		if b.Contract.Level > 0 {
			tb.Declarer = "E" // the seat only matters for the side: "NS"/"EW" as BCS may give it
			if b.DeclarerNS {
				tb.Declarer = "N"
			}
			if d := strings.ToUpper(strings.TrimSpace(b.Direction)); len(d) == 1 && strings.Contains("NESW", d) {
				tb.Declarer = d
			}
		}
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
		md.fill(rv.FirstBoard, all)
		if err := recordMatch(st, ev, md); err != nil {
			s.fail(w, http.StatusBadRequest, err)
			return
		}
	}
	q := url.Values{"bm": {"read"}, "taken": {strconv.Itoa(taken)}, "kept": {strconv.Itoa(kept)}, "pending": {strconv.Itoa(pending + len(bad))}}
	http.Redirect(w, r, fmt.Sprintf("/e/%s?%s#r%d", p.EventID, q.Encode(), rv.Round), http.StatusSeeOther)
}
