package community

import (
	"errors"
	"math"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/unstrange/backend/internal/db"
)

const planSelect = `SELECT a.*,COALESCE(u.display_name,p.username,'Your host') host_name,COALESCE(p.username,'') host_username,u.avatar_url host_avatar,
 (SELECT COUNT(*) FROM activity_participants ap WHERE ap.activity_id=a.id AND ap.status='joined') participant_count,
 COALESCE((SELECT status FROM activity_participants WHERE activity_id=a.id AND user_id=$1),'none') membership,
 (SELECT id FROM conversations WHERE activity_id=a.id) conversation_id
 FROM activities a JOIN users u ON u.id=a.creator_id LEFT JOIN profiles p ON p.user_id=u.id`
const planAccess = ` AND ` + noBlock + ` AND (a.event_id IS NULL OR EXISTS(SELECT 1 FROM event_members WHERE event_id=a.event_id AND user_id=$1))
 AND (a.visibility='public' OR a.creator_id=$1 OR EXISTS(SELECT 1 FROM follows WHERE follower_id=$1 AND following_id=a.creator_id) OR EXISTS(SELECT 1 FROM activity_participants WHERE activity_id=a.id AND user_id=$1 AND status='joined'))`

type planInput struct {
	Title       string    `json:"title" binding:"required,max=120"`
	Description string    `json:"description" binding:"max=4000"`
	Location    string    `json:"location" binding:"required,max=200"`
	CoverURL    string    `json:"cover_url"`
	Category    string    `json:"category" binding:"required,oneof=Social Sports Food Outdoors Arts Travel"`
	Visibility  string    `json:"visibility" binding:"required,oneof=public friends"`
	Approval    string    `json:"approval" binding:"required,oneof=instant approval"`
	Capacity    int       `json:"max_participants" binding:"min=2,max=500"`
	Starts      time.Time `json:"starts_at"`
	Ends        time.Time `json:"ends_at"`
	Latitude    *float64  `json:"latitude"`
	Longitude   *float64  `json:"longitude"`
	EventID     *string   `json:"event_id"`
}

func coordinates(lat, lon *float64) bool {
	return (lat == nil && lon == nil) || (lat != nil && lon != nil && !math.IsNaN(*lat) && !math.IsNaN(*lon) && *lat >= -90 && *lat <= 90 && *lon >= -180 && *lon <= 180)
}
func CreatePlan(c *gin.Context) {
	if !requireIdentity(c) {
		return
	}
	var in planInput
	if !bind(c, &in) {
		return
	}
	if trim(in.Title) == "" || trim(in.Location) == "" || !in.Starts.After(time.Now()) || !in.Ends.After(in.Starts) || !coordinates(in.Latitude, in.Longitude) {
		problem(c, 400, "Choose a future start, a later end, and a valid location")
		return
	}
	ctx := c.Request.Context()
	me := actor(c)
	if in.EventID != nil && (!validID(c, *in.EventID) || !eventMember(c, *in.EventID)) {
		return
	}
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		failure(c, err)
		return
	}
	defer tx.Rollback(ctx)
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO activities(creator_id,title,description,location,cover_url,category,visibility,approval,max_participants,starts_at,ends_at,latitude,longitude,event_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING id`, me, trim(in.Title), trim(in.Description), trim(in.Location), in.CoverURL, in.Category, in.Visibility, in.Approval, in.Capacity, in.Starts, in.Ends, in.Latitude, in.Longitude, in.EventID).Scan(&id)
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO activity_participants(activity_id,user_id,status) VALUES($1,$2,'joined')`, id, me)
	}
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO conversations(activity_id,state) VALUES($1,'accepted')`, id)
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
func ListPlans(c *gin.Context) {
	from, until, part, zone, ok := planWindow(c, 365)
	if !ok {
		return
	}
	limit := 30
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if offset < 0 {
		offset = 0
	}
	lat, lon := c.Query("lat"), c.Query("lon")
	radius, _ := strconv.ParseFloat(c.DefaultQuery("radius", "25"), 64)
	if radius <= 0 || radius > 500 {
		radius = 25
	}
	var latitude, longitude *float64
	if lat != "" || lon != "" {
		a, e1 := strconv.ParseFloat(lat, 64)
		b, e2 := strconv.ParseFloat(lon, 64)
		if e1 != nil || e2 != nil || !coordinates(&a, &b) {
			problem(c, 400, "Invalid location")
			return
		}
		latitude = &a
		longitude = &b
	}
	filter := ` WHERE a.is_active AND ($2='' OR a.category=$2) AND ($3='' OR a.title ILIKE '%'||$3||'%' OR a.location ILIKE '%'||$3||'%')
 AND ($4='' OR a.event_id::text=$4) AND ($5!='mine' OR EXISTS(SELECT 1 FROM activity_participants WHERE activity_id=a.id AND user_id=$1 AND status IN ('joined','pending')))
 AND ($5!='following' OR EXISTS(SELECT 1 FROM follows WHERE follower_id=$1 AND following_id=a.creator_id))
 AND ($5='mine' OR a.starts_at>NOW())` + planAccess + `
 AND ($6::double precision IS NULL OR (a.latitude IS NOT NULL AND 6371*2*asin(sqrt(LEAST(1.0,power(sin(radians(a.latitude-$6)/2),2)+cos(radians($6))*cos(radians(a.latitude))*power(sin(radians(a.longitude-$7)/2),2))))<=$8))
 AND ($5='mine' OR (a.starts_at >= $11 AND a.starts_at < $12))
 AND ($13='any' OR ($13='morning' AND EXTRACT(HOUR FROM (a.starts_at AT TIME ZONE 'UTC')+make_interval(mins=>$14)) BETWEEN 5 AND 11)
 OR ($13='afternoon' AND EXTRACT(HOUR FROM (a.starts_at AT TIME ZONE 'UTC')+make_interval(mins=>$14)) BETWEEN 12 AND 16)
 OR ($13='evening' AND EXTRACT(HOUR FROM (a.starts_at AT TIME ZONE 'UTC')+make_interval(mins=>$14)) BETWEEN 17 AND 23))
 ORDER BY CASE WHEN a.category=ANY(COALESCE((SELECT interests FROM profiles WHERE user_id=$1),'{}')) THEN 0 ELSE 1 END,a.starts_at LIMIT $9 OFFSET $10`
	list(c, planSelect+filter, actor(c), c.Query("category"), c.Query("q"), c.Query("event_id"), c.Query("scope"), latitude, longitude, radius, limit, offset, from, until, part, zone)
}
func GetPlan(c *gin.Context) {
	id := c.Param("id")
	if !validID(c, id) {
		return
	}
	ctx := c.Request.Context()
	me := actor(c)
	v, err := one(ctx, planSelect+` WHERE a.id=$2`+planAccess, me, id)
	if errors.Is(err, pgx.ErrNoRows) {
		problem(c, 404, "Plan not found")
		return
	}
	if err != nil {
		failure(c, err)
		return
	}
	participants, err := many(ctx, `SELECT ap.user_id,ap.status,COALESCE(u.display_name,p.username,'Member') display_name,p.username,u.avatar_url FROM activity_participants ap JOIN users u ON u.id=ap.user_id LEFT JOIN profiles p ON p.user_id=u.id WHERE ap.activity_id=$2 AND (ap.status='joined' OR $1=(SELECT creator_id FROM activities WHERE id=$2) OR ap.user_id=$1) AND `+noBlock+` ORDER BY ap.joined_at`, me, id)
	if err != nil {
		failure(c, err)
		return
	}
	v["participants"] = participants
	c.JSON(200, v)
}
func Membership(c *gin.Context) {
	id := c.Param("id")
	if !validID(c, id) {
		return
	}
	var in struct {
		Join bool `json:"join"`
	}
	if !bind(c, &in) {
		return
	}
	ctx := c.Request.Context()
	me := actor(c)
	_, err := one(ctx, planSelect+` WHERE a.id=$2`+planAccess, me, id)
	if err != nil {
		problem(c, 404, "Plan not found")
		return
	}
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		failure(c, err)
		return
	}
	defer tx.Rollback(ctx)
	var host, title, approval string
	var capacity int
	var active bool
	var starts time.Time
	err = tx.QueryRow(ctx, `SELECT creator_id,title,approval,max_participants,is_active,starts_at FROM activities WHERE id=$1 FOR UPDATE`, id).Scan(&host, &title, &approval, &capacity, &active, &starts)
	if err != nil {
		failure(c, err)
		return
	}
	if host == me {
		problem(c, 400, "Hosts can cancel the plan instead of leaving")
		return
	}
	state := "none"
	if in.Join {
		if !active || !starts.After(time.Now()) {
			problem(c, 409, "This plan is no longer accepting participants")
			return
		}
		var current string
		err = tx.QueryRow(ctx, `SELECT status FROM activity_participants WHERE activity_id=$1 AND user_id=$2`, id, me).Scan(&current)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			failure(c, err)
			return
		}
		if current == "joined" || current == "pending" {
			c.JSON(200, object{"membership": current})
			return
		}
		state = "joined"
		if approval == "approval" {
			state = "pending"
		}
		if state == "joined" {
			var count int
			err = tx.QueryRow(ctx, `SELECT COUNT(*) FROM activity_participants WHERE activity_id=$1 AND status='joined'`, id).Scan(&count)
			if err != nil {
				failure(c, err)
				return
			}
			if capacity > 0 && count >= capacity {
				problem(c, 409, "This plan is full")
				return
			}
		}
		_, err = tx.Exec(ctx, `INSERT INTO activity_participants(activity_id,user_id,status) VALUES($1,$2,$3) ON CONFLICT(activity_id,user_id) DO UPDATE SET status=$3,joined_at=NOW()`, id, me, state)
	} else {
		_, err = tx.Exec(ctx, `DELETE FROM activity_participants WHERE activity_id=$1 AND user_id=$2`, id, me)
	}
	if err == nil && in.Join {
		_, err = tx.Exec(ctx, `INSERT INTO notifications(user_id,title,body,activity_id) VALUES($1,$2,$3,$4)`, host, "A new participant", title+": "+state, id)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		failure(c, err)
		return
	}
	c.JSON(200, object{"membership": state})
}
func DecideMembership(c *gin.Context) {
	id, user := c.Param("id"), c.Param("user")
	if !validID(c, id) || !validID(c, user) {
		return
	}
	var in struct {
		Accept bool `json:"accept"`
	}
	if !bind(c, &in) {
		return
	}
	ctx := c.Request.Context()
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		failure(c, err)
		return
	}
	defer tx.Rollback(ctx)
	var host, title string
	var cap int
	var active bool
	var starts time.Time
	err = tx.QueryRow(ctx, `SELECT creator_id,title,max_participants,is_active,starts_at FROM activities WHERE id=$1 FOR UPDATE`, id).Scan(&host, &title, &cap, &active, &starts)
	if err != nil || host != actor(c) {
		problem(c, 403, "Only the host can manage requests")
		return
	}
	if !active || !starts.After(time.Now()) {
		problem(c, 409, "This plan is no longer accepting participants")
		return
	}
	if blocked(ctx, host, user) {
		problem(c, 403, "This member is unavailable")
		return
	}
	status := "declined"
	if in.Accept {
		status = "joined"
		var count int
		err = tx.QueryRow(ctx, `SELECT COUNT(*) FROM activity_participants WHERE activity_id=$1 AND status='joined'`, id).Scan(&count)
		if err != nil {
			failure(c, err)
			return
		}
		if cap > 0 && count >= cap {
			problem(c, 409, "This plan is full")
			return
		}
	}
	tag, err := tx.Exec(ctx, `UPDATE activity_participants SET status=$3 WHERE activity_id=$1 AND user_id=$2 AND status='pending'`, id, user, status)
	if err == nil && tag.RowsAffected() == 0 {
		problem(c, 409, "This request has already been handled")
		return
	}
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO notifications(user_id,title,body,activity_id) VALUES($1,$2,$3,$4)`, user, "Your join request", title+": "+status, id)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		failure(c, err)
		return
	}
	c.JSON(200, object{"membership": status})
}
func CancelPlan(c *gin.Context) {
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
	tag, err := tx.Exec(ctx, `UPDATE activities SET is_active=FALSE WHERE id=$1 AND creator_id=$2 AND is_active`, id, actor(c))
	if err != nil {
		failure(c, err)
		return
	}
	if tag.RowsAffected() == 0 {
		problem(c, 404, "Active plan not found")
		return
	}
	_, err = tx.Exec(ctx, `INSERT INTO notifications(user_id,title,body,activity_id) SELECT ap.user_id,'Plan cancelled',a.title,a.id FROM activity_participants ap JOIN activities a ON a.id=ap.activity_id WHERE a.id=$1 AND ap.user_id!=$2 AND ap.status IN ('joined','pending')`, id, actor(c))
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		failure(c, err)
		return
	}
	c.JSON(200, object{"cancelled": true})
}
func ReviewMeetup(c *gin.Context) {
	id := c.Param("id")
	if !validID(c, id) {
		return
	}
	var in struct {
		User   string `json:"user_id" binding:"required,uuid"`
		Rating int    `json:"rating" binding:"min=1,max=5"`
		Body   string `json:"body" binding:"max=1000"`
	}
	if !bind(c, &in) {
		return
	}
	if in.User == actor(c) {
		problem(c, 400, "Choose another participant")
		return
	}
	tag, err := db.Pool.Exec(c.Request.Context(), `INSERT INTO meetup_reviews(activity_id,reviewer_id,target_user_id,rating,body) SELECT a.id,$2,$3,$4,$5 FROM activities a WHERE a.id=$1 AND a.is_active AND a.ends_at<NOW() AND EXISTS(SELECT 1 FROM activity_participants WHERE activity_id=a.id AND user_id=$2 AND status='joined') AND EXISTS(SELECT 1 FROM activity_participants WHERE activity_id=a.id AND user_id=$3 AND status='joined') ON CONFLICT(activity_id,reviewer_id,target_user_id) DO UPDATE SET rating=$4,body=$5`, id, actor(c), in.User, in.Rating, trim(in.Body))
	if err != nil {
		failure(c, err)
		return
	}
	if tag.RowsAffected() == 0 {
		problem(c, 403, "Reviews open after a meetup you both attended")
		return
	}
	c.JSON(200, object{"saved": true})
}

func UserCreatedPlans(c *gin.Context) {
	id := c.Param("id")
	if !validID(c, id) {
		return
	}
	list(c, planSelect+` WHERE a.creator_id=$2`+planAccess+` ORDER BY a.starts_at DESC LIMIT 100`, actor(c), id)
}
func UserJoinedPlans(c *gin.Context) {
	id := c.Param("id")
	if !validID(c, id) {
		return
	}
	list(c, planSelect+` WHERE a.creator_id!=$2 AND EXISTS(SELECT 1 FROM activity_participants WHERE activity_id=a.id AND user_id=$2 AND status='joined')`+planAccess+` ORDER BY a.starts_at DESC LIMIT 100`, actor(c), id)
}
