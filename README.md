# Khatru Payments

A Lightning payment library for Nostr relays built with the khatru framework. This library provides a pluggable payment system that allows relay operators to require Lightning payments for access from users not in their Web of Trust (WoT).

## Features

- **Multiple Payment Providers**: Support for ZBD and phoenixd backends
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
    "github.com/bitkarrot/khatru-payments"
    "github.com/fiatjaf/khatru"
)

// Initialize payment system
config := payments.Config{
    Provider:         "zbd",
    PaymentAmount:    21000, // 21 sats in millisatoshis
    AccessDuration:   "1month",
    LightningAddress: "fund@honey.hivetalk.org",
    ZBDAPIKey:        "your-zbd-api-key",
}

paymentSystem, err := payments.New(config)
if err != nil {
    log.Fatal(err)
}

// Integrate with khatru relay
relay := khatru.NewRelay()
relay.RejectEvent = append(relay.RejectEvent, paymentSystem.RejectEventHandler)

// Add payment endpoints
mux := relay.Router()
paymentSystem.RegisterHandlers(mux)
```

## Configuration

Set environment variables or use the Config struct:

```bash
PAYMENT_PROVIDER=zbd
ZBD_API_KEY=your-api-key
LIGHTNING_ADDRESS=fund@honey.hivetalk.org
PAYMENT_AMOUNT_MSAT=21000
ACCESS_DURATION=1month
PAID_ACCESS_FILE=./data/paid_access.json
CHARGE_MAPPING_FILE=./data/charge_mappings.json
```

## License

MIT License
