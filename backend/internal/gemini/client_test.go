package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yasusudas/ai-ni-oshieru/backend/internal/learning"
	"google.golang.org/genai"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDecodeSummaryRejectsMalformedOutput(t *testing.T) {
	cases := []string{
		`{"understood":[],"review":[],"review":[]}`,
		`{"Understood":[],"review":[]}`,
		`{"understood":[{"Point":"x","turnIndex":1,"quote":"aaaaa"}],"review":[]}`,
		`{"understood":null,"review":[]}`,
		`{"understood":[],"review":[]} {}`,
	}
	for _, text := range cases {
		if _, err := decodeSummary(text); err == nil {
			t.Errorf("accepted %s", text)
		}
	}
	if _, err := decodeSummary(`{"understood":[],"review":[]}`); err != nil {
		t.Fatal(err)
	}
}

func TestVertexRequestAndStructuredResponse(t *testing.T) {
	t.Setenv("GOOGLE_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	var bodies []map[string]any
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		if !strings.Contains(r.URL.Path, "generateContent") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return nil, err
		}
		var body map[string]any
		if err := json.Unmarshal(data, &body); err != nil {
			t.Error(err)
			return nil, err
		}
		bodies = append(bodies, body)
		if len(bodies) == 4 {
			w.WriteHeader(http.StatusGatewayTimeout)
			_, _ = io.WriteString(w, `{"error":{"code":504,"status":"DEADLINE_EXCEEDED"}}`)
			return w.Result(), nil
		}
		text := "考えを説明できますか？"
		if len(bodies) == 3 {
			text = `{"understood":[],"review":[]}`
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"candidates": []any{map[string]any{"finishReason": "STOP", "content": map[string]any{"role": "model", "parts": []any{map[string]any{"text": text}}}}}})
		return w.Result(), nil
	})}
	sdk, err := genai.NewClient(context.Background(), &genai.ClientConfig{Backend: genai.BackendVertexAI, Project: "test-project", Location: "test-location", HTTPClient: client, HTTPOptions: genai.HTTPOptions{BaseURL: "https://example.test"}})
	if err != nil {
		t.Fatal(err)
	}
	c := &Client{client: sdk, model: "test-model", maxTokens: 2048}
	history := []learning.Turn{{Role: "model", Content: "何ですか？"}, {Role: "user", Content: "説明です。"}, {Role: "model", Content: "なぜですか？"}}
	if _, err := c.Start(context.Background(), "教材"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Chat(context.Background(), "教材", history, "回答"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Summary(context.Background(), "教材", history); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Start(context.Background(), "教材"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Vertex 504: %v", err)
	}
	for _, body := range bodies {
		if body["systemInstruction"] == nil {
			t.Error("missing system instruction")
		}
	}
	contents, ok := bodies[1]["contents"].([]any)
	if !ok || len(contents) != 5 {
		t.Fatalf("chat contents: %#v", bodies[1]["contents"])
	}
	for i, role := range []string{"user", "model", "user", "model", "user"} {
		if contents[i].(map[string]any)["role"] != role {
			t.Errorf("role %d: %#v", i, contents[i])
		}
	}
	generation, ok := bodies[2]["generationConfig"].(map[string]any)
	if !ok || generation["responseMimeType"] != "application/json" || generation["responseSchema"] == nil {
		t.Errorf("missing schema: %#v", bodies[2])
	}
}
