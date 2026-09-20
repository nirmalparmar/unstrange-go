package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/unstrange/backend/internal/config"
	"github.com/unstrange/backend/internal/db"
)

var ErrOTPCooldown = errors.New("OTP requested too recently")
var ErrMockPhoneNotAllowed = errors.New("This preview only supports configured test phone numbers")

var otpClient = &http.Client{Timeout: 12 * time.Second}

type otpResult struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	ReqID   string `json:"reqId"`
}

func msg91(ctx context.Context, action string, payload any) (otpResult, error) {
	var result otpResult
	if config.C.MSG91AuthToken == "" || config.C.MSG91WidgetID == "" {
		return result, errors.New("SMS is not configured")
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return result, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://control.msg91.com/api/v5/widget/"+action, bytes.NewReader(data))
	if err != nil {
		return result, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("authkey", config.C.MSG91AuthToken)
	resp, err := otpClient.Do(req)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result, errors.New("SMS provider unavailable")
	}
	if err = json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 65536)).Decode(&result); err != nil {
		return result, err
	}
	if result.Type != "success" {
		return result, errors.New("The code could not be sent or verified")
	}
	return result, nil
}
func SendOTP(ctx context.Context, phone string) (string, error) {
	if config.C.OTPMode == "mock" && !config.C.AllowsMockPhone(phone) {
		return "", ErrMockPhoneNotAllowed
	}
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, phone); err != nil {
		return "", err
	}
	var recent bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM phone_otps WHERE phone=$1 AND created_at>NOW()-INTERVAL '30 seconds')`, phone).Scan(&recent)
	if err != nil {
		return "", err
	}
	if recent {
		return "", ErrOTPCooldown
	}
	reqID := uuid.NewString()
	otp := ""
	if config.C.OTPMode == "mock" {
		otp = config.C.MockOTP
	} else {
		result, e := msg91(ctx, "sendOtp", map[string]string{"identifier": phone, "widgetId": config.C.MSG91WidgetID})
		if e != nil {
			return "", e
		}
		reqID = result.ReqID
		if reqID == "" {
			return "", errors.New("SMS provider returned no request ID")
		}
	}
	_, err = tx.Exec(ctx, `DELETE FROM phone_otps WHERE phone=$1`, phone)
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO phone_otps(phone,otp,req_id,expires_at) VALUES($1,$2,$3,NOW()+INTERVAL '10 minutes')`, phone, otp, reqID)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	return reqID, err
}
func VerifyOTP(ctx context.Context, phone, otp, reqID string) error {
	if config.C.OTPMode == "mock" && !config.C.AllowsMockPhone(phone) {
		return ErrMockPhoneNotAllowed
	}
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var id, stored string
	var attempts int
	err = tx.QueryRow(ctx, `SELECT id,otp,attempts FROM phone_otps WHERE phone=$1 AND req_id=$2 AND expires_at>NOW() ORDER BY created_at DESC LIMIT 1 FOR UPDATE`, phone, reqID).Scan(&id, &stored, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("Code expired or not found. Request another code.")
	}
	if err != nil {
		return err
	}
	if attempts >= 5 {
		return errors.New("Too many attempts. Request another code.")
	}
	_, err = tx.Exec(ctx, `UPDATE phone_otps SET attempts=attempts+1 WHERE id=$1`, id)
	if err != nil {
		return err
	}
	var verification error
	if config.C.OTPMode == "mock" {
		if stored == "" || stored != otp {
			verification = errors.New("Incorrect code")
		}
	} else {
		if stored != "" {
			return errors.New("Code delivery mode changed. Request another code.")
		}
		_, verification = msg91(ctx, "verifyOtp", map[string]string{"reqId": reqID, "otp": otp})
	}
	if verification != nil {
		if err = tx.Commit(ctx); err != nil {
			return err
		}
		return verification
	}
	_, err = tx.Exec(ctx, `DELETE FROM phone_otps WHERE id=$1`, id)
	if err == nil {
		err = tx.Commit(ctx)
	}
	return err
}
