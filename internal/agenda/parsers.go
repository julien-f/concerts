package agenda

import (
	"bytes"
	"encoding/json"
	neturl "net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"concerts/internal/prefill"
)

// euroGlued is « 42€ »: written « 42 € » as the form shows prices.
var euroGlued = regexp.MustCompile(`(\d)\s*€`)

type mc2Category struct {
	Name string `json:"name"` // HTML-escaped, like the title
	Slug string `json:"slug"`
}

// mc2 reads The Events Calendar's REST listing. A show on several dates has one event per
// date: its id, not its URL, identifies the concert.
func mc2(page *neturl.URL, body []byte, _ string) ([]listed, []*neturl.URL, error) {
	var p struct {
		Events []struct {
			ID         int64         `json:"id"`
			URL        string        `json:"url"`
			Title      string        `json:"title"`
			StartDate  string        `json:"start_date"` // local wall clock, « 2026-11-19 20:00:00 »
			AllDay     bool          `json:"all_day"`
			Cost       string        `json:"cost"`
			Categories []mc2Category `json:"categories"`
		} `json:"events"`
		Next string `json:"next_rest_url"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, nil, err
	}
	var items []listed
	for _, e := range p.Events {
		has := func(slug string) bool {
			return slices.ContainsFunc(e.Categories, func(c mc2Category) bool { return c.Slug == slug })
		}
		// Comedy and cabaret shows carry « Musique » too; « Théâtre » alone would also drop concerts.
		if e.ID <= 0 || !has("musique") || has("humour") || has("cabaret") {
			continue
		}
		var labels []string // « Musique » tells nothing more
		for _, c := range e.Categories {
			if c.Slug != "musique" {
				labels = append(labels, html.UnescapeString(c.Name))
			}
		}
		l := listed{id: strconv.FormatInt(e.ID, 10), page: e.URL}
		var hms string
		l.date, hms, _ = strings.Cut(e.StartDate, " ")
		if !e.AllDay && len(hms) >= 5 {
			l.time = hms[:5]
		}
		l.artist, l.full = cleanTitle(html.UnescapeString(e.Title))
		l.price = euroGlued.ReplaceAllString(html.UnescapeString(e.Cost), "$1 €")
		l.genre = genre(labels...)
		items = append(items, l)
	}
	var next []*neturl.URL
	if p.Next != "" {
		u, err := page.Parse(p.Next)
		if err != nil {
			return nil, nil, withoutURL(err)
		}
		next = append(next, u)
	}
	return items, next, nil
}

// belleKeep: the music event types of La Belle Électrique (« Concert », « DJ sets », « Soirées
// Electro »…, « Gratuit » for a free concert), not « Formation », « Visite », « Stand-up »…
func belleKeep(kind string) bool {
	kind = strings.ToLower(kind)
	return slices.ContainsFunc([]string{"concert", "dj", "soirée", "festival", "gratuit"}, func(k string) bool {
		return strings.Contains(kind, k)
	})
}

// belle reads La Belle Électrique's month pages, « Suivant » leading to the next.
func belle(page *neturl.URL, body []byte, _ string) ([]listed, []*neturl.URL, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	var items []listed
	for _, card := range all(doc, class("event-card")) {
		kind := text(first(card, class("tag")))
		if !belleKeep(kind) {
			continue
		}
		l := listed{page: link(page, first(card, tag(atom.A)))}
		if u, err := neturl.Parse(l.page); err == nil {
			l.id, _ = strings.CutPrefix(u.Path, "/fr/agenda/")
		}
		l.artist, l.full = cleanTitle(text(first(card, tag(atom.H2, atom.H3))))
		// « Face B Live • Milena », « Skip The Use | Love & Anxiety Tour »: the artist is between.
		if _, after, ok := cutLast(l.artist, " • "); ok {
			l.artist = after
		}
		l.artist, _, _ = strings.Cut(l.artist, " | ")
		// « 19.11.26 / 20h », or « Du 05.02.27 / 20h au 06.02.27 / 1h »: the start.
		when := strings.TrimPrefix(text(first(card, func(n *html.Node) bool {
			return slices.ContainsFunc(classes(n), func(c string) bool { return strings.HasPrefix(c, "date-") })
		})), "Du ")
		when, _, _ = strings.Cut(when, " au ")
		day, hour, _ := strings.Cut(when, " / ")
		if d, err := time.Parse("02.01.06", day); err == nil {
			l.date = d.Format(time.DateOnly)
		}
		l.time = prefill.FrenchTime(hour)
		// « Rock<br>Grande Salle • Concert »: the genre line, then the hall.
		var line strings.Builder
		if h4 := first(card, class("h4")); h4 != nil {
			for c := h4.FirstChild; c != nil && c.DataAtom != atom.Br; c = c.NextSibling {
				line.WriteString(" " + text(c))
			}
		}
		if k := strings.ToLower(kind); k == "concert" || k == "gratuit" {
			kind = ""
		}
		l.genre = genre(line.String(), kind)
		items = append(items, l)
	}
	var next []*neturl.URL
	if a := first(doc, func(n *html.Node) bool { return n.DataAtom == atom.A && text(n) == "Suivant" }); a != nil {
		if u, err := neturl.Parse(link(page, a)); err == nil {
			next = append(next, u)
		}
	}
	return items, next, nil
}

func cutLast(s, sep string) (before, after string, found bool) {
	if i := strings.LastIndex(s, sep); i >= 0 {
		return s[:i], s[i+len(sep):], true
	}
	return s, "", false
}

// lilyade reads L'ilyade's Elementor loop: music items only, info items date (often
// without year), time, then « Hall, » and its town; data-next-page leads to the next page.
func lilyade(page *neturl.URL, body []byte, today string) ([]listed, []*neturl.URL, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	var items []listed
	for _, it := range all(doc, class("e-loop-item")) {
		if !slices.Contains(classes(it), "genre-musique") {
			continue
		}
		title := first(it, tag(atom.H3))
		if title == nil {
			continue
		}
		l := listed{id: classPrefix(it, "post-"), page: link(page, first(title, tag(atom.A))),
			ticket: link(page, first(it, class("elementor-button")))}
		l.artist, l.full = cleanTitle(text(title))
		infos := all(it, class("elementor-icon-list-text"))
		for i, n := range infos {
			s := text(n)
			switch {
			case l.date == "":
				l.date = listingDate(s, today)
			case l.time == "" && l.venue == "":
				l.time = prefill.FrenchTime(s)
			}
			if l.date != "" && l.venue == "" && strings.HasSuffix(s, ",") {
				l.venue = strings.TrimRight(s, ", ")
				if i+1 < len(infos) {
					l.address = text(infos[i+1])
				}
			}
		}
		items = append(items, l)
	}
	// The last page still names a next one: data-page reaches data-max-page there.
	if n := first(doc, func(n *html.Node) bool { return attr(n, "data-next-page") != "" }); n != nil {
		at, err1 := strconv.Atoi(attr(n, "data-page"))
		last, err2 := strconv.Atoi(attr(n, "data-max-page"))
		if u, err := page.Parse(attr(n, "data-next-page")); err == nil && err1 == nil && err2 == nil && at < last {
			return items, []*neturl.URL{u}, nil
		}
	}
	return items, nil, nil
}

// amperage reads L'Ampérage's month blocks (class month-YYYY-MM, which gives the year of
// « MER. 18 NOVEMBRE »): concerts and club nights, not talks.
func amperage(page *neturl.URL, body []byte, _ string) ([]listed, []*neturl.URL, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	var items []listed
	for _, month := range all(doc, func(n *html.Node) bool {
		return slices.Contains(classes(n), "strate") && classPrefix(n, "month-") != ""
	}) {
		year, _, _ := strings.Cut(classPrefix(month, "month-"), "-")
		for _, it := range all(month, func(n *html.Node) bool { return attr(n, "data-event-id") != "" }) {
			kind := strings.ToLower(text(first(it, class("type"))))
			if kind != "concert" && kind != "club" {
				continue
			}
			l := listed{id: attr(it, "data-event-id"), page: link(page, first(it, class("btn-link")))}
			l.artist, l.full = cleanTitle(text(first(it, class("artistes"))))
			if f := strings.Fields(text(first(it, class("date")))); len(f) == 3 {
				l.date = prefill.FrenchDate(f[1] + " " + f[2] + " " + year)
			}
			l.time = prefill.FrenchTime(text(first(it, class("heure")))) // « 19h30 – 23h30 »: the start
			if kind == "concert" {
				kind = ""
			}
			l.genre = genre(text(first(it, class("style"))), kind)
			items = append(items, l)
		}
	}
	return items, nil, nil
}

// laSourceSkip are La Source's tags for shows other than concerts.
var laSourceSkip = []string{"jeune public", "humour", "stand up"}

// laSource reads La Source's programme cards; times are on the event pages only.
func laSource(page *neturl.URL, body []byte, today string) ([]listed, []*neturl.URL, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	var items []listed
	for _, card := range all(doc, class("ls-event-card")) {
		var tags []string
		for _, n := range all(card, class("ls-tag")) {
			tags = append(tags, text(n))
		}
		if slices.ContainsFunc(tags, func(t string) bool { return slices.Contains(laSourceSkip, strings.ToLower(t)) }) {
			continue
		}
		title := first(card, class("ls-event-title"))
		if title == nil {
			continue
		}
		l := listed{page: link(page, first(title, tag(atom.A)))}
		if u, err := neturl.Parse(l.page); err == nil {
			p, _ := strings.CutPrefix(u.Path, "/event/")
			l.id = strings.Trim(p, "/")
		}
		l.artist, l.full = cleanTitle(text(title))
		l.genre = genre(tags...)
		if lines := all(card, class("ls-ev-line")); len(lines) > 0 {
			l.date = listingDate(text(lines[0]), today)
		}
		items = append(items, l)
	}
	return items, nil, nil
}

// retourDeScene reads the promoter's agenda grid: concerts and micro-lives at various halls
// (« Ilyade - Seyssinet-Pariset »), not trainings; times are on the event pages only.
func retourDeScene(page *neturl.URL, body []byte, _ string) ([]listed, []*neturl.URL, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	var items []listed
	for _, it := range all(doc, class("dp-dfg-item")) {
		micro := slices.Contains(classes(it), "category-micro-live")
		if !micro && !slices.Contains(classes(it), "category-concert") {
			continue
		}
		l := listed{id: classPrefix(it, "post-"), page: link(page, first(it, tag(atom.A)))}
		if micro {
			l.genre = "Micro-live"
		}
		l.artist, l.full = cleanTitle(text(first(it, class("entry-title"))))
		if d, err := time.Parse("02/01/2006", text(first(it, class("dp-dfg-cf-date")))); err == nil {
			l.date = d.Format(time.DateOnly)
		}
		l.venue, l.address, _ = strings.Cut(text(first(it, class("dp-dfg-cf-lieu"))), " - ")
		items = append(items, l)
	}
	return items, nil, nil
}
