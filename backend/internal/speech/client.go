package speech

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"cloud.google.com/go/auth/credentials"
	"cloud.google.com/go/auth/httptransport"
	"github.com/yasusudas/ai-ni-oshieru/backend/internal/jsoncheck"
)

const speechScope = "https://www.googleapis.com/auth/cloud-platform"

var (
	projectPattern  = regexp.MustCompile(`^[a-z][a-z0-9-]{4,28}[a-z0-9]$`)
	locationPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	modelPattern    = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
)

type Client struct {
	httpClient *http.Client
	endpoint   string
	recognizer string
	model      string
}

func NewClient(project, location, model string) (*Client, error) {
	if !projectPattern.MatchString(project) || !locationPattern.MatchString(location) || !modelPattern.MatchString(model) {
		return nil, ErrInternal
	}
	creds, err := credentials.DetectDefault(&credentials.DetectOptions{Scopes: []string{speechScope}})
	if err != nil {
		return nil, ErrInternal
	}
	host := "https://" + location + "-speech.googleapis.com/"
	if location == "global" {
		host = "https://speech.googleapis.com/"
	}
	discard := slog.New(slog.NewTextHandler(io.Discard, nil))
	client, err := httptransport.NewClient(&httptransport.Options{
		Credentials: creds, Endpoint: host, Logger: discard, DisableTelemetry: true,
	})
	if err != nil {
		return nil, ErrInternal
	}
	client.Timeout = 45 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{
		httpClient: client,
		endpoint:   host + "v2/projects/" + project + "/locations/" + location + "/recognizers/_:recognize",
		recognizer: "projects/" + project + "/locations/" + location + "/recognizers/_",
		model:      model,
	}, nil
}

func (c *Client) Transcribe(ctx context.Context, audio []byte) (string, error) {
	if c == nil || c.httpClient == nil || len(audio) == 0 {
		return "", ErrInternal
	}
	requestBody := struct {
		Recognizer string `json:"recognizer"`
		Config     struct {
			AutoDecodingConfig struct{} `json:"autoDecodingConfig"`
			LanguageCodes      []string `json:"languageCodes"`
			Model              string   `json:"model"`
			Features           struct {
				EnableAutomaticPunctuation bool `json:"enableAutomaticPunctuation"`
			} `json:"features"`
		} `json:"config"`
		Content []byte `json:"content"`
	}{Recognizer: c.recognizer, Content: audio}
	requestBody.Config.LanguageCodes = []string{"ja-JP"}
	requestBody.Config.Model = c.model
	requestBody.Config.Features.EnableAutomaticPunctuation = true
	body, err := json.Marshal(requestBody)
	if err != nil {
		return "", ErrInternal
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", ErrInternal
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
			return "", context.DeadlineExceeded
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		switch {
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound:
			return "", ErrInternal
		case resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusGatewayTimeout:
			return "", context.DeadlineExceeded
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
			return "", ErrUnavailable
		default:
			return "", ErrFailed
		}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20+1))
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
			return "", context.DeadlineExceeded
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", ErrFailed
	}
	if len(data) > 1<<20 || !jsoncheck.Valid(data) {
		return "", ErrFailed
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || fields == nil {
		return "", ErrFailed
	}
	results, present := fields["results"]
	if !present {
		return "", nil
	}
	if raw := bytes.TrimSpace(results); len(raw) == 0 || raw[0] != '[' {
		return "", ErrFailed
	}
	var result struct {
		Results []*struct {
			Alternatives []*struct {
				Transcript *string `json:"transcript"`
			} `json:"alternatives"`
		} `json:"results"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", ErrFailed
	}
	var text strings.Builder
	for _, item := range result.Results {
		if item == nil || len(item.Alternatives) == 0 || item.Alternatives[0] == nil || item.Alternatives[0].Transcript == nil {
			return "", ErrFailed
		}
		text.WriteString(*item.Alternatives[0].Transcript)
	}
	return strings.TrimSpace(text.String()), nil
}
