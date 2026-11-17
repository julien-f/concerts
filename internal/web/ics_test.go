package web

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"concerts/internal/store"
)

var stamp = time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)

func TestICSGolden(t *testing.T) {
	t.Parallel()
	tests := map[string]store.Concert{
		// Winter time: 20:30 in Paris is 19:30 UTC.
		"timed.ics": {ID: 3, Artist: "Zaz", Date: "2027-03-12", Time: "20:30", Venue: "L’Olympia", Address: "28 bd des Capucines; Paris",
			TicketURL: "https://tickets.example/zaz?a=1&b=2"},
		// No time: an all-day event whose exclusive end crosses the year.
		"allday.ics": {ID: 4, Artist: "Les Négresses Vertes 🎸", Date: "2027-12-31", Venue: "Plein air"},
	}
	for name, c := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			want, err := os.ReadFile("testdata/" + name)
			if err != nil {
				t.Fatal(err)
			}
			if got := ics(7, c, stamp); got != string(want) {
				t.Fatalf("ics =\n%q\nwant\n%q", got, want)
			}
		})
	}
}

func TestICSSummerTime(t *testing.T) {
	t.Parallel()
	got := ics(1, store.Concert{ID: 1, Artist: "A", Date: "2027-07-04", Time: "21:00", Venue: "V"}, stamp)
	if !strings.Contains(got, "DTSTART:20270704T190000Z\r\nDTEND:20270704T220000Z\r\n") {
		t.Fatalf("summer time (UTC+2) not applied:\n%s", got)
	}
}

func TestICSEscapingAndFolding(t *testing.T) {
	t.Parallel()
	artist := `a\b;c,d` + "\nURL:https://evil.example\r\n" + strings.Repeat("é", 60) + "🎸"
	got := ics(1, store.Concert{ID: 1, Artist: artist, Date: "2027-01-01", Venue: "V"}, stamp)

	if !strings.HasSuffix(got, "END:VCALENDAR\r\n") || strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Fatalf("not CRLF throughout:\n%q", got)
	}
	var unfolded []string
	for _, l := range strings.Split(strings.TrimSuffix(got, "\r\n"), "\r\n") {
		if len(l) > 75 || !utf8.ValidString(l) {
			t.Errorf("line of %d octets or cut inside a rune: %q", len(l), l)
		}
		if strings.HasPrefix(l, "URL:") {
			t.Errorf("a field started a property line: %q", l)
		}
		if rest, ok := strings.CutPrefix(l, " "); ok {
			unfolded[len(unfolded)-1] += rest
		} else {
			unfolded = append(unfolded, l)
		}
	}
	want := `SUMMARY:a\\b\;c\,d\nURL:https://evil.example\n` + strings.Repeat("é", 60) + "🎸"
	found := false
	for _, l := range unfolded {
		found = found || l == want
	}
	if !found {
		t.Fatalf("unfolded lines lack %q:\n%q", want, unfolded)
	}
}

// TestICSFoldInvalidUTF8: stray continuation bytes must not stall folding (they once looped forever).
func TestICSFoldInvalidUTF8(t *testing.T) {
	t.Parallel()
	done := make(chan string)
	go func() { done <- icsFold("DESCRIPTION:" + strings.Repeat("\x80", 200)) }()
	select {
	case got := <-done:
		for _, l := range strings.Split(strings.TrimSuffix(got, "\r\n"), "\r\n") {
			if len(l) > 75 {
				t.Errorf("line of %d octets", len(l))
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("icsFold did not return")
	}
}

func TestAgendaRoute(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	b, secret := ts.member(t, "Léa")
	g := "/g/" + secret
	b.postRedirect(t, g+"/concerts", concertValues("Zaz", "2026-07-01", "20:30", "Olympia", "https://tickets.example/z", ""))
	b.postRedirect(t, g+"/concerts", validConcert())
	b.postRedirect(t, g+"/concerts/2/delete", nil)

	_, page := b.get(t, g+"/concerts/1")
	assertContains(t, page, `href="`+g+`/concerts/1/agenda.ics">Ajouter à mon agenda`)

	// No identity needed: the link is the access, like the concert page.
	resp, body := ts.browser(t).get(t, g+"/concerts/1/agenda.ics")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	for k, want := range map[string]string{
		"Content-Type":        "text/calendar; charset=utf-8",
		"Content-Disposition": `attachment; filename="concert.ics"`,
		"Cache-Control":       "private, no-store",
	} {
		if got := resp.Header.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	assertContains(t, body, "SUMMARY:Zaz", "DTSTART:20260701T183000Z", "Billets : https://tickets.example/z")
	if strings.Contains(body, secret) || strings.Contains(body, "/g/") {
		t.Errorf("the calendar holds the group link:\n%s", body)
	}

	for path, want := range map[string]int{
		g + "/concerts/2/agenda.ics":                   http.StatusNotFound, // deleted
		g + "/concerts/9/agenda.ics":                   http.StatusNotFound,
		"/g/" + NewSecret() + "/concerts/1/agenda.ics": http.StatusNotFound,
	} {
		if resp, _ := b.get(t, path); resp.StatusCode != want {
			t.Errorf("GET %s: status = %d, want %d", path, resp.StatusCode, want)
		}
	}
}
