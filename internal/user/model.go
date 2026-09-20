package user

import "time"

type User struct {
	ID          string    `json:"id"`
	Email       *string   `json:"email"`
	DisplayName *string   `json:"display_name"`
	AvatarURL   *string   `json:"avatar_url"`
	Provider    string    `json:"provider"`
	ProviderID  string    `json:"provider_id"`
	IsVerified  bool      `json:"is_verified"`
	IsActive    bool      `json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
