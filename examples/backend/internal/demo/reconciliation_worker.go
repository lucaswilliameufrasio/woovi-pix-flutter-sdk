package demo

import (
	"context"
	"errors"
	"time"
)

// ReconciliationCycle summarizes one bounded worker pass. Err is a queue-level
// failure (for example, a database error claiming the batch); per-attempt
// failures are counted separately in Failed.
type ReconciliationCycle struct {
	Claimed  int
	Resolved int
	Failed   int
	Err      error
}

// ChargeReconciliationWorker periodically drains uncertain charge attempts.
// It can only issue GET lookups through ReconcilePendingChargeAttempts.
type ChargeReconciliationWorker struct {
	store    *PostgresStore
	client   *WooviChargeClient
	interval time.Duration
	limit    int
	retry    ReconciliationRetryPolicy
	clock    func() time.Time
	onCycle  func(ReconciliationCycle)
}

func NewChargeReconciliationWorker(store *PostgresStore, client *WooviChargeClient, interval time.Duration, limit int, retry ReconciliationRetryPolicy, clock func() time.Time, onCycle func(ReconciliationCycle)) (*ChargeReconciliationWorker, error) {
	if store == nil || client == nil || interval <= 0 || limit < 1 || limit > 500 || retry.validate() != nil {
		return nil, errors.New("invalid reconciliation worker configuration")
	}
	if clock == nil {
		clock = time.Now
	}
	return &ChargeReconciliationWorker{
		store: store, client: client, interval: interval, limit: limit, retry: retry, clock: clock, onCycle: onCycle,
	}, nil
}

// Run starts polling after the first interval and returns nil on graceful
// context cancellation. Queue and item failures are reported through onCycle;
// they do not terminate future polling.
func (w *ChargeReconciliationWorker) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			outcomes, err := ReconcilePendingChargeAttempts(ctx, w.store, w.client, w.limit, w.clock().UTC(), w.retry)
			cycle := ReconciliationCycle{Claimed: len(outcomes), Err: err}
			for _, outcome := range outcomes {
				if outcome.Err != nil {
					cycle.Failed++
				} else {
					cycle.Resolved++
				}
			}
			if w.onCycle != nil {
				w.onCycle(cycle)
			}
		}
	}
}
