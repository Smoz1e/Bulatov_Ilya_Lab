package positions

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/Smoz1e/Bulatov_Ilya_Lab/internal/model"
)

type scanner interface {
	StaleTrips(context.Context, time.Duration) ([]model.StaleTrip, error)
	IsStale(context.Context, uuid.UUID, time.Duration) (bool, error)
}
type pushGateway interface {
	RequestPosition(context.Context, uuid.UUID, uuid.UUID) error
}
type metrics interface {
	StaleCheck(context.Context, string)
	QueueChange(context.Context, int64)
	WorkerChange(context.Context, int64)
	PositionRequest(context.Context, string, float64)
}
type Ticker interface {
	C() <-chan time.Time
	Stop()
}
type realTicker struct{ ticker *time.Ticker }

func (ticker *realTicker) C() <-chan time.Time { return ticker.ticker.C }
func (ticker *realTicker) Stop()               { ticker.ticker.Stop() }
func NewTicker(interval time.Duration) Ticker  { return &realTicker{ticker: time.NewTicker(interval)} }

type Pool struct {
	scanner     scanner
	push        pushGateway
	queue       *Queue
	metrics     metrics
	logger      *slog.Logger
	ticker      Ticker
	staleAfter  time.Duration
	timeout     time.Duration
	workers     int
	scanCtx     context.Context
	stopScanner context.CancelFunc
	workCtx     context.Context
	stopWorkers context.CancelFunc
	scannerDone chan struct{}
	workersDone chan struct{}
}

func NewPool(scanner scanner, push pushGateway, queue *Queue, metrics metrics, logger *slog.Logger,
	ticker Ticker, staleAfter, timeout time.Duration, workers int,
) *Pool {
	scanCtx, stopScanner := context.WithCancel(context.Background())
	workCtx, stopWorkers := context.WithCancel(context.Background())
	return &Pool{
		scanner: scanner, push: push, queue: queue, metrics: metrics, logger: logger, ticker: ticker,
		staleAfter: staleAfter, timeout: timeout, workers: workers, scanCtx: scanCtx, stopScanner: stopScanner,
		workCtx: workCtx, stopWorkers: stopWorkers, scannerDone: make(chan struct{}), workersDone: make(chan struct{}),
	}
}

func (pool *Pool) Start() {
	var workers sync.WaitGroup
	workers.Add(pool.workers)
	for range pool.workers {
		go func() { defer workers.Done(); pool.work() }()
	}
	go func() { workers.Wait(); close(pool.workersDone) }()
	go pool.scan()
}

func (pool *Pool) scan() {
	defer close(pool.scannerDone)
	defer pool.ticker.Stop()
	for {
		select {
		case <-pool.scanCtx.Done():
			return
		case <-pool.ticker.C():
			pool.scanOnce()
		}
	}
}

func (pool *Pool) scanOnce() {
	ctx, span := otel.Tracer("trip-service").Start(pool.scanCtx, "scan stale trips")
	defer span.End()
	trips, err := pool.scanner.StaleTrips(ctx, pool.staleAfter)
	if err != nil {
		if ctx.Err() == nil {
			pool.logger.ErrorContext(ctx, "scan stale trips", "error", err)
		}
		return
	}
	for _, trip := range trips {
		if ctx.Err() != nil {
			return
		}
		result := pool.queue.Enqueue(Task{TripID: trip.TripID, DriverID: trip.DriverID, Span: span.SpanContext()})
		pool.metrics.StaleCheck(ctx, string(result))
		if result == Queued {
			pool.metrics.QueueChange(ctx, 1)
		}
		if result == Dropped {
			pool.logger.WarnContext(ctx, "position queue full", "trip_id", trip.TripID)
		}
	}
}

func (pool *Pool) work() {
	for {
		select {
		case <-pool.workCtx.Done():
			return
		case item, ok := <-pool.queue.tasks:
			if !ok {
				return
			}
			pool.metrics.QueueChange(pool.workCtx, -1)
			if !pool.queue.Start(item) {
				pool.metrics.PositionRequest(pool.workCtx, "cancelled", 0)
				continue
			}
			pool.process(item)
			pool.queue.Finish(item)
		}
	}
}

func (pool *Pool) process(item *entry) {
	ctx, span := otel.Tracer("trip-service").Start(pool.workCtx, "request position", trace.WithNewRoot(),
		trace.WithLinks(trace.Link{SpanContext: item.task.Span}))
	defer span.End()
	pool.metrics.WorkerChange(ctx, 1)
	defer pool.metrics.WorkerChange(ctx, -1)
	ctx, cancel := context.WithTimeout(ctx, pool.timeout)
	defer cancel()
	stale, err := pool.scanner.IsStale(ctx, item.task.TripID, pool.staleAfter)
	if err != nil {
		pool.logger.WarnContext(ctx, "check stale trip", "trip_id", item.task.TripID, "error", err)
		pool.metrics.PositionRequest(ctx, "error", 0)
		return
	}
	if !stale {
		pool.metrics.PositionRequest(ctx, "cancelled", 0)
		return
	}
	start := time.Now()
	err = pool.push.RequestPosition(ctx, item.task.TripID, item.task.DriverID)
	result := "success"
	if err != nil {
		result = "error"
		pool.logger.WarnContext(ctx, "request position", "trip_id", item.task.TripID, "error", err)
	}
	pool.metrics.PositionRequest(ctx, result, time.Since(start).Seconds())
}

func (pool *Pool) Stop(ctx context.Context) error {
	pool.stopScanner()
	select {
	case <-pool.scannerDone:
	case <-ctx.Done():
		pool.stopWorkers()
		return fmt.Errorf("stop scanner: %w", ctx.Err())
	}
	close(pool.queue.tasks)
	select {
	case <-pool.workersDone:
		pool.stopWorkers()
		return nil
	case <-ctx.Done():
		pool.stopWorkers()
		return fmt.Errorf("drain workers: %w", ctx.Err())
	}
}
