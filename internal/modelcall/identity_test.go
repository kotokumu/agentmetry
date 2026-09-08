package modelcall_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/kotokumu/agentmetry/internal/modelcall"
)

func TestClaudeCallIDGolden(t *testing.T) {
	got := modelcall.CallID("claude", "sess-1", modelcall.IdentityClaudeClientRequestID, modelcall.StringAlias("req-b"))
	want := "ea58ba8b5153a54a619ab874a049bc5dcf78aa6b08606f5da24263c1580cb595"
	if got != want {
		t.Fatalf("CallID() = %q, want %q", got, want)
	}
}

func TestJournalLocatorAndCodexCallIDGolden(t *testing.T) {
	payloadHash := make([]byte, sha256.Size)
	for index := range payloadHash {
		payloadHash[index] = byte(index)
	}
	locator, err := modelcall.EncodeJournalLocator("codex", "log", payloadHash, 2, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(locator) != 94 {
		t.Fatalf("locator length = %d, want 94", len(locator))
	}
	locatorHash := sha256.Sum256(locator)
	if got, want := hex.EncodeToString(locatorHash[:]), "7ab7e9c0fb5915bb8d6ca43c7361adeb3d4cbe64de75c451c6bb60c223c4b952"; got != want {
		t.Fatalf("locator SHA-256 = %q, want %q", got, want)
	}
	got := modelcall.CallID("codex", "conv-1", modelcall.IdentityJournalEvidenceFallback, modelcall.JournalAlias(locator))
	want := "fa6ea03db78abbab9790d22b3cf15ac87ab1ddacfe5d31e49e154aa194cf9c16"
	if got != want {
		t.Fatalf("CallID() = %q, want %q", got, want)
	}
}
