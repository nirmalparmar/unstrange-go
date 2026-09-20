package community

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/unstrange/backend/internal/db"
)

func eventMember(c *gin.Context, id string) bool {
	var ok bool
	err := db.Pool.QueryRow(c.Request.Context(), `SELECT EXISTS(SELECT 1 FROM event_members WHERE event_id=$1 AND user_id=$2)`, id, actor(c)).Scan(&ok)
	if err != nil {
		failure(c, err)
		return false
	}
	if !ok {
		problem(c, 403, "Join this event with its invite code first")
		return false
	}
	return true
}
func Events(c *gin.Context) {
	list(c, `SELECT e.id,e.title,e.description,e.organization,e.cover_url,e.location,e.starts_at,e.ends_at,e.owner_id,(SELECT COUNT(*) FROM event_members WHERE event_id=e.id) member_count FROM events e JOIN event_members m ON m.event_id=e.id WHERE m.user_id=$1 ORDER BY e.starts_at DESC LIMIT 100`, actor(c))
}
func CreateEvent(c *gin.Context) {
	if !requireIdentity(c) {
		return
	}
	var in struct {
		Title        string    `json:"title" binding:"required,max=120"`
		Description  string    `json:"description" binding:"max=4000"`
		Organization string    `json:"organization" binding:"max=120"`
		Location     string    `json:"location" binding:"max=200"`
		Cover        string    `json:"cover_url"`
		Starts       time.Time `json:"starts_at"`
		Ends         time.Time `json:"ends_at"`
	}
	if !bind(c, &in) {
		return
	}
	if !in.Starts.After(time.Now()) || !in.Ends.After(in.Starts) {
		problem(c, 400, "Choose a future start and a later end")
		return
	}
	ctx := c.Request.Context()
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		failure(c, err)
		return
	}
	defer tx.Rollback(ctx)
	var id, code string
	err = tx.QueryRow(ctx, `INSERT INTO events(owner_id,title,description,organization,location,cover_url,starts_at,ends_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id,invite_code`, actor(c), trim(in.Title), trim(in.Description), trim(in.Organization), trim(in.Location), in.Cover, in.Starts, in.Ends).Scan(&id, &code)
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO event_members(event_id,user_id) VALUES($1,$2)`, id, actor(c))
	}
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO conversations(event_id,state) VALUES($1,'accepted')`, id)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		failure(c, err)
		return
	}
	c.JSON(201, object{"id": id, "invite_code": code})
}
func JoinEvent(c *gin.Context) {
	var in struct {
		Code string `json:"code" binding:"required,uuid"`
	}
	if !bind(c, &in) {
		return
	}
	var id, owner string
	err := db.Pool.QueryRow(c.Request.Context(), `SELECT id,owner_id FROM events WHERE invite_code=$1 AND ends_at>NOW()`, in.Code).Scan(&id, &owner)
	if err != nil {
		problem(c, 404, "This invite is invalid or the event has ended")
		return
	}
	if blocked(c.Request.Context(), actor(c), owner) {
		problem(c, 403, "This event is unavailable")
		return
	}
	_, err = db.Pool.Exec(c.Request.Context(), `INSERT INTO event_members(event_id,user_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, id, actor(c))
	if err != nil {
		failure(c, err)
		return
	}
	c.JSON(200, object{"id": id})
}
func GetEvent(c *gin.Context) {
	id := c.Param("id")
	if !validID(c, id) || !eventMember(c, id) {
		return
	}
	ctx := c.Request.Context()
	v, err := one(ctx, `SELECT e.*,(SELECT id FROM conversations WHERE event_id=e.id) conversation_id FROM events e WHERE id=$1`, id)
	if err != nil {
		failure(c, err)
		return
	}
	members, err := many(ctx, `SELECT u.id user_id,u.display_name,u.avatar_url,p.username,p.interests,p.bio FROM event_members m JOIN users u ON u.id=m.user_id LEFT JOIN profiles p ON p.user_id=u.id WHERE m.event_id=$2 AND `+noBlock+` ORDER BY CASE WHEN p.interests && (SELECT interests FROM profiles WHERE user_id=$1) THEN 0 ELSE 1 END,m.joined_at LIMIT 200`, actor(c), id)
	if err != nil {
		failure(c, err)
		return
	}
	programme, err := many(ctx, `SELECT a.id,a.title,a.starts_at,a.ends_at,a.location FROM activities a JOIN users u ON u.id=a.creator_id WHERE a.event_id=$2 AND a.is_active AND `+noBlock+` ORDER BY a.starts_at LIMIT 200`, actor(c), id)
	if err != nil {
		failure(c, err)
		return
	}
	v["programme"] = programme
	v["members"] = members
	c.JSON(200, v)
}
func LeaveEvent(c *gin.Context) {
	id := c.Param("id")
	if !validID(c, id) {
		return
	}
	ctx := c.Request.Context()
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		failure(c, err)
		return
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `DELETE FROM event_members m USING events e WHERE m.event_id=e.id AND e.id=$1 AND m.user_id=$2 AND e.owner_id!=$2`, id, actor(c))
	if err != nil {
		failure(c, err)
		return
	}
	if tag.RowsAffected() == 0 {
		problem(c, 400, "Event hosts cannot leave their own event")
		return
	}
	_, err = tx.Exec(ctx, `DELETE FROM activity_participants ap USING activities a WHERE ap.activity_id=a.id AND a.event_id=$1 AND ap.user_id=$2`, id, actor(c))
	if err == nil {
		_, err = tx.Exec(ctx, `UPDATE activities SET is_active=FALSE WHERE event_id=$1 AND creator_id=$2`, id, actor(c))
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		failure(c, err)
		return
	}
	c.JSON(200, object{"left": true})
}
func Questions(c *gin.Context) {
	id := c.Param("id")
	if !validID(c, id) || !eventMember(c, id) {
		return
	}
	list(c, `SELECT q.*,u.display_name,u.avatar_url,host.display_name host_name,host.avatar_url host_avatar FROM event_questions q JOIN users u ON u.id=q.user_id JOIN events e ON e.id=q.event_id JOIN users host ON host.id=e.owner_id WHERE q.event_id=$2 AND `+noBlock+` ORDER BY q.created_at`, actor(c), id)
}
func AskQuestion(c *gin.Context) {
	id := c.Param("id")
	if !validID(c, id) || !eventMember(c, id) {
		return
	}
	var in struct {
		Body string `json:"body" binding:"required,max=2000"`
	}
	if !bind(c, &in) {
		return
	}
	if trim(in.Body) == "" {
		problem(c, 400, "Write a question first")
		return
	}
	_, err := db.Pool.Exec(c.Request.Context(), `INSERT INTO event_questions(event_id,user_id,body) VALUES($1,$2,$3)`, id, actor(c), trim(in.Body))
	if err != nil {
		failure(c, err)
		return
	}
	c.JSON(201, object{"sent": true})
}
func AnswerQuestion(c *gin.Context) {
	id, q := c.Param("id"), c.Param("question")
	if !validID(c, id) || !validID(c, q) {
		return
	}
	var in struct {
		Answer string `json:"answer" binding:"required,max=4000"`
	}
	if !bind(c, &in) {
		return
	}
	tag, err := db.Pool.Exec(c.Request.Context(), `UPDATE event_questions q SET answer=$4,answered_at=NOW() FROM events e WHERE q.event_id=e.id AND e.id=$1 AND q.id=$2 AND e.owner_id=$3`, id, q, actor(c), trim(in.Answer))
	if err != nil {
		failure(c, err)
		return
	}
	if tag.RowsAffected() == 0 {
		problem(c, 403, "Only the event host can answer questions")
		return
	}
	c.JSON(200, object{"saved": true})
}
