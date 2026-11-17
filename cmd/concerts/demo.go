package main

import (
	"context"
	"time"

	"concerts/internal/store"
)

// demo fills a fresh database with a group for `make demo` and scripts/verify-ui.sh:
// three members, upcoming and past concerts relative to today.
func demo(ctx context.Context, st *store.Store, baseURL string) error {
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
	day := func(offset int) string { return time.Now().AddDate(0, 0, offset).Format(time.DateOnly) }
	var concerts []int64
	seed := []store.Concert{
		{Artist: "Fontaines D.C. + Kneecap", Date: day(10), Time: "20:00", Venue: "Zénith Paris", Address: "211 av. Jean Jaurès, 75019 Paris", TicketURL: "https://example.com/billets/fontaines",
			Price: "39,90–52 €",
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
	return nil
}
