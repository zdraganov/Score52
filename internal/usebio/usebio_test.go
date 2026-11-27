package usebio

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bridge.bg/scorer/internal/score"
)

func pairsEvent() PairsEvent {
	c, _ := score.ParseContract("4S")
	x, _ := score.ParseContract("3NTX")
	return PairsEvent{
		Title: "Турнир по двойки", Date: time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC), Version: "test", Method: MatchPoints,
		Pairs: []Pair{
			{Number: 1, Players: []Player{{"Иван Иванов", "1001"}, {"Петър Петров", "1002"}}, Place: 1, Score: 3, Percent: 75, Boards: 1},
			{Number: 2, Players: []Player{{"Мария Георгиева", "1003"}, {"Анна Димитрова", "1004"}}, Place: 2, Score: 1, Percent: 25, Boards: 1},
		},
		Boards: []Board{{Number: 1, Lines: []Line{
			{Round: 1, NS: 1, EW: 2, Played: true, Contract: c, Declarer: "N", Lead: "HK", Tricks: 10, Score: 420, NSPoints: 75, EWPoints: 25},
			{Round: 1, NS: 3, EW: 4, Played: true, Contract: x, Declarer: "S", Tricks: 8, Score: -200, NSPoints: 25, EWPoints: 75},
		}}},
	}
}

func teamsEvent() TeamsEvent {
	return TeamsEvent{
		Title: "Отборен", Version: "test",
		Teams: []Team{
			{ID: 1, Name: "Левски", Players: []Player{{"Иван Иванов", "1001"}}, Place: 1, VP: 15.5},
			{ID: 2, Name: "Слънчев бряг", Place: 2, VP: 4.5},
		},
		Matches: []Match{{Played: true, Round: 1, Home: 1, Away: 2, FirstBoard: 1, LastBoard: 12, IMPsHome: 30, IMPsAway: 12, VPHome: 15.5, VPAway: 4.5}},
	}
}

func TestWritePairs(t *testing.T) {
	var b bytes.Buffer
	if err := WritePairs(&b, pairsEvent()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`<USEBIO Version="1.2">`, `EVENT_TYPE="MP_PAIRS"`, "<CONTRACT>3NTX</CONTRACT>", "<NATIONAL_ID_NUMBER>1001</NATIONAL_ID_NUMBER>", "<DATE>27/11/2026</DATE>"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("missing %s in\n%s", want, b.String())
		}
	}
}

// parseWithSite runs bridge.bg's own UsebioParser on a file, when PHP and the site's repository are here.
func parseWithSite(t *testing.T, xml []byte) map[string]any {
	t.Helper()
	site := os.Getenv("BRIDGEBG_DIR") // e.g. a worktree of the site; default: the repository next to this one
	if site == "" {
		site = "../../../bridge.bg"
	}
	parser, _ := filepath.Abs(filepath.Join(site, "next/src/Results/UsebioParser.php"))
	if _, err := os.Stat(parser); err != nil {
		t.Skip("bridge.bg repository not next to this one")
	}
	if _, err := exec.LookPath("php"); err != nil {
		t.Skip("no php")
	}
	f := filepath.Join(t.TempDir(), "1.xml")
	if err := os.WriteFile(f, xml, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("php", "-r", `require dirname($argv[1])."/PbnParser.php"; require $argv[1]; echo json_encode((new App\Results\UsebioParser())->parse($argv[2]));`, parser, f).CombinedOutput()
	if err != nil {
		t.Fatalf("php: %v\n%s", err, out)
	}
	var v map[string]any
	if err := json.Unmarshal(out, &v); err != nil || v == nil {
		t.Fatalf("site parser refused the file: %s", out)
	}
	return v
}

func TestSiteReadsPairs(t *testing.T) {
	var b bytes.Buffer
	WritePairs(&b, pairsEvent())
	v := parseWithSite(t, b.Bytes())
	if v["scoring"] != "pairs" || v["imps"] != false || v["event"] != "Турнир по двойки" {
		t.Errorf("event: %v", v)
	}
	r := v["ranking"].([]any)[0].(map[string]any)
	if r["Rank"] != "1" || r["PairId"] != "1" || r["Names"] != "Иван Иванов - Петър Петров" || r["TotalPercentage"] != "75.00" || r["MemberID2"] != "1002" {
		t.Errorf("ranking: %v", r)
	}
	s := v["boards"].([]any)[0].(map[string]any)["scores"].([]any)[1].(map[string]any)
	if s["Contract"] != "3NTX" || s["Score_NS"] != "-200" || s["Result"] != "8" || s["Percentage_EW"] != "75.00" {
		t.Errorf("board: %v", s)
	}
}

func TestSiteReadsIMPPairs(t *testing.T) {
	e := pairsEvent()
	e.Method = Butler
	var b bytes.Buffer
	WritePairs(&b, e)
	v := parseWithSite(t, b.Bytes())
	if v["imps"] != true {
		t.Errorf("not read as IMPs: %v", v)
	}
	r := v["ranking"].([]any)[0].(map[string]any)
	if r["TotalScoreIMP"] != "3.00" {
		t.Errorf("ranking: %v", r)
	}
	s := v["boards"].([]any)[0].(map[string]any)["scores"].([]any)[0].(map[string]any)
	if s["IMP_NS"] != "75.00" {
		t.Errorf("board: %v", s)
	}
}

func TestSiteReadsTeams(t *testing.T) {
	var b bytes.Buffer
	if err := WriteTeams(&b, teamsEvent()); err != nil {
		t.Fatal(err)
	}
	v := parseWithSite(t, b.Bytes())
	if v["scoring"] != "teams" {
		t.Fatalf("%v", v)
	}
	r := v["ranking"].([]any)[1].(map[string]any)
	if r["TeamName"] != "Слънчев бряг" || r["TotalScoreVP"] != "4.50" || r["Rank"] != "2" {
		t.Errorf("ranking: %v", r)
	}
	m := v["rounds"].([]any)[0].(map[string]any)
	// a site before bridge.bg#240 drops the match's imps ("1-12,,")
	if d := m["MatchDetails"]; d != "1-12,30,12" && d != "1-12,," {
		t.Errorf("match details: %v", d)
	}
	if m["TeamId_Home"] != "1" || m["TeamId_Away"] != "2" || m["VP_Home"] != "15.50" {
		t.Errorf("match: %v", m)
	}
}

// A stage in groups: bridge.bg ranks A before B with the places running on, and shows real VPs.
func TestSiteReadsTeamGroups(t *testing.T) {
	e := TeamsEvent{Title: "Етап 2", Teams: []Team{
		{ID: 3, Name: "B1", Group: "B", Place: 1, VP: 40},
		{ID: 1, Name: "A1", Group: "A", Place: 1, VP: 30},
		{ID: 2, Name: "A2", Group: "A", Place: 2, VP: 20},
		{ID: 4, Name: "B2", Group: "B", Place: 2, VP: 10},
	}, Matches: []Match{
		{Played: true, Round: 9, Home: 1, Away: 2, IMPsHome: 20, IMPsAway: 5, VPHome: 15, VPAway: 5},
		{Round: 10, Home: 3, Away: 4},
	}}
	var b bytes.Buffer
	if err := WriteTeams(&b, e); err != nil {
		t.Fatal(err)
	}
	v := parseWithSite(t, b.Bytes())
	groups, ok := v["groups"].(map[string]any)
	if !ok || len(groups) != 2 {
		t.Skipf("this bridge.bg has no USEBIO groups yet (set BRIDGEBG_DIR to a newer one): %v", v["groups"])
	}
	var got []string
	for _, r := range v["ranking"].([]any) {
		row := r.(map[string]any)
		got = append(got, row["TeamName"].(string)+"#"+row["Rank"].(string)+"@"+row["ScoreCarryOver"].(string))
	}
	if strings.Join(got, " ") != "A1#1@1000 A2#2@1000 B1#3@0 B2#4@0" {
		t.Errorf("ranking: %v", got)
	}
	rounds := v["rounds"].([]any)
	if d := rounds[1].(map[string]any)["MatchDetails"]; d != ",," {
		t.Errorf("unplayed match has details %q", d)
	}
}
