package community

import (
	"github.com/gin-gonic/gin"
	"github.com/unstrange/backend/internal/db"
	"github.com/unstrange/backend/internal/post"
	"strconv"
	"time"
)

func UpdatePlan(c *gin.Context) {
	if !validID(c, c.Param("id")) {
		return
	}
	var in planInput
	if !bind(c, &in) {
		return
	}
	if trim(in.Title) == "" || trim(in.Location) == "" || !in.Starts.After(time.Now()) || !in.Ends.After(in.Starts) || !coordinates(in.Latitude, in.Longitude) {
		problem(c, 400, "Choose a future start, later end and valid location")
		return
	}
	ctx := c.Request.Context()
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		failure(c, err)
		return
	}
	defer tx.Rollback(ctx)
	var capacity int
	err = tx.QueryRow(ctx, `SELECT max_participants FROM activities WHERE id=$1 AND creator_id=$2 AND is_active AND starts_at>NOW() FOR UPDATE`, c.Param("id"), actor(c)).Scan(&capacity)
	if err != nil {
		problem(c, 403, "Only the host can edit an upcoming plan")
		return
	}
	var going int
	err = tx.QueryRow(ctx, `SELECT COUNT(*) FROM activity_participants WHERE activity_id=$1 AND status='joined'`, c.Param("id")).Scan(&going)
	if err != nil {
		failure(c, err)
		return
	}
	if in.Capacity < going {
		problem(c, 409, "Capacity cannot be smaller than the current group")
		return
	}
	_, err = tx.Exec(ctx, `UPDATE activities SET title=$3,description=$4,location=$5,cover_url=$6,category=$7,visibility=$8,approval=$9,max_participants=$10,starts_at=$11,ends_at=$12,latitude=$13,longitude=$14 WHERE id=$1 AND creator_id=$2`, c.Param("id"), actor(c), trim(in.Title), trim(in.Description), trim(in.Location), in.CoverURL, in.Category, in.Visibility, in.Approval, in.Capacity, in.Starts, in.Ends, in.Latitude, in.Longitude)
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO notifications(user_id,title,body,activity_id) SELECT user_id,'Plan updated',$2,activity_id FROM activity_participants WHERE activity_id=$1 AND user_id!=$3 AND status IN('joined','pending')`, c.Param("id"), trim(in.Title), actor(c))
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		failure(c, err)
		return
	}
	c.JSON(200, object{"id": c.Param("id")})
}
func RemoveParticipant(c *gin.Context) {
	if !validID(c, c.Param("id")) || !validID(c, c.Param("user")) {
		return
	}
	if c.Param("user") == actor(c) {
		problem(c, 400, "Hosts can cancel the plan instead")
		return
	}
	tag, err := db.Pool.Exec(c.Request.Context(), `DELETE FROM activity_participants ap USING activities a WHERE a.id=ap.activity_id AND a.id=$1 AND a.creator_id=$2 AND ap.user_id=$3 AND a.starts_at>NOW()`, c.Param("id"), actor(c), c.Param("user"))
	if err != nil {
		failure(c, err)
		return
	}
	if tag.RowsAffected() == 0 {
		problem(c, 403, "Participant unavailable")
		return
	}
	c.JSON(200, object{"removed": true})
}
func ReadNotification(c *gin.Context) {
	if !validID(c, c.Param("id")) {
		return
	}
	_, err := db.Pool.Exec(c.Request.Context(), `UPDATE notifications SET read_at=NOW() WHERE id=$1 AND user_id=$2`, c.Param("id"), actor(c))
	if err != nil {
		failure(c, err)
		return
	}
	c.JSON(200, object{"read": true})
}
func LikeComment(c *gin.Context) {
	if !validID(c, c.Param("id")) {
		return
	}
	var postID, owner string
	err := db.Pool.QueryRow(c.Request.Context(), `SELECT post_id,user_id FROM comments WHERE id=$1`, c.Param("id")).Scan(&postID, &owner)
	if err != nil || blocked(c.Request.Context(), actor(c), owner) {
		problem(c, 404, "Comment unavailable")
		return
	}
	if _, err = post.FindByID(c.Request.Context(), postID, actor(c)); err != nil {
		problem(c, 404, "Comment unavailable")
		return
	}
	tag, err := db.Pool.Exec(c.Request.Context(), `DELETE FROM comment_likes WHERE comment_id=$1 AND user_id=$2`, c.Param("id"), actor(c))
	if err != nil {
		failure(c, err)
		return
	}
	liked := tag.RowsAffected() == 0
	if liked {
		_, err = db.Pool.Exec(c.Request.Context(), `INSERT INTO comment_likes(comment_id,user_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, c.Param("id"), actor(c))
	}
	if err != nil {
		failure(c, err)
		return
	}
	c.JSON(200, object{"liked": liked})
}
func ToggleFollow(c *gin.Context) {
	target := c.Param("id")
	if !validID(c, target) {
		return
	}
	if target == actor(c) {
		problem(c, 400, "Choose another member")
		return
	}
	ctx := c.Request.Context()
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		failure(c, err)
		return
	}
	defer tx.Rollback(ctx)
	// Lock the target profile so follow requests cannot race with visibility changes.
	var private bool
	err = tx.QueryRow(ctx, `SELECT is_private FROM profiles WHERE user_id=$1 FOR UPDATE`, target).Scan(&private)
	if err != nil {
		problem(c, 404, "Member unavailable")
		return
	}
	tag, err := tx.Exec(ctx, `DELETE FROM follows WHERE follower_id=$1 AND following_id=$2`, actor(c), target)
	if err != nil {
		failure(c, err)
		return
	}
	following, requested := false, false
	if tag.RowsAffected() == 0 {
		tag, err = tx.Exec(ctx, `DELETE FROM follow_requests WHERE requester_id=$1 AND recipient_id=$2`, actor(c), target)
		if err != nil {
			failure(c, err)
			return
		}
		if tag.RowsAffected() == 0 {
			if private {
				_, err = tx.Exec(ctx, `INSERT INTO follow_requests(requester_id,recipient_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, actor(c), target)
				requested = true
				if err == nil {
					_, err = tx.Exec(ctx, `INSERT INTO notifications(user_id,actor_id,title,kind) VALUES($1,$2,'Requested to follow you','follow_request')`, target, actor(c))
				}
			} else {
				_, err = tx.Exec(ctx, `INSERT INTO follows(follower_id,following_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, actor(c), target)
				following = true
			}
		}
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		failure(c, err)
		return
	}
	c.JSON(200, object{"following": following, "requested": requested})
}
func FollowRequests(c *gin.Context) {
	list(c, `SELECT u.id user_id,u.display_name,u.avatar_url,p.username,r.created_at FROM follow_requests r JOIN users u ON u.id=r.requester_id JOIN profiles p ON p.user_id=u.id WHERE r.recipient_id=$1 AND u.is_active AND `+noBlock+` ORDER BY r.created_at`, actor(c))
}
func DecideFollow(c *gin.Context) {
	target := c.Param("user")
	if !validID(c, target) {
		return
	}
	var in struct {
		Accept bool `json:"accept"`
	}
	if !bind(c, &in) {
		return
	}
	if blocked(c.Request.Context(), actor(c), target) {
		problem(c, 403, "Member unavailable")
		return
	}
	ctx := c.Request.Context()
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		failure(c, err)
		return
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `DELETE FROM follow_requests WHERE requester_id=$1 AND recipient_id=$2`, target, actor(c))
	if err != nil {
		failure(c, err)
		return
	}
	if tag.RowsAffected() == 0 {
		problem(c, 404, "Request unavailable")
		return
	}
	if in.Accept {
		_, err = tx.Exec(ctx, `INSERT INTO follows(follower_id,following_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, target, actor(c))
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		failure(c, err)
		return
	}
	c.JSON(200, object{"accepted": in.Accept})
}
func ReportEvidence(c *gin.Context) {
	if !adminOnly(c) || !validID(c, c.Param("id")) {
		return
	}
	// Only messages supplied by a participant's report are disclosed to reviewers.
	// Other conversations belonging to the reported account are never exposed.
	list(c, `SELECT m.id,m.body,m.created_at,u.display_name FROM reports r JOIN messages m ON m.conversation_id=r.conversation_id JOIN users u ON u.id=m.sender_id WHERE r.id=$1 AND r.status='open' ORDER BY m.id DESC LIMIT 50`, c.Param("id"))
}
func DeleteQuestion(c *gin.Context) {
	if !validID(c, c.Param("id")) || !validID(c, c.Param("question")) || !eventMember(c, c.Param("id")) {
		return
	}
	tag, err := db.Pool.Exec(c.Request.Context(), `DELETE FROM event_questions q USING events e WHERE q.event_id=e.id AND e.id=$1 AND q.id=$2 AND (q.user_id=$3 OR e.owner_id=$3)`, c.Param("id"), c.Param("question"), actor(c))
	if err != nil {
		failure(c, err)
		return
	}
	if tag.RowsAffected() == 0 {
		problem(c, 403, "Question unavailable")
		return
	}
	c.JSON(200, object{"deleted": true})
}

// Date and time filters use an explicit client offset, never the server's timezone.
func planWindow(c *gin.Context, days int) (time.Time, time.Time, string, int, bool) {
	from := time.Now()
	until := from.AddDate(0, 0, days)
	var err error
	if value := c.Query("from"); value != "" {
		from, err = time.Parse(time.RFC3339, value)
		if err != nil {
			problem(c, 400, "Invalid start date")
			return from, until, "", 0, false
		}
	}
	if value := c.Query("until"); value != "" {
		until, err = time.Parse(time.RFC3339, value)
		if err != nil {
			problem(c, 400, "Invalid end date")
			return from, until, "", 0, false
		}
	}
	part := c.DefaultQuery("time", "any")
	if part != "any" && part != "morning" && part != "afternoon" && part != "evening" {
		problem(c, 400, "Invalid time filter")
		return from, until, part, 0, false
	}
	offset, err := strconv.Atoi(c.DefaultQuery("tz_offset", "0"))
	if err != nil || offset < -840 || offset > 840 || !until.After(from) || until.Sub(from) > 366*24*time.Hour {
		problem(c, 400, "Invalid date range")
		return from, until, part, offset, false
	}
	return from, until, part, offset, true
}

// MemberSummary exposes only the public identity used in safety and connection UI.
func MemberSummary(c *gin.Context) {
	id := c.Param("user")
	if !validID(c, id) {
		return
	}
	detail(c, `SELECT u.id user_id,u.display_name,u.avatar_url,p.username,p.location FROM users u JOIN profiles p ON p.user_id=u.id WHERE u.id=$2 AND u.is_active AND `+noBlock, actor(c), id)
}
