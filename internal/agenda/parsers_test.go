package agenda

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"concerts/internal/store"
)

// fixtureToday is the day the fixtures were downloaded.
const fixtureToday = "2026-11-18"

func venue(t *testing.T, key string) *Venue {
	t.Helper()
	i := slices.IndexFunc(Venues, func(v Venue) bool { return v.Key == key })
	if i < 0 {
		t.Fatalf("no venue %q", key)
	}
	return &Venues[i]
}

// row shows an event on one line, whitespace runs collapsed: id | artist | date time | venue, address | link | price.
func row(e store.Event) string {
	return strings.Join(strings.Fields(fmt.Sprintf("%s | %s | %s %s | %s, %s | %s | %s",
		e.SourceID, e.Artist, e.Date, e.Time, e.Venue, e.Address, e.TicketURL, e.Price)), " ")
}

// parseFixture parses testdata/file as v's listing page at pageURL, then cleans the events
// as Sync does; an event the form would refuse shows as « invalid ».
func parseFixture(t *testing.T, v *Venue, file, pageURL string) (rows, next []string) {
	t.Helper()
	body, err := os.ReadFile("testdata/" + file)
	if err != nil {
		t.Fatal(err)
	}
	items, nextURLs, err := v.parse(mustURL(t, pageURL), body, fixtureToday)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range items {
		e, ok := v.event(l)
		if !ok {
			rows = append(rows, "invalid: "+l.id)
			continue
		}
		rows = append(rows, row(e))
	}
	for _, u := range nextURLs {
		next = append(next, u.String())
	}
	return rows, next
}

// TestParsers: the fixtures are listings trimmed to a few items, keeping the cases each venue
// shows (filtered categories, status words, series names, dates without year or ranges).
func TestParsers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		venue, file, page string
		want, next        []string
	}{
		{
			// « L'Ange du foyer » (théâtre) left out; La Bouche: one show, one id per date.
			venue: "mc2", file: "mc2.json", page: "https://www.mc2grenoble.fr/wp-json/tribe/events/v1/events?categories=musique&per_page=50",
			want: []string{
				"61600 | Raphaël – Karaoké | 2026-11-19 20:00 | MC2, | https://www.mc2grenoble.fr/spectacle/raphael-cyril-teste-karaoke/ | 42 €",
				"61424 | Último helecho | 2026-11-19 20:00 | MC2, | https://www.mc2grenoble.fr/spectacle/ultimo-helecho/ | 34 €",
				"61602 | Jouer la lumière | 2026-12-06 11:00 | MC2, | https://www.mc2grenoble.fr/spectacle/jouer-la-lumiere/ | 13 €",
				"10026968 | La Bouche | 2026-12-17 20:00 | MC2, | https://www.mc2grenoble.fr/spectacle/la-bouche/2026-12-17/ | 30 €",
				"10026969 | La Bouche | 2026-12-18 20:00 | MC2, | https://www.mc2grenoble.fr/spectacle/la-bouche/2026-12-18/ | 30 €",
				"61604 | Symphonie africaine | 2026-12-19 20:00 | MC2, | https://www.mc2grenoble.fr/spectacle/symphonie-africaine/ | 45 €",
			},
		},
		{
			// Visit and training left out; « COMPLET • », « REPORTÉ • » and series names stripped.
			venue: "belle-electrique", file: "belle-electrique.html", page: "https://www.la-belle-electrique.com/fr/programmation",
			want: []string{
				"the-inspector-cluzo | The Inspector Cluzo + Grant Haua | 2026-11-18 20:00 | La Belle Électrique, | https://www.la-belle-electrique.com/fr/agenda/the-inspector-cluzo |",
				"sniper | Sniper + Kenz | 2026-11-19 20:00 | La Belle Électrique, | https://www.la-belle-electrique.com/fr/agenda/sniper | Complet",
				"face-b-live-milena | Milena | 2026-11-19 20:30 | La Belle Électrique, | https://www.la-belle-electrique.com/fr/agenda/face-b-live-milena |",
				"les-femmes-sen-melent-jasmine-not-jafar-op16-goupile-2026 | Jasmine Not Jafar + Goupile + OP.16 | 2026-11-21 20:00 | La Belle Électrique, | https://www.la-belle-electrique.com/fr/agenda/les-femmes-sen-melent-jasmine-not-jafar-op16-goupile-2026 |",
				"aperomix-non-binary-beats | Non-Binary Beats + Pat'Soucis | 2026-11-24 20:00 | La Belle Électrique, | https://www.la-belle-electrique.com/fr/agenda/aperomix-non-binary-beats |",
				"face-b-festival-dolce-cinema | Soirée d'ouverture - Festival Dolce Cinema | 2026-11-26 20:30 | La Belle Électrique, | https://www.la-belle-electrique.com/fr/agenda/face-b-festival-dolce-cinema |",
				"kadavar | Kadavar + Scott Hepple and the Sun Band | 2026-12-01 20:00 | La Belle Électrique, | https://www.la-belle-electrique.com/fr/agenda/kadavar |",
			},
			next: []string{"https://www.la-belle-electrique.com/fr/programmation?from=2027-02"},
		},
		{
			// Ranges « Du … au … » start on their first day; a tour name after « | » dropped.
			// Its « Suivant » was edited to point back to the first page.
			venue: "belle-electrique", file: "belle-electrique-2.html", page: "https://www.la-belle-electrique.com/fr/programmation?from=2027-02",
			want: []string{
				"acid-arab-live | Acid Arab live + Kasbah + Kbira | 2027-02-05 20:00 | La Belle Électrique, | https://www.la-belle-electrique.com/fr/agenda/acid-arab-live |",
				"eczodia-nkn-rouge | ECZODIA + NKN + ROÜGE | 2027-02-20 23:55 | La Belle Électrique, | https://www.la-belle-electrique.com/fr/agenda/eczodia-nkn-rouge |",
				"skip-the-use | Skip The Use + SUPREME NIGHTCLUB | 2027-03-13 20:00 | La Belle Électrique, | https://www.la-belle-electrique.com/fr/agenda/skip-the-use | Complet",
			},
			next: []string{"https://www.la-belle-electrique.com/fr/programmation"},
		},
		{
			// Music only; dates without year (Eloïz's edited to yesterday: a past item still
			// listed, which Sync skips); another hall; ticket links on ticketing sites.
			venue: "lilyade", file: "lilyade.html", page: "https://www.lilyade.fr/programmation/",
			want: []string{
				"6194 | Rakoon + Sumac Dub + Bladi Sound System | 2026-11-21 20:00 | L'ilyade, Seyssinet-Pariset | https://www.retourdescene.net/agenda/#billetterie |",
				"6029 | Eloïz | 2026-11-17 20:30 | L'ilyade, Seyssinet-Pariset | https://www.rpo.net/spectacles/eloiz |",
				"6228 | 60e Déferlante / One Shot Festival | 2026-12-05 20:00 | L'ilyade, Seyssinet-Pariset | https://www.helloasso.com/associations/music-5/evenements/60e-deferlants |",
				"6583 | Joey Gluten + Krav Boca + Tedaak + Ponderale | 2026-12-18 20:00 | L'ilyade, Seyssinet-Pariset | https://www.billetweb.fr/joey-gluten-krav-boca-tedeak-ponderale |",
				"6134 | Pop Corn | 2027-01-21 19:30 | L'ilyade, Seyssinet-Pariset | https://billetterie-lilyade.mapado.com/event/740002-big-ukulele-syndicate-pop-corn |",
				"6175 | Balade Musicale | 2027-05-23 10:30 | Centre de Loisirs Jean-Moulin, Seyssinet-Pariset | https://billetterie-lilyade.mapado.com/event/742347-balade-musicale-verdee |",
				"5919 | Bernard Minet Metal Band | 2027-05-29 19:30 | L'ilyade, Seyssinet-Pariset | https://my.weezevent.com/bernard-minet-metal-band-grenoble-lilyade-seyssinet-pariset |",
			},
			next: []string{"https://www.lilyade.fr/programmation/2/"},
		},
		{
			// The talk left out; the year from the month block; « 00h00 – 06h00 » starts at midnight.
			venue: "amperage", file: "amperage.html", page: "https://www.amperage.fr/la-programmation/",
			want: []string{
				"930 | Mon Corps Sait ! Du corset au corps qui sait. | 2026-11-18 19:30 | L'Ampérage, | https://www.amperage.fr/evenements/mon-corps-sait-du-corset-au-corps-qui-sait/ |",
				"892 | Doomsday | 2026-11-20 00:00 | L'Ampérage, | https://www.amperage.fr/evenements/doomsday/ |",
				"752 | MDNS | 2026-11-26 20:00 | L'Ampérage, | https://www.amperage.fr/evenements/mdns/ |",
				"834 | Desire and Johnny Jewel | 2026-12-01 20:00 | L'Ampérage, | https://www.amperage.fr/evenements/desire-and-johnny-jewel/ |",
				"937 | Lenaïg | 2027-02-10 20:00 | L'Ampérage, | https://www.amperage.fr/evenements/lenaig/ |",
			},
		},
		{
			// Young audience and stand-up left out; « \ COMPLET / » and a trailing « – » stripped.
			venue: "la-source", file: "la-source.html", page: "https://lasource-fontaine.fr/programme/",
			want: []string{
				"aitawa-captain-xxi | Aïtawa + Captain XXI | 2026-11-19 | La Source, | https://lasource-fontaine.fr/event/aitawa-captain-xxi/ |",
				"6331 | Harold Lopez-Nussa – Suite Habana | 2026-11-25 | La Source, | https://lasource-fontaine.fr/event/6331/ |",
				"le-truc | Le TRUC | 2027-01-15 | La Source, | https://lasource-fontaine.fr/event/le-truc/ | Complet",
				"manon-lisa | Manon Lisa | 2027-05-12 | La Source, | https://lasource-fontaine.fr/event/manon-lisa/ |",
			},
		},
		{
			// Training left out; « (COMPLET) » stripped; halls with and without their town.
			venue: "retour-de-scene", file: "retour-de-scene.html", page: "https://www.retourdescene.net/agenda/",
			want: []string{
				"390755 | Micro’live – Ada Unn | 2026-11-19 | Bibliothèque Centre Ville, | https://www.retourdescene.net/microlive-ada-unn/ |",
				"389827 | RAKOON + SUMAC DUB + BLADI SOUND SYSTEM | 2026-11-21 | Ilyade, Seyssinet-Pariset | https://www.retourdescene.net/rakoon-sumac-dub-bladi-sound-system/ |",
				"390570 | BONNE NUIT + LOREN 6 | 2026-12-03 | L'Aquarium, | https://www.retourdescene.net/bonne-nuit/ |",
				"390401 | BEN PLG | 2026-12-09 | La Belle Electrique, Grenoble | https://www.retourdescene.net/ben-plg/ | Complet",
				"390873 | HK | 2027-05-15 | La Belle Electrique, | https://www.retourdescene.net/hk/ |",
			},
		},
	} {
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()
			rows, next := parseFixture(t, venue(t, tc.venue), tc.file, tc.page)
			if !slices.Equal(rows, tc.want) || !slices.Equal(next, tc.next) {
				t.Errorf("got\n%s\nnext %q\nwant\n%s\nnext %q", strings.Join(rows, "\n"), next, strings.Join(tc.want, "\n"), tc.next)
			}
		})
	}
}

// TestBellePages: the fetcher follows « Suivant » once, though the second page links back to the first.
func TestBellePages(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var requests []string
	client := testSite(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.URL.RequestURI())
		mu.Unlock()
		file := map[string]string{"/fr/programmation": "belle-electrique.html", "/fr/programmation?from=2027-02": "belle-electrique-2.html"}[r.URL.RequestURI()]
		body, err := os.ReadFile("testdata/" + file)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		_, _ = w.Write(body)
	})
	v := *venue(t, "belle-electrique")
	v.Site = append(v.Site[:0:0], "www.example.com")
	v.Listings = []string{"https://www.example.com/fr/programmation"}
	s := &sleeps{}
	items, pages, err := newFetcher(client, s).listing(context.Background(), &v, fixtureToday)
	if err != nil || pages != 2 || len(items) != 10 || len(requests) != 2 || !slices.Equal(s.d, []time.Duration{v.Delay}) {
		t.Errorf("listing = %d items, %d pages, %v; requests %v, sleeps %v", len(items), pages, err, requests, s.d)
	}
}

func TestCleanTitle(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]struct {
		s    string
		full bool
	}{
		"COMPLET • Sniper + Kenz":       {"Sniper + Kenz", true},
		"Complet · Sniper":              {"Sniper", true},
		"REPORTÉ • Izïa":                {"Izïa", false},
		"ANNULÉ - Izïa":                 {"Izïa", false},
		"BEN PLG (COMPLET)":             {"BEN PLG", true},
		"Le TRUC  \\  COMPLET /":        {"Le TRUC", true},
		"REPORTÉ • X (COMPLET)":         {"X", true},
		"Suite Habana –":                {"Suite Habana", false},
		"Completement Fou":              {"Completement Fou", false},
		"  The   Inspector Cluzo ":      {"The Inspector Cluzo", false},
		"Les Complets du Rock • Live":   {"Les Complets du Rock • Live", false},
		"Report (annulé)":               {"Report", false},
		"Raphaël – Karaoké":             {"Raphaël – Karaoké", false},
		"AC/DC Tribute":                 {"AC/DC Tribute", false},
		"COMPLET • REPORTÉ • Les Ogres": {"Les Ogres", true},
	} {
		s, full := cleanTitle(in)
		if s != want.s || full != want.full {
			t.Errorf("cleanTitle(%q) = %q, %v; want %q, %v", in, s, full, want.s, want.full)
		}
	}
}

func TestListingDate(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"Jeudi 19 novembre 2026":         "2026-11-19",
		"19 novembre 2026":               "2026-11-19",
		"Mercredi 18 novembre":           "2026-11-18", // today
		"Mardi 17 novembre":              "2026-11-17", // yesterday, still listed: past, not next year
		"16 décembre":                    "2026-12-16",
		"Jeudi 21 janvier":               "2027-01-21",
		"Jeudi 1er avril":                "2027-04-01",
		"18 septembre":                   "2027-09-18", // 10 months ahead
		"19 septembre":                   "2026-09-19", // more: a past item
		"Du 18 novembre au 09 décembre":  "2026-11-18",
		"Du 13 mai 2027 au 02 juin 2027": "2027-05-13",
		"31 février":                     "",
		"bientôt":                        "",
	} {
		if got := listingDate(in, fixtureToday); got != want {
			t.Errorf("listingDate(%q) = %q, want %q", in, got, want)
		}
	}
	if got := listingDate("29 février", "2027-11-18"); got != "2028-02-29" { // no 29th in 2027: the next leap year
		t.Errorf("listingDate(29 février) in 2027 = %q, want 2028-02-29", got)
	}
}

func TestEvent(t *testing.T) {
	t.Parallel()
	v := &Venue{Name: "La Salle", Site: []string{"www.example.com"}, Ticketing: []string{"billetweb.fr"}}
	base := listed{id: "x", artist: "Les Hiboux", date: "2026-12-01", page: "https://www.example.com/e/x"}
	long := strings.Repeat("a", 120)
	for _, tc := range []struct {
		name string
		edit func(*listed)
		want string // row, or "invalid"
	}{
		{"plain", func(*listed) {}, "x | Les Hiboux | 2026-12-01 | La Salle, | https://www.example.com/e/x |"},
		{"ticketing site", func(l *listed) { l.ticket = "https://www.billetweb.fr/hiboux" },
			"x | Les Hiboux | 2026-12-01 | La Salle, | https://www.billetweb.fr/hiboux |"},
		{"ticket elsewhere: the event page", func(l *listed) { l.ticket = "https://evil.example/pay" },
			"x | Les Hiboux | 2026-12-01 | La Salle, | https://www.example.com/e/x |"},
		{"page elsewhere: no link", func(l *listed) { l.page = "https://evil.example/e" }, "x | Les Hiboux | 2026-12-01 | La Salle, | |"},
		{"full with a price", func(l *listed) { l.price, l.full = "29 €", true },
			"x | Les Hiboux | 2026-12-01 | La Salle, | https://www.example.com/e/x | 29 € · Complet"},
		{"full without a price", func(l *listed) { l.full = true },
			"x | Les Hiboux | 2026-12-01 | La Salle, | https://www.example.com/e/x | Complet"},
		{"full, price too long for both", func(l *listed) { l.price, l.full = "De 12 € à 35 € selon tarif", true },
			"x | Les Hiboux | 2026-12-01 | La Salle, | https://www.example.com/e/x | Complet"},
		{"price over 30 runes dropped", func(l *listed) { l.price = long },
			"x | Les Hiboux | 2026-12-01 | La Salle, | https://www.example.com/e/x |"},
		{"artist clipped", func(l *listed) { l.artist = long },
			"x | " + strings.Repeat("a", 99) + "… | 2026-12-01 | La Salle, | https://www.example.com/e/x |"},
		{"another hall", func(l *listed) { l.venue, l.address = "L'Aquarium", "Grenoble" },
			"x | Les Hiboux | 2026-12-01 | L'Aquarium, Grenoble | https://www.example.com/e/x |"},
		{"no artist", func(l *listed) { l.artist = " " }, "invalid"},
		{"no date", func(l *listed) { l.date = "" }, "invalid"},
		{"no id", func(l *listed) { l.id = "" }, "invalid"},
		{"id too long", func(l *listed) { l.id = strings.Repeat("i", 257) }, "invalid"},
	} {
		l := base
		tc.edit(&l)
		e, ok := v.event(l)
		got := "invalid"
		if ok {
			got = row(e)
		}
		if got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestMC2Next(t *testing.T) {
	t.Parallel()
	page := mustURL(t, "https://www.mc2grenoble.fr/wp-json/tribe/events/v1/events?per_page=50")
	_, next, err := mc2(page, []byte(`{"events":[],"next_rest_url":"https://www.mc2grenoble.fr/wp-json/tribe/events/v1/events?page=2"}`), fixtureToday)
	if err != nil || len(next) != 1 || next[0].Query().Get("page") != "2" {
		t.Errorf("next = %v, %v", next, err)
	}
	// A bad next link fails the listing, without the venue-supplied URL in the error.
	_, _, err = mc2(page, []byte(`{"events":[],"next_rest_url":"https://www.mc2grenoble.fr/private\u0001"}`), fixtureToday)
	if err == nil || strings.Contains(err.Error(), "mc2grenoble") || strings.Contains(err.Error(), "private") {
		t.Errorf("bad next link: err = %v, want one without the URL", err)
	}
}

// TestVenues: the compiled-in list is usable as is.
func TestVenues(t *testing.T) {
	t.Parallel()
	keys := map[string]bool{}
	promoter := false
	for _, v := range Venues {
		if keys[v.Key] || v.Name == "" || v.parse == nil || len(v.Listings) == 0 || v.MaxPages < len(v.Listings) || len(v.Site) == 0 {
			t.Errorf("venue %q: duplicate key or missing field", v.Key)
		}
		keys[v.Key] = true
		for _, l := range v.Listings {
			if !v.site(mustURL(t, l)) {
				t.Errorf("venue %q: listing off its site", v.Key)
			}
		}
		if promoter && !v.Promoter {
			t.Errorf("venue %q listed after a promoter", v.Key)
		}
		promoter = promoter || v.Promoter
	}
	unit, err := os.ReadFile("../../deploy/concerts-sync.service")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^TimeoutStartSec=(\d+)min$`).FindSubmatch(unit)
	if m == nil {
		t.Fatal("no TimeoutStartSec=<n>min in the sync unit")
	}
	if limit, _ := strconv.Atoi(string(m[1])); time.Duration(len(Venues))*venueTimeout > time.Duration(limit)*time.Minute*9/10 {
		t.Errorf("%d venues × %v leave no margin in the sync unit's TimeoutStartSec", len(Venues), venueTimeout)
	}
	if got := Name("belle-electrique"); got != "La Belle Électrique" {
		t.Errorf("Name(belle-electrique) = %q", got)
	}
	if got := Name("gone"); got != "" {
		t.Errorf("Name(gone) = %q, want empty", got)
	}
}
