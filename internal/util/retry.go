package util

import (
	"fmt"
	"time"
)

type RetryConfig struct {
	Attempts int
	Delay    time.Duration
	Factor   float64
}

func DefaultRetry() RetryConfig {
	return RetryConfig{
		Attempts: 3,
		Delay:    500 * time.Millisecond,
		Factor:   2.0,
	}
}

func Retry(cfg RetryConfig, fn func() error) error {
	var err error
	delay := cfg.Delay

	for i := 0; i < cfg.Attempts; i++ {
		if err = fn(); err == nil {
			return nil
		}

		if i == cfg.Attempts-1 {
			break
		}

		time.Sleep(delay)
		delay = time.Duration(float64(delay) * cfg.Factor)
	}

	return fmt.Errorf("retry failed after %d attempts: %w", cfg.Attempts, err)
}
