package community

import (
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/unstrange/backend/internal/db"
	"github.com/unstrange/backend/internal/post"
)

const conversationAccess = `((cv.recipient_id IS NOT NULL
 AND EXISTS(SELECT 1 FROM users WHERE id=cv.requester_id AND is_active)
 AND EXISTS(SELECT 1 FROM users WHERE id=cv.recipient_id AND is_active)
 AND $1 IN (cv.requester_id,cv.recipient_id) AND NOT EXISTS(SELECT 1 FROM blocks WHERE (blocker_id=cv.requester_id AND blocked_id=cv.recipient_id) OR (blocker_id=cv.recipient_id AND blocked_id=cv.requester_id)))
 OR (cv.activity_id IS NOT NULL AND EXISTS(SELECT 1 FROM activity_participants ap JOIN activities a ON a.id=ap.activity_id WHERE ap.activity_id=cv.activity_id AND ap.user_id=$1 AND ap.status='joined' AND a.is_active AND NOT EXISTS(SELECT 1 FROM blocks WHERE (blocker_id=$1 AND blocked_id=a.creator_id) OR (blocker_id=a.creator_id AND blocked_id=$1))))
 OR (cv.event_id IS NOT NULL AND EXISTS(SELECT 1 FROM event_members WHERE event_id=cv.event_id AND user_id=$1)))`
const conversationSelect = `SELECT cv.*,COALESCE(a.title,e.title,u.display_name,p.username,'Conversation') title,COALESCE(u.avatar_url,a.cover_url,e.cover_url) avatar_url,
 CASE WHEN cv.requester_id=$1 THEN cv.recipient_id ELSE cv.requester_id END peer_id,
 (SELECT COUNT(*) FROM messages m WHERE m.conversation_id=cv.id AND m.sender_id!=$1 AND m.id>COALESCE((SELECT last_message_id FROM conversation_reads WHERE conversation_id=cv.id AND user_id=$1),0) AND NOT EXISTS(SELECT 1 FROM blocks WHERE (blocker_id=$1 AND blocked_id=m.sender_id) OR (blocker_id=m.sender_id AND blocked_id=$1))) unread_count,
 COALESCE((SELECT last_message_id FROM conversation_reads WHERE conversation_id=cv.id AND user_id=CASE WHEN cv.requester_id=$1 THEN cv.recipient_id ELSE cv.requester_id END),0) peer_read_id,
 COALESCE((SELECT m.body FROM messages m WHERE m.conversation_id=cv.id AND NOT EXISTS(SELECT 1 FROM blocks WHERE (blocker_id=$1 AND blocked_id=m.sender_id) OR (blocker_id=m.sender_id AND blocked_id=$1)) ORDER BY m.id DESC LIMIT 1),'Say hello') last_message,
 COALESCE((SELECT m.created_at FROM messages m WHERE m.conversation_id=cv.id AND NOT EXISTS(SELECT 1 FROM blocks WHERE (blocker_id=$1 AND blocked_id=m.sender_id) OR (blocker_id=m.sender_id AND blocked_id=$1)) ORDER BY m.id DESC LIMIT 1),cv.created_at) updated_at
 FROM conversations cv LEFT JOIN activities a ON a.id=cv.activity_id LEFT JOIN events e ON e.id=cv.event_id
 LEFT JOIN users u ON u.id=CASE WHEN cv.requester_id=$1 THEN cv.recipient_id ELSE cv.requester_id END LEFT JOIN profiles p ON p.user_id=u.id`

func Conversations(c *gin.Context) {
	list(c, conversationSelect+` WHERE `+conversationAccess+` AND cv.state!='declined' ORDER BY updated_at DESC LIMIT 100`, actor(c))
}
func GetConversation(c *gin.Context) {
	if !validID(c, c.Param("id")) {
		return
	}
	detail(c, conversationSelect+` WHERE cv.id=$2 AND `+conversationAccess, actor(c), c.Param("id"))
}
func RequestConversation(c *gin.Context) {
	if !requireIdentity(c) {
		return
	}
	var in struct {
		User string `json:"user_id" binding:"required,uuid"`
	}
	if !bind(c, &in) {
		return
	}
	me := actor(c)
	ctx := c.Request.Context()
	if me == in.User || blocked(ctx, me, in.User) {
		problem(c, 403, "This person is unavailable")
		return
	}
	var exists bool
	err := db.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM profiles p JOIN users u ON u.id=p.user_id WHERE u.id=$1 AND p.is_onboarded AND u.is_active)`, in.User).Scan(&exists)
	if err != nil {
		failure(c, err)
		return
	}
	if !exists {
		problem(c, 404, "Person not found")
		return
	}
	v, err := one(ctx, `SELECT id,state FROM conversations WHERE LEAST(requester_id,recipient_id)=LEAST($1::uuid,$2::uuid) AND GREATEST(requester_id,recipient_id)=GREATEST($1::uuid,$2::uuid)`, me, in.User)
	if err == nil {
		if v["state"] == "declined" {
			problem(c, 403, "This person isn't accepting your chat request")
			return
		}
		c.JSON(200, v)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		failure(c, err)
		return
	}
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		failure(c, err)
		return
	}
	defer tx.Rollback(ctx)
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO conversations(requester_id,recipient_id) VALUES($1,$2) ON CONFLICT DO NOTHING RETURNING id`, me, in.User).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		problem(c, 409, "A conversation already exists. Refresh your inbox.")
		return
	}
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO notifications(user_id,title,body,conversation_id) VALUES($1,'New chat request','Someone would like to connect with you',$2)`, in.User, id)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		failure(c, err)
		return
	}
	c.JSON(201, object{"id": id, "state": "pending"})
}
func DecideConversation(c *gin.Context) {
	if !validID(c, c.Param("id")) {
		return
	}
	var in struct {
		Accept bool `json:"accept"`
	}
	if !bind(c, &in) {
		return
	}
	state := "declined"
	if in.Accept {
		state = "accepted"
	}
	tag, err := db.Pool.Exec(c.Request.Context(), `UPDATE conversations cv SET state=$3 WHERE cv.id=$2 AND cv.recipient_id=$1 AND cv.state='pending' AND `+conversationAccess, actor(c), c.Param("id"), state)
	if err != nil {
		failure(c, err)
		return
	}
	if tag.RowsAffected() == 0 {
		problem(c, 403, "This request is unavailable")
		return
	}
	c.JSON(200, object{"state": state})
}
func chatAllowed(c *gin.Context) bool {
	var ok bool
	err := db.Pool.QueryRow(c.Request.Context(), `SELECT EXISTS(SELECT 1 FROM conversations cv WHERE cv.id=$2 AND cv.state='accepted' AND `+conversationAccess+`)`, actor(c), c.Param("id")).Scan(&ok)
	if err != nil {
		failure(c, err)
		return false
	}
	if !ok {
		problem(c, 403, "Accept the request or join the group to chat")
		return false
	}
	return true
}
func Messages(c *gin.Context) {
	if !validID(c, c.Param("id")) || !chatAllowed(c) {
		return
	}
	after, _ := strconv.ParseInt(c.DefaultQuery("after", "0"), 10, 64)
	list(c, `SELECT m.id,m.sender_id,m.body,m.attachment_id,m.plan_id,m.post_id,m.story_id,m.client_id,m.created_at,COALESCE(u.display_name,p.username,'Member') display_name,u.avatar_url FROM messages m JOIN users u ON u.id=m.sender_id LEFT JOIN profiles p ON p.user_id=u.id WHERE m.conversation_id=$2 AND m.id>$3 AND `+noBlock+` ORDER BY m.id LIMIT 100`, actor(c), c.Param("id"), after)
}
func SendMessage(c *gin.Context) {
	if !requireIdentity(c) {
		return
	}
	if !validID(c, c.Param("id")) || !chatAllowed(c) {
		return
	}
	var in struct {
		Body       string  `json:"body" binding:"max=4000"`
		Attachment *string `json:"attachment_id" binding:"omitempty,uuid"`
		Plan       *string `json:"plan_id" binding:"omitempty,uuid"`
		Post       *string `json:"post_id" binding:"omitempty,uuid"`
		ClientID   string  `json:"client_id" binding:"required,uuid"`
	}
	if !bind(c, &in) {
		return
	}

	in.Body = trim(in.Body)
	references := 0
	for _, id := range []*string{in.Attachment, in.Plan, in.Post} {
		if id != nil {
			references++
		}
	}
	if references > 1 {
		problem(c, 400, "Choose one attachment at a time")
		return
	}
	if in.Body == "" {
		switch {
		case in.Attachment != nil:
			in.Body = "Photo"
		case in.Plan != nil:
			in.Body = "Shared a plan"
		case in.Post != nil:
			in.Body = "Shared a moment"
		default:
			problem(c, 400, "Write a message first")
			return
		}
	}
	ctx := c.Request.Context()
	if in.Plan != nil {
		if _, err := one(ctx, planSelect+` WHERE a.id=$2`+planAccess, actor(c), *in.Plan); err != nil {
			problem(c, 404, "Plan unavailable")
			return
		}
	}
	if in.Post != nil {
		if _, err := post.FindByID(ctx, *in.Post, actor(c)); err != nil {
			problem(c, 404, "Moment unavailable")
			return
		}
	}
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		failure(c, err)
		return
	}
	defer tx.Rollback(ctx)
	if in.Attachment != nil {
		var owner string
		err = tx.QueryRow(ctx, `SELECT owner_id FROM private_media WHERE id=$1 AND owner_id=$2 AND purpose='chat' AND conversation_id=$3 AND (expires_at IS NULL OR expires_at>NOW()) FOR UPDATE`, *in.Attachment, actor(c), c.Param("id")).Scan(&owner)
		if err != nil {
			problem(c, 404, "Photo unavailable; choose it again")
			return
		}
	}
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO messages(conversation_id,sender_id,body,client_id,attachment_id,plan_id,post_id) VALUES($1,$2,$3,$4,$5,$6,$7)
 ON CONFLICT(sender_id,client_id) DO UPDATE SET client_id=EXCLUDED.client_id WHERE messages.conversation_id=EXCLUDED.conversation_id AND messages.body=EXCLUDED.body AND messages.attachment_id IS NOT DISTINCT FROM EXCLUDED.attachment_id AND messages.plan_id IS NOT DISTINCT FROM EXCLUDED.plan_id AND messages.post_id IS NOT DISTINCT FROM EXCLUDED.post_id RETURNING id`, c.Param("id"), actor(c), in.Body, in.ClientID, in.Attachment, in.Plan, in.Post).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		problem(c, 409, "This request ID was already used for a different message")
		return
	}
	if err == nil && in.Attachment != nil {
		_, err = tx.Exec(ctx, `UPDATE private_media SET expires_at=NULL WHERE id=$1 AND EXISTS(SELECT 1 FROM messages WHERE id=$2 AND attachment_id=$1)`, *in.Attachment, id)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		failure(c, err)
		return
	}
	c.JSON(201, object{"id": id})
}

func ReadConversation(c *gin.Context) {
	if !validID(c, c.Param("id")) || !chatAllowed(c) {
		return
	}
	var in struct {
		Last int64 `json:"last_message_id" binding:"min=0"`
	}
	if !bind(c, &in) {
		return
	}
	// Clamp to a real message in this conversation, never an arbitrary future ID.
	_, err := db.Pool.Exec(c.Request.Context(), `INSERT INTO conversation_reads(conversation_id,user_id,last_message_id)
 SELECT $1,$2,COALESCE(MAX(id),0) FROM messages WHERE conversation_id=$1 AND id<=$3
 ON CONFLICT(conversation_id,user_id) DO UPDATE SET last_message_id=GREATEST(conversation_reads.last_message_id,EXCLUDED.last_message_id)`, c.Param("id"), actor(c), in.Last)
	if err != nil {
		failure(c, err)
		return
	}
	c.JSON(200, object{"read": true})
}

func ReplyStory(c *gin.Context) {
	if !requireIdentity(c) || !validID(c, c.Param("id")) {
		return
	}
	var in struct {
		Body   string `json:"body" binding:"required,max=4000"`
		Client string `json:"client_id" binding:"required,uuid"`
	}
	if !bind(c, &in) {
		return
	}
	if trim(in.Body) == "" {
		problem(c, 400, "Write a reply first")
		return
	}
	ctx := c.Request.Context()
	var owner string
	err := db.Pool.QueryRow(ctx, `SELECT user_id FROM stories WHERE id=$1 AND expires_at>NOW()`, c.Param("id")).Scan(&owner)
	if err != nil || owner == actor(c) || blocked(ctx, actor(c), owner) {
		problem(c, 404, "Story unavailable")
		return
	}
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		failure(c, err)
		return
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `INSERT INTO conversations(requester_id,recipient_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, actor(c), owner)
	var id, state, requester string
	if err == nil {
		err = tx.QueryRow(ctx, `SELECT id,state,requester_id FROM conversations WHERE LEAST(requester_id,recipient_id)=LEAST($1::uuid,$2::uuid) AND GREATEST(requester_id,recipient_id)=GREATEST($1::uuid,$2::uuid) FOR UPDATE`, actor(c), owner).Scan(&id, &state, &requester)
	}
	if err != nil {
		failure(c, err)
		return
	}
	if state == "declined" {
		problem(c, 403, "This person isn't accepting your replies")
		return
	}
	if state == "pending" {
		var exists bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM messages WHERE conversation_id=$1 AND client_id!=$2)`, id, in.Client).Scan(&exists)
		if err != nil {
			failure(c, err)
			return
		}
		if exists || requester != actor(c) {
			problem(c, 409, "Accept the chat request before sending another reply")
			return
		}
	}
	tag, err := tx.Exec(ctx, `INSERT INTO messages(conversation_id,sender_id,body,client_id,story_id) VALUES($1,$2,$3,$4,$5) ON CONFLICT(sender_id,client_id) DO NOTHING`, id, actor(c), trim(in.Body), in.Client, c.Param("id"))
	if err == nil && tag.RowsAffected() == 0 {
		var same bool
		err = tx.QueryRow(ctx, `SELECT conversation_id=$3 AND body=$4 AND story_id=$5 FROM messages WHERE sender_id=$1 AND client_id=$2`, actor(c), in.Client, id, trim(in.Body), c.Param("id")).Scan(&same)
		if err == nil && !same {
			problem(c, 409, "This request ID was already used for a different reply")
			return
		}
	}
	if err == nil && tag.RowsAffected() > 0 {
		_, err = tx.Exec(ctx, `INSERT INTO notifications(user_id,actor_id,title,body,conversation_id,kind) VALUES($1,$2,'Replied to your story',$3,$4,'story_reply')`, owner, actor(c), trim(in.Body), id)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		failure(c, err)
		return
	}
	c.JSON(201, object{"conversation_id": id, "state": state})
}
