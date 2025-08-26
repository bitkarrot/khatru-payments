package payments

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/nbd-wtf/go-nostr"
)

// PaymentProvider interface for different Lightning payment providers
type PaymentProvider interface {
	// CreateInvoice creates a payment invoice for the specified amount
	CreateInvoice(ctx context.Context, amount int64, description string, pubkey string) (*Invoice, error)

	// VerifyPayment checks if a payment has been completed
	VerifyPayment(ctx context.Context, paymentHash string) (*PaymentVerification, error)

	// GetProviderName returns the name of the payment provider
	GetProviderName() string
}

// Invoice represents a Lightning invoice
type Invoice struct {
	PaymentRequest string    `json:"payment_request"`
	PaymentHash    string    `json:"payment_hash"`
	Amount         int64     `json:"amount"`
	Description    string    `json:"description"`
	ExpiresAt      time.Time `json:"expires_at"`
}

// PaymentVerification represents the result of payment verification
type PaymentVerification struct {
	Paid        bool      `json:"paid"`
	PaymentHash string    `json:"payment_hash"`
	Amount      int64     `json:"amount"`
	PaidAt      time.Time `json:"paid_at"`
}

// PaymentRequest represents the response sent to users who need to pay
type PaymentRequest struct {
	Message string `json:"message"`
	Invoice string `json:"invoice"`
	Amount  int64  `json:"amount"`
}

// Config holds payment system configuration
type Config struct {
	Provider           string        `json:"provider"`             // "zbd" or "phoenixd"
	PaymentAmount      int64         `json:"payment_amount"`       // in millisatoshis
	AccessDuration     string        `json:"access_duration"`      // "1week", "1month", "1year", "forever"
	LightningAddress   string        `json:"lightning_address"`    // for ZBD
	ZBDAPIKey          string        `json:"zbd_api_key"`          // for ZBD
	PhoenixdURL        string        `json:"phoenixd_url"`         // for phoenixd
	PhoenixdPassword   string        `json:"phoenixd_password"`    // for phoenixd
	PaidAccessFile     string        `json:"paid_access_file"`     // storage file path
	ChargeMappingFile  string        `json:"charge_mapping_file"`  // charge mapping file path
	RejectMessage      string        `json:"reject_message"`       // custom rejection message
}

// System represents the payment system
type System struct {
	config              Config
	provider            PaymentProvider
	paidAccessStorage   *PaidAccessStorage
	chargeMappingStorage *ChargeMappingStorage
	accessDuration      time.Duration
	
	// Performance counters
	paymentRequests    uint64
	successfulPayments uint64
}

// New creates a new payment system
func New(config Config) (*System, error) {
	// Set defaults
	if config.PaymentAmount == 0 {
		config.PaymentAmount = 21000 // 21 sats
	}
	if config.AccessDuration == "" {
		config.AccessDuration = "1month"
	}
	if config.PaidAccessFile == "" {
		config.PaidAccessFile = "./data/paid_access.json"
	}
	if config.ChargeMappingFile == "" {
		config.ChargeMappingFile = "./data/charge_mappings.json"
	}
	if config.RejectMessage == "" {
		config.RejectMessage = "You are not part of the WoT, payment required to join relay"
	}

	// Parse access duration
	accessDuration := calculateExpirationTime(config.AccessDuration).Sub(time.Now())

	// Initialize provider
	var provider PaymentProvider
	var err error

	switch config.Provider {
	case "zbd":
		if config.ZBDAPIKey == "" {
			return nil, fmt.Errorf("ZBD_API_KEY required for ZBD provider")
		}
		if config.LightningAddress == "" {
			return nil, fmt.Errorf("LIGHTNING_ADDRESS required for ZBD provider")
		}
		provider, err = NewZBDProvider(config.ZBDAPIKey, config.LightningAddress)
	case "phoenixd":
		if config.PhoenixdPassword == "" {
			return nil, fmt.Errorf("PHOENIXD_PASSWORD required for phoenixd provider")
		}
		if config.PhoenixdURL == "" {
			config.PhoenixdURL = "http://localhost:9740"
		}
		provider, err = NewPhoenixdProvider(config.PhoenixdURL, config.PhoenixdPassword)
	default:
		return nil, fmt.Errorf("unsupported payment provider: %s (supported: zbd, phoenixd)", config.Provider)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to initialize %s provider: %w", config.Provider, err)
	}

	// Initialize storage
	paidAccessStorage := NewPaidAccessStorage(config.PaidAccessFile)
	chargeMappingStorage := NewChargeMappingStorage(config.ChargeMappingFile)

	system := &System{
		config:               config,
		provider:             provider,
		paidAccessStorage:    paidAccessStorage,
		chargeMappingStorage: chargeMappingStorage,
		accessDuration:       accessDuration,
	}

	// Start cleanup routine
	go system.startCleanupRoutine()

	log.Printf("💰 Payment system initialized with %s provider", provider.GetProviderName())
	log.Printf("💰 Lightning Address: %s", config.LightningAddress)
	log.Printf("💰 Payment Amount: %d msat (%d sats)", config.PaymentAmount, config.PaymentAmount/1000)
	log.Printf("💰 Access Duration: %s", config.AccessDuration)

	return system, nil
}

// NewFromEnv creates a payment system from environment variables
func NewFromEnv() (*System, error) {
	config := Config{
		Provider:           getEnvWithDefault("PAYMENT_PROVIDER", "zbd"),
		LightningAddress:   os.Getenv("LIGHTNING_ADDRESS"),
		ZBDAPIKey:          os.Getenv("ZBD_API_KEY"),
		PhoenixdURL:        getEnvWithDefault("PHOENIXD_URL", "http://localhost:9740"),
		PhoenixdPassword:   os.Getenv("PHOENIXD_PASSWORD"),
		AccessDuration:     getEnvWithDefault("ACCESS_DURATION", "1month"),
		PaidAccessFile:     getEnvWithDefault("PAID_ACCESS_FILE", "./data/paid_access.json"),
		ChargeMappingFile:  getEnvWithDefault("CHARGE_MAPPING_FILE", "./data/charge_mappings.json"),
		RejectMessage:      getEnvWithDefault("PAYMENT_REJECT_MESSAGE", "You are not part of the WoT, payment required to join relay"),
	}

	// Parse payment amount
	if amountStr := os.Getenv("PAYMENT_AMOUNT_MSAT"); amountStr != "" {
		amount, err := strconv.ParseInt(amountStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid PAYMENT_AMOUNT_MSAT: %w", err)
		}
		config.PaymentAmount = amount
	}

	return New(config)
}

// HasAccess checks if a pubkey has valid paid access
func (s *System) HasAccess(pubkey string) bool {
	return s.paidAccessStorage.HasAccess(pubkey)
}

// CreateInvoice creates an invoice for a pubkey
func (s *System) CreateInvoice(ctx context.Context, pubkey string) (*Invoice, error) {
	description := fmt.Sprintf("WoT Relay Access - pubkey:%s", pubkey)
	
	return s.provider.CreateInvoice(
		ctx,
		s.config.PaymentAmount,
		description,
		pubkey,
	)
}

// VerifyPayment verifies a payment and grants access if paid
func (s *System) VerifyPayment(ctx context.Context, paymentHash, pubkey string) (*PaymentVerification, error) {
	verification, err := s.provider.VerifyPayment(ctx, paymentHash)
	if err != nil {
		return nil, err
	}

	if verification.Paid {
		err = s.paidAccessStorage.AddPaidAccess(
			pubkey,
			paymentHash,
			verification.Amount,
			s.accessDuration,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to grant access: %w", err)
		}

		atomic.AddUint64(&s.successfulPayments, 1)
		log.Printf("💰 Payment verified and access granted for pubkey: %s...", pubkey[:16])
	}

	return verification, nil
}

// RejectEventHandler returns a khatru RejectEvent function
func (s *System) RejectEventHandler(ctx context.Context, event *nostr.Event) (bool, string) {
	// Check if user has paid access
	if s.HasAccess(event.PubKey) {
		log.Printf("💰 Allowing event from paid user: %s...", event.PubKey[:16])
		return false, ""
	}

	// User hasn't paid, reject with payment request
	atomic.AddUint64(&s.paymentRequests, 1)

	// Create payment request
	invoice, err := s.CreateInvoice(ctx, event.PubKey)
	if err != nil {
		log.Printf("❌ Failed to create invoice for %s: %v", event.PubKey[:16], err)
		return true, "payment required but invoice creation failed"
	}

	paymentReq := PaymentRequest{
		Message: s.config.RejectMessage,
		Invoice: invoice.PaymentRequest,
		Amount:  invoice.Amount,
	}

	paymentJSON, _ := json.Marshal(paymentReq)
	return true, string(paymentJSON)
}

// RegisterHandlers registers HTTP handlers for payment endpoints
func (s *System) RegisterHandlers(mux *http.ServeMux) {
	mux.HandleFunc("POST /verify-payment", s.verifyPaymentHandler)
	mux.HandleFunc("POST /webhook/zbd", s.zbdWebhookHandler)
	mux.HandleFunc("GET /debug/payments", s.debugPaymentsHandler)
}

// GetStats returns payment statistics
func (s *System) GetStats() map[string]interface{} {
	accessStats := s.paidAccessStorage.GetStats()
	
	return map[string]interface{}{
		"payment_requests":     atomic.LoadUint64(&s.paymentRequests),
		"successful_payments":  atomic.LoadUint64(&s.successfulPayments),
		"total_members":        accessStats["total_members"],
		"active_members":       accessStats["active_members"],
		"expired_members":      accessStats["expired_members"],
		"provider":             s.provider.GetProviderName(),
		"lightning_address":    s.config.LightningAddress,
		"payment_amount_msat":  s.config.PaymentAmount,
		"payment_amount_sats":  s.config.PaymentAmount / 1000,
		"access_duration":      s.config.AccessDuration,
	}
}

// startCleanupRoutine starts the cleanup routine for expired access
func (s *System) startCleanupRoutine() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := s.paidAccessStorage.CleanupExpired(); err != nil {
				log.Printf("❌ Error cleaning up expired access: %v", err)
			}
			s.chargeMappingStorage.Cleanup()
		}
	}
}

// calculateExpirationTime calculates expiration time based on duration string
func calculateExpirationTime(duration string) time.Time {
	switch duration {
	case "forever":
		return time.Time{} // Zero time means never expires
	case "1week":
		return time.Now().AddDate(0, 0, 7)
	case "1month":
		return time.Now().AddDate(0, 1, 0)
	case "1year":
		return time.Now().AddDate(1, 0, 0)
	default:
		// Try to parse as duration string (e.g., "720h")
		if d, err := time.ParseDuration(duration); err == nil {
			return time.Now().Add(d)
		}
		// Default to 1 month if parsing fails
		return time.Now().AddDate(0, 1, 0)
	}
}

// getEnvWithDefault gets environment variable with default value
func getEnvWithDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
