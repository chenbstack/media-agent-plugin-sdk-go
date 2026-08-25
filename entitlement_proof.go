package pluginsdk

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// SignedEntitlementProof is a Cloud-signed commercial entitlement snapshot.
// Payload and Signature are raw Ed25519 material encoded by the transport;
// Pack plugins verify them with their embedded Cloud public key.
type SignedEntitlementProof struct {
	KeyID     string
	Payload   []byte
	Signature []byte
}

// EntitlementProofSource lets a trusted Pack plugin obtain the latest opaque
// Cloud proof without receiving the instance secret or token. The plugin owns
// signature verification and product/entitlement field checks.
type EntitlementProofSource interface {
	CurrentEntitlementProof(context.Context) (SignedEntitlementProof, error)
}

var ErrInvalidEntitlementProof = errors.New("invalid entitlement proof")

// VerifyEntitlementProof verifies a Cloud proof for one stable entitlement.
// The caller supplies its private-Pack public key; the host only transports
// opaque bytes and cannot manufacture a valid proof.
func VerifyEntitlementProof(ctx context.Context, source EntitlementProofSource, publicKey, entitlement string) error {
	if source == nil || strings.TrimSpace(publicKey) == "" {
		return ErrInvalidEntitlementProof
	}
	proof, err := source.CurrentEntitlementProof(ctx)
	if err != nil { return fmt.Errorf("read entitlement proof: %w", err) }
	keyText := strings.TrimPrefix(strings.TrimSpace(publicKey), "ed25519:")
	key, err := base64.RawStdEncoding.DecodeString(keyText)
	if err != nil || len(key) != ed25519.PublicKeySize || !ed25519.Verify(ed25519.PublicKey(key), proof.Payload, proof.Signature) {
		return ErrInvalidEntitlementProof
	}
	var payload struct {
		Version int `json:"version"`
		LeaseExpiresAt string `json:"lease_expires_at"`
		Entitlements []struct { Key string `json:"key"`; Enabled bool `json:"enabled"` } `json:"entitlements"`
	}
	if err := json.Unmarshal(proof.Payload, &payload); err != nil || payload.Version != 1 { return ErrInvalidEntitlementProof }
	lease, err := time.Parse(time.RFC3339, payload.LeaseExpiresAt)
	if err != nil || !time.Now().UTC().Before(lease) { return ErrInvalidEntitlementProof }
	for _, item := range payload.Entitlements { if item.Key == entitlement && item.Enabled { return nil } }
	return ErrInvalidEntitlementProof
}
