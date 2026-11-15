package web

import (
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // Europe/Paris must not depend on the host's zoneinfo

	"concerts/internal/store"
)

// paris is the one time zone of the app (ADR 0005).
var paris = mustLoadLocation("Europe/Paris")

func mustLoadLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err) // embedded by time/tzdata, so never at run time
	}
	return loc
}

var (
	weekdays = [...]string{"dim.", "lun.", "mar.", "mer.", "jeu.", "ven.", "sam."}
	months   = [...]string{"janv.", "févr.", "mars", "avr.", "mai", "juin", "juil.", "août", "sept.", "oct.", "nov.", "déc."}
)

// stub is a date split for the ticket stub: « sam. », « 14 », « mars ».
type stub struct{ Weekday, Day, Month string }

func stubOf(date string) stub {
	d, err := time.Parse(time.DateOnly, date)
	if err != nil {
		return stub{}
	}
	return stub{weekdays[d.Weekday()], strconv.Itoa(d.Day()), months[d.Month()-1]}
}

// frDate formats a YYYY-MM-DD date as « sam. 14 mars », adding the year when it is not now's.
func frDate(date string, now time.Time) string {
	d, err := time.Parse(time.DateOnly, date)
	if err != nil {
		return date
	}
	s := stubOf(date)
	out := s.Weekday + " " + s.Day + " " + s.Month
	if d.Year() != now.In(paris).Year() {
		out += " " + strconv.Itoa(d.Year())
	}
	return out
}

// frTime formats HH:MM as « 20 h » or « 20 h 30 », with no-break spaces.
func frTime(hm string) string {
	h, m, _ := strings.Cut(hm, ":")
	h = strings.TrimPrefix(h, "0")
	if h == "" {
		h = "0"
	}
	if m == "00" || m == "" {
		return h + " h"
	}
	return h + " h " + m
}

// when is a concert's date and optional time: « sam. 14 mars · 20 h ».
func when(c store.Concert, now time.Time) string {
	s := frDate(c.Date, now)
	if c.Time != "" {
		s += " · " + frTime(c.Time)
	}
	return s
}

// datetime is the machine-readable value of a concert's <time> element.
func datetime(c store.Concert) string {
	if c.Time == "" {
		return c.Date
	}
	return c.Date + "T" + c.Time
}
