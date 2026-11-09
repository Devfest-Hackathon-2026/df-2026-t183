package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/yasusudas/ai-ni-oshieru/backend/internal/jsoncheck"
	"github.com/yasusudas/ai-ni-oshieru/backend/internal/learning"
	"os"
	"strconv"
	"time"
)

func parseKeyRing(raw string) (learning.KeyRing, error) {
	invalid := errors.New("鍵設定の形式が不正です")
	var fields map[string]json.RawMessage
	if !jsoncheck.Valid([]byte(raw)) || json.Unmarshal([]byte(raw), &fields) != nil || len(fields) != 2 || fields["activeKeyId"] == nil || fields["keys"] == nil {
		return learning.KeyRing{}, invalid
	}
	var input struct {
		Active string            `json:"activeKeyId"`
		Keys   map[string]string `json:"keys"`
	}
	if json.Unmarshal([]byte(raw), &input) != nil {
		return learning.KeyRing{}, invalid
	}
	ring := learning.KeyRing{Active: input.Active, Keys: map[string][]byte{}}
	for id, value := range input.Keys {
		key, err := base64.StdEncoding.Strict().DecodeString(value)
		if err != nil {
			return ring, invalid
		}
		ring.Keys[id] = key
	}
	if err := ring.Validate(); err != nil {
		return learning.KeyRing{}, invalid
	}
	return ring, nil
}
func persistenceConfig() (*learning.Security, time.Duration, error) {
	proofConfig := os.Getenv("SESSION_KEYS_JSON")
	viewConfig := os.Getenv("HISTORY_KEYS_JSON")
	if proofConfig == "" && viewConfig == "" {
		return nil, 0, nil
	}
	proof, err := parseKeyRing(proofConfig)
	if err != nil {
		return nil, 0, errors.New("SESSION_KEYS_JSON を設定してください")
	}
	view, err := parseKeyRing(viewConfig)
	if err != nil {
		return nil, 0, errors.New("HISTORY_KEYS_JSON を設定してください")
	}
	security, err := learning.NewSecurity(proof, view)
	if err != nil {
		return nil, 0, errors.New("署名と閲覧に用途別の鍵を設定してください")
	}
	days := 30
	if value := os.Getenv("HISTORY_TTL_DAYS"); value != "" {
		days, err = strconv.Atoi(value)
		if err != nil || days < 1 || days > 3650 {
			return nil, 0, errors.New("HISTORY_TTL_DAYS は 1〜3650 の整数にしてください")
		}
	}
	return security, time.Duration(days) * 24 * time.Hour, nil
}
