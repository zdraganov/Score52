package web

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/zdraganov/Score52/internal/pairs"
	"github.com/zdraganov/Score52/internal/store"
	"github.com/zdraganov/Score52/internal/usebio"
)

// Files for bridge.bg, as its slots read them (ResultFiles there):
//
//	.s1, .s2...    qualifying session k: the qualifying's standings after it (cumulative, as the site takes
//	               the latest session for the overall order and ELO), its own result beside them, its boards
//	.f1, .f2...    final A, B: the final's standings with the carry-over split into its parts; the boards
//	               too when the final was one session
//	.f1s1, .f1s2   a session of a final played in several: the final so far, carry-over in, its boards
//
// One qualifying session and no finals is the tournament's own file, without a slot.

// fileBase is the name bridge.bg knows the event by: its tournament number when set, else the file's.
func fileBase(st *store.Store, id string) string {
	if v, _ := st.Meta(store.MetaBridgeBG); v != "" {
		return v
	}
	return id
}

// export builds the file of a stage after its k-th session (k = 0: the whole stage, a final's own
// file) and its name's slot part.
func export(st *store.Store, event, stage string, k int) (usebio.PairsEvent, string, error) {
	var ev usebio.PairsEvent
	all, err := st.Sessions()
	if err != nil {
		return ev, "", err
	}
	pl, err := loadPlan(st)
	if err != nil {
		return ev, "", err
	}
	ss := stageSessions(all, stage)
	if len(ss) == 0 {
		return ev, "", errors.New("етапът още няма сесии")
	}
	whole := k == 0
	if whole {
		k = len(ss)
	}
	if k < 1 || k > len(ss) {
		return ev, "", errors.New("няма такава сесия")
	}
	// the stage's sessions up to k, the others as they are
	var cut []store.Session
	for _, x := range all {
		if x.Stage != stage || indexIn(ss, x) < k {
			cut = append(cut, x)
		}
	}

	var carry map[int]float64
	var fraction float64
	var entries []int64
	if stage == "" {
		es, _ := st.Entries()
		for _, e := range es {
			entries = append(entries, e.ID)
		}
	} else {
		f, ok := pl.final(stage)
		if !ok {
			return ev, "", fmt.Errorf("няма финал %s", stage)
		}
		if _, carry, err = qualifying(st, all); err != nil {
			return ev, "", err
		}
		fraction, entries = f.Carry, f.Entries
	}
	sr, err := stageStandings(st, cut, stage, carry, fraction, entries)
	if err != nil {
		return ev, "", err
	}

	cur := ss[k-1]
	d, err := sessionDataOf(st, cur)
	if err != nil {
		return ev, "", err
	}
	res, err := scoreSession(d)
	if err != nil {
		return ev, "", err
	}
	// a session's file has its boards; a final's own file only when the final was one session
	withSession := !whole
	withBoards := withSession || len(ss) == 1

	// pair numbers: the session's, when its boards go along (the travellers use them); otherwise the
	// place in the list of entries
	es, _ := st.Entries()
	byID := map[int64]store.Entry{}
	number := map[int64]int{}
	for i, e := range es {
		byID[e.ID] = e
		number[e.ID] = i + 1
	}
	if withBoards {
		for i, e := range d.Seating {
			if e != 0 {
				number[e] = i + 1
			}
		}
	}

	ev.Method = map[string]usebio.Method{string(pairs.MethodMP): usebio.MatchPoints, string(pairs.MethodCrossIMP): usebio.CrossIMPs,
		string(pairs.MethodButler): usebio.Butler}[cur.Scoring]
	ev.Date, _ = time.Parse("2006-01-02", must(st.Meta(store.MetaDate)))
	ev.Title = event + " – " + stageName(stage)
	if stage != "" {
		ev.CarryWeight = fraction
	}
	if withSession {
		ev.Title = event + " – " + cur.Name
	}
	for i, row := range sr.Rows {
		if row.Boards == 0 {
			continue
		}
		pr := usebio.Pair{Number: number[row.Entry], Place: row.Rank, Score: row.Total.Total, Percent: row.Total.Total, Boards: row.Boards,
			Tied: i > 0 && row.Rank == sr.Rows[i-1].Rank || i+1 < len(sr.Rows) && row.Rank == sr.Rows[i+1].Rank}
		// a two-winner Mitchell's line, when the numbers are that session's
		if n := pr.Number; withBoards && d.Movement.TwoFields && d.Seating[n-1] == row.Entry {
			pr.Direction = map[bool]string{true: "NS", false: "EW"}[n <= d.Movement.Tables]
		}
		if withSession && row.PerSession[k-1] != nil {
			pr.Session = &usebio.SessionResult{Percent: *row.PerSession[k-1], Boards: row.SessionBoards[k-1]}
		}
		if stage != "" && fraction > 0 {
			carried := fraction * row.Total.Carry
			pr.Carry = &usebio.CarryParts{Carried: carried, Own: row.Total.Total - carried, Qualifying: row.Total.Carry, Final: row.Total.Score}
		}
		for _, p := range byID[row.Entry].Players {
			pr.Players = append(pr.Players, usebio.Player{Name: p.Name, FederationID: p.FederationID})
		}
		ev.Pairs = append(ev.Pairs, pr)
	}
	if withBoards {
		ev.Boards = sessionBoards(d, res)
	}

	var slot string
	switch {
	case stage == "" && len(ss) == 1 && len(pl.Finals) == 0:
		slot = ""
	case stage == "":
		slot = fmt.Sprintf(".s%d", k)
	case whole:
		slot = fmt.Sprintf(".f%d", finalNumber(stage))
	default:
		slot = fmt.Sprintf(".f%ds%d", finalNumber(stage), k)
	}
	return ev, slot, nil
}

func finalNumber(stage string) int { return int(stage[0]-'A') + 1 }

func indexIn(ss []store.Session, x store.Session) int {
	for i, y := range ss {
		if y.ID == x.ID {
			return i
		}
	}
	return -1
}

func (s *Server) sendExport(w http.ResponseWriter, st *store.Store, p page, stage string, k int) {
	ev, slot, err := export(st, p.Event, stage, k)
	if err != nil {
		s.fail(w, http.StatusBadRequest, err)
		return
	}
	var buf bytes.Buffer
	if err := usebio.WritePairs(&buf, ev); err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s%s.xml"`, fileBase(st, p.EventID), slot))
	w.Write(buf.Bytes())
}

// usebio is a session's file; for a final played in one session, the final's own file (it is the same
// session, with the carry-over).
func (s *Server) usebio(w http.ResponseWriter, r *http.Request) {
	st, p, d, ok := s.loadSession(w, r)
	if !ok {
		return
	}
	all, _ := st.Sessions()
	ss := stageSessions(all, d.Stage)
	k := indexIn(ss, d.Session) + 1
	if d.Stage != "" && len(ss) == 1 {
		k = 0
	}
	s.sendExport(w, st, p, d.Stage, k)
}

// stageUsebio is a final's own file, or for the qualifying its latest session's (the standings so far).
func (s *Server) stageUsebio(w http.ResponseWriter, r *http.Request) {
	st, p, ok := s.eventPage(w, r)
	if !ok {
		return
	}
	stage := r.PathValue("stage")
	k := 0
	if stage == "q" {
		all, _ := st.Sessions()
		stage, k = "", len(stageSessions(all, ""))
	}
	s.sendExport(w, st, p, stage, k)
}
