package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/unstrange/backend/internal/auth"
	"github.com/unstrange/backend/internal/db"
	"github.com/unstrange/backend/pkg/response"
)

const UserIDKey = "user_id"

func RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if header == "" || !strings.HasPrefix(header, "Bearer ") {
			response.Error(c, http.StatusUnauthorized, "missing authorization header")
			c.Abort()
			return
		}

		tokenStr := strings.TrimPrefix(header, "Bearer ")
		claims, err := auth.ValidateAccessToken(tokenStr)
		if err != nil {
			response.Error(c, http.StatusUnauthorized, "invalid or expired token")
			c.Abort()
			return
		}

		var active bool
		if err := db.Pool.QueryRow(c.Request.Context(), "SELECT is_active FROM users WHERE id=$1", claims.UserID).Scan(&active); err != nil || !active {
			response.Error(c, http.StatusUnauthorized, "account unavailable")
			c.Abort()
			return
		}
		c.Set(UserIDKey, claims.UserID)
		c.Next()
	}
}
