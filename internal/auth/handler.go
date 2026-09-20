package auth

import (
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/unstrange/backend/internal/user"
	"github.com/unstrange/backend/pkg/response"
)

var phonePattern = regexp.MustCompile(`^[1-9][0-9]{6,14}$`)

type SendOTPRequest struct {
	Phone string `json:"phone" binding:"required,max=20"`
}

func SendOTPHandler(c *gin.Context) {
	var req SendOTPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid request")
		return
	}

	// Strip + prefix if present — MSG91 expects digits with country code, no +
	phone := strings.TrimPrefix(strings.TrimSpace(req.Phone), "+")

	if !phonePattern.MatchString(phone) {
		response.Error(c, http.StatusBadRequest, "invalid phone number")
		return
	}

	reqID, err := SendOTP(c.Request.Context(), phone)
	if err != nil {
		if errors.Is(err, ErrOTPCooldown) {
			c.Header("Retry-After", "30")
			response.Error(c, http.StatusTooManyRequests, "Wait 30 seconds before requesting another code")
		} else {
			response.Error(c, http.StatusServiceUnavailable, "Unable to send a code. Please try again later.")
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "OTP sent",
		"req_id":  reqID,
	})
}

type VerifyOTPRequest struct {
	Phone string `json:"phone" binding:"required,max=20"`
	OTP   string `json:"otp" binding:"required,min=4,max=8,numeric"`
	ReqID string `json:"req_id" binding:"required,max=200"`
}

func VerifyOTPHandler(c *gin.Context) {
	var req VerifyOTPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid request")
		return
	}

	phone := strings.TrimPrefix(strings.TrimSpace(req.Phone), "+")

	if !phonePattern.MatchString(phone) {
		response.Error(c, http.StatusBadRequest, "Invalid phone number")
		return
	}
	if err := VerifyOTP(c.Request.Context(), phone, req.OTP, req.ReqID); err != nil {
		response.Error(c, http.StatusUnauthorized, "The code is incorrect, expired, or unavailable. Try again or request a new code.")
		return
	}

	// Upsert user with provider=phone, provider_id=phone number
	u, err := user.Upsert(c.Request.Context(), user.UpsertParams{
		Provider:   "phone",
		ProviderID: phone,
	})
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to save user")
		return
	}

	issueTokens(c, u)
}

// --- Token refresh ---

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required,len=64,hexadecimal"`
}

func RefreshHandler(c *gin.Context) {
	var req RefreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid request")
		return
	}

	userID, err := ConsumeRefreshToken(c.Request.Context(), req.RefreshToken)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, "Invalid request")
		return
	}

	u, err := user.FindByID(c.Request.Context(), userID)
	if err != nil {
		response.Error(c, http.StatusNotFound, "user not found")
		return
	}

	issueTokens(c, u)
}

// --- Logout ---

func LogoutHandler(c *gin.Context) {
	var req RefreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid request")
		return
	}
	if err := RevokeRefreshToken(c.Request.Context(), req.RefreshToken); err != nil {
		response.Error(c, http.StatusServiceUnavailable, "Unable to sign out. Please try again.")
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "logged out"})
}

// --- helpers ---

func issueTokens(c *gin.Context, u *user.User) {
	if !u.IsActive {
		response.Error(c, 401, "Account unavailable")
		return
	}
	accessToken, err := GenerateAccessToken(u.ID)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to generate access token")
		return
	}

	refreshToken, err := GenerateRefreshToken(c.Request.Context(), u.ID)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to generate refresh token")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"user":          u,
	})
}
