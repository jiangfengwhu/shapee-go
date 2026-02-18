package llm

import (
	"context"
	"keepy-go/config"
)

const (
	ProviderOpenAI   = "openai"
	ProviderVertexAI = "vertexai"
)

// ChatConfig 统一的聊天配置
type ChatConfig struct {
	Provider       string // LLM 提供商类型
	OpenAIConfig   config.OpenAIConfig
	VertexAIConfig config.VertexAIConfig

	// 通用配置
	Tools   []ToolDefinition
	History []ChatMessage
}

// Chat 统一的 AI 调用入口，根据 Provider 自动选择对应的实现
func StreamChat(ctx context.Context, cfg ChatConfig) ChatStream {
	switch cfg.Provider {
	case ProviderVertexAI:
		return VertexAIChat(ctx, VertexAIChatConfig{
			ProjectID:                cfg.VertexAIConfig.ProjectID,
			Location:                 cfg.VertexAIConfig.Location,
			Model:                    cfg.VertexAIConfig.Model,
			ServiceAccountEmail:      cfg.VertexAIConfig.ServiceAccountEmail,
			ServiceAccountPrivateKey: cfg.VertexAIConfig.ServiceAccountPrivateKey,
			Tools:                    cfg.Tools,
			History:                  cfg.History,
		})
	case ProviderOpenAI:
		fallthrough
	default:
		return OpenAIChat(ctx, OpenAIChatConfig{
			APIKey:  cfg.OpenAIConfig.APIKeys,
			BaseURL: cfg.OpenAIConfig.BaseURL,
			Tools:   cfg.Tools,
			History: cfg.History,
		})
	}
}
