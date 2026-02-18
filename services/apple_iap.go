package services

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	ErrInvalidTransaction   = errors.New("invalid signed transaction")
	ErrTransactionExpired   = errors.New("transaction expired")
	ErrProductMismatch      = errors.New("product id mismatch")
	ErrInvalidCertChain     = errors.New("invalid certificate chain")
	ErrUntrustedCertificate = errors.New("untrusted certificate")
)

// Apple Root CA - G3 certificate (downloaded from https://www.apple.com/certificateauthority/)
// This is the root certificate used to sign StoreKit 2 transactions
const appleRootCAG3PEM = `-----BEGIN CERTIFICATE-----
MIICQzCCAcmgAwIBAgIILcX8iNLFS5UwCgYIKoZIzj0EAwMwZzEbMBkGA1UEAwwS
QXBwbGUgUm9vdCBDQSAtIEczMSYwJAYDVQQLDB1BcHBsZSBDZXJ0aWZpY2F0aW9u
IEF1dGhvcml0eTETMBEGA1UECgwKQXBwbGUgSW5jLjELMAkGA1UEBhMCVVMwHhcN
MTQwNDMwMTgxOTA2WhcNMzkwNDMwMTgxOTA2WjBnMRswGQYDVQQDDBJBcHBsZSBS
b290IENBIC0gRzMxJjAkBgNVBAsMHUFwcGxlIENlcnRpZmljYXRpb24gQXV0aG9y
aXR5MRMwEQYDVQQKDApBcHBsZSBJbmMuMQswCQYDVQQGEwJVUzB2MBAGByqGSM49
AgEGBSuBBAAiA2IABJjpLz1AcqTtkyJygRMc3RCV8cWjTnHcFBbZDuWmBSp3ZHtf
TjjTuxxEtX/1H7YyYl3J6YRbTzBPEVoA/VhYDKX1DyxNB0cTddqXl5dvMVztK517
IDvYuVTZXpmkOlEKMaNCMEAwHQYDVR0OBBYEFLuw3qFYM4iapIqZ3r6966/ayySr
MA8GA1UdEwEB/wQFMAMBAf8wDgYDVR0PAQH/BAQDAgEGMAoGCCqGSM49BAMDA2gA
MGUCMQCD6cHEFl4aXTQY2e3v9GwOAEZLuN+yRhHFD/3meoyhpmvOwgPUnPWTxnS4
at+qIxUCMG1mihDK1A3UT82NQz60imOlM27jbdoXt2QfyFMm+YhidDkLF1vLUagM
6BgD56KyKA==
-----END CERTIFICATE-----`

// appleRootCertPool is a certificate pool containing Apple's trusted root certificates
var appleRootCertPool *x509.CertPool

func init() {
	appleRootCertPool = x509.NewCertPool()

	// Parse and add Apple Root CA - G3
	block, _ := pem.Decode([]byte(appleRootCAG3PEM))
	if block != nil {
		cert, err := x509.ParseCertificate(block.Bytes)
		if err == nil {
			appleRootCertPool.AddCert(cert)
		}
	}
}

// AppleIAPConfig holds Apple IAP configuration
type AppleIAPConfig struct {
	BundleID string           // Your app's bundle ID for validation
	Products map[string]int64 // product_id -> amount mapping
}

// AppleIAPService handles StoreKit 2 JWS verification
type AppleIAPService struct {
	config *AppleIAPConfig
}

// NewAppleIAPService creates a new Apple IAP service
func NewAppleIAPService(cfg *AppleIAPConfig) *AppleIAPService {
	return &AppleIAPService{
		config: cfg,
	}
}

// JWSTransactionPayload represents the decoded transaction payload from StoreKit 2
type JWSTransactionPayload struct {
	TransactionID         string `json:"transactionId"`
	OriginalTransactionID string `json:"originalTransactionId"`
	BundleID              string `json:"bundleId"`
	ProductID             string `json:"productId"`
	PurchaseDate          int64  `json:"purchaseDate"`
	OriginalPurchaseDate  int64  `json:"originalPurchaseDate"`
	Quantity              int    `json:"quantity"`
	Type                  string `json:"type"`
	InAppOwnershipType    string `json:"inAppOwnershipType"`
	SignedDate            int64  `json:"signedDate"`
	Environment           string `json:"environment"`
	TransactionReason     string `json:"transactionReason,omitempty"`
	Storefront            string `json:"storefront,omitempty"`
	StorefrontID          string `json:"storefrontId,omitempty"`
	Price                 int64  `json:"price,omitempty"`
	Currency              string `json:"currency,omitempty"`
	ExpiresDate           int64  `json:"expiresDate,omitempty"`
	RevocationDate        int64  `json:"revocationDate,omitempty"`
	RevocationReason      int    `json:"revocationReason,omitempty"`
	IsUpgraded            bool   `json:"isUpgraded,omitempty"`
	OfferType             int    `json:"offerType,omitempty"`
	OfferIdentifier       string `json:"offerIdentifier,omitempty"`
	AppAccountToken       string `json:"appAccountToken,omitempty"`
}

// VerifyResult contains the verification result with full transaction details
type VerifyResult struct {
	TransactionID         string
	OriginalTransactionID string
	ProductID             string
	Amount                int64
	Environment           string
	BundleID              string
	AppAccountToken       string
	PurchaseDate          int64
	SignedDate            int64
	TransactionType       string
	Quantity              int
	Storefront            string
	StorefrontID          string
	Price                 int64
	Currency              string
}

// VerifySignedTransaction verifies a StoreKit 2 signed transaction (JWS format)
func (s *AppleIAPService) VerifySignedTransaction(signedTransaction string, expectedProductID string) (*VerifyResult, error) {
	// Parse JWS without verification first to get the header
	parts := strings.Split(signedTransaction, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("%w: invalid JWS format", ErrInvalidTransaction)
	}

	// Decode header to get x5c certificate chain
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("%w: failed to decode header", ErrInvalidTransaction)
	}

	var header struct {
		Alg string   `json:"alg"`
		X5C []string `json:"x5c"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, fmt.Errorf("%w: failed to parse header", ErrInvalidTransaction)
	}

	if len(header.X5C) == 0 {
		return nil, fmt.Errorf("%w: missing x5c certificate chain", ErrInvalidCertChain)
	}

	// Parse certificates from x5c
	certs := make([]*x509.Certificate, len(header.X5C))
	for i, certB64 := range header.X5C {
		certDER, err := base64.StdEncoding.DecodeString(certB64)
		if err != nil {
			return nil, fmt.Errorf("%w: failed to decode certificate %d", ErrInvalidCertChain, i)
		}
		cert, err := x509.ParseCertificate(certDER)
		if err != nil {
			return nil, fmt.Errorf("%w: failed to parse certificate %d", ErrInvalidCertChain, i)
		}
		certs[i] = cert
	}

	// Verify the certificate chain against Apple's root certificate
	if err := s.verifyCertificateChain(certs); err != nil {
		return nil, err
	}

	// Get the public key from the leaf certificate (first in the chain)
	leafCert := certs[0]
	publicKey, ok := leafCert.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("%w: expected ECDSA public key", ErrInvalidCertChain)
	}

	// Parse and verify the JWT with the extracted public key
	token, err := jwt.Parse(signedTransaction, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodECDSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return publicKey, nil
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidTransaction, err)
	}

	if !token.Valid {
		return nil, ErrInvalidTransaction
	}

	// Extract claims
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("%w: failed to parse claims", ErrInvalidTransaction)
	}

	// Convert claims to payload struct
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to marshal claims", ErrInvalidTransaction)
	}

	var payload JWSTransactionPayload
	if err := json.Unmarshal(claimsJSON, &payload); err != nil {
		return nil, fmt.Errorf("%w: failed to parse payload", ErrInvalidTransaction)
	}

	// Validate bundle ID if configured
	if s.config.BundleID != "" && payload.BundleID != s.config.BundleID {
		return nil, fmt.Errorf("%w: bundle ID mismatch", ErrInvalidTransaction)
	}

	// Validate product ID
	if payload.ProductID != expectedProductID {
		return nil, ErrProductMismatch
	}

	// Check if transaction has been revoked
	if payload.RevocationDate > 0 {
		return nil, fmt.Errorf("%w: transaction has been revoked", ErrInvalidTransaction)
	}

	// Reject sandbox transactions in release mode
	if os.Getenv("GIN_MODE") == "release" && payload.Environment == "Sandbox" {
		return nil, fmt.Errorf("%w: sandbox transactions not allowed in production", ErrInvalidTransaction)
	}

	// Reject transactions older than 7 days
	signedTime := time.UnixMilli(payload.SignedDate)
	if time.Since(signedTime) > 7*24*time.Hour {
		return nil, fmt.Errorf("%w: transaction signed more than 7 days ago", ErrTransactionExpired)
	}

	// Get amount from products config
	amount, ok := s.config.Products[expectedProductID]
	if !ok {
		return nil, fmt.Errorf("%w: unknown product %s", ErrProductMismatch, expectedProductID)
	}

	return &VerifyResult{
		TransactionID:         payload.TransactionID,
		OriginalTransactionID: payload.OriginalTransactionID,
		ProductID:             payload.ProductID,
		Amount:                amount,
		Environment:           payload.Environment,
		BundleID:              payload.BundleID,
		AppAccountToken:       payload.AppAccountToken,
		PurchaseDate:          payload.PurchaseDate,
		SignedDate:            payload.SignedDate,
		TransactionType:       payload.Type,
		Quantity:              payload.Quantity,
		Storefront:            payload.Storefront,
		StorefrontID:          payload.StorefrontID,
		Price:                 payload.Price,
		Currency:              payload.Currency,
	}, nil
}

// verifyCertificateChain verifies the x5c certificate chain against Apple's root CA
func (s *AppleIAPService) verifyCertificateChain(certs []*x509.Certificate) error {
	if len(certs) < 2 {
		return fmt.Errorf("%w: need at least leaf and intermediate certificates", ErrInvalidCertChain)
	}

	// Build intermediate certificate pool
	intermediates := x509.NewCertPool()
	for i := 1; i < len(certs); i++ {
		intermediates.AddCert(certs[i])
	}

	// Verify the leaf certificate chains to Apple's trusted root
	leafCert := certs[0]
	opts := x509.VerifyOptions{
		Roots:         appleRootCertPool,
		Intermediates: intermediates,
		CurrentTime:   time.Now(),
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}

	_, err := leafCert.Verify(opts)
	if err != nil {
		return fmt.Errorf("%w: certificate chain verification failed: %v", ErrUntrustedCertificate, err)
	}

	return nil
}
