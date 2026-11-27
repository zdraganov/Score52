package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/zdraganov/Score52/internal/movement"
	"github.com/zdraganov/Score52/internal/store"
)

// dealPlan is what the director plans before the pairs are known: how many are expected, how many boards
// a pair should play, and what was dealt for that (copies of boards 1..PerCopy).
type dealPlan struct {
	From    int `json:"from"`
	To      int `json:"to"`
	Boards  int `json:"boards"`
	Tol     int `json:"tol"`
	Copies  int `json:"copies,omitempty"`
	PerCopy int `json:"per_copy,omitempty"`
}

func loadDeal(st *store.Store) dealPlan {
	var d dealPlan
	if v, _ := st.Meta(store.MetaDeal); v != "" {
		json.Unmarshal([]byte(v), &d)
	}
	return d
}

// dealRow is a number of pairs and how a deal option would seat it.
type dealRow struct {
	Pairs  int
	Layout *movement.Layout
}

type dealOption struct {
	movement.DealAdvice
	Rows []dealRow
}

// dealPage: expected pairs and boards in, the deals worth making out, each with how every number of
// pairs in the range would play with it.
func (s *Server) dealPage(w http.ResponseWriter, r *http.Request) {
	st, p, ok := s.eventPage(w, r)
	if !ok {
		return
	}
	d := loadDeal(st)
	q := r.URL.Query()
	for k, v := range map[string]*int{"from": &d.From, "to": &d.To, "boards": &d.Boards, "tol": &d.Tol} {
		if n, err := strconv.Atoi(q.Get(k)); err == nil {
			*v = n
		}
	}
	if d.Boards == 0 {
		d.Boards = 26
	}
	d.Tol = 0 // a pair plays exactly the boards set
	var opts []dealOption
	if d.To >= 4 && d.From >= 4 && d.From <= d.To && d.To-d.From <= 80 && d.Boards > 0 {
		for _, a := range movement.Advise(d.From, d.To, d.Boards, d.Tol) {
			o := dealOption{DealAdvice: a}
			for n := d.To; n >= d.From; n-- {
				row := dealRow{Pairs: n}
				if l, ok := a.ByPairs[n]; ok {
					row.Layout = &l
				}
				o.Rows = append(o.Rows, row)
			}
			opts = append(opts, o)
		}
	}
	entries, _ := st.Entries()
	p.Title = "Раздавания"
	p.Data = map[string]any{"Deal": d, "Options": opts, "Entries": len(entries), "Asked": d.To > 0}
	s.render(w, "deal", p)
}

// saveDeal records what the director dealt; the session's layouts keep to it.
func (s *Server) saveDeal(w http.ResponseWriter, r *http.Request) {
	st, p, ok := s.eventPage(w, r)
	if !ok {
		return
	}
	var d dealPlan
	for k, v := range map[string]*int{"from": &d.From, "to": &d.To, "boards": &d.Boards, "tol": &d.Tol, "copies": &d.Copies, "per_copy": &d.PerCopy} {
		*v, _ = strconv.Atoi(r.PostFormValue(k))
	}
	if d.Copies < 1 || d.PerCopy < 1 {
		s.fail(w, http.StatusBadRequest, errors.New("няма избрано раздаване"))
		return
	}
	b, _ := json.Marshal(d)
	if err := st.SetMeta(store.MetaDeal, string(b)); err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	st.SetMeta(store.MetaCopies, strconv.Itoa(d.Copies))
	http.Redirect(w, r, "/e/"+p.EventID, http.StatusSeeOther)
}
