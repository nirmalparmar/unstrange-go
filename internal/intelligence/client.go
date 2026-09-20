// Package intelligence keeps optional AI calls on the server, never on devices.
package intelligence

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/unstrange/backend/internal/config"
)

var Client = &http.Client{Timeout: 15 * time.Second}

func Enabled() bool { return config.C.AIKey != "" && config.C.AIModel != "" }
func request(ctx context.Context, path string, body any, result any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	base := config.C.AIBaseURL
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(base, "/")+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+config.C.AIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New("AI service unavailable")
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(result)
}

// Generate never sends phone numbers, coordinates or conversation history.
func Generate(ctx context.Context, instructions, input string) (string, error) {
	if !Enabled() {
		return "", errors.New("AI is not configured")
	}
	var result struct {
		Status string `json:"status"`
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	err := request(ctx, "/responses", map[string]any{"model": config.C.AIModel, "instructions": instructions, "input": input, "store": false, "max_output_tokens": 300}, &result)
	if err != nil {
		return "", err
	}
	if result.Status != "completed" {
		return "", errors.New("AI response was incomplete")
	}
	for _, item := range result.Output {
		if item.Type != "message" {
			continue
		}
		for _, content := range item.Content {
			if content.Type == "output_text" && strings.TrimSpace(content.Text) != "" {
				return strings.TrimSpace(content.Text), nil
			}
		}
	}
	return "", errors.New("AI returned no text")
}
func Moderate(ctx context.Context, text string) (bool, error) {
	if config.C.AIKey == "" {
		if config.C.RequireModeration {
			return false, errors.New("Moderation is not configured")
		}
		return false, nil
	}
	var result struct {
		Results []struct {
			Flagged bool `json:"flagged"`
		} `json:"results"`
	}
	err := request(ctx, "/moderations", map[string]any{"model": "omni-moderation-latest", "input": text}, &result)
	if err != nil {
		return false, err
	}
	if len(result.Results) == 0 {
		return false, errors.New("Missing moderation result")
	}
	return result.Results[0].Flagged, nil
}
