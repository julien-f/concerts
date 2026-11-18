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
	Details   bool // the listing has no times: each event's page is read once, with prefill
	Promoter  bool // lists other halls' concerts, so syncs after them: their copy wins the dedup
	parse     parser
}

// parser reads a listing page: its events, and the next listing pages it links to.
type parser func(page *neturl.URL, body []byte, today string) ([]listed, []*neturl.URL, error)

// listed is an event as a listing gives it; Sync cleans and validates it.
type listed struct {
	id string
}
