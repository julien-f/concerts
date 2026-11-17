// Package prefill reads a concert from a ticketing page: Open Graph and JSON-LD.
package prefill

import (
	"bytes"
	"cmp"
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
	Artist, Date, Time, Venue, Address, OnSaleDate string
}

// page holds the raw metadata found in a document.
type page struct {
	meta   map[string]string // og:title, twitter:title… (first value wins)
	title  string
	ldJSON [][]byte
	texts  []text // visible text, only for hosts with a site rule
}

// text is a visible text node; class is the innermost open element's, cleared by any end tag.
type text struct {
	s, class string
	heading  bool
}

// maxTexts bounds the memory a page of tiny text nodes can take.
const maxTexts = 5000

// voidTags never get an end tag, so they must not replace the open element's class.
var voidTags = []string{"area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "source", "track", "wbr"}

// scan collects meta tags, <title>, JSON-LD blocks and, withTexts, visible text, with the tokenizer: no tree, no scripts run.
func scan(r io.Reader, withTexts bool) page {
	p := page{meta: map[string]string{}}
	z := html.NewTokenizer(r)
	var class string
	var headings, hidden int // open h1–h6; open svg or template, whose text is not shown as is
	depth := func(tag string) *int {
		switch tag {
		case "h1", "h2", "h3", "h4", "h5", "h6":
			return &headings
		case "svg", "template":
			return &hidden
		}
		return nil
	}
	for {
		switch tt := z.Next(); tt {
		case html.ErrorToken:
			return p
		case html.TextToken:
			if !withTexts || hidden > 0 || len(p.texts) >= maxTexts {
				continue
			}
			if s := clean(string(z.Text())); s != "" {
				p.texts = append(p.texts, text{s: s, class: class, heading: headings > 0})
			}
		case html.EndTagToken:
			class = ""
			name, _ := z.TagName()
			if d := depth(string(name)); d != nil {
				*d = max(*d-1, 0)
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			t := z.Token()
			if !slices.Contains(voidTags, t.Data) { // HTML ignores the slash of <span/>: it opens the element
				class = ""
				for _, a := range t.Attr {
					if a.Key == "class" {
						class = a.Val
					}
				}
				if d := depth(t.Data); d != nil && tt == html.StartTagToken { // <svg/> does close itself
					*d++
				}
			}
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
				ld := slices.ContainsFunc(t.Attr, func(a html.Attribute) bool {
					return a.Key == "type" && strings.EqualFold(strings.TrimSpace(a.Val), "application/ld+json")
				})
				if z.Next() == html.TextToken && ld {
					p.ldJSON = append(p.ldJSON, bytes.Clone(z.Text()))
				}
				class = "" // the token read may have been the end tag
			case "style", "noscript", "textarea", "iframe", "noembed", "noframes", "xmp":
				z.Next() // raw text: the tokenizer returns the content, markup included, as one text token
				class = ""
			}
		}
	}
}

// Parse extracts a Result from a page of host; now is in Europe/Paris and drops past on-sale dates.
func Parse(r io.Reader, host string, now time.Time) Result {
	rule := siteRule(host)
	p := scan(r, rule != nil)
	var res Result
	if ev := findEvent(p.ldJSON); ev != nil {
		res = fromEvent(ev, now)
	}
	if res.Artist == "" {
		res.Artist = titleArtist(p)
	}
	// A date from JSON-LD means an event was found: do not mix in another source.
	if rule != nil && res.Date == "" {
		site := rule(p.texts)
		res.Date, res.Time = site.Date, cmp.Or(res.Time, site.Time)
		res.Venue, res.Address = cmp.Or(res.Venue, site.Venue), cmp.Or(res.Address, site.Address)
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
		if a := address(first(loc["address"])); a != res.Venue { // some pages repeat the venue
			res.Address = a
		}
	}
	res.OnSaleDate = onSale(ev["offers"], now)
	return res
}

// address formats a schema.org address: a string, or a PostalAddress as
// "street, postcode locality" (the country is left out: the app is French).
func address(v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return clean(str(v))
	}
	street := clean(str(m["streetAddress"]))
	postal, city := clean(str(m["postalCode"])), clean(str(m["addressLocality"]))
	// Some sites put the whole address in streetAddress: do not repeat the city.
	if lower := strings.ToLower(street); (postal != "" && strings.Contains(lower, strings.ToLower(postal))) ||
		(city != "" && strings.Contains(lower, strings.ToLower(city))) {
		postal, city = "", ""
	}
	var parts []string
	for _, s := range []string{street, clean(postal + " " + city)} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
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
