package position

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Smoz1e/Bulatov_Ilya_Lab/internal/domain/trip"
	"github.com/Smoz1e/Bulatov_Ilya_Lab/internal/model"
)

type fakeRepo struct {
	value trip.Trip
	saved int
}

func (repo *fakeRepo) GetForUpdate(context.Context, uuid.UUID) (trip.Trip, error) {
	return repo.value, nil
}

func (repo *fakeRepo) GetByID(context.Context, uuid.UUID) (trip.Trip, error) { return repo.value, nil }

func (repo *fakeRepo) SavePosition(_ context.Context, p model.Position) (model.Position, error) {
	repo.saved++
	p.ID = 42
	return p, nil
}

func (repo *fakeRepo) ListPositions(context.Context, uuid.UUID) ([]model.Position, error) {
	return []model.Position{}, nil
}

type fakeTx struct{ err error }

func (tx fakeTx) Do(ctx context.Context, fn func(context.Context) error) error {
	if err := fn(ctx); err != nil {
		return err
	}
	return tx.err
}

type fakeCancel struct{ calls int }

func (c *fakeCancel) Cancel(uuid.UUID) { c.calls++ }

type fakeMetrics struct{ saved int }

func (m *fakeMetrics) PositionSaved(context.Context) { m.saved++ }

func TestSavePositionRequiresActiveTrip(t *testing.T) {
	repo := &fakeRepo{value: trip.Trip{Status: trip.StatusCompleted}}
	cancellation := &fakeCancel{}
	metrics := &fakeMetrics{}
	usecase := New(repo, fakeTx{}, cancellation, metrics, time.Now)
	_, err := usecase.Save(context.Background(), model.Position{TripID: uuid.New(), RecordedAt: time.Now()})
	if !errors.Is(err, trip.ErrTripCompleted) || repo.saved != 0 || cancellation.calls != 0 {
		t.Fatalf("error=%v saved=%d cancelled=%d", err, repo.saved, cancellation.calls)
	}
}

func TestSavePositionCancelsOnlyAfterCommit(t *testing.T) {
	repo := &fakeRepo{value: trip.Trip{Status: trip.StatusActive}}
	cancellation := &fakeCancel{}
	metrics := &fakeMetrics{}
	failure := errors.New("commit failed")
	usecase := New(repo, fakeTx{err: failure}, cancellation, metrics, time.Now)
	_, err := usecase.Save(context.Background(), model.Position{TripID: uuid.New(), RecordedAt: time.Now()})
	if !errors.Is(err, failure) || cancellation.calls != 0 || metrics.saved != 0 {
		t.Fatalf("error=%v cancelled=%d metrics=%d", err, cancellation.calls, metrics.saved)
	}
}

func TestSaveRejectsInvalidCoordinatesAndFutureTimestamp(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	repo := &fakeRepo{value: trip.Trip{Status: trip.StatusActive}}
	usecase := New(repo, fakeTx{}, &fakeCancel{}, &fakeMetrics{}, func() time.Time { return now })
	for _, p := range []model.Position{
		{TripID: uuid.New(), Coordinates: model.Coordinates{Latitude: 91}, RecordedAt: now},
		{TripID: uuid.New(), Coordinates: model.Coordinates{Longitude: -181}, RecordedAt: now},
		{TripID: uuid.New(), RecordedAt: now.Add(time.Minute + time.Second)},
		{TripID: uuid.New()},
	} {
		var validation *trip.ValidationError
		if _, err := usecase.Save(context.Background(), p); !errors.As(err, &validation) {
			t.Errorf("position %#v: got %v", p, err)
		}
	}
	if repo.saved != 0 {
		t.Fatalf("invalid coordinates were saved: %d", repo.saved)
	}
}

func TestSavePositionCancelsQueueAfterSuccess(t *testing.T) {
	repo := &fakeRepo{value: trip.Trip{Status: trip.StatusActive}}
	cancellation := &fakeCancel{}
	metrics := &fakeMetrics{}
	usecase := New(repo, fakeTx{}, cancellation, metrics, time.Now)
	saved, err := usecase.Save(context.Background(), model.Position{TripID: uuid.New(), RecordedAt: time.Now()})
	if err != nil || saved.ID != 42 || repo.saved != 1 || cancellation.calls != 1 || metrics.saved != 1 {
		t.Fatalf("error=%v saved=%d cancelled=%d metrics=%d", err, repo.saved, cancellation.calls, metrics.saved)
	}
}
