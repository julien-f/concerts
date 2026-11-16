package web

import (
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"concerts/internal/store"
)

const (
	// concertLength is the assumed length of a timed concert: calendars need an end.
	concertLength = 3 * time.Hour
	icsUTC        = "20060102T150405Z"
)

// icsEscape escapes a TEXT value (RFC 5545 §3.3.11); escaping newlines also stops
// a field from starting a new property line.
var icsEscape = strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\r\n", `\n`, "\n", `\n`, "\r", `\n`)

// icsFold splits a content line into CRLF-terminated lines of at most 75 octets,
// continuation lines starting with a space, never inside a UTF-8 sequence (§3.1).
func icsFold(line string) string {
	var b strings.Builder
	limit := 75
	for len(line) > limit {
		// A rune spans at most 4 octets: step back at most 3, so invalid UTF-8
		// (a run of continuation bytes) cannot stall the loop.
		cut := limit
		for cut > limit-3 && !utf8.RuneStart(line[cut]) {
			cut--
		}
		b.WriteString(line[:cut] + "\r\n ")
		line, limit = line[cut:], 74 // the leading space counts
	}
	b.WriteString(line + "\r\n")
	return b.String()
}

// ics is a one-event calendar for c. It holds no link to the group: the secret
// must not reach a calendar provider. Times are UTC, so no VTIMEZONE is needed.
func ics(groupID int64, c store.Concert, stamp time.Time) string {
	lines := []string{
		"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//concerts//FR", "BEGIN:VEVENT",
		// Stable, so importing twice does not duplicate; no host, per RFC 7986 §5.3.
		fmt.Sprintf("UID:concert-%d-%d", groupID, c.ID),
		"DTSTAMP:" + stamp.UTC().Format(icsUTC),
	}
	if start, err := time.ParseInLocation(time.DateOnly+" 15:04", c.Date+" "+c.Time, paris); err == nil {
		lines = append(lines,
			"DTSTART:"+start.UTC().Format(icsUTC),
			"DTEND:"+start.Add(concertLength).UTC().Format(icsUTC))
	} else {
		day, _ := time.Parse(time.DateOnly, c.Date)
		lines = append(lines,
			"DTSTART;VALUE=DATE:"+day.Format("20060102"),
			"DTEND;VALUE=DATE:"+day.AddDate(0, 0, 1).Format("20060102")) // exclusive end
	}
	lines = append(lines, "SUMMARY:"+icsEscape.Replace(c.Artist), "LOCATION:"+icsEscape.Replace(c.Venue))
	if c.TicketURL != "" {
		lines = append(lines, "DESCRIPTION:"+icsEscape.Replace("Billets : "+c.TicketURL))
	}
	lines = append(lines, "END:VEVENT", "END:VCALENDAR")
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(icsFold(l))
	}
	return b.String()
}

// agenda serves one concert as an .ics download, for « Ajouter à mon agenda ».
func (s *server) agenda(w http.ResponseWriter, r *http.Request, gr *groupRequest) {
	c, ok := s.concert(w, r, gr)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="concert.ics"`)
	w.Header().Set("Cache-Control", "private, no-store")
	_, _ = w.Write([]byte(ics(gr.Group.ID, c, s.now())))
}
