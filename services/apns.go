package services

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"keepy-go/config"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	apnsProductionURL = "https://api.push.apple.com"
	apnsSandboxURL    = "https://api.sandbox.push.apple.com"
	tokenRefreshAfter = 50 * time.Minute
)

type APNsService struct {
	cfg        config.APNsConfig
	privateKey *ecdsa.PrivateKey
	token      string
	tokenTime  time.Time
	mu         sync.Mutex
	client     *http.Client
}

type APNsPayload struct {
	Aps APNsAps `json:"aps"`
}

type APNsAps struct {
	Alert APNsAlert `json:"alert"`
	Sound string    `json:"sound,omitempty"`
	Badge *int      `json:"badge,omitempty"`
}

type APNsAlert struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

func NewAPNsService(cfg config.APNsConfig) (*APNsService, error) {
	if cfg.AuthKey == "" {
		return &APNsService{cfg: cfg, client: &http.Client{Timeout: 30 * time.Second}}, nil
	}

	key, err := parseAPNsKey(cfg.AuthKey)
	if err != nil {
		return nil, fmt.Errorf("parse APNs auth key: %w", err)
	}

	return &APNsService{
		cfg:        cfg,
		privateKey: key,
		client:     &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func parseAPNsKey(keyPEM string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(keyPEM))
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}

	ecKey, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("key is not ECDSA")
	}
	return ecKey, nil
}

func (s *APNsService) getToken() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.token != "" && time.Since(s.tokenTime) < tokenRefreshAfter {
		return s.token, nil
	}

	if s.privateKey == nil {
		return "", fmt.Errorf("APNs auth key not configured")
	}

	token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"iss": s.cfg.TeamID,
		"iat": time.Now().Unix(),
	})
	token.Header["kid"] = s.cfg.KeyID

	signed, err := token.SignedString(s.privateKey)
	if err != nil {
		return "", fmt.Errorf("sign JWT: %w", err)
	}

	s.token = signed
	s.tokenTime = time.Now()
	return s.token, nil
}

func (s *APNsService) baseURL() string {
	if s.cfg.IsProduction {
		return apnsProductionURL
	}
	return apnsSandboxURL
}

// SendPush 向指定设备发送推送通知
func (s *APNsService) SendPush(deviceToken, title, body string) error {
	if deviceToken == "" {
		return fmt.Errorf("empty device token")
	}

	token, err := s.getToken()
	if err != nil {
		return err
	}

	payload := APNsPayload{
		Aps: APNsAps{
			Alert: APNsAlert{Title: title, Body: body},
			Sound: "default",
		},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	url := fmt.Sprintf("%s/3/device/%s", s.baseURL(), deviceToken)
	req, err := http.NewRequest("POST", url, bytes.NewReader(payloadBytes))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("authorization", "bearer "+token)
	req.Header.Set("apns-topic", s.cfg.BundleID)
	req.Header.Set("apns-push-type", "alert")
	req.Header.Set("apns-priority", "10")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return nil
	}

	respBody, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("APNs error (status %d): %s", resp.StatusCode, string(respBody))
}

// IsConfigured 检查 APNs 是否配置完整
func (s *APNsService) IsConfigured() bool {
	return s.privateKey != nil && s.cfg.KeyID != "" && s.cfg.TeamID != "" && s.cfg.BundleID != ""
}
