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

type JinaConfig struct {
	APIKey string `json:"api_key"`
}

type SearchAPIConfig struct {
	APIKey string `json:"api_key"`
}

// Config 配置结构体
type Config struct {
	Database struct {
		URL string `json:"url"`
	} `json:"database"`
	OpenAI     OpenAIConfig      `json:"openai"`
	VertexAI   VertexAIConfig    `json:"vertex_ai"`
	Jina       JinaConfig        `json:"jina"`
	SearchAPI  SearchAPIConfig   `json:"searchapi"`
	Prompts  map[string]string `json:"prompts"`
	Port     string            `json:"port"`
	AppleIAP struct {
		BundleID string           `json:"bundle_id"`
		Products map[string]int64 `json:"products"`
	} `json:"apple_iap"`
	Provider string `json:"provider"`
}

// Load 从配置文件加载配置
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

	// 对所有Prompts中的字段进行读取文件替换
	for key, value := range config.Prompts {
		if content, err := os.ReadFile(value); err == nil {
			config.Prompts[key] = string(content)
		}
	}

	return &config, nil
}

// Validate 验证配置
func (c *Config) Validate() error {
	// 验证数据库配置
	if c.Database.URL == "" {
		return fmt.Errorf("请在配置文件中设置数据库连接 URL")
	}

	// 验证OpenAI配置
	if c.OpenAI.APIKeys == "" {
		return fmt.Errorf("请在配置文件中设置OpenAI API密钥")
	}

	return nil
}
