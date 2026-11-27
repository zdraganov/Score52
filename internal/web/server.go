// Package web is Score52's interface: pages served on 127.0.0.1 and opened in the browser, in
// Bulgarian. Server-rendered HTML with plain forms, no JavaScript framework and nothing from the
// internet, so it works offline at the venue.
package web

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zdraganov/Score52/internal/store"
)

//go:embed templates/*.html static/*
var assets embed.FS

// Ext is the extension of an event file.
const Ext = ".score52"

type Server struct {
	dir   string
	token string // every form carries it: another site open in the browser can't post here

	mu     sync.Mutex
	stores map[string]*store.Store

	pages map[string]*template.Template
}

// New serves the events in dir (created if missing).
func New(dir string) (*Server, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	s := &Server{dir: dir, token: hex.EncodeToString(b), stores: map[string]*store.Store{}, pages: map[string]*template.Template{}}
	names, err := fs.Glob(assets, "templates/*.html")
	if err != nil {
		return nil, err
	}
	for _, n := range names {
		if strings.HasSuffix(n, "layout.html") {
			continue
		}
		t, err := template.New("").Funcs(funcs).ParseFS(assets, "templates/layout.html", n)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", n, err)
		}
		s.pages[strings.TrimSuffix(filepath.Base(n), ".html")] = t
	}
	return s, nil
}

// Close closes every open event file.
func (s *Server) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, st := range s.stores {
		st.Close()
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(assets, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("POST /events", s.createEvent)
	mux.HandleFunc("GET /e/{id}", s.event)
	mux.HandleFunc("POST /e/{id}/pairs", s.addPair)
	mux.HandleFunc("POST /e/{id}/pairs/{pid}/delete", s.deletePair)
	mux.HandleFunc("GET /e/{id}/new-session", s.newSession)
	mux.HandleFunc("POST /e/{id}/sessions", s.createSession)
	mux.HandleFunc("GET /e/{id}/s/{slot}", s.session)
	mux.HandleFunc("GET /e/{id}/s/{slot}/b/{board}", s.traveller)
	mux.HandleFunc("POST /e/{id}/s/{slot}/b/{board}", s.saveTraveller)
	mux.HandleFunc("GET /e/{id}/s/{slot}/results", s.results)
	mux.HandleFunc("GET /e/{id}/s/{slot}/usebio.xml", s.usebio)
	mux.HandleFunc("GET /e/{id}/standings", s.standings)
	mux.HandleFunc("POST /e/{id}/finals", s.planFinals)
	mux.HandleFunc("GET /e/{id}/stage/{stage}/usebio.xml", s.stageUsebio)
	return s.guard(mux)
}

// guard refuses a form post without this run's token, and keeps the browser from caching pages.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.PostFormValue("t") != s.token {
			http.Error(w, "Формулярът е от стара страница или от друг сайт. Презаредете страницата.", http.StatusForbidden)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// page is what every template gets.
type page struct {
	Title   string
	Token   string
	EventID string
	Event   string // its title
	Error   string
	Data    any
}

func (s *Server) render(w http.ResponseWriter, name string, p page) {
	p.Token = s.token
	t, ok := s.pages[name]
	if !ok {
		http.Error(w, "no page "+name, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "layout", p); err != nil {
		fmt.Fprintf(w, "<p>%s</p>", template.HTMLEscapeString(err.Error()))
	}
}

func (s *Server) fail(w http.ResponseWriter, status int, err error) {
	w.WriteHeader(status)
	s.render(w, "error", page{Title: "Грешка", Error: err.Error()})
}

var idRe = regexp.MustCompile(`^[0-9a-z-]{1,64}$`)

// open gives the event's store, opening its file once.
func (s *Server) open(id string) (*store.Store, error) {
	if !idRe.MatchString(id) {
		return nil, errors.New("няма такова състезание")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if st, ok := s.stores[id]; ok {
		return st, nil
	}
	path := filepath.Join(s.dir, id+Ext)
	if _, err := os.Stat(path); err != nil {
		return nil, errors.New("няма такова състезание")
	}
	st, err := store.Open(path)
	if err != nil {
		return nil, err
	}
	s.stores[id] = st
	return st, nil
}

// --- events ---

type eventInfo struct {
	ID, Title, Date string
	Entries         int
}

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	files, _ := filepath.Glob(filepath.Join(s.dir, "*"+Ext))
	var events []eventInfo
	for _, f := range files {
		id := strings.TrimSuffix(filepath.Base(f), Ext)
		st, err := s.open(id)
		if err != nil {
			continue
		}
		title, _ := st.Meta(store.MetaTitle)
		date, _ := st.Meta(store.MetaDate)
		entries, _ := st.Entries()
		events = append(events, eventInfo{ID: id, Title: title, Date: date, Entries: len(entries)})
	}
	sort.Slice(events, func(i, j int) bool { return events[i].ID > events[j].ID })
	s.render(w, "home", page{Title: "Състезания", Data: map[string]any{
		"Events": events, "Today": time.Now().Format("2006-01-02"), "Dir": s.dir}})
}

func (s *Server) createEvent(w http.ResponseWriter, r *http.Request) {
	title := strings.TrimSpace(r.PostFormValue("title"))
	date := r.PostFormValue("date")
	if title == "" {
		s.fail(w, http.StatusBadRequest, errors.New("състезанието няма име"))
		return
	}
	if _, err := time.Parse("2006-01-02", date); err != nil {
		s.fail(w, http.StatusBadRequest, errors.New("невалидна дата"))
		return
	}
	// the file is named by date and a number: "2026-11-27-1"
	var id string
	for n := 1; ; n++ {
		id = fmt.Sprintf("%s-%d", date, n)
		if _, err := os.Stat(filepath.Join(s.dir, id+Ext)); errors.Is(err, os.ErrNotExist) {
			break
		}
	}
	st, err := store.Open(filepath.Join(s.dir, id+Ext))
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	for k, v := range map[string]string{store.MetaTitle: title, store.MetaDate: date, store.MetaKind: "pairs"} {
		if err := st.SetMeta(k, v); err != nil {
			st.Close()
			s.fail(w, http.StatusInternalServerError, err)
			return
		}
	}
	s.mu.Lock()
	s.stores[id] = st
	s.mu.Unlock()
	http.Redirect(w, r, "/e/"+id, http.StatusSeeOther)
}

// eventPage loads the event's store and title for a page under /e/{id}.
func (s *Server) eventPage(w http.ResponseWriter, r *http.Request) (*store.Store, page, bool) {
	id := r.PathValue("id")
	st, err := s.open(id)
	if err != nil {
		s.fail(w, http.StatusNotFound, err)
		return nil, page{}, false
	}
	title, _ := st.Meta(store.MetaTitle)
	return st, page{EventID: id, Event: title}, true
}

func (s *Server) event(w http.ResponseWriter, r *http.Request) {
	st, p, ok := s.eventPage(w, r)
	if !ok {
		return
	}
	entries, err := st.Entries()
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	sessions, err := st.Sessions()
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	date, _ := st.Meta(store.MetaDate)
	p.Title = p.Event
	p.Data = map[string]any{"Entries": entries, "Sessions": sessions, "Date": date, "Seated": seated(sessions)}
	s.render(w, "event", p)
}

// seated are the entries some session uses: they can't be deleted.
func seated(sessions []store.Session) map[int64]bool {
	out := map[int64]bool{}
	for _, x := range sessions {
		for _, e := range x.Seating {
			out[e] = true
		}
	}
	return out
}

func (s *Server) addPair(w http.ResponseWriter, r *http.Request) {
	st, p, ok := s.eventPage(w, r)
	if !ok {
		return
	}
	var ids []int64
	for _, k := range []string{"1", "2"} {
		name := strings.TrimSpace(r.PostFormValue("name" + k))
		if name == "" {
			s.fail(w, http.StatusBadRequest, errors.New("двойката трябва да има двама играчи"))
			return
		}
		id, err := st.AddPlayer(store.Player{Name: name, FederationID: strings.TrimSpace(r.PostFormValue("ibn" + k))})
		if err != nil {
			s.fail(w, http.StatusInternalServerError, err)
			return
		}
		ids = append(ids, id)
	}
	entries, _ := st.Entries()
	if _, err := st.AddEntry("", len(entries)+1, ids); err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	http.Redirect(w, r, "/e/"+p.EventID+"#pairs", http.StatusSeeOther)
}

func (s *Server) deletePair(w http.ResponseWriter, r *http.Request) {
	st, p, ok := s.eventPage(w, r)
	if !ok {
		return
	}
	pid, err := strconv.ParseInt(r.PathValue("pid"), 10, 64)
	if err == nil {
		err = st.DeleteEntry(pid)
	}
	if err != nil {
		s.fail(w, http.StatusBadRequest, fmt.Errorf("двойката не може да се изтрие: %w", err))
		return
	}
	http.Redirect(w, r, "/e/"+p.EventID+"#pairs", http.StatusSeeOther)
}

// names is "Иван Иванов – Петър Петров".
func names(e store.Entry) string {
	var n []string
	for _, p := range e.Players {
		n = append(n, p.Name)
	}
	if len(n) == 0 {
		return e.Name
	}
	return strings.Join(n, " – ")
}

// suits renders a contract with suit symbols, hearts and diamonds in red: "3NTX" -> 3NT X, "4H" -> 4♥.
func suits(c string) template.HTML {
	sym := map[byte]string{'S': "♠", 'H': `<span class="red">♥</span>`, 'D': `<span class="red">♦</span>`, 'C': "♣"}
	if len(c) < 2 || c[0] < '1' || c[0] > '7' {
		return template.HTML(template.HTMLEscapeString(c))
	}
	rest := c[1:]
	var out string
	if strings.HasPrefix(rest, "NT") {
		out, rest = "NT", rest[2:]
	} else if s, ok := sym[rest[0]]; ok {
		out, rest = s, rest[1:]
	}
	return template.HTML(c[:1] + out + template.HTMLEscapeString(rest))
}

var funcs = template.FuncMap{
	"deref":  func(v *float64) float64 { return *v },
	"pct100": func(v float64) string { return strconv.FormatFloat(100*v, 'f', -1, 64) },
	"dict": func(kv ...any) map[string]any {
		m := map[string]any{}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i].(string)] = kv[i+1]
		}
		return m
	},
	"names":  names,
	"suits":  suits,
	"fields": strings.Fields,
	"inc":    func(i int) int { return i + 1 },
	"pct":    func(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) },
	"num": func(v float64) string {
		return strings.TrimSuffix(strings.TrimRight(strconv.FormatFloat(v, 'f', 2, 64), "0"), ".")
	},
	"signed": func(v int) string {
		if v > 0 {
			return "+" + strconv.Itoa(v)
		}
		return strconv.Itoa(v)
	},
}
