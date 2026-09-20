package post

import "time"

// Post is the database row model.
type Post struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Caption   string    `json:"caption"`
	Location  string    `json:"location"`
	Type      string    `json:"type"` // "post" or "reel"
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Media represents a single media attachment on a post.
type Media struct {
	ID        string  `json:"id"`
	URL       string  `json:"url"`
	Type      string  `json:"type"` // "image" or "video"
	Width     int     `json:"width"`
	Height    int     `json:"height"`
	Duration  float64 `json:"duration"`
	SortOrder int     `json:"sort_order"`
}

// Author is a lightweight user object embedded in post responses.
type Author struct {
	ID          string  `json:"id"`
	Username    string  `json:"username"`
	DisplayName *string `json:"display_name"`
	AvatarURL   *string `json:"avatar_url"`
}

// Response is the API response for a single post with all enriched data.
type Response struct {
	ID           string    `json:"id"`
	Author       Author    `json:"author"`
	Caption      string    `json:"caption"`
	Location     string    `json:"location"`
	Type         string    `json:"type"`
	Media        []Media   `json:"media"`
	LikeCount    int       `json:"like_count"`
	CommentCount int       `json:"comment_count"`
	IsLiked      bool      `json:"is_liked"`
	IsBookmarked bool      `json:"is_bookmarked"`
	CreatedAt    time.Time `json:"created_at"`
}

// CreateRequest is the payload for creating a post.
type CreateRequest struct {
	Audience string       `json:"audience" binding:"omitempty,oneof=public followers"`
	Caption  string       `json:"caption"`
	Location string       `json:"location"`
	Type     string       `json:"type"` // "post" or "reel"
	Media    []MediaInput `json:"media" binding:"required,min=1"`
}

type MediaInput struct {
	URL      string  `json:"url" binding:"required"`
	Type     string  `json:"type" binding:"required"` // "image" or "video"
	Width    int     `json:"width"`
	Height   int     `json:"height"`
	Duration float64 `json:"duration"`
}
