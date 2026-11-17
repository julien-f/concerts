package prefill

import (
	"encoding/json"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"
)

var paris = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		panic(err) // the tests need real zone data, as the app embeds it
	}
	return loc
}()

// now is before every fixture's concert; Fnac's first validFrom (2025) is past, its second (2027) future.
var now = time.Date(2026, 11, 16, 10, 0, 0, 0, paris)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := fs.ReadFile(os.DirFS("testdata"), name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestParseFixtures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		file, host string
		want       Result
	}{
		// Performer array (blank and repeated names skipped), offset date, no on-sale date, a non-event block beside.
		{"dice.html", "dice.fm", Result{Artist: "Les Hiboux + Nova Test", Date: "2026-11-27", Time: "20:00", Venue: "Salle Exemple", Address: "Paris", Price: "30 €"}},
		// Generic Event, local time without offset, pretty-printed JSON after a WebSite block.
		{"olympia.html", "www.olympiahall.com", Result{Artist: "Jeanne Exemple", Date: "2026-12-08", Time: "20:00", Venue: "L'Exemple", Price: "39,90 €"}},
		// Minutes only, date-only endDate ignored, full PostalAddress (country dropped).
		{"zenith.html", "www.zenith-grenoble.fr", Result{Artist: "Nova Test", Date: "2027-01-24", Time: "20:00", Venue: "Arène Exemple", Address: "12, esplanade Exemple, 38000 GRENOBLE"}},
		// ComedyEvent, EventVenue, milliseconds; past validFrom skipped, nested future one kept; broken block skipped;
		// AggregateOffer without currency (euros), and the nested offer counted in the price range.
		{"fnac.html", "www.fnacspectacles.com", Result{Artist: "Marc Essai", Date: "2027-11-04", Time: "20:00", Venue: "Théâtre Exemple", OnSaleDate: "2027-02-14", Price: "55–64 €"}},
		// No JSON-LD: og:title written with name=.
		{"accor.html", "www.accorarena.com", Result{Artist: "Duo Fictif"}},
		// Top-level array, local time without offset, validFrom null, performer object; description with
		// CR LF, an HTML entity and a soft hyphen.
		{"belle-electrique.html", "billetterie.la-belle-electrique.com", Result{Artist: "Collectif Exemple : Soirée Test", Date: "2027-04-23", Time: "20:00",
			Venue: "La Salle Électrique Exemple", Address: "12, esplanade Exemple, 38000 GRENOBLE", Price: "26–29 €",
			Description: "Une soirée, cinq figures du collectif.\nL'une d’elles chante."}},
		// No Event in JSON-LD: visible info list; the svg, noscript and script dates, and the related event, are not taken.
		{"lilyade.html", "www.lilyade.fr", Result{Artist: "Les Hiboux + Nova Test", Date: "2026-12-05", Time: "20:00", Venue: "L'Exemple", Address: "Villefictive"}},
		{"lilyade.html", "example.com", Result{Artist: "Les Hiboux + Nova Test"}},
		// No Event in JSON-LD: « Lieu » and « Horaire » blocks; the all-caps artist is kept as served;
		// price and artist blocks between « Billetterie » and « Lieu », without icon glyph or « Suivre ».
		{"retourdescene.html", "www.retourdescene.net", Result{Artist: "LES HIBOUX + NOVA TEST", Date: "2026-12-05", Time: "20:00",
			Venue: "L’Exemple", Address: "1 Rue de l’Exemple, 38000 Villefictive", Price: "20–23 €",
			Description: "LES HIBOUX\nUne musique électronique hybride, le samedi 12 décembre 2026 en tournée.\n\nNOVA TEST\nUn duo électro venu\nde Grenoble."}},
	}
	for _, tt := range tests {
		t.Run(tt.file+"@"+tt.host, func(t *testing.T) {
			t.Parallel()
			if got := parse(strings.NewReader(fixture(t, tt.file)), tt.host, now); got != tt.want {
				t.Fatalf("parse = %+v\nwant    %+v", got, tt.want)
			}
		})
	}
}

// TestProse: a JSON-LD description comes out as text the form accepts, words kept apart.
func TestProse(t *testing.T) {
	t.Parallel()
	tests := []struct{ in, want string }{
		{"Un&amp;deux\r\ntrois", "Un&deux\ntrois"},
		{"Ex\u00ademple\u200b", "Exemple"},
		{"Un\tduo\u00a0ici", "Un duo ici"},
		{"Un duo\u2800ici", "Un duo ici"},
	}
	for _, tt := range tests {
		if got := prose(tt.in); got != tt.want {
			t.Errorf("prose(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestUnglue: sites that strip markup from JSON-LD descriptions glue the words around it.
func TestUnglue(t *testing.T) {
	t.Parallel()
	tests := []struct{ in, want string }{
		// La Belle Électrique, Sniper + Kenz: an artist heading glued to its paragraph, and to the previous one.
		{"SNIPERGroupe fondateur du rap", "SNIPER\nGroupe fondateur du rap"},
		{"et l’énergie.KENZOriginaire de Grenoble", "et l’énergie.\nKENZ\nOriginaire de Grenoble"},
		{"de détermination.En 2024, elle", "de détermination. En 2024, elle"},
		{"prévue pour 2027.KENZ revient sur scène", "prévue pour 2027. KENZ revient sur scène"},
		{"Fontaines D.C.Le groupe", "Fontaines D.C. Le groupe"},
		{"Quoi?Rien!Enfin", "Quoi? Rien! Enfin"},
		// Left alone: initials, acronyms, names with an inner capital, spaced text.
		{"U.S.A. et R.E.M.", "U.S.A. et R.E.M."},
		{"Paul McCartney et LeBron", "Paul McCartney et LeBron"},
		{"Le DJ Boudj. Puis AC/DC", "Le DJ Boudj. Puis AC/DC"},
	}
	for _, tt := range tests {
		if got := unglue(tt.in); got != tt.want {
			t.Errorf("unglue(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPrice(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, offers, want string }{
		{"one tier", `{"price":32,"priceCurrency":"EUR"}`, "32 €"},
		{"equal tiers", `[{"price":32},{"price":"32.00"}]`, "32 €"},
		{"range, cents with comma or dot", `[{"price":"29,50"},{"price":"32.5"}]`, "29,50–32,50 €"},
		{"aggregate low and high", `{"@type":"AggregateOffer","lowPrice":20,"highPrice":45}`, "20–45 €"},
		{"other currency skipped", `[{"price":10,"priceCurrency":"USD"},{"price":25}]`, "25 €"},
		{"currency inherited", `{"priceCurrency":"GBP","lowPrice":10,"offers":[{"price":12}]}`, ""},
		{"free and junk amounts skipped", `[{"price":0},{"price":"gratuit"},{"price":12000},{"price":-5},{"price":18}]`, "18 €"},
		{"none", `{"url":"https://x.example"}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var v any
			if err := json.Unmarshal([]byte(tt.offers), &v); err != nil {
				t.Fatal(err)
			}
			if got := price(v); got != tt.want {
				t.Errorf("price(%s) = %q, want %q", tt.offers, got, tt.want)
			}
		})
	}
}

func TestParseSiteRules(t *testing.T) {
	t.Parallel()
	lilyade := func(t *testing.T, old, repl string) string {
		return strings.ReplaceAll(fixture(t, "lilyade.html"), old, repl)
	}
	const ld = `<script type="application/ld+json">{"@type":"MusicEvent","name":"Ld","startDate":"2027-01-09T21:00"}</script>`
	const span = `<span class="elementor-post-info__item--type-custom">`
	tests := []struct {
		name, host string
		html       func(t *testing.T) string
		want       Result
	}{
		{"main date missing: the related event's is not taken", "www.lilyade.fr",
			func(t *testing.T) string { return lilyade(t, "Samedi 5 décembre 2026", "Bientôt") },
			Result{Artist: "Les Hiboux + Nova Test"}},
		{"time missing: venue not shifted into it", "www.lilyade.fr",
			func(t *testing.T) string { return lilyade(t, "20h00", "Horaire à venir") },
			Result{Artist: "Les Hiboux + Nova Test", Date: "2026-12-05"}},
		{"a JSON-LD date wins: no mixing of sources", "www.lilyade.fr",
			func(*testing.T) string {
				return ld + span + "Samedi 5 décembre 2026</span>" + span + "20h00</span>" + span + "X</span>" + span + "Y</span>"
			},
			Result{Artist: "Ld", Date: "2027-01-09", Time: "21:00"}},
		{"JSON-LD without date: the rule fills the empty fields only", "lilyade.fr",
			func(*testing.T) string {
				return `<script type="application/ld+json">{"@type":"MusicEvent","name":"Ld","location":"Salle Ld"}</script>` +
					span + "Samedi 5 décembre 2026</span>" + span + "20h00</span>" + span + "X</span>" + span + "Y</span>"
			},
			Result{Artist: "Ld", Date: "2026-12-05", Time: "20:00", Venue: "Salle Ld", Address: "Y"}},
		{"city missing: the next block's date is not taken as the address", "www.lilyade.fr",
			func(t *testing.T) string { return lilyade(t, "Villefictive", "") },
			Result{Artist: "Les Hiboux + Nova Test", Date: "2026-12-05", Time: "20:00", Venue: "L'Exemple"}},
		{"venue's trailing separator trimmed", "www.lilyade.fr",
			func(*testing.T) string {
				return span + "Samedi 5 décembre 2026</span>" + span + "20h00</span>" + span + "L'Exemple,</span>" + span + "Ville</span>"
			},
			Result{Date: "2026-12-05", Time: "20:00", Venue: "L'Exemple", Address: "Ville"}},
		{"a self-closing non-void tag opens an element (HTML ignores the slash)", "www.lilyade.fr",
			func(*testing.T) string { return span + `<i class="icon"/>Samedi 5 décembre 2026</span>` },
			Result{}},
		{"class cleared by an end tag", "www.lilyade.fr",
			func(*testing.T) string { return "<p>" + span + "x</span>Samedi 5 décembre 2026</p>" },
			Result{}},
		{"class cleared by an empty script's end tag", "www.lilyade.fr",
			func(*testing.T) string { return span + "x<script></script>Samedi 5 décembre 2026</span>" },
			Result{}},
		{"class kept across <br>", "www.lilyade.fr",
			func(*testing.T) string { return span + "Bientôt<br>Samedi 5 décembre 2026</span>" },
			Result{Date: "2026-12-05"}},
		{"Lieu without street, 1er", "www.retourdescene.net",
			func(*testing.T) string {
				return `<h3>Lieu</h3><p>Salle<br>Ville, Isère 38000 France</p><h3>Horaire</h3><p>1er mars 2027<br>21h</p>`
			},
			Result{Date: "2027-03-01", Time: "21:00", Venue: "Salle", Address: "38000 Ville"}},
		{"Lieu without postcode: no address", "www.retourdescene.net",
			func(*testing.T) string {
				return `<h3>Lieu</h3><p>Salle<br>Rue X<br>Lyon</p><h3>Horaire</h3><p>Le soir</p>`
			},
			Result{Venue: "Salle"}},
		{"no price: the description is still read, a free text price kept in it", "www.retourdescene.net",
			func(*testing.T) string {
				return `<a>Billetterie</a><p>Prix libre</p><h1>X</h1><p>Un  <a>Suivre</a>  duo.</p><h3>Lieu</h3>`
			},
			Result{Description: "Prix libre\n\nX\nUn duo."}},
		{"invisible characters dropped from the description, as from JSON-LD's", "www.retourdescene.net",
			func(*testing.T) string { return "<a>Billetterie</a><p>Ex\u00ademple\u200b\tici</p><h3>Lieu</h3>" },
			Result{Description: "Exemple ici"}},
		{"price with spaces and cents", "www.retourdescene.net",
			func(*testing.T) string { return "<a>Billetterie</a><p>12,50 € / 15 €</p><h3>Lieu</h3>" },
			Result{Price: "12,50–15 €"}},
		{"rule of another host not applied", "www.retourdescene.net",
			func(t *testing.T) string { return fixture(t, "lilyade.html") },
			Result{Artist: "Les Hiboux + Nova Test"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := parse(strings.NewReader(tt.html(t)), tt.host, now); got != tt.want {
				t.Fatalf("parse = %+v\nwant    %+v", got, tt.want)
			}
		})
	}
}

func TestSiteRule(t *testing.T) {
	t.Parallel()
	for host, ok := range map[string]bool{
		"www.lilyade.fr": true, "lilyade.fr": true, "WWW.LILYADE.FR": true, "www.lilyade.fr.": true, "www.retourdescene.net": true,
		"lilyade.fr.evil.com": false, "evil-lilyade.fr": false, "www.www.lilyade.fr": false, "lilyade.fr:443": false, "": false,
	} {
		if got := siteRule(host) != nil; got != ok {
			t.Errorf("siteRule(%q) found = %v, want %v", host, got, ok)
		}
	}
}

func TestFrenchDate(t *testing.T) {
	t.Parallel()
	for s, want := range map[string]string{
		"Samedi 21 novembre 2026": "2026-11-21", "21 novembre 2026": "2026-11-21", "1er mars 2027": "2027-03-01",
		"lundi 1er FÉVRIER 2027": "2027-02-01", "5 decembre 2026": "2026-12-05", "15 aout 2027": "2027-08-15",
		"Samedi 21 novembre 2026": "2026-11-21", " jeudi 3 juin 2027 ": "2027-06-03",
		"31 février 2027": "", "21 novembre": "", "le 21 novembre 2026": "", "Samedi 21 novembre 2026 – 20h": "",
		"21 brumaire 2026": "", "samedi 2026": "", "": "",
	} {
		if got := frenchDate(s); got != want {
			t.Errorf("frenchDate(%q) = %q, want %q", s, got, want)
		}
	}
}

func TestFrenchTime(t *testing.T) {
	t.Parallel()
	for s, want := range map[string]string{
		"20h00": "20:00", "20h": "20:00", "20 h 30": "20:30", "9h": "09:00", "20h > 01h (portes 20h, début 20h30)": "20:00",
		"portes 19H30": "19:30", "20 h": "20:00", "20 h 30": "20:30", "24h": "", "120h": "", "20h75": "", "20:00": "", "": "",
	} {
		if got := frenchTime(s); got != want {
			t.Errorf("frenchTime(%q) = %q, want %q", s, got, want)
		}
	}
}

func TestParseEdgeCases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, html string
		want       Result
	}{
		{"UTC offset converted to Paris", `<script type="application/ld+json">{"@type":"MusicEvent","name":"A","startDate":"2027-07-04T19:00:00Z"}</script>`,
			Result{Artist: "A", Date: "2027-07-04", Time: "21:00"}},
		{"offset without seconds, as ISO 8601 allows", `<script type="application/ld+json">{"@type":"MusicEvent","name":"A","startDate":"2027-07-04T19:00+02:00"}</script>`,
			Result{Artist: "A", Date: "2027-07-04", Time: "19:00"}},
		{"offset without a colon, as ISO 8601's basic format writes it", `<script type="application/ld+json">{"@type":"MusicEvent","name":"A","startDate":"2027-07-04T19:00:00+0200"}</script>`,
			Result{Artist: "A", Date: "2027-07-04", Time: "19:00"}},
		{"invisible characters dropped from one-line fields", `<script type="application/ld+json">{"@type":"MusicEvent","name":"Fon\u00adtaines","location":{"name":"Salle\u200b\tX"}}</script>`,
			Result{Artist: "Fontaines", Venue: "Salle X"}},
		{"date only, no time", `<script type="application/ld+json">{"@type":"Festival","name":"F","startDate":"2027-07-04","location":"Plein air"}</script>`,
			Result{Artist: "F", Date: "2027-07-04", Venue: "Plein air"}},
		{"MusicEvent preferred, in @graph", `<script type="APPLICATION/LD+JSON" id="x">{"@graph":[{"@type":"Event","name":"Other"},{"@type":["Thing","MusicEvent"],"name":"Music"}]}</script>`,
			Result{Artist: "Music"}},
		{"ItemList of events", `<script type="application/ld+json">{"@type":"ItemList","itemListElement":[{"item":{"@type":"MusicEvent","name":"Listed"}}]}</script>`,
			Result{Artist: "Listed"}},
		{"title fallback, cut at the first separator", `<title>Artiste &amp; Co - Salle | Site</title>`, Result{Artist: "Artiste & Co"}},
		{"an icon's svg title is not the page's", `<svg><title>Icône</title></svg><title>Artiste - Salle</title>`, Result{Artist: "Artiste"}},
		{"og:title 'null' skipped", `<meta property="og:title" content="null"><meta name="twitter:title" content="Tw @ Venue">`, Result{Artist: "Tw"}},
		{"address as a string", `<script type="application/ld+json">{"@type":"MusicEvent","name":"A","location":{"name":"S","address":" 1 rue X, Lyon "}}</script>`,
			Result{Artist: "A", Venue: "S", Address: "1 rue X, Lyon"}},
		{"address array, no venue name", `<script type="application/ld+json">{"@type":"MusicEvent","name":"A","location":{"address":[{"streetAddress":"1 rue X","postalCode":"69001"}]}}</script>`,
			Result{Artist: "A", Address: "1 rue X, 69001"}},
		{"city not repeated when the street holds it", `<script type="application/ld+json">{"@type":"MusicEvent","name":"A","location":{"address":{"streetAddress":"1 rue X, 75001 Paris","postalCode":"75001","addressLocality":"PARIS"}}}</script>`,
			Result{Artist: "A", Address: "1 rue X, 75001 Paris"}},
		{"address equal to the venue dropped", `<script type="application/ld+json">{"@type":"MusicEvent","name":"A","location":{"name":"S","address":"S"}}</script>`,
			Result{Artist: "A", Venue: "S"}},
		{"garbage date ignored", `<script type="application/ld+json">{"@type":"MusicEvent","name":"A","startDate":"bientôt"}</script>`, Result{Artist: "A"}},
		{"nothing", `<p>Just a moment...</p>`, Result{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := parse(strings.NewReader(tt.html), "", now); got != tt.want {
				t.Fatalf("parse = %+v, want %+v", got, tt.want)
			}
		})
	}
}
