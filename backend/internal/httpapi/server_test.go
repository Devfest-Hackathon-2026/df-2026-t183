package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yasusudas/ai-ni-oshieru/backend/internal/learning"
)

type fakeGenerator struct {
	calls   int
	reply   string
	err     error
	raw     learning.RawSummary
	history []learning.Turn
	message string
}

func (g *fakeGenerator) Start(ctx context.Context, content string) (string, error) {
	g.calls++
	return g.reply, g.err
}
func (g *fakeGenerator) Chat(ctx context.Context, content string, h []learning.Turn, message string) (string, error) {
	g.calls++
	g.history = h
	g.message = message
	return g.reply, g.err
}
func (g *fakeGenerator) Summary(ctx context.Context, content string, h []learning.Turn) (learning.RawSummary, error) {
	g.calls++
	return g.raw, g.err
}

func requestTest(h http.Handler, method, path, body, origin, ctype string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if ctype != "" {
		r.Header.Set("Content-Type", ctype)
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestInvalidInputNeverCallsGemini(t *testing.T) {
	cases := []struct {
		name, path, body string
		status           int
	}{
		{"empty", "/api/start", `{"learningContent":"  \n "}`, 400},
		{"null", "/api/start", `{"learningContent":null}`, 400},
		{"case", "/api/start", `{"LearningContent":"abc"}`, 400},
		{"duplicate", "/api/start", `{"learningContent":"abc","learningContent":"def"}`, 400},
		{"nested duplicate", "/api/chat", `{"sessionId":"2417ac2f-3cd3-4a18-8dd9-82a0205e5912","learningContent":"a","history":[{"role":"model","role":"user","content":"a"}],"message":"a"}`, 400},
		{"two values", "/api/start", `{"learningContent":"a"}{}`, 400},
		{"invalid utf8", "/api/start", "{\"learningContent\":\"\xff\"}", 400},
		{"unpaired surrogate", "/api/start", `{"learningContent":"\ud800"}`, 400},
		{"too long", "/api/start", `{"learningContent":"` + strings.Repeat("あ", 10001) + `"}`, 413},
		{"body too large", "/api/start", `{"learningContent":"` + strings.Repeat("a", maxBody) + `"}`, 413},
		{"no user", "/api/summary", `{"sessionId":"2417ac2f-3cd3-4a18-8dd9-82a0205e5912","learningContent":"a","history":[{"role":"model","content":"a"}]}`, 400},
		{"wrong role", "/api/chat", `{"sessionId":"2417ac2f-3cd3-4a18-8dd9-82a0205e5912","learningContent":"a","history":[{"role":"user","content":"a"}],"message":"b"}`, 400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := &fakeGenerator{}
			h := New(g, []string{"http://localhost:5173"})
			w := requestTest(h, "POST", tc.path, tc.body, "", "application/json")
			if w.Code != tc.status || g.calls != 0 {
				t.Fatalf("status %d calls %d body %s", w.Code, g.calls, w.Body.String())
			}
		})
	}
}

func TestUnicodeAndHistoryLimits(t *testing.T) {
	g := &fakeGenerator{reply: "説明できますか？"}
	h := New(g, nil)
	body := `{"learningContent":"` + strings.Repeat("😀", 10000) + `"}`
	w := requestTest(h, "POST", "/api/start", body, "", "application/json")
	if w.Code != 200 {
		t.Fatalf("10000 code points: %d %s", w.Code, w.Body.String())
	}
	for _, count := range []int{39, 40} {
		history := make([]learning.Turn, count)
		for i := range history {
			role := "model"
			if i%2 == 1 {
				role = "user"
			}
			history[i] = learning.Turn{Role: role, Content: "発言"}
		}
		data, err := json.Marshal(map[string]any{"sessionId": "2417ac2f-3cd3-4a18-8dd9-82a0205e5912", "learningContent": "教材", "history": history, "message": "回答"})
		if err != nil {
			t.Fatal(err)
		}
		w = requestTest(h, "POST", "/api/chat", string(data), "", "application/json")
		want := 200
		if count == 40 {
			want = 413
		}
		if w.Code != want {
			t.Fatalf("history %d: %d", count, w.Code)
		}
	}
}

func TestSummaryResponse(t *testing.T) {
	str := func(s string) *string { return &s }
	i := 1
	u := []learning.RawUnderstood{{Point: str("順番にまとめる"), TurnIndex: &i, Quote: str("順番にまとめるものです")}}
	r := []learning.RawReview{{Kind: str("unverified"), Text: str("取り出し方は未確認")}}
	g := &fakeGenerator{raw: learning.RawSummary{Understood: &u, Review: &r}}
	h := New(g, nil)
	body := `{"sessionId":"2417ac2f-3cd3-4a18-8dd9-82a0205e5912","learningContent":"教材","history":[{"role":"model","content":"説明して"},{"role":"user","content":"順番にまとめるものです"}]}`
	w := requestTest(h, "POST", "/api/summary", body, "", "application/json")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"evidence":"順番にまとめるものです"`) || strings.Contains(w.Body.String(), `"saved"`) {
		t.Fatalf("summary: %d %s", w.Code, w.Body.String())
	}
	g.raw.Understood = nil
	w = requestTest(h, "POST", "/api/summary", body, "", "application/json")
	if w.Code != 502 {
		t.Fatalf("invalid model output: %d", w.Code)
	}
}

func TestStartChatAndFailure(t *testing.T) {
	g := &fakeGenerator{reply: " 説明できますか？ "}
	h := New(g, []string{"http://localhost:5173"})
	w := requestTest(h, "POST", "/api/start", `{"learningContent":"配列です。"}`, "http://localhost:5173", "application/json")
	var result map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || w.Code != 200 || !uuidPattern.MatchString(result["sessionId"]) || result["reply"] != "説明できますか？" {
		t.Fatalf("unexpected start: %d %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Fatal("missing headers")
	}
	body := `{"sessionId":"` + result["sessionId"] + `","learningContent":"配列","history":[{"role":"model","content":"何ですか？"}],"message":"値を並べます"}`
	w = requestTest(h, "POST", "/api/chat", body, "", "application/json")
	if w.Code != 200 || len(g.history) != 1 || g.message != "値を並べます" {
		t.Fatalf("chat: %d %s", w.Code, w.Body.String())
	}
	g.err = context.DeadlineExceeded
	w = requestTest(h, "POST", "/api/chat", body, "", "application/json")
	if w.Code != 504 {
		t.Fatal(w.Code)
	}
	g.err = errors.New("secret material")
	w = requestTest(h, "POST", "/api/chat", body, "", "application/json")
	if w.Code != 502 || strings.Contains(w.Body.String(), "secret") {
		t.Fatalf("leak: %s", w.Body.String())
	}
}

func TestInvalidQuestionOutput(t *testing.T) {
	g := &fakeGenerator{}
	h := New(g, nil)
	start := `{"learningContent":"配列"}`
	chat := `{"sessionId":"2417ac2f-3cd3-4a18-8dd9-82a0205e5912","learningContent":"配列","history":[{"role":"model","content":"何ですか？"}],"message":"値を並べます"}`
	for _, reply := range []string{
		"配列は値を順番に並べる仕組みです。",
		"配列とは何？ どんなときに使う？",
		"値を順番に並べます。例はありますか？",
		"正解です\n例はありますか？",
		strings.Repeat("あ", 300) + "？",
	} {
		g.reply = reply
		for path, body := range map[string]string{"/api/start": start, "/api/chat": chat} {
			w := requestTest(h, "POST", path, body, "", "application/json")
			if w.Code != 502 || !strings.Contains(w.Body.String(), `"code":"GENERATION_FAILED"`) {
				t.Errorf("%s with %q: %d %s", path, reply, w.Code, w.Body.String())
			}
		}
	}
}

func TestCORSAndMethod(t *testing.T) {
	g := &fakeGenerator{}
	h := New(g, []string{"https://example.test"})
	w := requestTest(h, "POST", "/api/start", `{}`, "https://evil.test", "application/json")
	if w.Code != 403 || w.Header().Get("Access-Control-Allow-Origin") != "" || g.calls != 0 {
		t.Fatal("origin accepted")
	}
	r := httptest.NewRequest("OPTIONS", "/api/start", nil)
	r.Header.Set("Origin", "https://example.test")
	r.Header.Set("Access-Control-Request-Method", "POST")
	r.Header.Set("Access-Control-Request-Headers", "content-type")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 204 || w.Header().Get("Access-Control-Allow-Methods") != "POST" || g.calls != 0 {
		t.Fatal("bad preflight")
	}
	w = requestTest(h, "GET", "/api/chat", "", "", "")
	if w.Code != 405 || w.Header().Get("Allow") == "" {
		t.Fatal("bad method")
	}
	w = requestTest(h, "POST", "/api/start", `{}`, "", "text/plain")
	if w.Code != 415 {
		t.Fatal("bad content type")
	}
}

type waitingGenerator struct{ fakeGenerator }

func (g *waitingGenerator) Start(ctx context.Context, content string) (string, error) {
	<-ctx.Done()
	return "", ctx.Err()
}
func TestTimeout(t *testing.T) {
	g := &waitingGenerator{}
	s := New(g, nil).(*Server)
	s.timeout = time.Millisecond
	w := requestTest(s, "POST", "/api/start", `{"learningContent":"教材"}`, "", "application/json")
	if w.Code != 504 {
		t.Fatal(w.Code)
	}
}
