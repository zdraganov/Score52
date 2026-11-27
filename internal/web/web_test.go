package web

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

type client struct {
	t     *testing.T
	base  string
	http  *http.Client
	token string
}

func (c *client) get(path string) (string, string) {
	c.t.Helper()
	r, err := c.http.Get(c.base + path)
	if err != nil {
		c.t.Fatal(err)
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	if r.StatusCode != 200 {
		c.t.Fatalf("GET %s: %d\n%s", path, r.StatusCode, b)
	}
	return string(b), r.Request.URL.Path
}

func (c *client) post(path string, v url.Values) (string, string) {
	c.t.Helper()
	v.Set("t", c.token)
	r, err := c.http.PostForm(c.base+path, v)
	if err != nil {
		c.t.Fatal(err)
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	if r.StatusCode != 200 {
		c.t.Fatalf("POST %s: %d\n%s", path, r.StatusCode, b)
	}
	return string(b), r.Request.URL.Path
}

func TestPairsFlow(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	jar, _ := cookiejar.New(nil)
	c := &client{t: t, base: ts.URL, http: &http.Client{Jar: jar}, token: s.token}

	home, _ := c.get("/")
	if !strings.Contains(home, "Ново състезание") {
		t.Fatal("home page")
	}
	// a post without the token is refused
	if r, _ := http.PostForm(ts.URL+"/events", url.Values{"title": {"x"}, "date": {"2026-11-27"}}); r.StatusCode != 403 {
		t.Errorf("post without token: %d", r.StatusCode)
	}

	_, ev := c.post("/events", url.Values{"title": {"Купа Пика"}, "date": {"2026-11-27"}})
	if ev != "/e/2026-11-27-1" {
		t.Fatalf("event at %s", ev)
	}
	for i := 1; i <= 9; i++ { // 9 pairs: a 5-table Mitchell with a phantom
		c.post(ev+"/pairs", url.Values{"name1": {"Играч " + string(rune('А'+i))}, "ibn1": {"100" + string(rune('0'+i))}, "name2": {"Партньор " + string(rune('А'+i))}})
	}
	page, _ := c.get(ev)
	if !strings.Contains(page, ">9</span>") {
		t.Error("9 pairs not listed")
	}

	opts, _ := c.get(ev + "/new-session?min=20&max=30")
	if !strings.Contains(opts, "препоръчан") || !strings.Contains(opts, "Мичъл") {
		t.Fatalf("options:\n%s", opts)
	}
	_, sess := c.post(ev+"/sessions", url.Values{"kind": {"mitchell"}, "tables": {"5"}, "rounds": {"5"}, "bpr": {"5"}, "method": {"mp"}})
	if sess != ev+"/s/s1" {
		t.Fatalf("session at %s", sess)
	}
	plan, _ := c.get(sess)
	if !strings.Contains(plan, "почива") || !strings.Contains(plan, "Маса 5") {
		t.Error("plan has no sit-out or tables")
	}

	// board 1: played at the tables of set 1; enter all real ones
	trav, _ := c.get(sess + "/b/1")
	rows := len(regexp.MustCompile(`name="c\d+"`).FindAllString(trav, -1))
	if rows < 3 {
		t.Fatalf("board 1 has %d rows", rows)
	}
	v := url.Values{"go": {"stay"}}
	results := []struct{ c, d, r, a string }{{"4S", "N", "=", ""}, {"4s", "s", "+1", ""}, {"3NT", "E", "-1", ""}, {"", "", "", "60-40"}, {"PASS", "", "", ""}}
	for i := 0; i < rows; i++ {
		x := results[i%len(results)]
		k := string(rune('0' + i))
		v.Set("c"+k, x.c)
		v.Set("d"+k, x.d)
		v.Set("r"+k, x.r)
		v.Set("a"+k, x.a)
	}
	saved, _ := c.post(sess+"/b/1", v)
	if !strings.Contains(saved, "&#43;420") || !strings.Contains(saved, "&#43;450") || !strings.Contains(saved, "4♠") {
		t.Errorf("traveller after save:\n%s", saved)
	}
	// a bad row is reported and the others still saved
	bad, _ := c.post(sess+"/b/2", url.Values{"go": {"stay"}, "c0": {"8S"}, "d0": {"N"}, "r0": {"="}})
	if !strings.Contains(bad, "поправете") || !strings.Contains(bad, "4S, 3NTX") {
		t.Errorf("bad row not reported")
	}

	res, _ := c.get(sess + "/results")
	if !strings.Contains(res, "Север – Юг") || !strings.Contains(res, "Изток – Запад") || !strings.Contains(res, "Липсват") {
		t.Error("results page")
	}
	xml, _ := c.get(sess + "/usebio.xml")
	if !strings.Contains(xml, `<USEBIO Version="1.2">`) || !strings.Contains(xml, "<CONTRACT>4S</CONTRACT>") || !strings.Contains(xml, "<DIRECTION>EW</DIRECTION>") {
		t.Errorf("usebio:\n%s", xml)
	}
}
