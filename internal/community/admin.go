package community

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/unstrange/backend/internal/config"
	"github.com/unstrange/backend/internal/db"
	"github.com/unstrange/backend/internal/intelligence"
)

func isAdmin(id string) bool {
	for _, admin := range strings.Split(config.C.AdminUserIDs, ",") {
		if strings.TrimSpace(admin) == id {
			return true
		}
	}
	return false
}
func adminOnly(c *gin.Context) bool {
	if !isAdmin(actor(c)) {
		problem(c, 403, "Administrator access required")
		return false
	}
	return true
}
func Capabilities(c *gin.Context) {
	var verified bool
	var status string
	_ = db.Pool.QueryRow(c.Request.Context(), `SELECT is_verified FROM users WHERE id=$1`, actor(c)).Scan(&verified)
	_ = db.Pool.QueryRow(c.Request.Context(), `SELECT status FROM verification_requests WHERE user_id=$1`, actor(c)).Scan(&status)
	var documentsAvailable bool
	_ = db.Pool.QueryRow(c.Request.Context(), `SELECT EXISTS(SELECT 1 FROM verification_requests v JOIN private_media d ON d.id=v.document_id JOIN private_media s ON s.id=v.selfie_id WHERE v.user_id=$1 AND d.expires_at>NOW() AND s.expires_at>NOW())`, actor(c)).Scan(&documentsAvailable)
	if status == "pending" && !documentsAvailable {
		status = "expired"
	}
	c.JSON(200, object{"ai": intelligence.Enabled(), "weather": config.C.WeatherURL != "", "moderation": config.C.AIKey != "", "admin": isAdmin(actor(c)), "identity_required": config.C.RequireIdentity, "verified": verified, "verification_status": status})
}
func requireIdentity(c *gin.Context) bool {
	if !config.C.RequireIdentity {
		return true
	}
	var verified bool
	err := db.Pool.QueryRow(c.Request.Context(), `SELECT is_verified FROM users WHERE id=$1`, actor(c)).Scan(&verified)
	if err != nil {
		failure(c, err)
		return false
	}
	if !verified {
		problem(c, 403, "Complete an identity check in Settings before hosting or starting a chat")
		return false
	}
	return true
}
func RequestVerification(c *gin.Context) {
	var in struct {
		Document string `json:"document_id" binding:"required,uuid"`
		Selfie   string `json:"selfie_id" binding:"required,uuid"`
		Type     string `json:"document_type" binding:"required,oneof=passport driving_licence national_id"`
		Consent  bool   `json:"consent"`
	}
	if !bind(c, &in) {
		return
	}
	if !in.Consent || in.Document == in.Selfie {
		problem(c, 400, "Add your ID and a separate selfie, then confirm consent")
		return
	}
	ctx := c.Request.Context()
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		failure(c, err)
		return
	}
	defer tx.Rollback(ctx)
	// Lock the account to prevent competing submissions and reviewer races.
	var verified bool
	err = tx.QueryRow(ctx, `SELECT is_verified FROM users WHERE id=$1 FOR UPDATE`, actor(c)).Scan(&verified)
	if err != nil {
		failure(c, err)
		return
	}
	if verified {
		problem(c, 409, "Your identity is already verified")
		return
	}
	var count int
	err = tx.QueryRow(ctx, `SELECT COUNT(*) FROM private_media WHERE id IN($1,$2) AND owner_id=$3 AND purpose='verification' AND expires_at>NOW()`, in.Document, in.Selfie, actor(c)).Scan(&count)
	if err != nil {
		failure(c, err)
		return
	}
	if count != 2 {
		problem(c, 400, "Upload both identity photos again")
		return
	}
	tag, err := tx.Exec(ctx, `INSERT INTO verification_requests(user_id,document_id,selfie_id,document_type,consent_at) VALUES($1,$2,$3,$4,NOW())
 ON CONFLICT(user_id) DO UPDATE SET document_id=$2,selfie_id=$3,document_type=$4,consent_at=NOW(),status='pending',created_at=NOW(),note='',reviewed_at=NULL,reviewed_by=NULL
 WHERE verification_requests.status='declined' OR verification_requests.document_id IS NULL OR verification_requests.selfie_id IS NULL OR NOT EXISTS(SELECT 1 FROM private_media WHERE id=verification_requests.document_id AND expires_at>NOW()) OR NOT EXISTS(SELECT 1 FROM private_media WHERE id=verification_requests.selfie_id AND expires_at>NOW())`, actor(c), in.Document, in.Selfie, in.Type)
	if err != nil {
		failure(c, err)
		return
	}
	if tag.RowsAffected() == 0 {
		problem(c, 409, "Your submission is already awaiting review")
		return
	}
	if err = tx.Commit(ctx); err != nil {
		failure(c, err)
		return
	}
	c.JSON(200, object{"requested": true})
}
func VerificationQueue(c *gin.Context) {
	if !adminOnly(c) {
		return
	}
	list(c, `SELECT v.user_id,v.status,v.created_at,v.document_id,v.selfie_id,v.document_type,u.display_name,u.avatar_url,p.username FROM verification_requests v JOIN users u ON u.id=v.user_id LEFT JOIN profiles p ON p.user_id=u.id WHERE status='pending' AND EXISTS(SELECT 1 FROM private_media WHERE id=v.document_id AND expires_at>NOW()) AND EXISTS(SELECT 1 FROM private_media WHERE id=v.selfie_id AND expires_at>NOW()) ORDER BY created_at`)
}
func VerifyIdentity(c *gin.Context) {
	if !adminOnly(c) || !validID(c, c.Param("user")) {
		return
	}
	var in struct {
		Verified bool   `json:"verified"`
		Note     string `json:"note" binding:"required,max=2000"`
	}
	if !bind(c, &in) {
		return
	}
	if c.Param("user") == actor(c) {
		problem(c, 403, "Another reviewer must verify your identity")
		return
	}
	status := "declined"
	if in.Verified {
		status = "verified"
	}
	ctx := c.Request.Context()
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		failure(c, err)
		return
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE verification_requests SET status=$2,note=$3,reviewed_by=$4,reviewed_at=NOW() WHERE user_id=$1 AND status='pending' AND (NOT $5 OR (document_id IS NOT NULL AND selfie_id IS NOT NULL AND EXISTS(SELECT 1 FROM private_media WHERE id=document_id AND expires_at>NOW()) AND EXISTS(SELECT 1 FROM private_media WHERE id=selfie_id AND expires_at>NOW())))`, c.Param("user"), status, trim(in.Note), actor(c), in.Verified)
	if err != nil {
		failure(c, err)
		return
	}
	if tag.RowsAffected() == 0 {
		problem(c, 409, "This request has already been handled")
		return
	}
	// Delete source documents at the end of review; keep only the decision audit.
	_, err = tx.Exec(ctx, `DELETE FROM private_media WHERE owner_id=$1 AND purpose='verification'`, c.Param("user"))
	if err != nil {
		failure(c, err)
		return
	}
	_, err = tx.Exec(ctx, `UPDATE users SET is_verified=$2 WHERE id=$1`, c.Param("user"), in.Verified)
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO notifications(user_id,title,body) VALUES($1,'Identity check updated',$2)`, c.Param("user"), status)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		failure(c, err)
		return
	}
	c.JSON(200, object{"status": status})
}
func ReportsQueue(c *gin.Context) {
	if !adminOnly(c) {
		return
	}
	list(c, `SELECT r.*,u.display_name target_name,u.avatar_url target_avatar,p.username target_username,reporter.display_name reporter_name,reporter.avatar_url reporter_avatar,reporter.created_at reporter_joined,po.caption reported_caption,(SELECT COALESCE(jsonb_agg(jsonb_build_object('url',url,'type',type) ORDER BY sort_order),'[]'::jsonb) FROM post_media WHERE post_id=r.post_id) reported_media FROM reports r JOIN users u ON u.id=r.target_user_id JOIN users reporter ON reporter.id=r.reporter_id LEFT JOIN profiles p ON p.user_id=u.id LEFT JOIN posts po ON po.id=r.post_id WHERE r.status='open' ORDER BY r.created_at LIMIT 100`)
}
func ResolveReport(c *gin.Context) {
	if !adminOnly(c) || !validID(c, c.Param("id")) {
		return
	}
	var in struct {
		Action string `json:"action" binding:"required,oneof=dismiss suspend"`
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
	var target string
	err = tx.QueryRow(ctx, `UPDATE reports SET status=$2 WHERE id=$1 AND status='open' RETURNING target_user_id`, c.Param("id"), in.Action).Scan(&target)
	if err != nil {
		problem(c, 409, "This report has already been handled")
		return
	}
	if target == actor(c) {
		problem(c, 403, "Another reviewer must handle this report")
		return
	}
	if in.Action == "suspend" {
		_, err = tx.Exec(ctx, `UPDATE users SET is_active=FALSE WHERE id=$1`, target)
		if err == nil {
			_, err = tx.Exec(ctx, `DELETE FROM refresh_tokens WHERE user_id=$1`, target)
		}
		if err == nil {
			_, err = tx.Exec(ctx, `UPDATE activities SET is_active=FALSE WHERE creator_id=$1`, target)
		}
	}
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO moderation_audit(reviewer_id,report_id,action) VALUES($1,$2,$3)`, actor(c), c.Param("id"), in.Action)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		failure(c, err)
		return
	}
	c.JSON(200, object{"resolved": true})
}
