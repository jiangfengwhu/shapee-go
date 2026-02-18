package llm

// ContentPart 表示多模态消息的一个部分（业务层抽象，与具体 Provider 无关）
type ContentPart struct {
	Type     string            `json:"type"`                // "text", "image", "audio", "file"
	Data     string            `json:"data,omitempty"`      // 内容数据: 文本内容/base64编码数据/URL
	MimeType string            `json:"mime_type,omitempty"` // MIME 类型，如 "image/jpeg", "audio/wav", "application/pdf"
	Metadata map[string]string `json:"metadata,omitempty"`  // 附加元数据，如 {"detail": "high", "filename": "doc.pdf"}
}

// ChatMessage 表示一条聊天消息
type ChatMessage struct {
	Role         string         `json:"role"`                    // "system", "user", "assistant", "tool"
	Content      string         `json:"content,omitempty"`       // 纯文本消息内容 (与 ContentParts 互斥)
	ContentParts []ContentPart  `json:"content_parts,omitempty"` // 多模态消息内容 (与 Content 互斥)
	ToolCallID   string         `json:"tool_call_id,omitempty"`  // tool 类型消息需要的 tool_call_id
	ToolCalls    []ToolCallInfo `json:"tool_calls,omitempty"`    // assistant 类型消息中的 tool calls
}

// ToolCallInfo 表示 assistant 消息中的 tool call 信息
type ToolCallInfo struct {
	ID       string           `json:"id"`
	Function ToolCallFunction `json:"function"`
}

// ToolCallFunction 表示 tool call 的函数信息
type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// StreamEventType 流式响应事件类型
type StreamEventType string

const (
	StreamEventText          StreamEventType = "text"
	StreamEventToolCallStart StreamEventType = "toolcall_start"
	StreamEventToolCall      StreamEventType = "toolcall"
)

// StreamEvent 流式响应事件
type StreamEvent struct {
	Type     StreamEventType // "text", "toolcall_start", "toolcall"
	Content  string          // 文本内容或工具调用参数
	ToolName string          // 工具名称 (仅 toolcall 类型)
}

// ChatStream 流式响应接口
type ChatStream interface {
	Next() bool
	Current() StreamEvent
	Err() error
}

// ToolDefinition 工具定义（业务层抽象）
type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"` // JSON Schema
}
