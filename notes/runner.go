package notes

import (
	"context"
	"encoding/json"
	"fmt"
	"keepy-go/config"
	"keepy-go/llm"
	"keepy-go/tools"
	"strings"
	"time"
)

type NoteInput struct {
	ChatHistory []llm.ChatMessage `json:"chat_history" binding:"required"` // 聊天消息列表（包含 system、历史记录和当前用户消息）
}

// serverSideTools maps tool names to their human-readable display messages.
var serverSideTools = map[string]string{
	"url_to_markdown": "正在读取网页内容...",
	"web_search":      "正在搜索...",
}

// ProcessNote processes the input note and stream responses via callback
func ProcessNote(ctx context.Context, cfg *config.Config, input NoteInput, callback func(response string)) error {
	// 1. Define Tools (Static)
	toolsDef := []llm.ToolDefinition{
		CreateDataSchemaTool(),
		UpdateDataSchemaTool(),
		DeleteDataSchemaTool(),
		AddDataRecordTool(),
		UpdateDataRecordTool(),
		DeleteDataRecordTool(),
		GetDataRecordTool(),
		URLToMarkdownTool(),
		WebSearchTool(),
	}

	// 2. Prepare history with system message
	systemMsg := llm.ChatMessage{
		Role:    "system",
		Content: cfg.Prompts["notes"],
	}
	history := append([]llm.ChatMessage{systemMsg}, input.ChatHistory...)

	// 3. Stream loop with server-side tool execution support
	for {
		stream := llm.StreamChat(ctx, llm.ChatConfig{
			Provider:       cfg.Provider,
			OpenAIConfig:   cfg.OpenAI,
			VertexAIConfig: cfg.VertexAI,
			Tools:          toolsDef,
			History:        history,
		})

		var textContent strings.Builder
		var toolCallName string
		var toolCallArgs string

		for stream.Next() {
			event := stream.Current()
			switch event.Type {
			case llm.StreamEventText:
				if event.Content != "" {
					textContent.WriteString(event.Content)
					callback(tools.NewTextResponse(event.Content))
				}
			case llm.StreamEventToolCallStart:
				toolCallName = event.ToolName
				if startMsg, ok := serverSideTools[event.ToolName]; ok {
					callback(tools.ServerToolStartResponse(event.ToolName, startMsg))
				} else {
					callback(tools.StartToolCallResponse(event.ToolName))
				}
			case llm.StreamEventToolCall:
				toolCallArgs = event.Content
				if _, ok := serverSideTools[event.ToolName]; !ok {
					callback(tools.NewToolCallResponse(event.ToolName, event.Content))
				}
			}
		}

		if err := stream.Err(); err != nil {
			return fmt.Errorf("stream error: %w", err)
		}

		// If the LLM called a server-side tool, execute it and continue the loop
		if _, ok := serverSideTools[toolCallName]; toolCallName != "" && ok {
			result, err := executeServerTool(ctx, cfg, toolCallName, toolCallArgs)
			if err != nil {
				result = fmt.Sprintf("Error: %s", err.Error())
				callback(tools.ServerToolEndResponse(toolCallName, toolCallArgs, result, err.Error(), false))
			} else {
				callback(tools.ServerToolEndResponse(toolCallName, toolCallArgs, result, "完成", true))
			}

			toolCallID := fmt.Sprintf("call_%d", time.Now().UnixNano())

			// Append assistant message with tool call to history
			history = append(history, llm.ChatMessage{
				Role:    "assistant",
				Content: textContent.String(),
				ToolCalls: []llm.ToolCallInfo{{
					ID: toolCallID,
					Function: llm.ToolCallFunction{
						Name:      toolCallName,
						Arguments: toolCallArgs,
					},
				}},
			})
			// Append tool result to history
			history = append(history, llm.ChatMessage{
				Role:       "tool",
				ToolCallID: toolCallID,
				Content:    result,
			})
			continue
		}

		// No server-side tool call, we're done
		break
	}

	return nil
}

// executeServerTool dispatches a server-side tool call and returns the result.
func executeServerTool(ctx context.Context, cfg *config.Config, toolName, argsJSON string) (string, error) {
	switch toolName {
	case "url_to_markdown":
		var args struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return "", fmt.Errorf("parse tool args: %w", err)
		}
		return FetchURLMarkdown(ctx, &cfg.Jina, args.URL)
	case "web_search":
		var args struct {
			Query string `json:"query"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return "", fmt.Errorf("parse tool args: %w", err)
		}
		return WebSearch(ctx, &cfg.SearchAPI, args.Query)
	default:
		return "", fmt.Errorf("unknown server-side tool: %s", toolName)
	}
}
