package social

import "time"

type Like struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	PostID    string    `json:"post_id"`
	CreatedAt time.Time `json:"created_at"`
}

type Comment struct {
	LikeCount int       `json:"like_count"`
	IsLiked   bool      `json:"is_liked"`
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	PostID    string    `json:"post_id"`
	ParentID  *string   `json:"parent_id,omitempty"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Joined author info
	Username    string  `json:"username"`
	DisplayName *string `json:"display_name"`
	AvatarURL   *string `json:"avatar_url"`
}

type Follow struct {
	FollowerID  string    `json:"follower_id"`
	FollowingID string    `json:"following_id"`
	CreatedAt   time.Time `json:"created_at"`
}

type FollowUser struct {
	UserID          string  `json:"user_id"`
	Username        string  `json:"username"`
	DisplayName     *string `json:"display_name"`
	AvatarURL       *string `json:"avatar_url"`
	FollowRequested bool    `json:"follow_requested"`
	IsFollowing     bool    `json:"is_following"`
}

type CommentRequest struct {
	Body     string  `json:"body" binding:"required,min=1,max=4000"`
	ParentID *string `json:"parent_id" binding:"omitempty,uuid"`
}
