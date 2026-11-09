package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/yasusudas/ai-ni-oshieru/backend/internal/learning"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

type fakeRepository struct {
	security       *learning.Security
	record         *learning.Record
	err            error
	commitErr      error
	reads, commits int
}

func (r *fakeRepository) Lookup(_ context.Context, id, digest string, expiry time.Time) (*learning.Record, error) {
	r.reads++
	if r.record != nil && r.record.PayloadDigest != digest {
		return nil, learning.ErrAlreadyFinished
	}
	return r.record, r.err
}
func (r *fakeRepository) Commit(_ context.Context, v learning.Record) (*learning.Record, error) {
	r.commits++
	if r.commitErr != nil {
		return nil, r.commitErr
	}
	if r.err != nil {
		return nil, r.err
	}
	if r.record == nil {
		v.ViewKeyID = "v"
		v.SavedAt = time.Now().UTC()
		v.ExpiresAt = v.SavedAt.Add(30 * 24 * time.Hour)
		r.record = &v
	}
	return r.record, nil
}
func (r *fakeRepository) Open(_ context.Context, key string) (*learning.Record, error) {
	r.reads++
	if r.err != nil {
		return nil, r.err
	}
	if r.record == nil {
		return nil, learning.ErrNotFound
	}
	normalized, err := learning.NormalizeViewKey(key)
	if err != nil {
		return nil, learning.ErrNotFound
	}
	expected, err := r.security.ViewKey(r.record.SessionID, r.record.ViewKeyID, r.record.ViewKeyNonce)
	if err != nil || normalized != expected {
		return nil, learning.ErrNotFound
	}
	return r.record, nil
}
func persistentFixture(t *testing.T) (*Server, *fakeGenerator, *fakeRepository) {
	t.Helper()
	a, b := make([]byte, 32), make([]byte, 32)
	a[0] = 1
	b[0] = 2
	security, err := learning.NewSecurity(learning.KeyRing{Active: "s", Keys: map[string][]byte{"s": a}}, learning.KeyRing{Active: "v", Keys: map[string][]byte{"v": b}})
	if err != nil {
		t.Fatal(err)
	}
	u, rev := []learning.RawUnderstood{}, []learning.RawReview{}
	g := &fakeGenerator{reply: "説明できる？", raw: learning.RawSummary{Understood: &u, Review: &rev}}
	r := &fakeRepository{security: security}
	return NewPersistent(g, nil, security, r, nil, nil).(*Server), g, r
}
func callJSON(t *testing.T, s *Server, path string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	body, e := json.Marshal(payload)
	if e != nil {
		t.Fatal(e)
	}
	return requestTest(s, "POST", path, string(body), "", "application/json")
}
func signedRequest(s *Server) map[string]any {
	id := "2417ac2f-3cd3-4a18-8dd9-82a0205e5912"
	history := []learning.Turn{{Role: "model", Content: "説明できる？"}, {Role: "user", Content: "値を並べるものです"}, {Role: "model", Content: "順番とは？"}}
	return map[string]any{"sessionId": id, "learningContent": "教材", "history": history, "sessionProof": s.security.Sign(id, "教材", history, s.now().Add(time.Hour))}
}
func TestPersistentStartChatSummaryOpen(t *testing.T) {
	s, g, r := persistentFixture(t)
	w := callJSON(t, s, "/api/start", map[string]any{"learningContent": " 教材 "})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var start struct {
		SessionID, Reply, SessionProof string
		SessionExpiresAt               time.Time
	}
	if e := json.Unmarshal(w.Body.Bytes(), &start); e != nil {
		t.Fatal(e)
	}
	h := []learning.Turn{{Role: "model", Content: start.Reply}}
	expiry, e := s.security.Verify(start.SessionProof, start.SessionID, "教材", h, s.now())
	if e != nil || expiry != start.SessionExpiresAt {
		t.Fatal("proof", e)
	}
	payload := map[string]any{"sessionId": start.SessionID, "learningContent": "教材", "history": h, "message": "値を並べるものです", "sessionProof": start.SessionProof}
	w = callJSON(t, s, "/api/chat", payload)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var chat struct{ Reply, SessionProof string }
	json.Unmarshal(w.Body.Bytes(), &chat)
	h = append(h, learning.Turn{Role: "user", Content: "値を並べるものです"}, learning.Turn{Role: "model", Content: chat.Reply})
	updated, e := s.security.Verify(chat.SessionProof, start.SessionID, "教材", h, s.now())
	if e != nil || !updated.Equal(expiry) {
		t.Fatal("extended expiry", e)
	}
	delete(payload, "message")
	payload["history"] = h
	payload["sessionProof"] = chat.SessionProof
	w = callJSON(t, s, "/api/summary", payload)
	if w.Code != 200 || r.commits != 1 {
		t.Fatal(w.Body.String())
	}
	first := w.Body.String()
	calls := g.calls
	w = callJSON(t, s, "/api/summary", payload)
	if w.Code != 200 || w.Body.String() != first || g.calls != calls {
		t.Fatal("replay", w.Body.String())
	}
	key, err := s.security.ViewKey(r.record.SessionID, r.record.ViewKeyID, r.record.ViewKeyNonce)
	if err != nil {
		t.Fatal(err)
	}
	w = callJSON(t, s, "/api/history/open", map[string]any{"viewKey": learning.FormatViewKey(key)})
	if w.Code != 200 || g.calls != calls {
		t.Fatal(w.Body.String())
	}
}

func TestPersistentAudioPreservesSignedLearning(t *testing.T) {
	s, g, repository := persistentFixture(t)
	synthesisCalls := 0
	combined := NewPersistentWithAudio(g, nil, s.security, repository, nil, nil, synthesizerFunc(func(_ context.Context, text string) ([]byte, error) {
		synthesisCalls++
		return []byte("mp3"), nil
	})).(*Server)
	if combined.now == nil || combined.synthesisSlots == nil || combined.speechSlots == nil {
		t.Fatal("missing persistent/audio initialization")
	}
	payload := signedRequest(combined)
	w := callJSON(t, combined, "/api/speech/synthesize", map[string]any{"text": "説明できる？"})
	if w.Code != 200 || synthesisCalls != 1 || g.calls != 0 || repository.reads != 0 || repository.commits != 0 {
		t.Fatal("synthesis changed learning state", w.Code)
	}
	w = callJSON(t, combined, "/api/summary", payload)
	if w.Code != 200 || repository.commits != 1 {
		t.Fatal("signed persistence unavailable with synthesis", w.Body.String())
	}
	first := w.Body.String()
	w = callJSON(t, combined, "/api/summary", payload)
	if w.Code != 200 || w.Body.String() != first || repository.commits != 1 {
		t.Fatal("saved result replay changed", w.Body.String())
	}
	var saved struct {
		ViewKey            string
		Summary            learning.Summary
		SavedAt, ExpiresAt time.Time
	}
	if err := json.Unmarshal([]byte(first), &saved); err != nil || saved.ViewKey == "" {
		t.Fatal("saved key unavailable", err)
	}
	w = callJSON(t, combined, "/api/history/open", map[string]any{"viewKey": saved.ViewKey})
	if w.Code != 200 || synthesisCalls != 1 {
		t.Fatal("saved key did not open record", w.Code)
	}
	var opened learning.Record
	if err := json.Unmarshal(w.Body.Bytes(), &opened); err != nil {
		t.Fatal(err)
	}
	if opened.SessionID != payload["sessionId"] || opened.LearningContent != payload["learningContent"] ||
		!reflect.DeepEqual(opened.History, payload["history"]) || !reflect.DeepEqual(opened.Summary, saved.Summary) ||
		!opened.SavedAt.Equal(saved.SavedAt) || !opened.ExpiresAt.Equal(saved.ExpiresAt) {
		t.Fatal("opened record differs from committed result")
	}
	wrongKey := "0" + saved.ViewKey[1:]
	if saved.ViewKey[0] == '0' {
		wrongKey = "1" + saved.ViewKey[1:]
	}
	for _, key := range []string{"placeholder", wrongKey} {
		w = callJSON(t, combined, "/api/history/open", map[string]any{"viewKey": key})
		if w.Code != 404 {
			t.Fatal("arbitrary key opened saved record", w.Code)
		}
	}

	payload["learningContent"] = "改変した教材"
	w = callJSON(t, combined, "/api/summary", payload)
	if w.Code != 401 || repository.commits != 1 {
		t.Fatal("signature validation lost with synthesis", w.Code)
	}
}
func TestInvalidProofNeverGeneratesOrReads(t *testing.T) {
	for _, path := range []string{"/api/summary", "/api/chat"} {
		for _, change := range []string{"missing", "tamper", "expired", "unknown"} {
			t.Run(path+"/"+change, func(t *testing.T) {
				s, g, r := persistentFixture(t)
				p := signedRequest(s)
				if path == "/api/chat" {
					p["message"] = "追加の説明"
				}
				switch change {
				case "missing":
					delete(p, "sessionProof")
				case "tamper":
					p["learningContent"] = "改変"
				case "expired":
					s.now = func() time.Time { return time.Now().Add(3 * time.Hour) }
				case "unknown":
					p["sessionProof"] = "s1.unknown.1.invalid"
				}
				w := callJSON(t, s, path, p)
				if w.Code != 401 || g.calls != 0 || r.reads != 0 || r.commits != 0 {
					t.Fatal(w.Code, g.calls, r.reads, w.Body.String())
				}
			})
		}
	}
}
func TestStorageErrorsAndStrictOpen(t *testing.T) {
	s, g, r := persistentFixture(t)
	r.err = errors.New("private failure")
	w := callJSON(t, s, "/api/summary", signedRequest(s))
	if w.Code != 503 || g.calls != 0 {
		t.Fatal(w.Body.String())
	}
	for _, body := range []string{`{"viewKey":null}`, `{"viewKey":1}`, `{"ViewKey":"x"}`, `{"viewKey":"x","viewKey":"y"}`, `{"viewKey":"x","extra":0}`} {
		w = requestTest(s, "POST", "/api/history/open", body, "", "application/json")
		if w.Code != 400 {
			t.Fatal(body, w.Code)
		}
	}
	r.err = learning.ErrNotFound
	w = callJSON(t, s, "/api/history/open", map[string]any{"viewKey": "invalid"})
	if w.Code != 404 {
		t.Fatal(w.Body.String())
	}
}
func TestGenerationFailureNeverCommits(t *testing.T) {
	s, g, r := persistentFixture(t)
	g.err = errors.New("failed")
	w := callJSON(t, s, "/api/summary", signedRequest(s))
	if w.Code != 502 || r.commits != 0 {
		t.Fatal(w.Body.String())
	}
}

func TestCommitFailureDoesNotReturnSaved(t *testing.T) {
	s, _, r := persistentFixture(t)
	r.commitErr = errors.New("result uncertain")
	w := callJSON(t, s, "/api/summary", signedRequest(s))
	if w.Code != 503 || r.commits != 1 {
		t.Fatal(w.Code, w.Body.String())
	}
	var out map[string]any
	json.Unmarshal(w.Body.Bytes(), &out)
	if _, ok := out["saved"]; ok {
		t.Fatal("reported success")
	}
}
func TestProofExpiryDuringChat(t *testing.T) {
	s, g, _ := persistentFixture(t)
	p := signedRequest(s)
	p["message"] = "追加の説明"
	now := s.now()
	calls := 0
	s.now = func() time.Time {
		calls++
		if calls == 1 {
			return now
		}
		return now.Add(3 * time.Hour)
	}
	w := callJSON(t, s, "/api/chat", p)
	if w.Code != 401 || g.calls != 1 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestUnicodeWhitespaceUsesNormalizedProof(t *testing.T) {
	s, _, r := persistentFixture(t)
	p := signedRequest(s)
	p["learningContent"] = "\u3000\u00a0教材\u2003\n"
	h := p["history"].([]learning.Turn)
	for i := range h {
		h[i].Content = "\u3000\u0085" + h[i].Content + "\u2028\t"
	}
	w := callJSON(t, s, "/api/summary", p)
	if w.Code != 200 || r.record == nil || r.record.LearningContent != "教材" {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, turn := range r.record.History {
		if turn.Content != learning.Trim(turn.Content) {
			t.Fatal("unnormalized history saved")
		}
	}
}
func TestOpenExposesOnlyPublicFields(t *testing.T) {
	s, g, r := persistentFixture(t)
	p := signedRequest(s)
	w := callJSON(t, s, "/api/summary", p)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	r.record.SchemaVersion = 1
	r.record.State = "saved"
	r.record.ViewKeyNonce = 4
	r.record.Summary = learning.Summary{}
	before := g.calls
	key, err := s.security.ViewKey(r.record.SessionID, r.record.ViewKeyID, r.record.ViewKeyNonce)
	if err != nil {
		t.Fatal(err)
	}
	w = callJSON(t, s, "/api/history/open", map[string]any{"viewKey": learning.FormatViewKey(key)})
	if w.Code != 200 || g.calls != before {
		t.Fatal(w.Code)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	var summary map[string]json.RawMessage
	if err := json.Unmarshal(fields["summary"], &summary); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"understood", "review"} {
		if string(summary[name]) != "[]" {
			t.Errorf("empty %s must be an array", name)
		}
	}
	allowed := map[string]bool{"sessionId": true, "learningContent": true, "history": true, "summary": true, "savedAt": true, "expiresAt": true}
	if len(fields) != len(allowed) {
		t.Fatal("unexpected response fields")
	}
	for key := range fields {
		if !allowed[key] {
			t.Fatalf("internal field exposed: %s", key)
		}
	}
}
