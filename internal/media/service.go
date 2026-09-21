package media

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"github.com/unstrange/backend/internal/config"
)

var client *s3.Client

// Derive storage names from the detected type, never from supplied filenames.
var mediaExtensions = map[string]string{
	"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp", "image/heic": ".heic",
	"video/mp4": ".mp4", "video/quicktime": ".mov", "video/webm": ".webm",
}

func Init() {
	cfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(config.C.AWSRegion),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(
				config.C.AWSAccessKeyID,
				config.C.AWSSecretAccessKey,
				"",
			),
		),
	)
	if err != nil {
		// Non-fatal: media uploads will fail but server still runs
		fmt.Printf("warning: failed to init AWS config: %v\n", err)
		return
	}
	client = s3.NewFromConfig(cfg)
}

// Upload stores a file in S3 under the given folder and returns the public URL.
// folder examples: "posts", "stories", "reels", "avatars"
func Upload(ctx context.Context, folder string, body io.ReadSeeker, contentType string) (string, error) {
	extension, allowed := mediaExtensions[contentType]
	if !allowed {
		return "", fmt.Errorf("unsupported content type")
	}
	if config.C.Env != "production" && config.C.AWSAccessKeyID == "" {
		name := uuid.NewString() + extension
		dir := filepath.Join("uploads", folder)
		if err := os.MkdirAll(dir, 0700); err != nil {
			return "", err
		}
		out, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return "", err
		}
		_, err = io.Copy(out, body)
		closeErr := out.Close()
		if err != nil {
			os.Remove(filepath.Join(dir, name))
			return "", err
		}
		if closeErr != nil {
			return "", closeErr
		}
		return "/uploads/" + folder + "/" + name, nil
	}
	if client == nil {
		return "", fmt.Errorf("s3 client not initialized")
	}

	key := fmt.Sprintf("%s/%s/%s%s",
		folder,
		time.Now().Format("2006/01"),
		uuid.New().String(),
		extension,
	)

	_, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(config.C.S3BucketName),
		Key:         aws.String(key),
		Body:        body,
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return "", fmt.Errorf("s3 upload: %w", err)
	}

	baseURL := config.C.AssetBaseURL
	if baseURL == "" {
		baseURL = fmt.Sprintf("https://%s.s3.%s.amazonaws.com",
			config.C.S3BucketName, config.C.AWSRegion)
	}
	return strings.TrimRight(baseURL, "/") + "/" + key, nil
}

// AllowedImageTypes for validation.
var AllowedImageTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/webp": true,
	"image/heic": true,
}

// AllowedVideoTypes for validation.
var AllowedVideoTypes = map[string]bool{
	"video/mp4":       true,
	"video/quicktime": true,
	"video/webm":      true,
}

func IsAllowedType(contentType string) bool {
	ct := strings.ToLower(contentType)
	return AllowedImageTypes[ct] || AllowedVideoTypes[ct]
}

func MediaTypeFromContent(contentType string) string {
	ct := strings.ToLower(contentType)
	if AllowedVideoTypes[ct] {
		return "video"
	}
	return "image"
}
