package post

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/unstrange/backend/internal/db"
)

var ErrNotFound = errors.New("post not found")

// Create inserts a post and its media attachments.
func Create(ctx context.Context, userID string, req CreateRequest) (*Response, error) {
	if req.Audience == "" {
		req.Audience = "public"
	}
	postType := req.Type
	if postType == "" {
		postType = "post"
	}

	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var postID string
	err = tx.QueryRow(ctx,
		`INSERT INTO posts (user_id, caption, location, type, audience)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id`,
		userID, req.Caption, req.Location, postType, req.Audience,
	).Scan(&postID)
	if err != nil {
		return nil, fmt.Errorf("insert post: %w", err)
	}

	for i, m := range req.Media {
		_, err = tx.Exec(ctx,
			`INSERT INTO post_media (post_id, url, type, width, height, duration, sort_order)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			postID, m.URL, m.Type, m.Width, m.Height, m.Duration, i,
		)
		if err != nil {
			return nil, fmt.Errorf("insert media: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return FindByID(ctx, postID, userID)
}

// FindByID returns a single post with all enriched data.
func FindByID(ctx context.Context, postID, viewerID string) (*Response, error) {
	r := &Response{}
	var mediaJSON []byte
	err := db.Pool.QueryRow(ctx, feedQuery+` WHERE p.id = $2`, viewerID, postID).Scan(
		&r.ID, &r.Caption, &r.Location, &r.Type, &r.CreatedAt,
		&r.Author.ID, &r.Author.Username, &r.Author.DisplayName, &r.Author.AvatarURL,
		&mediaJSON, &r.LikeCount, &r.CommentCount, &r.IsLiked, &r.IsBookmarked,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	r.Media = parseMedia(mediaJSON)
	return r, nil
}

// Delete removes a post owned by the given user.
func Delete(ctx context.Context, postID, userID string) error {
	tag, err := db.Pool.Exec(ctx,
		`DELETE FROM posts WHERE id = $1 AND user_id = $2`, postID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// feedQuery is the base SELECT used by both feed listing and single-post fetch.
// $1 = viewerID (used for is_liked / is_bookmarked checks).
const feedQuery = `
	SELECT p.id, p.caption, p.location, p.type, p.created_at,
	       u.id, COALESCE(pr.username, ''), u.display_name, u.avatar_url,
	       COALESCE(
	         (SELECT json_agg(json_build_object(
	           'id', pm.id, 'url', pm.url, 'type', pm.type,
	           'width', pm.width, 'height', pm.height,
	           'duration', pm.duration, 'sort_order', pm.sort_order
	         ) ORDER BY pm.sort_order)
	         FROM post_media pm WHERE pm.post_id = p.id), '[]'::json
	       ) as media,
	       (SELECT COUNT(*) FROM likes l WHERE l.post_id = p.id)::INT as like_count,
	       (SELECT COUNT(*) FROM comments c WHERE c.post_id = p.id)::INT as comment_count,
	       EXISTS(SELECT 1 FROM likes l WHERE l.post_id = p.id AND l.user_id = $1) as is_liked,
	       EXISTS(SELECT 1 FROM bookmarks b WHERE b.post_id = p.id AND b.user_id = $1) as is_bookmarked
	FROM posts p
	JOIN users u ON u.id = p.user_id AND u.is_active
 AND (p.audience='public' OR p.user_id=$1 OR EXISTS(SELECT 1 FROM follows WHERE follower_id=$1 AND following_id=p.user_id))
 AND NOT EXISTS(SELECT 1 FROM blocks WHERE (blocker_id=$1 AND blocked_id=u.id) OR (blocker_id=u.id AND blocked_id=$1))
 AND (u.id=$1 OR NOT COALESCE((SELECT is_private FROM profiles WHERE user_id=u.id),FALSE) OR EXISTS(SELECT 1 FROM follows WHERE follower_id=$1 AND following_id=u.id))
	LEFT JOIN profiles pr ON pr.user_id = p.user_id`

// ListFeed returns posts from people the viewer follows + own posts.
func ListFeed(ctx context.Context, viewerID string, limit, offset int) ([]Response, int64, error) {
	var total int64
	err := db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM posts p
		 WHERE p.type IN ('post','reel')
		   AND (p.user_id = $1 OR EXISTS(SELECT 1 FROM follows WHERE follower_id = $1 AND following_id = p.user_id))`,
		viewerID,
	).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	rows, err := db.Pool.Query(ctx,
		feedQuery+`
		 WHERE p.type IN ('post','reel')
		   AND (p.user_id = $1 OR EXISTS(SELECT 1 FROM follows WHERE follower_id = $1 AND following_id = p.user_id))
		 ORDER BY p.created_at DESC
		 LIMIT $2 OFFSET $3`,
		viewerID, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	return collectPosts(rows), total, nil
}

// ListByUser returns posts for a specific user's profile.
func ListByUser(ctx context.Context, targetUserID, viewerID string, postType string, limit, offset int) ([]Response, int64, error) {
	var total int64
	err := db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM posts WHERE user_id = $1 AND type = $2`, targetUserID, postType,
	).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	rows, err := db.Pool.Query(ctx,
		feedQuery+`
		 WHERE p.user_id = $2 AND p.type = $3
		 ORDER BY p.created_at DESC
		 LIMIT $4 OFFSET $5`,
		viewerID, targetUserID, postType, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	return collectPosts(rows), total, nil
}

// ListReels returns reel-type posts for the reels feed (from everyone).
func ListReels(ctx context.Context, viewerID string, limit, offset int) ([]Response, int64, error) {
	var total int64
	err := db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM posts WHERE type = 'reel'`).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	rows, err := db.Pool.Query(ctx,
		feedQuery+`
		 WHERE p.type = 'reel'
		 ORDER BY p.created_at DESC
		 LIMIT $2 OFFSET $3`,
		viewerID, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	return collectPosts(rows), total, nil
}

// ListBookmarked returns posts bookmarked by the viewer.
func ListBookmarked(ctx context.Context, viewerID string, limit, offset int) ([]Response, int64, error) {
	var total int64
	err := db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM bookmarks WHERE user_id = $1`, viewerID,
	).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	rows, err := db.Pool.Query(ctx,
		feedQuery+`
		 JOIN bookmarks bk ON bk.post_id = p.id AND bk.user_id = $1
		 ORDER BY bk.created_at DESC
		 LIMIT $2 OFFSET $3`,
		viewerID, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	return collectPosts(rows), total, nil
}

// Explore returns recent posts from all users (discovery feed).
func Explore(ctx context.Context, viewerID string, limit, offset int) ([]Response, int64, error) {
	var total int64
	err := db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM posts WHERE type IN ('post','reel')`).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	rows, err := db.Pool.Query(ctx,
		feedQuery+`
		 WHERE p.type IN ('post','reel')
		 ORDER BY p.created_at DESC
		 LIMIT $2 OFFSET $3`,
		viewerID, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	return collectPosts(rows), total, nil
}

func collectPosts(rows pgx.Rows) []Response {
	var posts []Response
	for rows.Next() {
		var r Response
		var mediaJSON []byte
		if err := rows.Scan(
			&r.ID, &r.Caption, &r.Location, &r.Type, &r.CreatedAt,
			&r.Author.ID, &r.Author.Username, &r.Author.DisplayName, &r.Author.AvatarURL,
			&mediaJSON, &r.LikeCount, &r.CommentCount, &r.IsLiked, &r.IsBookmarked,
		); err != nil {
			continue
		}
		r.Media = parseMedia(mediaJSON)
		posts = append(posts, r)
	}
	if posts == nil {
		posts = []Response{}
	}
	return posts
}

func parseMedia(data []byte) []Media {
	var media []Media
	if err := json.Unmarshal(data, &media); err != nil || media == nil {
		return []Media{}
	}
	return media
}
