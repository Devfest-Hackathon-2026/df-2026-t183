package learning

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

const securityTestSessionID = "2417ac2f-3cd3-4a18-8dd9-82a0205e5912"

func securityTestRings() (KeyRing, KeyRing) {
	return KeyRing{Active: "proof-1", Keys: map[string][]byte{"proof-1": bytes.Repeat([]byte{0x11}, 32)}},
		KeyRing{Active: "view-1", Keys: map[string][]byte{"view-1": bytes.Repeat([]byte{0x22}, 32)}}
}

func securityForTest(t *testing.T) *Security {
	t.Helper()
	proof, view := securityTestRings()
	security, err := NewSecurity(proof, view)
	if err != nil {
		t.Fatal(err)
	}
	return security
}

func TestKeyRingValidation(t *testing.T) {
	key := bytes.Repeat([]byte{1}, 32)
	for _, id := range []string{"a", "0_-aZ", strings.Repeat("A", 32)} {
		if err := (KeyRing{Active: id, Keys: map[string][]byte{id: key}}).Validate(); err != nil {
			t.Errorf("valid ID rejected: %v", err)
		}
	}
	for _, id := range []string{"", "a.b", "a/b", "a b", "a\n", "あ", strings.Repeat("A", 33)} {
		if err := (KeyRing{Active: id, Keys: map[string][]byte{id: key}}).Validate(); err == nil {
			t.Error("invalid active ID accepted")
		}
		if err := (KeyRing{Active: "active", Keys: map[string][]byte{"active": key, id: key}}).Validate(); err == nil {
			t.Error("invalid old ID accepted")
		}
	}
	for _, ring := range []KeyRing{
		{},
		{Active: "active"},
		{Active: "active", Keys: map[string][]byte{"other": key}},
		{Active: "active", Keys: map[string][]byte{"active": nil}},
		{Active: "active", Keys: map[string][]byte{"active": key[:31]}},
		{Active: "active", Keys: map[string][]byte{"active": key, "old": key[:31]}},
	} {
		if err := ring.Validate(); err == nil {
			t.Error("invalid ring accepted")
		}
	}
	if err := (KeyRing{Active: "long", Keys: map[string][]byte{"long": bytes.Repeat([]byte{1}, 128)}}).Validate(); err != nil {
		t.Errorf("long key rejected: %v", err)
	}
}

func TestSecurityRejectsInvalidAndSharedKeys(t *testing.T) {
	proof, view := securityTestRings()
	for _, pair := range [][2]KeyRing{{{}, view}, {proof, {}}} {
		if _, err := NewSecurity(pair[0], pair[1]); err == nil {
			t.Error("invalid key ring accepted")
		}
	}
	view.Keys["old-view"] = append([]byte(nil), proof.Keys[proof.Active]...)
	if _, err := NewSecurity(proof, view); err == nil {
		t.Error("same material accepted under different key IDs and purposes")
	}
	proof.Keys["old-proof"] = bytes.Repeat([]byte{3}, 32)
	view.Keys["old-view"] = bytes.Repeat([]byte{3}, 32)
	if _, err := NewSecurity(proof, view); err == nil {
		t.Error("shared old key material accepted")
	}
}

func TestSecurityClonesKeyRings(t *testing.T) {
	proof, view := securityTestRings()
	security, err := NewSecurity(proof, view)
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Unix(2_000_000_000, 0)
	history := []Turn{{Role: "model", Content: "何を学んだ？"}}
	token := security.Sign(securityTestSessionID, "教材", history, expires)
	viewKey, err := security.ViewKey(securityTestSessionID, view.Active, 0)
	if err != nil {
		t.Fatal(err)
	}
	proof.Keys[proof.Active][0] ^= 0xff
	view.Keys[view.Active][0] ^= 0xff
	delete(proof.Keys, proof.Active)
	delete(view.Keys, view.Active)
	proof.Active = "changed"
	view.Active = "changed"
	if got := security.Sign(securityTestSessionID, "教材", history, expires); got != token {
		t.Error("caller mutation changed the signing key")
	}
	if got, err := security.ViewKey(securityTestSessionID, security.ActiveViewKeyID(), 0); err != nil || got != viewKey {
		t.Error("caller mutation changed the view key")
	}
}

func TestProofFixedPayload(t *testing.T) {
	security := securityForTest(t)
	expires := time.Unix(2_000_000_000, 123)
	history := []Turn{{Role: "model", Content: "何を学んだ？"}, {Role: "user", Content: "配列を学んだ。"}}
	token := security.Sign(securityTestSessionID, "配列\n順番", history, expires)
	payload := `{"purpose":"session-proof","version":1,"keyId":"proof-1","sessionId":"2417ac2f-3cd3-4a18-8dd9-82a0205e5912","expiresUnix":2000000000,"learningContent":"配列\n順番","history":[{"role":"model","content":"何を学んだ？"},{"role":"user","content":"配列を学んだ。"}]}`
	mac := hmac.New(sha256.New, bytes.Repeat([]byte{0x11}, 32))
	_, _ = mac.Write([]byte(payload))
	want := "s1.proof-1.2000000000." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if token != want {
		t.Fatal("proof payload differs from the fixed typed JSON format")
	}
	got, err := security.Verify(token, securityTestSessionID, "配列\n順番", history, expires.Add(-time.Second))
	if err != nil || !got.Equal(time.Unix(expires.Unix(), 0)) || got.Location() != time.UTC {
		t.Errorf("Verify expiration = %v, %v", got, err)
	}
	if strings.Contains(token, "配列") || strings.Contains(token, "何を学んだ") || strings.Contains(token, securityTestSessionID) {
		t.Error("proof exposes session content")
	}
}

func TestProofRejectsTampering(t *testing.T) {
	security := securityForTest(t)
	expires := time.Unix(2_000_000_000, 0)
	now := expires.Add(-time.Hour)
	history := []Turn{{Role: "model", Content: "何を学んだ？"}, {Role: "user", Content: "配列を学んだ。"}, {Role: "model", Content: "配列とは？"}}
	token := security.Sign(securityTestSessionID, "教材", history, expires)
	parts := strings.Split(token, ".")
	alteredParts := func(index int, value string) string {
		copyParts := append([]string(nil), parts...)
		copyParts[index] = value
		return strings.Join(copyParts, ".")
	}
	for name, test := range map[string]struct {
		token, id, content string
		history            []Turn
	}{
		"session":      {token, "different-session", "教材", history},
		"content":      {token, securityTestSessionID, "改変した教材", history},
		"order":        {token, securityTestSessionID, "教材", []Turn{history[2], history[1], history[0]}},
		"role":         {token, securityTestSessionID, "教材", []Turn{{Role: "user", Content: history[0].Content}, history[1], history[2]}},
		"turn content": {token, securityTestSessionID, "教材", []Turn{{Role: "model", Content: "偽の質問？"}, history[1], history[2]}},
		"removed turn": {token, securityTestSessionID, "教材", history[:2]},
		"added turn":   {token, securityTestSessionID, "教材", append(append([]Turn(nil), history...), Turn{Role: "user", Content: "追加"})},
		"version":      {alteredParts(0, "s2"), securityTestSessionID, "教材", history},
		"key":          {alteredParts(1, "unknown"), securityTestSessionID, "教材", history},
		"expiry":       {alteredParts(2, "2000000001"), securityTestSessionID, "教材", history},
		"tag":          {alteredParts(3, base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0}, 32))), securityTestSessionID, "教材", history},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := security.Verify(test.token, test.id, test.content, test.history, now)
			if !errors.Is(err, ErrInvalidSession) || !got.IsZero() {
				t.Errorf("tampering accepted: expiration = %v, error = %v", got, err)
			}
		})
	}
}

func TestProofRejectsNonCanonicalTokens(t *testing.T) {
	security := securityForTest(t)
	expires := time.Unix(2_000_000_000, 0)
	history := []Turn{{Role: "model", Content: "何を学んだ？"}}
	token := security.Sign(securityTestSessionID, "教材", history, expires)
	parts := strings.Split(token, ".")
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	lastIndex := strings.IndexByte(alphabet, parts[3][len(parts[3])-1])
	nonCanonicalTag := parts[3][:len(parts[3])-1] + string(alphabet[lastIndex+1])
	for name, malformed := range map[string]string{
		"empty":             "",
		"oversized":         strings.Repeat(".", 101),
		"missing segment":   strings.Join(parts[:3], "."),
		"extra segment":     token + ".extra",
		"space prefix":      " " + token,
		"space suffix":      token + " ",
		"uppercase version": strings.Replace(token, "s1.", "S1.", 1),
		"leading zero":      strings.Replace(token, ".2000000000.", ".02000000000.", 1),
		"positive sign":     strings.Replace(token, ".2000000000.", ".+2000000000.", 1),
		"floating expiry":   strings.Replace(token, ".2000000000.", ".2e9.", 1),
		"overflow expiry":   strings.Replace(token, ".2000000000.", ".9223372036854775808.", 1),
		"padded tag":        token + "=",
		"newline in tag":    strings.Join(parts[:3], ".") + "." + parts[3][:10] + "\n" + parts[3][10:],
		"nonzero pad bits":  strings.Join(parts[:3], ".") + "." + nonCanonicalTag,
		"short tag":         token[:len(token)-1],
		"long tag":          token + "A",
		"empty tag":         strings.Join(parts[:3], ".") + ".",
		"invalid tag":       strings.Join(parts[:3], ".") + "." + strings.Repeat("!", 43),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := security.Verify(malformed, securityTestSessionID, "教材", history, expires.Add(-time.Hour)); !errors.Is(err, ErrInvalidSession) {
				t.Errorf("malformed proof accepted: %v", err)
			}
		})
	}
}

func TestProofExpirationBoundary(t *testing.T) {
	security := securityForTest(t)
	expires := time.Unix(2_000_000_000, 0)
	history := []Turn{{Role: "model", Content: "何を学んだ？"}}
	token := security.Sign(securityTestSessionID, "教材", history, expires)
	if _, err := security.Verify(token, securityTestSessionID, "教材", history, expires.Add(-time.Nanosecond)); err != nil {
		t.Errorf("valid proof rejected before expiration: %v", err)
	}
	for _, now := range []time.Time{expires, expires.Add(time.Nanosecond), expires.Add(time.Hour)} {
		if _, err := security.Verify(token, securityTestSessionID, "教材", history, now); !errors.Is(err, ErrInvalidSession) {
			t.Errorf("expired proof accepted at %v: %v", now, err)
		}
	}
}

func TestProofBindsKeyID(t *testing.T) {
	proof, view := securityTestRings()
	proof.Keys["proof-2"] = append([]byte(nil), proof.Keys[proof.Active]...)
	security, err := NewSecurity(proof, view)
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Unix(2_000_000_000, 0)
	history := []Turn{{Role: "model", Content: "何を学んだ？"}}
	token := security.Sign(securityTestSessionID, "教材", history, expires)
	token = strings.Replace(token, ".proof-1.", ".proof-2.", 1)
	if _, err := security.Verify(token, securityTestSessionID, "教材", history, expires.Add(-time.Second)); !errors.Is(err, ErrInvalidSession) {
		t.Error("proof is not bound to its key ID")
	}
}

func TestSecurityKeyRotation(t *testing.T) {
	proof, view := securityTestRings()
	oldSecurity, err := NewSecurity(proof, view)
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Unix(2_000_000_000, 0)
	history := []Turn{{Role: "model", Content: "何を学んだ？"}}
	oldProof := oldSecurity.Sign(securityTestSessionID, "教材", history, expires)
	oldView, err := oldSecurity.ViewKey(securityTestSessionID, "view-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	proof.Keys["proof-2"] = bytes.Repeat([]byte{0x33}, 32)
	proof.Active = "proof-2"
	view.Keys["view-2"] = bytes.Repeat([]byte{0x44}, 32)
	view.Active = "view-2"
	rotated, err := NewSecurity(proof, view)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rotated.Verify(oldProof, securityTestSessionID, "教材", history, expires.Add(-time.Second)); err != nil {
		t.Errorf("retained old proof rejected: %v", err)
	}
	if got, err := rotated.ViewKey(securityTestSessionID, "view-1", 0); err != nil || got != oldView {
		t.Error("retained old view key cannot be recreated")
	}
	newProof := rotated.Sign(securityTestSessionID, "教材", history, expires)
	if !strings.HasPrefix(newProof, "s1.proof-2.") {
		t.Error("new proof does not use the active key")
	}
	if got, err := rotated.ViewKey(securityTestSessionID, rotated.ActiveViewKeyID(), 0); err != nil || got == oldView || rotated.ActiveViewKeyID() != "view-2" {
		t.Error("new view key does not use the active key")
	}
	delete(proof.Keys, "proof-1")
	delete(view.Keys, "view-1")
	retired, err := NewSecurity(proof, view)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := retired.Verify(oldProof, securityTestSessionID, "教材", history, expires.Add(-time.Second)); !errors.Is(err, ErrInvalidSession) {
		t.Error("removed proof key still accepted")
	}
	if _, err := retired.ViewKey(securityTestSessionID, "view-1", 0); err == nil {
		t.Error("removed view key still accepted")
	}
}

func TestViewKeyHMACVectors(t *testing.T) {
	security := securityForTest(t)
	for nonce, want := range map[int64]string{0: "NYFWFDQSGQWK", 1: "ZFRGHPGBG3N4", math.MaxInt64: "10QES33B57SV"} {
		got, err := security.ViewKey(securityTestSessionID, "view-1", nonce)
		if err != nil || got != want {
			t.Errorf("view key differs from first 60 HMAC bits for nonce %d: %v", nonce, err)
		}
	}
	if _, err := security.ViewKey(securityTestSessionID, "unknown", 0); err == nil {
		t.Error("unknown view key ID accepted")
	}
	if _, err := security.ViewKey(securityTestSessionID, "view-1", -1); err == nil {
		t.Error("negative collision nonce accepted")
	}
	if got, err := security.ViewKey("other-session", "view-1", 0); err != nil || got == "NYFWFDQSGQWK" {
		t.Error("view key is not bound to session")
	}
}

func TestNormalizeAndFormatViewKey(t *testing.T) {
	for input, want := range map[string]string{
		"7KQM3XWD9HTR":                     "7KQM3XWD9HTR",
		"7kqm-3xwd-9htr":                   "7KQM3XWD9HTR",
		"\t7 K Q M\n3XWD\r9HTR\u3000":      "7KQM3XWD9HTR",
		"\u00a07KQM-3XWD-9HTR\u2028\u2029": "7KQM3XWD9HTR",
		"Iilo-0O11-Abcd":                   "11100011ABCD",
	} {
		got, err := NormalizeViewKey(input)
		if err != nil || got != want {
			t.Errorf("normalized key mismatch: %v", err)
		}
		formatted := FormatViewKey(input)
		if formatted != want[:4]+"-"+want[4:8]+"-"+want[8:] {
			t.Error("formatted key mismatch")
		}
		if roundTrip, err := NormalizeViewKey(formatted); err != nil || roundTrip != want {
			t.Error("formatted key cannot be normalized")
		}
	}
	for _, invalid := range []string{
		"", "--- \t\n", "7KQM3XWD9HT", "7KQM3XWD9HTRR", "7KQM-3XWD-9HTU", "7KQM_3XWD_9HTR",
		"7KQM—3XWD—9HTR", "７KQM3XWD9HTR", "7KQM3XWD9HTＲ", "7KQM\x003XWD9HTR", "7KQM\u200b3XWD9HTR",
		"7KQM3XWD9HTR", "7KQM3XWD9HT\xff",
	} {
		if got, err := NormalizeViewKey(invalid); err == nil || got != "" {
			t.Error("invalid view key accepted")
		}
		if got := FormatViewKey(invalid); got != "" {
			t.Error("invalid view key formatted")
		}
	}
}

func TestPayloadDigestAndKeyHash(t *testing.T) {
	history := []Turn{{Role: "model", Content: "何を学んだ？"}, {Role: "user", Content: "配列を学んだ。"}}
	payload := `{"learningContent":"教材","history":[{"role":"model","content":"何を学んだ？"},{"role":"user","content":"配列を学んだ。"}]}`
	want := sha256.Sum256([]byte(payload))
	digest := PayloadDigest("教材", history)
	if digest != hex.EncodeToString(want[:]) || len(digest) != 64 {
		t.Error("payload digest differs from canonical typed JSON")
	}
	if PayloadDigest("別の教材", history) == digest || PayloadDigest("教材", []Turn{history[1], history[0]}) == digest || PayloadDigest("教材", history[:1]) == digest {
		t.Error("payload digest is not bound to content and ordered history")
	}
	changedRole := []Turn{{Role: "user", Content: history[0].Content}, history[1]}
	changedContent := []Turn{{Role: "model", Content: "改変した質問？"}, history[1]}
	if PayloadDigest("教材", changedRole) == digest || PayloadDigest("教材", changedContent) == digest {
		t.Error("payload digest is not bound to every turn field")
	}
	raw := "7KQM3XWD9HTR"
	keyDigest := sha256.Sum256([]byte(raw))
	if got := KeyHash(raw); got != hex.EncodeToString(keyDigest[:]) || len(got) != 64 {
		t.Error("view key hash differs from lowercase SHA-256 hex")
	}
}
