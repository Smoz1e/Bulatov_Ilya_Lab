package trip

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Smoz1e/Bulatov_Ilya_Lab/internal/infra/postgres/transaction"
	"github.com/Smoz1e/Bulatov_Ilya_Lab/internal/model"
)

func (repository *Repository) SavePosition(ctx context.Context, position model.Position) (model.Position, error) {
	queryCtx, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	db := transaction.FromContext(queryCtx, repository.pool)
	if err := db.QueryRow(queryCtx, `INSERT INTO trip_positions (trip_id, latitude, longitude, recorded_at)
		VALUES ($1, $2, $3, $4) RETURNING id`, position.TripID,
		position.Coordinates.Latitude, position.Coordinates.Longitude, position.RecordedAt).Scan(&position.ID); err != nil {
		return model.Position{}, fmt.Errorf("insert position: %w", err)
	}
	if _, err := db.Exec(queryCtx, `UPDATE trips SET last_position_at = GREATEST(last_position_at, $2),
		updated_at = now() WHERE id = $1`, position.TripID, position.RecordedAt); err != nil {
		return model.Position{}, fmt.Errorf("update last position: %w", err)
	}
	return position, nil
}

func (repository *Repository) ListPositions(ctx context.Context, id uuid.UUID) ([]model.Position, error) {
	queryCtx, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	rows, err := transaction.FromContext(queryCtx, repository.pool).Query(queryCtx, `SELECT id, trip_id, latitude, longitude, recorded_at
		FROM trip_positions WHERE trip_id = $1 ORDER BY recorded_at, id`, id)
	if err != nil {
		return nil, fmt.Errorf("list positions: %w", err)
	}
	defer rows.Close()
	positions := make([]model.Position, 0)
	for rows.Next() {
		var position model.Position
		if err := rows.Scan(&position.ID, &position.TripID, &position.Coordinates.Latitude,
			&position.Coordinates.Longitude, &position.RecordedAt); err != nil {
			return nil, fmt.Errorf("scan position: %w", err)
		}
		positions = append(positions, position)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read positions: %w", err)
	}
	return positions, nil
}

func (repository *Repository) StaleTrips(ctx context.Context, after time.Duration) ([]model.StaleTrip, error) {
	queryCtx, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	rows, err := transaction.FromContext(queryCtx, repository.pool).Query(queryCtx, `SELECT id, driver_id FROM trips
		WHERE status = 'active' AND COALESCE(last_position_at, started_at) < now() - ($1 * interval '1 second')
		ORDER BY COALESCE(last_position_at, started_at), id`, after.Seconds())
	if err != nil {
		return nil, fmt.Errorf("select stale trips: %w", err)
	}
	defer rows.Close()
	var trips []model.StaleTrip
	for rows.Next() {
		var trip model.StaleTrip
		if err := rows.Scan(&trip.TripID, &trip.DriverID); err != nil {
			return nil, fmt.Errorf("scan stale trip: %w", err)
		}
		trips = append(trips, trip)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read stale trips: %w", err)
	}
	return trips, nil
}

func (repository *Repository) IsStale(ctx context.Context, id uuid.UUID, after time.Duration) (bool, error) {
	queryCtx, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	var stale bool
	err := transaction.FromContext(queryCtx, repository.pool).QueryRow(queryCtx, `SELECT EXISTS (
		SELECT 1 FROM trips WHERE id = $1 AND status = 'active'
		AND COALESCE(last_position_at, started_at) < now() - ($2 * interval '1 second'))`, id, after.Seconds()).Scan(&stale)
	if err != nil {
		return false, fmt.Errorf("check stale trip: %w", err)
	}
	return stale, nil
}
