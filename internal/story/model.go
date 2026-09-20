package story

import "time"

type Story struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	MediaURL  string    `json:"media_url"`
	MediaType string    `json:"media_type"`
	Duration  float64   `json:"duration"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// StoryItem is a single story with view state.
type StoryItem struct {
	ID        string    `json:"id"`
	MediaURL  string    `json:"media_url"`
	MediaType string    `json:"media_type"`
	Duration  float64   `json:"duration"`
	IsViewed  bool      `json:"is_viewed"`
	CreatedAt time.Time `json:"created_at"`
}

// UserStories groups stories by user for the stories tray.
type UserStories struct {
	UserID      string      `json:"user_id"`
	Username    string      `json:"username"`
	DisplayName *string     `json:"display_name"`
	AvatarURL   *string     `json:"avatar_url"`
	HasUnviewed bool        `json:"has_unviewed"`
	Stories     []StoryItem `json:"stories"`
}

// CreateRequest for creating a story.
type CreateRequest struct {
	MediaURL  string  `json:"media_url" binding:"required"`
	MediaType string  `json:"media_type"` // "image" or "video"
	Duration  float64 `json:"duration"`   // display duration in seconds
}
