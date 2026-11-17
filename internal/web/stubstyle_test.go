package web

import "testing"

func TestStubStyle(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ a, b string }{
		{"Daft Punk", "daft punk"},
		{"ZAHO DE SAGAZAN", "Zaho de Sagazan"},
		{"Éloïse", "éloïse"},
	} {
		if got, want := stubStyle(tc.a), stubStyle(tc.b); got != want {
			t.Errorf("stubStyle(%q) = %q, stubStyle(%q) = %q, want equal", tc.a, got, tc.b, want)
		}
	}
	// Pinned: changing the hash would recolour every concert.
	for _, tc := range []struct{ artist, want string }{
		{"Daft Punk", "hue-4 pat-2"},
		{"Fontaines D.C.", "hue-7 pat-0"},
	} {
		if got := stubStyle(tc.artist); got != tc.want {
			t.Errorf("stubStyle(%q) = %q, want %q", tc.artist, got, tc.want)
		}
	}
}
