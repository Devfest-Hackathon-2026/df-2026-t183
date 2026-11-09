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

	"github.com/yasusudas/ai-ni-oshieru/backend/internal/synthesis"
)

type synthesizerFunc func(context.Context, string) ([]byte, error)

func (f synthesizerFunc) Synthesize(ctx context.Context, text string) ([]byte, error) {
	return f(ctx, text)
}

func synthesisRequest(body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/speech/synthesize", strings.NewReader(body))
	r.Header.Set("Origin", "https://allowed.example")
	r.Header.Set("Content-Type", "application/json")
	return r
}

func TestSynthesisSuccessAndBodyLimits(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		declared   int64
		status     int
		wantText   string
	}{
		{"trim and play", `{"text":"　配列とは？　"}`, -2, 200, "配列とは？"},
		{"300 codepoints", `{"text":"` + strings.Repeat("あ", 299) + `？"}`, -2, 200, strings.Repeat("あ", 299) + "？"},
		{"301 codepoints", `{"text":"` + strings.Repeat("あ", 300) + `？"}`, -2, 413, ""},
		{"exact body", `{"text":"テスト？"}` + strings.Repeat(" ", maxSynthesisBody-len(`{"text":"テスト？"}`)), -2, 200, "テスト？"},
		{"one byte over", `{"text":"テスト？"}` + strings.Repeat(" ", maxSynthesisBody-len(`{"text":"テスト？"}`)+1), -1, 413, ""},
		{"false small length", `{"text":"テスト？"}` + strings.Repeat(" ", maxSynthesisBody-len(`{"text":"テスト？"}`)+1), 1, 413, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			h := NewWithAudio(nil, []string{"https://allowed.example"}, nil, nil, synthesizerFunc(func(_ context.Context, text string) ([]byte, error) {
				calls++
				if text != tc.wantText {
					t.Errorf("synthesized %q, want %q", text, tc.wantText)
				}
				return []byte("mp3"), nil
			}))
			r := synthesisRequest(tc.body)
			if tc.declared != -2 {
				r.ContentLength = tc.declared
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status %d, want %d: %s", w.Code, tc.status, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("missing no-store")
			}
			if tc.status == 200 {
				if calls != 1 || w.Body.String() != "mp3" || w.Header().Get("Content-Type") != "audio/mpeg" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
					t.Fatalf("invalid audio response: calls=%d headers=%v body=%q", calls, w.Header(), w.Body.String())
				}
			} else if calls != 0 {
				t.Fatal("synthesizer called for oversize input")
			}
		})
	}
}

func TestSynthesisRejectsInvalidInput(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"empty", `{"text":""}`, 400},
		{"space only", `{"text":"　 \n"}`, 400},
		{"missing", `{}`, 400},
		{"null", `{"text":null}`, 400},
		{"number", `{"text":12}`, 400},
		{"array", `[]`, 400},
		{"unknown", `{"text":"質問？","voice":"other"}`, 400},
		{"duplicate", `{"text":"質問？","text":"別の質問？"}`, 400},
		{"case variant", `{"Text":"質問？"}`, 400},
		{"invalid JSON", `{"text":`, 400},
		{"invalid surrogate", `{"text":"\ud800"}`, 400},
		{"invalid UTF-8", string([]byte{'{', '"', 't', 'e', 'x', 't', '"', ':', '"', 0xff, '"', '}'}), 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewWithAudio(nil, nil, nil, nil, synthesizerFunc(func(context.Context, string) ([]byte, error) {
				t.Fatal("synthesizer called on invalid input")
				return nil, nil
			}))
			w := httptest.NewRecorder()
			r := synthesisRequest(tc.body)
			r.Header.Del("Origin")
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestSynthesisRejectsBeforeReading(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*http.Request)
		status int
	}{
		{"origin", func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }, 403},
		{"method", func(r *http.Request) { r.Method = http.MethodGet }, 405},
		{"query", func(r *http.Request) { r.URL.RawQuery = "voice=other" }, 400},
		{"content type", func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, 415},
		{"preflight", func(r *http.Request) {
			r.Method = http.MethodOptions
			r.Header.Set("Access-Control-Request-Method", "POST")
			r.Header.Set("Access-Control-Request-Headers", "Content-Type")
		}, 204},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewWithAudio(nil, []string{"https://allowed.example"}, nil, nil, nil)
			r := synthesisRequest("")
			r.Body = io.NopCloser(forbiddenRead{t})
			tc.change(r)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestSynthesisFailureContract(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result []byte
		err    error
		status int
		code   string
	}{
		{"internal", nil, synthesis.ErrInternal, 500, "INTERNAL_ERROR"},
		{"unavailable", nil, synthesis.ErrUnavailable, 503, "SYNTHESIS_UNAVAILABLE"},
		{"timeout", nil, context.DeadlineExceeded, 504, "SYNTHESIS_TIMEOUT"},
		{"failure", nil, synthesis.ErrFailed, 502, "SYNTHESIS_FAILED"},
		{"unknown", nil, errors.New("private cloud detail"), 502, "SYNTHESIS_FAILED"},
		{"empty audio", nil, nil, 502, "SYNTHESIS_FAILED"},
		{"oversize audio", bytes.Repeat([]byte{1}, 2<<20+1), nil, 502, "SYNTHESIS_FAILED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewWithAudio(nil, nil, nil, nil, synthesizerFunc(func(context.Context, string) ([]byte, error) { return tc.result, tc.err }))
			r := synthesisRequest(`{"text":"質問？"}`)
			r.Header.Del("Origin")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			var got struct {
				Error apiError `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got.Error.Code != tc.code {
				t.Fatalf("error code %q, want %q", got.Error.Code, tc.code)
			}
			if strings.Contains(w.Body.String(), "private cloud detail") || strings.Contains(w.Body.String(), "質問？") {
				t.Fatal("private detail leaked in error")
			}
		})
	}
}

func TestSynthesisConcurrencyAndCancellation(t *testing.T) {
	entered := make(chan struct{}, 2)
	h := NewWithAudio(nil, nil, nil, nil, synthesizerFunc(func(ctx context.Context, _ string) ([]byte, error) {
		entered <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{}, 2)
	for range 2 {
		go func() {
			r := synthesisRequest(`{"text":"質問？"}`).WithContext(ctx)
			r.Header.Del("Origin")
			h.ServeHTTP(httptest.NewRecorder(), r)
			done <- struct{}{}
		}()
	}
	for range 2 {
		<-entered
	}
	w := httptest.NewRecorder()
	r := synthesisRequest(`{"text":"質問？"}`)
	r.Header.Del("Origin")
	h.ServeHTTP(w, r)
	if w.Code != 503 {
		t.Fatalf("status %d", w.Code)
	}
	cancel()
	for range 2 {
		<-done
	}
	if len(h.(*Server).synthesisSlots) != 0 {
		t.Fatal("slot leaked")
	}
}
