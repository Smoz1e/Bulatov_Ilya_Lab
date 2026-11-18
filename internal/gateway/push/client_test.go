package push

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRequestPositionSendsContract(t *testing.T) {
	trip, driver := uuid.New(), uuid.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/push" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			RequestID string            `json:"request_id"`
			Recipient string            `json:"recipient_id"`
			Kind      string            `json:"kind"`
			Data      map[string]string `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.RequestID == "" || body.Recipient != driver.String() || body.Kind != "REQUEST_POSITION" || body.Data["trip_id"] != trip.String() {
			t.Errorf("unexpected body: %#v", body)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	client, err := New(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.RequestPosition(context.Background(), trip, driver); err != nil {
		t.Fatal(err)
	}
}

func TestRequestPositionReportsHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer server.Close()
	client, err := New(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.RequestPosition(context.Background(), uuid.New(), uuid.New()); err == nil {
		t.Fatal("expected error for 503")
	}
}
