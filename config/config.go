package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type OpenAIConfig struct {
	APIKeys string `json:"api_keys"`
	BaseURL string `json:"base_url"`
}

type VertexAIConfig struct {
	ProjectID                string `json:"project_id"`
	Location                 string `json:"location"`
	Model                    string `json:"model"`
	ServiceAccountEmail      string `json:"service_account_email"`
	ServiceAccountPrivateKey string `json:"service_account_private_key"`
}

type APNsConfig struct {
	AuthKeyPath  string `json:"auth_key_path"`
	AuthKey      string `json:"auth_key"`
	KeyID        string `json:"key_id"`
	TeamID       string `json:"team_id"`
	BundleID     string `json:"bundle_id"`
	IsProduction bool   `json:"is_production"`
}

type Config struct {
	Database struct {
		URL string `json:"url"`
	} `json:"database"`
	OpenAI   OpenAIConfig   `json:"openai"`
	VertexAI VertexAIConfig `json:"vertex_ai"`
	APNs     APNsConfig     `json:"apns"`
	Prompts  map[string]string `json:"prompts"`
	Port     string            `json:"port"`
	AppleIAP struct {
		BundleID string          `json:"bundle_id"`
		Products map[string]bool `json:"products"`
	} `json:"apple_iap"`
	Provider string `json:"provider"`

	// AllowMultipleWeightUpdatesPerDay 为 true 时允许同一天多次更新体重，便于开发测试；生产环境建议为 false
	AllowMultipleWeightUpdatesPerDay bool `json:"allow_multiple_weight_updates_per_day"`
}

func Load(filename string) (*Config, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("无法打开配置文件 %s: %v", filename, err)
	}
	defer file.Close()

	var config Config
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %v", err)
	}

	for key, value := range config.Prompts {
		if content, err := os.ReadFile(value); err == nil {
			config.Prompts[key] = string(content)
		}
	}

	// 如果 auth_key_path 指定了文件，从文件读取 APNs 密钥
	if config.APNs.AuthKeyPath != "" && config.APNs.AuthKey == "" {
		if content, err := os.ReadFile(config.APNs.AuthKeyPath); err == nil {
			config.APNs.AuthKey = string(content)
		}
	}

	return &config, nil
}

func (c *Config) Validate() error {
	if c.Database.URL == "" {
		return fmt.Errorf("请在配置文件中设置数据库连接 URL")
	}
	if c.OpenAI.APIKeys == "" && c.VertexAI.ProjectID == "" {
		return fmt.Errorf("请在配置文件中设置至少一个 LLM 提供商")
	}
	return nil
}
