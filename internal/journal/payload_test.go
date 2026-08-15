package journal

import (
	"bytes"
	"crypto/sha256"
	"testing"
)

func TestEncodeRoundTripsCompressiblePayload(t *testing.T) {
	raw := bytes.Repeat([]byte("repeated OTLP resource and scope metadata\n"), 512)

	payload, err := Encode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if payload.Codec() != CodecZstd {
		t.Fatalf("codec = %q, want %q", payload.Codec(), CodecZstd)
	}
	if len(payload.Bytes()) >= len(raw)/2 {
		t.Fatalf("compressed payload = %d bytes, raw = %d bytes", len(payload.Bytes()), len(raw))
	}
	decoded, err := payload.Decode()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, raw) {
		t.Fatal("decoded payload differs from acknowledged bytes")
	}
}

func TestRestoreRejectsIdentitySizeBeforeCopying(t *testing.T) {
	stored := bytes.Repeat([]byte{1}, MaxPayloadBytes+1)
	if _, err := Restore(CodecIdentity, stored, 1, sha256.Sum256([]byte{1})); err == nil {
		t.Fatal("mismatched identity payload decoded successfully")
	}
}

func TestEncodeKeepsTinyPayloadAsIdentity(t *testing.T) {
	raw := []byte{0x0a, 0x00}

	payload, err := Encode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if payload.Codec() != CodecIdentity {
		t.Fatalf("codec = %q, want %q", payload.Codec(), CodecIdentity)
	}
}

func TestStoredPayloadRejectsCorruptionAndUnsafeSizes(t *testing.T) {
	raw := bytes.Repeat([]byte("compress me"), 128)
	payload, err := Encode(raw)
	if err != nil {
		t.Fatal(err)
	}
	corrupt := payload.Bytes()
	corrupt[len(corrupt)/2] ^= 0xff

	if _, err := Restore(payload.Codec(), corrupt, payload.OriginalSize(), payload.SHA256()); err == nil {
		t.Fatal("corrupt payload decoded successfully")
	}
	if _, err := Restore(CodecZstd, payload.Bytes(), MaxPayloadBytes+1, payload.SHA256()); err == nil {
		t.Fatal("oversized payload decoded successfully")
	}
	if _, err := Restore("future", payload.Bytes(), payload.OriginalSize(), payload.SHA256()); err == nil {
		t.Fatal("unknown codec decoded successfully")
	}
}
