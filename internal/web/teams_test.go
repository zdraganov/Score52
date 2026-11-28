package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// A Swiss teams event from the home page to the file for bridge.bg: 5 teams (a bye), boards of both
// rooms, a correction, standings.
func TestTeamsSwiss(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	c := &client{t: t, base: ts.URL, http: &http.Client{}, token: s.token}

	_, ev := c.post("/events", url.Values{"title": {"Отборно"}, "date": {"2026-12-05"}, "kind": {"teams"}})
	for i := 1; i <= 5; i++ {
		v := url.Values{"name": {fmt.Sprint("Отбор ", i)}}
		for k := 1; k <= 4; k++ {
			v.Set(fmt.Sprint("p", k), fmt.Sprintf("Играч %d-%d", i, k))
		}
		c.post(ev+"/teams", v)
	}
	home, _ := c.get("/")
	if !strings.Contains(home, "5 отбора") {
		t.Error("the home page doesn't count teams")
	}
	page, _ := c.post(ev+"/teams/start", url.Values{"format": {"swiss"}, "boards": {"2"}, "bye_vp": {"12"}})
	if !strings.Contains(page, "Чифтосай рунд 1") {
		t.Fatal("no pairing after the start")
	}
	page, _ = c.post(ev+"/teams/pair", url.Values{"boards": {"2"}, "first": {"1"}})
	links := regexp.MustCompile(`href="(/e/[\w-]+/r/1/m/\d+)"`).FindAllStringSubmatch(page, -1)
	if len(links) != 4 || !strings.Contains(page, "почива") { // two matches, two links each, one bye
		t.Fatalf("round 1: %d match links", len(links))
	}
	// match 1: open 4S= by N (+420 on board 1, none vul), closed 4S-1 by S (-50): home +470 = 10 IMPs;
	// board 2 adjusted 3:0
	m1 := links[0][1]
	page, _ = c.post(m1, url.Values{"oc0": {"4s"}, "od0": {"N"}, "or0": {"="}, "cc0": {"4S"}, "cd0": {"S"}, "cr0": {"-1"},
		"ah1": {"3"}, "aa1": {"0"}})
	if !strings.Contains(page, "13 : 0 IMP") || !strings.Contains(page, "VP") {
		t.Fatalf("match 1 total:\n%s", page[strings.Index(page, "<h1>"):strings.Index(page, "<form")])
	}
	// a correction: board 1 closed made too, 0 IMPs; the match is recorded again
	page, _ = c.post(m1, url.Values{"oc0": {"4S"}, "od0": {"N"}, "or0": {"="}, "cc0": {"4S"}, "cd0": {"S"}, "cr0": {"="},
		"ah1": {"3"}, "aa1": {"0"}})
	if !strings.Contains(page, "3 : 0 IMP") {
		t.Fatal("the correction didn't change the total")
	}
	// a bad row is shown, not saved
	page, _ = c.post(links[2][1], url.Values{"oc0": {"4S"}, "od0": {"X"}, "or0": {"="}})
	if !strings.Contains(page, "разиграващият") {
		t.Error("a bad declarer passed")
	}
	evPage, _ := c.get(ev)
	if strings.Contains(evPage, "Чифтосай рунд 2") {
		t.Error("round 2 can be paired before round 1 is in")
	}
	c.post(links[2][1], url.Values{"oc0": {"3NT"}, "od0": {"S"}, "or0": {"+1"}, "cc0": {"PASS"}, "oc1": {"PASS"}, "cc1": {"PASS"}})
	evPage, _ = c.get(ev)
	if !strings.Contains(evPage, "Чифтосай рунд 2") {
		t.Fatal("round 1 is in but round 2 can't be paired")
	}
	st, _ := c.get(ev + "/standings")
	if !strings.Contains(st, "Отбор") || !strings.Contains(st, "<b>12</b>") { // the bye's 12 VP
		t.Errorf("standings:\n%s", st[strings.Index(st, "<tbody>"):])
	}
	xml, _ := c.get(ev + "/teams/usebio.xml")
	if !strings.Contains(xml, "SWISS_TEAMS") || strings.Count(xml, "<MATCH>") != 2 {
		t.Errorf("usebio:\n%s", xml)
	}
}
