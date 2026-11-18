package agenda

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	neturl "net/url"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"concerts/internal/store"
)

// pipeParser reads a test listing: one « id|artist|date|time|page » line per event.
func pipeParser(page *neturl.URL, body []byte, _ string) ([]listed, []*neturl.URL, error) {
	var items []listed
	for line := range strings.Lines(strings.TrimSpace(string(body))) {
		f := strings.Split(strings.TrimSpace(line), "|")
		if len(f) != 5 {
			return nil, nil, fmt.Errorf("bad line %q", line)
		}
		u, err := page.Parse(f[4])
		if err != nil {
			return nil, nil, err
		}
		l := listed{id: f[0], date: f[2], time: f[3], page: u.String()}
		l.artist, l.full = cleanTitle(f[1])
		items = append(items, l)
	}
	return items, nil, nil
}

// syncSite serves test listings and event pages, counting the requests to event pages.
type syncSite struct {
	mu       sync.Mutex
	pages    map[string]string   // path → body
	listings map[string]int      // requests per listing path
	events   int                 // requests to /e/…, found or not
	before   func(*http.Request) // if set, called first, without the lock
}

func (s *syncSite) handle(w http.ResponseWriter, r *http.Request) {
	if s.before != nil {
		s.before(r)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	body, ok := s.pages[r.URL.Path]
	if strings.HasPrefix(r.URL.Path, "/e/") {
		s.events++
	} else {
		s.listings[r.URL.Path]++
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	_, _ = w.Write([]byte(body))
}

// eventPage is an event page prefill reads: JSON-LD with a start time.
func eventPage(start string) string {
	return `<html><head><script type="application/ld+json">{"@type":"MusicEvent","name":"x","startDate":"` + start + `"}</script></head></html>`
}

func syncVenue(key, listing string, details, promoter bool) Venue {
	return Venue{
		Key: key, Name: strings.ToUpper(key), Listings: []string{"https://www.example.com" + listing}, Format: "text/html",
		Site: []string{"www.example.com"}, MaxPages: 1, Details: details, Promoter: promoter, parse: pipeParser,
	}
}

type syncEnv struct {
	st    *store.Store
	site  *syncSite
	sync  func(venues []Venue) (string, error) // returns the logs
	run   func(ctx context.Context, venues []Venue) (string, error)
	group func(name string, follows ...string) int64
}

func newSyncEnv(t *testing.T, pages map[string]string) *syncEnv {
	t.Helper()
	ctx := context.Background()
	now := func() time.Time { return time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC) }
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "c.db"), now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	site := &syncSite{pages: pages, listings: map[string]int{}}
	client := testSite(t, site.handle)
	run := func(ctx context.Context, venues []Venue) (string, error) {
		var logs bytes.Buffer
		err := Sync(ctx, st, client, venues, now, (&sleeps{}).sleep, slog.New(slog.NewTextHandler(&logs, nil)))
		return logs.String(), err
	}
	return &syncEnv{
		st: st, site: site, run: run,
		sync: func(venues []Venue) (string, error) { return run(ctx, venues) },
		group: func(name string, follows ...string) int64 {
			g, err := st.CreateGroup(ctx, name, "secret-"+name)
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range follows {
				if err := st.Follow(ctx, g.ID, v); err != nil {
					t.Fatal(err)
				}
			}
			return g.ID
		},
	}
}

// concerts lists a group's concerts as « source:artist date time ».
func (e *syncEnv) concerts(t *testing.T, group int64) []string {
	t.Helper()
	cs, err := e.st.Concerts(context.Background(), group)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, c := range cs {
		out = append(out, strings.TrimSpace(c.Source+":"+c.Artist+" "+c.Date+" "+c.Time))
	}
	slices.Sort(out)
	return out
}

func TestSync(t *testing.T) {
	t.Parallel()
	env := newSyncEnv(t, map[string]string{
		"/hall": "h1|COMPLET • Les Hiboux|2026-12-01|20:00|/e/h1\n" +
			"h2|Nova|2026-11-17|20:00|/e/h2\n" + // yesterday
			"h3||2026-12-02|20:00|/e/h3\n", // no artist: invalid
		// The promoter lists the hall's concert too, and one of its own without time.
		"/promo": "p1|LES HIBOUX + Nova|2026-12-01||/e/p1\n" +
			"p2|Ventura|2026-12-05||/e/p2\n" +
			"p3|Lune|2026-12-06||/e/p3\n",
		"/e/p1":  eventPage("2026-12-01T20:00:00+01:00"),
		"/e/p2":  eventPage("2026-12-05T20:30:00+01:00"),
		"/e/p3":  eventPage("2026-12-07T21:00:00+01:00"), // another date: no time taken
		"/other": "o1|Ignored|2026-12-01|20:00|/e/o1\n",
	})
	g1 := env.group("g1", "hall", "promo")
	g2 := env.group("g2", "hall")
	venues := []Venue{
		syncVenue("hall", "/hall", false, false),
		syncVenue("promo", "/promo", true, true),
		syncVenue("other", "/other", false, false), // nobody follows it
	}

	logs, err := env.sync(venues)
	if err != nil {
		t.Fatalf("Sync: %v\n%s", err, logs)
	}
	want1 := []string{"hall:Les Hiboux 2026-12-01 20:00", "promo:Lune 2026-12-06", "promo:Ventura 2026-12-05 20:30"}
	if got := env.concerts(t, g1); !slices.Equal(got, want1) {
		t.Errorf("g1 = %q, want %q", got, want1)
	}
	if got := env.concerts(t, g2); !slices.Equal(got, want1[:1]) {
		t.Errorf("g2 = %q, want %q", got, want1[:1])
	}
	// Each event read is kept once for every group, as imported: « Relire la billetterie » reads it.
	cs, err := env.st.Concerts(context.Background(), g2)
	if err != nil || len(cs) != 1 {
		t.Fatalf("g2: %d concerts, %v", len(cs), err)
	}
	if c, err := env.st.SourceEvent(context.Background(), g2, cs[0].ID); err != nil || c.Artist != "Les Hiboux" || !c.SoldOut || c.Time != "20:00" {
		t.Errorf("venue event kept = %+v, %v", c, err)
	}
	// p1 is the hall's concert: its page is read anyway (before the dedup), p2 and p3 too.
	if env.site.events != 3 || env.site.listings["/other"] != 0 || env.site.listings["/hall"] != 1 {
		t.Errorf("requests: %d event pages, listings %v", env.site.events, env.site.listings)
	}
	for _, leak := range []string{"https://", "/hall", "/e/", "Hiboux", "Ventura", "secret"} {
		if strings.Contains(logs, leak) {
			t.Errorf("logs hold %q:\n%s", leak, logs)
		}
	}
	for _, want := range []string{"venue=hall host=www.example.com", "past=1", "invalid=1", "event_pages=3", "duplicate=1"} {
		if !strings.Contains(logs, want) {
			t.Errorf("logs lack %q:\n%s", want, logs)
		}
	}

	// Event pages are read once ever, whatever the groups hold.
	env.group("g3", "promo")
	logs, err = env.sync(venues)
	if err != nil || env.site.events != 3 {
		t.Fatalf("second Sync: %v, %d event page requests in all, want 3\n%s", err, env.site.events, logs)
	}
	if got := env.concerts(t, g1); !slices.Equal(got, want1) {
		t.Errorf("g1 after the second run = %q, want %q", got, want1)
	}
}

func TestSyncFailures(t *testing.T) {
	t.Parallel()
	env := newSyncEnv(t, map[string]string{
		"/ok":    "a|Les Hiboux|2026-12-01|20:00|/e/a\n",
		"/empty": "b|Nova|2026-11-01|20:00|/e/b\n", // only a past event: the parser broke
	})
	g := env.group("g", "ok", "empty", "missing")
	logs, err := env.sync([]Venue{
		syncVenue("missing", "/missing", false, false), // 404
		syncVenue("empty", "/empty", false, false),
		syncVenue("ok", "/ok", false, false),
	})
	if err == nil || !strings.Contains(err.Error(), "missing, empty") {
		t.Errorf("Sync = %v, want missing and empty failed\n%s", err, logs)
	}
	if got := env.concerts(t, g); !slices.Equal(got, []string{"ok:Les Hiboux 2026-12-01 20:00"}) {
		t.Errorf("concerts = %q: the venue that worked is still applied", got)
	}
}

func TestSyncCap(t *testing.T) {
	t.Parallel()
	var listing strings.Builder
	for i := range maxEvents + 5 {
		fmt.Fprintf(&listing, "e%d|Artist %d|2027-01-%02d|20:00|/e/%d\n", i, i, i%28+1, i)
	}
	env := newSyncEnv(t, map[string]string{"/big": listing.String()})
	g := env.group("g", "big")
	if _, err := env.sync([]Venue{syncVenue("big", "/big", false, false)}); err != nil {
		t.Fatal(err)
	}
	if got := len(env.concerts(t, g)); got != maxEvents {
		t.Errorf("%d concerts, want %d", got, maxEvents)
	}
}

func TestSyncDetailBudget(t *testing.T) {
	t.Parallel()
	pages := map[string]string{}
	var listing strings.Builder
	for i := range maxDetails + 3 {
		fmt.Fprintf(&listing, "e%d|Artist %d|2027-01-%02d||/e/%d\n", i, i, i%28+1, i)
		pages[fmt.Sprintf("/e/%d", i)] = eventPage(fmt.Sprintf("2027-01-%02dT20:00:00+01:00", i%28+1))
	}
	pages["/l"] = listing.String()
	env := newSyncEnv(t, pages)
	env.group("g", "l")
	v := []Venue{syncVenue("l", "/l", true, false)}
	for run, want := range []int{maxDetails, maxDetails + 3, maxDetails + 3} {
		if _, err := env.sync(v); err != nil || env.site.events != want {
			t.Errorf("run %d: %v, %d event pages read in all, want %d", run+1, err, env.site.events, want)
		}
	}
}

// TestSyncCancelled: a cancel mid-run keeps what the venues read before it applied.
func TestSyncCancelled(t *testing.T) {
	t.Parallel()
	env := newSyncEnv(t, map[string]string{"/a": "a|A|2026-12-01|20:00|/e/a\n", "/b": "b|B|2026-12-02|20:00|/e/b\n"})
	g := env.group("g", "a", "b")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	env.site.before = func(r *http.Request) {
		if r.URL.Path == "/b" {
			cancel()
		}
	}
	logs, err := env.run(ctx, []Venue{syncVenue("a", "/a", false, false), syncVenue("b", "/b", false, false)})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Sync = %v, want context.Canceled\n%s", err, logs)
	}
	if got := env.concerts(t, g); !slices.Equal(got, []string{"a:A 2026-12-01 20:00"}) {
		t.Errorf("concerts = %q, want a's only", got)
	}
}

// TestSyncVenueTimeout: a venue that hangs fails alone once its deadline passes.
func TestSyncVenueTimeout(t *testing.T) {
	t.Parallel()
	env := newSyncEnv(t, map[string]string{"/slow": "s|S|2026-12-01|20:00|/e/s\n", "/b": "b|B|2026-12-02|20:00|/e/b\n"})
	g := env.group("g", "slow", "b")
	env.site.before = func(r *http.Request) {
		if r.URL.Path == "/slow" {
			<-r.Context().Done()
		}
	}
	slow := syncVenue("slow", "/slow", false, false)
	slow.timeout = time.Millisecond
	logs, err := env.sync([]Venue{slow, syncVenue("b", "/b", false, false)})
	if err == nil || !strings.Contains(err.Error(), "slow") {
		t.Errorf("Sync = %v, want slow failed\n%s", err, logs)
	}
	if got := env.concerts(t, g); !slices.Equal(got, []string{"b:B 2026-12-02 20:00"}) {
		t.Errorf("concerts = %q, want b's", got)
	}
}

// TestSyncDetailFailures: an event page without a date is read again next run, and the venue
// fails when none of its pages had one; a page gone (404) is not read again.
func TestSyncDetailFailures(t *testing.T) {
	t.Parallel()
	env := newSyncEnv(t, map[string]string{
		"/l":   "a|A|2026-12-01||/e/a\nb|B|2026-12-02||/e/b\n", // /e/b: 404
		"/e/a": "<html><p>Bientôt</p></html>",
	})
	g := env.group("g", "l")
	v := []Venue{syncVenue("l", "/l", true, false)}
	logs, err := env.sync(v)
	if err == nil || err.Error() != "venues failed: l" {
		t.Errorf("Sync = %v, want l failed: no event page had a date\n%s", err, logs)
	}
	if !strings.Contains(logs, "undated=1") {
		t.Errorf("logs lack undated=1:\n%s", logs)
	}
	if got := env.concerts(t, g); !slices.Equal(got, []string{"l:A 2026-12-01", "l:B 2026-12-02"}) {
		t.Errorf("concerts = %q", got)
	}
	env.site.mu.Lock()
	env.site.pages["/e/a"] = eventPage("2026-12-01T20:00:00+01:00")
	env.site.mu.Unlock()
	if logs, err := env.sync(v); err != nil || env.site.events != 3 {
		t.Errorf("second Sync: %v, %d event page requests in all, want 3 (a again, b gone)\n%s", err, env.site.events, logs)
	}
	if got := env.concerts(t, g); !slices.Equal(got, []string{"l:A 2026-12-01 20:00", "l:B 2026-12-02"}) {
		t.Errorf("concerts after the second run = %q", got)
	}
}
