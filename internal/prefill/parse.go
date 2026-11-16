// Package prefill reads a concert from a ticketing page: Open Graph and JSON-LD.
package prefill

import (
	"bytes"
	"encoding/json"
	"io"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode"

	"golang.org/x/net/html"
)

// Result is what a ticketing page tells about a concert; fields are "" when unknown.
// Date, Time and OnSaleDate are Europe/Paris wall clock (YYYY-MM-DD, HH:MM).
type Result struct {
	Artist, Date, Time, Venue, OnSaleDate string
}

// page holds the raw metadata found in a document.
type page struct {
	meta   map[string]string // og:title, twitter:title… (first value wins)
	title  string
	ldJSON [][]byte
}

// scan collects meta tags, <title> and JSON-LD blocks with the tokenizer: no tree, no scripts run.
func scan(r io.Reader) page {
	p := page{meta: map[string]string{}}
	z := html.NewTokenizer(r)
	for {
		switch z.Next() {
		case html.ErrorToken:
			return p
		case html.StartTagToken, html.SelfClosingTagToken:
			t := z.Token()
			switch t.Data {
			case "meta":
				var key, content string
				for _, a := range t.Attr {
					switch a.Key {
					case "property", "name": // Accor Arena writes og:title with name=
						key = strings.ToLower(a.Val)
					case "content":
						content = a.Val
					}
				}
				if _, seen := p.meta[key]; key != "" && !seen {
					p.meta[key] = content
				}
			case "title":
				if z.Next() == html.TextToken && p.title == "" {
					p.title = string(z.Text())
				}
			case "script":
				for _, a := range t.Attr {
					if a.Key == "type" && strings.EqualFold(strings.TrimSpace(a.Val), "application/ld+json") && z.Next() == html.TextToken {
						p.ldJSON = append(p.ldJSON, bytes.Clone(z.Text()))
					}
				}
			}
		}
	}
}

// Parse extracts a Result from an HTML document. now's location is the app's
// time zone (Europe/Paris); on-sale dates before now are dropped.
func Parse(r io.Reader, now time.Time) Result {
	p := scan(r)
	var res Result
	if ev := findEvent(p.ldJSON); ev != nil {
		res = fromEvent(ev, now)
	}
	if res.Artist == "" {
		res.Artist = titleArtist(p)
	}
	return res
}

// walk calls fn on every object of a JSON value, parents first, keys in sorted
// order so the first match is the same on every run.
func walk(v any, fn func(map[string]any)) {
	switch v := v.(type) {
	case []any:
		for _, e := range v {
			walk(e, fn)
		}
	case map[string]any:
		fn(v)
		for _, k := range slices.Sorted(maps.Keys(v)) {
			walk(v[k], fn)
		}
	}
}

// findEvent returns the first schema.org event (MusicEvent preferred) in the
// JSON-LD blocks, wherever it sits: top level, array, @graph, ItemList…
func findEvent(blocks [][]byte) map[string]any {
	var music, event map[string]any
	for _, b := range blocks {
		var v any
		if json.Unmarshal(b, &v) != nil {
			continue // one broken block must not hide the others
		}
		walk(v, func(n map[string]any) {
			ts := types(n)
			if music == nil && slices.Contains(ts, "MusicEvent") {
				music = n
			}
			if event == nil && slices.ContainsFunc(ts, func(t string) bool { return strings.HasSuffix(t, "Event") || t == "Festival" }) {
				event = n
			}
		})
	}
	if music != nil {
		return music
	}
	return event
}

func types(n map[string]any) []string {
	switch t := n["@type"].(type) {
	case string:
		return []string{t}
	case []any:
		var out []string
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func fromEvent(ev map[string]any, now time.Time) Result {
	var res Result
	res.Artist = clean(nameOf(first(ev["performer"])))
	if name := clean(str(ev["name"])); res.Artist == "" {
		res.Artist = name
	}
	res.Date, res.Time = wallClock(str(ev["startDate"]), now.Location())
	switch loc := first(ev["location"]).(type) {
	case string:
		res.Venue = clean(loc)
	case map[string]any:
		res.Venue = clean(str(loc["name"]))
	}
	res.OnSaleDate = onSale(ev["offers"], now)
	return res
}

// first returns v, or its first element if it is an array.
func first(v any) any {
	if a, ok := v.([]any); ok && len(a) > 0 {
		return a[0]
	}
	return v
}

func str(v any) string { s, _ := v.(string); return s }

func nameOf(v any) string {
	if m, ok := v.(map[string]any); ok {
		return str(m["name"])
	}
	return str(v)
}

// clean trims spaces (no-break ones too) and makes text valid UTF-8.
func clean(s string) string {
	return strings.TrimFunc(strings.ToValidUTF8(s, ""), unicode.IsSpace)
}

var dateLayouts = []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02T15:04", time.DateOnly}

// wallClock turns a schema.org date into a date and time in loc ("" for a date alone).
// A time without offset is already loc's wall clock; one with an offset is converted.
func wallClock(s string, loc *time.Location) (date, hm string) {
	s = clean(s)
	for _, layout := range dateLayouts {
		t, err := time.ParseInLocation(layout, s, loc)
		if err != nil {
			continue
		}
		t = t.In(loc)
		if layout == time.DateOnly {
			return t.Format(time.DateOnly), ""
		}
		return t.Format(time.DateOnly), t.Format("15:04")
	}
	return "", ""
}

// onSale finds the earliest validFrom or availabilityStarts among the offers,
// however nested, from today on: an earlier one is a listing date, not news.
func onSale(offers any, now time.Time) string {
	today, best := now.Format(time.DateOnly), ""
	walk(offers, func(n map[string]any) {
		for _, k := range []string{"validFrom", "availabilityStarts"} {
			if d, _ := wallClock(str(n[k]), now.Location()); d >= today && (best == "" || d < best) {
				best = d
			}
		}
	})
	return best
}

// titleSeparators end the artist part of a page title, e.g. "Artist - Venue | Site".
var titleSeparators = []string{" | ", "｜", " - ", " – ", " @ ", " Tickets", " Billets"}

// titleArtist guesses the artist from og:title, twitter:title or <title>. Never a date:
// title formats differ too much ("27 Nov", "mer., 28/10/2026").
func titleArtist(p page) string {
	for _, s := range []string{p.meta["og:title"], p.meta["twitter:title"], p.title} {
		s = clean(s)
		if s == "" || s == "null" {
			continue
		}
		cut := len(s)
		for _, sep := range titleSeparators {
			if i := strings.Index(s, sep); i > 0 && i < cut {
				cut = i
			}
		}
		return clean(s[:cut])
	}
	return ""
}
