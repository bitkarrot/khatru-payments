package payments

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"
)

// PhoenixdProvider implements PaymentProvider interface for phoenixd
type PhoenixdProvider struct {
	baseURL  string
	password string
}

// NewPhoenixdProvider creates a new phoenixd payment provider
func NewPhoenixdProvider(baseURL, password string) (*PhoenixdProvider, error) {
	if password == "" {
		return nil, fmt.Errorf("phoenixd password is required")
	}
	if baseURL == "" {
		baseURL = "http://localhost:9740"
	}

	return &PhoenixdProvider{
		baseURL:  baseURL,
		password: password,
	}, nil
}

// GetProviderName returns the provider name
func (p *PhoenixdProvider) GetProviderName() string {
	return "phoenixd"
}

// Phoenixd API structures
type PhoenixdInvoiceRequest struct {
	AmountSat   int64  `json:"amountSat"`
	Description string `json:"description"`
	ExternalID  string `json:"externalId,omitempty"`
}

type PhoenixdInvoiceResponse struct {
	AmountSat      int64  `json:"amountSat"`
	PaymentHash    string `json:"paymentHash"`
	Serialized     string `json:"serialized"` // BOLT11 invoice
	Description    string `json:"description"`
	ExternalID     string `json:"externalId"`
	CreatedAtUnix  int64  `json:"createdAt"`
	ExpiresAtUnix  int64  `json:"expiresAt"`
}

type PhoenixdPaymentResponse struct {
	PaymentHash     string `json:"paymentHash"`
	Preimage        string `json:"preimage"`
	ExternalID      string `json:"externalId"`
	Description     string `json:"description"`
	Invoice         string `json:"invoice"`
	IsPaid          bool   `json:"isPaid"`
	ReceivedSat     int64  `json:"receivedSat"`
	Fees            int64  `json:"fees"`
	CompletedAtUnix int64  `json:"completedAt"`
	CreatedAtUnix   int64  `json:"createdAt"`
}

// CreateInvoice creates a Lightning invoice using phoenixd
func (p *PhoenixdProvider) CreateInvoice(ctx context.Context, amount int64, description string, pubkey string) (*Invoice, error) {
	// Convert millisatoshis to satoshis
	amountSat := amount / 1000
	if amountSat == 0 {
		amountSat = 1 // minimum 1 sat
	}

	// Create external ID using pubkey hash for tracking
	hash := sha256.Sum256([]byte(pubkey + fmt.Sprintf("%d", time.Now().Unix())))
	externalID := hex.EncodeToString(hash[:])[:16]

	invoiceReq := PhoenixdInvoiceRequest{
		AmountSat:   amountSat,
		Description: description,
		ExternalID:  externalID,
	}

	reqBody, err := json.Marshal(invoiceReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal invoice request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/v1/createinvoice", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("", p.password) // phoenixd uses HTTP basic auth with empty username

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("phoenixd API error: %d - %s", resp.StatusCode, string(body))
	}

	var invoiceResp PhoenixdInvoiceResponse
	if err := json.Unmarshal(body, &invoiceResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Convert timestamps
	expiresAt := time.Unix(invoiceResp.ExpiresAtUnix, 0)

	return &Invoice{
		PaymentRequest: invoiceResp.Serialized,
		PaymentHash:    invoiceResp.PaymentHash,
		Amount:         amount, // return original amount in millisatoshis
		Description:    invoiceResp.Description,
		ExpiresAt:      expiresAt,
	}, nil
}

// VerifyPayment verifies a payment using phoenixd API
func (p *PhoenixdProvider) VerifyPayment(ctx context.Context, paymentHash string) (*PaymentVerification, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", p.baseURL+"/v1/payments/incoming/"+paymentHash, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.SetBasicAuth("", p.password)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		// Payment not found, likely not paid yet
		return &PaymentVerification{
			Paid:        false,
			PaymentHash: paymentHash,
			Amount:      0,
			PaidAt:      time.Time{},
		}, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("phoenixd API error: %d - %s", resp.StatusCode, string(body))
	}

	var paymentResp PhoenixdPaymentResponse
	if err := json.Unmarshal(body, &paymentResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Convert amount back to millisatoshis
	amountMsat := paymentResp.ReceivedSat * 1000

	// Convert timestamp
	paidAt := time.Unix(paymentResp.CompletedAtUnix, 0)

	verification := &PaymentVerification{
		Paid:        paymentResp.IsPaid,
		PaymentHash: paymentHash,
		Amount:      amountMsat,
		PaidAt:      paidAt,
	}

	return verification, nil
}
