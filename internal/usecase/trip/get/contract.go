package get

import (
	"context"

	trip "github.com/Smoz1e/Bulatov_Ilya_Lab/internal/domain/trip"
	"github.com/google/uuid"
)

//go:generate go tool mockgen -source contract.go -destination mocks_test.go -package get_test -mock_names tripRepository=MockTripRepository
type tripRepository interface {
	GetByID(context.Context, uuid.UUID) (trip.Trip, error)
}
