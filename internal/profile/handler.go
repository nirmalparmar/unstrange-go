package profile

import (
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/unstrange/backend/internal/db"
	"github.com/unstrange/backend/internal/middleware"
	"github.com/unstrange/backend/pkg/response"
)

// GetMyProfile handles GET /api/v1/profile
func GetMyProfile(c *gin.Context) {
	userID := c.GetString(middleware.UserIDKey)
	p, err := FindByUserID(c.Request.Context(), userID)
	if errors.Is(err, ErrNotFound) {
		// Return empty profile stub for users who haven't completed onboarding
		c.JSON(http.StatusOK, gin.H{
			"user_id":      userID,
			"is_onboarded": false,
		})
		return
	}
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to fetch profile")
		return
	}
	c.JSON(http.StatusOK, p)
}

// UpdateProfile handles PUT /api/v1/profile
func UpdateProfile(c *gin.Context) {
	userID := c.GetString(middleware.UserIDKey)

	var params UpdateParams
	if err := c.ShouldBindJSON(&params); err != nil {
		response.Error(c, http.StatusBadRequest, "invalid request body")
		return
	}

	if params.Username != nil && !regexp.MustCompile(`^[a-z0-9_]{3,24}$`).MatchString(*params.Username) {
		response.Error(c, 400, "Use 3–24 lowercase letters, numbers or underscores for your username")
		return
	}
	if params.DisplayName != nil && (strings.TrimSpace(*params.DisplayName) == "" || len(*params.DisplayName) > 100) {
		response.Error(c, 400, "Enter a name under 100 characters")
		return
	}
	if params.Bio != nil && len(*params.Bio) > 2000 {
		response.Error(c, 400, "Bio is too long")
		return
	}
	if params.IsOnboarded != nil && *params.IsOnboarded && (params.Username == nil || params.DisplayName == nil) {
		response.Error(c, 400, "A name and username are required")
		return
	}
	p, err := Upsert(c.Request.Context(), userID, params)
	if errors.Is(err, ErrUsernameTaken) {
		response.Error(c, http.StatusConflict, "username already taken")
		return
	}
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to update profile")
		return
	}
	c.JSON(http.StatusOK, p)
}

// GetByUsername handles GET /api/v1/profile/:username
func GetByUsername(c *gin.Context) {
	viewerID := c.GetString(middleware.UserIDKey)
	username := c.Param("username")

	p, err := FindByUsername(c.Request.Context(), username)
	if errors.Is(err, ErrNotFound) {
		response.Error(c, http.StatusNotFound, "profile not found")
		return
	}
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to fetch profile")
		return
	}

	// Check if viewer follows this user
	if viewerID != p.UserID {
		p.Email = nil // Hide private identity fields from other users
		p.DateOfBirth = nil
		p.Gender = ""
		var isFollowing bool
		_ = db.Pool.QueryRow(c.Request.Context(),
			`SELECT EXISTS(SELECT 1 FROM follows WHERE follower_id = $1 AND following_id = $2)`,
			viewerID, p.UserID,
		).Scan(&isFollowing)
		p.IsFollowing = isFollowing

		// If private and not following, hide details
		if p.IsPrivate && !isFollowing {
			p.Bio = ""
			p.Location = ""
			p.Website = ""
			p.Instagram = ""
			p.Interests = []string{}
		}
	}

	_ = db.Pool.QueryRow(c.Request.Context(), `SELECT EXISTS(SELECT 1 FROM follow_requests WHERE requester_id=$1 AND recipient_id=$2)`, viewerID, p.UserID).Scan(&p.FollowRequested)
	c.JSON(http.StatusOK, p)
}

// DiscoverUsers handles GET /api/v1/discover/users
func DiscoverUsers(c *gin.Context) {
	viewerID := c.GetString(middleware.UserIDKey)
	limit, offset := response.Pagination(c)

	cards, total, err := Discover(c.Request.Context(), viewerID, limit, offset)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to discover users")
		return
	}
	response.List(c, cards, total, limit, offset)
}

// SearchHandler handles GET /api/v1/search
func SearchHandler(c *gin.Context) {
	viewerID := c.GetString(middleware.UserIDKey)
	query := c.Query("q")
	if query == "" {
		response.Error(c, http.StatusBadRequest, "query parameter 'q' is required")
		return
	}

	limit, offset := response.Pagination(c)
	cards, total, err := Search(c.Request.Context(), viewerID, query, limit, offset)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "search failed")
		return
	}
	response.List(c, cards, total, limit, offset)
}
