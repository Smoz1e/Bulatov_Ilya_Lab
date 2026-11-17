package position

import (
	"context"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/Smoz1e/Bulatov_Ilya_Lab/internal/domain/trip"
	"github.com/Smoz1e/Bulatov_Ilya_Lab/internal/model"
)

type repository interface {
	GetForUpdate(context.Context, uuid.UUID) (trip.Trip, error)
	GetByID(context.Context, uuid.UUID) (trip.Trip, error)
	SavePosition(context.Context, model.Position) (model.Position, error)
	ListPositions(context.Context, uuid.UUID) ([]model.Position, error)
}

type transactionManager interface {
	Do(context.Context, func(context.Context) error) error
}

type canceller interface{ Cancel(uuid.UUID) }
type metrics interface{ PositionSaved(context.Context) }

type Usecase struct {
	repository repository
	tx         transactionManager
	cancel     canceller
	metrics    metrics
	now        func() time.Time
}

func New(repo repository, tx transactionManager, cancel canceller, metrics metrics, now func() time.Time) *Usecase {
	return &Usecase{repository: repo, tx: tx, cancel: cancel, metrics: metrics, now: now}
}

func (usecase *Usecase) Save(ctx context.Context, position model.Position) (model.Position, error) {
	if math.IsNaN(position.Coordinates.Latitude) || math.Abs(position.Coordinates.Latitude) > 90 {
		return model.Position{}, &trip.ValidationError{Field: "coordinates.latitude", Message: "must be between -90 and 90"}
	}
	if math.IsNaN(position.Coordinates.Longitude) || math.Abs(position.Coordinates.Longitude) > 180 {
		return model.Position{}, &trip.ValidationError{Field: "coordinates.longitude", Message: "must be between -180 and 180"}
	}
	if position.RecordedAt.IsZero() || position.RecordedAt.After(usecase.now().Add(time.Minute)) {
		return model.Position{}, &trip.ValidationError{Field: "recorded_at", Message: "must be a valid timestamp at most one minute in the future"}
	}
	var saved model.Position
	err := usecase.tx.Do(ctx, func(txCtx context.Context) error {
		value, err := usecase.repository.GetForUpdate(txCtx, position.TripID)
		if err != nil {
			return err
		}
		if value.Status != trip.StatusActive {
			return trip.ErrTripCompleted
		}
		saved, err = usecase.repository.SavePosition(txCtx, position)
		return err
	})
	if err != nil {
		return model.Position{}, err
	}
	usecase.cancel.Cancel(position.TripID)
	usecase.metrics.PositionSaved(ctx)
	return saved, nil
}

func (usecase *Usecase) List(ctx context.Context, id uuid.UUID) ([]model.Position, error) {
	if _, err := usecase.repository.GetByID(ctx, id); err != nil {
		return nil, err
	}
	return usecase.repository.ListPositions(ctx, id)
}
