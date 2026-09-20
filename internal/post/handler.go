package post

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/unstrange/backend/internal/middleware"
	"github.com/unstrange/backend/pkg/response"
)

// CreateHandler handles POST /api/v1/posts
func CreateHandler(c *gin.Context) {
	userID := c.GetString(middleware.UserIDKey)

	var req CreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "invalid request: media is required")
		return
	}

	if req.Type != "post" && req.Type != "reel" {
		req.Type = "post"
	}

	p, err := Create(c.Request.Context(), userID, req)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to create post")
		return
	}
	c.JSON(http.StatusCreated, p)
}

// GetHandler handles GET /api/v1/posts/:id
func GetHandler(c *gin.Context) {
	viewerID := c.GetString(middleware.UserIDKey)
	postID := c.Param("id")

	p, err := FindByID(c.Request.Context(), postID, viewerID)
	if errors.Is(err, ErrNotFound) {
		response.Error(c, http.StatusNotFound, "post not found")
		return
	}
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to fetch post")
		return
	}
	c.JSON(http.StatusOK, p)
}

// DeleteHandler handles DELETE /api/v1/posts/:id
func DeleteHandler(c *gin.Context) {
	userID := c.GetString(middleware.UserIDKey)
	postID := c.Param("id")

	err := Delete(c.Request.Context(), postID, userID)
	if errors.Is(err, ErrNotFound) {
		response.Error(c, http.StatusNotFound, "post not found or not owned by you")
		return
	}
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to delete post")
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

// FeedHandler handles GET /api/v1/feed
func FeedHandler(c *gin.Context) {
	viewerID := c.GetString(middleware.UserIDKey)
	limit, offset := response.Pagination(c)

	posts, total, err := ListFeed(c.Request.Context(), viewerID, limit, offset)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to fetch feed")
		return
	}
	response.List(c, posts, total, limit, offset)
}

// ReelsHandler handles GET /api/v1/reels
func ReelsHandler(c *gin.Context) {
	viewerID := c.GetString(middleware.UserIDKey)
	limit, offset := response.Pagination(c)

	posts, total, err := ListReels(c.Request.Context(), viewerID, limit, offset)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to fetch reels")
		return
	}
	response.List(c, posts, total, limit, offset)
}

// UserPostsHandler handles GET /api/v1/users/:id/posts
func UserPostsHandler(c *gin.Context) {
	viewerID := c.GetString(middleware.UserIDKey)
	targetUserID := c.Param("id")
	postType := c.DefaultQuery("type", "post")
	limit, offset := response.Pagination(c)

	posts, total, err := ListByUser(c.Request.Context(), targetUserID, viewerID, postType, limit, offset)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to fetch posts")
		return
	}
	response.List(c, posts, total, limit, offset)
}

// BookmarksHandler handles GET /api/v1/bookmarks
func BookmarksHandler(c *gin.Context) {
	viewerID := c.GetString(middleware.UserIDKey)
	limit, offset := response.Pagination(c)

	posts, total, err := ListBookmarked(c.Request.Context(), viewerID, limit, offset)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to fetch bookmarks")
		return
	}
	response.List(c, posts, total, limit, offset)
}

// ExploreHandler handles GET /api/v1/explore
func ExploreHandler(c *gin.Context) {
	viewerID := c.GetString(middleware.UserIDKey)
	limit, offset := response.Pagination(c)

	posts, total, err := Explore(c.Request.Context(), viewerID, limit, offset)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to fetch explore")
		return
	}
	response.List(c, posts, total, limit, offset)
}
