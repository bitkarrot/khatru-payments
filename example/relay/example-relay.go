//go:build relay

package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	payments "github.com/bitkarrot/khatru-payments"
	"github.com/fiatjaf/khatru"
	"github.com/joho/godotenv"
	"github.com/nbd-wtf/go-nostr"
)

func main() {
	// Load .env file
	if err := godotenv.Load(); err != nil {
		log.Printf("Warning: .env file not found or could not be loaded: %v", err)
	}

	// Initialize payment system from environment variables
	paymentSystem, err := payments.NewFromEnv()
	if err != nil {
		log.Fatal("Failed to initialize payment system:", err)
	}

	// Create khatru relay
	relay := khatru.NewRelay()

	// Set relay info
	relay.Info.Name = "Example Relay with Payments"
	relay.Info.Description = "A relay that requires payment for non-WoT users"
	relay.Info.PubKey = "your-relay-pubkey"
	relay.Info.Contact = "admin@example.com"

	// Add payment-based event rejection
	relay.RejectEvent = append(relay.RejectEvent, func(ctx context.Context, event *nostr.Event) (bool, string) {
		// Your WoT logic here - check if user is in Web of Trust
		isInWoT := checkWebOfTrust(event.PubKey)

		if isInWoT {
			return false, "" // Allow WoT users
		}

		// For non-WoT users, use payment system
		return paymentSystem.RejectEventHandler(ctx, event)
	})

	// Register payment endpoints
	mux := relay.Router()
	paymentSystem.RegisterHandlers(mux)

	// Add custom endpoint to show payment stats
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		stats := paymentSystem.GetStats()
		response := map[string]interface{}{
			"relay":         "Example Relay with Payments",
			"payment_stats": stats,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	})

	log.Println("🚀 Relay with payment system running on :3334")
	log.Println("💰 Payment endpoints:")
	log.Println("   POST /verify-payment")
	log.Println("   POST /webhook/zbd")
	log.Println("   GET /debug/payments")

	if err := http.ListenAndServe(":3334", relay); err != nil {
		log.Fatal(err)
	}
}

// checkWebOfTrust - implement your WoT logic here
func checkWebOfTrust(pubkey string) bool {
	// This is where you'd implement your Web of Trust logic
	// For example, check if pubkey is in your trust network
	return false // For demo, treat all users as non-WoT
}
