package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// ProductEvent — бір өнім оқиғасы. Props — тек рұқсат етілген кілттер мен
// мәндерден тұратын JSON (productevents каталогы тексерген).
type ProductEvent struct {
	Name  string
	Props string
	// ClientTime — құрылғы сағаты; бос болса NULL жазылады.
	ClientTime time.Time
}

// InsertProductEvents — бір сұраныстың оқиғалары бір транзакцияда.
func (s *Store) InsertProductEvents(ctx context.Context, userID, platform, appVersion string,
	events []ProductEvent, at time.Time) error {
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO product_events (id, user_id, name, props, platform, app_version, client_ts, created_at)
			VALUES (?,?,?,?,?,?,?,?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, e := range events {
			if _, err := stmt.ExecContext(ctx, traits.NewID(), userID, e.Name, e.Props, platform, appVersion,
				msPtr(&e.ClientTime), ms(at)); err != nil {
				return err
			}
		}
		return nil
	})
}

// ProductEventCounts — кезеңдегі оқиғалар саны атауы бойынша, көбі алдымен.
func (s *Store) ProductEventCounts(ctx context.Context, from, to time.Time) ([]Point, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT name, COUNT(*) FROM product_events
		WHERE created_at BETWEEN ? AND ?
		GROUP BY name ORDER BY 2 DESC, 1`, ms(from), ms(to))
	return collectPoints(rows, err)
}
