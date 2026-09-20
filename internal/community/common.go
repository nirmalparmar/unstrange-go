// Package community implements the meetup, messaging and event domain.
package community

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/unstrange/backend/internal/db"
	"github.com/unstrange/backend/internal/middleware"
)

type object = map[string]any

func actor(c *gin.Context) string { return c.GetString(middleware.UserIDKey) }
func problem(c *gin.Context, code int, message string) {
	c.AbortWithStatusJSON(code, object{"error": message})
}
func failure(c *gin.Context, err error) {
	log.Printf("%s %s: %v", c.Request.Method, c.FullPath(), err)
	problem(c, 500, "Something went wrong. Please try again.")
}
func validID(c *gin.Context, id string) bool {
	if _, err := uuid.Parse(id); err != nil {
		problem(c, 400, "Invalid identifier")
		return false
	}
	return true
}
func bind(c *gin.Context, v any) bool {
	if err := c.ShouldBindJSON(v); err != nil {
		problem(c, 400, "Check the required fields and try again")
		return false
	}
	return true
}
func one(ctx context.Context, sql string, args ...any) (object, error) {
	var raw []byte
	err := db.Pool.QueryRow(ctx, "SELECT to_jsonb(result) FROM ("+sql+") result", args...).Scan(&raw)
	if err != nil {
		return nil, err
	}
	var v object
	err = json.Unmarshal(raw, &v)
	return v, err
}
func many(ctx context.Context, sql string, args ...any) ([]object, error) {
	rows, err := db.Pool.Query(ctx, "SELECT to_jsonb(result) FROM ("+sql+") result", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []object{}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var v object
		if err = json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}
func list(c *gin.Context, sql string, args ...any) {
	v, err := many(c.Request.Context(), sql, args...)
	if err != nil {
		failure(c, err)
		return
	}
	c.JSON(200, object{"items": v})
}
func detail(c *gin.Context, sql string, args ...any) {
	v, err := one(c.Request.Context(), sql, args...)
	if errors.Is(err, pgx.ErrNoRows) {
		problem(c, 404, "Not found")
		return
	}
	if err != nil {
		failure(c, err)
		return
	}
	c.JSON(200, v)
}
func notify(ctx context.Context, userID, title, body string, activityID, conversationID *string) error {
	_, err := db.Pool.Exec(ctx, `INSERT INTO notifications(user_id,title,body,activity_id,conversation_id) VALUES($1,$2,$3,$4,$5)`, userID, title, body, activityID, conversationID)
	return err
}
func blocked(ctx context.Context, a, b string) bool {
	var yes bool
	err := db.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM blocks WHERE (blocker_id=$1 AND blocked_id=$2) OR (blocker_id=$2 AND blocked_id=$1))`, a, b).Scan(&yes)
	return err != nil || yes
}
func trim(s string) string { return strings.TrimSpace(s) }

const noBlock = `NOT EXISTS(SELECT 1 FROM blocks b WHERE (b.blocker_id=$1 AND b.blocked_id=u.id) OR (b.blocker_id=u.id AND b.blocked_id=$1))`

func Register(r *gin.RouterGroup) {
	r.GET("/members/:user", MemberSummary)
	r.GET("/capabilities", Capabilities)
	r.POST("/private-media", UploadPrivateMedia)
	r.GET("/private-media/:asset", ReadPrivateMedia)
	r.DELETE("/private-media/:asset", DeletePrivateMedia)
	r.PUT("/conversations/:id/read", ReadConversation)
	r.POST("/stories/:id/reply", ReplyStory)
	r.PUT("/plans/:id", UpdatePlan)
	r.DELETE("/plans/:id/participants/:user", RemoveParticipant)
	r.GET("/follow-requests", FollowRequests)
	r.PUT("/follow-requests/:user", DecideFollow)
	r.POST("/comments/:id/like", LikeComment)
	r.GET("/admin/reports/:id/evidence", ReportEvidence)
	r.PUT("/notifications/:id/read", ReadNotification)
	r.DELETE("/events/:id/questions/:question", DeleteQuestion)
	r.GET("/suggestions/plans", Recommendations)
	r.POST("/verification", RequestVerification)
	r.GET("/admin/verifications", VerificationQueue)
	r.PUT("/admin/verifications/:user", VerifyIdentity)
	r.GET("/admin/reports", ReportsQueue)
	r.PUT("/admin/reports/:id", ResolveReport)
	r.GET("/plans", ListPlans)
	r.POST("/plans", CreatePlan)
	r.GET("/plans/:id", GetPlan)
	r.DELETE("/plans/:id", CancelPlan)
	r.PUT("/plans/:id/membership", Membership)
	r.PUT("/plans/:id/participants/:user", DecideMembership)
	r.POST("/plans/:id/reviews", ReviewMeetup)
	r.GET("/people", People)
	r.PUT("/location", SaveLocation)
	r.GET("/conversations", Conversations)
	r.POST("/conversations", RequestConversation)
	r.GET("/conversations/:id", GetConversation)
	r.PUT("/conversations/:id", DecideConversation)
	r.GET("/conversations/:id/messages", Messages)
	r.POST("/conversations/:id/messages", SendMessage)
	r.GET("/notifications", Notifications)
	r.PUT("/notifications/read", ReadNotifications)
	r.POST("/reports", Report)
	r.GET("/blocks", Blocks)
	r.PUT("/blocks/:user", Block)
	r.DELETE("/blocks/:user", Unblock)
	r.GET("/events", Events)
	r.POST("/events", CreateEvent)
	r.POST("/events/join", JoinEvent)
	r.GET("/events/:id", GetEvent)
	r.DELETE("/events/:id/membership", LeaveEvent)
	r.GET("/events/:id/questions", Questions)
	r.POST("/events/:id/questions", AskQuestion)
	r.PUT("/events/:id/questions/:question", AnswerQuestion)
	r.GET("/suggestions/icebreaker/:user", Icebreaker)
	r.DELETE("/account", DeleteAccount)
}
