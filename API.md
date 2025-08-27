# Khatru-Payments Go Module API Documentation

The `khatru-payments` module provides Lightning Network payment integration for Nostr relays using the [khatru](https://github.com/fiatjaf/khatru) framework. It supports multiple payment providers and persistent access control.

## Installation

```go
go get github.com/bitkarrot/khatru-payments
```

## Quick Start

```go
package main

import (
    "log"
    payments "github.com/bitkarrot/khatru-payments"
    "github.com/joho/godotenv"
)

func main() {
    // Load environment variables
    godotenv.Load()
    
    // Initialize payment system from environment
    paymentSystem, err := payments.NewFromEnv()
    if err != nil {
        log.Fatal("Failed to initialize payment system:", err)
    }
    
    // Use with khatru relay
    relay := khatru.NewRelay()
    relay.RejectEvent = paymentSystem.RejectEventHandler
}
```

## Core Types

### System

The main payment system that coordinates providers, storage, and access control.

```go
type System struct {
    // Private fields
}
```

### PaymentProvider Interface

Interface that payment providers must implement:

```go
type PaymentProvider interface {
    CreateInvoice(ctx context.Context, amount int64, description string, pubkey string) (*Invoice, error)
    VerifyPayment(ctx context.Context, paymentHash string) (*PaymentVerification, error)
    GetProviderName() string
}
```

### Invoice

Represents a Lightning Network payment invoice:

```go
type Invoice struct {
    PaymentRequest string    `json:"payment_request"` // BOLT11 invoice
    PaymentHash    string    `json:"payment_hash"`    // Payment hash
    Amount         int64     `json:"amount"`          // Amount in millisatoshis
    Description    string    `json:"description"`     // Invoice description
    ExpiresAt      time.Time `json:"expires_at"`      // Expiration time
}
```

### PaymentVerification

Result of payment verification:

```go
type PaymentVerification struct {
    Paid        bool      `json:"paid"`         // Whether payment was completed
    PaymentHash string    `json:"payment_hash"` // Payment hash
    Amount      int64     `json:"amount"`       // Amount paid in millisatoshis
    PaidAt      time.Time `json:"paid_at"`      // When payment was completed
}
```

### Config

Configuration for the payment system:

```go
type Config struct {
    Provider          string `json:"provider"`            // "zbd" or "phoenixd"
    PaymentAmount     int64  `json:"payment_amount"`      // Amount in millisatoshis
    AccessDuration    string `json:"access_duration"`     // "1week", "1month", "1year", "forever"
    LightningAddress  string `json:"lightning_address"`   // For ZBD provider
    ZBDAPIKey         string `json:"zbd_api_key"`         // ZBD API key
    PhoenixdURL       string `json:"phoenixd_url"`        // Phoenixd server URL
    PhoenixdPassword  string `json:"phoenixd_password"`   // Phoenixd password
    PaidAccessFile    string `json:"paid_access_file"`    // Storage file path
    ChargeMappingFile string `json:"charge_mapping_file"` // Charge mapping file
    RejectMessage     string `json:"reject_message"`      // Custom rejection message
}
```

## Main Functions

### New(config Config) (*System, error)

Creates a new payment system with the provided configuration.

```go
config := payments.Config{
    Provider:         "zbd",
    PaymentAmount:    21000, // 21 sats in millisatoshis
    AccessDuration:   "1month",
    LightningAddress: "user@zbd.gg",
    ZBDAPIKey:        "your-api-key",
}

system, err := payments.New(config)
if err != nil {
    log.Fatal(err)
}
```

### NewFromEnv() (*System, error)

Creates a payment system using environment variables. This is the recommended approach for production deployments.

**Required Environment Variables:**

**For ZBD Provider:**
- `PAYMENT_PROVIDER=zbd`
- `ZBD_API_KEY` - Your ZBD API key
- `LIGHTNING_ADDRESS` - Your Lightning address (e.g., user@zbd.gg)

**For Phoenixd Provider:**
- `PAYMENT_PROVIDER=phoenixd`
- `PHOENIXD_PASSWORD` - Phoenixd authentication password
- `PHOENIXD_URL` - Phoenixd server URL (default: http://localhost:9740)

**Optional Environment Variables:**
- `PAYMENT_AMOUNT_MSAT` - Payment amount in millisatoshis (default: 21000)
- `ACCESS_DURATION` - Access duration: "1week", "1month", "1year", "forever" (default: "1month")
- `PAID_ACCESS_FILE` - Storage file path (default: "./data/paid_access.json")
- `CHARGE_MAPPING_FILE` - Charge mapping file (default: "./data/charge_mappings.json")
- `PAYMENT_REJECT_MESSAGE` - Custom rejection message

```go
system, err := payments.NewFromEnv()
if err != nil {
    log.Fatal(err)
}
```

## System Methods

### HasAccess(pubkey string) bool

Checks if a pubkey has valid paid access.

```go
if system.HasAccess("npub1...") {
    // User has access
}
```

### CreateInvoice(ctx context.Context, pubkey string) (*Invoice, error)

Creates a payment invoice for a specific pubkey.

```go
invoice, err := system.CreateInvoice(ctx, "npub1...")
if err != nil {
    log.Printf("Failed to create invoice: %v", err)
    return
}

fmt.Printf("Payment request: %s\n", invoice.PaymentRequest)
fmt.Printf("Amount: %d sats\n", invoice.Amount/1000)
```

### VerifyPayment(ctx context.Context, paymentHash, pubkey string) (*PaymentVerification, error)

Verifies a payment and grants access if successful.

```go
verification, err := system.VerifyPayment(ctx, paymentHash, pubkey)
if err != nil {
    log.Printf("Verification failed: %v", err)
    return
}

if verification.Paid {
    fmt.Println("Payment successful, access granted!")
}
```

### RejectEventHandler(ctx context.Context, event *nostr.Event) (bool, string)

Khatru-compatible event handler that implements payment-gated access control. Returns `(true, reason)` to reject events from unpaid users with payment instructions.

```go
relay := khatru.NewRelay()
relay.RejectEvent = system.RejectEventHandler

// The handler will:
// 1. Check if user has paid access
// 2. Allow events from paid users
// 3. Reject events from unpaid users with payment invoice
// 4. Automatically check for completed payments
```

### RegisterHandlers(mux *http.ServeMux)

Registers HTTP endpoints for payment management:

- `POST /verify-payment` - Manual payment verification
- `POST /webhook/zbd` - ZBD webhook handler
- `GET /debug/payments` - Payment statistics

```go
mux := http.NewServeMux()
system.RegisterHandlers(mux)

http.ListenAndServe(":8080", mux)
```

### GetStats() map[string]interface{}

Returns payment system statistics.

```go
stats := system.GetStats()
fmt.Printf("Payment requests: %v\n", stats["payment_requests"])
fmt.Printf("Successful payments: %v\n", stats["successful_payments"])
fmt.Printf("Active members: %v\n", stats["active_members"])
```

## HTTP Endpoints

### POST /verify-payment

Manually verify a payment and grant access.

**Request:**
```json
{
    "payment_hash": "abc123...",
    "pubkey": "npub1..."
}
```

**Response:**
```json
{
    "paid": true,
    "payment_hash": "abc123...",
    "amount": 21000,
    "access_granted": true
}
```

### POST /webhook/zbd

ZBD webhook endpoint for automatic payment processing (ZBD provider only).

### GET /debug/payments

Returns human-readable payment statistics.

## Payment Providers

### ZBD Provider

Uses the ZBD API for Lightning payments. Requires:
- ZBD API key
- Lightning address

Features:
- Webhook support for automatic payment processing
- Lightning address payments
- Persistent charge mapping

### Phoenixd Provider

Uses a local phoenixd node for Lightning payments. Requires:
- Running phoenixd instance
- Authentication password

Features:
- Self-hosted Lightning node
- Direct Lightning Network integration
- Persistent charge mapping

## Complete Example

```go
package main

import (
    "context"
    "log"
    "net/http"
    
    payments "github.com/bitkarrot/khatru-payments"
    "github.com/fiatjaf/khatru"
    "github.com/joho/godotenv"
)

func main() {
    // Load environment variables
    if err := godotenv.Load(); err != nil {
        log.Printf("Warning: .env file not found: %v", err)
    }

    // Initialize payment system
    paymentSystem, err := payments.NewFromEnv()
    if err != nil {
        log.Fatal("Failed to initialize payment system:", err)
    }

    // Setup Nostr relay with payment gating
    relay := khatru.NewRelay()
    relay.Info.Name = "Paid Relay"
    relay.Info.Description = "A payment-gated Nostr relay"
    
    // Use payment system for access control
    relay.RejectEvent = paymentSystem.RejectEventHandler

    // Setup HTTP handlers
    mux := http.NewServeMux()
    
    // Register payment endpoints
    paymentSystem.RegisterHandlers(mux)
    
    // Register relay WebSocket endpoint
    mux.HandleFunc("/", relay.ServeHTTP)

    // Start server
    log.Println("Starting server on :8080")
    log.Fatal(http.ListenAndServe(":8080", mux))
}
```

## Access Duration Options

- `"1week"` - 7 days access
- `"1month"` - 30 days access  
- `"1year"` - 365 days access
- `"forever"` - Permanent access
- Custom duration strings (e.g., `"720h"`)

## Storage

The system uses JSON files for persistent storage:

- **Paid Access Storage** (`paid_access.json`) - Tracks which pubkeys have paid access and when it expires
- **Charge Mapping Storage** (`charge_mappings.json`) - Maps payment hashes to pubkeys for verification

Both storage files are automatically created and managed by the system.

## Error Handling

All methods return standard Go errors. Common error scenarios:

- Invalid configuration (missing API keys, etc.)
- Payment provider API failures
- Storage file access issues
- Invalid payment hashes or pubkeys

Always check errors and implement appropriate fallback behavior for your relay.
