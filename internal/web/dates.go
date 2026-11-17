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
	// fullMonths name the month headings of the list.
	fullMonths = [...]string{"janvier", "février", "mars", "avril", "mai", "juin", "juillet", "août", "septembre", "octobre", "novembre", "décembre"}
)

// monthOf is the heading of a YYYY-MM-DD date's month: « mars 2027 ».
func monthOf(date string) string {
	d, err := time.Parse(time.DateOnly, date)
	if err != nil {
		return date
	}
	return fullMonths[d.Month()-1] + " " + strconv.Itoa(d.Year())
}

// today is the clock's date in Paris, comparable with concert dates: a concert is past from the next day.
func (s *server) today() string { return s.now().In(paris).Format(time.DateOnly) }

func (s *server) upcoming(c store.Concert) bool { return c.Date >= s.today() }

// stub is a YYYY-MM-DD date split for the ticket stub: « sam. », « 14 », « mars »; the concert page
// adds Year and Time (ticketDate), the card shows them elsewhere.
type stub struct{ Weekday, Day, Month, Year, Time string }

func stubOf(date string) stub {
	d, _ := time.Parse(time.DateOnly, date)
	return stub{Weekday: weekdays[d.Weekday()], Day: strconv.Itoa(d.Day()), Month: months[d.Month()-1]}
}

// otherYear is d's year when it is not now's, else "".
func (s *server) otherYear(d time.Time) string {
	if d.Year() != s.now().In(paris).Year() {
		return strconv.Itoa(d.Year())
	}
	return ""
}

// frDate formats a YYYY-MM-DD date as « sam. 14 mars », adding the year when it is not now's.
func (s *server) frDate(date string) string {
	d, err := time.Parse(time.DateOnly, date)
	if err != nil {
		return date
	}
	st := stubOf(date)
	out := st.Weekday + " " + st.Day + " " + st.Month
	if y := s.otherYear(d); y != "" {
		out += " " + y
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
	out := s.frDate(c.Date)
	if c.Time != "" {
		out += " · " + frTime(c.Time)
	}
	return out
}

// ticketDate is the concert page's stub: the card's, plus the time and the year when it is not now's.
func (s *server) ticketDate(c store.Concert) stub {
	t := stubOf(c.Date)
	if c.Time != "" {
		t.Time = frTime(c.Time)
	}
	if d, err := time.Parse(time.DateOnly, c.Date); err == nil {
		t.Year = s.otherYear(d)
	}
	return t
}

// datetime is the machine-readable value of a concert's <time> element.
func datetime(c store.Concert) string {
	if c.Time == "" {
		return c.Date
	}
	return c.Date + "T" + c.Time
}
