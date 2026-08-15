// Package journal owns the durable, replayable representation of accepted OTLP
// exports. Query projections are deliberately outside this package.
package journal

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/klauspost/compress/zstd"
)

type Codec string

const (
	CodecIdentity   Codec = "identity"
	CodecZstd       Codec = "zstd"
	MaxPayloadBytes       = 32 << 20
)

var ErrInvalidPayload = errors.New("invalid journal payload")

// Payload is a verified stored representation of acknowledged protobuf bytes.
type Payload struct {
	codec        Codec
	stored       []byte
	originalSize int
	sha256       [sha256.Size]byte
}

func Encode(raw []byte) (Payload, error) {
	if len(raw) > MaxPayloadBytes {
		return Payload{}, fmt.Errorf("%w: original size %d exceeds %d", ErrInvalidPayload, len(raw), MaxPayloadBytes)
	}
	hash := sha256.Sum256(raw)
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest))
	if err != nil {
		return Payload{}, fmt.Errorf("create zstd encoder: %w", err)
	}
	compressed := encoder.EncodeAll(raw, nil)
	encoder.Close()
	codec, stored := CodecZstd, compressed
	if len(compressed) >= len(raw) {
		codec, stored = CodecIdentity, raw
	}
	return Payload{codec: codec, stored: bytes.Clone(stored), originalSize: len(raw), sha256: hash}, nil
}

func Restore(codec Codec, stored []byte, originalSize int, wantHash [sha256.Size]byte) ([]byte, error) {
	if originalSize < 0 || originalSize > MaxPayloadBytes {
		return nil, fmt.Errorf("%w: original size %d exceeds limit", ErrInvalidPayload, originalSize)
	}
	var raw []byte
	switch codec {
	case CodecIdentity:
		if len(stored) != originalSize {
			return nil, fmt.Errorf("%w: stored identity size %d, want %d", ErrInvalidPayload, len(stored), originalSize)
		}
		raw = bytes.Clone(stored)
	case CodecZstd:
		decoder, err := zstd.NewReader(nil, zstd.WithDecoderMaxMemory(MaxPayloadBytes*2))
		if err != nil {
			return nil, fmt.Errorf("create zstd decoder: %w", err)
		}
		raw, err = decoder.DecodeAll(stored, make([]byte, 0, originalSize))
		decoder.Close()
		if err != nil {
			return nil, fmt.Errorf("%w: decode zstd: %v", ErrInvalidPayload, err)
		}
	default:
		return nil, fmt.Errorf("%w: unsupported codec %q", ErrInvalidPayload, codec)
	}
	if len(raw) != originalSize {
		return nil, fmt.Errorf("%w: decoded size %d, want %d", ErrInvalidPayload, len(raw), originalSize)
	}
	if got := sha256.Sum256(raw); got != wantHash {
		return nil, fmt.Errorf("%w: sha256 mismatch", ErrInvalidPayload)
	}
	return raw, nil
}

func (payload Payload) Codec() Codec              { return payload.codec }
func (payload Payload) Bytes() []byte             { return bytes.Clone(payload.stored) }
func (payload Payload) OriginalSize() int         { return payload.originalSize }
func (payload Payload) SHA256() [sha256.Size]byte { return payload.sha256 }
func (payload Payload) Decode() ([]byte, error) {
	return Restore(payload.codec, payload.stored, payload.originalSize, payload.sha256)
}
