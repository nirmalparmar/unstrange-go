package story

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/unstrange/backend/internal/middleware"
	"github.com/unstrange/backend/pkg/response"
)

// CreateHandler handles POST /api/v1/stories
func CreateHandler(c *gin.Context) {
	userID := c.GetString(middleware.UserIDKey)

	var req CreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "media_url is required")
		return
	}

	s, err := Create(c.Request.Context(), userID, req)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to create story")
		return
	}
	c.JSON(http.StatusCreated, s)
}

// FeedHandler handles GET /api/v1/stories
func FeedHandler(c *gin.Context) {
	viewerID := c.GetString(middleware.UserIDKey)

	stories, err := ListFeed(c.Request.Context(), viewerID)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to fetch stories")
		return
	}
	c.JSON(http.StatusOK, stories)
}

// ViewHandler handles POST /api/v1/stories/:id/view
func ViewHandler(c *gin.Context) {
	viewerID := c.GetString(middleware.UserIDKey)
	storyID := c.Param("id")

	if err := MarkViewed(c.Request.Context(), storyID, viewerID); err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to mark viewed")
		return
	}
	c.JSON(http.StatusOK, gin.H{"viewed": true})
}

// DeleteHandler handles DELETE /api/v1/stories/:id
func DeleteHandler(c *gin.Context) {
	userID := c.GetString(middleware.UserIDKey)
	storyID := c.Param("id")

	if err := Delete(c.Request.Context(), storyID, userID); err != nil {
		response.Error(c, http.StatusNotFound, "story not found or not owned by you")
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}
