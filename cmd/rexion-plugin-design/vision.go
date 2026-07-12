// vision.go — OpenAI-compatible vision API client used by design_analyze /
// design_to_code / design_compare. Only the Go standard library is used.
//
// Configuration is entirely via environment variables so secrets never land in
// tool arguments:
//
//	DESIGN_LLM_API_KEY  required; bearer token sent as "Authorization: Bearer <key>"
//	DESIGN_LLM_BASE_URL optional; defaults to https://api.openai.com/v1
//	DESIGN_LLM_MODEL    optional; defaults to gpt-4o (a widely-available vision model)
//
// CallVisionAPI sends a single user message containing one text part (the
// prompt) and one image_url part (the base64 image as a data URL). The HTTP
// timeout is 60 seconds — vision requests are slow.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	visionDefaultBaseURL = "https://api.openai.com/v1"
	visionDefaultModel   = "gpt-4o"
	visionTimeout        = 60 * time.Second
)

// visionConfig reads the vision API configuration from the environment.
type visionConfig struct {
	APIKey  string
	BaseURL string
	Model   string
}

func loadVisionConfig() (visionConfig, error) {
	cfg := visionConfig{
		APIKey:  os.Getenv("DESIGN_LLM_API_KEY"),
		BaseURL: strings.TrimRight(os.Getenv("DESIGN_LLM_BASE_URL"), "/"),
		Model:   os.Getenv("DESIGN_LLM_MODEL"),
	}
	if cfg.APIKey == "" {
		return visionConfig{}, fmt.Errorf("DESIGN_LLM_API_KEY is not set; configure it to enable design analysis")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = visionDefaultBaseURL
	}
	if cfg.Model == "" {
		cfg.Model = visionDefaultModel
	}
	return cfg, nil
}

// visionMessage mirrors the OpenAI chat-completions message shape. content is a
// slice of typed parts (text / image_url).
type visionMessage struct {
	Role    string        `json:"role"`
	Content []visionPart  `json:"content"`
}

type visionPart struct {
	Type     string       `json:"type"`
	Text     string       `json:"text,omitempty"`
	ImageURL *visionImage `json:"image_url,omitempty"`
}

type visionImage struct {
	URL string `json:"url"`
}

// visionRequest is the request body POSTed to /chat/completions.
type visionRequest struct {
	Model    string          `json:"model"`
	Messages []visionMessage `json:"messages"`
}

// visionResponse captures the subset of the chat-completions response we read.
type visionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

// CallVisionAPI sends one base64 image plus a prompt to the configured
// OpenAI-compatible vision endpoint and returns the assistant's text reply.
// base64Image may be either raw base64 or a full data URL (data:image/...;base64,...).
func CallVisionAPI(base64Image, prompt string) (string, error) {
	cfg, err := loadVisionConfig()
	if err != nil {
		return "", err
	}

	dataURL := normalizeDataURL(base64Image)

	body := visionRequest{
		Model: cfg.Model,
		Messages: []visionMessage{
			{
				Role: "user",
				Content: []visionPart{
					{Type: "text", Text: prompt},
					{Type: "image_url", ImageURL: &visionImage{URL: dataURL}},
				},
			},
		},
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal vision request: %w", err)
	}

	url := cfg.BaseURL + "/chat/completions"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("build vision request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)

	client := &http.Client{Timeout: visionTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("vision API call failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read vision response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("vision API returned status %d: %s", resp.StatusCode, truncate(string(raw), 400))
	}

	var parsed visionResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("decode vision response: %w", err)
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return "", fmt.Errorf("vision API error: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("vision API returned no choices")
	}
	return parsed.Choices[0].Message.Content, nil
}

// postVision POSTs a pre-built chat-completions payload to the configured
// vision endpoint and returns the assistant's text reply. Callers that need a
// non-standard message shape (e.g. design_compare sends two images) build their
// own body and hand it here; everything else uses CallVisionAPI.
func postVision(cfg visionConfig, payload []byte) (string, error) {
	url := cfg.BaseURL + "/chat/completions"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("build vision request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)

	client := &http.Client{Timeout: visionTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("vision API call failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read vision response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("vision API returned status %d: %s", resp.StatusCode, truncate(string(raw), 400))
	}

	var parsed visionResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("decode vision response: %w", err)
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return "", fmt.Errorf("vision API error: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("vision API returned no choices")
	}
	return parsed.Choices[0].Message.Content, nil
}

// normalizeDataURL ensures the base64 image is a full data URL consumable by the
// OpenAI vision endpoint. If raw already starts with "data:" it is returned
// unchanged; otherwise a PNG data URL prefix is prepended (PNG is a safe default
// since design_upload normalizes uploads to .png).
func normalizeDataURL(raw string) string {
	if strings.HasPrefix(raw, "data:") {
		return raw
	}
	return "data:image/png;base64," + raw
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
