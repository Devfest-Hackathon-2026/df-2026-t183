package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yasusudas/ai-ni-oshieru/backend/internal/speech"
)

type validatorFunc func(context.Context, []byte, string) error

func (f validatorFunc) Validate(c context.Context, b []byte, s string) error { return f(c, b, s) }

type transcriberFunc func(context.Context, []byte) (string, error)

func (f transcriberFunc) Transcribe(c context.Context, b []byte) (string, error) { return f(c, b) }

func speechRequest(body io.Reader) *http.Request {
	r := httptest.NewRequest("POST", "/api/speech/transcribe", body)
	r.Header.Set("Content-Type", "audio/webm;codecs=opus")
	r.Header.Set("Origin", "https://allowed.example")
	return r
}
func speechCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var response struct {
		Error apiError `json:"error"`
	}
	if json.Unmarshal(w.Body.Bytes(), &response) != nil {
		t.Fatal("invalid error JSON")
	}
	return response.Error.Code
}
func TestSpeechBodyBoundary(t *testing.T) {
	for _, tc := range []struct {
		name     string
		size     int
		declared int64
		status   int
	}{
		{"exact", speech.MaxBody, speech.MaxBody, 200}, {"one over", speech.MaxBody + 1, speech.MaxBody + 1, 413},
		{"unknown exact", speech.MaxBody, -1, 200}, {"unknown over", speech.MaxBody + 1, -1, 413},
		{"false small", speech.MaxBody + 1, 1, 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			h := NewWithSpeech(nil, []string{"https://allowed.example"}, validatorFunc(func(_ context.Context, b []byte, _ string) error {
				if len(b) != speech.MaxBody {
					t.Fatal("wrong body")
				}
				calls++
				return nil
			}), transcriberFunc(func(context.Context, []byte) (string, error) { calls++; return "テスト", nil }))
			r := speechRequest(bytes.NewReader(make([]byte, tc.size)))
			r.ContentLength = tc.declared
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("got %d want %d: %s", w.Code, tc.status, w.Body)
			}
			if tc.status == 413 && calls != 0 {
				t.Fatal("oversize reached speech")
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("missing no-store")
			}
		})
	}
}
func TestSpeechErrorContract(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		validation, errorValue error
		text                   string
		status                 int
		code                   string
	}{
		{"unknown internal", errors.New("private details"), nil, "", 500, "INTERNAL_ERROR"},
		{"invalid", speech.ErrInvalid, nil, "", 400, "INVALID_INPUT"}, {"too long", speech.ErrTooLarge, nil, "", 413, "INPUT_TOO_LARGE"},
		{"unsupported", speech.ErrUnsupported, nil, "", 415, "UNSUPPORTED_MEDIA_TYPE"}, {"internal", speech.ErrInternal, nil, "", 500, "INTERNAL_ERROR"},
		{"failed", nil, speech.ErrFailed, "", 502, "TRANSCRIPTION_FAILED"}, {"unavailable", nil, speech.ErrUnavailable, "", 503, "TRANSCRIPTION_UNAVAILABLE"},
		{"timeout", nil, context.DeadlineExceeded, "", 504, "TRANSCRIPTION_TIMEOUT"}, {"silence", nil, nil, "　 \n", 400, "INVALID_INPUT"},
		{"1000 codepoints", nil, nil, strings.Repeat("😀", 1000), 200, ""}, {"1001 codepoints", nil, nil, strings.Repeat("😀", 1001), 502, "TRANSCRIPTION_FAILED"},
		{"invalid utf8", nil, nil, "\xff", 502, "TRANSCRIPTION_FAILED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			h := NewWithSpeech(nil, []string{"https://allowed.example"}, validatorFunc(func(context.Context, []byte, string) error { return tc.validation }), transcriberFunc(func(context.Context, []byte) (string, error) { calls++; return tc.text, tc.errorValue }))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, speechRequest(strings.NewReader("synthetic")))
			if w.Code != tc.status {
				t.Fatalf("got %d want %d: %s", w.Code, tc.status, w.Body)
			}
			if tc.code != "" && speechCode(t, w) != tc.code {
				t.Fatal("wrong code")
			}
			if tc.validation != nil && calls != 0 {
				t.Fatal("transcriber called on invalid audio")
			}
		})
	}
}

type forbiddenRead struct{ t *testing.T }

func (r forbiddenRead) Read([]byte) (int, error) {
	r.t.Fatal("body must not be read")
	return 0, errors.New("unexpected read")
}
func TestSpeechRejectsBeforeReading(t *testing.T) {
	cases := []struct {
		name   string
		change func(*http.Request)
		status int
	}{
		{"origin", func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }, 403},
		{"method", func(r *http.Request) { r.Method = "GET" }, 405},
		{"query", func(r *http.Request) { r.URL.RawQuery = "model=other" }, 400},
		{"missing type", func(r *http.Request) { r.Header.Del("Content-Type") }, 415},
		{"JSON", func(r *http.Request) { r.Header.Set("Content-Type", "application/json") }, 415},
		{"wrong codec", func(r *http.Request) { r.Header.Set("Content-Type", "audio/webm;codecs=vorbis") }, 415},
		{"extra parameter", func(r *http.Request) { r.Header.Set("Content-Type", "audio/webm;charset=utf-8") }, 415},
		{"duplicate type", func(r *http.Request) { r.Header.Add("Content-Type", "audio/ogg") }, 415},
		{"compression", func(r *http.Request) { r.Header.Set("Content-Encoding", "gzip") }, 415},
		{"preflight", func(r *http.Request) {
			r.Method = "OPTIONS"
			r.Header.Set("Access-Control-Request-Method", "POST")
			r.Header.Set("Access-Control-Request-Headers", "Content-Type")
		}, 204},
		{"bad preflight", func(r *http.Request) { r.Method = "OPTIONS"; r.Header.Set("Access-Control-Request-Method", "DELETE") }, 403},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := NewWithSpeech(nil, []string{"https://allowed.example"}, validatorFunc(func(context.Context, []byte, string) error { t.Fatal("validator called"); return nil }), transcriberFunc(func(context.Context, []byte) (string, error) { t.Fatal("transcriber called"); return "", nil }))
			r := speechRequest(forbiddenRead{t})
			tc.change(r)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("got %d want %d: %s", w.Code, tc.status, w.Body)
			}
		})
	}
}
func TestSpeechConcurrencyAndCancellation(t *testing.T) {
	entered := make(chan struct{}, 2)
	h := NewWithSpeech(nil, []string{"https://allowed.example"}, validatorFunc(func(ctx context.Context, _ []byte, _ string) error {
		entered <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}), transcriberFunc(func(context.Context, []byte) (string, error) {
		t.Error("transcriber after cancellation")
		return "", nil
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{}, 2)
	for range 2 {
		go func() {
			h.ServeHTTP(httptest.NewRecorder(), speechRequest(strings.NewReader("synthetic")).WithContext(ctx))
			done <- struct{}{}
		}()
	}
	for range 2 {
		<-entered
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, speechRequest(forbiddenRead{t}))
	if w.Code != 503 {
		t.Fatalf("got %d", w.Code)
	}
	cancel()
	for range 2 {
		<-done
	}
	if len(h.(*Server).speechSlots) != 0 {
		t.Fatal("slot not released")
	}
}
