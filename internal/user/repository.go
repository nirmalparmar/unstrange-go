package user

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/unstrange/backend/internal/db"
)

var ErrNotFound = errors.New("user not found")

func FindByProviderID(ctx context.Context, provider, providerID string) (*User, error) {
	u := &User{}
	err := db.Pool.QueryRow(ctx,
		`SELECT id, email, display_name, avatar_url, provider, provider_id,
		        is_verified, is_active, created_at, updated_at
		 FROM users WHERE provider = $1 AND provider_id = $2`,
		provider, providerID,
	).Scan(&u.ID, &u.Email, &u.DisplayName, &u.AvatarURL,
		&u.Provider, &u.ProviderID, &u.IsVerified, &u.IsActive,
		&u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

func FindByID(ctx context.Context, id string) (*User, error) {
	u := &User{}
	err := db.Pool.QueryRow(ctx,
		`SELECT id, email, display_name, avatar_url, provider, provider_id,
		        is_verified, is_active, created_at, updated_at
		 FROM users WHERE id = $1`,
		id,
	).Scan(&u.ID, &u.Email, &u.DisplayName, &u.AvatarURL,
		&u.Provider, &u.ProviderID, &u.IsVerified, &u.IsActive,
		&u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

type UpsertParams struct {
	Email       *string
	DisplayName *string
	AvatarURL   *string
	Provider    string
	ProviderID  string
}

func Upsert(ctx context.Context, p UpsertParams) (*User, error) {
	u := &User{}
	err := db.Pool.QueryRow(ctx,
		`INSERT INTO users (email, display_name, avatar_url, provider, provider_id)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (provider, provider_id) DO UPDATE SET
		   email        = COALESCE(EXCLUDED.email, users.email),
		   display_name = COALESCE(EXCLUDED.display_name, users.display_name),
		   avatar_url   = COALESCE(EXCLUDED.avatar_url, users.avatar_url),
		   updated_at   = NOW()
		 RETURNING id, email, display_name, avatar_url, provider, provider_id,
		           is_verified, is_active, created_at, updated_at`,
		p.Email, p.DisplayName, p.AvatarURL, p.Provider, p.ProviderID,
	).Scan(&u.ID, &u.Email, &u.DisplayName, &u.AvatarURL,
		&u.Provider, &u.ProviderID, &u.IsVerified, &u.IsActive,
		&u.CreatedAt, &u.UpdatedAt)
	return u, err
}
