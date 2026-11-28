package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/zdraganov/Score52/internal/movement"
	"github.com/zdraganov/Score52/internal/pairs"
	"github.com/zdraganov/Score52/internal/store"
)

// A pairs event is a qualifying (sessions s1, s2...) and optionally finals A, B... (sessions fA1, fA2...),
// filled from the qualifying's ranking with a carry-over.

type finalSpec struct {
	Name    string  `json:"name"`
	Carry   float64 `json:"carry"`   // 0.25: the qualifying counts a quarter (MP), or a quarter of its IMPs
	Entries []int64 `json:"entries"` // in qualifying order
}

type plan struct {
	Finals []finalSpec `json:"finals"`
}

func loadPlan(st *store.Store) (plan, error) {
	var p plan
	v, err := st.Meta(store.MetaPairsPlan)
	if err != nil || v == "" {
		return p, err
	}
	return p, json.Unmarshal([]byte(v), &p)
}

func (p plan) final(name string) (finalSpec, bool) {
	for _, f := range p.Finals {
		if f.Name == name {
			return f, true
		}
	}
	return finalSpec{}, false
}

func stageSessions(sessions []store.Session, stage string) []store.Session {
	var out []store.Session
	for _, x := range sessions {
		if x.Stage == stage {
			out = append(out, x)
		}
	}
	return out
}

func stageName(stage string) string {
	if stage == "" {
		return "Квалификация"
	}
	return "Финал " + stage
}

// stageResult is a stage's standings: every pair's result in each session and in total.
type stageResult struct {
	Stage    string
	Sessions []store.Session
	MP       bool
	Carry    float64 // the final's carry-over fraction
	Rows     []stageRow
}

type stageRow struct {
	pairs.Total
	Entry      int64
	Names      string
	Boards     int
	PerSession []*float64 // % (MP) or IMPs, nil where the pair didn't play
	// SessionBoards are the boards it played in each session
	SessionBoards []int
}

// stageStandings adds up a stage's sessions by entry. carry is each entry's qualifying total for a final.
func stageStandings(st *store.Store, sessions []store.Session, stage string, carry map[int]float64, fraction float64, entries []int64) (stageResult, error) {
	sr := stageResult{Stage: stage, Sessions: stageSessions(sessions, stage), MP: true, Carry: fraction}
	all, err := st.Entries()
	if err != nil {
		return sr, err
	}
	byID := map[int64]store.Entry{}
	for _, e := range all {
		byID[e.ID] = e
	}
	per := map[int64][]*float64{}
	boards := map[int64]int{}
	sb := map[int64][]int{}
	var scores [][]pairs.SessionScore
	for i, x := range sr.Sessions {
		sr.MP = x.Scoring == string(pairs.MethodMP)
		d, err := sessionDataOf(st, x)
		if err != nil {
			return sr, err
		}
		res, err := scoreSession(d)
		if err != nil {
			return sr, err
		}
		var ss []pairs.SessionScore
		for num, t := range res.Totals {
			e := d.Seating[num-1]
			if e == 0 {
				continue
			}
			ss = append(ss, pairs.SessionScore{Pair: int(e), Score: t.Score, Top: t.Top, Boards: t.Boards})
			boards[e] += t.Boards
			if sb[e] == nil {
				sb[e] = make([]int, len(sr.Sessions))
			}
			sb[e][i] = t.Boards
			v := t.Score
			if sr.MP && t.Top > 0 {
				v = 100 * t.Score / t.Top
			}
			if per[e] == nil {
				per[e] = make([]*float64, len(sr.Sessions))
			}
			per[e][i] = &v
		}
		scores = append(scores, ss)
	}
	scoring := pairs.IMP
	if sr.MP {
		scoring = pairs.MP
	}
	totals := pairs.Totals(scoring, scores, carry, pairs.Carry{Fraction: fraction})
	seen := map[int64]bool{}
	for _, t := range totals {
		e := int64(t.Pair)
		seen[e] = true
		sr.Rows = append(sr.Rows, stageRow{Total: t, Entry: e, Names: names(byID[e]), Boards: boards[e], PerSession: pad(per[e], len(sr.Sessions)),
			SessionBoards: sb[e]})
	}
	// pairs of the stage that haven't scored yet (a final before its first result)
	for _, e := range entries {
		if !seen[e] {
			sr.Rows = append(sr.Rows, stageRow{Total: pairs.Total{Pair: int(e), Carry: carry[int(e)]}, Entry: e, Names: names(byID[e]),
				PerSession: pad(nil, len(sr.Sessions))})
		}
	}
	return sr, nil
}

func pad(v []*float64, n int) []*float64 {
	if v == nil {
		v = make([]*float64, n)
	}
	return v
}

// qualifying is the qualifying's standings, and its totals by entry as a final's carry-over.
func qualifying(st *store.Store, sessions []store.Session) (stageResult, map[int]float64, error) {
	entries, err := st.Entries()
	if err != nil {
		return stageResult{}, nil, err
	}
	var ids []int64
	for _, e := range entries {
		ids = append(ids, e.ID)
	}
	q, err := stageStandings(st, sessions, "", nil, 0, ids)
	if err != nil {
		return q, nil, err
	}
	carry := map[int]float64{}
	for _, r := range q.Rows {
		carry[int(r.Entry)] = r.Total.Total
	}
	return q, carry, nil
}

func (s *Server) standings(w http.ResponseWriter, r *http.Request) {
	st, p, ok := s.eventPage(w, r)
	if !ok {
		return
	}
	if eventKind(st) == kindTeams {
		s.teamsStandings(w, st, p)
		return
	}
	sessions, err := st.Sessions()
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	q, carry, err := qualifying(st, sessions)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	pl, err := loadPlan(st)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	var finals []stageResult
	for _, f := range pl.Finals {
		fr, err := stageStandings(st, sessions, f.Name, carry, f.Carry, f.Entries)
		if err != nil {
			s.fail(w, http.StatusInternalServerError, err)
			return
		}
		finals = append(finals, fr)
	}
	finalSessions := len(sessions) - len(q.Sessions)
	p.Title = "Класиране – " + p.Event
	p.Data = map[string]any{"Q": q, "Finals": finals, "CanPlan": len(q.Sessions) > 0 && finalSessions == 0, "Entries": len(q.Rows)}
	s.render(w, "standings", p)
}

// planFinals splits the qualifying ranking into finals: A = the first n, B = the next m..., the last
// one taking the rest when its count is left empty.
func (s *Server) planFinals(w http.ResponseWriter, r *http.Request) {
	st, p, ok := s.eventPage(w, r)
	if !ok {
		return
	}
	sessions, _ := st.Sessions()
	if len(sessions) != len(stageSessions(sessions, "")) {
		s.fail(w, http.StatusBadRequest, errors.New("финалите вече са започнали; разпределението не може да се промени"))
		return
	}
	q, _, err := qualifying(st, sessions)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	pct, _ := strconv.ParseFloat(strings.TrimSpace(r.PostFormValue("carry")), 64)
	if pct < 0 || pct > 100 {
		s.fail(w, http.StatusBadRequest, errors.New("пренасянето е между 0 и 100%"))
		return
	}
	var specs []pairs.Final
	for _, name := range []string{"A", "B", "C", "D"} {
		v := strings.TrimSpace(r.PostFormValue("n" + name))
		if v == "" {
			continue
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			s.fail(w, http.StatusBadRequest, fmt.Errorf("финал %s: невалиден брой", name))
			return
		}
		if n == 0 && name == "A" {
			s.fail(w, http.StatusBadRequest, errors.New("финал A трябва да има брой двойки"))
			return
		}
		specs = append(specs, pairs.Final{Name: name, Count: n})
		if n == 0 { // the rest: nothing after it
			break
		}
	}
	if len(specs) == 0 {
		s.fail(w, http.StatusBadRequest, errors.New("няма финали"))
		return
	}
	var ranking []pairs.Total
	for _, r := range q.Rows {
		ranking = append(ranking, r.Total)
	}
	// a tie on a cut is the director's to settle (extra boards, head-to-head...): say who and where
	at := 0
	for i, f := range specs {
		if f.Count == 0 || i == len(specs)-1 {
			break
		}
		at += f.Count
		if at < len(q.Rows) && q.Rows[at-1].Total.Total == q.Rows[at].Total.Total {
			s.fail(w, http.StatusBadRequest, fmt.Errorf("%s и %s са наравно (%s) на границата на финал %s. Изберете друг брой или решете равенството и въведете корекция.",
				q.Rows[at-1].Names, q.Rows[at].Names, strconv.FormatFloat(q.Rows[at].Total.Total, 'f', 2, 64), f.Name))
			return
		}
	}
	split, _, err := pairs.Qualify(ranking, specs)
	if err != nil {
		s.fail(w, http.StatusBadRequest, fmt.Errorf("разделянето не става: %w", err))
		return
	}
	var pl plan
	for _, f := range specs {
		fs := finalSpec{Name: f.Name, Carry: pct / 100}
		for _, e := range split[f.Name] {
			fs.Entries = append(fs.Entries, int64(e))
		}
		if len(fs.Entries) < 4 {
			s.fail(w, http.StatusBadRequest, fmt.Errorf("финал %s има %d двойки: трябват поне 4", f.Name, len(fs.Entries)))
			return
		}
		pl.Finals = append(pl.Finals, fs)
	}
	b, _ := json.Marshal(pl)
	if err := st.SetMeta(store.MetaPairsPlan, string(b)); err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	http.Redirect(w, r, "/e/"+p.EventID+"/standings", http.StatusSeeOther)
}

// stageEntries are the pairs a new session of the stage seats, in the order to start from: the
// qualifying's ranking (or the order they were entered, before any result), or the final's.
func stageEntries(st *store.Store, sessions []store.Session, stage string) ([]int64, *stageResult, error) {
	if stage == "" {
		q, _, err := qualifying(st, sessions)
		if err != nil {
			return nil, nil, err
		}
		var ids []int64
		for _, r := range q.Rows {
			ids = append(ids, r.Entry)
		}
		if len(q.Sessions) == 0 { // no ranking yet: as entered
			entries, _ := st.Entries()
			ids = ids[:0]
			for _, e := range entries {
				ids = append(ids, e.ID)
			}
		}
		return ids, &q, nil
	}
	pl, err := loadPlan(st)
	if err != nil {
		return nil, nil, err
	}
	f, ok := pl.final(stage)
	if !ok {
		return nil, nil, fmt.Errorf("няма финал %s", stage)
	}
	_, carry, err := qualifying(st, sessions)
	if err != nil {
		return nil, nil, err
	}
	fr, err := stageStandings(st, sessions, stage, carry, f.Carry, f.Entries)
	if err != nil {
		return nil, nil, err
	}
	var ids []int64
	for _, r := range fr.Rows {
		ids = append(ids, r.Entry)
	}
	return ids, &fr, nil
}

// seatSession seats a new session of a stage on movement m. The stage's first session takes the
// order as it is (a final's: by qualifying rank, snaked over the fields); later ones are mixed so
// that pairs who met already don't meet again and the fields change.
func seatSession(m movement.Movement, order []int64, earlier []store.Session, seed uint64) []int64 {
	ord := make([]int, len(order))
	for i, e := range order {
		ord[i] = int(e)
	}
	h := pairs.NewHistory()
	w := pairs.Weights{} // first session: no history, just the order
	if len(earlier) > 0 {
		w = pairs.DefaultWeights
		for _, x := range earlier {
			seat := make([]int, len(x.Seating))
			for i, e := range x.Seating {
				seat[i] = int(e)
			}
			h.Add(x.Movement, seat)
		}
	}
	got := pairs.Seat(m, ord, h, w, seed)
	out := make([]int64, len(got))
	for i, e := range got {
		out[i] = int64(e)
	}
	return out
}
