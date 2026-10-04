package demo

import (
	"testing"
	"time"
)

func TestReconciliationRetryPolicyValidation(t *testing.T) {
	valid := []ReconciliationRetryPolicy{
		DefaultReconciliationRetryPolicy(),
		{BaseDelay: time.Second, MaxDelay: time.Second, JitterFraction: 0},
		{BaseDelay: time.Minute, MaxDelay: 24 * time.Hour, JitterFraction: 0.5},
	}
	for _, policy := range valid {
		if err := policy.validate(); err != nil {
			t.Errorf("valid policy %#v rejected: %v", policy, err)
		}
	}
	invalid := []ReconciliationRetryPolicy{
		{BaseDelay: time.Millisecond, MaxDelay: time.Second},
		{BaseDelay: time.Minute, MaxDelay: time.Second},
		{BaseDelay: time.Second, MaxDelay: 25 * time.Hour},
		{BaseDelay: time.Second, MaxDelay: time.Hour, JitterFraction: -0.1},
		{BaseDelay: time.Second, MaxDelay: time.Hour, JitterFraction: 0.51},
	}
	for _, policy := range invalid {
		if err := policy.validate(); err == nil {
			t.Errorf("invalid policy %#v accepted", policy)
		}
	}
}
