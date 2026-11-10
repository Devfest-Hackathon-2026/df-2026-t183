package store

import (
	"cloud.google.com/go/firestore"
	"context"
	"github.com/yasusudas/ai-ni-oshieru/backend/internal/learning"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"time"
)

type firestoreDB struct{ client *firestore.Client }
type firestoreReader struct {
	ctx    context.Context
	client *firestore.Client
	tx     *firestore.Transaction
}

func New(client *firestore.Client, security *learning.Security, ttl time.Duration) *Store {
	return &Store{db: &firestoreDB{client}, security: security, ttl: ttl, now: time.Now}
}
func (d *firestoreDB) read(ctx context.Context, f func(reader) error) error {
	return d.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		return f(&firestoreReader{ctx, d.client, tx})
	}, firestore.ReadOnly)
}
func (d *firestoreDB) transact(ctx context.Context, f func(transaction) error) error {
	return d.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		return f(&firestoreReader{ctx, d.client, tx})
	})
}
func (r *firestoreReader) get(collection, id string, out any) (bool, error) {
	doc, e := r.tx.Get(r.client.Collection(collection).Doc(id))
	if status.Code(e) == codes.NotFound {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	return true, doc.DataTo(out)
}
func (r *firestoreReader) record(id string) (*learning.Record, error) {
	var out learning.Record
	ok, e := r.get("learningSessions", id, &out)
	if e != nil || !ok {
		return nil, e
	}
	out.SessionID = id
	return &out, nil
}
func (r *firestoreReader) key(id string) (*keyRecord, error) {
	var out keyRecord
	ok, e := r.get("viewKeys", id, &out)
	if e != nil || !ok {
		return nil, e
	}
	return &out, nil
}
func (r *firestoreReader) createRecord(id string, v learning.Record) error {
	return r.tx.Create(r.client.Collection("learningSessions").Doc(id), v)
}
func (r *firestoreReader) createKey(id string, v keyRecord) error {
	return r.tx.Create(r.client.Collection("viewKeys").Doc(id), v)
}
