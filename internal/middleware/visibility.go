package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/unstrange/backend/internal/db"
)

// Protect legacy resource routes with the same block/private rules as the meetup API.
func Visibility() gin.HandlerFunc {
	return func(c *gin.Context) {
		route := c.FullPath()
		id := c.Param("id")
		userID := ""
		ctx := c.Request.Context()
		me := c.GetString(UserIDKey)
		if id != "" {
			if _, err := uuid.Parse(id); err != nil {
				c.AbortWithStatusJSON(400, gin.H{"error": "Invalid identifier"})
				return
			}
		}
		switch {
		case strings.HasPrefix(route, "/api/v1/posts/"):
			_ = db.Pool.QueryRow(ctx, `SELECT user_id FROM posts WHERE id=$1`, id).Scan(&userID)
		case strings.HasPrefix(route, "/api/v1/stories/"):
			_ = db.Pool.QueryRow(ctx, `SELECT user_id FROM stories WHERE id=$1 AND expires_at>NOW()`, id).Scan(&userID)
		case strings.HasPrefix(route, "/api/v1/users/"):
			userID = id
		case strings.HasPrefix(route, "/api/v1/profile/"):
			_ = db.Pool.QueryRow(ctx, `SELECT user_id FROM profiles WHERE username=$1`, c.Param("username")).Scan(&userID)
		default:
			c.Next()
			return
		}
		if userID == "" {
			c.AbortWithStatusJSON(404, gin.H{"error": "Not found"})
			return
		}
		var visible bool
		err := db.Pool.QueryRow(ctx, `SELECT EXISTS(
            SELECT 1 FROM users WHERE id=$2 AND is_active
            AND NOT EXISTS(SELECT 1 FROM blocks
                WHERE (blocker_id=$1 AND blocked_id=$2)
                   OR (blocker_id=$2 AND blocked_id=$1))
        )`, me, userID).Scan(&visible)
		if err != nil || !visible {
			c.AbortWithStatusJSON(404, gin.H{"error": "Not found"})
			return
		}
		if me != userID && !strings.Contains(route, "/profile/") {
			var private, following bool
			err = db.Pool.QueryRow(ctx, `SELECT COALESCE(p.is_private,FALSE),EXISTS(SELECT 1 FROM follows WHERE follower_id=$1 AND following_id=$2) FROM users u LEFT JOIN profiles p ON p.user_id=u.id WHERE u.id=$2`, me, userID).Scan(&private, &following)
			if err != nil {
				c.AbortWithStatusJSON(404, gin.H{"error": "Person not found"})
				return
			}
			if private && !following && !strings.HasSuffix(route, "/follow") {
				c.AbortWithStatusJSON(403, gin.H{"error": "This member's moments are private"})
				return
			}
		}
		if strings.HasPrefix(route, "/api/v1/posts/") && me != userID {
			var restricted bool
			err = db.Pool.QueryRow(ctx, `SELECT audience='followers' AND NOT EXISTS(SELECT 1 FROM follows WHERE follower_id=$1 AND following_id=$2) FROM posts WHERE id=$3`, me, userID, id).Scan(&restricted)
			if err != nil || restricted {
				c.AbortWithStatusJSON(404, gin.H{"error": "Moment unavailable"})
				return
			}
		}
		c.Next()
	}
}
