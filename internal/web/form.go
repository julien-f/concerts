package web

import (
	"net/http"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxFormBytes caps request bodies; the largest form, a full description, takes about 24 kB.
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
	return valid(strings.Join(strings.Fields(v), " "), maxRunes)
}

// cleanParagraphs is cleanText for multi-line text: it keeps line feeds, folds runs of
// blank lines into one and trims the whole.
func cleanParagraphs(v string, maxRunes int) (string, bool) {
	return valid(paragraphs(v), maxRunes)
}

// paragraphs collapses whitespace within each line (dropping CR) and keeps one blank line at most between lines.
func paragraphs(v string) string {
	var lines []string
	for l := range strings.SplitSeq(v, "\n") {
		l = strings.Join(strings.Fields(l), " ")
		if l != "" || (len(lines) > 0 && lines[len(lines)-1] != "") {
			lines = append(lines, l)
		}
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

// lines splits a text cleaned by paragraphs into its non-blank lines, one HTML paragraph each.
func lines(v string) []string {
	return slices.DeleteFunc(strings.Split(v, "\n"), func(l string) bool { return l == "" })
}

// valid returns v, true if v is valid UTF-8 of 1 to maxRunes runes without
// invisible characters other than line feeds; "", false otherwise.
func valid(v string, maxRunes int) (string, bool) {
	if !utf8.ValidString(v) || v == "" || utf8.RuneCountInString(v) > maxRunes ||
		strings.ContainsFunc(v, func(r rune) bool { return r != '\n' && invisible(r) }) {
		return "", false
	}
	return v, true
}

// invisible reports control, format, variation selector, default-ignorable and braille blank characters.
func invisible(r rune) bool {
	return r == '⠀' || unicode.In(r, unicode.Cc, unicode.Cf, unicode.Variation_Selector, unicode.Other_Default_Ignorable_Code_Point)
}

// cleanName is cleanText for first names, which must also start with a letter or digit.
func cleanName(v string, maxRunes int) (string, bool) {
	v, ok := cleanText(v, maxRunes)
	if r, _ := utf8.DecodeRuneInString(v); !ok || !unicode.In(r, unicode.L, unicode.N) {
		return "", false
	}
	return v, true
}
