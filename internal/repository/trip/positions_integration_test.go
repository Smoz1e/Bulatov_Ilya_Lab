//go:build integration

package trip_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Smoz1e/Bulatov_Ilya_Lab/internal/infra/postgres/transaction"
	"github.com/Smoz1e/Bulatov_Ilya_Lab/internal/model"
	repository "github.com/Smoz1e/Bulatov_Ilya_Lab/internal/repository/trip"
)

func TestPositionUpdatesLatestTimeAndRollsBack(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	repo := repository.New(pool, 3*time.Second)
	manager := transaction.New(pool)
	value := integrationTrip(t)
	t.Cleanup(func() { cleanupTrip(t, pool, value.ID) })
	if err := repo.Save(ctx, value); err != nil {
		t.Fatal(err)
	}
	newTime := time.Now().UTC().Truncate(time.Microsecond)
	for _, recorded := range []time.Time{newTime, newTime.Add(-time.Minute)} {
		err := manager.Do(ctx, func(txCtx context.Context) error {
			_, err := repo.SavePosition(txCtx, model.Position{TripID: value.ID, Coordinates: model.Coordinates{Latitude: 1, Longitude: 2}, RecordedAt: recorded})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	var last time.Time
	if err := pool.QueryRow(ctx, "SELECT last_position_at FROM trips WHERE id=$1", value.ID).Scan(&last); err != nil {
		t.Fatal(err)
	}
	if !last.Equal(newTime) {
		t.Fatalf("last_position_at = %s, want %s", last, newTime)
	}
	failure := errors.New("rollback")
	err := manager.Do(ctx, func(txCtx context.Context) error {
		if _, err := repo.SavePosition(txCtx, model.Position{TripID: value.ID, Coordinates: model.Coordinates{Latitude: 1, Longitude: 2}, RecordedAt: newTime.Add(time.Minute)}); err != nil {
			return err
		}
		return failure
	})
	if !errors.Is(err, failure) {
		t.Fatalf("rollback error = %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT last_position_at, (SELECT count(*) FROM trip_positions WHERE trip_id=$1) FROM trips WHERE id=$1", value.ID).Scan(&last, &count); err != nil {
		t.Fatal(err)
	}
	if !last.Equal(newTime) || count != 2 {
		t.Fatalf("rollback left last=%s count=%d", last, count)
	}
}

func TestStaleTripsExcludeFreshAndCompleted(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	repo := repository.New(pool, 3*time.Second)
	stale, fresh, finished := integrationTrip(t), integrationTrip(t), integrationTrip(t)
	stale.StartedAt = time.Now().Add(-2 * time.Hour)
	finished.StartedAt = stale.StartedAt
	if err := repo.Save(ctx, stale); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, finished); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{stale.ID.String(), fresh.ID.String(), finished.ID.String()} {
		id := id
		t.Cleanup(func() {
			if _, err := pool.Exec(ctx, "DELETE FROM trips WHERE id=$1", id); err != nil {
				t.Error(err)
			}
		})
	}
	if _, err := pool.Exec(ctx, "UPDATE trips SET status='completed', finished_at=now() WHERE id=$1", finished.ID); err != nil {
		t.Fatal(err)
	}
	values, err := repo.StaleTrips(ctx, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, value := range values {
		found[value.TripID.String()] = true
	}
	if !found[stale.ID.String()] || found[fresh.ID.String()] || found[finished.ID.String()] {
		t.Fatalf("unexpected stale trips: %v", found)
	}
}
