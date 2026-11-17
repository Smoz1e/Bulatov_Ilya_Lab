package position

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/google/uuid"

	"github.com/Smoz1e/Bulatov_Ilya_Lab/internal/domain/trip"
	api "github.com/Smoz1e/Bulatov_Ilya_Lab/internal/generated"
	"github.com/Smoz1e/Bulatov_Ilya_Lab/internal/http/domainerror"
	"github.com/Smoz1e/Bulatov_Ilya_Lab/internal/model"
)

type usecase interface {
	Save(context.Context, model.Position) (model.Position, error)
	List(context.Context, uuid.UUID) ([]model.Position, error)
}

type Handler struct{ usecase usecase }

func New(value usecase) *Handler { return &Handler{usecase: value} }

func (handler *Handler) Create(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil || !requiredFields(body) {
		problem(w, r, &trip.ValidationError{Field: "body", Message: "invalid position"})
		return
	}
	var request api.PositionData
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		problem(w, r, &trip.ValidationError{Field: "body", Message: "invalid position"})
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		problem(w, r, &trip.ValidationError{Field: "body", Message: "invalid position"})
		return
	}
	value, err := handler.usecase.Save(r.Context(), model.Position{TripID: id, Coordinates: model.Coordinates{
		Latitude: request.Coordinates.Latitude, Longitude: request.Coordinates.Longitude,
	}, RecordedAt: request.RecordedAt})
	if err != nil {
		problem(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toAPI(value))
}

func requiredFields(body []byte) bool {
	var root map[string]json.RawMessage
	if json.Unmarshal(body, &root) != nil || len(root["recorded_at"]) == 0 || bytes.Equal(root["recorded_at"], []byte("null")) {
		return false
	}
	var coords map[string]json.RawMessage
	if json.Unmarshal(root["coordinates"], &coords) != nil {
		return false
	}
	for _, field := range []string{"latitude", "longitude"} {
		if len(coords[field]) == 0 || bytes.Equal(coords[field], []byte("null")) {
			return false
		}
	}
	return true
}

func (handler *Handler) List(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	positions, err := handler.usecase.List(r.Context(), id)
	if err != nil {
		problem(w, r, err)
		return
	}
	values := make([]api.TripPosition, 0, len(positions))
	for _, position := range positions {
		values = append(values, toAPI(position))
	}
	writeJSON(w, http.StatusOK, api.TripPositionsResponse{Positions: values})
}

func toAPI(value model.Position) api.TripPosition {
	return api.TripPosition{Id: value.ID, TripId: value.TripID, RecordedAt: value.RecordedAt,
		Coordinates: api.Coordinates{Latitude: value.Coordinates.Latitude, Longitude: value.Coordinates.Longitude}}
}

func problem(w http.ResponseWriter, r *http.Request, err error) {
	status, body := domainerror.Problem(err, r.URL.Path)
	w.Header().Set("Content-Type", "application/problem+json")
	writeJSON(w, status, body)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
