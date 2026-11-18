// Package prefill reads a concert from a ticketing page: Open Graph, JSON-LD and, for a few
// sites, rules on the visible text.
package prefill

import (
	"bytes"
	"cmp"
	"encoding/json"
	"io"
	"maps"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/net/html"
)

// Result is what a ticketing page tells about a concert; fields are "" when unknown.
// Date, Time and OnSaleDate are Europe/Paris wall clock (YYYY-MM-DD, HH:MM).
// Price is for display (« 29–32 € »); Description keeps line feeds, at any length (the caller caps it).
type Result struct {
	Artist, Date, Time, Venue, Address, OnSaleDate, Price, Description, Genre string
}

// page holds the raw metadata found in a document.
type page struct {
	meta   map[string]string // og:title, twitter:title… (first value wins)
	title  string
	ldJSON [][]byte
	texts  []text // visible text, only for hosts with a site rule
}

// text is a visible text node; class is the innermost open element's, cleared by any end tag.
// block: a block tag (paragraph, line break, heading…) came between it and the previous text.
type text struct {
	s, class       string
	heading, block bool
}

var blockTags = []string{"p", "br", "div", "li", "h1", "h2", "h3", "h4", "h5", "h6"}

// maxTexts bounds the memory a page of tiny text nodes can take.
const maxTexts = 5000

// voidTags never get an end tag, so they must not replace the open element's class.
var voidTags = []string{"area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "source", "track", "wbr"}

// scan collects meta tags, <title>, JSON-LD blocks and, withTexts, visible text, with the tokenizer: no tree, no scripts run.
func scan(r io.Reader, withTexts bool) page {
	p := page{meta: map[string]string{}}
	z := html.NewTokenizer(r)
	var class string
	var block bool
	var headings, hidden int // open h1–h6; open svg or template, whose text and title are not the page's
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
				p.texts = append(p.texts, text{s: s, class: class, heading: headings > 0, block: block})
				block = false
			}
		case html.EndTagToken:
			class = ""
			name, _ := z.TagName()
			block = block || slices.Contains(blockTags, string(name))
			if d := depth(string(name)); d != nil {
				*d = max(*d-1, 0)
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			t := z.Token()
			block = block || slices.Contains(blockTags, t.Data)
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
				if hidden == 0 && z.Next() == html.TextToken && p.title == "" { // not an svg's
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
		res.Date, res.Time = site.Date, site.Time // JSON-LD sets a time only with a date
		res.Venue, res.Address = cmp.Or(res.Venue, site.Venue), cmp.Or(res.Address, site.Address)
		res.Price, res.Description = cmp.Or(res.Price, site.Price), cmp.Or(res.Description, site.Description)
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
	res.Artist = performers(ev["performer"])
	if name := clean(str(ev["name"])); res.Artist == "" {
		res.Artist = name
	}
	res.Date, res.Time = wallClock(str(ev["startDate"]), now.Location())
	switch loc := first(ev["location"]).(type) {
	case string:
		res.Venue = clean(str(loc))
	case map[string]any:
		res.Venue = clean(str(loc["name"]))
		if a := address(first(loc["address"])); a != res.Venue { // some pages repeat the venue
			res.Address = a
		}
	}
	res.OnSaleDate = onSale(ev["offers"], now)
	res.Price = price(ev["offers"])
	res.Genre = genre(ev["genre"])
	res.Description = unglue(prose(str(ev["description"])))
	return res
}

// unglue separates words that a site glued by stripping the markup between them: a capitalised
// word after a run of capitals goes on its own line (« SNIPERGroupe »), and so does such a heading
// after a sentence end (« énergie.KENZOriginaire »); another sentence start gets a space
// (« détermination.En », « 2027.KENZ revient », « D.C.Le »). Initials (« U.S.A. ») and names
// like « McCartney » are left alone.
func unglue(s string) string {
	r := []rune(s)
	is := func(i int, f func(rune) bool) bool { return i >= 0 && i < len(r) && f(r[i]) }
	upper := func(i int) bool { return is(i, unicode.IsUpper) }
	lower := func(i int) bool { return is(i, unicode.IsLower) }
	word := func(i int) bool { return is(i, unicode.IsLetter) || is(i, unicode.IsDigit) }
	// gluedHeading: a run of capitals from i glued to a capitalised word.
	gluedHeading := func(i int) bool {
		j := i
		for upper(j) {
			j++
		}
		return j-i >= 3 && lower(j)
	}
	var b strings.Builder
	for i, c := range r {
		switch {
		case upper(i) && lower(i+1) && upper(i-1) && upper(i-2):
			b.WriteByte('\n')
		case upper(i) && is(i-1, func(p rune) bool { return strings.ContainsRune(".!?", p) }) && word(i-2) && (word(i-3) || lower(i+1)):
			if gluedHeading(i) {
				b.WriteByte('\n')
			} else {
				b.WriteByte(' ')
			}
		}
		b.WriteRune(c)
	}
	return b.String()
}

// prose makes a JSON-LD description plain text: line feeds for CR, then clean.
func prose(s string) string {
	return clean(strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(s))
}

// plain keeps a value's words apart and acceptable to the form, which would reject it
// whole: line feeds kept, other blanks (tabs, braille blank…) as spaces, other
// Invisible characters (soft hyphens, zero-width spaces…) dropped.
func plain(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n':
			return r
		case unicode.IsSpace(r) || r == '\u2800':
			return ' '
		case Invisible(r):
			return -1
		}
		return r
	}, s)
}

// Invisible reports control, format, variation selector, default-ignorable and braille blank
// characters: the form rejects them, and plain drops them from every prefilled value.
func Invisible(r rune) bool {
	return r == '\u2800' || unicode.In(r, unicode.Cc, unicode.Cf, unicode.Variation_Selector, unicode.Other_Default_Ignorable_Code_Point)
}

// price formats the range of euro amounts among offers, however nested; a priceCurrency
// applies to the offers within, and none means euros. "" when there is none.
func price(offers any) string {
	var lo, hi float64
	var visit func(v any, currency string)
	visit = func(v any, currency string) {
		switch v := v.(type) {
		case []any:
			for _, e := range v {
				visit(e, currency)
			}
		case map[string]any:
			currency = cmp.Or(clean(str(v["priceCurrency"])), currency)
			if strings.EqualFold(cmp.Or(currency, "EUR"), "EUR") {
				for _, k := range []string{"price", "lowPrice", "highPrice"} {
					lo, hi = widen(lo, hi, amount(v[k]))
				}
			}
			for _, k := range slices.Sorted(maps.Keys(v)) {
				visit(v[k], currency)
			}
		}
	}
	visit(offers, "")
	return formatPrice(lo, hi)
}

// amount reads a schema.org price, a number or a string with a dot or comma, rounded to the cent;
// 0 when missing, not positive or implausible (≥ 10,000: a placeholder, not a ticket).
func amount(v any) float64 {
	var p float64
	switch v := v.(type) {
	case float64:
		p = v
	case string:
		p, _ = strconv.ParseFloat(strings.Replace(clean(v), ",", ".", 1), 64)
	}
	if p <= 0 || p >= 10000 || math.IsNaN(p) {
		return 0
	}
	return math.Round(p*100) / 100
}

// widen extends the price range lo–hi (0–0 when empty) to p, unless p is not positive.
func widen(lo, hi, p float64) (float64, float64) {
	if p <= 0 {
		return lo, hi
	}
	return cmp.Or(min(lo, p), p), max(hi, p)
}

// formatPrice writes « 32 € » or « 29–32 € », with cents only when there are some (« 39,90 € »); "" for no price.
func formatPrice(lo, hi float64) string {
	euros := func(p float64) string {
		if p == math.Trunc(p) {
			return strconv.FormatFloat(p, 'f', 0, 64)
		}
		return strings.Replace(strconv.FormatFloat(p, 'f', 2, 64), ".", ",", 1)
	}
	switch {
	case hi == 0:
		return ""
	case lo == hi:
		return euros(lo) + " €"
	}
	return euros(lo) + "–" + euros(hi) + " €"
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

// performers joins the performers' names with « + », a separator the concert page splits on.
func performers(v any) string {
	a, ok := v.([]any)
	if !ok {
		a = []any{v}
	}
	var names []string
	for _, p := range a {
		if n := clean(nameOf(p)); n != "" && !slices.Contains(names, n) {
			names = append(names, n)
		}
	}
	return strings.Join(names, " + ")
}

// genre joins schema.org genres, a string or an array of them, with « , ».
func genre(v any) string {
	a, ok := v.([]any)
	if !ok {
		a = []any{v}
	}
	var gs []string
	for _, g := range a {
		if s := clean(str(g)); s != "" && !slices.Contains(gs, s) {
			gs = append(gs, s)
		}
	}
	return strings.Join(gs, ", ")
}

// str is a JSON-LD string with HTML entities decoded: WordPress encodes them (« &#8217; », « &amp; »).
// Only whole entities closed by « ; »: html.UnescapeString alone reads « Salle&regie » and
// « Salle&regie; » as « Salle®ie… », decoding the longest entity name a word starts with.
func str(v any) string {
	s, _ := v.(string)
	return entity.ReplaceAllStringFunc(s, func(e string) string {
		if d := html.UnescapeString(e); d == ";" || !strings.HasSuffix(d, ";") {
			return d
		}
		return e
	})
}

var entity = regexp.MustCompile(`&(?:#[0-9]+|#[xX][0-9a-fA-F]+|[A-Za-z][A-Za-z0-9]*);`)

func nameOf(v any) string {
	if m, ok := v.(map[string]any); ok {
		return str(m["name"])
	}
	return str(v)
}

// clean makes text valid UTF-8 and plain, then trims spaces (no-break ones too).
func clean(s string) string {
	return strings.TrimFunc(plain(strings.ToValidUTF8(s, "")), unicode.IsSpace)
}

var dateLayouts = []string{time.RFC3339Nano, "2006-01-02T15:04Z07:00", "2006-01-02T15:04:05Z0700", "2006-01-02T15:04Z0700", "2006-01-02T15:04:05Z07", "2006-01-02T15:04Z07", "2006-01-02T15:04:05", "2006-01-02T15:04", time.DateOnly}

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
