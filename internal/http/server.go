package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	trip "github.com/Smoz1e/Bulatov_Ilya_Lab/internal/domain/trip"
	api "github.com/Smoz1e/Bulatov_Ilya_Lab/internal/generated"
	"github.com/Smoz1e/Bulatov_Ilya_Lab/internal/http/domainerror"
	"github.com/Smoz1e/Bulatov_Ilya_Lab/internal/model"
)

type pinger interface {
	Ping(context.Context) error
}

type createTripUsecase interface {
	Create(context.Context, model.CreateTripInput) (trip.Trip, error)
}

type getTripUsecase interface {
	Get(context.Context, api.TripId) (trip.Trip, error)
}

type finishTripUsecase interface {
	Finish(context.Context, api.TripId) (trip.Trip, error)
}

type Server struct {
	api.Unimplemented
	create       createTripUsecase
	get          getTripUsecase
	finish       finishTripUsecase
	pinger       pinger
	queryTimeout time.Duration
}

func NewServer(
	create createTripUsecase,
	get getTripUsecase,
	finish finishTripUsecase,
	pinger pinger,
	queryTimeout time.Duration,
) *Server {
	return &Server{
		create:       create,
		get:          get,
		finish:       finish,
		pinger:       pinger,
		queryTimeout: queryTimeout,
	}
}

func (server *Server) CreateTrip(w http.ResponseWriter, r *http.Request, _ api.CreateTripParams) {
	var request api.TripData
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		server.writeProblem(w, r, &trip.ValidationError{Field: "body", Message: err.Error()})
		return
	}
	if err := ensureSingleJSONValue(decoder); err != nil {
		server.writeProblem(w, r, &trip.ValidationError{Field: "body", Message: err.Error()})
		return
	}

	value, err := server.create.Create(r.Context(), toCreateInput(request))
	if err != nil {
		server.writeProblem(w, r, err)
		return
	}

	w.Header().Set("Location", "/api/v1/trips/"+value.ID.String())
	writeJSON(w, http.StatusCreated, toAPITrip(value))
}

func (server *Server) GetTrip(w http.ResponseWriter, r *http.Request, id api.TripId) {
	value, err := server.get.Get(r.Context(), id)
	if err != nil {
		server.writeProblem(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, toAPITrip(value))
}

func (server *Server) FinishTrip(w http.ResponseWriter, r *http.Request, id api.TripId) {
	value, err := server.finish.Finish(r.Context(), id)
	if err != nil {
		server.writeProblem(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, toAPITrip(value))
}

func (server *Server) Health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, api.HealthResponse{Status: api.Ok})
}

func (server *Server) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), server.queryTimeout)
	defer cancel()

	if err := server.pinger.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, api.HealthResponse{Status: api.Unavailable})
		return
	}

	writeJSON(w, http.StatusOK, api.HealthResponse{Status: api.Ok})
}

func (server *Server) writeProblem(w http.ResponseWriter, r *http.Request, err error) {
	WriteProblem(w, r, err)
}

func HandleOpenAPIError(w http.ResponseWriter, r *http.Request, err error) {
	WriteProblem(w, r, &trip.ValidationError{Field: "request", Message: err.Error()})
}

func WriteProblem(w http.ResponseWriter, r *http.Request, err error) {
	status, body := domainerror.Problem(err, r.URL.Path)
	w.Header().Set("Content-Type", "application/problem+json")
	writeJSON(w, status, body)
}

func ensureSingleJSONValue(decoder *json.Decoder) error {
	var additionalValue any
	err := decoder.Decode(&additionalValue)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return err
	}

	return errors.New("request body must contain a single JSON object")
}

func toCreateInput(request api.TripData) model.CreateTripInput {
	return model.CreateTripInput{
		UserID:   request.UserId,
		DriverID: request.DriverId,
		StartPoint: model.Coordinates{
			Latitude:  request.StartPoint.Latitude,
			Longitude: request.StartPoint.Longitude,
		},
		EndPoint: model.Coordinates{
			Latitude:  request.EndPoint.Latitude,
			Longitude: request.EndPoint.Longitude,
		},
		Price: request.Price,
	}
}

func toAPITrip(value trip.Trip) api.Trip {
	return api.Trip{
		DriverId: value.DriverID,
		EndPoint: api.Coordinates{
			Latitude:  value.EndPoint.Latitude,
			Longitude: value.EndPoint.Longitude,
		},
		FinishedAt:     value.FinishedAt,
		Id:             value.ID,
		LastPositionAt: value.LastPositionAt,
		Price:          value.Price,
		StartPoint: api.Coordinates{
			Latitude:  value.StartPoint.Latitude,
			Longitude: value.StartPoint.Longitude,
		},
		StartedAt: value.StartedAt,
		Status:    api.TripStatus(value.Status),
		UserId:    value.UserID,
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		return
	}
}
