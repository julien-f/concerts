package web

import (
	"strings"
	"testing"
)

func TestCleanParagraphs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, in, want string
		ok             bool
	}{
		{"one line", "  Un   deux  ", "Un deux", true},
		{"CR LF and lone CR", "Un\r\nDeux\rTrois", "Un\nDeux Trois", true},
		{"blank lines folded", "\n\nUn\n \n\t\n\nDeux\n\n", "Un\n\nDeux", true},
		{"only spaces", " \n \r\n ", "", false},
		{"soft hyphen", "Ex\u00ademple", "", false},
		{"control character", "Un\x07", "", false},
		{"at the limit", strings.Repeat("é", 2000), strings.Repeat("é", 2000), true},
		{"over the limit", strings.Repeat("é", 2001), "", false},
		{"limit counts kept line feeds", strings.Repeat("a", 1000) + "\n\n" + strings.Repeat("b", 999), "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := cleanParagraphs(tt.in, 2000)
			if got != tt.want || ok != tt.ok {
				t.Errorf("cleanParagraphs(%q) = %q, %v; want %q, %v", tt.in, got, ok, tt.want, tt.ok)
			}
		})
	}
}
