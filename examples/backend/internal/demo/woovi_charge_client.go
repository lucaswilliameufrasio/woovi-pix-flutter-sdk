package demo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxWooviResponseBytes = 1 << 20

// WooviChargeClient creates charges from a trusted merchant backend only.
// It intentionally never retries: after a timeout, the caller must reconcile
// using the same correlationID before deciding whether another POST is safe.
type WooviChargeClient struct {
	appID  string
	base   *url.URL
	client *http.Client
}

type WooviCharge struct {
	CorrelationID string
	TransactionID string
	Status        string
	Value         int64
	BRCode        string
	ExpiresAt     time.Time
}

func NewWooviChargeClient(appID, baseURL string, client *http.Client) (*WooviChargeClient, error) {
	if appID == "" || len(appID) > 4096 {
		return nil, errors.New("invalid Woovi AppID")
	}
	if baseURL == "" {
		baseURL = "https://api.woovi.com"
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("invalid Woovi API base URL")
	}
	if parsed.Scheme != "https" && !isLoopbackHTTP(parsed) {
		return nil, errors.New("woovi API URL must use HTTPS (HTTP is allowed only for loopback tests)")
	}
	if client == nil {
		client = &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	return &WooviChargeClient{appID: appID, base: parsed, client: client}, nil
}

func isLoopbackHTTP(parsed *url.URL) bool {
	if parsed.Scheme != "http" {
		return false
	}
	if strings.EqualFold(parsed.Hostname(), "localhost") {
		return true
	}
	ip := net.ParseIP(parsed.Hostname())
	return ip != nil && ip.IsLoopback()
}

func (c *WooviChargeClient) CreateCharge(ctx context.Context, correlationID string, amountCents int64) (WooviCharge, error) {
	if correlationID == "" || len(correlationID) > 128 || amountCents <= 0 || amountCents > 100_000_000 {
		return WooviCharge{}, errors.New("invalid charge correlationID or amount")
	}
	endpoint := *c.base
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/api/v1/charge"
	endpoint.RawPath = ""
	body, err := json.Marshal(struct {
		CorrelationID string `json:"correlationID"`
		Value         int64  `json:"value"`
	}{CorrelationID: correlationID, Value: amountCents})
	if err != nil {
		return WooviCharge{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return WooviCharge{}, fmt.Errorf("build Woovi charge request: %w", err)
	}
	req.Header.Set("Authorization", c.appID)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	response, err := c.client.Do(req)
	if err != nil {
		return WooviCharge{}, fmt.Errorf("woovi charge request outcome is uncertain; reconcile correlationID before retrying: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return WooviCharge{}, fmt.Errorf("woovi charge request returned HTTP %d; reconcile correlationID before retrying", response.StatusCode)
	}
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxWooviResponseBytes+1))
	if err != nil {
		return WooviCharge{}, fmt.Errorf("read Woovi charge response: %w", err)
	}
	if len(responseBody) > maxWooviResponseBytes {
		return WooviCharge{}, errors.New("woovi charge response exceeds size limit")
	}
	var decoded struct {
		CorrelationID string `json:"correlationID"`
		TransactionID string `json:"transactionID"`
		Identifier    string `json:"identifier"`
		BRCode        string `json:"brCode"`
		Charge        struct {
			CorrelationID string `json:"correlationID"`
			TransactionID string `json:"transactionID"`
			Identifier    string `json:"identifier"`
			Status        string `json:"status"`
			Value         int64  `json:"value"`
			BRCode        string `json:"brCode"`
			ExpiresDate   string `json:"expiresDate"`
		} `json:"charge"`
	}
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return WooviCharge{}, fmt.Errorf("decode Woovi charge response: %w", err)
	}
	chargeID := decoded.Charge.CorrelationID
	if chargeID == "" {
		chargeID = decoded.CorrelationID
	}
	if chargeID != "" && chargeID != correlationID {
		return WooviCharge{}, errors.New("woovi returned a different correlationID")
	}
	brCode := decoded.Charge.BRCode
	if brCode == "" {
		brCode = decoded.BRCode
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, decoded.Charge.ExpiresDate)
	if decoded.Charge.Status == "" || err != nil || (decoded.Charge.Status == "ACTIVE" && brCode == "") {
		return WooviCharge{}, errors.New("woovi charge response is missing status or valid expiry, or active charge brCode")
	}
	transactionID := decoded.Charge.TransactionID
	if transactionID == "" {
		transactionID = decoded.Charge.Identifier
	}
	if transactionID == "" {
		transactionID = decoded.TransactionID
	}
	if transactionID == "" {
		transactionID = decoded.Identifier
	}
	value := decoded.Charge.Value
	if value == 0 {
		value = amountCents
	}
	return WooviCharge{CorrelationID: correlationID, TransactionID: transactionID, Status: decoded.Charge.Status, Value: value, BRCode: brCode, ExpiresAt: expiresAt.UTC()}, nil
}

// SimulatePayment invokes Woovi's test-only charge payment endpoint. The
// configured base URL is validated as the sandbox before this method is exposed.
func (c *WooviChargeClient) SimulatePayment(ctx context.Context, transactionID string) error {
	if transactionID == "" || len(transactionID) > 128 {
		return errors.New("invalid Woovi sandbox transactionID")
	}
	endpoint := *c.base
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/openpix/testing"
	endpoint.RawPath = ""
	query := endpoint.Query()
	query.Set("transactionID", transactionID)
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return fmt.Errorf("build Woovi sandbox payment simulation request: %w", err)
	}
	request.Header.Set("Authorization", c.appID)
	request.Header.Set("Accept", "application/json")
	response, err := c.client.Do(request)
	if err != nil {
		return fmt.Errorf("woovi sandbox payment simulation failed: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("woovi sandbox payment simulation returned HTTP %d", response.StatusCode)
	}
	if _, err := io.Copy(io.Discard, io.LimitReader(response.Body, maxWooviResponseBytes+1)); err != nil {
		return fmt.Errorf("read Woovi sandbox payment simulation response: %w", err)
	}
	return nil
}

// GetCharge retrieves one charge by its documented charge ID or correlationID.
// This read is suitable for reconciling an ambiguous create outcome using the
// exact correlation ID already committed in the local attempt record.
func (c *WooviChargeClient) GetCharge(ctx context.Context, correlationID string) (WooviCharge, error) {
	if correlationID == "" || len(correlationID) > 128 {
		return WooviCharge{}, errors.New("invalid charge correlationID")
	}
	endpoint := *c.base
	basePath := strings.TrimRight(endpoint.Path, "/") + "/api/v1/charge/"
	endpoint.Path = basePath + correlationID
	endpoint.RawPath = basePath + url.PathEscape(correlationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return WooviCharge{}, fmt.Errorf("build woovi charge lookup request: %w", err)
	}
	req.Header.Set("Authorization", c.appID)
	req.Header.Set("Accept", "application/json")
	response, err := c.client.Do(req)
	if err != nil {
		return WooviCharge{}, fmt.Errorf("woovi charge lookup failed: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return WooviCharge{}, fmt.Errorf("woovi charge lookup returned HTTP %d", response.StatusCode)
	}
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxWooviResponseBytes+1))
	if err != nil {
		return WooviCharge{}, fmt.Errorf("read woovi charge lookup response: %w", err)
	}
	if len(responseBody) > maxWooviResponseBytes {
		return WooviCharge{}, errors.New("woovi charge lookup response exceeds size limit")
	}
	var decoded struct {
		Charge struct {
			CorrelationID string `json:"correlationID"`
			TransactionID string `json:"transactionID"`
			Identifier    string `json:"identifier"`
			Value         int64  `json:"value"`
			Status        string `json:"status"`
			BRCode        string `json:"brCode"`
			ExpiresDate   string `json:"expiresDate"`
		} `json:"charge"`
	}
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return WooviCharge{}, fmt.Errorf("decode woovi charge lookup response: %w", err)
	}
	if decoded.Charge.CorrelationID != correlationID || decoded.Charge.Status == "" {
		return WooviCharge{}, errors.New("woovi charge lookup returned a mismatched or incomplete charge")
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, decoded.Charge.ExpiresDate)
	if err != nil {
		return WooviCharge{}, errors.New("woovi charge lookup returned an invalid expiresDate")
	}
	transactionID := decoded.Charge.TransactionID
	if transactionID == "" {
		transactionID = decoded.Charge.Identifier
	}
	return WooviCharge{
		CorrelationID: decoded.Charge.CorrelationID,
		TransactionID: transactionID,
		Status:        decoded.Charge.Status,
		Value:         decoded.Charge.Value,
		BRCode:        decoded.Charge.BRCode,
		ExpiresAt:     expiresAt.UTC(),
	}, nil
}
