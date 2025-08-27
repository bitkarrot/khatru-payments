package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
)

const (
	RelayWSURL   = "ws://localhost:3334"   // WebSocket for Nostr protocol
	RelayHTTPURL = "http://localhost:3334" // HTTP for payment stats/verification
)

type PaymentStats struct {
	PaymentRequests    int    `json:"payment_requests"`
	SuccessfulPayments int    `json:"successful_payments"`
	TotalMembers       int    `json:"total_members"`
	ActiveMembers      int    `json:"active_members"`
	ExpiredMembers     int    `json:"expired_members"`
	Provider           string `json:"provider"`
	LightningAddress   string `json:"lightning_address"`
	PaymentAmountMsat  int    `json:"payment_amount_msat"`
	PaymentAmountSats  int    `json:"payment_amount_sats"`
	AccessDuration     string `json:"access_duration"`
}

type RelayInfo struct {
	Relay        string       `json:"relay"`
	PaymentStats PaymentStats `json:"payment_stats"`
}

type PaymentRequest struct {
	Message string `json:"message"`
	Invoice string `json:"invoice"`
	Amount  int64  `json:"amount"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage:")
		fmt.Println("  go run client.go stats          - Check payment statistics")
		fmt.Println("  go run client.go test-payment    - Test payment flow")
		fmt.Println("  go run client.go connect         - Connect and send test event")
		fmt.Println("  go run client.go verify <hash>   - Verify payment by hash")
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "stats":
		checkPaymentStats()
	case "test-payment":
		testPaymentFlow()
	case "connect":
		connectAndTest()
	case "verify":
		if len(os.Args) < 3 {
			log.Fatal("Usage: go run client.go verify <payment_hash>")
		}
		verifyPayment(os.Args[2])
	default:
		log.Fatal("Unknown command:", command)
	}
}

// checkPaymentStats fetches and displays payment statistics
func checkPaymentStats() {
	fmt.Println("🔍 Checking payment statistics...")

	resp, err := http.Get(RelayHTTPURL)
	if err != nil {
		log.Fatal("Failed to connect to relay:", err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatal("Failed to read response:", err)
	}

	//	fmt.Printf("Raw response: %s\n", string(body))
	fmt.Printf("Response status: %d\n", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		log.Fatal("HTTP error:", resp.StatusCode, string(body))
	}

	var relayInfo RelayInfo
	if err := json.Unmarshal(body, &relayInfo); err != nil {
		log.Fatal("Failed to parse response:", err)
	}

	fmt.Printf("\n📊 Relay Statistics:\n")
	fmt.Printf("  Relay Name: %s\n", relayInfo.Relay)
	fmt.Printf("  Provider: %s\n", relayInfo.PaymentStats.Provider)
	fmt.Printf("  Lightning Address: %s\n", relayInfo.PaymentStats.LightningAddress)
	fmt.Printf("  Payment Amount: %d sats (%d msat)\n",
		relayInfo.PaymentStats.PaymentAmountSats,
		relayInfo.PaymentStats.PaymentAmountMsat)
	fmt.Printf("  Access Duration: %s\n", relayInfo.PaymentStats.AccessDuration)
	fmt.Printf("\n💰 Payment Statistics:\n")
	fmt.Printf("  Payment Requests: %d\n", relayInfo.PaymentStats.PaymentRequests)
	fmt.Printf("  Successful Payments: %d\n", relayInfo.PaymentStats.SuccessfulPayments)
	fmt.Printf("  Total Members: %d\n", relayInfo.PaymentStats.TotalMembers)
	fmt.Printf("  Active Members: %d\n", relayInfo.PaymentStats.ActiveMembers)
	fmt.Printf("  Expired Members: %d\n", relayInfo.PaymentStats.ExpiredMembers)

	// Also check debug endpoint
	fmt.Println("\n🐛 Debug Information:")
	debugResp, err := http.Get(RelayHTTPURL + "/debug/payments")
	if err != nil {
		fmt.Printf("  Failed to get debug info: %v\n", err)
		return
	}
	defer debugResp.Body.Close()

	debugBody, err := ioutil.ReadAll(debugResp.Body)
	if err != nil {
		fmt.Printf("  Failed to read debug response: %v\n", err)
		return
	}

	fmt.Printf("  %s\n", string(debugBody))
}

// testPaymentFlow tests the complete payment workflow
func testPaymentFlow() {
	fmt.Println("🧪 Testing payment flow...")

	// Generate a test keypair
	sk := generatePrivateKey()
	pk, _ := nostr.GetPublicKey(sk)

	npub, _ := nip19.EncodePublicKey(pk)
	fmt.Printf("  Generated test keypair: %s\n", npub)

	// Connect to relay
	relay, err := nostr.RelayConnect(context.Background(), RelayWSURL)
	if err != nil {
		log.Fatal("Failed to connect to relay:", err)
	}
	defer relay.Close()

	fmt.Printf("  Connected to relay: %s\n", RelayWSURL)

	// Create a test event
	event := &nostr.Event{
		PubKey:    pk,
		CreatedAt: nostr.Now(),
		Kind:      1,
		Tags:      []nostr.Tag{},
		Content:   "This is a test event to trigger payment flow",
	}

	// Sign the event
	event.Sign(sk)

	fmt.Printf("  Created test event: %s\n", event.ID)

	// Try to publish the event (should trigger payment request)
	fmt.Println("  Publishing event (expecting payment request)...")

	err = relay.Publish(context.Background(), *event)
	if err != nil {
		fmt.Printf("  ❌ Event rejected: %v\n", err)
		// Check if the error contains payment information
		if strings.Contains(err.Error(), "invoice") || strings.Contains(err.Error(), "payment") {
			fmt.Println("  💳 Payment required - check error message for invoice details")
		}
	} else {
		fmt.Println("  ✅ Event published successfully (user might be in WoT)")
	}
}

// connectAndTest connects to the relay and sends a test event
func connectAndTest() {
	fmt.Println("🔗 Connecting to relay and testing...")

	// Generate a test keypair
	sk := generatePrivateKey()
	pk, _ := nostr.GetPublicKey(sk)

	npub, _ := nip19.EncodePublicKey(pk)
	fmt.Printf("  Test pubkey: %s\n", npub)

	// Connect to relay
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	relay, err := nostr.RelayConnect(ctx, RelayWSURL)
	if err != nil {
		log.Fatal("Failed to connect to relay:", err)
	}
	defer relay.Close()

	fmt.Printf("  ✅ Connected to relay: %s\n", RelayWSURL)

	// Subscribe to events to see what happens
	filters := []nostr.Filter{{
		Authors: []string{pk},
		Limit:   10,
	}}

	sub, err := relay.Subscribe(ctx, filters)
	if err != nil {
		log.Fatal("Failed to subscribe:", err)
	}

	fmt.Println("  📡 Subscribed to events...")

	// Create and publish a test event
	event := &nostr.Event{
		PubKey:    pk,
		CreatedAt: nostr.Now(),
		Kind:      1,
		Tags:      []nostr.Tag{},
		Content:   fmt.Sprintf("Test message from client at %s", time.Now().Format(time.RFC3339)),
	}

	event.Sign(sk)
	fmt.Printf("  📝 Created event: %s\n", event.ID)

	// Publish the event
	err = relay.Publish(ctx, *event)
	if err != nil {
		fmt.Printf("  ❌ Event rejected: %v\n", err)
		// Check if the error contains payment information
		if strings.Contains(err.Error(), "invoice") || strings.Contains(err.Error(), "payment") {
			fmt.Println("  💳 Payment required - check error message for invoice details")
		}
	} else {
		fmt.Println("  ✅ Event published successfully!")
	}

	// Listen for any events for a bit
	fmt.Println("  👂 Listening for events...")
	timeout := time.After(3 * time.Second)

	for {
		select {
		case event := <-sub.Events:
			fmt.Printf("  📨 Received event: %s - %s\n", event.ID, event.Content)
		case <-timeout:
			fmt.Println("  ⏰ Done listening")
			return
		case <-sub.EndOfStoredEvents:
			fmt.Println("  📚 End of stored events")
		}
	}
}

// verifyPayment manually verifies a payment
func verifyPayment(paymentHash string) {
	fmt.Printf("🔍 Verifying payment: %s\n", paymentHash)

	// Generate a test pubkey for verification
	sk := generatePrivateKey()
	pk, _ := nostr.GetPublicKey(sk)

	requestBody := map[string]string{
		"payment_hash": paymentHash,
		"pubkey":       pk,
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		log.Fatal("Failed to marshal request:", err)
	}

	resp, err := http.Post(RelayHTTPURL+"/verify-payment", "application/json", strings.NewReader(string(jsonBody)))
	if err != nil {
		log.Fatal("Failed to verify payment:", err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatal("Failed to read response:", err)
	}

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("  ❌ Verification failed: %s\n", string(body))
		return
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		log.Fatal("Failed to parse response:", err)
	}

	fmt.Printf("  Payment Status: %v\n", result["paid"])
	fmt.Printf("  Payment Hash: %v\n", result["payment_hash"])
	fmt.Printf("  Amount: %v\n", result["amount"])

	if accessGranted, ok := result["access_granted"]; ok && accessGranted.(bool) {
		fmt.Printf("  ✅ Access granted!\n")
	}
}

// generatePrivateKey generates a random private key for testing
func generatePrivateKey() string {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		log.Fatal("Failed to generate private key:", err)
	}
	return hex.EncodeToString(b)
}
