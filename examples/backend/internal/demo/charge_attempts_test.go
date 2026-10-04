package demo

import "testing"

func TestMergeProviderChargeStatusIsMonotonic(t *testing.T) {
	tests := []struct {
		current  string
		observed string
		want     string
	}{
		{"", "ACTIVE", "ACTIVE"},
		{"ACTIVE", "ACTIVE", "ACTIVE"},
		{"ACTIVE", "EXPIRED", "EXPIRED"},
		{"EXPIRED", "ACTIVE", "EXPIRED"},
		{"ACTIVE", "COMPLETED", "COMPLETED"},
		{"EXPIRED", "COMPLETED", "COMPLETED"},
		{"COMPLETED", "ACTIVE", "COMPLETED"},
		{"COMPLETED", "EXPIRED", "COMPLETED"},
	}
	for _, test := range tests {
		t.Run(test.current+"_then_"+test.observed, func(t *testing.T) {
			got, err := MergeProviderChargeStatus(test.current, test.observed)
			if err != nil {
				t.Fatalf("MergeProviderChargeStatus() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("MergeProviderChargeStatus() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestMergeProviderChargeStatusRejectsUnknownStates(t *testing.T) {
	for _, test := range [][2]string{{"UNKNOWN", "ACTIVE"}, {"ACTIVE", "UNKNOWN"}, {"", ""}} {
		if _, err := MergeProviderChargeStatus(test[0], test[1]); err == nil {
			t.Fatalf("MergeProviderChargeStatus(%q, %q) expected error", test[0], test[1])
		}
	}
}
