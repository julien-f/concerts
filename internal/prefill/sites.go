package prefill

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// siteRules read a Result from the visible text of pages without a schema.org Event, by host without "www.".
var siteRules = map[string]func([]text) Result{
	"lasource-fontaine.fr": laSource,
	"lilyade.fr":           lilyade,
	"retourdescene.net":    retourDeScene,
}

// laSource reads « Date de début » (« 12 mai 2027 20 h 30 min »); the hall is the venue, its footer
// address the address. Its « Salle » item is the room (« Grande Salle »).
func laSource(texts []text) Result {
	i := slices.IndexFunc(texts, func(t text) bool { return strings.EqualFold(t.s, "Date de début") })
	if i < 0 || i+1 == len(texts) {
		return Result{}
	}
	f := strings.Fields(texts[i+1].s)
	if len(f) < 3 {
		return Result{}
	}
	res := Result{Date: FrenchDate(strings.Join(f[:3], " ")), Time: FrenchTime(strings.Join(f[3:], " ")), Venue: "La Source"}
	if res.Date == "" {
		return Result{}
	}
	for _, t := range texts {
		if slices.Contains(strings.Fields(t.class), "ls-ico-pin") {
			res.Address = t.s
			if at := postcode.FindStringIndex(t.s); at != nil && at[0] > 0 { // « 38 avenue Lénine 38600 Fontaine »
				res.Address = strings.TrimRight(t.s[:at[0]], ", ") + ", " + t.s[at[0]:]
			}
			break
		}
	}
	return res
}

// siteRule returns the rule for an exact host, case and trailing dot aside.
func siteRule(host string) func([]text) Result {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	return siteRules[strings.TrimPrefix(host, "www.")]
}

// lilyade reads the info items date, time, venue, city: the footer address is the hall's, not the event's.
func lilyade(texts []text) Result {
	var items []string
	for _, t := range texts {
		if t.heading && strings.EqualFold(t.s, "À découvrir également") {
			break // other events follow, with their own dates
		}
		if slices.Contains(strings.Fields(t.class), "elementor-post-info__item--type-custom") {
			items = append(items, t.s)
		}
	}
	isDate := func(s string) bool { return FrenchDate(s) != "" }
	i := slices.IndexFunc(items, isDate)
	if i < 0 {
		return Result{}
	}
	res := Result{Date: FrenchDate(items[i])}
	block := items[i+1:]
	if j := slices.IndexFunc(block, isDate); j >= 0 {
		block = block[:j] // the next date starts the desktop copy of the same items
	}
	if len(block) > 0 {
		res.Time = FrenchTime(block[0])
	}
	if res.Time != "" && len(block) > 1 {
		res.Venue = strings.TrimRight(block[1], ", ") // related events write « L'ilyade, »
	}
	if res.Time != "" && len(block) > 2 {
		res.Address = block[2]
	}
	return res
}

var (
	postcode = regexp.MustCompile(`\b\d{5}\b`)
	// rdsCity is the city line of a Retour de Scène venue: « Seyssinet-Pariset, Isère 38170 France ».
	rdsCity = regexp.MustCompile(`^(.+?), .+ (\d{5}) France$`)
)

// retourDeScene reads the « Lieu » (venue, street, city line) and « Horaire » (date, then times) blocks.
func retourDeScene(texts []text) Result {
	var res Result
	if lieu := section(texts, "Lieu"); len(lieu) > 0 {
		res.Venue = lieu[0]
		for i := 1; i < len(lieu); i++ {
			if !postcode.MatchString(lieu[i]) {
				continue
			}
			res.Address = lieu[i]
			if m := rdsCity.FindStringSubmatch(lieu[i]); m != nil {
				res.Address = m[2] + " " + m[1]
			}
			if i == 2 { // one line between venue and city: the street
				res.Address = strings.TrimRight(lieu[1], ", ") + ", " + res.Address // some streets end with « , »
			}
			break
		}
	}
	horaire := section(texts, "Horaire")
	for i, s := range horaire {
		if res.Date = FrenchDate(s); res.Date != "" {
			if i+1 < len(horaire) {
				res.Time = FrenchTime(horaire[i+1]) // « 20h > 01h (portes 20h, début de concert 20h30) »: the first
			}
			break
		}
	}
	res.Price, res.Description = retourDeSceneAbout(texts)
	return res
}

var (
	// rdsPrice is a text of euro amounts only: « 20€/23€ », « 12 € ».
	rdsPrice   = regexp.MustCompile(`^\d+(?:[.,]\d\d?)?[ \x{a0}\x{202f}]?€(?:[ \x{a0}\x{202f}]?/[ \x{a0}\x{202f}]?\d+(?:[.,]\d\d?)?[ \x{a0}\x{202f}]?€)*$`)
	euroAmount = regexp.MustCompile(`\d+(?:[.,]\d\d?)?`)
)

// retourDeSceneAbout reads what follows the « Billetterie » button up to « Lieu »: the price,
// then the artist blocks (heading, paragraphs), without « Suivre » links or icon glyphs.
func retourDeSceneAbout(texts []text) (price, description string) {
	start := slices.IndexFunc(texts, func(t text) bool { return !t.heading && strings.EqualFold(t.s, "Billetterie") })
	if start < 0 {
		return "", ""
	}
	var b strings.Builder
	newLine := false // a skipped text's block break still separates its neighbours
	for _, t := range texts[start+1:] {
		if t.heading && strings.EqualFold(t.s, "Lieu") {
			break
		}
		newLine = newLine || t.block
		if t.s == "Suivre" || !strings.ContainsFunc(t.s, func(r rune) bool { return unicode.In(r, unicode.L, unicode.N) }) {
			continue
		}
		if b.Len() == 0 && price == "" && rdsPrice.MatchString(t.s) {
			var lo, hi float64
			for _, s := range euroAmount.FindAllString(t.s, -1) {
				lo, hi = widen(lo, hi, amount(s))
			}
			price, newLine = formatPrice(lo, hi), false
			continue
		}
		switch {
		case b.Len() == 0:
		case t.heading:
			b.WriteString("\n\n")
		case newLine:
			b.WriteString("\n")
		default:
			b.WriteString(" ")
		}
		b.WriteString(t.s)
		newLine = false
	}
	return price, plain(b.String())
}

// section returns the texts between the heading named name and the next heading.
func section(texts []text, name string) []string {
	var out []string
	in := false
	for _, t := range texts {
		if t.heading {
			if in {
				break
			}
			in = strings.EqualFold(t.s, name)
		} else if in {
			out = append(out, t.s)
		}
	}
	return out
}

var (
	frenchDays   = []string{"lundi", "mardi", "mercredi", "jeudi", "vendredi", "samedi", "dimanche"}
	frenchMonths = map[string]time.Month{
		"janvier": 1, "février": 2, "fevrier": 2, "mars": 3, "avril": 4, "mai": 5, "juin": 6, "juillet": 7,
		"août": 8, "aout": 8, "septembre": 9, "octobre": 10, "novembre": 11, "décembre": 12, "decembre": 12,
	}
)

// FrenchDate parses a whole text « [weekday] D[er] month YYYY » into YYYY-MM-DD, "" otherwise.
func FrenchDate(s string) string {
	f := strings.Fields(strings.ToLower(s))
	if len(f) == 4 && slices.Contains(frenchDays, f[0]) {
		f = f[1:]
	}
	if len(f) != 3 || len(f[2]) != 4 {
		return ""
	}
	month, ok := frenchMonths[f[1]]
	day, errDay := strconv.Atoi(strings.TrimSuffix(f[0], "er"))
	year, errYear := strconv.Atoi(f[2])
	if !ok || errDay != nil || errYear != nil {
		return ""
	}
	t := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	if t.Day() != day || t.Month() != month { // « 31 février » rolls over
		return ""
	}
	return t.Format(time.DateOnly)
}

// RE2's \s is ASCII only: French typography puts U+00A0 or U+202F around « h ».
var frenchTimeRE = regexp.MustCompile(`(?i)\b([01]?\d|2[0-3])[ \x{a0}\x{202f}]?h[ \x{a0}\x{202f}]?([0-5]\d)?\b`)

// FrenchTime finds the first « 20h », « 20h30 » or « 20 h 30 » in s, as HH:MM; "" if none.
func FrenchTime(s string) string {
	m := frenchTimeRE.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	h, _ := strconv.Atoi(m[1])
	return fmt.Sprintf("%02d:%s", h, cmp.Or(m[2], "00"))
}
