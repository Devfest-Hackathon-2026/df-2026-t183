package store

import (
	"context"
	"errors"
	"github.com/yasusudas/ai-ni-oshieru/backend/internal/learning"
	"reflect"
	"sync"
	"testing"
	"time"
)

type memoryDB struct {
	sync.Mutex
	records     map[string]learning.Record
	keys        map[string]keyRecord
	fail        bool
	lost        bool
	failKey     bool
	beforeRetry func()
	attempts    int
}
type memoryTx struct {
	records map[string]learning.Record
	keys    map[string]keyRecord
	failKey bool
}

func (m *memoryTx) record(id string) (*learning.Record, error) {
	r, ok := m.records[id]
	if !ok {
		return nil, nil
	}
	return &r, nil
}
func (m *memoryTx) key(id string) (*keyRecord, error) {
	r, ok := m.keys[id]
	if !ok {
		return nil, nil
	}
	return &r, nil
}
func (m *memoryTx) createRecord(id string, r learning.Record) error { m.records[id] = r; return nil }
func (m *memoryTx) createKey(id string, r keyRecord) error {
	if m.failKey {
		return errors.New("key create failed")
	}
	m.keys[id] = r
	return nil
}
func (m *memoryDB) read(_ context.Context, f func(reader) error) error {
	m.Lock()
	defer m.Unlock()
	if m.fail {
		return errors.New("offline")
	}
	return f(&memoryTx{records: m.records, keys: m.keys})
}
func (m *memoryDB) transact(_ context.Context, f func(transaction) error) error {
	m.Lock()
	defer m.Unlock()
	if m.fail {
		return errors.New("offline")
	}
	for {
		tx := &memoryTx{records: map[string]learning.Record{}, keys: map[string]keyRecord{}, failKey: m.failKey}
		for k, v := range m.records {
			tx.records[k] = v
		}
		for k, v := range m.keys {
			tx.keys[k] = v
		}
		m.attempts++
		if err := f(tx); err != nil {
			return err
		}
		if m.beforeRetry != nil {
			hook := m.beforeRetry
			m.beforeRetry = nil
			hook()
			continue
		}
		m.records = tx.records
		m.keys = tx.keys
		break
	}
	if m.lost {
		return errors.New("commit response lost")
	}
	return nil
}
func fixture(t *testing.T) (*Store, *memoryDB, learning.Record) {
	t.Helper()
	a, b := make([]byte, 32), make([]byte, 32)
	a[0] = 1
	b[0] = 2
	security, err := learning.NewSecurity(learning.KeyRing{Active: "s", Keys: map[string][]byte{"s": a}}, learning.KeyRing{Active: "v", Keys: map[string][]byte{"v": b}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	db := &memoryDB{records: map[string]learning.Record{}, keys: map[string]keyRecord{}}
	s := &Store{db: db, security: security, ttl: 30 * 24 * time.Hour, now: func() time.Time { return now }}
	r := learning.Record{SessionID: "test-session", LearningContent: "教材", History: []learning.Turn{{Role: "model", Content: "質問？"}, {Role: "user", Content: "学習者の説明です"}}, Summary: learning.Summary{Understood: []learning.Understood{}, Review: []learning.Review{}}, ProofExpiresAt: now.Add(2 * time.Hour)}
	r.PayloadDigest = learning.PayloadDigest(r.LearningContent, r.History)
	return s, db, r
}
func TestCommitReplayAndOpen(t *testing.T) {
	s, db, r := fixture(t)
	ctx := context.Background()
	first, e := s.Commit(ctx, r)
	if e != nil {
		t.Fatal(e)
	}
	r.Summary.Review = []learning.Review{{Kind: "unverified", Text: "後発の生成結果"}}
	again, e := s.Commit(ctx, r)
	if e != nil || !reflect.DeepEqual(first, again) {
		t.Fatal("replay changed", e)
	}
	key, _ := s.security.ViewKey(r.SessionID, first.ViewKeyID, first.ViewKeyNonce)
	opened, e := s.Open(ctx, learning.FormatViewKey(key))
	if e != nil || !reflect.DeepEqual(first, opened) {
		t.Fatal("open", e)
	}
	if len(db.records) != 1 || len(db.keys) != 1 {
		t.Fatal("duplicates")
	}
	r.PayloadDigest = "different"
	if _, e = s.Commit(ctx, r); !errors.Is(e, learning.ErrAlreadyFinished) {
		t.Fatal(e)
	}
	s.now = func() time.Time { return first.ExpiresAt }
	if _, e = s.Open(ctx, key); !errors.Is(e, learning.ErrNotFound) {
		t.Fatal(e)
	}
}
func TestConcurrentCommitAndLostResponse(t *testing.T) {
	s, db, r := fixture(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := s.Commit(ctx, r); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if len(db.records) != 1 || len(db.keys) != 1 {
		t.Fatal("duplicates")
	}
	s, db, r = fixture(t)
	db.lost = true
	if out, e := s.Commit(ctx, r); e == nil || out != nil {
		t.Fatal("uncertain commit succeeded")
	}
	db.lost = false
	out, e := s.Lookup(ctx, r.SessionID, r.PayloadDigest, r.ProofExpiresAt)
	if e != nil || out == nil {
		t.Fatal("retry failed", e)
	}
}
func TestCollisionExpiryDeletionAndFailure(t *testing.T) {
	s, db, r := fixture(t)
	ctx := context.Background()
	key, _ := s.security.ViewKey(r.SessionID, "v", 0)
	db.keys[learning.KeyHash(key)] = keyRecord{SessionID: "other"}
	out, e := s.Commit(ctx, r)
	if e != nil || out.ViewKeyNonce != 1 {
		t.Fatal("collision", e)
	}
	db.records[r.SessionID] = learning.Record{State: "deleted"}
	if _, e = s.Commit(ctx, r); !errors.Is(e, learning.ErrAlreadyFinished) {
		t.Fatal(e)
	}
	s.now = func() time.Time { return r.ProofExpiresAt }
	if _, e = s.Lookup(ctx, r.SessionID, r.PayloadDigest, r.ProofExpiresAt); !errors.Is(e, learning.ErrInvalidSession) {
		t.Fatal(e)
	}
	s, db, r = fixture(t)
	db.fail = true
	if out, e = s.Commit(ctx, r); e == nil || out != nil || len(db.records) != 0 {
		t.Fatal("failure saved")
	}
	if _, e = s.Open(ctx, "invalid"); !errors.Is(e, learning.ErrNotFound) {
		t.Fatal(e)
	}
}
func TestExpiryBeforeCommitAndKeyMismatch(t *testing.T) {
	s, db, r := fixture(t)
	s.now = func() time.Time { return r.ProofExpiresAt }
	if _, e := s.Commit(context.Background(), r); !errors.Is(e, learning.ErrInvalidSession) || len(db.records) != 0 {
		t.Fatal(e)
	}
	s, db, r = fixture(t)
	out, e := s.Commit(context.Background(), r)
	if e != nil {
		t.Fatal(e)
	}
	key, _ := s.security.ViewKey(r.SessionID, out.ViewKeyID, 0)
	out.ViewKeyNonce = 1
	db.records[r.SessionID] = *out
	if _, e = s.Open(context.Background(), key); !errors.Is(e, learning.ErrNotFound) {
		t.Fatal(e)
	}
}

func TestTransactionRetryAdoptsCommittedWinner(t *testing.T) {
	s, db, candidate := fixture(t)
	winner := candidate
	winner.SchemaVersion = 1
	winner.State = "saved"
	winner.ViewKeyID = "v"
	winner.SavedAt = s.now()
	winner.ExpiresAt = winner.SavedAt.Add(s.ttl)
	winner.Summary.Review = []learning.Review{{Kind: "unverified", Text: "先に確定した振り返り"}}
	key, err := s.security.ViewKey(winner.SessionID, winner.ViewKeyID, 0)
	if err != nil {
		t.Fatal(err)
	}
	db.beforeRetry = func() {
		db.records[winner.SessionID] = winner
		db.keys[learning.KeyHash(key)] = keyRecord{winner.SessionID, winner.ExpiresAt}
	}
	got, err := s.Commit(context.Background(), candidate)
	if err != nil || db.attempts != 2 || !reflect.DeepEqual(got, &winner) || len(db.records) != 1 || len(db.keys) != 1 {
		t.Fatal("retry did not adopt winner", err, db.attempts)
	}
}
func TestTransactionRetryRejectsExpiredProof(t *testing.T) {
	s, db, candidate := fixture(t)
	db.beforeRetry = func() { s.now = func() time.Time { return candidate.ProofExpiresAt } }
	got, err := s.Commit(context.Background(), candidate)
	if !errors.Is(err, learning.ErrInvalidSession) || got != nil || db.attempts != 2 || len(db.records) != 0 || len(db.keys) != 0 {
		t.Fatal("expired retry committed", err, db.attempts)
	}
}
func TestKeyCreationFailureRollsBackRecord(t *testing.T) {
	s, db, candidate := fixture(t)
	db.failKey = true
	got, err := s.Commit(context.Background(), candidate)
	if err == nil || got != nil || len(db.records) != 0 || len(db.keys) != 0 {
		t.Fatal("partial write escaped transaction")
	}
	db.failKey = false
	if _, err = s.Commit(context.Background(), candidate); err != nil || len(db.records) != 1 || len(db.keys) != 1 {
		t.Fatal("retry after rollback", err)
	}
}
func TestStoredRecordSurvivesKeyRotation(t *testing.T) {
	s, _, candidate := fixture(t)
	ctx := context.Background()
	saved, err := s.Commit(ctx, candidate)
	if err != nil {
		t.Fatal(err)
	}
	originalKey, err := s.security.ViewKey(saved.SessionID, saved.ViewKeyID, saved.ViewKeyNonce)
	if err != nil {
		t.Fatal(err)
	}
	proof, oldView, newView := make([]byte, 32), make([]byte, 32), make([]byte, 32)
	proof[0] = 1
	oldView[0] = 2
	newView[0] = 3
	s.security, err = learning.NewSecurity(learning.KeyRing{Active: "s", Keys: map[string][]byte{"s": proof}}, learning.KeyRing{Active: "v2", Keys: map[string][]byte{"v": oldView, "v2": newView}})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.Commit(ctx, candidate)
	if err != nil || !reflect.DeepEqual(replay, saved) {
		t.Fatal("rotation changed saved response", err)
	}
	replayKey, err := s.security.ViewKey(replay.SessionID, replay.ViewKeyID, replay.ViewKeyNonce)
	if err != nil || replayKey != originalKey {
		t.Fatal("rotation changed view key", err)
	}
	opened, err := s.Open(ctx, originalKey)
	if err != nil || !reflect.DeepEqual(opened, saved) {
		t.Fatal("old view key lost access", err)
	}
	candidate.SessionID = "next-session"
	fresh, err := s.Commit(ctx, candidate)
	if err != nil || fresh.ViewKeyID != "v2" {
		t.Fatal("new record did not use active key", err)
	}
}
