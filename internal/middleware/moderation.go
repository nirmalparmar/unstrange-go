package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/unstrange/backend/internal/intelligence"
)

// Moderate text before it is persisted. Provider outages fail closed when configured.
func ModerateContent() gin.HandlerFunc {
	return func(c *gin.Context) {
		if (c.Request.Method != "POST" && c.Request.Method != "PUT") || !strings.Contains(c.ContentType(), "json") || strings.Contains(c.FullPath(), "/reports") || strings.Contains(c.FullPath(), "/admin/") {
			c.Next()
			return
		}
		raw, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.AbortWithStatusJSON(413, gin.H{"error": "Content is too large"})
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(raw))
		var obj map[string]any
		if json.Unmarshal(raw, &obj) != nil {
			c.Next()
			return
		}
		var texts []string
		for _, key := range []string{"title", "description", "body", "caption", "bio", "answer"} {
			if value, ok := obj[key].(string); ok && strings.TrimSpace(value) != "" {
				texts = append(texts, value)
			}
		}
		if len(texts) == 0 {
			c.Next()
			return
		}
		flagged, err := intelligence.Moderate(c.Request.Context(), strings.Join(texts, "\n"))
		if err != nil {
			c.AbortWithStatusJSON(503, gin.H{"error": "Content checks are temporarily unavailable. Please try again shortly."})
			return
		}
		if flagged {
			c.AbortWithStatusJSON(422, gin.H{"error": "This content was flagged for review. Please revise it before sharing."})
			return
		}
		c.Next()
	}
}
