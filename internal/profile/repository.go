package profile

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/unstrange/backend/internal/db"
)

var ErrNotFound = errors.New("profile not found")
var ErrUsernameTaken = errors.New("username already taken")

// fullSelect is the common SELECT for profile queries.
const fullSelect = `
	SELECT p.user_id, p.username, p.bio, p.location, p.website,
	       p.date_of_birth::TEXT, p.gender, p.instagram, p.interests,
	       p.is_onboarded, p.is_private,
	       u.display_name, u.avatar_url, u.email,
	       (SELECT COUNT(*) FROM follows WHERE following_id = p.user_id) as follower_count,
	       (SELECT COUNT(*) FROM follows WHERE follower_id = p.user_id) as following_count,
	       (SELECT COUNT(*) FROM posts WHERE user_id = p.user_id) as post_count,
	       p.created_at, p.updated_at,p.discoverable,(SELECT COUNT(*) FROM activity_participants ap JOIN activities a ON a.id=ap.activity_id WHERE ap.user_id=p.user_id AND ap.status='joined' AND a.ends_at<NOW() AND a.is_active)
	FROM profiles p
	JOIN users u ON u.id = p.user_id`

func scanProfile(row pgx.Row) (*Profile, error) {
	p := &Profile{}
	err := row.Scan(
		&p.UserID, &p.Username, &p.Bio, &p.Location, &p.Website,
		&p.DateOfBirth, &p.Gender, &p.Instagram, &p.Interests,
		&p.IsOnboarded, &p.IsPrivate,
		&p.DisplayName, &p.AvatarURL, &p.Email,
		&p.FollowerCount, &p.FollowingCount, &p.PostCount,
		&p.CreatedAt, &p.UpdatedAt, &p.Discoverable, &p.MeetupCount,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if p.Interests == nil {
		p.Interests = []string{}
	}
	return p, err
}

func FindByUserID(ctx context.Context, userID string) (*Profile, error) {
	return scanProfile(db.Pool.QueryRow(ctx, fullSelect+` WHERE p.user_id = $1`, userID))
}

func FindByUsername(ctx context.Context, username string) (*Profile, error) {
	return scanProfile(db.Pool.QueryRow(ctx, fullSelect+` WHERE p.username = $1`, username))
}

// Upsert creates or updates a profile. Used during onboarding.
func Upsert(ctx context.Context, userID string, p UpdateParams) (*Profile, error) {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx,
		`INSERT INTO profiles (user_id, username, bio, location, website, date_of_birth, gender, instagram, interests, is_onboarded, is_private)
		 VALUES ($1, $2, COALESCE($3,''), COALESCE($4,''), COALESCE($5,''), $6::DATE, COALESCE($7,''), COALESCE($8,''), COALESCE($9::text[],'{}'::text[]), COALESCE($10, FALSE), COALESCE($11, FALSE))
		 ON CONFLICT (user_id) DO UPDATE SET
		   username    = COALESCE(NULLIF($2,''), profiles.username),
		   bio         = COALESCE($3, profiles.bio),
		   location    = COALESCE($4, profiles.location),
		   website     = COALESCE($5, profiles.website),
		   date_of_birth = COALESCE($6::DATE, profiles.date_of_birth),
		   gender      = COALESCE($7, profiles.gender),
		   instagram   = COALESCE($8, profiles.instagram),
		   interests   = COALESCE($9, profiles.interests),
		   is_onboarded = COALESCE($10, profiles.is_onboarded),
		   is_private   = COALESCE($11, profiles.is_private),
		   updated_at  = NOW()`,
		userID,
		derefStr(p.Username),
		p.Bio,
		p.Location,
		p.Website,
		p.DateOfBirth,
		p.Gender,
		p.Instagram,
		p.Interests,
		p.IsOnboarded,
		p.IsPrivate,
	)
	if err != nil {
		if strings.Contains(err.Error(), "profiles_username_key") {
			return nil, ErrUsernameTaken
		}
		return nil, fmt.Errorf("upsert profile: %w", err)
	}

	// Update users table fields if provided
	if p.DisplayName != nil || p.AvatarURL != nil {
		_, err = tx.Exec(ctx,
			`UPDATE users SET
				display_name = COALESCE($2, display_name),
				avatar_url   = COALESCE($3, avatar_url),
				updated_at   = NOW()
			 WHERE id = $1`,
			userID, p.DisplayName, p.AvatarURL,
		)
		if err != nil {
			return nil, fmt.Errorf("update user: %w", err)
		}
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return FindByUserID(ctx, userID)
}

// Search finds profiles matching a query string.
func Search(ctx context.Context, viewerID, query string, limit, offset int) ([]ProfileCard, int64, error) {
	q := "%" + strings.ToLower(query) + "%"

	var total int64
	err := db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM profiles p
		 JOIN users u ON u.id = p.user_id
		 WHERE LOWER(p.username) LIKE $1 OR LOWER(u.display_name) LIKE $1`,
		q,
	).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	rows, err := db.Pool.Query(ctx,
		`SELECT p.user_id, p.username, u.display_name, u.avatar_url, p.bio,
		        EXISTS(SELECT 1 FROM follows WHERE follower_id = $2 AND following_id = p.user_id) as is_following
		 FROM profiles p
		 JOIN users u ON u.id = p.user_id
		 WHERE LOWER(p.username) LIKE $1 OR LOWER(u.display_name) LIKE $1
		 ORDER BY p.username
		 LIMIT $3 OFFSET $4`,
		q, viewerID, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	return collectProfileCards(rows), total, nil
}

// Discover returns users the viewer doesn't follow, ordered by popularity.
func Discover(ctx context.Context, viewerID string, limit, offset int) ([]ProfileCard, int64, error) {
	var total int64
	err := db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM profiles p
		 WHERE p.user_id != $1
		   AND NOT EXISTS(SELECT 1 FROM follows WHERE follower_id = $1 AND following_id = p.user_id)`,
		viewerID,
	).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	rows, err := db.Pool.Query(ctx,
		`SELECT p.user_id, p.username, u.display_name, u.avatar_url, p.bio, FALSE as is_following
		 FROM profiles p
		 JOIN users u ON u.id = p.user_id
		 WHERE p.user_id != $1
		   AND NOT EXISTS(SELECT 1 FROM follows WHERE follower_id = $1 AND following_id = p.user_id)
		 ORDER BY (SELECT COUNT(*) FROM follows WHERE following_id = p.user_id) DESC
		 LIMIT $2 OFFSET $3`,
		viewerID, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	return collectProfileCards(rows), total, nil
}

func collectProfileCards(rows pgx.Rows) []ProfileCard {
	var cards []ProfileCard
	for rows.Next() {
		var c ProfileCard
		if err := rows.Scan(&c.UserID, &c.Username, &c.DisplayName, &c.AvatarURL, &c.Bio, &c.IsFollowing); err != nil {
			continue
		}
		cards = append(cards, c)
	}
	if cards == nil {
		cards = []ProfileCard{}
	}
	return cards
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
