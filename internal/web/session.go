package web

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zdraganov/Score52/internal/movement"
	"github.com/zdraganov/Score52/internal/pairs"
	"github.com/zdraganov/Score52/internal/score"
	"github.com/zdraganov/Score52/internal/store"
	"github.com/zdraganov/Score52/internal/usebio"
)

var kindNames = map[movement.Kind]string{
	movement.Mitchell:         "Мичъл",
	movement.SkipMitchell:     "Мичъл с прескачане",
	movement.Howell:           "Хауел",
	movement.HowellDuplicated: "Хауел с дублирани раздавания",
}

var methodNames = map[string]string{
	string(pairs.MethodMP):       "Мачпойнти (%)",
	string(pairs.MethodCrossIMP): "Крос-импове",
	string(pairs.MethodButler):   "Бътлер",
}

type option struct {
	movement.Option
	Name string
}

func (s *Server) newSession(w http.ResponseWriter, r *http.Request) {
	st, p, ok := s.eventPage(w, r)
	if !ok {
		return
	}
	entries, _ := st.Entries()
	if len(entries) < 4 {
		s.fail(w, http.StatusBadRequest, errors.New("за сесия трябват поне 4 двойки"))
		return
	}
	lo, _ := strconv.Atoi(r.URL.Query().Get("min"))
	hi, _ := strconv.Atoi(r.URL.Query().Get("max"))
	if lo <= 0 {
		lo = 20
	}
	if hi < lo {
		hi = lo + 10
	}
	var opts []option
	for _, o := range movement.Options(len(entries), lo, hi) {
		opts = append(opts, option{Option: o, Name: kindNames[o.Kind]})
	}
	p.Title = "Нова сесия"
	p.Data = map[string]any{"Pairs": len(entries), "Min": lo, "Max": hi, "Options": opts, "Methods": methodNames}
	s.render(w, "new-session", p)
}

// buildMovement makes the movement an option names, for `pairs` pairs (an odd number gets a phantom).
func buildMovement(kind movement.Kind, pairCount, tables, rounds int) (movement.Movement, error) {
	switch kind {
	case movement.Mitchell, movement.SkipMitchell:
		m, err := movement.NewMitchell(tables, rounds, movement.MitchellOptions{})
		if err != nil {
			return m, err
		}
		switch m.Pairs - pairCount {
		case 0:
		case 1:
			m.Phantom = m.Pairs // the last EW pair sits out
		default:
			return m, fmt.Errorf("a Mitchell on %d tables is for %d or %d pairs, not %d", tables, m.Pairs-1, m.Pairs, pairCount)
		}
		return m, m.Validate()
	case movement.Howell:
		return movement.NewHowell(pairCount, rounds, false)
	case movement.HowellDuplicated:
		return movement.NewHowell(pairCount, rounds, true)
	}
	return movement.Movement{}, fmt.Errorf("unknown movement %q", kind)
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	st, p, ok := s.eventPage(w, r)
	if !ok {
		return
	}
	entries, _ := st.Entries()
	sessions, _ := st.Sessions()
	tables, _ := strconv.Atoi(r.PostFormValue("tables"))
	rounds, _ := strconv.Atoi(r.PostFormValue("rounds"))
	bpr, _ := strconv.Atoi(r.PostFormValue("bpr"))
	method := r.PostFormValue("method")
	if _, ok := methodNames[method]; !ok || bpr < 1 {
		s.fail(w, http.StatusBadRequest, errors.New("невалидни настройки на сесията"))
		return
	}
	m, err := buildMovement(movement.Kind(r.PostFormValue("kind")), len(entries), tables, rounds)
	if err != nil {
		s.fail(w, http.StatusBadRequest, err)
		return
	}
	// pairs take the movement's numbers in the order they were entered; the phantom's place stays 0
	seating := make([]int64, m.Pairs)
	at := 0
	for i := range seating {
		if i+1 == m.Phantom {
			continue
		}
		seating[i] = entries[at].ID
		at++
	}
	n := len(sessions) + 1
	x := store.Session{Slot: fmt.Sprintf("s%d", n), Name: fmt.Sprintf("Сесия %d", n), Scoring: method, BoardsPerRound: bpr, Movement: m, Seating: seating}
	if _, err := st.SaveSession(x); err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	http.Redirect(w, r, "/e/"+p.EventID+"/s/"+x.Slot, http.StatusSeeOther)
}

// sessionData is a session with everything its pages show.
type sessionData struct {
	store.Session
	KindName   string
	MethodName string
	Pairs      map[int]string // movement number -> names
	Boards     []int
	Expected   map[int]int // board -> times played
	Entered    map[int]int // board -> results in
	Results    []store.Result
}

func (s *Server) loadSession(w http.ResponseWriter, r *http.Request) (*store.Store, page, *sessionData, bool) {
	st, p, ok := s.eventPage(w, r)
	if !ok {
		return nil, p, nil, false
	}
	sessions, err := st.Sessions()
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return nil, p, nil, false
	}
	var x *store.Session
	for i := range sessions {
		if sessions[i].Slot == r.PathValue("slot") {
			x = &sessions[i]
		}
	}
	if x == nil {
		s.fail(w, http.StatusNotFound, errors.New("няма такава сесия"))
		return nil, p, nil, false
	}
	entries, _ := st.Entries()
	byID := map[int64]store.Entry{}
	for _, e := range entries {
		byID[e.ID] = e
	}
	d := &sessionData{Session: *x, KindName: kindNames[x.Movement.Kind], MethodName: methodNames[x.Scoring],
		Pairs: map[int]string{}, Expected: map[int]int{}, Entered: map[int]int{}}
	for i, id := range x.Seating {
		if id != 0 {
			d.Pairs[i+1] = names(byID[id])
		}
	}
	m := x.Movement
	for _, seat := range m.Seats {
		if seat.NS == 0 || seat.EW == 0 || seat.NS == m.Phantom || seat.EW == m.Phantom {
			continue
		}
		for _, b := range boardsOf(seat, x.BoardsPerRound, x.FirstBoard) {
			if d.Expected[b] == 0 {
				d.Boards = append(d.Boards, b)
			}
			d.Expected[b]++
		}
	}
	sort.Ints(d.Boards)
	if d.Results, err = st.Results(x.ID); err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return nil, p, nil, false
	}
	for _, res := range d.Results {
		d.Entered[res.Board]++
	}
	return st, p, d, true
}

func boardsOf(seat movement.Seat, bpr, first int) []int {
	out := make([]int, bpr)
	for i := range out {
		out[i] = first + (seat.Set-1)*bpr + i
	}
	return out
}

// planCell is one table in one round of the plan.
type planCell struct {
	NS, EW        int
	First, Last   int
	SitOut, Empty bool
}

func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	_, p, d, ok := s.loadSession(w, r)
	if !ok {
		return
	}
	m := d.Movement
	grid := make([][]planCell, m.Rounds)
	for i := range grid {
		grid[i] = make([]planCell, m.Tables)
		for j := range grid[i] {
			grid[i][j].Empty = true
		}
	}
	for _, seat := range m.Seats {
		b := boardsOf(seat, d.BoardsPerRound, d.FirstBoard)
		grid[seat.Round-1][seat.Table-1] = planCell{NS: seat.NS, EW: seat.EW, First: b[0], Last: b[len(b)-1],
			SitOut: seat.NS == m.Phantom || seat.EW == m.Phantom}
	}
	done := 0
	for _, b := range d.Boards {
		if d.Entered[b] >= d.Expected[b] {
			done++
		}
	}
	p.Title = d.Name
	p.Data = map[string]any{"S": d, "Grid": grid, "Done": done}
	s.render(w, "session", p)
}

// --- traveller ---

type travRow struct {
	Round, Table int
	NS, EW       int
	NSNames      string
	EWNames      string
	Contract     string
	Declarer     string
	Result       string
	Adjusted     string // "60-40"...
	Score        *int
	NSPoints     *float64
	Err          string
}

var adjustedScores = []string{"60-40", "50-50", "40-60", "60-60", "50-40", "40-50", "40-40"}

// travRows are the tables where the board is played, with what was entered for it.
func travRows(d *sessionData, board int) []travRow {
	m := d.Movement
	have := map[[2]int]store.Result{}
	for _, res := range d.Results {
		if res.Board == board {
			have[[2]int{res.NS, res.EW}] = res
		}
	}
	var rows []travRow
	for _, seat := range m.Seats {
		if seat.NS == 0 || seat.EW == 0 || seat.NS == m.Phantom || seat.EW == m.Phantom {
			continue
		}
		in := false
		for _, b := range boardsOf(seat, d.BoardsPerRound, d.FirstBoard) {
			in = in || b == board
		}
		if !in {
			continue
		}
		row := travRow{Round: seat.Round, Table: seat.Table, NS: seat.NS, EW: seat.EW, NSNames: d.Pairs[seat.NS], EWNames: d.Pairs[seat.EW]}
		if res, ok := have[[2]int{seat.NS, seat.EW}]; ok {
			if res.AdjNS != nil {
				row.Adjusted = fmt.Sprintf("%g-%g", *res.AdjNS, *res.AdjEW)
			} else {
				row.Contract, row.Declarer = res.Contract, res.Declarer
				sc := res.Score
				row.Score = &sc
				if c, err := score.ParseContract(res.Contract); err == nil && c.Level > 0 {
					row.Result = relative(res.Tricks - c.Level - 6)
				}
			}
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].Round < rows[j].Round || rows[i].Round == rows[j].Round && rows[i].Table < rows[j].Table
	})
	return rows
}

func relative(n int) string {
	switch {
	case n == 0:
		return "="
	case n > 0:
		return "+" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

func (s *Server) traveller(w http.ResponseWriter, r *http.Request) {
	_, p, d, ok := s.loadSession(w, r)
	if !ok {
		return
	}
	board, _ := strconv.Atoi(r.PathValue("board"))
	if d.Expected[board] == 0 {
		s.fail(w, http.StatusNotFound, errors.New("това раздаване не се играе в сесията"))
		return
	}
	rows := travRows(d, board)
	s.addPoints(d, board, rows)
	s.renderTraveller(w, p, d, board, rows)
}

// addPoints puts each row's points on the board (percent or IMPs) next to it.
func (s *Server) addPoints(d *sessionData, board int, rows []travRow) {
	res, err := scoreSession(d)
	if err != nil {
		return
	}
	for _, l := range res.Lines {
		if l.Board != board {
			continue
		}
		for i := range rows {
			if rows[i].NS == l.NS && rows[i].EW == l.EW {
				v := l.NSPoints
				rows[i].NSPoints = &v
			}
		}
	}
}

func (s *Server) renderTraveller(w http.ResponseWriter, p page, d *sessionData, board int, rows []travRow) {
	prev, next := 0, 0
	for i, b := range d.Boards {
		if b == board {
			if i > 0 {
				prev = d.Boards[i-1]
			}
			if i+1 < len(d.Boards) {
				next = d.Boards[i+1]
			}
		}
	}
	vul := [...]string{"Никой", "СЮ", "ИЗ", "Всички"}[score.BoardVulnerability(board)]
	dealer := [...]string{"С", "И", "Ю", "З"}[score.BoardDealer(board)]
	p.Title = fmt.Sprintf("Раздаване %d", board)
	p.Data = map[string]any{"S": d, "Board": board, "Rows": rows, "Prev": prev, "Next": next, "Vul": vul, "Dealer": dealer,
		"Adjusted": adjustedScores, "IMP": d.Scoring != string(pairs.MethodMP)}
	s.render(w, "traveller", p)
}

func (s *Server) saveTraveller(w http.ResponseWriter, r *http.Request) {
	st, p, d, ok := s.loadSession(w, r)
	if !ok {
		return
	}
	board, _ := strconv.Atoi(r.PathValue("board"))
	if d.Expected[board] == 0 {
		s.fail(w, http.StatusNotFound, errors.New("това раздаване не се играе в сесията"))
		return
	}
	rows := travRows(d, board)
	bad := false
	for i := range rows {
		row := &rows[i]
		k := strconv.Itoa(i)
		row.Contract = strings.ToUpper(strings.TrimSpace(r.PostFormValue("c" + k)))
		row.Declarer = strings.ToUpper(strings.TrimSpace(r.PostFormValue("d" + k)))
		row.Result = strings.TrimSpace(r.PostFormValue("r" + k))
		row.Adjusted = r.PostFormValue("a" + k)
		row.Score = nil
		res, del, err := parseRow(board, row)
		if err != nil {
			row.Err, bad = err.Error(), true
			continue
		}
		if del {
			err = st.DeleteResult(d.ID, board, row.NS, row.EW)
		} else {
			res.Round, res.Table = row.Round, row.Table
			err = st.PutResult(d.ID, res)
		}
		if err != nil {
			row.Err, bad = err.Error(), true
		}
	}
	if bad {
		p.Error = "Някои редове не са записани: поправете отбелязаните."
		s.renderTraveller(w, p, d, board, rows)
		return
	}
	target := fmt.Sprintf("/e/%s/s/%s/b/%d", p.EventID, d.Slot, board)
	if r.PostFormValue("go") == "next" {
		for _, b := range d.Boards {
			if b > board {
				target = fmt.Sprintf("/e/%s/s/%s/b/%d", p.EventID, d.Slot, b)
				break
			}
		}
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// parseRow turns a traveller row's inputs into a result; an empty row deletes what was there.
func parseRow(board int, row *travRow) (store.Result, bool, error) {
	res := store.Result{Board: board, NS: row.NS, EW: row.EW}
	if row.Adjusted != "" {
		a, b, ok := strings.Cut(row.Adjusted, "-")
		ns, err1 := strconv.ParseFloat(a, 64)
		ew, err2 := strconv.ParseFloat(b, 64)
		if !ok || err1 != nil || err2 != nil {
			return res, false, errors.New("невалиден присъден резултат")
		}
		res.AdjNS, res.AdjEW = &ns, &ew
		return res, false, nil
	}
	if row.Contract == "" {
		if row.Declarer != "" || row.Result != "" {
			return res, false, errors.New("няма договор")
		}
		return res, true, nil
	}
	c, err := score.ParseContract(strings.ReplaceAll(row.Contract, " ", ""))
	if err != nil {
		return res, false, errors.New("договорът се пише като 4S, 3NTX, 6HXX или PASS")
	}
	res.Contract = c.String()
	if c.Level == 0 {
		return res, false, nil
	}
	if !strings.Contains("NESW", row.Declarer) || len(row.Declarer) != 1 {
		return res, false, errors.New("разиграващият е N, E, S или W")
	}
	res.Declarer = row.Declarer
	tricks := c.Level + 6
	if row.Result != "=" {
		n, err := strconv.Atoi(row.Result)
		if err != nil || n == 0 {
			return res, false, errors.New("резултатът се пише като =, +1 или -2")
		}
		tricks += n
	}
	if tricks < 0 || tricks > 13 {
		return res, false, errors.New("такъв резултат не е възможен")
	}
	res.Tricks = tricks
	res.Score = score.NSScore(board, c, row.Declarer == "N" || row.Declarer == "S", tricks)
	return res, false, nil
}

// --- standings ---

func scoreSession(d *sessionData) (pairs.SessionResult, error) {
	var lines []pairs.Line
	for _, r := range d.Results {
		l := pairs.Line{Board: r.Board, NS: r.NS, EW: r.EW, Played: r.AdjNS == nil, Score: r.Score}
		if r.AdjNS != nil {
			l.AdjNS, l.AdjEW = *r.AdjNS, *r.AdjEW
		}
		lines = append(lines, l)
	}
	return pairs.ScoreSession(d.Movement, d.BoardsPerRound, d.FirstBoard, pairs.Method(d.Scoring), 0.1, lines)
}

type standing struct {
	Place   int
	Tied    bool
	Pair    int
	Names   string
	Score   float64 // MP or IMPs
	Percent float64
	Boards  int
}

// standings ranks the session's pairs, per field: a two-winner Mitchell ranks NS and EW apart.
func standings(d *sessionData) ([][]standing, string, error) {
	res, err := scoreSession(d)
	if err != nil {
		return nil, "", err
	}
	m := d.Movement
	mp := d.Scoring == string(pairs.MethodMP)
	field := func(from, to int) []standing {
		var out []standing
		for p := from; p <= to; p++ {
			if p == m.Phantom || d.Pairs[p] == "" {
				continue
			}
			t := res.Totals[p]
			st := standing{Pair: p, Names: d.Pairs[p], Score: t.Score, Boards: t.Boards}
			if mp && t.Top > 0 {
				st.Percent = 100 * t.Score / t.Top
			}
			out = append(out, st)
		}
		key := func(s standing) float64 {
			if mp {
				return s.Percent
			}
			return s.Score
		}
		sort.SliceStable(out, func(i, j int) bool { return key(out[i]) > key(out[j]) })
		for i := range out {
			out[i].Place = i + 1
			if i > 0 && key(out[i]) == key(out[i-1]) {
				out[i].Place, out[i].Tied, out[i-1].Tied = out[i-1].Place, true, true
			}
		}
		return out
	}
	if m.TwoFields {
		return [][]standing{field(1, m.Tables), field(m.Tables+1, 2*m.Tables)}, "", nil
	}
	return [][]standing{field(1, m.Pairs)}, "", nil
}

func (s *Server) results(w http.ResponseWriter, r *http.Request) {
	_, p, d, ok := s.loadSession(w, r)
	if !ok {
		return
	}
	fields, _, err := standings(d)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	names := []string{""}
	if len(fields) == 2 {
		names = []string{"Север – Юг", "Изток – Запад"}
	}
	missing := 0
	for _, b := range d.Boards {
		if d.Entered[b] < d.Expected[b] {
			missing += d.Expected[b] - d.Entered[b]
		}
	}
	p.Title = "Класиране – " + d.Name
	p.Data = map[string]any{"S": d, "Fields": fields, "FieldNames": names, "MP": d.Scoring == string(pairs.MethodMP), "Missing": missing}
	s.render(w, "results", p)
}

// --- USEBIO ---

func (s *Server) usebio(w http.ResponseWriter, r *http.Request) {
	st, p, d, ok := s.loadSession(w, r)
	if !ok {
		return
	}
	fields, _, err := standings(d)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	res, _ := scoreSession(d)
	entries, _ := st.Entries()
	byID := map[int64]store.Entry{}
	for _, e := range entries {
		byID[e.ID] = e
	}
	method := map[string]usebio.Method{string(pairs.MethodMP): usebio.MatchPoints, string(pairs.MethodCrossIMP): usebio.CrossIMPs, string(pairs.MethodButler): usebio.Butler}[d.Scoring]
	date, _ := time.Parse("2006-01-02", must(st.Meta(store.MetaDate)))
	ev := usebio.PairsEvent{Title: p.Event + " – " + d.Name, Date: date, Method: method}
	for fi, f := range fields {
		for _, sd := range f {
			pr := usebio.Pair{Number: sd.Pair, Place: sd.Place, Tied: sd.Tied, Score: sd.Score, Percent: sd.Percent, Boards: sd.Boards}
			if len(fields) == 2 {
				pr.Direction = [...]string{"NS", "EW"}[fi]
			}
			for _, pl := range byID[d.Seating[sd.Pair-1]].Players {
				pr.Players = append(pr.Players, usebio.Player{Name: pl.Name, FederationID: pl.FederationID})
			}
			ev.Pairs = append(ev.Pairs, pr)
		}
	}
	points := map[[3]int][2]float64{}
	for _, l := range res.Lines {
		points[[3]int{l.Board, l.NS, l.EW}] = [2]float64{l.NSPoints, l.EWPoints}
	}
	byBoard := map[int]*usebio.Board{}
	for _, r := range d.Results {
		b := byBoard[r.Board]
		if b == nil {
			b = &usebio.Board{Number: r.Board}
			byBoard[r.Board] = b
		}
		pt := points[[3]int{r.Board, r.NS, r.EW}]
		line := usebio.Line{Round: r.Round, NS: r.NS, EW: r.EW, NSPoints: pt[0], EWPoints: pt[1]}
		if r.AdjNS != nil {
			line.Adjusted = fmt.Sprintf("A%g/%g", *r.AdjNS, *r.AdjEW)
		} else {
			c, _ := score.ParseContract(r.Contract)
			line.Played, line.Contract, line.Declarer, line.Lead, line.Tricks, line.Score = true, c, r.Declarer, r.Lead, r.Tricks, r.Score
		}
		b.Lines = append(b.Lines, line)
	}
	for _, n := range d.Boards {
		if b := byBoard[n]; b != nil {
			ev.Boards = append(ev.Boards, *b)
		}
	}
	var buf bytes.Buffer
	if err := usebio.WritePairs(&buf, ev); err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.%s.xml"`, p.EventID, d.Slot))
	w.Write(buf.Bytes())
}

func must(s string, _ error) string { return s }
