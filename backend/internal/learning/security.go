package learning

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

var (
	ErrInvalidSession = errors.New("invalid session")
	errInvalidViewKey = errors.New("invalid view key")
	keyIDPattern      = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)
	crockfordEncoding = base32.NewEncoding(crockfordAlphabet).WithPadding(base32.NoPadding)
)

type KeyRing struct {
	Active string
	Keys   map[string][]byte
}

func (r KeyRing) Validate() error {
	if !keyIDPattern.MatchString(r.Active) {
		return errors.New("invalid active key ID")
	}
	if _, ok := r.Keys[r.Active]; !ok {
		return errors.New("active key is missing")
	}
	for id, key := range r.Keys {
		if !keyIDPattern.MatchString(id) || len(key) < 32 {
			return errors.New("invalid key configuration")
		}
	}
	return nil
}

type Security struct {
	proofKeys KeyRing
	viewKeys  KeyRing
}

func NewSecurity(proofKeys, viewKeys KeyRing) (*Security, error) {
	if err := proofKeys.Validate(); err != nil {
		return nil, err
	}
	if err := viewKeys.Validate(); err != nil {
		return nil, err
	}
	for _, proofKey := range proofKeys.Keys {
		for _, viewKey := range viewKeys.Keys {
			if hmac.Equal(proofKey, viewKey) {
				return nil, errors.New("keys must not be shared between purposes")
			}
		}
	}
	return &Security{proofKeys: cloneKeyRing(proofKeys), viewKeys: cloneKeyRing(viewKeys)}, nil
}

func cloneKeyRing(r KeyRing) KeyRing {
	cloned := KeyRing{Active: r.Active, Keys: make(map[string][]byte, len(r.Keys))}
	for id, key := range r.Keys {
		cloned.Keys[id] = append([]byte(nil), key...)
	}
	return cloned
}

type proofPayload struct {
	Purpose         string `json:"purpose"`
	Version         int    `json:"version"`
	KeyID           string `json:"keyId"`
	SessionID       string `json:"sessionId"`
	ExpiresUnix     int64  `json:"expiresUnix"`
	LearningContent string `json:"learningContent"`
	History         []Turn `json:"history"`
}

func proofTag(key []byte, keyID, id, content string, history []Turn, expiresUnix int64) []byte {
	payload, _ := json.Marshal(proofPayload{
		Purpose: "session-proof", Version: 1, KeyID: keyID, SessionID: id,
		ExpiresUnix: expiresUnix, LearningContent: content, History: history,
	})
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}

func (s *Security) Sign(id, content string, history []Turn, expires time.Time) string {
	keyID := s.proofKeys.Active
	expiresUnix := expires.Unix()
	tag := proofTag(s.proofKeys.Keys[keyID], keyID, id, content, history, expiresUnix)
	return "s1." + keyID + "." + strconv.FormatInt(expiresUnix, 10) + "." + base64.RawURLEncoding.EncodeToString(tag)
}

func (s *Security) Verify(token, id, content string, history []Turn, now time.Time) (time.Time, error) {
	if len(token) > 100 {
		return time.Time{}, ErrInvalidSession
	}
	parts := strings.Split(token, ".")
	if len(parts) != 4 || parts[0] != "s1" {
		return time.Time{}, ErrInvalidSession
	}
	key, ok := s.proofKeys.Keys[parts[1]]
	if !ok {
		return time.Time{}, ErrInvalidSession
	}
	expiresUnix, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || strconv.FormatInt(expiresUnix, 10) != parts[2] || now.Unix() >= expiresUnix {
		return time.Time{}, ErrInvalidSession
	}
	tag, err := base64.RawURLEncoding.Strict().DecodeString(parts[3])
	if err != nil || len(tag) != sha256.Size || base64.RawURLEncoding.EncodeToString(tag) != parts[3] {
		return time.Time{}, ErrInvalidSession
	}
	expected := proofTag(key, parts[1], id, content, history, expiresUnix)
	if !hmac.Equal(tag, expected) {
		return time.Time{}, ErrInvalidSession
	}
	return time.Unix(expiresUnix, 0).UTC(), nil
}

func (s *Security) ActiveViewKeyID() string { return s.viewKeys.Active }

func (s *Security) ViewKey(id, keyID string, nonce int64) (string, error) {
	key, ok := s.viewKeys.Keys[keyID]
	if !ok || nonce < 0 {
		return "", errInvalidViewKey
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("view-key|v1|" + id + "|" + strconv.FormatInt(nonce, 10)))
	return crockfordEncoding.EncodeToString(mac.Sum(nil))[:12], nil
}

func NormalizeViewKey(input string) (string, error) {
	var normalized strings.Builder
	for _, char := range input {
		if char == '-' || unicode.IsSpace(char) {
			continue
		}
		if char >= 'a' && char <= 'z' {
			char -= 'a' - 'A'
		}
		switch char {
		case 'I', 'L':
			char = '1'
		case 'O':
			char = '0'
		}
		if char > unicode.MaxASCII || !strings.ContainsRune(crockfordAlphabet, char) || normalized.Len() >= 12 {
			return "", errInvalidViewKey
		}
		normalized.WriteByte(byte(char))
	}
	if normalized.Len() != 12 {
		return "", errInvalidViewKey
	}
	return normalized.String(), nil
}

func FormatViewKey(raw string) string {
	normalized, err := NormalizeViewKey(raw)
	if err != nil {
		return ""
	}
	return normalized[:4] + "-" + normalized[4:8] + "-" + normalized[8:]
}

func PayloadDigest(content string, history []Turn) string {
	payload, _ := json.Marshal(struct {
		LearningContent string `json:"learningContent"`
		History         []Turn `json:"history"`
	}{LearningContent: content, History: history})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func KeyHash(raw string) string {
	digest := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(digest[:])
}
