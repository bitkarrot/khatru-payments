package payments

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// PhoenixdProvider implements PaymentProvider interface for phoenixd
type PhoenixdProvider struct {
	baseURL              string
	password             string
	// Map payment hash to external ID for verification
	paymentMap           map[string]string
	// Map payment hash to pubkey for verification
	pubkeyMap            map[string]string
	mu                   sync.RWMutex
	// Persistent storage references
	chargeMappingStorage *ChargeMappingStorage
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
		baseURL:    baseURL,
		password:   password,
		paymentMap: make(map[string]string),
		pubkeyMap:  make(map[string]string),
	}, nil
}

// NewPhoenixdProviderWithStorage creates a new phoenixd payment provider with persistent storage
func NewPhoenixdProviderWithStorage(baseURL, password string, chargeMappingStorage *ChargeMappingStorage) (*PhoenixdProvider, error) {
	if password == "" {
		return nil, fmt.Errorf("phoenixd password is required")
	}
	if baseURL == "" {
		baseURL = "http://localhost:9740"
	}

	return &PhoenixdProvider{
		baseURL:              baseURL,
		password:             password,
		paymentMap:           make(map[string]string),
		pubkeyMap:            make(map[string]string),
		chargeMappingStorage: chargeMappingStorage,
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
	CreatedAt      int64  `json:"createdAt"`
	ExpiresAt      int64  `json:"expiresAt"`
}

type PhoenixdPaymentResponse struct {
	PaymentHash   string `json:"paymentHash"`
	Preimage      string `json:"preimage"`
	ExternalID    string `json:"externalId"`
	Description   string `json:"description"`
	Invoice       string `json:"invoice"`
	IsPaid        bool   `json:"isPaid"`
	ReceivedSat   int64  `json:"receivedSat"`
	Fees          int64  `json:"fees"`
	CompletedAt   int64  `json:"completedAt"`
	CreatedAt     int64  `json:"createdAt"`
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

	// phoenixd expects form data, not JSON
	formData := fmt.Sprintf("amountSat=%d&description=%s&externalId=%s", 
		amountSat, 
		description, 
		externalID)

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/createinvoice", strings.NewReader(formData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
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

	// Store payment hash and pubkey mapping for payment verification
	p.mu.Lock()
	p.paymentMap[invoiceResp.PaymentHash] = externalID
	p.pubkeyMap[invoiceResp.PaymentHash] = pubkey
	p.mu.Unlock()
	
	// Also store in persistent storage if available
	if p.chargeMappingStorage != nil {
		p.chargeMappingStorage.Store(invoiceResp.PaymentHash, externalID)
	}

	// Convert timestamps
	expiresAt := time.Unix(invoiceResp.ExpiresAt, 0)

	return &Invoice{
		PaymentRequest: invoiceResp.Serialized,
		PaymentHash:    invoiceResp.PaymentHash,
		Amount:         amount, // return original amount in millisatoshis
		Description:    invoiceResp.Description,
		ExpiresAt:      expiresAt,
	}, nil
}

// VerifyPayment checks if a payment has been completed
func (p *PhoenixdProvider) VerifyPayment(ctx context.Context, paymentHash string) (*PaymentVerification, error) {
	// Get external ID from payment map or persistent storage
	p.mu.RLock()
	externalID, exists := p.paymentMap[paymentHash]
	p.mu.RUnlock()

	// If not found in memory, try persistent storage
	if !exists && p.chargeMappingStorage != nil {
		if storedID, found := p.chargeMappingStorage.Get(paymentHash); found {
			externalID = storedID
			exists = true
			// Store back in memory for faster future access
			p.mu.Lock()
			p.paymentMap[paymentHash] = externalID
			p.mu.Unlock()
		}
	}

	if !exists {
		return &PaymentVerification{
			Paid:        false,
			PaymentHash: paymentHash,
		}, nil
	}

	req, err := http.NewRequestWithContext(ctx, "GET", p.baseURL+"/payments/incoming/"+paymentHash, nil)
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
	paidAt := time.Unix(paymentResp.CompletedAt, 0)

	verification := &PaymentVerification{
		Paid:        paymentResp.IsPaid,
		PaymentHash: paymentHash,
		Amount:      amountMsat,
		PaidAt:      paidAt,
	}

	return verification, nil
}

// CheckExistingPayments checks for any existing payments for a pubkey and returns verification if paid
func (p *PhoenixdProvider) CheckExistingPayments(ctx context.Context, pubkey string) (*PaymentVerification, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	for paymentHash, storedPubkey := range p.pubkeyMap {
		if storedPubkey == pubkey {
			log.Printf("🔍 Found payment for this pubkey - checking hash: %s", paymentHash)
			verification, err := p.VerifyPayment(ctx, paymentHash)
			if err == nil && verification.Paid {
				log.Printf("💰 Found paid invoice! Payment hash: %s", paymentHash)
				return verification, nil
			}
		}
	}
	
	return nil, nil // No paid payments found
}
