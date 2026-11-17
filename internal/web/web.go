// Package web holds the HTTP handlers, templates and static files.
package web

import (
	"bytes"
	"cmp"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"concerts/internal/prefill"
	"concerts/internal/store"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static
var staticFS embed.FS

// NewSecret returns a fresh group link secret: 16 random bytes, base64url without padding.
func NewSecret() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // never fails, see crypto/rand.Read
	return base64.RawURLEncoding.EncodeToString(b)
}

// abs is the absolute URL of a path on this site, for links read outside it (previews, WhatsApp).
func (s *server) abs(path string) string {
	return s.baseURL + path
}

// GroupURL returns the shareable link of a group.
func GroupURL(baseURL, secret string) string {
	return strings.TrimSuffix(baseURL, "/") + "/g/" + secret
}

type server struct {
	store   *store.Store
	log     *slog.Logger
	pages   map[string]*template.Template
	static  staticAssets
	secure  bool   // cookies get the Secure attribute
	baseURL string // public URL without trailing slash, for absolute links
	now     func() time.Time
	fetch   *http.Client // ticketing pages, for prefill
	fetches *fetchLimiter
}

// Config is what the app takes from its environment.
type Config struct {
	BaseURL string // public URL, e.g. https://concerts.example.com
	Now     func() time.Time
	Fetch   *http.Client // for ticketing pages; prefill.NewClient() if nil
}

// ParseBaseURL accepts only http(s)://host, optionally with a trailing slash: absolute links
// append their path to it, and the app serves the whole host.
func ParseBaseURL(s string) (*url.URL, error) {
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || strings.Contains(s, "#") ||
		strings.HasSuffix(u.Host, ":") {
		return nil, fmt.Errorf("base URL %q: want http(s)://host", s)
	}
	return u, nil
}

// New returns the app's HTTP handler; it fails if a template does not parse.
func New(st *store.Store, log *slog.Logger, cfg Config) (http.Handler, error) {
	u, err := ParseBaseURL(cfg.BaseURL)
	if err != nil {
		return nil, err
	}
	if h := u.Hostname(); u.Scheme == "http" && !strings.EqualFold(h, "localhost") && !net.ParseIP(h).IsLoopback() {
		log.Warn("plain http base URL: cookies lack Secure and shared links send the secret in clear; use https",
			"host", u.Host)
	}
	if cfg.Fetch == nil {
		cfg.Fetch = prefill.NewClient()
	}
	static, err := loadStatic()
	if err != nil {
		return nil, err
	}
	s := &server{store: st, log: log, static: static,
		secure: u.Scheme == "https", baseURL: strings.TrimSuffix(cfg.BaseURL, "/"), now: cfg.Now,
		fetch: cfg.Fetch, fetches: newFetchLimiter()}
	s.pages, err = parsePages(template.FuncMap{
		"static":     static.url,
		"abs":        s.abs,
		"stub":       stubOf,
		"stubStyle":  stubStyle,
		"ticketDate": s.ticketDate,
		"when":       s.when,
		"frdate":     s.frDate,
		"frtime":     frTime,
		"datetime":   datetime,
	})
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /static/{name}", s.serveStatic)
	mux.HandleFunc("GET /g/{secret}", s.inGroup(s.home))
	mux.HandleFunc("POST /g/{secret}/me", s.inGroup(s.join))
	mux.HandleFunc("POST /g/{secret}/logout", s.inGroup(s.logout))
	mux.HandleFunc("GET /g/{secret}/concerts/new", s.inGroup(requireMe(s.newConcert)))
	mux.HandleFunc("POST /g/{secret}/concerts", s.inGroup(requireMe(s.createConcert)))
	mux.HandleFunc("GET /g/{secret}/concerts/{id}", s.inGroup(s.redirectToConcert))
	mux.HandleFunc("GET /g/{secret}/concerts/{id}/edit", s.inGroup(requireMe(s.editConcert)))
	mux.HandleFunc("POST /g/{secret}/concerts/{id}", s.inGroup(requireMe(s.updateConcert)))
	mux.HandleFunc("POST /g/{secret}/concerts/{id}/refresh", s.inGroup(requireMe(s.refreshConcert)))
	mux.HandleFunc("POST /g/{secret}/concerts/{id}/delete", s.inGroup(requireMe(s.deleteConcert)))
	mux.HandleFunc("POST /g/{secret}/concerts/{id}/restore", s.inGroup(requireMe(s.restoreConcert)))
	mux.HandleFunc("POST /g/{secret}/concerts/{id}/status", s.inGroup(requireMe(s.setStatus)))
	mux.HandleFunc("GET /g/{secret}/concerts/{id}/history", s.inGroup(s.showHistory))
	mux.HandleFunc("POST /g/{secret}/concerts/{id}/revisions/{rev}/revert", s.inGroup(requireMe(s.revertConcert)))
	mux.HandleFunc("GET /c/{token}", s.publicConcert)
	mux.HandleFunc("GET /c/{token}/agenda.ics", s.agenda)
	return s.logRequests(securityHeaders(http.NewCrossOriginProtection().Handler(mux))), nil
}

// staticAssets maps each static file to a content-hashed name, so it can be cached forever.
type staticAssets struct {
	hashed map[string]string // file name -> hashed name
	bodies map[string][]byte // hashed name -> content
}

func loadStatic() (staticAssets, error) {
	a := staticAssets{hashed: map[string]string{}, bodies: map[string][]byte{}}
	entries, err := fs.ReadDir(staticFS, "static")
	if err != nil {
		return a, err
	}
	for _, e := range entries {
		body, err := staticFS.ReadFile("static/" + e.Name())
		if err != nil {
			return a, err
		}
		sum := sha256.Sum256(body)
		ext := path.Ext(e.Name())
		h := strings.TrimSuffix(e.Name(), ext) + "." + hex.EncodeToString(sum[:8]) + ext
		a.hashed[e.Name()] = h
		a.bodies[h] = body
	}
	return a, nil
}

// url returns the cacheable URL of a static file; templates call it as {{static "style.css"}}.
func (a staticAssets) url(name string) (string, error) {
	h, ok := a.hashed[name]
	if !ok {
		return "", fmt.Errorf("unknown static file %q", name)
	}
	return "/static/" + h, nil
}

func (s *server) serveStatic(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	body, ok := s.static.bodies[name]
	if !ok {
		s.notFound(w, r, nil)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(body))
}

// parsePages builds one template per page, each combined with the layout and the shared partials.
func parsePages(funcs template.FuncMap) (map[string]*template.Template, error) {
	names, err := fs.Glob(templatesFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	pages := map[string]*template.Template{}
	for _, n := range names {
		if n == "templates/layout.html" || n == "templates/partials.html" {
			continue
		}
		t, err := template.New("").Funcs(funcs).ParseFS(templatesFS, "templates/layout.html", "templates/partials.html", n)
		if err != nil {
			return nil, err
		}
		pages[strings.TrimSuffix(strings.TrimPrefix(n, "templates/"), ".html")] = t
	}
	return pages, nil
}

// render writes a full page; it renders into a buffer first so a template error becomes a clean 500.
func (s *server) render(w http.ResponseWriter, r *http.Request, status int, page string, data any) {
	var buf bytes.Buffer
	if err := s.pages[page].ExecuteTemplate(&buf, "layout", data); err != nil {
		s.fail(w, r, fmt.Errorf("render %s: %w", page, err))
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

// fail logs err and answers a 500 that reveals nothing.
func (s *server) fail(w http.ResponseWriter, r *http.Request, err error) {
	s.log.ErrorContext(r.Context(), "internal error", "err", err)
	http.Error(w, "Erreur interne, réessaie plus tard.", http.StatusInternalServerError)
}

// notFoundText is the same for an unknown, rotated or truncated secret, so it reveals nothing;
// it sends whoever was cut off by a rotation to the operator, not to the members.
const notFoundText = "Page introuvable. Si ce lien vient de ton groupe, il a peut-être changé ou été coupé : " +
	"demande le bon lien à la personne qui gère le groupe."

// notFound answers 404: inside a group (gr != nil), a page with the group header and
// a way back; for an unknown secret or static file, notFoundText.
// htmx, which swaps no 4xx, is told to reload instead (e.g. a concert deleted meanwhile).
func (s *server) notFound(w http.ResponseWriter, r *http.Request, gr *groupRequest) {
	if isHTMX(r) {
		w.Header().Set("HX-Refresh", "true") // the reload then gets the page
	}
	if gr == nil || isHTMX(r) {
		http.Error(w, notFoundText, http.StatusNotFound)
		return
	}
	gr.Notice = "Erreur"
	s.render(w, r, http.StatusNotFound, "notfound", gr)
}

// contentSecurityPolicy allows only same-origin resources and no inline code;
// htmx complies through its <meta name="htmx-config"> in the layout.
const contentSecurityPolicy = "default-src 'self'; style-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'"

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", contentSecurityPolicy)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Robots-Tag", "noindex")
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// logRequests logs the matched route pattern, never the raw path, so no spelling
// of a URL (//g/…, /./g/…) can put a group secret in the logs; no IP either.
func (s *server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		// ServeMux sets r.Pattern on this same request; it stays empty when nothing matched.
		s.log.InfoContext(r.Context(), "request", "route", cmp.Or(r.Pattern, r.Method+" (unmatched)"), "status", rec.status)
	})
}

func (s *server) healthz(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		s.log.ErrorContext(r.Context(), "healthz", "err", err)
		http.Error(w, "db unavailable", http.StatusServiceUnavailable)
		return
	}
	_, _ = w.Write([]byte("ok"))
}
