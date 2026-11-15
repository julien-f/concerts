// Package web holds the HTTP handlers, templates and static files.
package web

import (
	"crypto/rand"
	"embed"
	"encoding/base64"
	"errors"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

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
	store *store.Store
	log   *slog.Logger
	pages map[string]*template.Template
}

// New returns the app's HTTP handler; it fails if a template does not parse.
func New(st *store.Store, log *slog.Logger) (http.Handler, error) {
	pages, err := parsePages()
	if err != nil {
		return nil, err
	}
	s := &server{store: st, log: log, pages: pages}
	static, err := fs.Sub(staticFS, "static")
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /g/{secret}", s.group)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
	return s.logRequests(securityHeaders(mux)), nil
}

// parsePages builds one template per page, each combined with the layout.
func parsePages() (map[string]*template.Template, error) {
	names, err := fs.Glob(templatesFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	pages := map[string]*template.Template{}
	for _, n := range names {
		if n == "templates/layout.html" {
			continue
		}
		t, err := template.ParseFS(templatesFS, "templates/layout.html", n)
		if err != nil {
			return nil, err
		}
		pages[strings.TrimSuffix(strings.TrimPrefix(n, "templates/"), ".html")] = t
	}
	return pages, nil
}

func (s *server) render(w http.ResponseWriter, r *http.Request, page string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.pages[page].ExecuteTemplate(w, "layout", data); err != nil {
		s.log.ErrorContext(r.Context(), "render", "page", page, "err", err)
	}
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

func (s *server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.log.InfoContext(r.Context(), "request", "method", r.Method, "path", redactPath(r.URL.Path), "status", rec.status)
	})
}

// redactPath hides the group secret so it never reaches the logs.
func redactPath(p string) string {
	rest, ok := strings.CutPrefix(p, "/g/")
	if !ok {
		return p
	}
	_, tail, _ := strings.Cut(rest, "/")
	if tail != "" {
		tail = "/" + tail
	}
	return "/g/<secret>" + tail
}

func (s *server) healthz(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		s.log.ErrorContext(r.Context(), "healthz", "err", err)
		http.Error(w, "db unavailable", http.StatusServiceUnavailable)
		return
	}
	_, _ = w.Write([]byte("ok"))
}

func (s *server) group(w http.ResponseWriter, r *http.Request) {
	g, err := s.store.GroupBySecret(r.Context(), r.PathValue("secret"))
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.log.ErrorContext(r.Context(), "group lookup", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.render(w, r, "group", g)
}
