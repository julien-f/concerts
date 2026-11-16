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
	for i, c := range []store.Concert{
		{Artist: "Fontaines D.C.", Date: day(10), Time: "20:00", Venue: "Zénith Paris", TicketURL: "https://example.com/billets/fontaines"},
		{Artist: "Yuksek", Date: day(25), Venue: "La Cigale"},
		{Artist: "Zaho de Sagazan", Date: day(60), Time: "20:30", Venue: "L'Olympia", OnSaleDate: day(5)},
		{Artist: "Air", Date: day(-20), Time: "20:00", Venue: "Salle Pleyel"},
		{Artist: "Justice", Date: day(-60), Venue: "Accor Arena"},
	} {
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
		{concerts[0], marc, store.Ticket}, {concerts[0], sam, store.Going}, {concerts[0], lea, store.Interested},
		{concerts[1], marc, store.Interested}, {concerts[3], lea, store.Ticket}, {concerts[3], sam, store.Ticket},
	} {
		if err := st.SetStatus(ctx, g.ID, s.concert, s.member, s.status); err != nil {
			return err
		}
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
