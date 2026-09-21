package media

import (
	"errors"
	"io"
	"log"
	"net/http"

	"github.com/aws/smithy-go"
	"github.com/gin-gonic/gin"
	"github.com/unstrange/backend/pkg/response"
)

const maxUploadSize = 50 << 20                         // 50 MB
const maxUploadRequestSize = maxUploadSize + (1 << 20) // Multipart headers and fields.

type UploadResponse struct {
	URL       string `json:"url"`
	MediaType string `json:"media_type"`
}

// UploadHandler handles POST /api/v1/media/upload
// Accepts multipart form with "file" field and optional "folder" field.
func UploadHandler(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUploadRequestSize)

	file, header, err := c.Request.FormFile("file")
	if c.Request.MultipartForm != nil {
		defer c.Request.MultipartForm.RemoveAll()
	}
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			response.Error(c, http.StatusRequestEntityTooLarge, "Choose a file smaller than 50 MB")
			return
		}
		response.Error(c, http.StatusBadRequest, "file is required")
		return
	}
	defer file.Close()
	if header.Size > maxUploadSize {
		response.Error(c, http.StatusRequestEntityTooLarge, "Choose a file smaller than 50 MB")
		return
	}

	head := make([]byte, 512)
	n, err := file.Read(head)
	if err != nil && err != io.EOF {
		response.Error(c, http.StatusBadRequest, "Could not read this file. Choose it again.")
		return
	}
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

	// Multipart files are seekable. Rewind after sniffing so the S3 SDK can
	// determine the size, sign the complete payload and retry failed requests.
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		response.Error(c, http.StatusBadRequest, "Could not read this file. Choose it again.")
		return
	}
	url, err := Upload(c.Request.Context(), folder, file, contentType)
	if err != nil {
		code := "storage_error"
		var storageError smithy.APIError
		if errors.As(err, &storageError) {
			code = storageError.ErrorCode()
		}
		log.Printf("public media upload failed: folder=%s code=%s", folder, code)
		response.Error(c, http.StatusServiceUnavailable, "Uploads are temporarily unavailable. Please try again shortly.")
		return
	}

	c.JSON(http.StatusOK, UploadResponse{
		URL:       url,
		MediaType: MediaTypeFromContent(contentType),
	})
}
