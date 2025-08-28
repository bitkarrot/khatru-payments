# Khatru Payments

A Lightning payment library for Nostr relays built with the khatru framework. This library provides a pluggable payment system that allows relay operators to require Lightning payments for access from users not in their Web of Trust (WoT).

TODO: NOTE - Go back and test phoenixd on testnet3

## Features

- **Multiple Payment Providers**: Support for ZBD and phoenixd backends
- **Real Payment Verification**: Actual API calls to verify payment status with providers
- **Smart Payment Tracking**: Payment hash to pubkey mapping for accurate verification
- **Flexible Access Control**: Configurable payment amounts and access durations
- **Persistent Storage**: JSON-based storage for paid access and payment tracking
- **Webhook Support**: Automatic payment verification via webhooks
- **Manual Verification**: REST endpoints for manual payment verification
- **Automatic Cleanup**: Expired access cleanup with configurable intervals

## Supported Providers

- **ZBD**: Integration with ZBD's Lightning API
- **phoenixd**: Integration with phoenixd Lightning node
- **Extensible**: Easy to add new providers (LNBits, Strike, Blink.sv, etc.)

## Installation

```bash
go get github.com/bitkarrot/khatru-payments
```

## Quick Start

```go
import (
    "context"
    "log"
    "net/http"
    
    "github.com/bitkarrot/khatru-payments"
    "github.com/fiatjaf/khatru"
    "github.com/nbd-wtf/go-nostr"
)

func main() {
    // Initialize payment system from environment variables
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

## Configuration

Set environment variables or use the Config struct:

```bash
# Payment Provider
PAYMENT_PROVIDER=zbd  # or "phoenixd"

# ZBD Configuration
ZBD_API_KEY=your-zbd-api-key
LIGHTNING_ADDRESS=fund@honey.hivetalk.org

# phoenixd Configuration (alternative)
PHOENIXD_URL=http://localhost:9740
PHOENIXD_PASSWORD=your-phoenixd-password

# Payment Settings
PAYMENT_AMOUNT_MSAT=21000  # 21 sats
ACCESS_DURATION=1month     # 1week, 1month, 1year, forever

# Storage
PAID_ACCESS_FILE=./data/paid_access.json
CHARGE_MAPPING_FILE=./data/charge_mappings.json

# Optional
PAYMENT_REJECT_MESSAGE="You are not part of the WoT, payment required to join relay"
```

## License

MIT License
