package harness

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

const fingerprintDomain = "agentmetry-harness-v1"

type Identity struct {
	Scope       string `json:"scope"`
	Fingerprint string `json:"fingerprint"`
	Label       string `json:"label,omitempty"`
}

func GenerateFingerprint(root, scope, label string, files []string) (Identity, error) {
	if !ValidScope(scope) {
		return Identity{}, fmt.Errorf("invalid harness scope")
	}
	label = strings.TrimSpace(label)
	if !ValidLabel(label) {
		return Identity{}, fmt.Errorf("invalid harness label")
	}
	if len(files) == 0 {
		return Identity{}, fmt.Errorf("at least one harness file is required")
	}
	physicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return Identity{}, fmt.Errorf("resolve harness root: %w", err)
	}
	physicalRoot, err = filepath.Abs(physicalRoot)
	if err != nil {
		return Identity{}, fmt.Errorf("make harness root absolute: %w", err)
	}
	type entry struct {
		path    string
		content []byte
	}
	entries := make([]entry, 0, len(files))
	seen := make(map[string]struct{}, len(files))
	for _, file := range files {
		if !utf8.ValidString(file) {
			return Identity{}, fmt.Errorf("harness path is not valid UTF-8")
		}
		candidate := file
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(physicalRoot, candidate)
		}
		candidate = filepath.Clean(candidate)
		relative, err := filepath.Rel(physicalRoot, candidate)
		if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return Identity{}, fmt.Errorf("harness file must be inside root")
		}
		normalized := filepath.ToSlash(filepath.Clean(relative))
		if !utf8.ValidString(normalized) {
			return Identity{}, fmt.Errorf("normalized harness path is not valid UTF-8")
		}
		if _, duplicate := seen[normalized]; duplicate {
			return Identity{}, fmt.Errorf("duplicate harness path %q", normalized)
		}
		seen[normalized] = struct{}{}
		if err := rejectSymlinkComponents(physicalRoot, relative); err != nil {
			return Identity{}, err
		}
		info, err := os.Stat(candidate)
		if err != nil {
			return Identity{}, fmt.Errorf("stat harness file %q: %w", normalized, err)
		}
		if !info.Mode().IsRegular() {
			return Identity{}, fmt.Errorf("harness path %q is not a regular file", normalized)
		}
		content, err := os.ReadFile(candidate)
		if err != nil {
			return Identity{}, fmt.Errorf("read harness file %q: %w", normalized, err)
		}
		entries = append(entries, entry{path: normalized, content: content})
	}
	sort.Slice(entries, func(left, right int) bool {
		return bytes.Compare([]byte(entries[left].path), []byte(entries[right].path)) < 0
	})
	hash := sha256.New()
	writeU32 := func(value uint32) { _ = binary.Write(hash, binary.BigEndian, value) }
	writeU64 := func(value uint64) { _ = binary.Write(hash, binary.BigEndian, value) }
	writeU32(uint32(len(fingerprintDomain)))
	_, _ = hash.Write([]byte(fingerprintDomain))
	writeU32(uint32(len(entries)))
	for _, item := range entries {
		pathBytes := []byte(item.path)
		writeU32(uint32(len(pathBytes)))
		_, _ = hash.Write(pathBytes)
		writeU64(uint64(len(item.content)))
		_, _ = hash.Write(item.content)
	}
	return Identity{Scope: scope, Fingerprint: "sha256:" + hex.EncodeToString(hash.Sum(nil)), Label: label}, nil
}

func rejectSymlinkComponents(root, relative string) error {
	current := root
	for _, component := range strings.Split(filepath.Clean(relative), string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("inspect harness path %q: %w", filepath.ToSlash(relative), err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("harness path %q contains a symlink", filepath.ToSlash(relative))
		}
	}
	return nil
}
