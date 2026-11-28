package bridgemate

import (
	"fmt"
	"time"
)

// A teams round is one .bws, as Magic Contest makes it: section A is the open room, B the closed room,
// one table per match in each, and both tables of match k are table k with Tables.Group k. The next
// round is a new file (Bridgemate can't regroup tables within one), and BCS is restarted on it.
//
// Pair numbers are the teams' numbers: at A the home team sits NS and the away team EW, at B the other
// way round, so each section's numbers are unique and the Bridgemates show the teams' own numbers.
//
// Which boards a match plays is the table's business: every table gets the range 1..highest and the
// players key in their first board (BM2FirstBoardManually), as at the BBF's events. The round is the
// real one, the rounds before it left empty (as Magic Contest does), so the Bridgemates show it.

const (
	SectionOpen   = 1
	SectionClosed = 2
)

// TeamMatch is one match of the round.
type TeamMatch struct {
	Table      int // 1, 2...: the match's tables in both rooms
	Home, Away int // team numbers
	// players to show: N, S, E, W of the open room (home NS) and of the closed room (away NS)
	Open, Closed [4]Player
}

// TeamsSetup lays out round `round` of a teams event, its boards numbered 1..highest.
func TeamsSetup(name string, date time.Time, round, highest int, matches []TeamMatch) (Setup, error) {
	if len(matches) == 0 {
		return Setup{}, fmt.Errorf("no matches")
	}
	if round < 1 || highest < 1 {
		return Setup{}, fmt.Errorf("round %d, boards 1..%d", round, highest)
	}
	s := Setup{Session: SessionRow{ID: 1, Name: clip(name, 40), Date: date}, Settings: TeamsSettings}
	for _, sec := range []struct {
		id     int
		letter string
	}{{SectionOpen, "A"}, {SectionClosed, "B"}} {
		s.Sections = append(s.Sections, SectionRow{ID: sec.id, Letter: sec.letter, Tables: len(matches), Session: 1, Scoring: ScoringTeams, Winners: 1})
	}
	seen := map[int]bool{}
	for _, m := range matches {
		if m.Table < 1 || seen[m.Table] || m.Home == m.Away || m.Home < 1 || m.Away < 1 {
			return Setup{}, fmt.Errorf("match at table %d: %d - %d", m.Table, m.Home, m.Away)
		}
		seen[m.Table] = true
		for _, room := range []struct {
			sec     int
			ns, ew  int
			players [4]Player
		}{{SectionOpen, m.Home, m.Away, m.Open}, {SectionClosed, m.Away, m.Home, m.Closed}} {
			s.Tables = append(s.Tables, TableRow{Section: room.sec, Table: m.Table, Group: m.Table})
			for r := 1; r < round; r++ {
				s.Rounds = append(s.Rounds, RoundRow{Section: room.sec, Table: m.Table, Round: r})
			}
			s.Rounds = append(s.Rounds, RoundRow{Section: room.sec, Table: m.Table, Round: round, NS: room.ns, EW: room.ew,
				LowBoard: 1, HighBoard: highest})
			for i, dir := range []string{"N", "S", "E", "W"} {
				if p := room.players[i]; p.Name != "" || p.Number != "" {
					s.Players = append(s.Players, PlayerRow{Section: room.sec, Table: m.Table, Direction: dir, Number: p.Number, Name: clip(p.Name, 18)})
				}
			}
		}
	}
	return s, s.Check()
}

// TeamsSettings are the Bridgemate settings of a BBF teams event, as Magic Contest wrote them at one
// (2026-11-28): the first board keyed in at the table, no member numbers, names shown, lead card,
// score recap. One row per section; a column the file doesn't have is left out.
var TeamsSettings = []Setting{
	{"ShowResults", 0}, {"ShowOwnResult", 0}, {"RepeatResults", 0}, {"MaximumResults", 0}, {"ShowPercentage", 0},
	{"GroupSections", 0}, {"ScorePoints", 0}, {"EnterResultsMethod", 0}, {"ShowPairNumbers", 0}, {"IntermediateResults", 0},
	{"AutopoweroffTime", 20}, {"VerificationTime", 2}, {"ShowContract", 1}, {"LeadCard", 1}, {"MemberNumbers", 0},
	{"MemberNumbersNoBlankEntry", 1}, {"BoardOrderVerification", 0}, {"HandRecordValidation", 0}, {"AutoShutDownBPC", 1},
	{"BM2PINcode", "0000"}, {"BM2ConfirmNP", 1}, {"BM2TDCall", 1}, {"BM2RemainingBoards", 1}, {"BM2NextSeatings", 0},
	{"BM2ScoreRecap", 1}, {"BM2AutoShowScoreRecap", 1}, {"BM2ScoreCorrection", 0}, {"BM2AutoBoardNumber", 1},
	{"BM2FirstBoardManually", 1}, {"BM2ValidateLeadCard", 0}, {"BM2ResultsOverview", 1}, {"BM2ShowPlayerNames", 1},
	{"BM2Ranking", 0}, {"BM2GameSummary", 0}, {"BM2SummaryPoints", 0}, {"BM2PairNumberEntry", 0}, {"BM2ResetFunctionKey", 1},
	{"BM2RecordBidding", 0}, {"BM2RecordPlay", 0}, {"BM2ValidateRecording", 0}, {"BM2ShowHands", 0},
	{"BM2NumberEntryEachRound", 1}, {"BM2NumberEntryPreloadValues", 1}, {"BM2NameSource", 2}, {"BM2ViewHandRecord", 0},
	{"BM2EnterHandRecord", 0}, {"BM2EnterHandRecordWhen", 0}, {"BM2TextBasedNumber", 0},
}
