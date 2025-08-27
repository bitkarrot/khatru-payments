# Adding New Payment Providers

This guide explains how to add support for new Lightning payment providers to the khatru-payments library.

## Overview

The library uses a `PaymentProvider` interface that makes it easy to add new backends. Each provider needs to implement invoice creation and payment verification.

## PaymentProvider Interface

```go
type PaymentProvider interface {
    // CreateInvoice creates a payment invoice for the specified amount
    // pubkey parameter is used for payment tracking and verification
    CreateInvoice(ctx context.Context, amount int64, description string, pubkey string) (*Invoice, error)

    // VerifyPayment checks if a payment has been completed
    // Returns verification details including payment status and amount
    VerifyPayment(ctx context.Context, paymentHash string) (*PaymentVerification, error)

    // GetProviderName returns the name of the payment provider
    GetProviderName() string
}
```

## Step-by-Step Implementation

### 1. Create Provider File

Create a new file named after your provider (e.g., `lnbits.go`, `strike.go`, `blink.go`):

```go
package payments

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io/ioutil"
    "net/http"
    "time"
)

// YourProviderProvider implements PaymentProvider interface
type YourProviderProvider struct {
    baseURL   string
    apiKey    string
    // Map payment hash to charge ID for verification
    chargeMap map[string]string
    // Map payment hash to pubkey for verification
    pubkeyMap map[string]string
    mu        sync.RWMutex
}

// NewYourProviderProvider creates a new provider instance
func NewYourProviderProvider(baseURL, apiKey string) (*YourProviderProvider, error) {
    if apiKey == "" {
        return nil, fmt.Errorf("API key is required")
    }
    if baseURL == "" {
        baseURL = "https://api.yourprovider.com" // default URL
    }

    return &YourProviderProvider{
        baseURL:   baseURL,
        apiKey:    apiKey,
        chargeMap: make(map[string]string),
        pubkeyMap: make(map[string]string),
    }, nil
}

// GetProviderName returns the provider name
func (y *YourProviderProvider) GetProviderName() string {
    return "YourProvider"
}
```

### 2. Implement CreateInvoice

```go
// API structures for your provider
type YourProviderInvoiceRequest struct {
    Amount      int64  `json:"amount"`      // in millisatoshis
    Description string `json:"description"`
    // Add provider-specific fields
}

type YourProviderInvoiceResponse struct {
    PaymentRequest string `json:"payment_request"` // BOLT11 invoice
    PaymentHash    string `json:"payment_hash"`
    Amount         int64  `json:"amount"`
    ExpiresAt      string `json:"expires_at"`
    // Add provider-specific fields
}

func (y *YourProviderProvider) CreateInvoice(ctx context.Context, amount int64, description string, pubkey string) (*Invoice, error) {
    // Prepare request
    reqData := YourProviderInvoiceRequest{
        Amount:      amount,
        Description: description,
    }

    reqBody, err := json.Marshal(reqData)
    if err != nil {
        return nil, fmt.Errorf("failed to marshal request: %w", err)
    }

    // Make HTTP request
    req, err := http.NewRequestWithContext(ctx, "POST", y.baseURL+"/v1/invoices", bytes.NewBuffer(reqBody))
    if err != nil {
        return nil, fmt.Errorf("failed to create request: %w", err)
    }

    // Set headers (adjust for your provider's auth method)
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", "Bearer "+y.apiKey)

    client := &http.Client{Timeout: 30 * time.Second}
    resp, err := client.Do(req)
    if err != nil {
        return nil, fmt.Errorf("failed to make request: %w", err)
    }
    defer resp.Body.Close()

    body, err := ioutil.ReadAll(resp.Body)
    if err != nil {
        return nil, fmt.Errorf("failed to read response: %w", err)
    }

    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("API error: %d - %s", resp.StatusCode, string(body))
    }

    // Parse response
    var invoiceResp YourProviderInvoiceResponse
    if err := json.Unmarshal(body, &invoiceResp); err != nil {
        return nil, fmt.Errorf("failed to unmarshal response: %w", err)
    }

    // Convert to library format
    expiresAt, _ := time.Parse(time.RFC3339, invoiceResp.ExpiresAt)

    // Store charge ID and pubkey mapping for payment verification
    y.mu.Lock()
    y.chargeMap[invoiceResp.PaymentHash] = invoiceResp.PaymentHash // or actual charge ID from provider
    y.pubkeyMap[invoiceResp.PaymentHash] = pubkey
    y.mu.Unlock()

    return &Invoice{
        PaymentRequest: invoiceResp.PaymentRequest,
        PaymentHash:    invoiceResp.PaymentHash,
        Amount:         invoiceResp.Amount,
        Description:    description,
        ExpiresAt:      expiresAt,
    }, nil
}
```

### 3. Implement VerifyPayment

```go
type YourProviderPaymentResponse struct {
    PaymentHash string `json:"payment_hash"`
    Status      string `json:"status"`      // "paid", "pending", "expired"
    Amount      int64  `json:"amount"`
    PaidAt      string `json:"paid_at"`
}

func (y *YourProviderProvider) VerifyPayment(ctx context.Context, paymentHash string) (*PaymentVerification, error) {
    // Look up charge ID from payment hash
    y.mu.RLock()
    chargeID, exists := y.chargeMap[paymentHash]
    y.mu.RUnlock()
    
    if !exists {
        return &PaymentVerification{
            Paid:        false,
            PaymentHash: paymentHash,
            Amount:      0,
            PaidAt:      time.Time{},
        }, fmt.Errorf("charge ID not found for payment hash: %s", paymentHash)
    }

    req, err := http.NewRequestWithContext(ctx, "GET", y.baseURL+"/v1/payments/"+chargeID, nil)
    if err != nil {
        return nil, fmt.Errorf("failed to create request: %w", err)
    }

    req.Header.Set("Authorization", "Bearer "+y.apiKey)

    client := &http.Client{Timeout: 30 * time.Second}
    resp, err := client.Do(req)
    if err != nil {
        return nil, fmt.Errorf("failed to make request: %w", err)
    }
    defer resp.Body.Close()

    body, err := ioutil.ReadAll(resp.Body)
    if err != nil {
        return nil, fmt.Errorf("failed to read response: %w", err)
    }

    if resp.StatusCode == http.StatusNotFound {
        return &PaymentVerification{
            Paid:        false,
            PaymentHash: paymentHash,
            Amount:      0,
            PaidAt:      time.Time{},
        }, nil
    }

    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("API error: %d - %s", resp.StatusCode, string(body))
    }

    var paymentResp YourProviderPaymentResponse
    if err := json.Unmarshal(body, &paymentResp); err != nil {
        return nil, fmt.Errorf("failed to unmarshal response: %w", err)
    }

    // Convert to library format
    paid := paymentResp.Status == "paid" // Adjust based on provider's status values
    paidAt, _ := time.Parse(time.RFC3339, paymentResp.PaidAt)

    return &PaymentVerification{
        Paid:        paid,
        PaymentHash: paymentHash,
        Amount:      paymentResp.Amount,
        PaidAt:      paidAt,
    }, nil
}
```

### 4. Add to Configuration System

Update `payments.go` to include your provider:

```go
// Add to Config struct
type Config struct {
    // ... existing fields ...
    YourProviderURL    string `json:"yourprovider_url"`
    YourProviderAPIKey string `json:"yourprovider_api_key"`
}

// Add to provider initialization in New()
switch config.Provider {
case "zbd":
    provider, err = NewZBDProvider(config.ZBDAPIKey, config.LightningAddress)
case "phoenixd":
    provider, err = NewPhoenixdProvider(config.PhoenixdURL, config.PhoenixdPassword)
case "yourprovider":  // ADD THIS
    provider, err = NewYourProviderProvider(config.YourProviderURL, config.YourProviderAPIKey)
default:
    return nil, fmt.Errorf("unsupported payment provider: %s (supported: zbd, phoenixd, yourprovider)", config.Provider)
}

// Add to NewFromEnv()
config := Config{
    // ... existing fields ...
    YourProviderURL:    os.Getenv("YOURPROVIDER_URL"),
    YourProviderAPIKey: os.Getenv("YOURPROVIDER_API_KEY"),
}
```

### 5. Add Webhook Support (Optional)

If your provider supports webhooks, add a handler method:

```go
// Add to your provider struct
func (y *YourProviderProvider) HandleWebhook(payload []byte) (*PaymentVerification, string, error) {
    var webhookData YourProviderWebhookPayload
    if err := json.Unmarshal(payload, &webhookData); err != nil {
        return nil, "", fmt.Errorf("failed to unmarshal webhook: %w", err)
    }

    // Extract pubkey from webhook data (implementation depends on how you embed it)
    pubkey := extractPubkeyFromWebhook(webhookData)
    
    if webhookData.Status != "paid" {
        return nil, "", nil // Not paid yet
    }

    verification := &PaymentVerification{
        Paid:        true,
        PaymentHash: webhookData.PaymentHash,
        Amount:      webhookData.Amount,
        PaidAt:      time.Now(),
    }

    return verification, pubkey, nil
}
```

Then add the webhook endpoint in `handlers.go`:

```go
func (s *System) RegisterHandlers(mux *http.ServeMux) {
    mux.HandleFunc("POST /verify-payment", s.verifyPaymentHandler)
    mux.HandleFunc("POST /webhook/zbd", s.zbdWebhookHandler)
    mux.HandleFunc("POST /webhook/yourprovider", s.yourProviderWebhookHandler) // ADD THIS
}

func (s *System) yourProviderWebhookHandler(w http.ResponseWriter, r *http.Request) {
    // Similar to zbdWebhookHandler but for your provider
}
```

### 6. Update Documentation

Add your provider to the README.md and example configurations:

```bash
# .env.example
PAYMENT_PROVIDER=yourprovider
YOURPROVIDER_URL=https://api.yourprovider.com
YOURPROVIDER_API_KEY=your-api-key
```

## Testing Your Provider

1. Create test files following the pattern of `test_zbd.go`
2. Test invoice creation and payment verification
3. Test webhook handling if implemented
4. Add example configuration to the documentation

## Common Patterns

### Authentication Methods
- **Bearer Token**: `req.Header.Set("Authorization", "Bearer "+apiKey)`
- **API Key Header**: `req.Header.Set("X-API-Key", apiKey)`
- **Basic Auth**: `req.SetBasicAuth(username, password)`

### Amount Handling
- Convert between satoshis and millisatoshis as needed
- Some APIs use satoshis, others use millisatoshis
- Always return amounts in millisatoshis from your provider

### Error Handling
- Check HTTP status codes
- Parse error responses from the API
- Provide meaningful error messages

### Timeouts
- Always use context with timeouts
- Set reasonable HTTP client timeouts (30 seconds is typical)

## Example Providers to Implement

Popular Lightning service providers that could be added:

- **LNBits** - Self-hosted Lightning wallet
- **Strike** - Lightning payments API
- **Blink (Bitcoin Beach Wallet)** - Lightning wallet API
- **OpenNode** - Lightning payment processor
- **LNPay** - Lightning payment service
- **Voltage** - Lightning infrastructure
- **Alby** - Lightning wallet and API

## Getting Help

- Check existing provider implementations (`zbd.go`, `phoenixd.go`) for reference
- Review the provider's API documentation
- Test with small amounts first
- Ask questions in the project issues or discussions

## Contributing

When adding a new provider:

1. Follow the existing code style
2. Add comprehensive error handling
3. Include tests
4. Update documentation
5. Submit a pull request with your implementation

The library maintainers will review and help integrate your provider into the main codebase.
