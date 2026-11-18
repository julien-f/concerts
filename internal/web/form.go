package web

import (
	"net/http"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"concerts/internal/validate"
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

// lines splits a text cleaned by validate.Paragraphs into its non-blank lines, one HTML paragraph each.
func lines(v string) []string {
	return slices.DeleteFunc(strings.Split(v, "\n"), func(l string) bool { return l == "" })
}

// CleanName is validate.Text for first names, which must also start with a letter or digit;
// exported for the operator's rename-member.
func CleanName(v string) (string, bool) {
	v, ok := validate.Text(v, maxNameRunes)
	r, _ := utf8.DecodeRuneInString(v)
	return v, ok && unicode.In(r, unicode.L, unicode.Nd)
}
