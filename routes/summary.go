package routes

import (
	"io"
	"keepy-go/config"
	"keepy-go/db"
	"keepy-go/llm"
	"keepy-go/tools"
	"net/http"

	"github.com/gin-gonic/gin"
)

type SummaryInput struct {
	ChatHistory []llm.ChatMessage `json:"chat_history" binding:"required"`
}

func SummaryRoutes(r gin.IRouter, cfg *config.Config) {
	r.POST("/note/summary", func(c *gin.Context) {
		var req SummaryInput
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		ticketID := c.GetString("ticket_id")
		if err := db.ConsumeTicket(c.Request.Context(), ticketID); err != nil {
			c.JSON(http.StatusPaymentRequired, gin.H{"error": err.Error()})
			return
		}

		// Add system message
		systemMessage := cfg.Prompts["summary"]
		systemMsg := llm.ChatMessage{Role: "system", Content: systemMessage}
		newHistory := append([]llm.ChatMessage{systemMsg}, req.ChatHistory...)

		c.Writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		c.Writer.Header().Set("Transfer-Encoding", "chunked")
		c.Writer.Header().Set("Cache-Control", "no-cache")
		c.Status(http.StatusOK)

		c.Stream(func(w io.Writer) bool {
			stream := llm.StreamChat(c.Request.Context(), llm.ChatConfig{
				Provider:       cfg.Provider,
				OpenAIConfig:   cfg.OpenAI,
				VertexAIConfig: cfg.VertexAI,
				History:        newHistory,
			})

			for stream.Next() {
				event := stream.Current()
				if event.Type == llm.StreamEventText && event.Content != "" {
					w.Write([]byte(tools.NewTextResponse(event.Content) + "\n"))
					if f, ok := w.(http.Flusher); ok {
						f.Flush()
					}
				}
			}
			return false
		})
	})
}
