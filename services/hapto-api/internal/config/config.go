// Package config loads hapto-api's runtime configuration from the
// environment. Defaults match the local dev setup in docker-compose.yml
// (host-run process talking to containers published on localhost).
package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	HTTPAddr    string
	DatabaseURL string
	RedisURL    string
	CryptoAddr  string

	CryptoTLSCert string
	CryptoTLSKey  string
	CryptoTLSCA   string

	PasswordPepper    string
	TOTPEncryptionKey []byte
	SessionIdleTTL    time.Duration
	SessionMaxTTL     time.Duration

	ResendAPIKey string
	EmailFrom    string
	AppBaseURL   string

	PaymentIntentTTL time.Duration
}

func Load() (Config, error) {
	totpKey, err := decodeKey(getEnv("AUTH_TOTP_ENCRYPTION_KEY", ""))
	if err != nil {
		return Config{}, fmt.Errorf("AUTH_TOTP_ENCRYPTION_KEY: %w", err)
	}

	idleTTL, err := getSeconds("SESSION_IDLE_TTL_SECONDS", 1800)
	if err != nil {
		return Config{}, err
	}
	maxTTL, err := getSeconds("SESSION_MAX_TTL_SECONDS", 604800)
	if err != nil {
		return Config{}, err
	}

	paymentIntentTTL, err := getSeconds("PAYMENT_INTENT_TTL_SECONDS", 300)
	if err != nil {
		return Config{}, err
	}

	return Config{
		HTTPAddr:    ":" + getEnv("PORT", "8080"),
		DatabaseURL: getEnv("DATABASE_URL", "postgres://hapto:hapto@localhost:5432/hapto?sslmode=disable"),
		RedisURL:    getEnv("REDIS_URL", "redis://localhost:6379/0"),
		CryptoAddr:  getEnv("HAPTO_CRYPTO_ADDR", "localhost:50051"),

		CryptoTLSCert: getEnv("HAPTO_CRYPTO_TLS_CERT", "../../certs/hapto-api.crt"),
		CryptoTLSKey:  getEnv("HAPTO_CRYPTO_TLS_KEY", "../../certs/hapto-api.key"),
		CryptoTLSCA:   getEnv("HAPTO_CRYPTO_TLS_CA", "../../certs/ca.crt"),

		PasswordPepper:    getEnv("AUTH_PASSWORD_PEPPER", ""),
		TOTPEncryptionKey: totpKey,
		SessionIdleTTL:    idleTTL,
		SessionMaxTTL:     maxTTL,

		ResendAPIKey: getEnv("RESEND_API_KEY", ""),
		EmailFrom:    getEnv("EMAIL_FROM", "hapto <noreply@hapto.dev>"),
		AppBaseURL:   getEnv("APP_BASE_URL", "http://localhost:3000"),

		PaymentIntentTTL: paymentIntentTTL,
	}, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getSeconds(key string, fallback int) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return time.Duration(fallback) * time.Second, nil
	}
	seconds, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return time.Duration(seconds) * time.Second, nil
}

// decodeKey base64-decodes a symmetric key. An empty input decodes to nil
// rather than erroring, so local dev can run without TOTP configured.
func decodeKey(encoded string) ([]byte, error) {
	if encoded == "" {
		return nil, nil
	}
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode base64: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("must decode to 32 bytes for AES-256, got %d", len(key))
	}
	return key, nil
}
