package web

import (
	"hash/fnv"
	"strconv"
	"strings"
)

// Counts of the .hue-N and .pat-N classes in style.css.
const (
	stubHues     = 12
	stubPatterns = 4
)

// stubStyle picks the classes of an artist's stub pattern, the same for every spelling case.
// Classes, not style="--h:…": the CSP allows no inline style.
func stubStyle(artist string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(artist))) // never fails; the artist is already trimmed by validate.Text
	n := h.Sum32()
	return "hue-" + strconv.Itoa(int(n%stubHues)) + " pat-" + strconv.Itoa(int(n/stubHues%stubPatterns))
}
