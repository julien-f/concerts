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
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

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
}

// New returns the app's HTTP handler; it fails if a template does not parse.
// baseURL is the public URL (e.g. https://concerts.example.com); now is the clock.
func New(st *store.Store, log *slog.Logger, baseURL string, now func() time.Time) (http.Handler, error) {
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("base URL %q: want http(s)://host", baseURL)
	}
	static, err := loadStatic()
	if err != nil {
		return nil, err
	}
	s := &server{store: st, log: log, static: static,
		secure: u.Scheme == "https", baseURL: strings.TrimSuffix(baseURL, "/"), now: now}
	s.pages, err = parsePages(template.FuncMap{
		"static":   static.url,
		"stub":     stubOfDate,
		"when":     s.when,
		"frdate":   s.frDate,
		"datetime": datetime,
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
	mux.HandleFunc("GET /g/{secret}/concerts/{id}", s.inGroup(s.showConcert))
	mux.HandleFunc("GET /g/{secret}/concerts/{id}/edit", s.inGroup(requireMe(s.editConcert)))
	mux.HandleFunc("POST /g/{secret}/concerts/{id}", s.inGroup(requireMe(s.updateConcert)))
	mux.HandleFunc("POST /g/{secret}/concerts/{id}/delete", s.inGroup(requireMe(s.deleteConcert)))
	mux.HandleFunc("POST /g/{secret}/concerts/{id}/restore", s.inGroup(requireMe(s.restoreConcert)))
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
		notFound(w)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(body))
}

// parsePages builds one template per page, each combined with the layout.
func parsePages(funcs template.FuncMap) (map[string]*template.Template, error) {
	names, err := fs.Glob(templatesFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	pages := map[string]*template.Template{}
	for _, n := range names {
		if n == "templates/layout.html" {
			continue
		}
		t, err := template.New("").Funcs(funcs).ParseFS(templatesFS, "templates/layout.html", n)
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

func notFound(w http.ResponseWriter) {
	http.Error(w, "Page introuvable.", http.StatusNotFound)
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
