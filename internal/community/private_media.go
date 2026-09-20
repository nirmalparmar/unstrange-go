package community

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/unstrange/backend/internal/config"
	"github.com/unstrange/backend/internal/db"
)

// Private images never enter the public media bucket. Authenticated reads are
// checked against current membership or a pending reviewer assignment each time.
func mediaCipher() (cipher.AEAD, error) {
	key, err := base64.StdEncoding.DecodeString(config.C.PrivateMediaKey)
	if config.C.PrivateMediaKey == "" && config.C.Env != "production" {
		derived := sha256.Sum256([]byte("unstrange-development-private-media:" + config.C.JWTSecret))
		key = derived[:]
	}
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func UploadPrivateMedia(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 9<<20)
	file, _, err := c.Request.FormFile("file")
	if c.Request.MultipartForm != nil {
		defer c.Request.MultipartForm.RemoveAll()
	}
	if err != nil {
		problem(c, 400, "Choose a JPEG or PNG photo smaller than 8 MB")
		return
	}
	defer file.Close()
	purpose, conversation := c.PostForm("purpose"), c.PostForm("conversation_id")
	if purpose != "chat" && purpose != "verification" {
		problem(c, 400, "Invalid upload purpose")
		return
	}
	if purpose == "chat" {
		if !validID(c, conversation) {
			return
		}
		var allowed bool
		err = db.Pool.QueryRow(c.Request.Context(), `SELECT EXISTS(SELECT 1 FROM conversations cv WHERE cv.id=$2 AND cv.state='accepted' AND `+conversationAccess+`)`, actor(c), conversation).Scan(&allowed)
		if err != nil || !allowed {
			problem(c, 403, "Join the conversation before adding photos")
			return
		}
	} else if conversation != "" {
		problem(c, 400, "Identity photos cannot be attached to conversations")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(file, (8<<20)+1))
	if err != nil || len(raw) > 8<<20 {
		problem(c, 413, "Choose a photo smaller than 8 MB")
		return
	}
	dimensions, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || (format != "jpeg" && format != "png") || dimensions.Width <= 0 || dimensions.Height <= 0 || int64(dimensions.Width)*int64(dimensions.Height) > 16000000 {
		problem(c, 400, "Choose a JPEG or PNG photo up to 16 megapixels")
		return
	}
	photo, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		problem(c, 400, "This photo could not be read")
		return
	}
	// Re-encoding strips EXIF location and other unnecessary metadata.
	var clean bytes.Buffer
	if err = jpeg.Encode(&clean, photo, &jpeg.Options{Quality: 88}); err != nil {
		failure(c, err)
		return
	}
	aead, err := mediaCipher()
	if err != nil {
		failure(c, err)
		return
	}
	id := uuid.NewString()
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		failure(c, err)
		return
	}
	encrypted := aead.Seal(nonce, nonce, clean.Bytes(), []byte(id+":"+actor(c)))
	ctx := c.Request.Context()
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		failure(c, err)
		return
	}
	defer tx.Rollback(ctx)
	// Serialize per-account quota checks, including concurrent uploads.
	_, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, actor(c))
	var count int
	if err == nil {
		err = tx.QueryRow(ctx, `SELECT COUNT(*) FROM private_media WHERE owner_id=$1 AND created_at>NOW()-INTERVAL '1 day'`, actor(c)).Scan(&count)
	}
	if err != nil {
		failure(c, err)
		return
	}
	if count >= 40 {
		problem(c, 429, "Daily photo upload limit reached")
		return
	}
	_, err = tx.Exec(ctx, `INSERT INTO private_media(id,owner_id,purpose,conversation_id,ciphertext,expires_at) VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5,NOW()+INTERVAL '7 days')`, id, actor(c), purpose, conversation, encrypted)
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		failure(c, err)
		return
	}
	c.JSON(201, object{"id": id, "media_type": "image"})
}

func ReadPrivateMedia(c *gin.Context) {
	id := c.Param("asset")
	if !validID(c, id) {
		return
	}
	var owner string
	var encrypted []byte
	err := db.Pool.QueryRow(c.Request.Context(), `SELECT pm.owner_id,pm.ciphertext FROM private_media pm
 WHERE pm.id=$2 AND (pm.purpose='verification' OR NOT EXISTS(SELECT 1 FROM blocks WHERE (blocker_id=$1 AND blocked_id=pm.owner_id) OR (blocker_id=pm.owner_id AND blocked_id=$1))) AND (pm.expires_at IS NULL OR pm.expires_at>NOW()) AND (
 (pm.owner_id=$1 AND (pm.purpose='verification' OR EXISTS(SELECT 1 FROM conversations cv WHERE cv.id=pm.conversation_id AND `+conversationAccess+`)))
 OR (pm.purpose='verification' AND $3 AND EXISTS(SELECT 1 FROM verification_requests v WHERE v.user_id=pm.owner_id AND v.status='pending' AND pm.id IN(v.document_id,v.selfie_id)))
 OR (pm.purpose='chat' AND EXISTS(SELECT 1 FROM messages m JOIN conversations cv ON cv.id=m.conversation_id WHERE m.attachment_id=pm.id AND `+conversationAccess+`)))`, actor(c), id, isAdmin(actor(c))).Scan(&owner, &encrypted)
	if err != nil {
		problem(c, 404, "Photo unavailable")
		return
	}
	aead, err := mediaCipher()
	if err != nil {
		failure(c, err)
		return
	}
	if len(encrypted) < aead.NonceSize() {
		problem(c, 500, "Photo unavailable")
		return
	}
	plain, err := aead.Open(nil, encrypted[:aead.NonceSize()], encrypted[aead.NonceSize():], []byte(id+":"+owner))
	if err != nil {
		problem(c, 500, "Photo unavailable")
		return
	}
	c.Header("Cache-Control", "private, no-store, max-age=0")
	c.Header("Content-Disposition", "inline; filename=private-photo.jpg")
	c.Data(200, "image/jpeg", plain)
}
func DeletePrivateMedia(c *gin.Context) {
	id := c.Param("asset")
	if !validID(c, id) {
		return
	}
	_, err := db.Pool.Exec(c.Request.Context(), `DELETE FROM private_media pm WHERE pm.id=$1 AND pm.owner_id=$2 AND NOT EXISTS(SELECT 1 FROM messages WHERE attachment_id=pm.id) AND NOT EXISTS(SELECT 1 FROM verification_requests WHERE status='pending' AND pm.id IN(document_id,selfie_id))`, id, actor(c))
	if err != nil {
		failure(c, err)
		return
	}
	c.JSON(200, object{"deleted": true})
}
func PurgeExpiredMedia(ctx context.Context) {
	purge := func() {
		if _, err := db.Pool.Exec(ctx, `DELETE FROM private_media WHERE expires_at<NOW()`); err != nil && ctx.Err() == nil {
			log.Printf("private media retention: %v", err)
		}
	}
	purge()
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			purge()
		}
	}
}
