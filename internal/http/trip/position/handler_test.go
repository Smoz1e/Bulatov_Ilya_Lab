package position

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Smoz1e/Bulatov_Ilya_Lab/internal/model"
)

type fakeService struct{ saved bool }

func (service *fakeService) Save(_ context.Context, value model.Position) (model.Position, error) {
	service.saved = true
	value.ID = 42
	return value, nil
}
func (*fakeService) List(context.Context, uuid.UUID) ([]model.Position, error) {
	return []model.Position{}, nil
}

func TestCreatePositionRejectsMissingFields(t *testing.T) {
	for _, body := range []string{
		`{"recorded_at":"2026-01-01T00:00:00Z"}`,
		`{"coordinates":{"latitude":0},"recorded_at":"2026-01-01T00:00:00Z"}`,
		`{"coordinates":{"latitude":0,"longitude":0}}`,
		`{"coordinates":{"latitude":0,"longitude":0},"recorded_at":"2026-01-01T00:00:00Z","other":1}`,
	} {
		service := &fakeService{}
		request := httptest.NewRequest(http.MethodPost, "/positions", strings.NewReader(body))
		response := httptest.NewRecorder()
		New(service).Create(response, request, uuid.New())
		if response.Code != http.StatusBadRequest || service.saved {
			t.Errorf("body %s: status=%d saved=%v", body, response.Code, service.saved)
		}
	}
}

func TestCreatePositionReturnsCreated(t *testing.T) {
	service := &fakeService{}
	body := `{"coordinates":{"latitude":0,"longitude":0},"recorded_at":"` + time.Now().UTC().Format(time.RFC3339) + `"}`
	response := httptest.NewRecorder()
	New(service).Create(response, httptest.NewRequest(http.MethodPost, "/positions", strings.NewReader(body)), uuid.New())
	if response.Code != http.StatusCreated || !service.saved {
		t.Fatalf("status=%d saved=%v", response.Code, service.saved)
	}
}
