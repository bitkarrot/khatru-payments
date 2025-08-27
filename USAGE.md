# Khatru Payments Usage Guide

## Quick Integration

### 1. Add to your relay project

```bash
go get github.com/bitkarrot/khatru-payments
```

### 2. Basic Integration

```go
package main

import (
    "context"
    "log"
    "net/http"
    
    "github.com/bitkarrot/khatru-payments"
    "github.com/fiatjaf/khatru"
    "github.com/nbd-wtf/go-nostr"
)

func main() {
    // Initialize payment system
    paymentSystem, err := payments.NewFromEnv()
    if err != nil {
        log.Fatal("Failed to initialize payment system:", err)
    }

    // Create relay
    relay := khatru.NewRelay()
    
    // Add payment-based rejection for non-WoT users
    relay.RejectEvent = append(relay.RejectEvent, func(ctx context.Context, event *nostr.Event) (bool, string) {
        // Check your Web of Trust logic first
        if isInWebOfTrust(event.PubKey) {
            return false, "" // Allow WoT users
        }
        
        // For non-WoT users, check payment or create invoice
        return paymentSystem.RejectEventHandler(ctx, event)
    })

    // Register payment endpoints
    mux := relay.Router()
    paymentSystem.RegisterHandlers(mux)
    
    log.Println("Relay with payments running on :3334")
    http.ListenAndServe(":3334", relay)
}

func isInWebOfTrust(pubkey string) bool {
    // Your WoT logic here
    return false
}
```

### 3. Environment Configuration

Create a `.env` file:

```bash
# Payment Provider
PAYMENT_PROVIDER=zbd  # or "phoenixd"

# ZBD Configuration
ZBD_API_KEY=your-zbd-api-key
LIGHTNING_ADDRESS=fund@honey.hivetalk.org

# Payment Settings
PAYMENT_AMOUNT_MSAT=21000  # 21 sats
ACCESS_DURATION=1month     # 1week, 1month, 1year, forever

# Storage
PAID_ACCESS_FILE=./data/paid_access.json
CHARGE_MAPPING_FILE=./data/charge_mappings.json
```

## Advanced Configuration

### Manual Configuration

```go
config := payments.Config{
    Provider:         "zbd",
    PaymentAmount:    21000, // millisatoshis
    AccessDuration:   "1month",
    LightningAddress: "fund@honey.hivetalk.org",
    ZBDAPIKey:        "your-api-key",
    PaidAccessFile:   "./data/paid_access.json",
    RejectMessage:    "Payment required for relay access",
}

paymentSystem, err := payments.New(config)
```

### phoenixd Configuration

```bash
PAYMENT_PROVIDER=phoenixd
PHOENIXD_URL=http://localhost:9740
PHOENIXD_PASSWORD=your-phoenixd-password
```

## API Endpoints

The library automatically registers these endpoints:

### POST /verify-payment
Manual payment verification
```json
{
    "payment_hash": "abc123...",
    "pubkey": "npub1..."
}
```

### POST /webhook/zbd
ZBD webhook handler (automatic)

### GET /debug/payments
Payment statistics and configuration

## Payment Flow

1. **Non-WoT user** submits event
2. **Relay rejects** with payment invoice in JSON format:
   ```json
   {
       "message": "Payment required for relay access",
       "invoice": "lnbc210n1...",
       "amount": 21000
   }
   ```
3. **User pays** the Lightning invoice
4. **Payment verified** via webhook or manual verification
5. **Access granted** for configured duration
6. **Future events** from that pubkey are accepted

## Monitoring

### Get Payment Stats
```go
stats := paymentSystem.GetStats()
// Returns: payment_requests, successful_payments, active_members, etc.
```

### Check User Access
```go
hasAccess := paymentSystem.HasAccess(pubkey)
```

## Storage

The library uses JSON files for persistence:

- `paid_access.json` - **Active paid users and expiration times** (persistent, required for production)
- `charge_mappings.json` - Payment hash to provider ID mappings (optional, can be in-memory)

Files are automatically created and managed by the library. The paid access storage is essential for production use and handles thousands of pubkeys efficiently. Payment hash mappings are handled in-memory by providers during the payment verification process.

## Error Handling

```go
paymentSystem, err := payments.NewFromEnv()
if err != nil {
    log.Printf("Payment system unavailable: %v", err)
    // Relay continues without payments
}
```

The library gracefully handles missing configuration and provider errors.

## Testing

Use the included sample data files for testing:

```bash
cp data/paid_access.json.example data/paid_access.json
cp data/charge_mappings.json.example data/charge_mappings.json
```

## Migration from Embedded Implementation

If you have an existing payment implementation in your relay:

1. Remove payment-related files (`payment.go`, `zbd_provider.go`, etc.)
2. Add the library dependency
3. Replace payment logic with library calls
4. Update environment variables if needed
5. Test with existing data files

The library is designed to be a drop-in replacement for the original bitkarrot implementation.
