package main

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestParseKeyRing(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 32)))
	valid := `{"activeKeyId":"one","keys":{"one":"` + key + `"}}`
	if _, err := parseKeyRing(valid); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"", `null`, `{}`, `{"activeKeyId":"one","keys":null}`, `{"activeKeyId":"one","keys":{"one":"bad"}}`, strings.Replace(valid, `"one","keys"`, `"missing","keys"`, 1), strings.Replace(valid, `"activeKeyId":"one"`, `"activeKeyId":"one","activeKeyId":"two"`, 1)} {
		if _, err := parseKeyRing(value); err == nil {
			t.Fatal("accepted malformed config")
		}
	}
}
func TestPersistenceConfig(t *testing.T) {
	t.Setenv("SESSION_KEYS_JSON", "")
	t.Setenv("HISTORY_KEYS_JSON", "")
	if security, _, err := persistenceConfig(); security != nil || err != nil {
		t.Fatalf("persistence should be disabled with no keys: %v", err)
	}
	key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 32)))
	other := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("y", 32)))
	t.Setenv("SESSION_KEYS_JSON", `{"activeKeyId":"s","keys":{"s":"`+key+`"}}`)
	if _, _, err := persistenceConfig(); err == nil {
		t.Fatal("accepted config with only one key ring")
	}
	t.Setenv("HISTORY_KEYS_JSON", `{"activeKeyId":"v","keys":{"v":"`+other+`"}}`)
	t.Setenv("HISTORY_TTL_DAYS", "")
	if _, ttl, err := persistenceConfig(); err != nil || ttl.Hours() != 720 {
		t.Fatal(ttl, err)
	}
	for _, value := range []string{"0", "-1", "3651", "bad"} {
		t.Setenv("HISTORY_TTL_DAYS", value)
		if _, _, err := persistenceConfig(); err == nil {
			t.Fatal("accepted TTL")
		}
	}
}
