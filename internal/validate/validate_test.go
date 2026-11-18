package validate

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"concerts/internal/store"
)

func TestConcert(t *testing.T) {
	t.Parallel()
	ok := store.Concert{Artist: "Zaz", Date: "2027-03-14", Venue: "La Source"}
	tests := []struct {
		name string
		in   store.Concert
		want store.Concert
		errs []string // failing fields
	}{
		{"minimal", ok, ok, nil},
		{"cleaned", store.Concert{Artist: "  Zaz \t live ", Date: "2027-03-14", Time: "9:05", Venue: " La  Source ",
			Address: "  ", TicketURL: " https://example.com/x ", Price: " 29 € ", Description: "Un\r\n\n\nDeux"},
			store.Concert{Artist: "Zaz live", Date: "2027-03-14", Time: "09:05", Venue: "La Source",
				TicketURL: "https://example.com/x", Price: "29 €", Description: "Un\n\nDeux"}, nil},
		{"invalid", store.Concert{Artist: " ", Date: "14/03/2027", Time: "25:00", Venue: strings.Repeat("a", 101),
			TicketURL: "javascript:alert(1)", OnSaleDate: "1999-12-31", Price: strings.Repeat("€", 31)},
			store.Concert{Date: "14/03/2027", Time: "25:00", Venue: strings.Repeat("a", 101),
				TicketURL: "javascript:alert(1)", OnSaleDate: "1999-12-31", Price: strings.Repeat("€", 31)},
			[]string{"artist", "date", "on_sale_date", "price", "ticket_url", "time", "venue"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, errs := Concert(tt.in)
			if got != tt.want {
				t.Errorf("Concert() = %+v, want %+v", got, tt.want)
			}
			if fields := slices.Sorted(maps.Keys(errs)); !slices.Equal(fields, tt.errs) {
				t.Errorf("errors on %v, want %v", fields, tt.errs)
			}
		})
	}
}

func TestParagraphs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, in, want string
		ok             bool
	}{
		{"one line", "  Un   deux  ", "Un deux", true},
		{"CR LF and lone CR", "Un\r\nDeux\rTrois", "Un\nDeux Trois", true},
		{"blank lines folded", "\n\nUn\n \n\t\n\nDeux\n\n", "Un\n\nDeux", true},
		{"only spaces", " \n \r\n ", "", false},
		{"soft hyphen", "Ex\u00ademple", "Ex\u00ademple", false},
		{"control character", "Un\x07", "Un\x07", false},
		{"at the limit", strings.Repeat("é", 2000), strings.Repeat("é", 2000), true},
		{"over the limit", strings.Repeat("é", 2001), strings.Repeat("é", 2001), false},
		{"limit counts kept line feeds", strings.Repeat("a", 1000) + "\n\n" + strings.Repeat("b", 999),
			strings.Repeat("a", 1000) + "\n\n" + strings.Repeat("b", 999), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := Paragraphs(tt.in, 2000)
			if got != tt.want || ok != tt.ok {
				t.Errorf("Paragraphs(%q) = %q, %v; want %q, %v", tt.in, got, ok, tt.want, tt.ok)
			}
		})
	}
}
