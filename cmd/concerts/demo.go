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
	for i, c := range []store.Concert{
		{Artist: "Fontaines D.C.", Date: day(10), Time: "20:00", Venue: "Zénith Paris", TicketURL: "https://example.com/billets/fontaines"},
		{Artist: "Yuksek", Date: day(25), Venue: "La Cigale"},
		{Artist: "Zaho de Sagazan", Date: day(60), Time: "20:30", Venue: "L'Olympia", OnSaleDate: day(5)},
		{Artist: "Air", Date: day(-20), Time: "20:00", Venue: "Salle Pleyel"},
		{Artist: "Justice", Date: day(-60), Venue: "Accor Arena"},
	} {
		if _, err := st.CreateConcert(ctx, g.ID, ids[i%len(ids)], c); err != nil {
			return err
		}
	}
	return nil
}
