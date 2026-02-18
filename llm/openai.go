package llm

import (
	"context"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/azure"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/packages/ssestream"
)

type OpenAIChatConfig struct {
	APIKey  string
	BaseURL string
	Tools   []ToolDefinition // 使用业务层类型
	History []ChatMessage    // 使用业务层类型
}

// openaiChatStream 实现 ChatStream 接口，封装 OpenAI 的 SSE stream
type openaiChatStream struct {
	stream          *ssestream.Stream[openai.ChatCompletionChunk]
	currentEvent    StreamEvent
	currentToolName string
	currentToolArgs strings.Builder
	toolCallStarted bool
	finished        bool
	pendingToolCall bool // 是否有待发送的 toolcall 事件
}

func (s *openaiChatStream) Next() bool {
	// 如果上一次返回的是 toolcall_start，需要继续处理流
	// 如果有待发送的完整 toolcall，先发送它
	if s.pendingToolCall {
		s.currentEvent = StreamEvent{
			Type:     StreamEventToolCall,
			ToolName: s.currentToolName,
			Content:  s.currentToolArgs.String(),
		}
		s.pendingToolCall = false
		s.finished = true
		return true
	}

	if s.finished {
		return false
	}

	for s.stream.Next() {
		chunk := s.stream.Current()
		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]
		delta := choice.Delta

		// Handle Tool Calls
		if len(delta.ToolCalls) > 0 {
			tc := delta.ToolCalls[0]

			if tc.Function.Name != "" {
				s.currentToolName = tc.Function.Name
				if !s.toolCallStarted {
					s.toolCallStarted = true
					s.currentEvent = StreamEvent{
						Type:     StreamEventToolCallStart,
						ToolName: s.currentToolName,
					}
					// 继续读取参数，但先返回 start 事件
					if tc.Function.Arguments != "" {
						s.currentToolArgs.WriteString(tc.Function.Arguments)
					}
					return true
				}
			}

			if tc.Function.Arguments != "" {
				s.currentToolArgs.WriteString(tc.Function.Arguments)
			}
			continue
		}

		// Handle Content
		if delta.Content != "" {
			s.currentEvent = StreamEvent{
				Type:    StreamEventText,
				Content: delta.Content,
			}
			return true
		}
	}

	// 流结束，检查是否有完整的 tool call 需要发送
	if s.toolCallStarted {
		s.pendingToolCall = true
		return s.Next()
	}

	s.finished = true
	return false
}

func (s *openaiChatStream) Current() StreamEvent {
	return s.currentEvent
}

func (s *openaiChatStream) Err() error {
	return s.stream.Err()
}

// OpenAIChat 发起 OpenAI 聊天请求，返回业务层抽象的 ChatStream
func OpenAIChat(ctx context.Context, config OpenAIChatConfig) ChatStream {
	client := openai.NewClient(
		azure.WithEndpoint(config.BaseURL, "2024-10-01-preview"),
		azure.WithAPIKey(config.APIKey),
	)

	// 转换业务层消息到 OpenAI 格式
	messages := convertToOpenAIMessages(config.History)

	// 转换业务层工具定义到 OpenAI 格式
	var tools []openai.ChatCompletionToolUnionParam
	for _, t := range config.Tools {
		tools = append(tools, openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
			Name:        t.Name,
			Description: openai.String(t.Description),
			Parameters:  openai.FunctionParameters(t.Parameters),
		}))
	}

	params := openai.ChatCompletionNewParams{
		Messages: messages,
		Tools:    tools,
		Model:    "xiaolvgongcheng-forepart-openai-eastus2-gpt-5-chat",
	}

	stream := client.Chat.Completions.NewStreaming(ctx, params)
	return &openaiChatStream{stream: stream}
}

// convertToOpenAIMessages 将业务层 ChatMessage 转换为 OpenAI 消息格式
func convertToOpenAIMessages(messages []ChatMessage) []openai.ChatCompletionMessageParamUnion {
	var result []openai.ChatCompletionMessageParamUnion
	for _, msg := range messages {
		switch msg.Role {
		case "system":
			result = append(result, openai.SystemMessage(msg.Content))
		case "user":
			// 处理多模态内容
			if len(msg.ContentParts) > 0 {
				var parts []openai.ChatCompletionContentPartUnionParam
				for _, part := range msg.ContentParts {
					switch part.Type {
					case "text":
						parts = append(parts, openai.ChatCompletionContentPartUnionParam{
							OfText: &openai.ChatCompletionContentPartTextParam{
								Text: part.Data,
							},
						})
					case "image":
						imageURLPart := openai.ChatCompletionContentPartImageParam{
							ImageURL: openai.ChatCompletionContentPartImageImageURLParam{
								URL: part.Data,
							},
						}
						if detail, ok := part.Metadata["detail"]; ok {
							imageURLPart.ImageURL.Detail = detail
						}
						parts = append(parts, openai.ChatCompletionContentPartUnionParam{
							OfImageURL: &imageURLPart,
						})
					case "file":
						filePart := openai.ChatCompletionContentPartFileParam{
							File: openai.ChatCompletionContentPartFileFileParam{},
						}
						// 检查是否是 file_id 引用
						if fileID, ok := part.Metadata["file_id"]; ok {
							filePart.File.FileID = param.NewOpt(fileID)
						} else {
							// 使用 base64 数据
							filePart.File.FileData = param.NewOpt(part.Data)
						}
						if filename, ok := part.Metadata["filename"]; ok {
							filePart.File.Filename = param.NewOpt(filename)
						}
						parts = append(parts, openai.ChatCompletionContentPartUnionParam{
							OfFile: &filePart,
						})
					case "audio":
						format := "wav" // 默认格式
						if part.MimeType == "audio/mp3" || part.MimeType == "audio/mpeg" {
							format = "mp3"
						}
						parts = append(parts, openai.ChatCompletionContentPartUnionParam{
							OfInputAudio: &openai.ChatCompletionContentPartInputAudioParam{
								InputAudio: openai.ChatCompletionContentPartInputAudioInputAudioParam{
									Data:   part.Data,
									Format: format,
								},
							},
						})
					}
				}
				result = append(result, openai.ChatCompletionMessageParamUnion{
					OfUser: &openai.ChatCompletionUserMessageParam{
						Content: openai.ChatCompletionUserMessageParamContentUnion{
							OfArrayOfContentParts: parts,
						},
					},
				})
			} else {
				result = append(result, openai.UserMessage(msg.Content))
			}
		case "assistant":
			assistantMsg := openai.AssistantMessage(msg.Content)
			if len(msg.ToolCalls) > 0 {
				var toolCalls []openai.ChatCompletionMessageToolCallUnionParam
				for _, tc := range msg.ToolCalls {
					toolCalls = append(toolCalls, openai.ChatCompletionMessageToolCallUnionParam{
						OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
							ID: tc.ID,
							Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
								Name:      tc.Function.Name,
								Arguments: tc.Function.Arguments,
							},
						},
					})
				}
				assistantMsg.OfAssistant.ToolCalls = toolCalls
			}
			result = append(result, assistantMsg)
		case "tool":
			result = append(result, openai.ToolMessage(msg.Content, msg.ToolCallID))
		}
	}
	return result
}
