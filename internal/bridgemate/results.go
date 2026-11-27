package bridgemate

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zdraganov/Score52/internal/pairs"
	"github.com/zdraganov/Score52/internal/score"
)

// Received is a row of ReceivedData as BCS writes it.
type Received struct {
	ID                    int
	Section, Table, Round int
	Board                 int
	PairNS, PairEW        int
	Declarer              int    // the declaring pair's number
	Direction             string // [NS/EW]: the declarer's seat N/E/S/W, or NS/EW
	Contract              string // "4 H x", "PASS", "" (adjusted / not played)
	Result                string // "=", "+1", "-2"
	LeadCard              string
	Remarks               string
	Logged                time.Time
	Erased                bool
}

// Status of a board's result.
type Status int

const (
	Played    Status = iota
	Adjusted         // an artificial score: AdjNS/AdjEW percentages
	NotPlayed        // "Not played": the TD decides later
	TDPending        // "Arbitral score": the TD has to enter it
)

// Board is the current result of one board at one table.
type Board struct {
	Received
	Status       Status
	Contract     score.Contract
	DeclarerNS   bool
	Tricks       int
	ScoreNS      int
	AdjNS, AdjEW float64
	// WrongDirection: BCS flagged the board as played the wrong way round; the score is as entered
	WrongDirection bool
}

// Line is the board as a traveller line for scoring; ok is false while there is nothing to score.
func (b Board) Line() (pairs.Line, bool) {
	l := pairs.Line{Board: b.Received.Board, NS: b.PairNS, EW: b.PairEW}
	switch b.Status {
	case Played:
		l.Played, l.Score = true, b.ScoreNS
	case Adjusted:
		l.AdjNS, l.AdjEW = b.AdjNS, b.AdjEW
	default:
		return l, false
	}
	return l, true
}

// Current reduces all received rows to each board's latest result: a re-entry is a new row, the
// one with the highest ID wins, and an erased latest row means the board has no result. Rows that
// cannot be read are returned as errors, not dropped silently.
func Current(rows []Received) ([]Board, []error) {
	type key struct{ s, t, r, b int }
	latest := map[key]Received{}
	for _, r := range rows {
		k := key{r.Section, r.Table, r.Round, r.Board}
		if old, ok := latest[k]; !ok || r.ID > old.ID {
			latest[k] = r
		}
	}
	var out []Board
	var errs []error
	for _, r := range latest {
		if r.Erased {
			continue
		}
		b, err := Parse(r)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool {
		a, c := out[i], out[j]
		if a.Section != c.Section {
			return a.Section < c.Section
		}
		if a.Received.Board != c.Received.Board {
			return a.Received.Board < c.Received.Board
		}
		return a.Table < c.Table
	})
	return out, errs
}

// Parse reads one row.
func Parse(r Received) (Board, error) {
	b := Board{Received: r}
	where := fmt.Sprintf("table %d round %d board %d", r.Table, r.Round, r.Board)
	remarks := strings.TrimSpace(r.Remarks)
	b.WrongDirection = strings.EqualFold(remarks, "Wrong direction")
	contract := strings.TrimSpace(r.Contract)

	if contract == "" {
		switch {
		case strings.EqualFold(remarks, "Not played"):
			b.Status = NotPlayed
		case strings.EqualFold(remarks, "Arbitral score"):
			b.Status = TDPending
		default:
			ns, ew, ok := percentages(remarks)
			if !ok {
				return b, fmt.Errorf("%s: no contract and remarks %q", where, remarks)
			}
			b.Status, b.AdjNS, b.AdjEW = Adjusted, ns, ew
		}
		return b, nil
	}

	c, err := parseContract(contract)
	if err != nil {
		return b, fmt.Errorf("%s: %w", where, err)
	}
	b.Contract = c
	if c.Level == 0 { // passed out
		return b, nil
	}
	switch d := strings.ToUpper(strings.TrimSpace(r.Direction)); d {
	case "N", "S", "NS":
		b.DeclarerNS = true
	case "E", "W", "EW":
	default:
		switch r.Declarer {
		case r.PairNS:
			b.DeclarerNS = true
		case r.PairEW:
		default:
			return b, fmt.Errorf("%s: no declarer (%q, pair %d)", where, r.Direction, r.Declarer)
		}
	}
	res := strings.TrimSpace(r.Result)
	tricks := c.Level + 6
	if res != "=" {
		n, err := strconv.Atoi(res)
		if err != nil || n == 0 {
			return b, fmt.Errorf("%s: result %q", where, r.Result)
		}
		tricks += n
	}
	if tricks < 0 || tricks > 13 {
		return b, fmt.Errorf("%s: %s %s is not possible", where, contract, res)
	}
	b.Tricks = tricks
	b.ScoreNS = score.NSScore(r.Board, c, b.DeclarerNS, tricks)
	return b, nil
}

// parseContract reads BCS's "4 H x", "3 NT xx", "PASS".
func parseContract(s string) (score.Contract, error) {
	f := strings.Fields(strings.ToUpper(s))
	if len(f) == 1 && f[0] == "PASS" {
		return score.PassOut, nil
	}
	if len(f) < 2 || len(f) > 3 {
		return score.Contract{}, fmt.Errorf("contract %q", s)
	}
	return score.ParseContract(strings.Join(f, ""))
}

// percentages reads an adjusted score "60%-40%" (NS-EW).
func percentages(s string) (float64, float64, bool) {
	a, b, ok := strings.Cut(strings.ReplaceAll(s, " ", ""), "-")
	if !ok || !strings.HasSuffix(a, "%") || !strings.HasSuffix(b, "%") {
		return 0, 0, false
	}
	ns, err1 := strconv.ParseFloat(strings.TrimSuffix(a, "%"), 64)
	ew, err2 := strconv.ParseFloat(strings.TrimSuffix(b, "%"), 64)
	return ns, ew, err1 == nil && err2 == nil
}
