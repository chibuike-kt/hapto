package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/chibuike-kt/hapto-api/internal/config"
	"github.com/chibuike-kt/hapto-api/internal/cryptoclient"
	"github.com/chibuike-kt/hapto-api/internal/device"
)

func main() {
	cfg := config.Load()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pgPool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connect postgres: %v", err)
	}
	defer pgPool.Close()

	if err := pgPool.Ping(ctx); err != nil {
		log.Fatalf("ping postgres: %v", err)
	}

	if err := device.ApplySchema(ctx, pgPool); err != nil {
		log.Fatalf("apply device schema: %v", err)
	}

	redisOpts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		log.Fatalf("parse redis url: %v", err)
	}
	redisClient := redis.NewClient(redisOpts)
	defer func() {
		if err := redisClient.Close(); err != nil {
			log.Printf("close redis client: %v", err)
		}
	}()

	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Fatalf("connect redis: %v", err)
	}

	cryptoClient, err := cryptoclient.Dial(cfg.CryptoAddr)
	if err != nil {
		log.Fatalf("dial hapto-crypto: %v", err)
	}
	defer func() {
		if err := cryptoClient.Close(); err != nil {
			log.Printf("close hapto-crypto client: %v", err)
		}
	}()

	deviceService := device.NewService(device.NewPostgresStore(pgPool), cryptoClient)
	deviceHandler := device.NewHandler(deviceService)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /devices", deviceHandler.RegisterDevice)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("hapto-api listening on %s", cfg.HTTPAddr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("serve: %v", err)
	}
}
