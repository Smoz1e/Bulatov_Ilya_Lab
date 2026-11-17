package telemetry

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestPositionMetricsRecordQueueAndResults(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	t.Cleanup(func() { otel.SetMeterProvider(previous); _ = provider.Shutdown(context.Background()) })
	metrics, err := NewMetrics()
	if err != nil {
		t.Fatal(err)
	}
	metrics.PositionSaved(context.Background())
	metrics.StaleCheck(context.Background(), "queued")
	metrics.QueueChange(context.Background(), 1)
	metrics.WorkerChange(context.Background(), 1)
	metrics.PositionRequest(context.Background(), "success", 0.01)
	var output metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, scope := range output.ScopeMetrics {
		for _, item := range scope.Metrics {
			found[item.Name] = true
		}
	}
	for _, name := range []string{"tripgo.positions.saved", "tripgo.stale_checks", "tripgo.position_requests.queue.size", "tripgo.position_requests.workers.active", "tripgo.position_requests", "tripgo.position_requests.duration"} {
		if !found[name] {
			t.Errorf("missing metric %s", name)
		}
	}
}
