package routes

import (
	"context"
	"io"
	"keepy-go/db"
	"keepy-go/llm"
	"keepy-go/tools"
	"net/http"

	"keepy-go/config"

	"github.com/gin-gonic/gin"
)

type ChatRequest struct {
	Prompt string `json:"prompt"`
}

func ChatRoutes(r gin.IRouter, cfg *config.Config) {
	// curl -X POST http://localhost:8080/chat -H "Content-Type: application/json" -H "X-Ticket-ID: ..." -d '{"prompt": "Hello, how are you?"}'
	r.POST("/chat", func(c *gin.Context) {
		var request ChatRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		ticketID := c.GetString("ticket_id")
		if err := db.ConsumeTicket(c.Request.Context(), ticketID); err != nil {
			c.JSON(http.StatusPaymentRequired, gin.H{"error": err.Error()})
			return
		}

		history := []llm.ChatMessage{
			{Role: "system", Content: "你是一个智能助手"},
			{Role: "user", Content: request.Prompt},
		}

		stream := llm.StreamChat(context.Background(), llm.ChatConfig{
			Provider:       cfg.Provider,
			OpenAIConfig:   cfg.OpenAI,
			VertexAIConfig: cfg.VertexAI,
			History:        history,
		})

		c.Writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		c.Writer.Header().Set("Transfer-Encoding", "chunked")
		c.Writer.Header().Set("Cache-Control", "no-cache")
		c.Status(http.StatusOK)

		c.Stream(func(w io.Writer) bool {
			if stream == nil {
				print("stream is nil\n")
				return false
			}
			if !stream.Next() {
				if err := stream.Err(); err != nil {
					print("stream error: ", err.Error(), "\n")
				}
				return false
			}

			event := stream.Current()
			switch event.Type {
			case llm.StreamEventText:
				if event.Content != "" {
					resp := tools.NewTextResponse(event.Content)
					_, _ = w.Write([]byte(resp + "\n"))
				}
			case llm.StreamEventToolCallStart:
				resp := tools.StartToolCallResponse(event.ToolName)
				_, _ = w.Write([]byte(resp + "\n"))
			case llm.StreamEventToolCall:
				resp := tools.NewToolCallResponse(event.ToolName, event.Content)
				_, _ = w.Write([]byte(resp + "\n"))
			}
			return true
		})
	})
}
