package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"parteez/internal/domain/artwork"
	"parteez/internal/domain/events"
	"parteez/internal/domain/shared"
	"parteez/internal/domain/venue"

	pgxtrm "github.com/avito-tech/go-transaction-manager/drivers/pgxv5/v2"
	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type EventStorage struct {
	pool    *pgxpool.Pool
	context *pgxtrm.CtxGetter
}

func NewEventStorage(pool *pgxpool.Pool, context *pgxtrm.CtxGetter) *EventStorage {
	return &EventStorage{
		pool:    pool,
		context: context,
	}
}

func (s *EventStorage) Save(ctx context.Context, event *events.Event) error {
	// id := "DEFAULT"
	// if event.ID != 0 {
	// 	id = fmt.Sprintf("%d", event.ID)
	// }

	row, err := eventToRow(event)
	if err != nil {
		return err
	}

	db := s.context.DefaultTrOrDB(ctx, s.pool)

	query := `
INSERT INTO events (
	id,
	title,
	description,
	genres,
	artwork_id,
	date,
	venue_id,
	lineup,
	age_restriction,
	promoter,
	tickets_url,
	tickets,
	is_draft,
	published_at,
	created_at,
	updated_at
)
VALUES (
	COALESCE(NULLIF($1, 0), (SELECT nextval('events_id_seq'))),
	$2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16
)
ON CONFLICT (id)
	DO UPDATE SET
		title = EXCLUDED.title,
		description = EXCLUDED.description,
		genres = EXCLUDED.genres,
		artwork_id = EXCLUDED.artwork_id,
		date = EXCLUDED.date,
		venue_id = EXCLUDED.venue_id,
		lineup = EXCLUDED.lineup,
		age_restriction = EXCLUDED.age_restriction,
		promoter = EXCLUDED.promoter,
		tickets_url = EXCLUDED.tickets_url,
		tickets = EXCLUDED.tickets,
		is_draft = EXCLUDED.is_draft,
		published_at = EXCLUDED.published_at,
		updated_at = EXCLUDED.updated_at
RETURNING id`

	result := db.QueryRow(ctx, query,
		row.ID,
		row.Title,
		row.Description,
		row.Genres,
		row.ArtworkID,
		row.Date,
		row.VenueID,
		row.LineUp,
		row.AgeRestriction,
		row.Promoter,
		row.TicketsURL,
		row.Tickets,
		row.IsDraft,
		row.PublishedAt,
		row.CreatedAt,
		row.UpdatedAt,
	)
	if err := result.Scan(&row.ID); err != nil {
		var pgerr *pgconn.PgError
		if errors.As(err, &pgerr) {
			switch pgerr.Code {
			case "23505":
				if pgerr.ConstraintName == "events_title_date_key" {
					return events.ErrEventAlreadyExists
				}
			case "23503":
				if strings.Contains(pgerr.ConstraintName, "artwork_id") {
					return fmt.Errorf("%w: %w", events.ErrEvent, artwork.ErrArtworkNotFound)
				}
				if strings.Contains(pgerr.ConstraintName, "venue_id") {
					return fmt.Errorf("%w: %w", events.ErrEvent, venue.ErrVenueNotFound)
				}
			}
		}
		return fmt.Errorf("%w: %w", shared.ErrDatabase, err)
	}

	event.ID, err = events.NewEventID(row.ID)
	if err != nil {
		return err
	}

	return nil
}

func (s *EventStorage) FindByDate(ctx context.Context, fromDate time.Time, toDate time.Time) ([]*events.Event, error) {
	const query = "SELECT * FROM events WHERE date && tsrange($1, $2, '[]')"
	db := s.context.DefaultTrOrDB(ctx, s.pool)
	result, err := db.Query(ctx, query, fromDate.Format(time.DateTime), toDate.Format(time.DateTime))
	if err != nil {
		return nil, err
	}
	rows, err := pgx.CollectRows(result, pgx.RowToStructByName[eventRow])
	if err != nil {
		return nil, err
	}
	// var rows []eventRow

	// if err := pgxscan.Select(ctx, db, &rows, query,
	// 	fromDate.Format(time.DateTime), toDate.Format(time.DateTime),
	// ); err != nil {
	// 	return nil, err
	// }

	results := make([]*events.Event, len(rows))
	for i, row := range rows {
		var err error
		results[i], err = rowToEvent(row)
		if err != nil {
			return nil, err
		}
	}

	return results, nil
}

func (s *EventStorage) FindAll(ctx context.Context) ([]*events.Event, error) {
	panic("not implemented")
}

func (s *EventStorage) FindById(ctx context.Context, id events.EventID) (*events.Event, error) {
	const query = "SELECT * FROM event WHERE id = $1"

	var row eventRow
	err := pgxscan.Get(ctx, s.pool, &row, query, id)
	if err != nil {
		return nil, err
	}

	event, err := rowToEvent(row)
	if err != nil {
		return nil, err
	}

	return event, nil
}

func (s *EventStorage) Delete(ctx context.Context, id int) error {
	_, err := s.pool.Exec(ctx, "DELETE FROM event WHERE id = $1", id)
	return err
}
