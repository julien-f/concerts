package main

import (
	"cmp"
	"context"
	"errors"
	"strings"
	"time"

	"concerts/internal/store"
)

// demo fills a fresh database with a group for `make demo` and scripts/verify-ui.sh:
// three members, upcoming and past concerts relative to now.
func demo(ctx context.Context, st *store.Store, baseURL string, now time.Time) error {
	g, err := newGroup(ctx, st, baseURL, "Les copains du concert")
	if err != nil {
		return err
	}
	var ids []int64
	for _, name := range []string{"Léa", "Marc", "Sam"} {
		m, err := st.CreateMember(ctx, g.ID, name)
		if err != nil {
			return err
		}
		ids = append(ids, m.ID)
	}
	day := func(offset int) string { return now.AddDate(0, 0, offset).Format(time.DateOnly) }
	var concerts []int64
	seed := []store.Concert{
		{Artist: "Fontaines D.C. + Kneecap", Date: day(10), Time: "20:00", Venue: "Zénith Paris", Address: "211 av. Jean Jaurès, 75019 Paris", TicketURL: "https://example.com/billets/fontaines",
			Price: "39,90–52 €", Genre: "Post-punk, Rap",
			// A glued word and a long link, as pasted pages bring: they must not widen the page.
			Description: "FONTAINES D.C.Le groupe de Dublin revient avec Romance, son quatrième album.\n\n" +
				"Première partie à 19 h 30. Infos : https://example.com/billets/fontaines/informations-pratiques-et-acces"},
		{Artist: "Yuksek", Date: day(25), Venue: "La Cigale", Price: "Prix libre"},
		{Artist: "Zaho de Sagazan", Date: day(60), Time: "20:30", Venue: "L'Olympia", OnSaleDate: day(5)},
		{Artist: "Air", Date: day(-20), Time: "20:00", Venue: "Salle Pleyel"},
		{Artist: "Justice", Date: day(-60), Venue: "Accor Arena"},
	}
	for i, c := range seed {
		id, err := st.CreateConcert(ctx, g.ID, ids[i%len(ids)], c, day(0))
		if err != nil {
			return err
		}
		concerts = append(concerts, id)
	}
	lea, marc, sam := ids[0], ids[1], ids[2]
	for _, s := range []struct {
		concert, member int64
		status          string
	}{
		{concerts[0], marc, store.Going}, {concerts[0], sam, store.Going}, {concerts[0], lea, store.Interested},
		{concerts[1], marc, store.Interested}, {concerts[3], lea, store.Going}, {concerts[3], sam, store.Going},
	} {
		if err := st.SetStatus(ctx, g.ID, s.concert, s.member, s.status); err != nil {
			return err
		}
	}
	// Two edits of the first concert, revisions 1 and 2 (verify-ui relies on them): its history and undo banner.
	first := seed[0]
	first.OnSaleDate = day(-30)
	if _, err := st.UpdateConcert(ctx, g.ID, marc, concerts[0], first, day(0)); err != nil {
		return err
	}
	first.Time = "20:30"
	if _, err := st.UpdateConcert(ctx, g.ID, sam, concerts[0], first, day(0)); err != nil {
		return err
	}
	// Older history, created after the concerts above (verify-ui relies on their ids): more past
	// concerts than the list shows, so its link to all of them is checked too.
	artists := []string{"Phoenix", "Feu! Chatterton", "Bon Iver", "Christine and the Queens", "Arctic Monkeys"}
	venues := []string{"La Cigale", "Le Trianon", "Zénith Paris", "L'Olympia"}
	for i := range 29 {
		c := store.Concert{Artist: artists[i%len(artists)], Date: day(-90 - 14*i), Venue: venues[i%len(venues)]}
		id, err := st.CreateConcert(ctx, g.ID, ids[i%len(ids)], c, day(0))
		if err != nil {
			return err
		}
		if err := st.SetStatus(ctx, g.ID, id, ids[(i+1)%len(ids)], store.Going); err != nil {
			return err
		}
	}
	return demoImports(ctx, st, g.ID, marc, day)
}

// demoImports follows two venues and imports their agenda, created last (verify-ui relies on the
// ids above): « À l'affiche » with a sold-out concert, one without time and a long artist, venue
// and genre, one import answered, so in the list, and one edited that its venue changed since.
func demoImports(ctx context.Context, st *store.Store, groupID, marc int64, day func(int) string) error {
	agendas := map[string][]store.Concert{
		"belle-electrique": {
			{Artist: "Bagarre", Date: day(12), Time: "20:00", Venue: "La Belle Électrique", Price: "22 €", Genre: "Pop, Électro",
				SoldOut: true},
			{Artist: "Orchestre National de Barbès + Les Ogres de Barback & invités", Date: day(33), Time: "19:30",
				Venue: "La Belle Électrique, grande salle (debout)", Price: "25–28 €",
				Genre: "Musiques du monde, Chanson festive, Fanfare, Raï, DJ set"},
			{Artist: "Lysistrata", Date: day(40), Time: "20:30", Venue: "La Belle Électrique", Genre: "Rock"},
		},
		"la-source": {
			{Artist: "Pomme", Date: day(18), Venue: "La Source", Genre: "Chanson, Folk"},
			{Artist: "Kompromat", Date: day(45), Time: "21:00", Venue: "La Source", Genre: "Électro, Techno"},
		},
	}
	for _, venue := range []string{"belle-electrique", "la-source"} {
		if err := st.Follow(ctx, groupID, venue); err != nil {
			return err
		}
		var events []store.Event
		for _, c := range agendas[venue] {
			c.TicketURL = "https://example.com/" + venue + "/" + strings.ToLower(strings.Fields(c.Artist)[0])
			events = append(events, store.Event{SourceID: c.TicketURL, Concert: c})
		}
		if err := st.PutVenueEvents(ctx, venue, events); err != nil {
			return err
		}
		if _, err := st.ApplyImports(ctx, groupID, venue, events, day(0)); err != nil {
			return err
		}
	}
	cs, err := st.ConcertsOn(ctx, groupID, day(45))
	if err != nil || len(cs) == 0 {
		return cmp.Or(err, errors.New("demo: Kompromat not imported"))
	}
	if err := st.SetStatus(ctx, groupID, cs[0].ID, marc, store.Interested); err != nil {
		return err
	}
	// Lysistrata, moved by Marc, then sold out at the venue: « Relire la billetterie » offers Heure and Complet.
	if cs, err = st.ConcertsOn(ctx, groupID, day(40)); err != nil || len(cs) == 0 {
		return cmp.Or(err, errors.New("demo: Lysistrata not imported"))
	}
	moved := cs[0]
	moved.Time = "21:00"
	if _, err := st.UpdateConcert(ctx, groupID, marc, moved.ID, moved, day(0)); err != nil {
		return err
	}
	sold := cs[0]
	sold.SoldOut = true
	return st.PutVenueEvents(ctx, "belle-electrique", []store.Event{{SourceID: sold.TicketURL, Concert: sold}})
}
