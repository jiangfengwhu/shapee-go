package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"iter"
	"strings"

	"cloud.google.com/go/auth"
	"cloud.google.com/go/auth/credentials"
	"google.golang.org/genai"
)

type VertexAIChatConfig struct {
	ProjectID                string
	Location                 string           // e.g., "us-central1"
	Model                    string           // e.g., "gemini-2.0-flash"
	ServiceAccountEmail      string           // Service account email
	ServiceAccountPrivateKey string           // Service account private key (PEM format)
	Tools                    []ToolDefinition // 使用业务层类型
	History                  []ChatMessage    // 使用业务层类型
	ResponseSchema           map[string]interface{}
}

func newVertexAIClient(ctx context.Context, config VertexAIChatConfig) (*genai.Client, error) {
	var creds *auth.Credentials
	var err error

	if config.ServiceAccountEmail != "" && config.ServiceAccountPrivateKey != "" {
		creds, err = credentials.DetectDefault(&credentials.DetectOptions{
			Scopes:          []string{"https://www.googleapis.com/auth/cloud-platform"},
			CredentialsJSON: buildServiceAccountJSON(config.ServiceAccountEmail, config.ServiceAccountPrivateKey, config.ProjectID),
		})
		if err != nil {
			return nil, err
		}
	}

	return genai.NewClient(ctx, &genai.ClientConfig{
		Project:     config.ProjectID,
		Location:    config.Location,
		Backend:     genai.BackendVertexAI,
		Credentials: creds,
	})
}

func vertexAIModelName(config VertexAIChatConfig) string {
	if config.Model != "" {
		return config.Model
	}
	return "gemini-2.0-flash"
}

// vertexAIChatStream 实现 ChatStream 接口，封装 Vertex AI 的流式响应
type vertexAIChatStream struct {
	next            func() (*genai.GenerateContentResponse, error, bool) // iter.Pull2 返回的拉取函数
	stop            func()                                               // iter.Pull2 返回的停止函数
	pendingParts    []*genai.Part                                        // 当前 response 中待处理的 parts
	currentEvent    StreamEvent
	currentToolName string
	currentToolArgs strings.Builder
	toolCallStarted bool
	finished        bool
	pendingToolCall bool
	err             error
}

func (s *vertexAIChatStream) Next() bool {
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

	if s.finished || s.err != nil {
		return false
	}

	for {
		// 先处理当前 response 中剩余的 parts
		for len(s.pendingParts) > 0 {
			part := s.pendingParts[0]
			s.pendingParts = s.pendingParts[1:]

			// Handle Function Calls
			if fc := part.FunctionCall; fc != nil {
				s.currentToolName = fc.Name
				// 将参数转换为 JSON 字符串
				argsJSON, _ := json.Marshal(fc.Args)
				s.currentToolArgs.WriteString(string(argsJSON))

				if !s.toolCallStarted {
					s.toolCallStarted = true
					s.currentEvent = StreamEvent{
						Type:     StreamEventToolCallStart,
						ToolName: s.currentToolName,
					}
					return true
				}
				continue
			}

			// Handle Text Content
			if part.Text != "" {
				s.currentEvent = StreamEvent{
					Type:    StreamEventText,
					Content: part.Text,
				}
				return true
			}
		}

		// 从迭代器拉取下一个 response
		if s.next == nil {
			break
		}
		resp, err, ok := s.next()
		if !ok {
			// 迭代器结束
			break
		}
		if err != nil {
			s.err = err
			return false
		}

		// 收集当前 response 的所有 parts
		for _, candidate := range resp.Candidates {
			if candidate.Content == nil {
				continue
			}
			s.pendingParts = append(s.pendingParts, candidate.Content.Parts...)
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

func (s *vertexAIChatStream) Current() StreamEvent {
	return s.currentEvent
}

func (s *vertexAIChatStream) Err() error {
	return s.err
}

// VertexAIChat 发起 Vertex AI 聊天请求，返回业务层抽象的 ChatStream
func VertexAIChat(ctx context.Context, config VertexAIChatConfig) ChatStream {
	client, err := newVertexAIClient(ctx, config)
	if err != nil {
		return &vertexAIChatStream{err: err, finished: true}
	}

	contents, systemInstruction := convertToVertexAIMessages(config.History)

	var tools []*genai.Tool
	if len(config.Tools) > 0 {
		var funcDecls []*genai.FunctionDeclaration
		for _, t := range config.Tools {
			funcDecls = append(funcDecls, &genai.FunctionDeclaration{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  convertToVertexAISchema(t.Parameters),
			})
		}
		tools = []*genai.Tool{{FunctionDeclarations: funcDecls}}
	}

	generateConfig := &genai.GenerateContentConfig{
		SystemInstruction: systemInstruction,
		Tools:             tools,
	}

	seqIter := client.Models.GenerateContentStream(ctx, vertexAIModelName(config), contents, generateConfig)
	next, stop := iter.Pull2(seqIter)

	return &vertexAIChatStream{next: next, stop: stop}
}

// VertexAIGenerateJSON 发起非流式 Vertex AI 请求，强制返回 JSON 格式
func VertexAIGenerateJSON(ctx context.Context, config VertexAIChatConfig) (string, error) {
	client, err := newVertexAIClient(ctx, config)
	if err != nil {
		return "", fmt.Errorf("vertexai generate json: %w", err)
	}

	contents, systemInstruction := convertToVertexAIMessages(config.History)

	generateConfig := &genai.GenerateContentConfig{
		SystemInstruction: systemInstruction,
		ResponseMIMEType:  "application/json",
	}
	if config.ResponseSchema != nil {
		generateConfig.ResponseSchema = convertToVertexAISchema(config.ResponseSchema)
	}

	resp, err := client.Models.GenerateContent(ctx, vertexAIModelName(config), contents, generateConfig)
	if err != nil {
		return "", fmt.Errorf("vertexai generate json: %w", err)
	}

	if resp != nil && len(resp.Candidates) > 0 && resp.Candidates[0].Content != nil {
		for _, part := range resp.Candidates[0].Content.Parts {
			if part.Text != "" {
				return part.Text, nil
			}
		}
	}

	return "", fmt.Errorf("vertexai generate json: no text content in response")
}

// convertToVertexAIMessages 将业务层 ChatMessage 转换为 Vertex AI 消息格式
func convertToVertexAIMessages(messages []ChatMessage) ([]*genai.Content, *genai.Content) {
	var contents []*genai.Content
	var systemInstruction *genai.Content

	for _, msg := range messages {
		switch msg.Role {
		case "system":
			// System message 作为 SystemInstruction，支持多条拼接
			if systemInstruction == nil {
				systemInstruction = &genai.Content{
					Parts: []*genai.Part{{Text: msg.Content}},
				}
			} else {
				// 追加到现有 system instruction
				systemInstruction.Parts = append(systemInstruction.Parts, &genai.Part{Text: msg.Content})
			}
		case "user":
			content := &genai.Content{Role: "user"}
			if len(msg.ContentParts) > 0 {
				for _, part := range msg.ContentParts {
					switch part.Type {
					case "text":
						content.Parts = append(content.Parts, &genai.Part{Text: part.Data})
					case "image":
						// 支持 base64 或 URL
						if strings.HasPrefix(part.Data, "data:") {
							// base64 内联数据，去掉 "data:...;base64," 前缀
							data := part.Data
							if idx := strings.Index(data, ","); idx != -1 {
								data = data[idx+1:]
							}
							// base64 解码
							decoded, err := base64.StdEncoding.DecodeString(data)
							if err != nil {
								continue // 解码失败，跳过这个图片
							}
							content.Parts = append(content.Parts, &genai.Part{
								InlineData: &genai.Blob{
									MIMEType: part.MimeType,
									Data:     decoded,
								},
							})
						} else {
							// URL
							content.Parts = append(content.Parts, &genai.Part{
								FileData: &genai.FileData{
									MIMEType: part.MimeType,
									FileURI:  part.Data,
								},
							})
						}
					}
				}
			} else {
				content.Parts = append(content.Parts, &genai.Part{Text: msg.Content})
			}
			contents = append(contents, content)
		case "assistant", "model":
			content := &genai.Content{Role: "model"}
			if len(msg.ToolCalls) > 0 {
				for _, tc := range msg.ToolCalls {
					var args map[string]any
					json.Unmarshal([]byte(tc.Function.Arguments), &args)
					content.Parts = append(content.Parts, &genai.Part{
						FunctionCall: &genai.FunctionCall{
							Name: tc.Function.Name,
							Args: args,
						},
					})
				}
			} else {
				content.Parts = append(content.Parts, &genai.Part{Text: msg.Content})
			}
			contents = append(contents, content)
		case "tool":
			// Tool response
			var result map[string]any
			json.Unmarshal([]byte(msg.Content), &result)
			content := &genai.Content{
				Role: "user", // Gemini uses user role for function responses
				Parts: []*genai.Part{{
					FunctionResponse: &genai.FunctionResponse{
						Name:     msg.ToolCallID, // 使用 ToolCallID 作为函数名
						Response: result,
					},
				}},
			}
			contents = append(contents, content)
		}
	}
	return contents, systemInstruction
}

// convertToVertexAISchema 将 JSON Schema 转换为 Vertex AI Schema
func convertToVertexAISchema(params map[string]interface{}) *genai.Schema {
	if params == nil {
		return nil
	}

	schema := &genai.Schema{}

	if t, ok := params["type"].(string); ok {
		switch t {
		case "object":
			schema.Type = genai.TypeObject
		case "array":
			schema.Type = genai.TypeArray
		case "string":
			schema.Type = genai.TypeString
		case "number":
			schema.Type = genai.TypeNumber
		case "integer":
			schema.Type = genai.TypeInteger
		case "boolean":
			schema.Type = genai.TypeBoolean
		}
	}

	if desc, ok := params["description"].(string); ok {
		schema.Description = desc
	}

	if props, ok := params["properties"].(map[string]any); ok {
		schema.Properties = make(map[string]*genai.Schema)
		for name, prop := range props {
			if propMap, ok := prop.(map[string]any); ok {
				schema.Properties[name] = convertToVertexAISchema(propMap)
			}
		}
	}

	if required, ok := params["required"].([]string); ok {
		schema.Required = required
	} else if required, ok := params["required"].([]interface{}); ok {
		for _, r := range required {
			if s, ok := r.(string); ok {
				schema.Required = append(schema.Required, s)
			}
		}
	}

	if items, ok := params["items"].(map[string]any); ok {
		schema.Items = convertToVertexAISchema(items)
	}

	if v, ok := params["minItems"].(int64); ok {
		schema.MinItems = &v
	}
	if v, ok := params["maxItems"].(int64); ok {
		schema.MaxItems = &v
	}

	if enum, ok := params["enum"].([]string); ok {
		schema.Enum = enum
	} else if enum, ok := params["enum"].([]interface{}); ok {
		for _, e := range enum {
			if s, ok := e.(string); ok {
				schema.Enum = append(schema.Enum, s)
			}
		}
	}

	return schema
}

// buildServiceAccountJSON 构建 service account JSON 凭据
func buildServiceAccountJSON(email, privateKey, projectID string) []byte {
	saJSON := map[string]string{
		"type":         "service_account",
		"project_id":   projectID,
		"client_email": email,
		"private_key":  privateKey,
		"auth_uri":     "https://accounts.google.com/o/oauth2/auth",
		"token_uri":    "https://oauth2.googleapis.com/token",
	}
	data, _ := json.Marshal(saJSON)
	return data
}
