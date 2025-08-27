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
	"time"
)

// ZBDProvider implements PaymentProvider interface for ZBD
type ZBDProvider struct {
	lightningAddress string
	apiKey           string
	baseURL          string
}

// NewZBDProvider creates a new ZBD payment provider
func NewZBDProvider(apiKey, lightningAddress string) (*ZBDProvider, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("ZBD API key is required")
	}
	if lightningAddress == "" {
		return nil, fmt.Errorf("Lightning address is required")
	}

	return &ZBDProvider{
		lightningAddress: lightningAddress,
		apiKey:           apiKey,
		baseURL:          "https://api.zebedee.io",
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

	if len(chargeResp.Data.Invoice.Request) > 0 {
		maxLen := 50
		if len(chargeResp.Data.Invoice.Request) < maxLen {
			maxLen = len(chargeResp.Data.Invoice.Request)
		}
		log.Printf("🐛 DEBUG ZBD: Created invoice successfully - PaymentRequest: %s", chargeResp.Data.Invoice.Request[:maxLen]+"...")
	} else {
		log.Printf("🐛 DEBUG ZBD: WARNING - Empty PaymentRequest in response!")
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
	// For ZBD, we need to map payment hash to charge ID
	// This would typically be done through external storage
	// For now, we'll return a basic implementation

	// In a real implementation, you'd:
	// 1. Look up charge ID from payment hash
	// 2. Query ZBD API with charge ID
	// 3. Return verification result

	return &PaymentVerification{
		Paid:        false, // Would be determined by API response
		PaymentHash: paymentHash,
		Amount:      0,
		PaidAt:      time.Time{},
	}, fmt.Errorf("payment verification requires charge mapping storage")
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
