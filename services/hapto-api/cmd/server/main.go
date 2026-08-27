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

	"github.com/chibuike-kt/hapto-api/internal/audit"
	"github.com/chibuike-kt/hapto-api/internal/auth"
	"github.com/chibuike-kt/hapto-api/internal/config"
	"github.com/chibuike-kt/hapto-api/internal/cryptoclient"
	"github.com/chibuike-kt/hapto-api/internal/device"
	"github.com/chibuike-kt/hapto-api/internal/email"
	"github.com/chibuike-kt/hapto-api/internal/migrate"
	"github.com/chibuike-kt/hapto-api/internal/ratelimit"
	"github.com/chibuike-kt/hapto-api/internal/session"
)

const (
	rateLimitBaseBackoff = time.Second
	rateLimitMaxBackoff  = 5 * time.Minute
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

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

	if err := migrate.Up(cfg.DatabaseURL); err != nil {
		log.Fatalf("run migrations: %v", err)
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

	cryptoClient, err := cryptoclient.Dial(cfg.CryptoAddr, cryptoclient.TLSConfig{
		CertFile: cfg.CryptoTLSCert,
		KeyFile:  cfg.CryptoTLSKey,
		CAFile:   cfg.CryptoTLSCA,
	})
	if err != nil {
		log.Fatalf("dial hapto-crypto: %v", err)
	}
	defer func() {
		if err := cryptoClient.Close(); err != nil {
			log.Printf("close hapto-crypto client: %v", err)
		}
	}()

	auditLog := audit.NewPostgresStore(pgPool)

	deviceService := device.NewService(device.NewPostgresStore(pgPool), cryptoClient, auditLog)
	deviceHandler := device.NewHandler(deviceService)

	sessionStore := session.NewStore(redisClient, cfg.SessionIdleTTL, cfg.SessionMaxTTL)
	loginLimiter := ratelimit.NewLimiter(redisClient, rateLimitBaseBackoff, rateLimitMaxBackoff)
	mailer := email.NewClient(cfg.ResendAPIKey, cfg.EmailFrom)

	authService := auth.NewService(
		auth.NewPostgresStore(pgPool),
		sessionStore,
		auth.NewLockoutTracker(redisClient),
		auth.NewPendingLoginStore(redisClient),
		mailer,
		auditLog,
		auth.ServiceConfig{
			Pepper:       cfg.PasswordPepper,
			TOTPKey:      cfg.TOTPEncryptionKey,
			ResetBaseURL: cfg.AppBaseURL,
		},
	)
	authHandler := auth.NewHandler(authService, loginLimiter)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /devices", deviceHandler.RegisterDevice)
	mux.Handle("POST /devices/{id}/revoke", sessionStore.RequireSession(http.HandlerFunc(deviceHandler.RevokeDevice)))

	mux.HandleFunc("POST /auth/signup", authHandler.Signup)
	mux.HandleFunc("POST /auth/login", authHandler.Login)
	mux.HandleFunc("POST /auth/login/verify-totp", authHandler.VerifyTOTPLogin)
	mux.HandleFunc("POST /auth/password/forgot", authHandler.ForgotPassword)
	mux.HandleFunc("POST /auth/password/reset", authHandler.ResetPassword)
	mux.Handle("POST /auth/totp/enroll", sessionStore.RequireSession(http.HandlerFunc(authHandler.EnrollTOTP)))
	mux.Handle("POST /auth/totp/confirm", sessionStore.RequireSession(http.HandlerFunc(authHandler.ConfirmTOTP)))

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
