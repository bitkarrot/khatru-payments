package payments

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// ZBDProvider implements PaymentProvider interface for ZBD
type ZBDProvider struct {
	apiKey               string
	baseURL              string
	lightning            string
	// Map payment hash to charge ID for verification
	chargeMap            map[string]string
	// Map payment hash to pubkey for verification
	pubkeyMap            map[string]string
	mu                   sync.RWMutex
	// Persistent storage references
	chargeMappingStorage *ChargeMappingStorage
}

// NewZBDProvider creates a new ZBD payment provider
func NewZBDProvider(apiKey, lightningAddress string) (*ZBDProvider, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("zBD API key is required")
	}
	if lightningAddress == "" {
		return nil, fmt.Errorf("lightning address is required")
	}

	return &ZBDProvider{
		apiKey:    apiKey,
		baseURL:   "https://api.zebedee.io",
		lightning: lightningAddress,
		chargeMap: make(map[string]string),
		pubkeyMap: make(map[string]string),
	}, nil
}

// NewZBDProviderWithStorage creates a new ZBD payment provider with persistent storage
func NewZBDProviderWithStorage(apiKey, lightningAddress string, chargeMappingStorage *ChargeMappingStorage) (*ZBDProvider, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("zBD API key is required")
	}
	if lightningAddress == "" {
		return nil, fmt.Errorf("lightning address is required")
	}

	return &ZBDProvider{
		apiKey:               apiKey,
		baseURL:              "https://api.zebedee.io",
		lightning:            lightningAddress,
		chargeMap:            make(map[string]string),
		pubkeyMap:            make(map[string]string),
		chargeMappingStorage: chargeMappingStorage,
	}, nil
}

// GetProviderName returns the provider name
func (z *ZBDProvider) GetProviderName() string {
	return "ZBD"
}

// ZBD API structures
type ZBDChargeRequest struct {
	Amount      string `json:"amount"`
	Description string `json:"description"`
	InternalID  string `json:"internalId,omitempty"`
	ExpiresIn   int    `json:"expiresIn,omitempty"`
}

type ZBDInvoice struct {
	Request  string `json:"request"`
	URI      string `json:"uri"`
	Preimage string `json:"preimage"`
}

type ZBDChargeData struct {
	ID          string     `json:"id"`
	Unit        string     `json:"unit"`
	Amount      string     `json:"amount"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	Invoice     ZBDInvoice `json:"invoice"`
	InternalID  string     `json:"internalId"`
	CreatedAt   string     `json:"createdAt"`
	ExpiresAt   string     `json:"expiresAt"`
	ConfirmedAt string     `json:"confirmedAt,omitempty"`
}

type ZBDChargeResponse struct {
	Success bool          `json:"success"`
	Data    ZBDChargeData `json:"data"`
	Message string        `json:"message"`
}

// CreateInvoice creates a Lightning invoice using ZBD Charges API
func (z *ZBDProvider) CreateInvoice(ctx context.Context, amount int64, description string, pubkey string) (*Invoice, error) {
	log.Printf("🐛 DEBUG ZBD: Creating invoice for pubkey=%s, amount=%d", pubkey[:16]+"...", amount)

	// Create internal ID using pubkey hash for tracking
	hash := sha256.Sum256([]byte(pubkey + fmt.Sprintf("%d", time.Now().Unix())))
	internalID := hex.EncodeToString(hash[:])[:16]

	chargeReq := ZBDChargeRequest{
		Amount:      fmt.Sprintf("%d", amount), // amount in millisatoshis
		Description: description,
		InternalID:  internalID,
		ExpiresIn:   3600, // 1 hour expiry
	}

	log.Printf("🐛 DEBUG ZBD: Charge request: %+v", chargeReq)

	reqBody, err := json.Marshal(chargeReq)
	if err != nil {
		log.Printf("🐛 DEBUG ZBD: Failed to marshal request: %v", err)
		return nil, fmt.Errorf("failed to marshal charge request: %w", err)
	}

	log.Printf("🐛 DEBUG ZBD: Making request to %s", z.baseURL+"/v0/charges")
	req, err := http.NewRequestWithContext(ctx, "POST", z.baseURL+"/v0/charges", bytes.NewBuffer(reqBody))
	if err != nil {
		log.Printf("🐛 DEBUG ZBD: Failed to create request: %v", err)
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apikey", z.apiKey)
	
	log.Printf("🐛 DEBUG ZBD: API Key length: %d", len(z.apiKey))
	log.Printf("🐛 DEBUG ZBD: Request headers: %+v", req.Header)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("🐛 DEBUG ZBD: Request failed: %v", err)
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("🐛 DEBUG ZBD: Failed to read response: %v", err)
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	log.Printf("🐛 DEBUG ZBD: Response status: %d", resp.StatusCode)
	log.Printf("🐛 DEBUG ZBD: Response body: %s", string(body))

	if resp.StatusCode != http.StatusOK {
		log.Printf("🐛 DEBUG ZBD: API error: %d - %s", resp.StatusCode, string(body))
		return nil, fmt.Errorf("ZBD API error: %d - %s", resp.StatusCode, string(body))
	}

	var chargeResp ZBDChargeResponse
	if err := json.Unmarshal(body, &chargeResp); err != nil {
		log.Printf("🐛 DEBUG ZBD: Failed to unmarshal response: %v", err)
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	log.Printf("🐛 DEBUG ZBD: Parsed response: %+v", chargeResp)

	// Parse amount back to int64
	amountMsat, err := strconv.ParseInt(chargeResp.Data.Amount, 10, 64)
	if err != nil {
		log.Printf("🐛 DEBUG ZBD: Failed to parse amount, using fallback: %v", err)
		amountMsat = amount // fallback to requested amount
	}

	// Parse expiry timestamp
	expiresAt, _ := time.Parse(time.RFC3339, chargeResp.Data.ExpiresAt)

	// Generate payment hash for tracking
	paymentHash := generatePaymentHash(chargeResp.Data.Invoice.Request, pubkey)
	
	// Store charge ID and pubkey mapping for payment verification
	z.mu.Lock()
	z.chargeMap[paymentHash] = chargeResp.Data.ID
	z.pubkeyMap[paymentHash] = pubkey
	z.mu.Unlock()
	
	// Also store in persistent storage if available
	if z.chargeMappingStorage != nil {
		z.chargeMappingStorage.Store(paymentHash, chargeResp.Data.ID)
	}
	
	log.Printf("🐛 DEBUG ZBD: Stored mapping - PaymentHash: %s -> ChargeID: %s, Pubkey: %s...", paymentHash, chargeResp.Data.ID, pubkey[:16])

	if len(chargeResp.Data.Invoice.Request) > 50 {
		log.Printf("🐛 DEBUG ZBD: Created invoice successfully - PaymentRequest: %s...", chargeResp.Data.Invoice.Request[:50])
	} else {
		log.Printf("🐛 DEBUG ZBD: Created invoice successfully - PaymentRequest: %s", chargeResp.Data.Invoice.Request)
	}

	return &Invoice{
		PaymentRequest: chargeResp.Data.Invoice.Request,
		PaymentHash:    paymentHash,
		Amount:         amountMsat,
		Description:    chargeResp.Data.Description,
		ExpiresAt:      expiresAt,
	}, nil
}

// VerifyPayment verifies a payment using ZBD API
func (z *ZBDProvider) VerifyPayment(ctx context.Context, paymentHash string) (*PaymentVerification, error) {
	// Check in-memory mapping first
	z.mu.RLock()
	chargeID, exists := z.chargeMap[paymentHash]
	z.mu.RUnlock()
	
	// If not found in memory, check persistent storage
	if !exists && z.chargeMappingStorage != nil {
		chargeID, exists = z.chargeMappingStorage.Get(paymentHash)
		if exists {
			// Load back into memory for faster future access
			z.mu.Lock()
			z.chargeMap[paymentHash] = chargeID
			z.mu.Unlock()
		}
	}
	
	if !exists {
		return &PaymentVerification{
			Paid:        false,
			PaymentHash: paymentHash,
			Amount:      0,
			PaidAt:      time.Time{},
		}, fmt.Errorf("charge ID not found for payment hash: %s", paymentHash)
	}
	
	log.Printf("🐛 DEBUG ZBD: Verifying payment - PaymentHash: %s -> ChargeID: %s", paymentHash, chargeID)
	
	// Query ZBD API to get charge status
	req, err := http.NewRequestWithContext(ctx, "GET", z.baseURL+"/v0/charges/"+chargeID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	req.Header.Set("apikey", z.apiKey)
	req.Header.Set("Content-Type", "application/json")
	
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	
	log.Printf("🐛 DEBUG ZBD: Verify response status: %d", resp.StatusCode)
	log.Printf("🐛 DEBUG ZBD: Verify response body: %s", string(body))
	
	if resp.StatusCode != 200 {
		return &PaymentVerification{
			Paid:        false,
			PaymentHash: paymentHash,
			Amount:      0,
			PaidAt:      time.Time{},
		}, fmt.Errorf("ZBD API error: %d - %s", resp.StatusCode, string(body))
	}
	
	var chargeResp ZBDChargeResponse
	if err := json.Unmarshal(body, &chargeResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	
	// Check if payment is confirmed
	isPaid := chargeResp.Data.Status == "completed"
	var paidAt time.Time
	var amount int64
	
	if isPaid && chargeResp.Data.ConfirmedAt != "" {
		paidAt, _ = time.Parse(time.RFC3339, chargeResp.Data.ConfirmedAt)
	}
	
	if chargeResp.Data.Amount != "" {
		amount, _ = strconv.ParseInt(chargeResp.Data.Amount, 10, 64)
	}
	
	log.Printf("🐛 DEBUG ZBD: Payment verification result - Paid: %v, Status: %s, Amount: %d", isPaid, chargeResp.Data.Status, amount)
	
	return &PaymentVerification{
		Paid:        isPaid,
		PaymentHash: paymentHash,
		Amount:      amount,
		PaidAt:      paidAt,
	}, nil
}

// ZBDWebhookPayload represents the webhook payload from ZBD
type ZBDWebhookPayload struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	Amount      string `json:"amount"`
	Description string `json:"description"`
	CreatedAt   string `json:"createdAt"`
	PaidAt      string `json:"paidAt,omitempty"`
	ExpiresAt   string `json:"expiresAt"`
}

// HandleWebhook processes ZBD webhook notifications
func (z *ZBDProvider) HandleWebhook(payload []byte) (*PaymentVerification, string, error) {
	var webhookPayload ZBDWebhookPayload
	if err := json.Unmarshal(payload, &webhookPayload); err != nil {
		return nil, "", fmt.Errorf("failed to unmarshal webhook payload: %w", err)
	}

	log.Printf("💰 Received ZBD webhook: ID=%s, Status=%s", webhookPayload.ID, webhookPayload.Status)

	if webhookPayload.Status != "completed" && webhookPayload.Status != "settled" {
		log.Printf("💰 Payment not completed yet: %s", webhookPayload.Status)
		return nil, "", nil
	}

	// Extract pubkey from description
	pubkey := extractPubkeyFromDescription(webhookPayload.Description)
	if pubkey == "" {
		return nil, "", fmt.Errorf("could not extract pubkey from webhook payload")
	}

	// Parse amount
	amount, err := strconv.ParseInt(webhookPayload.Amount, 10, 64)
	if err != nil {
		return nil, "", fmt.Errorf("invalid amount in webhook: %w", err)
	}

	verification := &PaymentVerification{
		Paid:        true,
		PaymentHash: webhookPayload.ID, // Use ZBD charge ID as payment hash
		Amount:      amount,
		PaidAt:      time.Now(),
	}

	return verification, pubkey, nil
}

// generatePaymentHash creates a deterministic hash for tracking payments
func generatePaymentHash(paymentRequest, pubkey string) string {
	data := fmt.Sprintf("%s:%s:%d", paymentRequest, pubkey, time.Now().Unix())
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// extractPubkeyFromDescription extracts pubkey from payment description
func extractPubkeyFromDescription(description string) string {
	// Look for "pubkey:" prefix in description
	prefix := "pubkey:"
	if len(description) > len(prefix) {
		if description[:len(prefix)] == prefix {
			return description[len(prefix):]
		}
	}

	return ""
}
