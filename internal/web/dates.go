package web

import (
	"fmt"
	"strconv"
	"time"
	_ "time/tzdata" // Europe/Paris must not depend on the host's zoneinfo

	"concerts/internal/store"
)

// paris is the one time zone of the app (ADR 0005); time/tzdata guarantees it loads.
var paris = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		panic(err)
	}
	return loc
}()

var (
	weekdays = [...]string{"dim.", "lun.", "mar.", "mer.", "jeu.", "ven.", "sam."}
	months   = [...]string{"janv.", "févr.", "mars", "avr.", "mai", "juin", "juil.", "août", "sept.", "oct.", "nov.", "déc."}
)

// stub is a date split for the ticket stub: « sam. », « 14 », « mars ».
type stub struct{ Weekday, Day, Month string }

func stubOf(d time.Time) stub {
	return stub{weekdays[d.Weekday()], strconv.Itoa(d.Day()), months[d.Month()-1]}
}

// stubOfDate is stubOf for a YYYY-MM-DD string, as templates hold.
func stubOfDate(date string) stub {
	d, _ := time.Parse(time.DateOnly, date)
	return stubOf(d)
}

// frDate formats a YYYY-MM-DD date as « sam. 14 mars », adding the year when it is not now's.
func frDate(date string, now time.Time) string {
	d, err := time.Parse(time.DateOnly, date)
	if err != nil {
		return date
	}
	s := stubOf(d)
	out := s.Weekday + " " + s.Day + " " + s.Month
	if d.Year() != now.In(paris).Year() {
		out += " " + strconv.Itoa(d.Year())
	}
	return out
}

// frTime formats HH:MM as « 20 h » or « 20 h 30 », with no-break spaces.
func frTime(hm string) string {
	t, err := time.Parse("15:04", hm)
	if err != nil {
		return hm
	}
	if t.Minute() == 0 {
		return fmt.Sprintf("%d h", t.Hour())
	}
	return fmt.Sprintf("%d h %02d", t.Hour(), t.Minute())
}

// when is a concert's date and optional time: « sam. 14 mars · 20 h ».
func (s *server) when(c store.Concert) string {
	out := frDate(c.Date, s.now())
	if c.Time != "" {
		out += " · " + frTime(c.Time)
	}
	return out
}

func (s *server) frDate(date string) string { return frDate(date, s.now()) }

// datetime is the machine-readable value of a concert's <time> element.
func datetime(c store.Concert) string {
	if c.Time == "" {
		return c.Date
	}
	return c.Date + "T" + c.Time
}
