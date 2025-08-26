package payments

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"sync/atomic"
)

// verifyPaymentHandler handles manual payment verification requests
func (s *System) verifyPaymentHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		PaymentHash string `json:"payment_hash"`
		Pubkey      string `json:"pubkey"`
	}

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.PaymentHash == "" || req.Pubkey == "" {
		http.Error(w, "payment_hash and pubkey are required", http.StatusBadRequest)
		return
	}

	// Verify payment using the configured provider
	verification, err := s.VerifyPayment(r.Context(), req.PaymentHash, req.Pubkey)
	if err != nil {
		log.Printf("❌ Payment verification failed: %v", err)
		http.Error(w, "Payment verification failed", http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"paid":         verification.Paid,
		"payment_hash": verification.PaymentHash,
		"amount":       verification.Amount,
	}

	if verification.Paid {
		log.Printf("💰 Payment verified and access granted for pubkey: %s...", req.Pubkey[:16])
		response["access_granted"] = true
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// zbdWebhookHandler handles ZBD webhook notifications
func (s *System) zbdWebhookHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Printf("❌ Failed to read ZBD webhook body: %v", err)
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	// Try to handle webhook with ZBD provider
	if zbdProvider, ok := s.provider.(*ZBDProvider); ok {
		verification, pubkey, err := zbdProvider.HandleWebhook(body)
		if err != nil {
			log.Printf("❌ Failed to process ZBD webhook: %v", err)
			http.Error(w, "Failed to process webhook", http.StatusInternalServerError)
			return
		}

		if verification != nil && verification.Paid && pubkey != "" {
			// Grant access
			err = s.paidAccessStorage.AddPaidAccess(
				pubkey,
				verification.PaymentHash,
				verification.Amount,
				s.accessDuration,
			)
			if err != nil {
				log.Printf("❌ Failed to add paid access: %v", err)
				http.Error(w, "Failed to grant access", http.StatusInternalServerError)
				return
			}

			atomic.AddUint64(&s.successfulPayments, 1)
			log.Printf("💰 Webhook processed: access granted for pubkey: %s...", pubkey[:16])
		}
	} else {
		log.Printf("❌ ZBD webhook received but provider is not ZBD")
		http.Error(w, "Invalid webhook for current provider", http.StatusBadRequest)
		return
	}

	// Respond with success
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// debugPaymentsHandler provides payment statistics
func (s *System) debugPaymentsHandler(w http.ResponseWriter, r *http.Request) {
	stats := s.GetStats()

	paymentStats := fmt.Sprintf(`Payment Statistics:

Payment Requests: %v
Successful Payments: %v
Total Paid Members: %v
Active Paid Members: %v
Expired Paid Members: %v

Payment Configuration:
Lightning Address: %v
Payment Amount: %v msat (%v sats)
Access Duration: %v
Provider: %v
`,
		stats["payment_requests"],
		stats["successful_payments"],
		stats["total_members"],
		stats["active_members"],
		stats["expired_members"],
		stats["lightning_address"],
		stats["payment_amount_msat"],
		stats["payment_amount_sats"],
		stats["access_duration"],
		stats["provider"],
	)

	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(paymentStats))
}
