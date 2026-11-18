package container

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Smoz1e/Bulatov_Ilya_Lab/internal/config"
	"github.com/Smoz1e/Bulatov_Ilya_Lab/internal/gateway/push"
	api "github.com/Smoz1e/Bulatov_Ilya_Lab/internal/generated"
	triphttp "github.com/Smoz1e/Bulatov_Ilya_Lab/internal/http"
	"github.com/Smoz1e/Bulatov_Ilya_Lab/internal/http/middleware"
	infraPostgres "github.com/Smoz1e/Bulatov_Ilya_Lab/internal/infra/postgres"
	"github.com/Smoz1e/Bulatov_Ilya_Lab/internal/infra/postgres/transaction"
	"github.com/Smoz1e/Bulatov_Ilya_Lab/internal/infra/telemetry"
	triprepository "github.com/Smoz1e/Bulatov_Ilya_Lab/internal/repository/trip"
	positionusecase "github.com/Smoz1e/Bulatov_Ilya_Lab/internal/usecase/position"
	createusecase "github.com/Smoz1e/Bulatov_Ilya_Lab/internal/usecase/trip/create"
	finishusecase "github.com/Smoz1e/Bulatov_Ilya_Lab/internal/usecase/trip/finish"
	getusecase "github.com/Smoz1e/Bulatov_Ilya_Lab/internal/usecase/trip/get"
	"github.com/Smoz1e/Bulatov_Ilya_Lab/internal/worker/positions"
)

type Application struct {
	Server    *http.Server
	Logger    *slog.Logger
	pool      *pgxpool.Pool
	telemetry *telemetry.Runtime
	positions *positions.Pool
	push      *push.Client
}

func New(ctx context.Context, cfg config.Config) (*Application, error) {
	level, err := logLevel(cfg.LogLevel)
	if err != nil {
		return nil, err
	}
	logger := telemetry.NewLogger(os.Stdout, level, cfg.Telemetry.ServiceName)
	runtime, err := telemetry.Setup(ctx, cfg.Telemetry, logger)
	if err != nil {
		return nil, fmt.Errorf("setup telemetry: %w", err)
	}

	pool, err := infraPostgres.NewPool(ctx, cfg.Database)
	if err != nil {
		if shutdownErr := runtime.Shutdown(context.WithoutCancel(ctx)); shutdownErr != nil {
			logger.Error("shutdown telemetry", "error", shutdownErr)
		}
		return nil, fmt.Errorf("connect to database: %w", err)
	}

	metrics, err := telemetry.NewMetrics()
	if err != nil {
		pool.Close()
		if shutdownErr := runtime.Shutdown(context.WithoutCancel(ctx)); shutdownErr != nil {
			logger.Error("shutdown telemetry", "error", shutdownErr)
		}
		return nil, fmt.Errorf("create metrics: %w", err)
	}

	repository := triprepository.New(pool, cfg.Database.QueryTimeout)
	transactionManager := transaction.New(pool)
	createTrip := createusecase.New(repository, repository, transactionManager, metrics, logger, time.Now)
	getTrip := getusecase.New(repository)
	finishTrip := finishusecase.New(repository, repository, transactionManager, metrics, logger, time.Now)
	queue := positions.NewQueue(cfg.Position.QueueSize)
	pushClient, err := push.New(cfg.Position.PushURL, cfg.Position.RequestTimeout)
	if err != nil {
		pool.Close()
		_ = runtime.Shutdown(context.WithoutCancel(ctx))
		return nil, fmt.Errorf("create push client: %w", err)
	}
	workerPool := positions.NewPool(repository, pushClient, queue, metrics, logger,
		positions.NewTicker(cfg.Position.CheckInterval), cfg.Position.StaleAfter, cfg.Position.RequestTimeout, cfg.Position.Workers)
	position := positionusecase.New(repository, transactionManager, queue, metrics, time.Now)
	server := triphttp.NewServer(createTrip, getTrip, finishTrip, pool, cfg.Database.QueryTimeout)
	server.SetPositions(position)
	workerPool.Start()

	router := chi.NewRouter()
	router.Use(middleware.Recover(logger))
	router.Use(middleware.Tracing)
	router.Use(middleware.Logging(logger))
	router.Use(middleware.Metrics(metrics))
	handler := api.HandlerWithOptions(server, api.ChiServerOptions{
		BaseRouter:       router,
		ErrorHandlerFunc: triphttp.HandleOpenAPIError,
	})

	return &Application{
		Server: &http.Server{
			Addr:              cfg.HTTPAddr,
			Handler:           handler,
			ReadTimeout:       cfg.HTTPReadTimeout,
			ReadHeaderTimeout: cfg.HTTPReadHeaderTimeout,
			WriteTimeout:      cfg.HTTPWriteTimeout,
			IdleTimeout:       cfg.HTTPIdleTimeout,
		},
		Logger:    logger,
		pool:      pool,
		telemetry: runtime,
		positions: workerPool,
		push:      pushClient,
	}, nil
}

func (application *Application) Close(ctx context.Context) error {
	workerErr := application.positions.Stop(ctx)
	application.push.Close()
	application.pool.Close()
	return errors.Join(workerErr, application.telemetry.Shutdown(ctx))
}

func logLevel(value string) (slog.Level, error) {
	var level slog.Level
	if err := level.UnmarshalText([]byte(value)); err != nil {
		return 0, fmt.Errorf("parse LOG_LEVEL: %w", err)
	}

	return level, nil
}
