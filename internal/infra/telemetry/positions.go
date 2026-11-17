package telemetry

import (
	"context"
	"fmt"
	"sync/atomic"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type positionMetrics struct {
	saved    metric.Int64Counter
	stale    metric.Int64Counter
	requests metric.Int64Counter
	duration metric.Float64Histogram
	queue    atomic.Int64
	active   atomic.Int64
}

func newPositionMetrics(meter metric.Meter) (*positionMetrics, error) {
	value := &positionMetrics{}
	var err error
	if value.saved, err = meter.Int64Counter("tripgo.positions.saved"); err != nil {
		return nil, fmt.Errorf("positions saved metric: %w", err)
	}
	if value.stale, err = meter.Int64Counter("tripgo.stale_checks"); err != nil {
		return nil, fmt.Errorf("stale checks metric: %w", err)
	}
	if value.requests, err = meter.Int64Counter("tripgo.position_requests"); err != nil {
		return nil, fmt.Errorf("position requests metric: %w", err)
	}
	if value.duration, err = meter.Float64Histogram("tripgo.position_requests.duration", metric.WithUnit("s")); err != nil {
		return nil, fmt.Errorf("position duration metric: %w", err)
	}
	queue, err := meter.Int64ObservableGauge("tripgo.position_requests.queue.size")
	if err != nil {
		return nil, fmt.Errorf("position queue metric: %w", err)
	}
	workers, err := meter.Int64ObservableGauge("tripgo.position_requests.workers.active")
	if err != nil {
		return nil, fmt.Errorf("position workers metric: %w", err)
	}
	_, err = meter.RegisterCallback(func(_ context.Context, observer metric.Observer) error {
		observer.ObserveInt64(queue, value.queue.Load())
		observer.ObserveInt64(workers, value.active.Load())
		return nil
	}, queue, workers)
	if err != nil {
		return nil, fmt.Errorf("register position metrics: %w", err)
	}
	return value, nil
}

func (metrics *positionMetrics) PositionSaved(ctx context.Context) { metrics.saved.Add(ctx, 1) }
func (metrics *positionMetrics) StaleCheck(ctx context.Context, result string) {
	metrics.stale.Add(ctx, 1, metric.WithAttributes(attribute.String("result", result)))
}

func (metrics *positionMetrics) QueueChange(_ context.Context, delta int64) { metrics.queue.Add(delta) }

func (metrics *positionMetrics) WorkerChange(_ context.Context, delta int64) {
	metrics.active.Add(delta)
}

func (metrics *positionMetrics) PositionRequest(ctx context.Context, result string, seconds float64) {
	attrs := metric.WithAttributes(attribute.String("result", result))
	metrics.requests.Add(ctx, 1, attrs)
	if result != "cancelled" {
		metrics.duration.Record(ctx, seconds, attrs)
	}
}
