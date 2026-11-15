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
// result is valid UTF-8 of 1 to maxRunes runes without invisible characters
// (control, format, variation selectors, default-ignorable, braille blank).
func cleanText(v string, maxRunes int) (string, bool) {
	v = strings.Join(strings.Fields(v), " ")
	if !utf8.ValidString(v) || v == "" || utf8.RuneCountInString(v) > maxRunes {
		return "", false
	}
	for _, r := range v {
		if r == '⠀' || unicode.In(r, unicode.Cc, unicode.Cf, unicode.Variation_Selector, unicode.Other_Default_Ignorable_Code_Point) {
			return "", false
		}
	}
	return v, true
}

// cleanName is cleanText for first names, which must also start with a letter or digit.
func cleanName(v string, maxRunes int) (string, bool) {
	v, ok := cleanText(v, maxRunes)
	if r, _ := utf8.DecodeRuneInString(v); !ok || !unicode.In(r, unicode.L, unicode.N) {
		return "", false
	}
	return v, true
}
