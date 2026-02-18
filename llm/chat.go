package llm

import "context"

// CollectChat 非流式调用 LLM 并返回 JSON 格式的完整回复
func CollectChat(ctx context.Context, cfg ChatConfig) (string, error) {
	return GenerateJSON(ctx, cfg)
}
