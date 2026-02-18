package services

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// Apple Server Notification V2 types
const (
	NotificationTypeRefund             = "REFUND"
	NotificationTypeConsumptionRequest = "CONSUMPTION_REQUEST"
	NotificationTypeDidChangeRenewal   = "DID_CHANGE_RENEWAL_STATUS"
	NotificationTypeDidRenew           = "DID_RENEW"
	NotificationTypeExpired            = "EXPIRED"
	NotificationTypeRevoke             = "REVOKE"
)

// NotificationV2Payload represents the decoded payload from Apple Server Notification V2
type NotificationV2Payload struct {
	NotificationType string             `json:"notificationType"`
	Subtype          string             `json:"subtype,omitempty"`
	NotificationUUID string             `json:"notificationUUID"`
	Data             NotificationV2Data `json:"data"`
	Version          string             `json:"version"`
	SignedDate       int64              `json:"signedDate"`
}

// NotificationV2Data contains the transaction data within the notification
type NotificationV2Data struct {
	AppAppleID            int64  `json:"appAppleId,omitempty"`
	BundleID              string `json:"bundleId"`
	BundleVersion         string `json:"bundleVersion,omitempty"`
	Environment           string `json:"environment"`
	SignedTransactionInfo string `json:"signedTransactionInfo"`
	SignedRenewalInfo     string `json:"signedRenewalInfo,omitempty"`
}

// AppleNotificationService handles Apple Server Notification V2 verification
type AppleNotificationService struct {
	bundleID string
}

// NewAppleNotificationService creates a new notification service
func NewAppleNotificationService(bundleID string) *AppleNotificationService {
	return &AppleNotificationService{
		bundleID: bundleID,
	}
}

// VerifyAndDecodeNotification verifies the signed notification and returns the decoded payload
func (s *AppleNotificationService) VerifyAndDecodeNotification(signedPayload string) (*NotificationV2Payload, error) {
	// Parse JWS
	parts := strings.Split(signedPayload, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWS format")
	}

	// Decode header to get x5c certificate chain
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("failed to decode header: %w", err)
	}

	var header struct {
		Alg string   `json:"alg"`
		X5C []string `json:"x5c"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, fmt.Errorf("failed to parse header: %w", err)
	}

	if len(header.X5C) == 0 {
		return nil, fmt.Errorf("missing x5c certificate chain")
	}

	// Parse certificates from x5c
	certs := make([]*x509.Certificate, len(header.X5C))
	for i, certB64 := range header.X5C {
		certDER, err := base64.StdEncoding.DecodeString(certB64)
		if err != nil {
			return nil, fmt.Errorf("failed to decode certificate %d: %w", i, err)
		}
		cert, err := x509.ParseCertificate(certDER)
		if err != nil {
			return nil, fmt.Errorf("failed to parse certificate %d: %w", i, err)
		}
		certs[i] = cert
	}

	// Verify the certificate chain against Apple's root certificate
	if err := verifyCertificateChainForNotification(certs); err != nil {
		return nil, fmt.Errorf("certificate chain verification failed: %w", err)
	}

	// Get the public key from the leaf certificate
	leafCert := certs[0]
	publicKey, ok := leafCert.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("expected ECDSA public key")
	}

	// Parse and verify the JWT
	token, err := jwt.Parse(signedPayload, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodECDSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return publicKey, nil
	})
	if err != nil {
		return nil, fmt.Errorf("JWT verification failed: %w", err)
	}

	if !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	// Extract claims
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("failed to parse claims")
	}

	// Convert claims to payload struct
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal claims: %w", err)
	}

	var payload NotificationV2Payload
	if err := json.Unmarshal(claimsJSON, &payload); err != nil {
		return nil, fmt.Errorf("failed to parse payload: %w", err)
	}

	// Validate bundle ID
	if s.bundleID != "" && payload.Data.BundleID != s.bundleID {
		return nil, fmt.Errorf("bundle ID mismatch: expected %s, got %s", s.bundleID, payload.Data.BundleID)
	}

	return &payload, nil
}

// DecodeSignedTransactionInfo decodes the signedTransactionInfo JWS within the notification
// Note: The signature is already verified as part of the notification chain
func (s *AppleNotificationService) DecodeSignedTransactionInfo(signedTransaction string) (*JWSTransactionPayload, error) {
	// For nested transactions in notifications, we need to verify them as well
	parts := strings.Split(signedTransaction, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWS format")
	}

	// Decode header to get x5c certificate chain
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("failed to decode header: %w", err)
	}

	var header struct {
		Alg string   `json:"alg"`
		X5C []string `json:"x5c"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, fmt.Errorf("failed to parse header: %w", err)
	}

	if len(header.X5C) == 0 {
		return nil, fmt.Errorf("missing x5c certificate chain")
	}

	// Parse certificates from x5c
	certs := make([]*x509.Certificate, len(header.X5C))
	for i, certB64 := range header.X5C {
		certDER, err := base64.StdEncoding.DecodeString(certB64)
		if err != nil {
			return nil, fmt.Errorf("failed to decode certificate %d: %w", i, err)
		}
		cert, err := x509.ParseCertificate(certDER)
		if err != nil {
			return nil, fmt.Errorf("failed to parse certificate %d: %w", i, err)
		}
		certs[i] = cert
	}

	// Verify the certificate chain
	if err := verifyCertificateChainForNotification(certs); err != nil {
		return nil, fmt.Errorf("certificate chain verification failed: %w", err)
	}

	// Get the public key from the leaf certificate
	leafCert := certs[0]
	publicKey, ok := leafCert.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("expected ECDSA public key")
	}

	// Parse and verify the JWT
	token, err := jwt.Parse(signedTransaction, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodECDSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return publicKey, nil
	})
	if err != nil {
		return nil, fmt.Errorf("JWT verification failed: %w", err)
	}

	if !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	// Extract claims
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("failed to parse claims")
	}

	// Convert claims to JWSTransactionPayload
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal claims: %w", err)
	}

	var payload JWSTransactionPayload
	if err := json.Unmarshal(claimsJSON, &payload); err != nil {
		return nil, fmt.Errorf("failed to parse transaction payload: %w", err)
	}

	return &payload, nil
}

// verifyCertificateChainForNotification verifies the x5c certificate chain against Apple's root CA
func verifyCertificateChainForNotification(certs []*x509.Certificate) error {
	if len(certs) < 2 {
		return fmt.Errorf("need at least leaf and intermediate certificates")
	}

	// Build intermediate certificate pool
	intermediates := x509.NewCertPool()
	for i := 1; i < len(certs); i++ {
		intermediates.AddCert(certs[i])
	}

	// Verify the leaf certificate chains to Apple's trusted root
	leafCert := certs[0]
	opts := x509.VerifyOptions{
		Roots:         appleRootCertPool, // Use the same root pool from apple_iap.go
		Intermediates: intermediates,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}

	_, err := leafCert.Verify(opts)
	if err != nil {
		return fmt.Errorf("certificate verification failed: %w", err)
	}

	return nil
}
