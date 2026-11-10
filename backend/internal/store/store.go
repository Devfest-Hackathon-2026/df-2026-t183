package store

import (
	"context"
	"crypto/subtle"
	"errors"
	"time"

	"github.com/yasusudas/ai-ni-oshieru/backend/internal/learning"
)

type keyRecord struct {
	SessionID string    `firestore:"sessionId"`
	ExpiresAt time.Time `firestore:"expiresAt"`
}
type reader interface {
	record(string) (*learning.Record, error)
	key(string) (*keyRecord, error)
}
type transaction interface {
	reader
	createRecord(string, learning.Record) error
	createKey(string, keyRecord) error
}
type database interface {
	read(context.Context, func(reader) error) error
	transact(context.Context, func(transaction) error) error
}
type Store struct {
	db       database
	security *learning.Security
	ttl      time.Duration
	now      func() time.Time
}

func (s *Store) existing(r *learning.Record, digest string, expiry time.Time) (*learning.Record, error) {
	if !s.now().Before(expiry) {
		return nil, learning.ErrInvalidSession
	}
	if r == nil {
		return nil, nil
	}
	if r.State != "saved" || r.PayloadDigest != digest {
		return nil, learning.ErrAlreadyFinished
	}
	return r, nil
}
func (s *Store) Lookup(ctx context.Context, id, digest string, expiry time.Time) (out *learning.Record, err error) {
	err = s.db.read(ctx, func(rd reader) error {
		r, e := rd.record(id)
		if e != nil {
			return e
		}
		out, e = s.existing(r, digest, expiry)
		return e
	})
	return
}
func (s *Store) Commit(ctx context.Context, candidate learning.Record) (out *learning.Record, err error) {
	err = s.db.transact(ctx, func(tx transaction) error {
		out = nil
		r, e := tx.record(candidate.SessionID)
		if e != nil {
			return e
		}
		r, e = s.existing(r, candidate.PayloadDigest, candidate.ProofExpiresAt)
		if e != nil {
			return e
		}
		if r != nil {
			out = r
			return nil
		}
		candidate.SchemaVersion = 1
		candidate.State = "saved"
		candidate.ViewKeyID = s.security.ActiveViewKeyID()
		candidate.SavedAt = s.now().UTC().Truncate(time.Second)
		candidate.ExpiresAt = candidate.SavedAt.Add(s.ttl)
		for n := int64(0); n < 100; n++ {
			key, e := s.security.ViewKey(candidate.SessionID, candidate.ViewKeyID, n)
			if e != nil {
				return e
			}
			hash := learning.KeyHash(key)
			taken, e := tx.key(hash)
			if e != nil {
				return e
			}
			if taken != nil {
				continue
			}
			if !s.now().Before(candidate.ProofExpiresAt) {
				return learning.ErrInvalidSession
			}
			candidate.ViewKeyNonce = n
			if e = tx.createRecord(candidate.SessionID, candidate); e != nil {
				return e
			}
			if e = tx.createKey(hash, keyRecord{candidate.SessionID, candidate.ExpiresAt}); e != nil {
				return e
			}
			out = &candidate
			return nil
		}
		return errors.New("view key allocation failed")
	})
	if err != nil {
		return nil, err
	}
	return
}
func (s *Store) Open(ctx context.Context, input string) (out *learning.Record, err error) {
	key, e := learning.NormalizeViewKey(input)
	if e != nil {
		return nil, learning.ErrNotFound
	}
	err = s.db.read(ctx, func(rd reader) error {
		k, e := rd.key(learning.KeyHash(key))
		if e != nil {
			return e
		}
		if k == nil || !s.now().Before(k.ExpiresAt) {
			return learning.ErrNotFound
		}
		r, e := rd.record(k.SessionID)
		if e != nil {
			return e
		}
		if r == nil || r.State != "saved" || !s.now().Before(r.ExpiresAt) {
			return learning.ErrNotFound
		}
		expected, e := s.security.ViewKey(r.SessionID, r.ViewKeyID, r.ViewKeyNonce)
		if e != nil {
			return e
		}
		if subtle.ConstantTimeCompare([]byte(key), []byte(expected)) != 1 {
			return learning.ErrNotFound
		}
		out = r
		return nil
	})
	return
}
