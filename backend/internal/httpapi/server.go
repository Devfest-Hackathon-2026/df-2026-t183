package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yasusudas/ai-ni-oshieru/backend/internal/jsoncheck"
	"github.com/yasusudas/ai-ni-oshieru/backend/internal/learning"
	"github.com/yasusudas/ai-ni-oshieru/backend/internal/speech"
	"github.com/yasusudas/ai-ni-oshieru/backend/internal/synthesis"
)

const maxBody = 512 << 10
const maxSynthesisBody = 4 << 10

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-4[0-9a-fA-F]{3}-[89aAbB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

type Server struct {
	generator         learning.Generator
	origins           map[string]bool
	timeout           time.Duration
	speechValidator   speech.Validator
	speechTranscriber speech.Transcriber
	speechSlots       chan struct{}
	security          *learning.Security
	repository        learning.Repository
	now               func() time.Time
	synthesizer       synthesis.Synthesizer
	synthesisSlots    chan struct{}
}
type request struct {
	SessionProof    string          `json:"sessionProof"`
	ViewKey         string          `json:"viewKey"`
	SessionID       string          `json:"sessionId"`
	LearningContent string          `json:"learningContent"`
	History         []learning.Turn `json:"history"`
	Message         string          `json:"message"`
}
type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func New(generator learning.Generator, origins []string) http.Handler {
	return NewWithSpeech(generator, origins, nil, nil)
}

func NewWithSpeech(generator learning.Generator, origins []string, validator speech.Validator, transcriber speech.Transcriber) http.Handler {
	return NewPersistent(generator, origins, nil, nil, validator, transcriber)
}

func NewPersistent(generator learning.Generator, origins []string, security *learning.Security, repository learning.Repository, validator speech.Validator, transcriber speech.Transcriber) http.Handler {
	return NewPersistentWithAudio(generator, origins, security, repository, validator, transcriber, nil)
}

func NewWithAudio(generator learning.Generator, origins []string, validator speech.Validator, transcriber speech.Transcriber, synthesizer synthesis.Synthesizer) http.Handler {
	return NewPersistentWithAudio(generator, origins, nil, nil, validator, transcriber, synthesizer)
}

func NewPersistentWithAudio(generator learning.Generator, origins []string, security *learning.Security, repository learning.Repository, validator speech.Validator, transcriber speech.Transcriber, synthesizer synthesis.Synthesizer) http.Handler {
	allowed := make(map[string]bool, len(origins))
	for _, origin := range origins {
		allowed[origin] = true
	}
	return &Server{
		generator: generator, origins: allowed, timeout: 60 * time.Second,
		speechValidator: validator, speechTranscriber: transcriber, speechSlots: make(chan struct{}, 2),
		synthesizer: synthesizer, synthesisSlots: make(chan struct{}, 2),
		security: security, repository: repository, now: time.Now,
	}
}
func send(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, status int, code, message string) {
	send(w, status, map[string]apiError{"error": {code, message}})
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	totalTimeout := 70 * time.Second
	if r.URL.Path == "/api/speech/transcribe" {
		totalTimeout = 50 * time.Second
	} else if r.URL.Path == "/api/speech/synthesize" {
		totalTimeout = 25 * time.Second
	}
	totalContext, totalCancel := context.WithTimeout(r.Context(), totalTimeout)
	defer totalCancel()
	r = r.WithContext(totalContext)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Add("Vary", "Origin")
	origin := r.Header.Get("Origin")
	if origin != "" {
		if !s.origins[origin] {
			fail(w, 403, "ORIGIN_NOT_ALLOWED", "許可されていない接続元です。")
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
	}
	if r.URL.Path != "/api/start" && r.URL.Path != "/api/chat" && r.URL.Path != "/api/summary" && r.URL.Path != "/api/speech/transcribe" && r.URL.Path != "/api/speech/synthesize" && (r.URL.Path != "/api/history/open" || s.security == nil) {
		fail(w, 404, "NOT_FOUND", "指定された API がありません。")
		return
	}
	if r.Method == http.MethodOptions {
		if origin == "" || r.Header.Get("Access-Control-Request-Method") != "POST" || !preflightHeaders(r.Header.Get("Access-Control-Request-Headers")) {
			fail(w, 403, "ORIGIN_NOT_ALLOWED", "許可されていない接続です。")
			return
		}
		w.Header().Set("Access-Control-Allow-Methods", "POST")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Add("Vary", "Access-Control-Request-Method")
		w.Header().Add("Vary", "Access-Control-Request-Headers")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST, OPTIONS")
		fail(w, 405, "METHOD_NOT_ALLOWED", "許可されていないメソッドです。")
		return
	}
	if r.URL.Path == "/api/speech/transcribe" {
		s.transcribe(w, r)
		return
	}
	media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || (params["charset"] != "" && !strings.EqualFold(params["charset"], "utf-8")) {
		fail(w, 415, "UNSUPPORTED_MEDIA_TYPE", "JSON を送信してください。")
		return
	}
	if r.URL.Path == "/api/speech/synthesize" {
		s.synthesize(w, r)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil {
		fail(w, 400, "INVALID_INPUT", "リクエストを読み取れません。")
		return
	}
	if len(body) > maxBody {
		fail(w, 413, "INPUT_TOO_LARGE", "入力が上限を超えています。")
		return
	}
	req, status := decodeRequest(body, r.URL.Path, s.security != nil)
	if status != 0 {
		if status == 413 {
			fail(w, 413, "INPUT_TOO_LARGE", "入力が上限を超えています。")
		} else {
			fail(w, 400, "INVALID_INPUT", "入力内容を確認してください。")
		}
		return
	}
	var proofExpiry time.Time
	if s.security != nil && (r.URL.Path == "/api/chat" || r.URL.Path == "/api/summary") {
		proofExpiry, err = s.security.Verify(req.SessionProof, req.SessionID, req.LearningContent, req.History, s.now())
		if s.persistenceError(w, err) {
			return
		}
	}
	if r.URL.Path == "/api/history/open" {
		record, err := s.repository.Open(r.Context(), req.ViewKey)
		if s.persistenceError(w, err) {
			return
		}
		send(w, 200, record)
		return
	}
	if s.security != nil && r.URL.Path == "/api/summary" {
		record, err := s.repository.Lookup(r.Context(), req.SessionID, learning.PayloadDigest(req.LearningContent, req.History), proofExpiry)
		if s.persistenceError(w, err) {
			return
		}
		if record != nil {
			s.savedResponse(w, record)
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.timeout)
	defer cancel()
	switch r.URL.Path {
	case "/api/start":
		reply, err := s.generator.Start(ctx, req.LearningContent)
		if s.generationError(w, ctx, err) {
			return
		}
		reply, err = learning.Question(reply)
		if s.generationError(w, ctx, err) {
			return
		}
		id, err := newUUID()
		if err != nil {
			fail(w, 500, "INTERNAL_ERROR", "処理に失敗しました。")
			return
		}
		if s.security != nil {
			expiry := s.now().UTC().Truncate(time.Second).Add(2 * time.Hour)
			proof := s.security.Sign(id, req.LearningContent, []learning.Turn{{Role: "model", Content: reply}}, expiry)
			send(w, 200, map[string]any{"sessionId": id, "reply": reply, "sessionProof": proof, "sessionExpiresAt": expiry})
			return
		}
		send(w, 200, map[string]string{"sessionId": id, "reply": reply})
	case "/api/chat":
		reply, err := s.generator.Chat(ctx, req.LearningContent, req.History, req.Message)
		if s.generationError(w, ctx, err) {
			return
		}
		reply, err = learning.Question(reply)
		if s.generationError(w, ctx, err) {
			return
		}
		if s.security != nil {
			if !s.now().Before(proofExpiry) {
				s.persistenceError(w, learning.ErrInvalidSession)
				return
			}
			history := append(req.History, learning.Turn{Role: "user", Content: req.Message}, learning.Turn{Role: "model", Content: reply})
			send(w, 200, map[string]string{"reply": reply, "sessionProof": s.security.Sign(req.SessionID, req.LearningContent, history, proofExpiry)})
			return
		}
		send(w, 200, map[string]string{"reply": reply})
	case "/api/summary":
		raw, err := s.generator.Summary(ctx, req.LearningContent, req.History)
		if s.generationError(w, ctx, err) {
			return
		}
		summary, err := learning.ValidateSummary(raw, req.History)
		if s.generationError(w, ctx, err) {
			return
		}
		if s.security != nil {
			record, err := s.repository.Commit(r.Context(), learning.Record{SessionID: req.SessionID, LearningContent: req.LearningContent, History: req.History, Summary: summary, PayloadDigest: learning.PayloadDigest(req.LearningContent, req.History), ProofExpiresAt: proofExpiry})
			if s.persistenceError(w, err) {
				return
			}
			s.savedResponse(w, record)
			return
		}
		send(w, 200, map[string]any{"sessionId": req.SessionID, "summary": summary})
	}
}

func (s *Server) synthesize(w http.ResponseWriter, r *http.Request) {
	if deadline, ok := r.Context().Deadline(); ok {
		controller := http.NewResponseController(w)
		_ = controller.SetReadDeadline(deadline)
		defer controller.SetReadDeadline(time.Time{})
	}
	if r.URL.RawQuery != "" {
		fail(w, 400, "INVALID_INPUT", "入力内容を確認してください。")
		return
	}
	if r.ContentLength > maxSynthesisBody {
		fail(w, 413, "INPUT_TOO_LARGE", "質問文が上限を超えています。")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxSynthesisBody+1))
	if err != nil {
		var timeout net.Error
		if errors.Is(r.Context().Err(), context.Canceled) {
			return
		}
		if errors.Is(r.Context().Err(), context.DeadlineExceeded) || (errors.As(err, &timeout) && timeout.Timeout()) {
			fail(w, 504, "SYNTHESIS_TIMEOUT", "読み上げがタイムアウトしました。")
			return
		}
		fail(w, 400, "INVALID_INPUT", "入力内容を確認してください。")
		return
	}
	if len(body) > maxSynthesisBody {
		fail(w, 413, "INPUT_TOO_LARGE", "質問文が上限を超えています。")
		return
	}
	if !jsoncheck.Valid(body) {
		fail(w, 400, "INVALID_INPUT", "入力内容を確認してください。")
		return
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil || len(fields) != 1 || fields["text"] == nil {
		fail(w, 400, "INVALID_INPUT", "入力内容を確認してください。")
		return
	}
	var input struct {
		Text *string `json:"text"`
	}
	if json.Unmarshal(body, &input) != nil || input.Text == nil {
		fail(w, 400, "INVALID_INPUT", "入力内容を確認してください。")
		return
	}
	question := learning.Trim(*input.Text)
	if question == "" {
		fail(w, 400, "INVALID_INPUT", "質問文を入力してください。")
		return
	}
	if learning.Length(question) > 300 {
		fail(w, 413, "INPUT_TOO_LARGE", "質問文が上限を超えています。")
		return
	}
	if s.synthesizer == nil {
		fail(w, 500, "INTERNAL_ERROR", "読み上げの設定を確認してください。")
		return
	}
	select {
	case s.synthesisSlots <- struct{}{}:
		defer func() { <-s.synthesisSlots }()
	default:
		fail(w, 503, "SYNTHESIS_UNAVAILABLE", "ただいま読み上げを利用できません。")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if errors.Is(ctx.Err(), context.Canceled) {
		return
	}
	if ctx.Err() != nil {
		s.synthesisError(w, ctx.Err())
		return
	}
	audio, err := s.synthesizer.Synthesize(ctx, question)
	if errors.Is(ctx.Err(), context.Canceled) {
		return
	}
	if ctx.Err() != nil {
		s.synthesisError(w, ctx.Err())
		return
	}
	if err != nil {
		s.synthesisError(w, err)
		return
	}
	if len(audio) == 0 || len(audio) > 2<<20 {
		s.synthesisError(w, synthesis.ErrFailed)
		return
	}
	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.Itoa(len(audio)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(audio)
}

func (s *Server) synthesisError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		fail(w, 504, "SYNTHESIS_TIMEOUT", "読み上げがタイムアウトしました。")
	case errors.Is(err, synthesis.ErrInternal):
		fail(w, 500, "INTERNAL_ERROR", "読み上げの設定を確認してください。")
	case errors.Is(err, synthesis.ErrUnavailable):
		fail(w, 503, "SYNTHESIS_UNAVAILABLE", "ただいま読み上げを利用できません。")
	default:
		fail(w, 502, "SYNTHESIS_FAILED", "読み上げに失敗しました。")
	}
}

func (s *Server) transcribe(w http.ResponseWriter, r *http.Request) {
	if deadline, ok := r.Context().Deadline(); ok {
		controller := http.NewResponseController(w)
		_ = controller.SetReadDeadline(deadline)
		defer controller.SetReadDeadline(time.Time{})
	}
	if r.URL.RawQuery != "" {
		fail(w, 400, "INVALID_INPUT", "入力内容を確認してください。")
		return
	}
	if s.speechValidator == nil || s.speechTranscriber == nil {
		fail(w, 503, "TRANSCRIPTION_UNAVAILABLE", "音声文字起こしを利用できません。")
		return
	}
	contentTypes := r.Header.Values("Content-Type")
	if len(contentTypes) != 1 {
		fail(w, 415, "UNSUPPORTED_MEDIA_TYPE", "対応する音声を送信してください。")
		return
	}
	mediaType, params, err := mime.ParseMediaType(contentTypes[0])
	if err != nil || !supportedAudioType(mediaType, params) {
		fail(w, 415, "UNSUPPORTED_MEDIA_TYPE", "対応する音声を送信してください。")
		return
	}
	encodings := r.Header.Values("Content-Encoding")
	if len(encodings) > 1 {
		fail(w, 415, "UNSUPPORTED_MEDIA_TYPE", "対応する音声を送信してください。")
		return
	}
	encoding := ""
	if len(encodings) == 1 {
		encoding = strings.TrimSpace(encodings[0])
	}
	if encoding != "" && !strings.EqualFold(encoding, "identity") {
		fail(w, 415, "UNSUPPORTED_MEDIA_TYPE", "対応する音声を送信してください。")
		return
	}
	select {
	case s.speechSlots <- struct{}{}:
		defer func() { <-s.speechSlots }()
	default:
		fail(w, 503, "TRANSCRIPTION_UNAVAILABLE", "ただいま音声文字起こしを利用できません。時間をおいてお試しください。")
		return
	}
	if r.ContentLength > speech.MaxBody {
		fail(w, 413, "INPUT_TOO_LARGE", "音声が上限を超えています。")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, speech.MaxBody+1))
	if err != nil {
		var timeout net.Error
		if errors.Is(r.Context().Err(), context.DeadlineExceeded) || (errors.As(err, &timeout) && timeout.Timeout()) {
			fail(w, 504, "TRANSCRIPTION_TIMEOUT", "文字起こしがタイムアウトしました。もう一度お試しください。")
			return
		}
		fail(w, 400, "INVALID_INPUT", "音声を読み取れません。")
		return
	}
	if len(body) > speech.MaxBody {
		fail(w, 413, "INPUT_TOO_LARGE", "音声が上限を超えています。")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 50*time.Second)
	defer cancel()
	if ctx.Err() != nil {
		s.speechError(w, ctx.Err())
		return
	}
	if err := s.speechValidator.Validate(ctx, body, contentTypes[0]); err != nil {
		s.speechError(w, err)
		return
	}
	if ctx.Err() != nil {
		s.speechError(w, ctx.Err())
		return
	}
	text, err := s.speechTranscriber.Transcribe(ctx, body)
	if ctx.Err() != nil {
		s.speechError(w, ctx.Err())
		return
	}
	if err != nil {
		s.speechError(w, err)
		return
	}
	text = learning.Trim(text)
	if text == "" {
		fail(w, 400, "INVALID_INPUT", "音声から言葉を確認できませんでした。もう一度お試しください。")
		return
	}
	if !utf8.ValidString(text) || learning.Length(text) > speech.MaxTranscript {
		fail(w, 502, "TRANSCRIPTION_FAILED", "文字起こしに失敗しました。もう一度お試しください。")
		return
	}
	send(w, 200, map[string]string{"text": text})
}

func supportedAudioType(media string, params map[string]string) bool {
	for key := range params {
		if key != "codecs" {
			return false
		}
	}
	codec := params["codecs"]
	switch media {
	case "audio/webm", "audio/ogg":
		return codec == "" || strings.EqualFold(codec, "opus")
	case "audio/mp4":
		return codec == "" || strings.EqualFold(codec, "mp4a.40.2")
	default:
		return false
	}
}

func (s *Server) speechError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, speech.ErrUnsupported):
		fail(w, 415, "UNSUPPORTED_MEDIA_TYPE", "対応する音声を送信してください。")
	case errors.Is(err, speech.ErrTooLarge):
		fail(w, 413, "INPUT_TOO_LARGE", "音声が上限を超えています。")
	case errors.Is(err, speech.ErrInvalid):
		fail(w, 400, "INVALID_INPUT", "音声を確認して、もう一度録音してください。")
	case errors.Is(err, context.DeadlineExceeded):
		fail(w, 504, "TRANSCRIPTION_TIMEOUT", "文字起こしがタイムアウトしました。もう一度お試しください。")
	case errors.Is(err, speech.ErrInternal):
		fail(w, 500, "INTERNAL_ERROR", "音声文字起こしの設定を確認してください。")
	case errors.Is(err, speech.ErrUnavailable):
		fail(w, 503, "TRANSCRIPTION_UNAVAILABLE", "ただいま音声文字起こしを利用できません。時間をおいてお試しください。")
	case errors.Is(err, speech.ErrFailed):
		fail(w, 502, "TRANSCRIPTION_FAILED", "文字起こしに失敗しました。もう一度お試しください。")
	default:
		fail(w, 500, "INTERNAL_ERROR", "音声文字起こしに失敗しました。")
	}
}
func preflightHeaders(headers string) bool {
	for _, h := range strings.Split(headers, ",") {
		if v := strings.TrimSpace(h); v != "" && !strings.EqualFold(v, "Content-Type") {
			return false
		}
	}
	return true
}
func (s *Server) generationError(w http.ResponseWriter, ctx context.Context, err error) bool {
	if err == nil && ctx.Err() == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		fail(w, 504, "GENERATION_TIMEOUT", "生成がタイムアウトしました。再試行してください。")
	} else {
		fail(w, 502, "GENERATION_FAILED", "生成に失敗しました。再試行してください。")
	}
	return true
}
func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	s := hex.EncodeToString(b[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", s[:8], s[8:12], s[12:16], s[16:20], s[20:]), nil
}

func decodeRequest(body []byte, path string, phaseTwo bool) (request, int) {
	var req request
	if !jsoncheck.Valid(body) {
		return req, 400
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil || fields == nil {
		return req, 400
	}
	if path == "/api/history/open" {
		if len(fields) != 1 || fields["viewKey"] == nil || string(fields["viewKey"]) == "null" || json.Unmarshal(body, &req) != nil {
			return req, 400
		}
		return req, 0
	}
	if phaseTwo && path != "/api/start" {
		if value, ok := fields["sessionProof"]; ok {
			if string(value) == "null" || json.Unmarshal(value, &req.SessionProof) != nil {
				return req, 400
			}
			delete(fields, "sessionProof")
		}
	}
	required := map[string]bool{"learningContent": true}
	if path != "/api/start" {
		required["sessionId"] = true
		required["history"] = true
	}
	if path == "/api/chat" {
		required["message"] = true
	}
	if len(fields) != len(required) {
		return req, 400
	}
	for key := range fields {
		if !required[key] {
			return req, 400
		}
	}
	if json.Unmarshal(body, &req) != nil {
		return req, 400
	}
	req.LearningContent = learning.Trim(req.LearningContent)
	if req.LearningContent == "" {
		return req, 400
	}
	if learning.Length(req.LearningContent) > 10000 {
		return req, 413
	}
	if path == "/api/start" {
		return req, 0
	}
	if !uuidPattern.MatchString(req.SessionID) || req.History == nil || len(req.History) == 0 {
		return req, 400
	}
	limit := 41
	if path == "/api/chat" {
		limit = 39
	}
	if len(req.History) > limit {
		return req, 413
	}
	var items []map[string]json.RawMessage
	if json.Unmarshal(fields["history"], &items) != nil {
		return req, 400
	}
	for i := range req.History {
		if len(items[i]) != 2 || items[i]["role"] == nil || items[i]["content"] == nil {
			return req, 400
		}
		turn := &req.History[i]
		expected := "model"
		max := 300
		if i%2 == 1 {
			expected = "user"
			max = 1000
		}
		turn.Content = learning.Trim(turn.Content)
		if turn.Role != expected || turn.Content == "" {
			return req, 400
		}
		if learning.Length(turn.Content) > max {
			return req, 413
		}
	}
	if path == "/api/chat" {
		if len(req.History)%2 == 0 {
			return req, 400
		}
		req.Message = learning.Trim(req.Message)
		if req.Message == "" {
			return req, 400
		}
		if learning.Length(req.Message) > 1000 {
			return req, 413
		}
	} else if len(req.History) < 2 {
		return req, 400
	}
	return req, 0
}

func (s *Server) persistenceError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, learning.ErrInvalidSession):
		fail(w, 401, "INVALID_SESSION", "学習の期限が切れているか、内容を確認できません。")
	case errors.Is(err, learning.ErrAlreadyFinished):
		fail(w, 409, "SESSION_ALREADY_FINISHED", "この学習はすでに終了しています。")
	case errors.Is(err, learning.ErrNotFound):
		fail(w, 404, "HISTORY_NOT_FOUND", "記録が見つからないか、期限が切れています。")
	default:
		fail(w, 503, "STORAGE_UNAVAILABLE", "保存先を確認できません。同じ内容で再試行してください。")
	}
	return true
}
func (s *Server) savedResponse(w http.ResponseWriter, r *learning.Record) {
	key, err := s.security.ViewKey(r.SessionID, r.ViewKeyID, r.ViewKeyNonce)
	if s.persistenceError(w, err) {
		return
	}
	send(w, 200, map[string]any{"sessionId": r.SessionID, "summary": r.Summary, "saved": true, "viewKey": learning.FormatViewKey(key), "savedAt": r.SavedAt, "expiresAt": r.ExpiresAt})
}
