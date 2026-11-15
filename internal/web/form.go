package web

import (
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxFormBytes caps request bodies; forms here are a few hundred bytes.
const maxFormBytes = 64 << 10

// parseForm reads a capped form body; on failure it answers 400 and returns false.
func parseForm(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Formulaire invalide.", http.StatusBadRequest)
		return false
	}
	return true
}

// cleanText trims v and collapses whitespace runs; it reports false unless the
// result is valid UTF-8 of 1 to maxRunes runes without control or format
// (zero-width, bidi) characters.
func cleanText(v string, maxRunes int) (string, bool) {
	v = strings.Join(strings.Fields(v), " ")
	if !utf8.ValidString(v) || v == "" || utf8.RuneCountInString(v) > maxRunes {
		return v, false
	}
	for _, r := range v {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return v, false
		}
	}
	return v, true
}
