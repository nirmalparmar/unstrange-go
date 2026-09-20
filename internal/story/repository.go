package story

import (
	"context"
	"fmt"
	"time"

	"github.com/unstrange/backend/internal/db"
)

const storyTTL = 24 * time.Hour

// Create inserts a new story with a 24h expiry.
func Create(ctx context.Context, userID string, req CreateRequest) (*Story, error) {
	mediaType := req.MediaType
	if mediaType == "" {
		mediaType = "image"
	}
	duration := req.Duration
	if duration <= 0 {
		duration = 5
	}

	s := &Story{}
	err := db.Pool.QueryRow(ctx,
		`INSERT INTO stories (user_id, media_url, media_type, duration, expires_at)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, user_id, media_url, media_type, duration, expires_at, created_at`,
		userID, req.MediaURL, mediaType, duration, time.Now().Add(storyTTL),
	).Scan(&s.ID, &s.UserID, &s.MediaURL, &s.MediaType, &s.Duration, &s.ExpiresAt, &s.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("insert story: %w", err)
	}
	return s, nil
}

// ListFeed returns active stories grouped by user, for people the viewer follows + own stories.
func ListFeed(ctx context.Context, viewerID string) ([]UserStories, error) {
	rows, err := db.Pool.Query(ctx,
		`SELECT s.id, s.user_id, s.media_url, s.media_type, s.duration, s.created_at,
		        u.id, COALESCE(pr.username, ''), u.display_name, u.avatar_url,
		        EXISTS(SELECT 1 FROM story_views sv WHERE sv.story_id = s.id AND sv.viewer_id = $1) as is_viewed
		 FROM stories s
		 JOIN users u ON u.id = s.user_id
		 LEFT JOIN profiles pr ON pr.user_id = s.user_id
		 WHERE s.expires_at > NOW()
 AND u.is_active AND NOT EXISTS(SELECT 1 FROM blocks WHERE (blocker_id=$1 AND blocked_id=u.id) OR (blocker_id=u.id AND blocked_id=$1))
		   AND (s.user_id = $1 OR EXISTS(SELECT 1 FROM follows WHERE follower_id = $1 AND following_id = s.user_id))
		 ORDER BY s.user_id, s.created_at ASC`,
		viewerID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	userMap := map[string]*UserStories{}
	var order []string

	for rows.Next() {
		var item StoryItem
		var userID, uid, username string
		var displayName, avatarURL *string

		if err := rows.Scan(
			&item.ID, &userID, &item.MediaURL, &item.MediaType, &item.Duration, &item.CreatedAt,
			&uid, &username, &displayName, &avatarURL, &item.IsViewed,
		); err != nil {
			continue
		}

		us, ok := userMap[userID]
		if !ok {
			us = &UserStories{
				UserID:      userID,
				Username:    username,
				DisplayName: displayName,
				AvatarURL:   avatarURL,
				Stories:     []StoryItem{},
			}
			userMap[userID] = us
			order = append(order, userID)
		}
		us.Stories = append(us.Stories, item)
		if !item.IsViewed {
			us.HasUnviewed = true
		}
	}

	// Own stories first, then unviewed, then viewed
	result := make([]UserStories, 0, len(order))
	// First add own stories
	if own, ok := userMap[viewerID]; ok {
		result = append(result, *own)
	}
	// Then unviewed
	for _, uid := range order {
		if uid == viewerID {
			continue
		}
		if userMap[uid].HasUnviewed {
			result = append(result, *userMap[uid])
		}
	}
	// Then viewed
	for _, uid := range order {
		if uid == viewerID {
			continue
		}
		if !userMap[uid].HasUnviewed {
			result = append(result, *userMap[uid])
		}
	}

	return result, nil
}

// MarkViewed records that a viewer has seen a story.
func MarkViewed(ctx context.Context, storyID, viewerID string) error {
	_, err := db.Pool.Exec(ctx,
		`INSERT INTO story_views (story_id, viewer_id)
		 VALUES ($1, $2)
		 ON CONFLICT DO NOTHING`,
		storyID, viewerID,
	)
	return err
}

// Delete removes a story owned by the given user.
func Delete(ctx context.Context, storyID, userID string) error {
	tag, err := db.Pool.Exec(ctx,
		`DELETE FROM stories WHERE id = $1 AND user_id = $2`, storyID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("story not found")
	}
	return nil
}
