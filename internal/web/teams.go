package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zdraganov/Score52/internal/score"
	"github.com/zdraganov/Score52/internal/store"
	"github.com/zdraganov/Score52/internal/teams"
	"github.com/zdraganov/Score52/internal/usebio"
)

// A teams event: the teams are entries (a name and 4-6 players); once started it is the teams.Event
// replayed from its log. Each match is two rooms, the open one with the home team NS; the boards of both
// are entered and the match's IMPs and VPs follow from them.

const (
	kindPairs = "pairs"
	kindTeams = "teams"
)

var formatNames = map[teams.Format]string{teams.Swiss: "Швейцарска система", teams.Danish: "Датска система",
	teams.RoundRobin: "Всеки срещу всеки", teams.Knockout: "Директни елиминации"}

// teamsSetup is chosen before the start.
type teamsSetup struct {
	Format teams.Format `json:"format"`
	Boards int          `json:"boards"` // per match
	ByeVP  float64      `json:"bye_vp"`
}

func loadSetup(st *store.Store) teamsSetup {
	ts := teamsSetup{Format: teams.Swiss, Boards: 8, ByeVP: 12}
	if v, _ := st.Meta(store.MetaTeamsSetup); v != "" {
		json.Unmarshal([]byte(v), &ts)
	}
	return ts
}

func eventKind(st *store.Store) string {
	if k, _ := st.Meta(store.MetaKind); k == kindTeams {
		return kindTeams
	}
	return kindPairs
}

func teamName(e store.Entry) string {
	if e.Name != "" {
		return e.Name
	}
	return names(e)
}

// --- the event page ---

type matchView struct {
	teams.Match
	Table              int
	HomeName, AwayName string
	Entered            int // boards complete
	GroupLabel         string
}

// Winner is a decided knockout match's winner.
func (m matchView) Winner() string {
	h, a := m.KOTotal()
	switch {
	case !m.Played || m.Tied:
		return ""
	case m.Bye() || h > a:
		return m.HomeName
	}
	return m.AwayName
}

func (m matchView) TotalHome() float64 { h, _ := m.KOTotal(); return h }
func (m matchView) TotalAway() float64 { _, a := m.KOTotal(); return a }

type roundView struct {
	Round   int
	Boards  int // the most boards a match of the round plays
	Matches []matchView
}

func (s *Server) teamsEvent(w http.ResponseWriter, r *http.Request, st *store.Store, p page) {
	entries, err := st.Entries()
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	ev, err := st.LoadTeams()
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	date, _ := st.Meta(store.MetaDate)
	setup := loadSetup(st)
	fmts := map[string]string{}
	for f, n := range formatNames {
		fmts[string(f)] = n
	}
	data := map[string]any{"Entries": entries, "Date": date, "Setup": setup, "Formats": fmts, "Started": ev != nil}
	if ev != nil {
		rounds, err := roundViews(st, ev, entries)
		if err != nil {
			s.fail(w, http.StatusInternalServerError, err)
			return
		}
		data["Rounds"], data["NextRound"] = rounds, ev.Round+1
		data["CanPair"] = allPlayed(ev)
		data["Format"] = formatNames[setup.Format]
		data["Groups"] = groupViews(ev)
		data["Bridgemate"] = s.bws != nil
		data["CurrentRound"] = ev.Round
		data["HasBws"] = ev.Round > 0 && s.bws != nil && fileExists(s.roundBws(p.EventID, ev.Round))
		data["BMNote"] = bmNote(r.URL.Query(), s.roundBws(p.EventID, ev.Round))
		data["CanChange"] = allPlayed(ev) && ev.Round > 0
	}
	p.Title = p.Event
	p.Data = data
	s.render(w, "teams-event", p)
}

func allPlayed(ev *teams.Event) bool {
	for _, m := range ev.Matches {
		if !m.Played || m.Tied {
			return false
		}
	}
	return true
}

func roundViews(st *store.Store, ev *teams.Event, entries []store.Entry) ([]roundView, error) {
	byID := map[int64]store.Entry{}
	for _, e := range entries {
		byID[e.ID] = e
	}
	boards, err := st.TeamBoards(0)
	if err != nil {
		return nil, err
	}
	done := map[[3]int]map[int]map[string]bool{}
	for _, b := range boards {
		k := [3]int{b.Round, b.Home, b.Away}
		if done[k] == nil {
			done[k] = map[int]map[string]bool{}
		}
		if done[k][b.Board] == nil {
			done[k][b.Board] = map[string]bool{}
		}
		done[k][b.Board][b.Room] = true
	}
	var out []roundView
	for _, m := range ev.Matches {
		if len(out) == 0 || out[len(out)-1].Round != m.Round {
			out = append(out, roundView{Round: m.Round})
		}
		rv := &out[len(out)-1]
		rv.Boards = max(rv.Boards, m.Boards)
		mv := matchView{Match: m, HomeName: teamName(byID[int64(m.Home)]), AwayName: teamName(byID[int64(m.Away)])}
		if m.Group != "" {
			mv.GroupLabel = groupLabel(m.Group)
		}
		if !m.Bye() {
			for _, rooms := range done[[3]int{m.Round, int(m.Home), int(m.Away)}] {
				if rooms[store.RoomAdj] || rooms[store.RoomOpen] && rooms[store.RoomClosed] {
					mv.Entered++
				}
			}
			mv.Table = 1
			for _, x := range rv.Matches {
				if !x.Bye() {
					mv.Table++
				}
			}
		}
		rv.Matches = append(rv.Matches, mv)
	}
	return out, nil
}

func (s *Server) addTeam(w http.ResponseWriter, r *http.Request) {
	st, p, ok := s.eventPage(w, r)
	if !ok {
		return
	}
	if ev, _ := st.LoadTeams(); ev != nil {
		s.fail(w, http.StatusBadRequest, errors.New("състезанието е започнало: отбори не се добавят"))
		return
	}
	name := strings.TrimSpace(r.PostFormValue("name"))
	if name == "" {
		s.fail(w, http.StatusBadRequest, errors.New("отборът няма име"))
		return
	}
	var ids []int64
	for k := 1; k <= 6; k++ {
		pn := strings.TrimSpace(r.PostFormValue(fmt.Sprint("p", k)))
		if pn == "" {
			continue
		}
		id, err := st.AddPlayer(store.Player{Name: pn, FederationID: strings.TrimSpace(r.PostFormValue(fmt.Sprint("ibn", k)))})
		if err != nil {
			s.fail(w, http.StatusInternalServerError, err)
			return
		}
		ids = append(ids, id)
	}
	if len(ids) < 4 {
		s.fail(w, http.StatusBadRequest, errors.New("отборът трябва да има поне 4 играчи"))
		return
	}
	entries, _ := st.Entries()
	if _, err := st.AddEntry(name, len(entries)+1, ids); err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	http.Redirect(w, r, "/e/"+p.EventID+"#teams", http.StatusSeeOther)
}

func (s *Server) deleteTeam(w http.ResponseWriter, r *http.Request) {
	st, p, ok := s.eventPage(w, r)
	if !ok {
		return
	}
	if ev, _ := st.LoadTeams(); ev != nil {
		s.fail(w, http.StatusBadRequest, errors.New("състезанието е започнало: отборите не се трият"))
		return
	}
	id, err := strconv.ParseInt(r.PathValue("tid"), 10, 64)
	if err == nil {
		err = st.DeleteEntry(id)
	}
	if err != nil {
		s.fail(w, http.StatusBadRequest, fmt.Errorf("отборът не може да се изтрие: %w", err))
		return
	}
	http.Redirect(w, r, "/e/"+p.EventID+"#teams", http.StatusSeeOther)
}

// startTeams fixes the teams and the format; the seeds are the order the teams were entered in.
func (s *Server) startTeams(w http.ResponseWriter, r *http.Request) {
	st, p, ok := s.eventPage(w, r)
	if !ok {
		return
	}
	if ev, _ := st.LoadTeams(); ev != nil {
		s.fail(w, http.StatusBadRequest, errors.New("състезанието вече е започнало"))
		return
	}
	setup := teamsSetup{Format: teams.Format(r.PostFormValue("format"))}
	var err1, err2 error
	setup.Boards, err1 = strconv.Atoi(r.PostFormValue("boards"))
	setup.ByeVP, err2 = strconv.ParseFloat(strings.ReplaceAll(r.PostFormValue("bye_vp"), ",", "."), 64)
	if _, ok := formatNames[setup.Format]; !ok || setup.Format == teams.Knockout || err1 != nil || err2 != nil ||
		setup.Boards < 1 || setup.Boards > 64 || setup.ByeVP < 0 || setup.ByeVP > 20 {
		s.fail(w, http.StatusBadRequest, errors.New("невалидни настройки"))
		return
	}
	entries, err := st.Entries()
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	if len(entries) < 3 {
		s.fail(w, http.StatusBadRequest, errors.New("трябват поне 3 отбора"))
		return
	}
	var ts []teams.Team
	for i, e := range entries {
		ts = append(ts, teams.Team{ID: teams.TeamID(e.ID), Name: teamName(e), Seed: i + 1})
	}
	ev, err := teams.New(ts, setup.Format, "wbf20", setup.ByeVP)
	if err != nil {
		s.fail(w, http.StatusBadRequest, err)
		return
	}
	b, _ := json.Marshal(setup)
	if err := st.SetMeta(store.MetaTeamsSetup, string(b)); err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	if err := st.SaveTeams(ev); err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	http.Redirect(w, r, "/e/"+p.EventID, http.StatusSeeOther)
}

// pairTeams makes the next round's matches.
func (s *Server) pairTeams(w http.ResponseWriter, r *http.Request) {
	st, p, ok := s.eventPage(w, r)
	if !ok {
		return
	}
	ev, err := st.LoadTeams()
	if err != nil || ev == nil {
		s.fail(w, http.StatusBadRequest, errors.New("състезанието не е започнало"))
		return
	}
	boards, err := strconv.Atoi(r.PostFormValue("boards"))
	if err != nil || boards < 1 || boards > 64 {
		s.fail(w, http.StatusBadRequest, errors.New("невалиден брой раздавания"))
		return
	}
	per := map[string]int{}
	for i, g := range ev.Groups {
		if v := strings.TrimSpace(r.PostFormValue(fmt.Sprint("boards", i))); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 64 {
				s.fail(w, http.StatusBadRequest, fmt.Errorf("%s: невалиден брой раздавания", groupLabel(g.Name)))
				return
			}
			per[g.Name] = n
		}
	}
	if _, err := ev.PairRoundBoards(boards, per); err != nil {
		s.fail(w, http.StatusBadRequest, fmt.Errorf("рундът не може да се чифтоса: %w", err))
		return
	}
	if err := st.SaveTeams(ev); err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/e/%s#r%d", p.EventID, ev.Round), http.StatusSeeOther)
}

// --- a match ---

type roomEntry struct {
	Contract, Declarer, Result string
	Score                      *int
	Err                        string
}

type boardRow struct {
	Board            int
	Entered          bool // something is stored under this board number
	Dealer, Vul      string
	Open, Closed     roomEntry
	AdjHome, AdjAway string
	IMPHome, IMPAway *float64 // nil until both rooms (or an adjusted score) are in
	Err              string
}

type matchData struct {
	Round, Table       int
	View               matchView
	Match              teams.Match
	HomeName, AwayName string
	Rows               []boardRow
	IMPsHome, IMPsAway float64
	Complete           bool
	VPHome, VPAway     float64
}

func (s *Server) loadMatch(w http.ResponseWriter, r *http.Request) (*store.Store, page, *teams.Event, *matchData, bool) {
	st, p, ok := s.eventPage(w, r)
	if !ok {
		return nil, p, nil, nil, false
	}
	ev, err := st.LoadTeams()
	if err != nil || ev == nil {
		s.fail(w, http.StatusNotFound, errors.New("състезанието не е започнало"))
		return nil, p, nil, nil, false
	}
	round, _ := strconv.Atoi(r.PathValue("round"))
	home, _ := strconv.Atoi(r.PathValue("home"))
	entries, _ := st.Entries()
	rounds, err := roundViews(st, ev, entries)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return nil, p, nil, nil, false
	}
	for _, rv := range rounds {
		for _, mv := range rv.Matches {
			if rv.Round == round && int(mv.Home) == home && !mv.Bye() {
				md := &matchData{Round: round, Table: mv.Table, View: mv, Match: mv.Match, HomeName: mv.HomeName, AwayName: mv.AwayName}
				boards, err := st.TeamBoards(round)
				if err != nil {
					s.fail(w, http.StatusInternalServerError, err)
					return nil, p, nil, nil, false
				}
				md.fill(boards)
				return st, p, ev, md, true
			}
		}
	}
	s.fail(w, http.StatusNotFound, errors.New("няма такъв мач"))
	return nil, p, nil, nil, false
}

// fill makes the match's board rows from what was entered, and its totals. The board numbers are the
// table's: whatever the two rooms played (they play the same boards), in order, and empty rows up to
// the match's number of boards, numbered on from the last one.
func (md *matchData) fill(boards []store.TeamBoard) {
	have := map[[2]any]store.TeamBoard{}
	seen := map[int]bool{}
	var nums []int
	for _, b := range boards {
		if b.Home == int(md.Match.Home) && b.Away == int(md.Match.Away) {
			have[[2]any{b.Board, b.Room}] = b
			if !seen[b.Board] {
				seen[b.Board] = true
				nums = append(nums, b.Board)
			}
		}
	}
	sort.Ints(nums)
	next := 1
	if len(nums) > 0 {
		next = nums[len(nums)-1] + 1
	}
	for len(nums) < md.Match.Boards {
		for seen[next] {
			next++
		}
		seen[next] = true
		nums = append(nums, next)
	}
	md.Rows = nil
	for _, n := range nums {
		row := boardRow{Board: n, Vul: [...]string{"Никой", "СЮ", "ИЗ", "Всички"}[score.BoardVulnerability(n)],
			Dealer: [...]string{"С", "И", "Ю", "З"}[score.BoardDealer(n)]}
		for _, x := range []struct {
			room string
			e    *roomEntry
		}{{store.RoomOpen, &row.Open}, {store.RoomClosed, &row.Closed}} {
			if b, ok := have[[2]any{n, x.room}]; ok {
				row.Entered = true
				x.e.Contract, x.e.Declarer = b.Contract, b.Declarer
				sc := b.Score
				x.e.Score = &sc
				if c, err := score.ParseContract(b.Contract); err == nil && c.Level > 0 {
					x.e.Result = relative(b.Tricks - c.Level - 6)
				}
			}
		}
		if b, ok := have[[2]any{n, store.RoomAdj}]; ok && b.AdjHome != nil {
			row.Entered = true
			row.AdjHome, row.AdjAway = num(*b.AdjHome), num(*b.AdjAway)
		}
		md.Rows = append(md.Rows, row)
	}
	md.total()
}

func num(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// total works out each board's IMPs and the match's.
func (md *matchData) total() {
	md.IMPsHome, md.IMPsAway, md.Complete = 0, 0, true
	done := 0
	for i := range md.Rows {
		row := &md.Rows[i]
		row.IMPHome, row.IMPAway = nil, nil
		var h, a float64
		switch {
		case row.AdjHome != "":
			h, _ = strconv.ParseFloat(row.AdjHome, 64)
			a, _ = strconv.ParseFloat(row.AdjAway, 64)
		case row.Open.Score != nil && row.Closed.Score != nil:
			d := score.TeamBoardIMP(*row.Open.Score, *row.Closed.Score)
			h, a = float64(max(d, 0)), float64(max(-d, 0))
		default:
			md.Complete = false
			continue
		}
		done++
		row.IMPHome, row.IMPAway = &h, &a
		md.IMPsHome += h
		md.IMPsAway += a
	}
	md.Complete = md.Complete && done == md.Match.Boards
	if md.Complete {
		md.VPHome, md.VPAway = score.MatchVP(score.WBF20{}, md.IMPsHome-md.IMPsAway, md.Match.Boards)
	}
}

// recordMatch puts a complete match into the event (again, when a correction changed it).
func recordMatch(st *store.Store, ev *teams.Event, md *matchData) error {
	if !md.Complete || md.Match.Played && md.Match.IMPsHome == md.IMPsHome && md.Match.IMPsAway == md.IMPsAway {
		return nil
	}
	if err := ev.Record(md.Round, md.Match.Home, md.Match.Away, md.IMPsHome, md.IMPsAway); err != nil {
		return err
	}
	return st.SaveTeams(ev)
}

func (s *Server) teamMatch(w http.ResponseWriter, r *http.Request) {
	_, p, _, md, ok := s.loadMatch(w, r)
	if !ok {
		return
	}
	s.renderMatch(w, p, md)
}

func (s *Server) renderMatch(w http.ResponseWriter, p page, md *matchData) {
	p.Title = fmt.Sprintf("Рунд %d, маса %d", md.Round, md.Table)
	p.Data = md
	s.render(w, "team-match", p)
}

func (s *Server) saveTeamMatch(w http.ResponseWriter, r *http.Request) {
	st, p, ev, md, ok := s.loadMatch(w, r)
	if !ok {
		return
	}
	bad := false
	key := func(b int, room string) store.TeamBoard {
		return store.TeamBoard{Round: md.Round, Home: int(md.Match.Home), Away: int(md.Match.Away), Room: room, Board: b}
	}
	home, away := int(md.Match.Home), int(md.Match.Away)
	// the board numbers first: the table played whichever boards it got; a renumbered row moves
	used := map[int]bool{}
	for i := range md.Rows {
		row := &md.Rows[i]
		n, err := row.Board, error(nil)
		if v := strings.TrimSpace(r.PostFormValue("b" + strconv.Itoa(i))); v != "" {
			n, err = strconv.Atoi(v)
		}
		switch {
		case err != nil || n < 1 || n > 999:
			row.Err, bad = "номерът на раздаването е от 1 до 999", true
		case used[n]:
			row.Err, bad = fmt.Sprintf("раздаване %d е два пъти", n), true
		}
		used[n] = true
		if row.Err == "" && n != row.Board {
			if row.Entered {
				for _, room := range []string{store.RoomOpen, store.RoomClosed, store.RoomAdj} {
					if err := st.DeleteTeamBoard(md.Round, home, away, room, row.Board); err != nil {
						s.fail(w, http.StatusInternalServerError, err)
						return
					}
				}
			}
			row.Board = n
		}
	}
	for i := range md.Rows {
		row := &md.Rows[i]
		k := strconv.Itoa(i)
		if row.Err != "" {
			continue
		}
		for _, x := range []struct {
			room, f string
			e       *roomEntry
		}{{store.RoomOpen, "o", &row.Open}, {store.RoomClosed, "c", &row.Closed}} {
			tr := travRow{Contract: strings.ToUpper(strings.TrimSpace(r.PostFormValue(x.f + "c" + k))),
				Declarer: strings.ToUpper(strings.TrimSpace(r.PostFormValue(x.f + "d" + k))),
				Result:   strings.TrimSpace(r.PostFormValue(x.f + "r" + k))}
			*x.e = roomEntry{Contract: tr.Contract, Declarer: tr.Declarer, Result: tr.Result}
			res, del, err := parseRow(row.Board, &tr)
			if err == nil && del {
				err = st.DeleteTeamBoard(md.Round, int(md.Match.Home), int(md.Match.Away), x.room, row.Board)
			} else if err == nil {
				b := key(row.Board, x.room)
				b.Contract, b.Declarer, b.Tricks, b.Score = res.Contract, res.Declarer, res.Tricks, res.Score
				if err = st.PutTeamBoard(b); err == nil {
					sc := res.Score
					x.e.Contract, x.e.Score = res.Contract, &sc
				}
			}
			if err != nil {
				x.e.Err, bad = err.Error(), true
			}
		}
		row.AdjHome = strings.ReplaceAll(strings.TrimSpace(r.PostFormValue("ah"+k)), ",", ".")
		row.AdjAway = strings.ReplaceAll(strings.TrimSpace(r.PostFormValue("aa"+k)), ",", ".")
		var err error
		if row.AdjHome == "" && row.AdjAway == "" {
			err = st.DeleteTeamBoard(md.Round, int(md.Match.Home), int(md.Match.Away), store.RoomAdj, row.Board)
		} else {
			h, err1 := strconv.ParseFloat(row.AdjHome, 64)
			a, err2 := strconv.ParseFloat(row.AdjAway, 64)
			if err1 != nil || err2 != nil || h < 0 || a < 0 || math.IsInf(h, 0) || math.IsInf(a, 0) {
				err = errors.New("присъденото е IMP за всеки отбор: две числа, 0 или повече")
			} else {
				b := key(row.Board, store.RoomAdj)
				b.AdjHome, b.AdjAway = &h, &a
				err = st.PutTeamBoard(b)
			}
		}
		if err != nil {
			row.Err, bad = err.Error(), true
		}
	}
	if bad {
		md.total()
		p.Error = "Някои раздавания не са записани: поправете отбелязаните."
		s.renderMatch(w, p, md)
		return
	}
	boards, err := st.TeamBoards(md.Round)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	md.fill(boards)
	if err := recordMatch(st, ev, md); err != nil {
		s.fail(w, http.StatusBadRequest, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/e/%s/r/%d/m/%d", p.EventID, md.Round, md.Match.Home), http.StatusSeeOther)
}

// --- standings and the file for bridge.bg ---

type teamRow struct {
	teams.Standing
	Name, Players string
	Tied          bool
	PerRound      []*float64 // VPs in each round, nil when it didn't play (yet)
	Out           int        // knockout: the round it went out in
}

type groupStanding struct {
	Name, Label, Format string
	Knockout            bool
	Rows                []teamRow
}

func teamStandings(st *store.Store) (*teams.Event, []groupStanding, map[int64]store.Entry, error) {
	ev, err := st.LoadTeams()
	if err != nil || ev == nil {
		return ev, nil, nil, err
	}
	entries, err := st.Entries()
	if err != nil {
		return ev, nil, nil, err
	}
	byID := map[int64]store.Entry{}
	for _, e := range entries {
		byID[e.ID] = e
	}
	per := map[teams.TeamID][]*float64{}
	for _, m := range ev.Matches {
		if !m.Played {
			continue
		}
		for _, side := range []struct {
			id teams.TeamID
			vp float64
		}{{m.Home, m.VPHome}, {m.Away, m.VPAway}} {
			if side.id == 0 {
				continue
			}
			if per[side.id] == nil {
				per[side.id] = make([]*float64, ev.Round)
			}
			v := side.vp
			per[side.id][m.Round-1] = &v
		}
	}
	outIn := map[teams.TeamID]int{}
	for _, m := range ev.Matches {
		if m.Knockout && m.Played && !m.Tied && !m.Bye() {
			if h, a := m.KOTotal(); h > a {
				outIn[m.Away] = m.Round
			} else {
				outIn[m.Home] = m.Round
			}
		}
	}
	var groups []groupStanding
	for _, g := range ev.Groups {
		gs := groupStanding{Name: g.Name, Label: groupLabel(g.Name), Format: formatNames[g.Format], Knockout: g.Format == teams.Knockout}
		ranked := ev.Standings(g.Name)
		if gs.Knockout {
			ranked = koRanking(ev, g.Name)
		}
		for _, sd := range ranked {
			e := byID[int64(sd.Team)]
			row := teamRow{Standing: sd, Name: teamName(e), Players: names(e), PerRound: per[sd.Team]}
			if gs.Knockout {
				row.Out = outIn[sd.Team]
			}
			if row.PerRound == nil {
				row.PerRound = make([]*float64, ev.Round)
			}
			gs.Rows = append(gs.Rows, row)
		}
		rows := gs.Rows
		for i := range rows {
			rows[i].Tied = i > 0 && rows[i].Rank == rows[i-1].Rank || i+1 < len(rows) && rows[i+1].Rank == rows[i].Rank
		}
		groups = append(groups, gs)
	}
	return ev, groups, byID, nil
}

func (s *Server) teamsStandings(w http.ResponseWriter, st *store.Store, p page) {
	ev, groups, _, err := teamStandings(st)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	if ev == nil {
		s.fail(w, http.StatusBadRequest, errors.New("състезанието не е започнало"))
		return
	}
	var rounds []int
	for r := 1; r <= ev.Round; r++ {
		rounds = append(rounds, r)
	}
	p.Title = "Класиране – " + p.Event
	p.Data = map[string]any{"Groups": groups, "Rounds": rounds}
	s.render(w, "team-standings", p)
}

func (s *Server) teamsUsebio(w http.ResponseWriter, r *http.Request) {
	st, p, ok := s.eventPage(w, r)
	if !ok {
		return
	}
	ev, groups, byID, err := teamStandings(st)
	if err != nil || ev == nil {
		s.fail(w, http.StatusBadRequest, errors.New("състезанието не е започнало"))
		return
	}
	date, _ := st.Meta(store.MetaDate)
	d, _ := time.Parse("2006-01-02", date)
	out := usebio.TeamsEvent{Title: p.Event, Date: d}
	// bridge.bg ranks group after group: A's places, then B's
	for _, g := range groups {
		for _, row := range g.Rows {
			t := usebio.Team{ID: int(row.Team), Name: row.Name, Group: g.Name, Place: row.Rank, Tied: row.Tied, VP: row.VP}
			for _, pl := range byID[int64(row.Team)].Players {
				t.Players = append(t.Players, usebio.Player{Name: pl.Name, FederationID: pl.FederationID})
			}
			out.Teams = append(out.Teams, t)
		}
	}
	// a match's boards: the lowest and highest number its tables played
	all, err := st.TeamBoards(0)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	span := map[[3]int][2]int{}
	for _, b := range all {
		k := [3]int{b.Round, b.Home, b.Away}
		x, ok := span[k]
		if !ok {
			x = [2]int{b.Board, b.Board}
		}
		span[k] = [2]int{min(x[0], b.Board), max(x[1], b.Board)}
	}
	for _, m := range ev.Matches {
		if m.Bye() {
			continue
		}
		x, ok := span[[3]int{m.Round, int(m.Home), int(m.Away)}]
		if !ok {
			x = [2]int{1, m.Boards}
		}
		out.Matches = append(out.Matches, usebio.Match{Played: m.Played, Round: m.Round, Home: int(m.Home), Away: int(m.Away),
			FirstBoard: x[0], LastBoard: x[1], IMPsHome: m.IMPsHome, IMPsAway: m.IMPsAway, VPHome: m.VPHome, VPAway: m.VPAway})
	}
	var buf bytes.Buffer
	if err := usebio.WriteTeams(&buf, out); err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.xml"`, fileBase(st, p.EventID)))
	w.Write(buf.Bytes())
}

// --- groups: split, exchange, format; knockout carry-over and extra boards ---

type groupView struct {
	Name, Label, Format string
	Teams               int
}

func groupLabel(name string) string {
	if name == "" {
		return "Общо"
	}
	return "Група " + name
}

func groupViews(ev *teams.Event) []groupView {
	var out []groupView
	for _, g := range ev.Groups {
		out = append(out, groupView{Name: g.Name, Label: groupLabel(g.Name), Format: formatNames[g.Format], Teams: len(ev.Standings(g.Name))})
	}
	return out
}

func parseCarry(kind, pct string) (teams.Carry, error) {
	c := teams.Carry{Kind: teams.CarryKind(kind)}
	switch c.Kind {
	case teams.CarryAll, teams.CarryHeadToHead:
	case teams.CarryNone:
		return c, nil
	default:
		return c, errors.New("пренасянето е: всичко, процент, нищо или от срещите помежду им")
	}
	pct = strings.TrimSpace(strings.ReplaceAll(pct, ",", "."))
	if pct == "" {
		return c, nil
	}
	v, err := strconv.ParseFloat(pct, 64)
	if err != nil || v <= 0 || v > 100 {
		return c, errors.New("процентът на пренасянето е между 0 и 100")
	}
	c.Fraction = v / 100
	return c, nil
}

// teamsChange applies a change to a started event and saves it.
func (s *Server) teamsChange(w http.ResponseWriter, r *http.Request, f func(st *store.Store, ev *teams.Event) error, back func(ev *teams.Event) string) {
	st, p, ok := s.eventPage(w, r)
	if !ok {
		return
	}
	ev, err := st.LoadTeams()
	if err != nil || ev == nil {
		s.fail(w, http.StatusBadRequest, errors.New("състезанието не е започнало"))
		return
	}
	if err := f(st, ev); err != nil {
		s.fail(w, http.StatusBadRequest, err)
		return
	}
	if err := st.SaveTeams(ev); err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	http.Redirect(w, r, "/e/"+p.EventID+back(ev), http.StatusSeeOther)
}

func (s *Server) splitTeams(w http.ResponseWriter, r *http.Request) {
	s.teamsChange(w, r, func(st *store.Store, ev *teams.Event) error {
		from := r.PostFormValue("from")
		var specs []teams.GroupSpec
		names := map[string]bool{}
		for k := 1; k <= 4; k++ {
			name := strings.ToUpper(strings.TrimSpace(r.PostFormValue(fmt.Sprint("name", k))))
			if name == "" {
				continue
			}
			if names[name] || len([]rune(name)) > 3 {
				return fmt.Errorf("група %q: имената са кратки и различни (A, B, C)", name)
			}
			names[name] = true
			sp := teams.GroupSpec{Name: name, Format: teams.Format(r.PostFormValue(fmt.Sprint("format", k)))}
			if _, ok := formatNames[sp.Format]; !ok {
				return fmt.Errorf("група %s: няма такава система", name)
			}
			if v := strings.TrimSpace(r.PostFormValue(fmt.Sprint("count", k))); v != "" {
				n, err := strconv.Atoi(v)
				if err != nil || n < 2 {
					return fmt.Errorf("група %s: поне 2 отбора", name)
				}
				sp.Count = n
			}
			c, err := parseCarry(r.PostFormValue(fmt.Sprint("carry", k)), r.PostFormValue(fmt.Sprint("pct", k)))
			if err != nil {
				return fmt.Errorf("група %s: %w", name, err)
			}
			sp.Carry = c
			specs = append(specs, sp)
		}
		if len(specs) < 2 {
			return errors.New("разделянето е на поне 2 групи")
		}
		if err := ev.Split(from, specs); err != nil {
			return fmt.Errorf("групите не могат да се направят: %w", err)
		}
		return nil
	}, func(*teams.Event) string { return "#groups" })
}

func (s *Server) exchangeTeams(w http.ResponseWriter, r *http.Request) {
	s.teamsChange(w, r, func(st *store.Store, ev *teams.Event) error {
		n, err := strconv.Atoi(r.PostFormValue("n"))
		if err != nil || n < 1 {
			return errors.New("колко отбора се разменят")
		}
		c, err := parseCarry(r.PostFormValue("carry"), r.PostFormValue("pct"))
		if err != nil {
			return err
		}
		if err := ev.Exchange(r.PostFormValue("upper"), r.PostFormValue("lower"), n, c); err != nil {
			return fmt.Errorf("размяната не става: %w", err)
		}
		return nil
	}, func(*teams.Event) string { return "#groups" })
}

func (s *Server) formatTeams(w http.ResponseWriter, r *http.Request) {
	s.teamsChange(w, r, func(st *store.Store, ev *teams.Event) error {
		f := teams.Format(r.PostFormValue("format"))
		g := ev.Group(r.PostFormValue("group"))
		if _, ok := formatNames[f]; !ok || g == nil {
			return errors.New("няма такава група или система")
		}
		if g.Format == f {
			return nil
		}
		return ev.SetFormat(g.Name, f)
	}, func(*teams.Event) string { return "#groups" })
}

func (s *Server) koCarry(w http.ResponseWriter, r *http.Request) {
	s.koChange(w, r, func(ev *teams.Event, m teams.Match) error {
		h, err1 := parseIMP(r.PostFormValue("carry_home"))
		a, err2 := parseIMP(r.PostFormValue("carry_away"))
		if err1 != nil || err2 != nil {
			return errors.New("пренесеното е IMP: числа, 0 или повече")
		}
		return ev.SetCarry(m.Round, m.Home, m.Away, h, a)
	})
}

func (s *Server) koPlayoff(w http.ResponseWriter, r *http.Request) {
	s.koChange(w, r, func(ev *teams.Event, m teams.Match) error {
		b, err := strconv.Atoi(r.PostFormValue("boards"))
		h, err1 := parseIMP(r.PostFormValue("imps_home"))
		a, err2 := parseIMP(r.PostFormValue("imps_away"))
		if err != nil || b < 1 || b > 8 || err1 != nil || err2 != nil {
			return errors.New("допълнителните раздавания: брой (1-8) и IMP на всеки отбор")
		}
		return ev.AddPlayoff(m.Round, m.Home, m.Away, b, h, a)
	})
}

func parseIMP(v string) (float64, error) {
	v = strings.TrimSpace(strings.ReplaceAll(v, ",", "."))
	if v == "" {
		return 0, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err == nil && (f < 0 || math.IsInf(f, 0) || math.IsNaN(f)) {
		err = errors.New("negative")
	}
	return f, err
}

func (s *Server) koChange(w http.ResponseWriter, r *http.Request, f func(ev *teams.Event, m teams.Match) error) {
	st, p, ev, md, ok := s.loadMatch(w, r)
	if !ok {
		return
	}
	if !md.Match.Knockout {
		s.fail(w, http.StatusBadRequest, errors.New("мачът не е на директни елиминации"))
		return
	}
	if err := f(ev, md.Match); err != nil {
		s.fail(w, http.StatusBadRequest, err)
		return
	}
	if err := st.SaveTeams(ev); err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/e/%s/r/%d/m/%d", p.EventID, md.Round, md.Match.Home), http.StatusSeeOther)
}

// koRanking ranks a knockout group: the teams still in first, then by the round they went out in, later
// first; teams out in the same round share the place.
func koRanking(ev *teams.Event, group string) []teams.Standing {
	all := ev.Standings(group)
	out := map[teams.TeamID]int{}
	for _, m := range ev.Matches {
		if m.Group != group || !m.Knockout || !m.Played || m.Tied || m.Bye() {
			continue
		}
		h, a := m.KOTotal()
		if h > a {
			out[m.Away] = m.Round
		} else {
			out[m.Home] = m.Round
		}
	}
	key := func(s teams.Standing) int {
		if r, ok := out[s.Team]; ok {
			return r
		}
		return math.MaxInt
	}
	sorted := append([]teams.Standing(nil), all...)
	for i := 1; i < len(sorted); i++ { // stable insertion: keeps the standings' order among equals
		for j := i; j > 0 && key(sorted[j]) > key(sorted[j-1]); j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	for i := range sorted {
		sorted[i].Rank = i + 1
		if i > 0 && key(sorted[i]) == key(sorted[i-1]) && key(sorted[i]) != math.MaxInt {
			sorted[i].Rank = sorted[i-1].Rank
		}
	}
	return sorted
}
