package prefill

import (
	"bytes"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"
)

var paris, _ = time.LoadLocation("Europe/Paris")

// now is before every fixture's concert; Fnac's first validFrom (2025) is past, its second (2027) future.
var now = time.Date(2026, 11, 16, 10, 0, 0, 0, paris)

func TestParseFixtures(t *testing.T) {
	t.Parallel()
	tests := map[string]Result{
		// Performer array, offset date, no on-sale date, a non-event block beside.
		"dice.html": {Artist: "Les Hiboux", Date: "2026-11-27", Time: "20:00", Venue: "Salle Exemple"},
		// Generic Event, local time without offset, pretty-printed JSON after a WebSite block.
		"olympia.html": {Artist: "Jeanne Exemple", Date: "2026-12-08", Time: "20:00", Venue: "L'Exemple"},
		// Minutes only, date-only endDate ignored.
		"zenith.html": {Artist: "Nova Test", Date: "2027-01-24", Time: "20:00", Venue: "Arène Exemple"},
		// ComedyEvent, EventVenue, milliseconds; past validFrom skipped, nested future one kept; broken block skipped.
		"fnac.html": {Artist: "Marc Essai", Date: "2027-11-04", Time: "20:00", Venue: "Théâtre Exemple", OnSaleDate: "2027-02-14"},
		// No JSON-LD: og:title written with name=.
		"accor.html": {Artist: "Duo Fictif"},
	}
	for name, want := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b, err := fs.ReadFile(os.DirFS("testdata"), name)
			if err != nil {
				t.Fatal(err)
			}
			if got := Parse(bytes.NewReader(b), now); got != want {
				t.Fatalf("Parse = %+v\nwant    %+v", got, want)
			}
		})
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
		{"date only, no time", `<script type="application/ld+json">{"@type":"Festival","name":"F","startDate":"2027-07-04","location":"Plein air"}</script>`,
			Result{Artist: "F", Date: "2027-07-04", Venue: "Plein air"}},
		{"MusicEvent preferred, in @graph", `<script type="APPLICATION/LD+JSON" id="x">{"@graph":[{"@type":"Event","name":"Other"},{"@type":["Thing","MusicEvent"],"name":"Music"}]}</script>`,
			Result{Artist: "Music"}},
		{"ItemList of events", `<script type="application/ld+json">{"@type":"ItemList","itemListElement":[{"item":{"@type":"MusicEvent","name":"Listed"}}]}</script>`,
			Result{Artist: "Listed"}},
		{"title fallback, cut at the first separator", `<title>Artiste &amp; Co - Salle | Site</title>`, Result{Artist: "Artiste & Co"}},
		{"og:title 'null' skipped", `<meta property="og:title" content="null"><meta name="twitter:title" content="Tw @ Venue">`, Result{Artist: "Tw"}},
		{"garbage date ignored", `<script type="application/ld+json">{"@type":"MusicEvent","name":"A","startDate":"bientôt"}</script>`, Result{Artist: "A"}},
		{"nothing", `<p>Just a moment...</p>`, Result{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Parse(strings.NewReader(tt.html), now); got != tt.want {
				t.Fatalf("Parse = %+v, want %+v", got, tt.want)
			}
		})
	}
}
