package push

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	pushapi "github.com/Smoz1e/Bulatov_Ilya_Lab/internal/generated/push"
)

type Client struct {
	endpoint string
	http     *http.Client
}

func New(baseURL string, timeout time.Duration) (*Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("invalid PUSH_HTTP_URL")
	}
	if timeout <= 0 {
		return nil, fmt.Errorf("push timeout must be positive")
	}
	return &Client{endpoint: strings.TrimRight(baseURL, "/") + "/api/v1/push", http: &http.Client{
		Timeout: timeout, Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment, MaxIdleConns: 20,
			MaxIdleConnsPerHost: 10, IdleConnTimeout: 90 * time.Second, ResponseHeaderTimeout: timeout,
		},
	}}, nil
}

func (client *Client) RequestPosition(ctx context.Context, trip, driver uuid.UUID) error {
	ctx, span := otel.Tracer("trip-service").Start(ctx, "push request position")
	defer span.End()
	data := map[string]string{"trip_id": trip.String()}
	body, err := json.Marshal(pushapi.PushRequest{RequestId: uuid.NewString(), RecipientId: driver, Kind: pushapi.REQUESTPOSITION, Data: &data})
	if err != nil {
		return fmt.Errorf("encode push request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create push request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(request.Header))
	response, err := client.http.Do(request)
	if err != nil {
		return fmt.Errorf("push transport: %w", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("push returned HTTP %d", response.StatusCode)
	}
	return nil
}

func (client *Client) Close() { client.http.CloseIdleConnections() }
