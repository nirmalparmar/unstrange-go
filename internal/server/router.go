package server

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/unstrange/backend/internal/auth"
	"github.com/unstrange/backend/internal/community"
	"github.com/unstrange/backend/internal/config"
	"github.com/unstrange/backend/internal/media"
	"github.com/unstrange/backend/internal/middleware"
	"github.com/unstrange/backend/internal/post"
	"github.com/unstrange/backend/internal/profile"
	"github.com/unstrange/backend/internal/social"
	"github.com/unstrange/backend/internal/story"
	"github.com/unstrange/backend/internal/user"
)

func NewRouter() *gin.Engine {
	r := gin.New()
	if err := r.SetTrustedProxies(config.C.TrustedProxies); err != nil {
		log.Fatal("invalid TRUSTED_PROXIES configuration")
	}
	r.RemoteIPHeaders = []string{"X-Forwarded-For"}
	r.Use(gin.Logger(), gin.Recovery())
	r.Use(func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Cache-Control", "no-store")
		c.Next()
	})
	r.Use(func(c *gin.Context) {
		if c.Request.URL.Path != "/api/v1/media/upload" && c.Request.URL.Path != "/api/v1/private-media" {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
		}
		c.Next()
	})
	if config.C.Env != "production" {
		r.Static("/uploads", "./uploads")
	}
	r.Use(corsMiddleware())

	// Health
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// ── Auth routes (public) ──
	authGroup := r.Group("/auth")
	authGroup.Use(middleware.RateLimit("auth", 60))
	{
		// Phone OTP
		authGroup.POST("/otp/send", auth.SendOTPHandler)
		authGroup.POST("/otp/verify", auth.VerifyOTPHandler)

		// Token management
		authGroup.POST("/refresh", auth.RefreshHandler)
		authGroup.POST("/logout", auth.LogoutHandler)

	}

	// ── Protected API routes ──
	api := r.Group("/api/v1")
	api.Use(middleware.RequireAuth(), middleware.RateLimit("api", 1000), middleware.Visibility(), middleware.ModerateContent())
	{
		// User
		api.GET("/me", meHandler)

		// Profile
		api.GET("/profile", profile.GetMyProfile)
		api.PUT("/profile", profile.UpdateProfile)
		api.GET("/profile/:username", profile.GetByUsername)

		// Posts
		api.POST("/posts", post.CreateHandler)
		api.GET("/posts/:id", post.GetHandler)
		api.DELETE("/posts/:id", post.DeleteHandler)

		// Feed
		api.GET("/feed", post.FeedHandler)
		api.GET("/explore", post.ExploreHandler)

		// Reels
		api.GET("/reels", post.ReelsHandler)

		// Stories
		api.POST("/stories", story.CreateHandler)
		api.GET("/stories", story.FeedHandler)
		api.POST("/stories/:id/view", story.ViewHandler)
		api.DELETE("/stories/:id", story.DeleteHandler)

		// Social — Likes & Bookmarks
		api.POST("/posts/:id/like", social.ToggleLikeHandler)
		api.POST("/posts/:id/bookmark", social.ToggleBookmarkHandler)
		api.GET("/bookmarks", post.BookmarksHandler)

		// Social — Comments
		api.POST("/posts/:id/comments", social.CreateCommentHandler)
		api.GET("/posts/:id/comments", social.ListCommentsHandler)
		api.DELETE("/comments/:id", social.DeleteCommentHandler)

		// Social — Follows
		api.POST("/users/:id/follow", community.ToggleFollow)
		api.GET("/users/:id/followers", social.ListFollowersHandler)
		api.GET("/users/:id/following", social.ListFollowingHandler)
		api.GET("/users/:id/posts", post.UserPostsHandler)

		// Discovery & Search
		api.GET("/discover/users", profile.DiscoverUsers)
		api.GET("/search", profile.SearchHandler)

		// Legacy activity routes (new clients use /plans)
		api.POST("/activities", community.CreatePlan)
		api.GET("/activities/:id", community.GetPlan)
		api.DELETE("/activities/:id", community.CancelPlan)
		api.POST("/activities/:id/join", func(c *gin.Context) { c.JSON(410, gin.H{"error": "Use PUT /api/v1/plans/:id/membership"}) })
		api.GET("/users/:id/activities/created", community.UserCreatedPlans)
		api.GET("/users/:id/activities/joined", community.UserJoinedPlans)

		community.Register(api)

		// Media upload
		api.POST("/media/upload", media.UploadHandler)
	}

	return r
}

func meHandler(c *gin.Context) {
	userID := c.GetString(middleware.UserIDKey)
	u, err := user.FindByID(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	c.JSON(http.StatusOK, u)
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", config.C.FrontendURL)
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type")
		c.Header("Access-Control-Allow-Credentials", "true")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
