package synthesis

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"time"

	"cloud.google.com/go/auth"
	"cloud.google.com/go/auth/credentials"
	"cloud.google.com/go/auth/httptransport"
	"github.com/yasusudas/ai-ni-oshieru/backend/internal/jsoncheck"
)

const (
	maxAudio      = 2 << 20
	maxResponse   = (maxAudio+2)/3*4 + 4096
	endpoint      = "https://texttospeech.googleapis.com/v1/text:synthesize"
	cloudScope    = "https://www.googleapis.com/auth/cloud-platform"
	voiceLanguage = "ja-JP"
	voiceName     = "ja-JP-Chirp3-HD-Puck"
)

var (
	ErrInternal    = errors.New("synthesis configuration failed")
	ErrUnavailable = errors.New("synthesis unavailable")
	ErrFailed      = errors.New("synthesis failed")
)

type Synthesizer interface {
	Synthesize(context.Context, string) ([]byte, error)
}

type Client struct {
	httpClient *http.Client
}

func NewClient() (*Client, error) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return nil, ErrInternal
	}
	creds, err := credentials.DetectDefault(&credentials.DetectOptions{Scopes: []string{cloudScope}})
	if err != nil {
		return nil, ErrInternal
	}
	client, err := httptransport.NewClient(transportOptions(creds))
	if err != nil {
		return nil, ErrInternal
	}
	client.Timeout = 20 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{httpClient: client}, nil
}

func transportOptions(creds *auth.Credentials) *httptransport.Options {
	return &httptransport.Options{
		Credentials: creds, Headers: make(http.Header), Endpoint: "https://texttospeech.googleapis.com/",
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), DisableTelemetry: true,
	}
}

func (c *Client) Synthesize(ctx context.Context, text string) ([]byte, error) {
	if c == nil || c.httpClient == nil || text == "" {
		return nil, ErrInternal
	}
	requestBody := struct {
		Input struct {
			Text string `json:"text"`
		} `json:"input"`
		Voice struct {
			LanguageCode string `json:"languageCode"`
			Name         string `json:"name"`
		} `json:"voice"`
		AudioConfig struct {
			AudioEncoding string `json:"audioEncoding"`
		} `json:"audioConfig"`
	}{}
	requestBody.Input.Text = text
	requestBody.Voice.LanguageCode = voiceLanguage
	requestBody.Voice.Name = voiceName
	requestBody.AudioConfig.AudioEncoding = "MP3"
	body, err := json.Marshal(requestBody)
	if err != nil {
		return nil, ErrInternal
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, ErrInternal
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
			return nil, context.DeadlineExceeded
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		switch {
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound:
			return nil, ErrInternal
		case resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusGatewayTimeout:
			return nil, context.DeadlineExceeded
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
			return nil, ErrUnavailable
		default:
			return nil, ErrFailed
		}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, context.DeadlineExceeded
		}
		return nil, ErrFailed
	}
	if len(data) > maxResponse || !jsoncheck.Valid(data) {
		return nil, ErrFailed
	}
	var response struct {
		AudioContent *string `json:"audioContent"`
	}
	if err := json.Unmarshal(data, &response); err != nil || response.AudioContent == nil {
		return nil, ErrFailed
	}
	if len(*response.AudioContent) > (maxAudio+2)/3*4 {
		return nil, ErrFailed
	}
	audio, err := base64.StdEncoding.Strict().DecodeString(*response.AudioContent)
	if err != nil || len(audio) == 0 || len(audio) > maxAudio || !hasMP3Frames(audio) {
		return nil, ErrFailed
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-nostdin", "-v", "error", "-xerror", "-threads", "1", "-f", "mp3", "-i", "pipe:0", "-map", "0:a:0", "-f", "null", "-")
	cmd.Stdin = bytes.NewReader(audio)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, context.DeadlineExceeded
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if errors.Is(err, exec.ErrNotFound) {
			return nil, ErrInternal
		}
		return nil, ErrFailed
	}
	return audio, nil
}
