package config

import (
	"encoding/base64"
	"fmt"
	"log"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	WeatherURL        string
	BindAddress       string
	AIKey             string
	AIModel           string
	AIBaseURL         string
	AdminUserIDs      string
	RequireModeration bool
	RequireIdentity   bool
	Port              string
	Env               string
	DatabaseURL       string
	DatabasePooledURL string
	TrustedProxies    []string

	JWTSecret       string
	PrivateMediaKey string

	// MSG91 OTP
	MSG91AuthToken string
	MSG91WidgetID  string
	// TEST_OTP is retained for older local development scripts.
	TestOTP       string
	OTPMode       string
	MockOTP       string
	MockOTPPhones []string

	AWSAccessKeyID     string
	AWSSecretAccessKey string
	AWSRegion          string
	S3BucketName       string
	AssetBaseURL       string

	AppURL      string
	FrontendURL string
}

var C Config

func Load() {
	if err := loadDotenv(); err != nil {
		log.Println("No .env file found, reading from environment")
	}

	C = Config{
		WeatherURL:        getEnv("WEATHER_API_URL", ""),
		BindAddress:       getEnv("BIND_ADDRESS", "127.0.0.1"),
		AIKey:             getEnv("OPENAI_API_KEY", ""),
		AIModel:           getEnv("AI_MODEL", ""),
		AIBaseURL:         getEnv("AI_BASE_URL", "https://api.openai.com/v1"),
		AdminUserIDs:      getEnv("ADMIN_USER_IDS", ""),
		RequireModeration: getEnv("REQUIRE_MODERATION", "false") == "true",
		RequireIdentity:   getEnv("REQUIRE_IDENTITY_VERIFICATION", "false") == "true",
		Port:              getEnv("PORT", "8080"),
		Env:               getEnv("ENV", "development"),
		DatabaseURL:       mustGetEnv("DATABASE_URL"),
		DatabasePooledURL: getEnv("DATABASE_URL_POOLED", ""),

		JWTSecret:       mustGetEnv("JWT_SECRET"),
		PrivateMediaKey: getEnv("PRIVATE_MEDIA_KEY", ""),

		MSG91AuthToken: getEnv("MSG91_AUTH_TOKEN", ""),
		MSG91WidgetID:  getEnv("MSG91_WIDGET_ID", ""),
		TestOTP:        getEnv("TEST_OTP", ""),
		OTPMode:        getEnv("OTP_MODE", ""),
		MockOTP:        getEnv("MOCK_OTP", ""),

		AWSAccessKeyID:     getEnv("AWS_ACCESS_KEY_ID", ""),
		AWSSecretAccessKey: getEnv("AWS_SECRET_ACCESS_KEY", ""),
		AWSRegion:          getEnv("AWS_REGION", "us-east-1"),
		S3BucketName:       getEnv("S3_BUCKET_NAME", "unstrange-assets"),
		AssetBaseURL:       strings.TrimRight(strings.TrimSpace(os.Getenv("ASSET_BASE_URL")), "/"),

		AppURL:      getEnv("APP_URL", "http://localhost:8080"),
		FrontendURL: getEnv("FRONTEND_URL", "http://localhost:3000"),
	}
	if C.AssetBaseURL != "" {
		assetURL, err := url.Parse(C.AssetBaseURL)
		if err != nil || assetURL.Host == "" || assetURL.User != nil || assetURL.RawQuery != "" || assetURL.ForceQuery || assetURL.Fragment != "" ||
			(assetURL.Scheme != "http" && assetURL.Scheme != "https") || (C.Env == "production" && assetURL.Scheme != "https") {
			log.Fatal("ASSET_BASE_URL must be an absolute HTTP(S) URL without credentials, query or fragment; production requires HTTPS")
		}
	}
	if err := C.configureOTP(os.Getenv("MOCK_OTP_PHONES")); err != nil {
		log.Fatal(err)
	}
	for _, proxy := range strings.Split(os.Getenv("TRUSTED_PROXIES"), ",") {
		if proxy = strings.TrimSpace(proxy); proxy != "" {
			C.TrustedProxies = append(C.TrustedProxies, proxy)
		}
	}
	if C.PrivateMediaKey != "" {
		key, err := base64.StdEncoding.DecodeString(C.PrivateMediaKey)
		if err != nil || len(key) != 32 {
			log.Fatal("PRIVATE_MEDIA_KEY must be a base64-encoded 32-byte key")
		}
	}
	if C.Env == "production" && C.PrivateMediaKey == "" {
		log.Fatal("PRIVATE_MEDIA_KEY is required in production")
	}
	if C.Env == "production" && C.TestOTP != "" {
		log.Fatal("TEST_OTP must be unset in production")
	}
	if C.Env == "production" && len(C.JWTSecret) < 32 {
		log.Fatal("JWT_SECRET must be at least 32 characters")
	}
}

// Mock delivery is explicit on deployed servers and limited to test accounts.
func (c *Config) configureOTP(phones string) error {
	if c.OTPMode == "" {
		c.OTPMode = "msg91"
		if c.Env != "production" && (c.TestOTP != "" || (c.MSG91AuthToken == "" && c.MSG91WidgetID == "")) {
			c.OTPMode = "mock"
		}
	}
	if c.OTPMode != "mock" && c.OTPMode != "msg91" {
		return fmt.Errorf("OTP_MODE must be mock or msg91")
	}
	if c.OTPMode != "mock" {
		return nil
	}
	if c.MockOTP == "" {
		c.MockOTP = c.TestOTP
		if c.MockOTP == "" {
			c.MockOTP = "123456"
		}
	}
	if !regexp.MustCompile(`^[0-9]{6}$`).MatchString(c.MockOTP) {
		return fmt.Errorf("MOCK_OTP must be exactly six digits")
	}
	for _, phone := range strings.Split(phones, ",") {
		phone = strings.TrimPrefix(strings.TrimSpace(phone), "+")
		if phone == "" {
			continue
		}
		if !regexp.MustCompile(`^[1-9][0-9]{6,14}$`).MatchString(phone) {
			return fmt.Errorf("MOCK_OTP_PHONES must contain international numbers separated by commas")
		}
		c.MockOTPPhones = append(c.MockOTPPhones, phone)
	}
	if c.Env == "production" && len(c.MockOTPPhones) == 0 {
		return fmt.Errorf("MOCK_OTP_PHONES is required for mock delivery in production; use dedicated test accounts")
	}
	return nil
}

func (c Config) AllowsMockPhone(phone string) bool {
	if c.OTPMode != "mock" {
		return false
	}
	if len(c.MockOTPPhones) == 0 {
		return c.Env != "production"
	}
	for _, allowed := range c.MockOTPPhones {
		if allowed == phone {
			return true
		}
	}
	return false
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func mustGetEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required env var %s is not set", key)
	}
	return v
}

func loadDotenv() error {
	if os.Getenv("SKIP_DOTENV") == "true" {
		return nil
	}
	return godotenv.Load()
}
