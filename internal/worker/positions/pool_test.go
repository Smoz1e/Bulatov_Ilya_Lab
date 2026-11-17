package positions

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/goleak"

	"github.com/Smoz1e/Bulatov_Ilya_Lab/internal/model"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

type fakeTicker struct{ ticks chan time.Time }

func (ticker *fakeTicker) C() <-chan time.Time { return ticker.ticks }
func (*fakeTicker) Stop()                      {}

type fakeScanner struct {
	trips    []model.StaleTrip
	scanned  chan struct{}
	notStale bool
}

func (scanner *fakeScanner) StaleTrips(context.Context, time.Duration) ([]model.StaleTrip, error) {
	scanner.scanned <- struct{}{}
	return scanner.trips, nil
}

func (scanner *fakeScanner) IsStale(context.Context, uuid.UUID, time.Duration) (bool, error) {
	return !scanner.notStale, nil
}

type fakePush struct {
	calls   chan uuid.UUID
	release chan struct{}
	fail    bool
	mu      sync.Mutex
}

func (push *fakePush) RequestPosition(ctx context.Context, trip, _ uuid.UUID) error {
	push.calls <- trip
	if push.release != nil {
		select {
		case <-push.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	push.mu.Lock()
	defer push.mu.Unlock()
	if push.fail {
		push.fail = false
		return errors.New("push failed")
	}
	return nil
}

type fakeMetrics struct {
	stale    chan EnqueueResult
	finished chan string
}

func (m *fakeMetrics) StaleCheck(_ context.Context, result string) { m.stale <- EnqueueResult(result) }
func (*fakeMetrics) QueueChange(context.Context, int64)            {}
func (*fakeMetrics) WorkerChange(context.Context, int64)           {}
func (m *fakeMetrics) PositionRequest(_ context.Context, result string, _ float64) {
	m.finished <- result
}

func TestPoolProcessesAfterPushFailure(t *testing.T) {
	ids := []uuid.UUID{uuid.New(), uuid.New()}
	scanner := &fakeScanner{trips: []model.StaleTrip{{TripID: ids[0]}, {TripID: ids[1]}}, scanned: make(chan struct{}, 1)}
	push := &fakePush{calls: make(chan uuid.UUID, 2), fail: true}
	metrics := &fakeMetrics{stale: make(chan EnqueueResult, 2), finished: make(chan string, 2)}
	ticker := &fakeTicker{ticks: make(chan time.Time, 1)}
	pool := NewPool(scanner, push, NewQueue(2), metrics, slog.New(slog.NewTextHandler(io.Discard, nil)), ticker, time.Minute, time.Second, 1)
	pool.Start()
	ticker.ticks <- time.Now()
	<-scanner.scanned
	first, second := <-push.calls, <-push.calls
	if first != ids[0] || second != ids[1] {
		t.Fatalf("calls %s, %s", first, second)
	}
	if a, b := <-metrics.finished, <-metrics.finished; a != "error" || b != "success" {
		t.Fatalf("results %s, %s", a, b)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := pool.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestPoolDrainsQueuedTasksOnStop(t *testing.T) {
	ids := []uuid.UUID{uuid.New(), uuid.New()}
	scanner := &fakeScanner{trips: []model.StaleTrip{{TripID: ids[0]}, {TripID: ids[1]}}, scanned: make(chan struct{}, 1)}
	push := &fakePush{calls: make(chan uuid.UUID, 2), release: make(chan struct{})}
	metrics := &fakeMetrics{stale: make(chan EnqueueResult, 2), finished: make(chan string, 2)}
	ticker := &fakeTicker{ticks: make(chan time.Time, 1)}
	pool := NewPool(scanner, push, NewQueue(2), metrics, slog.New(slog.NewTextHandler(io.Discard, nil)), ticker, time.Minute, time.Second, 1)
	pool.Start()
	ticker.ticks <- time.Now()
	<-push.calls
	<-metrics.stale
	<-metrics.stale
	stopped := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		stopped <- pool.Stop(ctx)
	}()
	close(push.release)
	<-push.calls
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
}

func TestCoordinateCancelsWaitingTask(t *testing.T) {
	first, second := uuid.New(), uuid.New()
	scanner := &fakeScanner{trips: []model.StaleTrip{{TripID: first}, {TripID: second}}, scanned: make(chan struct{}, 1)}
	push := &fakePush{calls: make(chan uuid.UUID, 2), release: make(chan struct{})}
	metrics := &fakeMetrics{stale: make(chan EnqueueResult, 2), finished: make(chan string, 2)}
	ticker := &fakeTicker{ticks: make(chan time.Time, 1)}
	queue := NewQueue(2)
	pool := NewPool(scanner, push, queue, metrics, slog.New(slog.NewTextHandler(io.Discard, nil)), ticker, time.Minute, time.Second, 1)
	pool.Start()
	ticker.ticks <- time.Now()
	if got := <-push.calls; got != first {
		t.Fatalf("first call = %s", got)
	}
	<-metrics.stale
	<-metrics.stale
	queue.Cancel(second)
	close(push.release)
	if got := <-metrics.finished; got != "success" {
		t.Fatalf("first result = %s", got)
	}
	if got := <-metrics.finished; got != "cancelled" {
		t.Fatalf("second result = %s", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := pool.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-push.calls:
		t.Fatalf("cancelled task sent push for %s", got)
	default:
	}
}

func TestPoolUsesConfiguredNumberOfWorkers(t *testing.T) {
	const workers = 3
	scanner := &fakeScanner{scanned: make(chan struct{}, 1)}
	for range workers {
		scanner.trips = append(scanner.trips, model.StaleTrip{TripID: uuid.New()})
	}
	push := &fakePush{calls: make(chan uuid.UUID, workers), release: make(chan struct{})}
	metrics := &fakeMetrics{stale: make(chan EnqueueResult, workers), finished: make(chan string, workers)}
	ticker := &fakeTicker{ticks: make(chan time.Time, 1)}
	pool := NewPool(scanner, push, NewQueue(workers), metrics, slog.New(slog.NewTextHandler(io.Discard, nil)), ticker, time.Minute, 5*time.Second, workers)
	pool.Start()
	ticker.ticks <- time.Now()
	for range workers {
		<-push.calls
	}
	close(push.release)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := pool.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestPoolDoesNotRequestPositionForFreshTrip(t *testing.T) {
	scanner := &fakeScanner{trips: []model.StaleTrip{{TripID: uuid.New()}}, scanned: make(chan struct{}, 1), notStale: true}
	push := &fakePush{calls: make(chan uuid.UUID, 1)}
	metrics := &fakeMetrics{stale: make(chan EnqueueResult, 1), finished: make(chan string, 1)}
	ticker := &fakeTicker{ticks: make(chan time.Time, 1)}
	pool := NewPool(scanner, push, NewQueue(1), metrics, slog.New(slog.NewTextHandler(io.Discard, nil)), ticker, time.Minute, time.Second, 1)
	pool.Start()
	ticker.ticks <- time.Now()
	if got := <-metrics.finished; got != "cancelled" {
		t.Fatalf("result = %s", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := pool.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-push.calls:
		t.Fatalf("sent push for fresh trip %s", id)
	default:
	}
}

func TestSecondTickDoesNotDuplicateInFlightTrip(t *testing.T) {
	id := uuid.New()
	scanner := &fakeScanner{trips: []model.StaleTrip{{TripID: id}}, scanned: make(chan struct{}, 2)}
	push := &fakePush{calls: make(chan uuid.UUID, 2), release: make(chan struct{})}
	metrics := &fakeMetrics{stale: make(chan EnqueueResult, 2), finished: make(chan string, 2)}
	ticker := &fakeTicker{ticks: make(chan time.Time, 2)}
	pool := NewPool(scanner, push, NewQueue(1), metrics, slog.New(slog.NewTextHandler(io.Discard, nil)), ticker, time.Minute, 5*time.Second, 1)
	pool.Start()
	ticker.ticks <- time.Now()
	<-push.calls
	if got := <-metrics.stale; got != Queued {
		t.Fatalf("first tick: %s", got)
	}
	ticker.ticks <- time.Now()
	if got := <-metrics.stale; got != Duplicate {
		t.Fatalf("second tick: %s", got)
	}
	close(push.release)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := pool.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-push.calls:
		t.Fatalf("duplicate push: %s", id)
	default:
	}
}

type orderedMetrics struct {
	entered chan struct{}
	release chan struct{}
	changes chan int64
}

func (m *orderedMetrics) StaleCheck(context.Context, string)             { close(m.entered); <-m.release }
func (m *orderedMetrics) QueueChange(_ context.Context, delta int64)     { m.changes <- delta }
func (*orderedMetrics) WorkerChange(context.Context, int64)              {}
func (*orderedMetrics) PositionRequest(context.Context, string, float64) {}

func TestQueueMetricIncrementsBeforeWorkerRemovesTask(t *testing.T) {
	scanner := &fakeScanner{trips: []model.StaleTrip{{TripID: uuid.New()}}, scanned: make(chan struct{}, 1)}
	push := &fakePush{calls: make(chan uuid.UUID, 1)}
	metrics := &orderedMetrics{entered: make(chan struct{}), release: make(chan struct{}), changes: make(chan int64, 2)}
	ticker := &fakeTicker{ticks: make(chan time.Time, 1)}
	pool := NewPool(scanner, push, NewQueue(1), metrics, slog.New(slog.NewTextHandler(io.Discard, nil)), ticker, time.Minute, time.Second, 1)
	pool.Start()
	ticker.ticks <- time.Now()
	<-metrics.entered
	first := <-metrics.changes
	close(metrics.release)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := pool.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if first != 1 {
		t.Fatalf("first queue change = %d, want 1", first)
	}
}
