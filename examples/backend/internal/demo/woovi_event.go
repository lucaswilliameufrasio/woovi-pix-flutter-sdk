package demo

import "context"

type WooviChargeEvent struct {
	Event  string `json:"event"`
	Charge struct {
		CorrelationID string `json:"correlationID"`
		Status        string `json:"status"`
		Value         int64  `json:"value"`
	} `json:"charge"`
	Pix struct {
		Status string `json:"status"`
	} `json:"pix"`
}

type WebhookResult struct {
	Applied   bool
	Duplicate bool
	Ignored   bool
	Rejected  bool
}

type WebhookVerifier interface {
	Verify(context.Context, []byte, string) (bool, error)
}
