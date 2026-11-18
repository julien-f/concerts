// Package validate cleans and checks a concert's fields, whether typed in the form or imported from a venue.
package validate

import (
	"fmt"
	"net/url"
	"strings"
	"time"
	_ "time/tzdata" // Europe/Paris must not depend on the host's zoneinfo
	"unicode/utf8"

	"concerts/internal/prefill"
	"concerts/internal/store"
)

// Paris is the one time zone of the app (ADR 0005); time/tzdata guarantees it loads.
var Paris = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		panic(err)
	}
	return loc
}()

// Today is now's date in Paris, comparable with concert dates: a concert is past from the next day.
func Today(now time.Time) string { return now.In(Paris).Format(time.DateOnly) }

const (
	MaxTextRunes        = 100
	MaxURLBytes         = 2000
	MaxPriceRunes       = 30
	MaxDescriptionRunes = 2000
)

// Concert cleans c's editable fields and validates them; errs is keyed by form field name.
func Concert(c store.Concert) (store.Concert, map[string]string) {
	errs := map[string]string{}
	var ok bool
	if c.Artist, ok = Text(c.Artist, MaxTextRunes); !ok {
		errs["artist"] = fmt.Sprintf("Indique l'artiste (%d caractères au plus).", MaxTextRunes)
	}
	if !Date(c.Date) {
		errs["date"] = "Indique une date valide."
	}
	if c.Time != "" {
		// Stored as HH:MM so text order is time order: "9:30" would sort after "20:00".
		if t, err := time.Parse("15:04", c.Time); err != nil {
			errs["time"] = "Heure invalide (ex. 20:30)."
		} else {
			c.Time = t.Format("15:04")
		}
	}
	if c.Venue, ok = Text(c.Venue, MaxTextRunes); !ok {
		errs["venue"] = fmt.Sprintf("Indique la salle (%d caractères au plus).", MaxTextRunes)
	}
	if strings.TrimSpace(c.Address) == "" {
		c.Address = ""
	} else if c.Address, ok = Text(c.Address, MaxTextRunes); !ok {
		errs["address"] = fmt.Sprintf("Adresse invalide (%d caractères au plus).", MaxTextRunes)
	}
	if c.TicketURL = strings.TrimSpace(c.TicketURL); c.TicketURL != "" && !TicketURL(c.TicketURL) {
		errs["ticket_url"] = "Lien invalide : il doit commencer par http:// ou https://."
	}
	if c.OnSaleDate != "" && !Date(c.OnSaleDate) {
		errs["on_sale_date"] = "Date de mise en vente invalide."
	}
	if strings.TrimSpace(c.Price) == "" {
		c.Price = ""
	} else if c.Price, ok = Text(c.Price, MaxPriceRunes); !ok {
		errs["price"] = fmt.Sprintf("Prix invalide (%d caractères au plus).", MaxPriceRunes)
	}
	if strings.TrimSpace(c.Description) == "" {
		c.Description = ""
	} else if c.Description, ok = Paragraphs(c.Description, MaxDescriptionRunes); !ok {
		errs["description"] = fmt.Sprintf("Description invalide (%d caractères au plus).", MaxDescriptionRunes)
	}
	return c, errs
}

// Date reports whether s is a YYYY-MM-DD date of this century.
func Date(s string) bool {
	d, err := time.Parse(time.DateOnly, s)
	return err == nil && d.Year() >= 2000 && d.Year() < 2100
}

// TicketURL keeps javascript:, data: and look-alike links (« fnac.com@evil.example », non-ASCII hosts) off pages.
func TicketURL(s string) bool {
	if len(s) > MaxURLBytes || !utf8.ValidString(s) || strings.ContainsFunc(s, func(r rune) bool { return r <= ' ' || r == 0x7f }) {
		return false
	}
	u, err := url.Parse(s)
	return err == nil && HTTPURL(u) && !strings.ContainsFunc(u.Host, func(r rune) bool { return r > '~' })
}

// HTTPURL: absolute http(s), a host name, no user part; the rule base URLs and ticket links share.
func HTTPURL(u *url.URL) bool {
	return (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil
}

// Text trims v and collapses whitespace runs; it reports false unless the
// result is valid UTF-8 of 1 to maxRunes runes without prefill.Invisible characters.
// The result is returned either way, so a form sent back keeps what was typed.
func Text(v string, maxRunes int) (string, bool) {
	return valid(strings.Join(strings.Fields(v), " "), maxRunes)
}

// Paragraphs is Text for multi-line text: it keeps line feeds, folds runs of
// blank lines into one and trims the whole.
func Paragraphs(v string, maxRunes int) (string, bool) {
	return valid(FoldParagraphs(v), maxRunes)
}

// FoldParagraphs collapses whitespace within each line (dropping CR) and keeps one blank line at most between lines.
func FoldParagraphs(v string) string {
	var lines []string
	for l := range strings.SplitSeq(v, "\n") {
		l = strings.Join(strings.Fields(l), " ")
		if l != "" || (len(lines) > 0 && lines[len(lines)-1] != "") {
			lines = append(lines, l)
		}
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

// valid returns v and whether it is valid UTF-8 of 1 to maxRunes runes without
// prefill.Invisible characters other than line feeds.
func valid(v string, maxRunes int) (string, bool) {
	return v, utf8.ValidString(v) && v != "" && utf8.RuneCountInString(v) <= maxRunes &&
		!strings.ContainsFunc(v, func(r rune) bool { return r != '\n' && prefill.Invisible(r) })
}
