package create

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	trip "github.com/Smoz1e/Bulatov_Ilya_Lab/internal/domain/trip"
	"github.com/Smoz1e/Bulatov_Ilya_Lab/internal/http/domainerror"
	"github.com/Smoz1e/Bulatov_Ilya_Lab/internal/model"
)

type Handler struct{ usecase usecase }

func New(usecase usecase) *Handler { return &Handler{usecase: usecase} }

func (handler *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var request struct {
		UserID     uuid.UUID         `json:"user_id"`
		DriverID   uuid.UUID         `json:"driver_id"`
		StartPoint model.Coordinates `json:"start_point"`
		EndPoint   model.Coordinates `json:"end_point"`
		Price      int64             `json:"price"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		handler.writeProblem(w, r, &trip.ValidationError{Field: "body", Message: err.Error()})
		return
	}
	input := model.CreateTripInput{UserID: request.UserID, DriverID: request.DriverID, StartPoint: request.StartPoint, EndPoint: request.EndPoint, Price: request.Price}
	value, err := handler.usecase.Create(r.Context(), input)
	if err != nil {
		handler.writeProblem(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Location", "/api/v1/trips/"+value.ID.String())
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(value)
}

func (handler *Handler) writeProblem(w http.ResponseWriter, r *http.Request, err error) {
	status, body := domainerror.Problem(err, r.URL.Path)
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
