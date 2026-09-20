package social

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/unstrange/backend/internal/middleware"
	"github.com/unstrange/backend/pkg/response"
)

// ToggleLikeHandler handles POST /api/v1/posts/:id/like
func ToggleLikeHandler(c *gin.Context) {
	userID := c.GetString(middleware.UserIDKey)
	postID := c.Param("id")

	liked, err := ToggleLike(c.Request.Context(), userID, postID)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to toggle like")
		return
	}
	c.JSON(http.StatusOK, gin.H{"liked": liked})
}

// ToggleBookmarkHandler handles POST /api/v1/posts/:id/bookmark
func ToggleBookmarkHandler(c *gin.Context) {
	userID := c.GetString(middleware.UserIDKey)
	postID := c.Param("id")

	bookmarked, err := ToggleBookmark(c.Request.Context(), userID, postID)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to toggle bookmark")
		return
	}
	c.JSON(http.StatusOK, gin.H{"bookmarked": bookmarked})
}

// CreateCommentHandler handles POST /api/v1/posts/:id/comments
func CreateCommentHandler(c *gin.Context) {
	userID := c.GetString(middleware.UserIDKey)
	postID := c.Param("id")

	var req CommentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "body is required")
		return
	}

	req.Body = strings.TrimSpace(req.Body)
	if req.Body == "" {
		response.Error(c, http.StatusBadRequest, "Write a comment first")
		return
	}
	comment, err := CreateComment(c.Request.Context(), userID, postID, req)
	if errors.Is(err, ErrInvalidParent) {
		response.Error(c, http.StatusBadRequest, "Reply to a comment on this post")
		return
	}
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to create comment")
		return
	}
	c.JSON(http.StatusCreated, comment)
}

// ListCommentsHandler handles GET /api/v1/posts/:id/comments
func ListCommentsHandler(c *gin.Context) {
	postID := c.Param("id")
	limit, offset := response.Pagination(c)

	comments, total, err := ListComments(c.Request.Context(), postID, c.GetString(middleware.UserIDKey), limit, offset)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to fetch comments")
		return
	}
	response.List(c, comments, total, limit, offset)
}

// DeleteCommentHandler handles DELETE /api/v1/comments/:id
func DeleteCommentHandler(c *gin.Context) {
	userID := c.GetString(middleware.UserIDKey)
	commentID := c.Param("id")

	if err := DeleteComment(c.Request.Context(), commentID, userID); err != nil {
		response.Error(c, http.StatusNotFound, "comment not found or not owned by you")
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

// ToggleFollowHandler handles POST /api/v1/users/:id/follow
func ToggleFollowHandler(c *gin.Context) {
	followerID := c.GetString(middleware.UserIDKey)
	followingID := c.Param("id")

	following, err := ToggleFollow(c.Request.Context(), followerID, followingID)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "Unable to change this follow")
		return
	}
	c.JSON(http.StatusOK, gin.H{"following": following})
}

// ListFollowersHandler handles GET /api/v1/users/:id/followers
func ListFollowersHandler(c *gin.Context) {
	viewerID := c.GetString(middleware.UserIDKey)
	userID := c.Param("id")
	limit, offset := response.Pagination(c)

	users, total, err := ListFollowers(c.Request.Context(), userID, viewerID, limit, offset)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to fetch followers")
		return
	}
	response.List(c, users, total, limit, offset)
}

// ListFollowingHandler handles GET /api/v1/users/:id/following
func ListFollowingHandler(c *gin.Context) {
	viewerID := c.GetString(middleware.UserIDKey)
	userID := c.Param("id")
	limit, offset := response.Pagination(c)

	users, total, err := ListFollowing(c.Request.Context(), userID, viewerID, limit, offset)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to fetch following")
		return
	}
	response.List(c, users, total, limit, offset)
}
