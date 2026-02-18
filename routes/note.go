package routes

import (
	"fmt"
	"io"
	"keepy-go/config"
	"keepy-go/db"
	"keepy-go/notes"
	"net/http"

	"github.com/gin-gonic/gin"
)

func NoteRoutes(r gin.IRouter, cfg *config.Config) {
	r.POST("/note/process", func(c *gin.Context) {
		var input notes.NoteInput
		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		ticketID := c.GetString("ticket_id")
		if err := db.ConsumeTicket(c.Request.Context(), ticketID); err != nil {
			c.JSON(http.StatusPaymentRequired, gin.H{"error": err.Error()})
			return
		}

		c.Writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		c.Writer.Header().Set("Transfer-Encoding", "chunked")
		c.Writer.Header().Set("Cache-Control", "no-cache")
		c.Status(http.StatusOK)

		c.Stream(func(w io.Writer) bool {
			err := notes.ProcessNote(c.Request.Context(), cfg, input, func(response string) {
				_, _ = w.Write([]byte(response + "\n"))
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			})
			if err != nil {
				_, _ = fmt.Fprintf(w, "{\"type\":\"error\", \"data\":\"%s\"}\n", err.Error())
				return false
			}
			return false // Stop iterating after ProcessNote returns
		})
	})
}
