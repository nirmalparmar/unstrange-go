package media

import (
	"bytes"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/unstrange/backend/pkg/response"
)

const maxUploadSize = 50 << 20 // 50 MB

type UploadResponse struct {
	URL       string `json:"url"`
	MediaType string `json:"media_type"`
}

// UploadHandler handles POST /api/v1/media/upload
// Accepts multipart form with "file" field and optional "folder" field.
func UploadHandler(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUploadSize)

	file, _, err := c.Request.FormFile("file")
	if c.Request.MultipartForm != nil {
		defer c.Request.MultipartForm.RemoveAll()
	}
	if err != nil {
		response.Error(c, http.StatusBadRequest, "file is required")
		return
	}
	defer file.Close()

	head := make([]byte, 512)
	n, _ := file.Read(head)
	contentType := http.DetectContentType(head[:n])
	if contentType == "application/octet-stream" && n >= 12 && string(head[4:8]) == "ftyp" {
		switch string(head[8:12]) {
		case "heic", "heix", "hevc", "mif1":
			contentType = "image/heic"
		case "qt  ":
			contentType = "video/quicktime"
		case "isom", "iso2", "avc1", "mp41", "mp42", "M4V ", "MSNV", "dash":
			contentType = "video/mp4"
		}
	}
	if !IsAllowedType(contentType) {
		response.Error(c, http.StatusBadRequest, "unsupported file type")
		return
	}

	folder := c.DefaultPostForm("folder", "uploads")
	switch folder {
	case "uploads", "posts", "stories", "reels", "avatars", "activities", "events":
	default:
		response.Error(c, 400, "invalid upload folder")
		return
	}

	url, err := Upload(c.Request.Context(), folder, io.MultiReader(bytes.NewReader(head[:n]), file), contentType)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "upload failed")
		return
	}

	c.JSON(http.StatusOK, UploadResponse{
		URL:       url,
		MediaType: MediaTypeFromContent(contentType),
	})
}
