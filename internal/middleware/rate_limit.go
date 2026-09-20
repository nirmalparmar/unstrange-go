package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/unstrange/backend/internal/db"
)

// Database-backed buckets work across server replicas. Expired buckets can be pruned daily.
func RateLimit(scope string, limit int) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == "GET" && !strings.Contains(c.FullPath(), "/suggestions/") {
			c.Next()
			return
		}
		requestScope, requestLimit := scope, limit
		if strings.Contains(c.FullPath(), "/suggestions/") {
			requestScope, requestLimit = "ai", 20
		}
		subject := c.GetString(UserIDKey)
		if subject == "" {
			subject = c.ClientIP()
		}
		var count int
		err := db.Pool.QueryRow(c.Request.Context(), `INSERT INTO request_limits(scope,subject,bucket) VALUES($1,$2,date_trunc('hour',NOW())) ON CONFLICT(scope,subject,bucket) DO UPDATE SET count=request_limits.count+1 RETURNING count`, requestScope, subject).Scan(&count)
		if err != nil {
			c.AbortWithStatusJSON(503, gin.H{"error": "Please try again shortly"})
			return
		}
		if count > requestLimit {
			c.Header("Retry-After", "3600")
			c.AbortWithStatusJSON(429, gin.H{"error": "Too many requests. Please try again later."})
			return
		}
		c.Next()
	}
}
