package profile

import "time"

type Profile struct {
	UserID      string   `json:"user_id"`
	Username    string   `json:"username"`
	Bio         string   `json:"bio"`
	Location    string   `json:"location"`
	Website     string   `json:"website"`
	DateOfBirth *string  `json:"date_of_birth,omitempty"`
	Gender      string   `json:"gender"`
	Instagram   string   `json:"instagram"`
	Interests   []string `json:"interests"`
	IsOnboarded bool     `json:"is_onboarded"`
	IsPrivate   bool     `json:"is_private"`

	// Joined from users table
	DisplayName *string `json:"display_name"`
	AvatarURL   *string `json:"avatar_url"`
	Email       *string `json:"email,omitempty"`

	// Computed counts
	FollowerCount  int `json:"follower_count"`
	FollowingCount int `json:"following_count"`
	PostCount      int `json:"post_count"`

	// For viewer context
	IsFollowing     bool `json:"is_following,omitempty"`
	FollowRequested bool `json:"follow_requested"`
	Discoverable    bool `json:"discoverable"`
	MeetupCount     int  `json:"meetup_count"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type UpdateParams struct {
	Username    *string  `json:"username"`
	DisplayName *string  `json:"display_name"`
	Bio         *string  `json:"bio"`
	Location    *string  `json:"location"`
	Website     *string  `json:"website"`
	DateOfBirth *string  `json:"date_of_birth"`
	Gender      *string  `json:"gender"`
	Instagram   *string  `json:"instagram"`
	AvatarURL   *string  `json:"avatar_url"`
	Interests   []string `json:"interests"`
	IsOnboarded *bool    `json:"is_onboarded"`
	IsPrivate   *bool    `json:"is_private"`
}

// ProfileCard is a lightweight profile for lists (discover, followers, search).
type ProfileCard struct {
	UserID      string  `json:"user_id"`
	Username    string  `json:"username"`
	DisplayName *string `json:"display_name"`
	AvatarURL   *string `json:"avatar_url"`
	Bio         string  `json:"bio"`
	IsFollowing bool    `json:"is_following"`
}
