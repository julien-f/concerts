// Package agenda imports the upcoming concerts of the venues groups follow: a list of venues
// compiled in, one parser per venue, a polite fetcher and Sync.
package agenda

import (
	neturl "net/url"
	"time"
)

// Venue is a hall, or a promoter, whose agenda the sync reads.
type Venue struct {
	Key, Name string
	Listings  []string // first listing pages; the parser finds the next ones
	Format    string   // media type of the listing pages
	Site      []string // hosts requested, redirects included
	Ticketing []string // further domains a ticket link may point to, subdomains included
	Delay     time.Duration
	MaxPages  int  // listing pages per run
	Details   bool // each event's page is read once, with prefill, for what the listing lacks (time, price, ticket link…)
	Promoter  bool // lists other halls' concerts, so syncs after them: their copy wins the dedup
	parse     parser
	timeout   time.Duration // 0: venueTimeout
}

// Name is the display name of the venue key, or "" if none is compiled in.
func Name(key string) string {
	for _, v := range Venues {
		if v.Key == key {
			return v.Name
		}
	}
	return ""
}

// Venues are the venues groups can follow, probed 2026-11-18: robots.txt checked by hand and
// noted beside each, no bot challenge. A key is stored in imported rows: never rename one.
var Venues = []Venue{
	{
		Key: "mc2", Name: "MC2",
		// The iCal feed stops at 30 events; robots.txt allows all.
		Listings: []string{"https://www.mc2grenoble.fr/wp-json/tribe/events/v1/events?categories=musique&per_page=50"},
		Format:   "application/json", Site: []string{"www.mc2grenoble.fr"}, MaxPages: 3, parse: mc2,
	},
	{
		Key: "belle-electrique", Name: "La Belle Électrique",
		// robots.txt: Crawl-delay: 10. Prices and ticket links are on the event pages only: the 8
		// listing pages take 80 s, the timeout leaves about 10 event pages a run.
		Listings: []string{"https://www.la-belle-electrique.com/fr/programmation"},
		Format:   "text/html", Site: []string{"www.la-belle-electrique.com"}, Ticketing: []string{"la-belle-electrique.com"},
		Delay: 10 * time.Second, MaxPages: 8, Details: true, timeout: 3 * time.Minute, parse: belle,
	},
	{
		Key: "lilyade", Name: "L'ilyade",
		// robots.txt allows all.
		Listings: []string{"https://www.lilyade.fr/programmation/"},
		Format:   "text/html", Site: []string{"www.lilyade.fr"}, MaxPages: 8, parse: lilyade,
		Ticketing: []string{"billetweb.fr", "helloasso.com", "mapado.com", "retourdescene.net", "rpo.net",
			"seetickets.com", "tickandlive.com", "weezevent.com"},
	},
	{
		Key: "amperage", Name: "L'Ampérage",
		// robots.txt: /wp-admin/ only.
		Listings: []string{"https://www.amperage.fr/la-programmation/"},
		Format:   "text/html", Site: []string{"www.amperage.fr"}, MaxPages: 1, parse: amperage,
	},
	{
		Key: "la-source", Name: "La Source",
		// robots.txt allows all.
		Listings: []string{"https://lasource-fontaine.fr/programme/"},
		Format:   "text/html", Site: []string{"lasource-fontaine.fr"}, Ticketing: []string{"lasource-fontaine.fr"},
		MaxPages: 1, Details: true, parse: laSource,
	},
	{
		Key: "retour-de-scene", Name: "Retour de Scène",
		// robots.txt: /feed and /wp-admin/ disallowed, neither used.
		Listings: []string{"https://www.retourdescene.net/agenda/"},
		Format:   "text/html", Site: []string{"www.retourdescene.net"}, MaxPages: 1, Details: true, Promoter: true,
		parse: retourDeScene,
	},
}

// parser reads a listing page: its events, and the next listing pages it links to.
type parser func(page *neturl.URL, body []byte, today string) ([]listed, []*neturl.URL, error)

// listed is an event as a listing gives it; Sync cleans and validates it.
type listed struct {
	id                    string
	artist                string // cleanTitle'd
	date, time            string // YYYY-MM-DD; HH:MM or ""
	venue, address, price string // venue "" for the venue itself
	page, ticket          string // the event's page on the venue's site; a ticket link or ""
	full                  bool   // « Complet »
	genre                 string // genre(…) of the listing's labels
}
