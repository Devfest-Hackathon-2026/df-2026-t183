package speech

import (
	"context"
	"errors"
)

const (
	MaxBody       = 5 << 20
	MaxDuration   = 31
	MaxTranscript = 1000
)

var (
	ErrInvalid     = errors.New("invalid audio")
	ErrTooLarge    = errors.New("audio exceeds limits")
	ErrUnsupported = errors.New("unsupported audio")
	ErrInternal    = errors.New("speech service is not configured")
	ErrFailed      = errors.New("speech recognition failed")
	ErrUnavailable = errors.New("speech service is unavailable")
)

type Validator interface {
	Validate(context.Context, []byte, string) error
}

type Transcriber interface {
	Transcribe(context.Context, []byte) (string, error)
}
