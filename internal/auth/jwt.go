package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/unstrange/backend/internal/config"
	"github.com/unstrange/backend/internal/db"
)

type Claims struct {
	UserID string `json:"user_id"`
	jwt.RegisteredClaims
}

func GenerateAccessToken(userID string) (string, error) {
	claims := Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(config.C.JWTSecret))
}

func ValidateAccessToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(config.C.JWTSecret), nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid || claims.UserID == "" {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

func GenerateRefreshToken(ctx context.Context, userID string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	expiresAt := time.Now().Add(30 * 24 * time.Hour)

	_, err := db.Pool.Exec(ctx,
		`INSERT INTO refresh_tokens (user_id, token, expires_at) VALUES ($1, $2, $3)`,
		userID, refreshTokenDigest(token), expiresAt,
	)
	return token, err
}

// Store a digest so a database read does not expose reusable credentials.
func refreshTokenDigest(token string) string {
	digest := sha256.Sum256([]byte(token))
	return "sha256:" + hex.EncodeToString(digest[:])
}

func RevokeRefreshToken(ctx context.Context, token string) error {
	_, err := db.Pool.Exec(ctx,
		`DELETE FROM refresh_tokens WHERE token = $1`, refreshTokenDigest(token),
	)
	return err
}

// ConsumeRefreshToken allows exactly one concurrent rotation of a refresh token.
func ConsumeRefreshToken(ctx context.Context, token string) (string, error) {
	var userID string
	err := db.Pool.QueryRow(ctx, `DELETE FROM refresh_tokens WHERE token=$1 AND expires_at>NOW() RETURNING user_id`, refreshTokenDigest(token)).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errors.New("invalid refresh token")
	}
	return userID, err
}
