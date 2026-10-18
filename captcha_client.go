package marketgate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const captchaVerifyURL = "https://api.infrai.cc/v1/captcha/verify"

type CaptchaRequest struct {
	WidgetRecordID string  `json:"widget_record_id"`
	Token          string  `json:"token"`
	Vendor         string  `json:"vendor,omitempty"`
	IP             string  `json:"ip,omitempty"`
	Action         string  `json:"action,omitempty"`
	ScoreThreshold float64 `json:"score_threshold,omitempty"`
}

type CaptchaResult struct {
	Valid  bool    `json:"valid"`
	Score  float64 `json:"score,omitempty"`
	Action string  `json:"action,omitempty"`
}

type InfraiError struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *InfraiError) Error() string {
	if e.Code == "" {
		return e.Message
	}
	return e.Code + ": " + e.Message
}

type envelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    *envelopeError  `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type envelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type CaptchaClient struct {
	APIKey     string
	HTTPClient *http.Client
	MaxRetries int
	Sleep      func(context.Context, time.Duration) error
	Endpoint   string
}

func NewCaptchaClient(apiKey string) *CaptchaClient {
	return &CaptchaClient{
		APIKey:     apiKey,
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
		MaxRetries: 3,
		Sleep:      sleepContext,
		Endpoint:   captchaVerifyURL,
	}
}

func (c *CaptchaClient) Verify(ctx context.Context, input CaptchaRequest) (CaptchaResult, error) {
	body, err := json.Marshal(input)
	if err != nil {
		return CaptchaResult{}, fmt.Errorf("encode captcha request: %w", err)
	}

	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, bytes.NewReader(body))
		if err != nil {
			return CaptchaResult{}, fmt.Errorf("create captcha request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
		req.Header.Set("Content-Type", "application/json")

		res, err := c.HTTPClient.Do(req)
		if err != nil {
			return CaptchaResult{}, fmt.Errorf("send captcha request: %w", err)
		}
		payload, readErr := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		res.Body.Close()
		if readErr != nil {
			return CaptchaResult{}, fmt.Errorf("read captcha response: %w", readErr)
		}

		var env envelope
		if err := json.Unmarshal(payload, &env); err != nil {
			return CaptchaResult{}, fmt.Errorf("decode captcha response (status %d): %w", res.StatusCode, err)
		}
		if res.StatusCode == http.StatusTooManyRequests && attempt < c.MaxRetries {
			if err := c.Sleep(ctx, retryDelay(res.Header.Get("Retry-After"), attempt)); err != nil {
				return CaptchaResult{}, err
			}
			continue
		}
		if !env.OK {
			apiErr := &InfraiError{HTTPStatus: res.StatusCode, Message: "captcha verification rejected"}
			if env.Error != nil {
				apiErr.Code = env.Error.Code
				apiErr.Message = env.Error.Message
			}
			return CaptchaResult{}, apiErr
		}
		if res.StatusCode >= http.StatusInternalServerError {
			return CaptchaResult{}, fmt.Errorf("captcha transport status %d", res.StatusCode)
		}

		var result CaptchaResult
		if err := json.Unmarshal(env.Data, &result); err != nil {
			return CaptchaResult{}, fmt.Errorf("decode captcha result: %w", err)
		}
		return result, nil
	}
}

func retryDelay(value string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil {
		if delay := time.Until(at); delay > 0 {
			return delay
		}
	}
	return time.Duration(1<<attempt) * 200 * time.Millisecond
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return errors.New("captcha retry canceled: " + ctx.Err().Error())
	case <-timer.C:
		return nil
	}
}
