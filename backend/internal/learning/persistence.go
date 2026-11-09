package learning

import (
	"context"
	"errors"
	"time"
)

var (
	ErrAlreadyFinished = errors.New("session already finished")
	ErrNotFound        = errors.New("record not found")
)

type Record struct {
	SessionID       string    `json:"sessionId" firestore:"-"`
	SchemaVersion   int       `json:"-" firestore:"schemaVersion"`
	State           string    `json:"-" firestore:"state"`
	LearningContent string    `json:"learningContent" firestore:"learningContent"`
	History         []Turn    `json:"history" firestore:"history"`
	Summary         Summary   `json:"summary" firestore:"summary"`
	PayloadDigest   string    `json:"-" firestore:"payloadDigest"`
	SavedAt         time.Time `json:"savedAt" firestore:"savedAt"`
	ExpiresAt       time.Time `json:"expiresAt" firestore:"expiresAt"`
	ProofExpiresAt  time.Time `json:"-" firestore:"proofExpiresAt"`
	ViewKeyID       string    `json:"-" firestore:"viewKeyId"`
	ViewKeyNonce    int64     `json:"-" firestore:"viewKeyNonce"`
}

type Repository interface {
	Lookup(context.Context, string, string, time.Time) (*Record, error)
	Commit(context.Context, Record) (*Record, error)
	Open(context.Context, string) (*Record, error)
}
