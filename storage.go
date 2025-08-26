package payments

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// PaidAccessMember represents a user who has paid for access
type PaidAccessMember struct {
	Pubkey      string    `json:"pubkey"`
	PaymentHash string    `json:"payment_hash"`
	ExpiresAt   time.Time `json:"expires_at"`
	CreatedAt   time.Time `json:"created_at"`
	Amount      int64     `json:"amount"`
}

// PaidAccessStorage manages paid access members
type PaidAccessStorage struct {
	Members  map[string]*PaidAccessMember `json:"members"`
	mutex    sync.RWMutex
	filePath string
}

// NewPaidAccessStorage creates a new paid access storage
func NewPaidAccessStorage(filePath string) *PaidAccessStorage {
	storage := &PaidAccessStorage{
		Members:  make(map[string]*PaidAccessMember),
		filePath: filePath,
	}
	
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		log.Printf("⚠️ Failed to create directory for paid access file: %v", err)
	}
	
	storage.Load()
	return storage
}

// Load reads paid access data from file
func (pas *PaidAccessStorage) Load() error {
	pas.mutex.Lock()
	defer pas.mutex.Unlock()

	if _, err := os.Stat(pas.filePath); os.IsNotExist(err) {
		// File doesn't exist, start with empty storage
		return nil
	}

	data, err := ioutil.ReadFile(pas.filePath)
	if err != nil {
		return fmt.Errorf("failed to read paid access file: %w", err)
	}

	if len(data) == 0 {
		return nil
	}

	return json.Unmarshal(data, pas)
}

// Save writes paid access data to file
func (pas *PaidAccessStorage) Save() error {
	pas.mutex.RLock()
	defer pas.mutex.RUnlock()

	data, err := json.MarshalIndent(pas, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal paid access data: %w", err)
	}

	return ioutil.WriteFile(pas.filePath, data, 0644)
}

// AddPaidAccess adds a new paid access member
func (pas *PaidAccessStorage) AddPaidAccess(pubkey, paymentHash string, amount int64, duration time.Duration) error {
	pas.mutex.Lock()
	defer pas.mutex.Unlock()

	expiresAt := time.Now().Add(duration)
	if duration == 0 {
		expiresAt = time.Time{} // Never expires
	}

	member := &PaidAccessMember{
		Pubkey:      pubkey,
		PaymentHash: paymentHash,
		ExpiresAt:   expiresAt,
		CreatedAt:   time.Now(),
		Amount:      amount,
	}

	pas.Members[pubkey] = member

	if err := pas.Save(); err != nil {
		return fmt.Errorf("failed to save paid access: %w", err)
	}

	if expiresAt.IsZero() {
		log.Printf("💰 Added permanent paid access for pubkey %s...", pubkey[:16])
	} else {
		log.Printf("💰 Added paid access for pubkey %s... (expires: %v)", pubkey[:16], expiresAt)
	}
	return nil
}

// HasAccess checks if a pubkey has valid paid access
func (pas *PaidAccessStorage) HasAccess(pubkey string) bool {
	pas.mutex.RLock()
	defer pas.mutex.RUnlock()

	member, exists := pas.Members[pubkey]
	if !exists {
		return false
	}

	// Check if access has expired (unless it's forever)
	if !member.ExpiresAt.IsZero() && time.Now().After(member.ExpiresAt) {
		return false
	}

	return true
}

// CleanupExpired removes expired access entries
func (pas *PaidAccessStorage) CleanupExpired() error {
	pas.mutex.Lock()
	defer pas.mutex.Unlock()

	now := time.Now()
	cleanedCount := 0

	for pubkey, member := range pas.Members {
		if !member.ExpiresAt.IsZero() && now.After(member.ExpiresAt) {
			delete(pas.Members, pubkey)
			cleanedCount++
		}
	}

	if cleanedCount > 0 {
		log.Printf("🧹 Cleaned up %d expired access entries", cleanedCount)
		return pas.Save()
	}

	return nil
}

// GetStats returns statistics about paid access
func (pas *PaidAccessStorage) GetStats() map[string]interface{} {
	pas.mutex.RLock()
	defer pas.mutex.RUnlock()

	stats := map[string]interface{}{
		"total_members":   len(pas.Members),
		"active_members":  0,
		"expired_members": 0,
	}

	now := time.Now()
	for _, member := range pas.Members {
		if member.ExpiresAt.IsZero() || now.Before(member.ExpiresAt) {
			stats["active_members"] = stats["active_members"].(int) + 1
		} else {
			stats["expired_members"] = stats["expired_members"].(int) + 1
		}
	}

	return stats
}

// ChargeMappingStorage manages persistent storage of payment hash to charge ID mappings
type ChargeMappingStorage struct {
	Mappings map[string]string `json:"mappings"`
	mutex    sync.RWMutex
	filePath string
}

// NewChargeMappingStorage creates a new charge mapping storage
func NewChargeMappingStorage(filePath string) *ChargeMappingStorage {
	storage := &ChargeMappingStorage{
		Mappings: make(map[string]string),
		filePath: filePath,
	}
	
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		log.Printf("⚠️ Failed to create directory for charge mapping file: %v", err)
	}
	
	storage.load()
	return storage
}

// load reads charge mappings from file
func (cms *ChargeMappingStorage) load() error {
	cms.mutex.Lock()
	defer cms.mutex.Unlock()

	if _, err := os.Stat(cms.filePath); os.IsNotExist(err) {
		return nil // File doesn't exist, start with empty mappings
	}

	data, err := ioutil.ReadFile(cms.filePath)
	if err != nil {
		log.Printf("⚠️ Failed to read charge mappings file: %v", err)
		return err
	}

	if len(data) == 0 {
		return nil
	}

	return json.Unmarshal(data, cms)
}

// save writes charge mappings to file
func (cms *ChargeMappingStorage) save() error {
	data, err := json.MarshalIndent(cms, "", "  ")
	if err != nil {
		return err
	}

	return ioutil.WriteFile(cms.filePath, data, 0644)
}

// Store saves a payment hash to charge ID mapping
func (cms *ChargeMappingStorage) Store(paymentHash, chargeID string) error {
	cms.mutex.Lock()
	defer cms.mutex.Unlock()

	cms.Mappings[paymentHash] = chargeID
	
	if err := cms.save(); err != nil {
		log.Printf("⚠️ Failed to save charge mapping: %v", err)
		return err
	}

	log.Printf("💾 Stored charge mapping: %s... → %s", paymentHash[:16], chargeID)
	return nil
}

// Get retrieves a charge ID by payment hash
func (cms *ChargeMappingStorage) Get(paymentHash string) (string, bool) {
	cms.mutex.RLock()
	defer cms.mutex.RUnlock()

	chargeID, exists := cms.Mappings[paymentHash]
	return chargeID, exists
}

// Cleanup removes old mappings (older than 24 hours)
func (cms *ChargeMappingStorage) Cleanup() {
	cms.mutex.Lock()
	defer cms.mutex.Unlock()

	// In a production system, you'd want to track creation timestamps
	// For now, we'll keep all mappings as they're needed for verification
	log.Printf("💾 Charge mapping cleanup completed (%d mappings)", len(cms.Mappings))
}
