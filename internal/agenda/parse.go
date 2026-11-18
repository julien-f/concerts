package agenda

import (
	"cmp"
	neturl "net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"concerts/internal/prefill"
	"concerts/internal/store"
	"concerts/internal/validate"
)

// all returns the elements under n, n included, that match, in document order, without descending into a match.
func all(n *html.Node, match func(*html.Node) bool) []*html.Node {
	var out []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && match(n) {
			out = append(out, n)
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return out
}

// first returns the first element under n, n included, that matches, or nil.
func first(n *html.Node, match func(*html.Node) bool) *html.Node {
	if n.Type == html.ElementNode && match(n) {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if f := first(c, match); f != nil {
			return f
		}
	}
	return nil
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func classes(n *html.Node) []string { return strings.Fields(attr(n, "class")) }

// class matches the elements whose class attribute holds c.
func class(c string) func(*html.Node) bool {
	return func(n *html.Node) bool { return slices.Contains(classes(n), c) }
}

// tag matches the elements of one of the tags.
func tag(tags ...atom.Atom) func(*html.Node) bool {
	return func(n *html.Node) bool { return slices.Contains(tags, n.DataAtom) }
}

// classPrefix returns the rest of n's first class starting with prefix (« post-12 » → « 12 »), or "".
func classPrefix(n *html.Node, prefix string) string {
	for _, c := range classes(n) {
		if rest, ok := strings.CutPrefix(c, prefix); ok && rest != "" {
			return rest
		}
	}
	return ""
}

// text is n's text, whitespace runs collapsed; a <br> counts as a space. "" for a nil n.
func text(n *html.Node) string {
	if n == nil {
		return ""
	}
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		switch {
		case n.Type == html.TextNode:
			b.WriteString(n.Data)
		case n.DataAtom == atom.Br:
			b.WriteString(" ")
		case n.DataAtom == atom.Script || n.DataAtom == atom.Style || n.DataAtom == atom.Svg:
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.Join(strings.Fields(b.String()), " ")
}

// link resolves the href of n (nil allowed) against page; "" if absent or invalid.
func link(page *neturl.URL, n *html.Node) string {
	if n == nil || attr(n, "href") == "" {
		return ""
	}
	u, err := page.Parse(strings.TrimSpace(attr(n, "href")))
	if err != nil {
		return ""
	}
	return u.String()
}

var (
	// A status opens or closes a title: « COMPLET • X », « X (COMPLET) », « X \ COMPLET / ».
	statusPrefix = regexp.MustCompile(`(?i)^(complet|annul[ée]e?|report[ée]e?)\s*[•·:|–-]\s*`)
	statusSuffix = regexp.MustCompile(`(?i)\s*(?:\(\s*(complet|annul[ée]e?|report[ée]e?)\s*\)|\\\s*(complet|annul[ée]e?|report[ée]e?)\s*/)$`)
)

// cleanTitle strips status words from a listing's title, reporting « complet », and
// trailing separators (« Suite Habana – »). Entities are decoded already.
func cleanTitle(s string) (string, bool) {
	s = strings.Join(strings.Fields(s), " ")
	full := false
	for {
		m := statusPrefix.FindStringSubmatch(s)
		if m == nil {
			m = statusSuffix.FindStringSubmatch(s)
		}
		if m == nil {
			break
		}
		full = full || strings.Contains(strings.ToLower(m[0]), "complet")
		s = strings.Replace(s, m[0], "", 1)
	}
	return strings.TrimRight(s, " •·|–-"), full
}

// listingDate reads a French date: « [weekday] D month [YYYY] », or a range « Du … au … »
// whose start counts. Without a year it is the next such day from today, unless that is
// more than 10 months ahead: a past item still listed, so the year before.
func listingDate(s, today string) string {
	s = strings.Join(strings.Fields(s), " ")
	if rest, ok := strings.CutPrefix(strings.ToLower(s), "du "); ok {
		s, _, _ = strings.Cut(rest, " au ")
	}
	if d := prefill.FrenchDate(s); d != "" {
		return d
	}
	t, err := time.Parse(time.DateOnly, today)
	if err != nil {
		return ""
	}
	for _, y := range []int{t.Year(), t.Year() + 1} {
		d := prefill.FrenchDate(s + " " + strconv.Itoa(y))
		switch {
		case d == "" || d < today: // no such day that year (29 February), or past
			continue
		case d > t.AddDate(0, 10, 0).Format(time.DateOnly):
			return prefill.FrenchDate(s + " " + strconv.Itoa(y-1))
		}
		return d
	}
	return ""
}

// clip cuts s to n runes, ending with « … » when cut.
func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

// event cleans and validates what a listing gave; false if the concert form would refuse it.
func (v *Venue) event(l listed) (store.Event, bool) {
	price := strings.Join(strings.Fields(l.price), " ")
	if utf8.RuneCountInString(price) > validate.MaxPriceRunes {
		price = ""
	}
	c, errs := validate.Concert(store.Concert{
		Artist:    clip(l.artist, validate.MaxTextRunes),
		Date:      l.date,
		Time:      l.time,
		Venue:     clip(cmp.Or(l.venue, v.Name), validate.MaxTextRunes),
		Address:   clip(l.address, validate.MaxTextRunes),
		TicketURL: v.ticketLink(l),
		Price:     price,
		SoldOut:   l.full,
	})
	id, ok := store.CleanSourceID(l.id)
	return store.Event{SourceID: id, Concert: c}, ok && len(errs) == 0
}

// ticketLink is the listing's ticket link if it points to the venue or a ticketing site it
// names, else the event's page: a compromised venue site cannot send members anywhere.
func (v *Venue) ticketLink(l listed) string {
	for _, s := range []string{l.ticket, l.page} {
		if u, err := neturl.Parse(s); err == nil && s != "" && v.ticketing(u) && validate.TicketURL(s) {
			return s
		}
	}
	return ""
}
