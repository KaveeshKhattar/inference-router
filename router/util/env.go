package util

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultMetricsPort   = 8000
	DefaultRouterPort    = 9000
	DefaultDiscoveryHost = "vllm-headless.default.svc.cluster.local"
	DefaultScrapeTimeout = 1500 * time.Millisecond
	DefaultLoadRecalc    = 1 * time.Second
	DefaultLocalWeight   = 1.0
)

func GetEnvString(name, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}

func GetEnvDuration(name string, fallback time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		log.Printf("invalid duration for %s=%q, using default %s", name, v, fallback)
		return fallback
	}
	return d
}

func GetEnvInt(name string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		log.Printf("invalid integer for %s=%q, using default %d", name, v, fallback)
		return fallback
	}
	return n
}

func GetEnvFloat(name string, fallback float64) float64 {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil || n < 0 {
		log.Printf("invalid float for %s=%q, using default %.2f", name, v, fallback)
		return fallback
	}
	return n
}