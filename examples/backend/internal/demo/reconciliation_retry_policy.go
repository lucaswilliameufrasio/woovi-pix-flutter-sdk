package demo

import (
	"errors"
	"time"
)

// ReconciliationRetryPolicy bounds exponential retry delays and can add
// independent jitter to spread a large recovery queue over time.
type ReconciliationRetryPolicy struct {
	BaseDelay      time.Duration
	MaxDelay       time.Duration
	JitterFraction float64
}

func DefaultReconciliationRetryPolicy() ReconciliationRetryPolicy {
	return ReconciliationRetryPolicy{BaseDelay: 5 * time.Second, MaxDelay: time.Hour, JitterFraction: 0.2}
}

func (p ReconciliationRetryPolicy) validate() error {
	if p.BaseDelay < time.Second || p.BaseDelay > time.Hour ||
		p.MaxDelay < p.BaseDelay || p.MaxDelay > 24*time.Hour ||
		p.JitterFraction < 0 || p.JitterFraction > 0.5 {
		return errors.New("invalid reconciliation retry policy")
	}
	return nil
}
