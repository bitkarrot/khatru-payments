//go:build client

package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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

	body, err := io.ReadAll(resp.Body)
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
		fmt.Printf("  🔍 Full error details: %+v\n", err)
		
		// Check if the error contains payment information
		if strings.Contains(err.Error(), "invoice") || strings.Contains(err.Error(), "payment") {
			fmt.Println("  💳 Payment required - check error message for invoice details")
			// Try to extract and display any JSON content from the error
			errorStr := err.Error()
			if strings.Contains(errorStr, "{") {
				start := strings.Index(errorStr, "{")
				if start != -1 {
					jsonPart := errorStr[start:]
					if end := strings.LastIndex(jsonPart, "}"); end != -1 {
						jsonPart = jsonPart[:end+1]
						fmt.Printf("  📄 JSON content: %s\n", jsonPart)
					}
				}
			}
		}
	} else {
		fmt.Println("  ✅ Event published successfully (user might be in WoT)")
	}
}

// Global test keypair for consistent testing
var testSK string
var testPK string

// loadOrCreateKeypair loads keypair from file or creates new one
func loadOrCreateKeypair() {
	const keypairFile = "test-keypair.txt"

	// Try to load existing keypair
	if data, err := ioutil.ReadFile(keypairFile); err == nil {
		lines := strings.Split(string(data), "\n")
		if len(lines) >= 2 {
			testSK = strings.TrimSpace(lines[0])
			testPK = strings.TrimSpace(lines[1])
			fmt.Printf("  📁 Loaded existing keypair from %s\n", keypairFile)
			return
		}
	}

	// Generate new keypair if file doesn't exist or is invalid
	testSK = generatePrivateKey()
	testPK, _ = nostr.GetPublicKey(testSK)

	// Save to file
	keypairData := fmt.Sprintf("%s\n%s\n", testSK, testPK)
	if err := ioutil.WriteFile(keypairFile, []byte(keypairData), 0600); err != nil {
		log.Printf("⚠️ Failed to save keypair to file: %v", err)
	} else {
		fmt.Printf("  💾 Saved new keypair to %s\n", keypairFile)
	}
}

// connectAndTest connects to the relay and sends a test event
func connectAndTest() {
	fmt.Println("🔗 Connecting to relay and testing...")

	// Load or create persistent keypair
	loadOrCreateKeypair()

	npub, _ := nip19.EncodePublicKey(testPK)
	fmt.Printf("  Test pubkey: %s\n", npub)

	// Connect to relay with longer timeout for payment operations
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	relay, err := nostr.RelayConnect(ctx, RelayWSURL)
	if err != nil {
		log.Fatal("Failed to connect to relay:", err)
	}
	defer relay.Close()

	fmt.Printf("  ✅ Connected to relay: %s\n", RelayWSURL)

	// Subscribe to events to see what happens
	filters := []nostr.Filter{{
		Authors: []string{testPK},
		Limit:   10,
	}}

	sub, err := relay.Subscribe(ctx, filters)
	if err != nil {
		log.Fatal("Failed to subscribe:", err)
	}

	fmt.Println("  📡 Subscribed to events...")

	// Create and publish a test event
	event := &nostr.Event{
		PubKey:    testPK,
		CreatedAt: nostr.Now(),
		Kind:      1,
		Tags:      []nostr.Tag{},
		Content:   fmt.Sprintf("Test message from client at %s", time.Now().Format(time.RFC3339)),
	}

	event.Sign(testSK)
	fmt.Printf("  📝 Created event: %s\n", event.ID)

	// Publish the event
	err = relay.Publish(ctx, *event)
	if err != nil {
		fmt.Printf("  ❌ Event rejected: %v\n", err)
		// Check if the error contains payment information
		if strings.Contains(err.Error(), "invoice") || strings.Contains(err.Error(), "payment") {
			fmt.Println("  💳 Payment required - check error message for invoice details")

			// Extract and display the invoice for manual payment
			if strings.Contains(err.Error(), "blocked:") {
				// Parse the payment request from the error message
				if invoice := extractInvoiceFromError(err.Error()); invoice != "" {
					fmt.Printf("  💰 Invoice to pay: %s\n", invoice)
					fmt.Printf("  💰 Amount: 21000 msat (21 sats)\n")
					fmt.Printf("  💰 Message: %s\n", strings.TrimPrefix(err.Error(), "blocked: "))

					fmt.Println("\n🔔 MANUAL PAYMENT REQUIRED")
					fmt.Println("Please pay the Lightning invoice above using your preferred Lightning wallet.")
					fmt.Print("Press ENTER after you have paid the invoice to continue testing...")

					// Wait for user input
					reader := bufio.NewReader(os.Stdin)
					reader.ReadLine()

					// Try publishing again after payment
					fmt.Println("\n🔄 Retrying event publication after payment...")
					err = relay.Publish(ctx, *event)
					if err != nil {
						fmt.Printf("  ❌ Event still rejected: %v\n", err)
					} else {
						fmt.Println("  ✅ Event published successfully after payment!")
					}
					return
				}
			}
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

	// Use the same pubkey that was used for creating the invoice
	if testSK == "" {
		log.Fatal("No test keypair available. Run 'connect' command first.")
	}

	requestBody := map[string]string{
		"payment_hash": paymentHash,
		"pubkey":       testPK,
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

// extractInvoiceFromError extracts Lightning invoice from error message
func extractInvoiceFromError(errorMsg string) string {
	// First try to parse as JSON if it contains invoice field
	if strings.Contains(errorMsg, `"invoice":`) {
		// Find the JSON part after "blocked: "
		jsonStart := strings.Index(errorMsg, "{")
		if jsonStart != -1 {
			jsonStr := errorMsg[jsonStart:]
			var paymentReq PaymentRequest
			if err := json.Unmarshal([]byte(jsonStr), &paymentReq); err == nil {
				return paymentReq.Invoice
			}
		}
	}

	// Fallback: Look for Lightning invoice pattern (starts with ln)
	parts := strings.Fields(errorMsg)
	for _, part := range parts {
		if strings.HasPrefix(part, "ln") && len(part) > 50 {
			return part
		}
	}
	return ""
}

// waitForManualPaymentAndRetry waits for user to manually pay the invoice and then retries publishing
func waitForManualPaymentAndRetry(relay *nostr.Relay, event *nostr.Event, pk, sk string) {
	fmt.Println("\n🔔 MANUAL PAYMENT REQUIRED")
	fmt.Println("Please pay the Lightning invoice above using your preferred Lightning wallet.")
	fmt.Print("Press ENTER after you have paid the invoice to continue testing...")

	// Wait for user input
	reader := bufio.NewReader(os.Stdin)
	reader.ReadLine()

	fmt.Println("\n🔄 Retrying event publication after payment...")

	// Create a new context for the retry with longer timeout for payment verification
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	// Create a new test event to verify write access
	retryEvent := &nostr.Event{
		PubKey:    pk,
		CreatedAt: nostr.Now(),
		Kind:      1,
		Tags:      []nostr.Tag{},
		Content:   fmt.Sprintf("Post-payment test message at %s", time.Now().Format(time.RFC3339)),
	}

	retryEvent.Sign(sk)
	fmt.Printf("  📝 Created retry event: %s\n", retryEvent.ID)

	// Try to publish again
	err := relay.Publish(ctx, *retryEvent)
	if err != nil {
		fmt.Printf("  ❌ Event still rejected after payment: %v\n", err)
		fmt.Println("  💡 The payment may not have been processed yet, or there might be an issue.")
		fmt.Println("  💡 Try waiting a few seconds and running the test again.")
	} else {
		fmt.Println("  ✅ Event published successfully after payment!")
		fmt.Println("  🎉 Payment verification complete - you now have write access to the relay!")

		// Listen for the published event
		fmt.Println("  👂 Listening for your published event...")

		filters := []nostr.Filter{{
			Authors: []string{pk},
			Limit:   1,
			Since:   &retryEvent.CreatedAt,
		}}

		sub, err := relay.Subscribe(ctx, filters)
		if err != nil {
			fmt.Printf("  ⚠️  Failed to subscribe: %v\n", err)
			return
		}

		timeout := time.After(5 * time.Second)
		for {
			select {
			case receivedEvent := <-sub.Events:
				if receivedEvent.ID == retryEvent.ID {
					fmt.Printf("  📨 ✅ Confirmed: Received your event back from relay: %s\n", receivedEvent.Content)
					return
				}
			case <-timeout:
				fmt.Println("  ⏰ Done listening")
				return
			case <-sub.EndOfStoredEvents:
				// Continue listening for new events
			}
		}
	}
}
