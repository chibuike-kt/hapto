// Package config loads hapto-api's runtime configuration from the
// environment. Defaults match the local dev setup in docker-compose.yml
// (host-run process talking to containers published on localhost).
package config

import "os"

type Config struct {
	HTTPAddr    string
	DatabaseURL string
	RedisURL    string
	CryptoAddr  string
}

func Load() Config {
	return Config{
		HTTPAddr:    getEnv("HTTP_ADDR", ":8080"),
		DatabaseURL: getEnv("DATABASE_URL", "postgres://hapto:hapto@localhost:5432/hapto?sslmode=disable"),
		RedisURL:    getEnv("REDIS_URL", "redis://localhost:6379/0"),
		CryptoAddr:  getEnv("HAPTO_CRYPTO_ADDR", "localhost:50051"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
