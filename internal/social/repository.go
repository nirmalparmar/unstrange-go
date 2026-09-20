package social

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/unstrange/backend/internal/db"
)

// The second parameter is always the viewer. Apply this to rows and counts.
const visibleMember = `u.is_active AND NOT EXISTS (
    SELECT 1 FROM blocks b
    WHERE (b.blocker_id=$2 AND b.blocked_id=u.id)
       OR (b.blocker_id=u.id AND b.blocked_id=$2)
)`

var ErrInvalidParent = errors.New("reply must belong to the same post")

// ── Likes ──

// ToggleLike adds or removes a like. Returns the new liked state.
func ToggleLike(ctx context.Context, userID, postID string) (liked bool, err error) {
	tag, err := db.Pool.Exec(ctx,
		`DELETE FROM likes WHERE user_id = $1 AND post_id = $2`, userID, postID)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() > 0 {
		return false, nil // was liked, now unliked
	}
	_, err = db.Pool.Exec(ctx,
		`INSERT INTO likes (user_id, post_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		userID, postID)
	return true, err
}

// ── Bookmarks ──

// ToggleBookmark adds or removes a bookmark. Returns the new bookmarked state.
func ToggleBookmark(ctx context.Context, userID, postID string) (bookmarked bool, err error) {
	tag, err := db.Pool.Exec(ctx,
		`DELETE FROM bookmarks WHERE user_id = $1 AND post_id = $2`, userID, postID)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() > 0 {
		return false, nil
	}
	_, err = db.Pool.Exec(ctx,
		`INSERT INTO bookmarks (user_id, post_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		userID, postID)
	return true, err
}

// ── Comments ──

func CreateComment(ctx context.Context, userID, postID string, req CommentRequest) (*Comment, error) {
	if req.ParentID != nil {
		var exists bool
		err := db.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM comments WHERE id=$1 AND post_id=$2)`, *req.ParentID, postID).Scan(&exists)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, ErrInvalidParent
		}
	}
	c := &Comment{}
	err := db.Pool.QueryRow(ctx,
		`INSERT INTO comments (user_id, post_id, parent_id, body)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, user_id, post_id, parent_id, body, created_at, updated_at`,
		userID, postID, req.ParentID, req.Body,
	).Scan(&c.ID, &c.UserID, &c.PostID, &c.ParentID, &c.Body, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("insert comment: %w", err)
	}
	// Fetch author info
	_ = db.Pool.QueryRow(ctx,
		`SELECT COALESCE(pr.username, ''), u.display_name, u.avatar_url
		 FROM users u LEFT JOIN profiles pr ON pr.user_id = u.id
		 WHERE u.id = $1`, userID,
	).Scan(&c.Username, &c.DisplayName, &c.AvatarURL)
	return c, nil
}

func ListComments(ctx context.Context, postID, viewerID string, limit, offset int) ([]Comment, int64, error) {
	var total int64
	err := db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM comments c JOIN users u ON u.id=c.user_id WHERE c.post_id=$1 AND `+visibleMember, postID, viewerID,
	).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	rows, err := db.Pool.Query(ctx,
		`SELECT c.id, c.user_id, c.post_id, c.parent_id, c.body, c.created_at, c.updated_at,
		        COALESCE(pr.username, ''), u.display_name, u.avatar_url,
 (SELECT COUNT(*) FROM comment_likes WHERE comment_id=c.id), EXISTS(SELECT 1 FROM comment_likes WHERE comment_id=c.id AND user_id=$2)
		 FROM comments c
		 JOIN users u ON u.id = c.user_id
		 LEFT JOIN profiles pr ON pr.user_id = c.user_id
		 WHERE c.post_id = $1 AND `+visibleMember+`
		 ORDER BY c.created_at ASC
		 LIMIT $3 OFFSET $4`,
		postID, viewerID, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	comments, err := collectComments(rows)
	return comments, total, err
}

func DeleteComment(ctx context.Context, commentID, userID string) error {
	tag, err := db.Pool.Exec(ctx,
		`DELETE FROM comments WHERE id = $1 AND user_id = $2`, commentID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("comment not found")
	}
	return nil
}

// ── Follows ──

// ToggleFollow follows or unfollows a user. Returns the new following state.
func ToggleFollow(ctx context.Context, followerID, followingID string) (following bool, err error) {
	if followerID == followingID {
		return false, fmt.Errorf("cannot follow yourself")
	}
	tag, err := db.Pool.Exec(ctx,
		`DELETE FROM follows WHERE follower_id = $1 AND following_id = $2`,
		followerID, followingID)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() > 0 {
		return false, nil
	}
	_, err = db.Pool.Exec(ctx,
		`INSERT INTO follows (follower_id, following_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		followerID, followingID)
	return true, err
}

func ListFollowers(ctx context.Context, userID, viewerID string, limit, offset int) ([]FollowUser, int64, error) {
	var total int64
	err := db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM follows f JOIN users u ON u.id=f.follower_id WHERE f.following_id=$1 AND `+visibleMember, userID, viewerID,
	).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	rows, err := db.Pool.Query(ctx,
		`SELECT u.id, COALESCE(pr.username, ''), u.display_name, u.avatar_url,
		        EXISTS(SELECT 1 FROM follows WHERE follower_id = $2 AND following_id = u.id) as is_following, EXISTS(SELECT 1 FROM follow_requests WHERE requester_id=$2 AND recipient_id=u.id)
		 FROM follows f
		 JOIN users u ON u.id = f.follower_id
		 LEFT JOIN profiles pr ON pr.user_id = u.id
		 WHERE f.following_id = $1 AND `+visibleMember+`
		 ORDER BY f.created_at DESC
		 LIMIT $3 OFFSET $4`,
		userID, viewerID, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	members, err := collectFollowUsers(rows)
	return members, total, err
}

func ListFollowing(ctx context.Context, userID, viewerID string, limit, offset int) ([]FollowUser, int64, error) {
	var total int64
	err := db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM follows f JOIN users u ON u.id=f.following_id WHERE f.follower_id=$1 AND `+visibleMember, userID, viewerID,
	).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	rows, err := db.Pool.Query(ctx,
		`SELECT u.id, COALESCE(pr.username, ''), u.display_name, u.avatar_url,
		        EXISTS(SELECT 1 FROM follows WHERE follower_id = $2 AND following_id = u.id) as is_following, EXISTS(SELECT 1 FROM follow_requests WHERE requester_id=$2 AND recipient_id=u.id)
		 FROM follows f
		 JOIN users u ON u.id = f.following_id
		 LEFT JOIN profiles pr ON pr.user_id = u.id
		 WHERE f.follower_id = $1 AND `+visibleMember+`
		 ORDER BY f.created_at DESC
		 LIMIT $3 OFFSET $4`,
		userID, viewerID, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	members, err := collectFollowUsers(rows)
	return members, total, err
}

func collectComments(rows pgx.Rows) ([]Comment, error) {
	comments := []Comment{}
	for rows.Next() {
		var c Comment
		if err := rows.Scan(
			&c.ID, &c.UserID, &c.PostID, &c.ParentID, &c.Body, &c.CreatedAt, &c.UpdatedAt,
			&c.Username, &c.DisplayName, &c.AvatarURL, &c.LikeCount, &c.IsLiked,
		); err != nil {
			return nil, err
		}
		comments = append(comments, c)
	}
	return comments, rows.Err()
}

func collectFollowUsers(rows pgx.Rows) ([]FollowUser, error) {
	users := []FollowUser{}
	for rows.Next() {
		var u FollowUser
		if err := rows.Scan(&u.UserID, &u.Username, &u.DisplayName, &u.AvatarURL, &u.IsFollowing, &u.FollowRequested); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}
