package admission

import (
	"time"

	"router/util"
)

// Config controls per-pod capacity, CoDel queue shedding, and debt behavior.
type Config struct {
	Enabled       bool
	PodCapacity   int
	QueueCapacity int
	TargetDelay   time.Duration
	Interval      time.Duration
	MaxWait       time.Duration
}

func DefaultConfig() Config {
	return Config{
		Enabled:       false,
		PodCapacity:   4,
		QueueCapacity: 32,
		TargetDelay:   100 * time.Millisecond,
		Interval:      500 * time.Millisecond,
		MaxWait:       5 * time.Second,
	}
}

func ConfigFromEnv() Config {
	cfg := DefaultConfig()
	if util.GetEnvString("ROUTER_ADMISSION_ENABLED", "") == "1" ||
		util.GetEnvString("ROUTER_ADMISSION_ENABLED", "") == "true" {
		cfg.Enabled = true
	}
	cfg.PodCapacity = util.GetEnvInt("ROUTER_ADMISSION_POD_CAPACITY", cfg.PodCapacity)
	cfg.QueueCapacity = util.GetEnvInt("ROUTER_ADMISSION_QUEUE_CAPACITY", cfg.QueueCapacity)
	cfg.TargetDelay = util.GetEnvDuration("ROUTER_ADMISSION_CODEL_TARGET", cfg.TargetDelay)
	cfg.Interval = util.GetEnvDuration("ROUTER_ADMISSION_CODEL_INTERVAL", cfg.Interval)
	cfg.MaxWait = util.GetEnvDuration("ROUTER_ADMISSION_MAX_WAIT", cfg.MaxWait)
	return cfg
}
