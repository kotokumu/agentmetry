package query

import (
	"strings"
	"time"
)

// SessionName describes an observed label separately from conversation identity.
type SessionName struct {
	Text       string
	Origin     string
	ObservedAt time.Time
}

// SelectSessionName selects unambiguous latest evidence without mutating input.
func SelectSessionName(names []SessionName) *SessionName {
	var latest time.Time
	for _, name := range names {
		if name.valid() && name.ObservedAt.After(latest) {
			latest = name.ObservedAt
		}
	}
	var selected *SessionName
	for _, name := range names {
		if !name.valid() || (!name.ObservedAt.IsZero() && !name.ObservedAt.Equal(latest)) {
			continue
		}
		if selected == nil {
			copy := name
			selected = &copy
			continue
		}
		if name.Text != selected.Text || name.Origin != selected.Origin {
			return nil
		}
		if name.ObservedAt.IsZero() {
			selected.ObservedAt = time.Time{}
		}
	}
	return selected
}

func (name SessionName) valid() bool {
	return strings.TrimSpace(name.Text) != "" && strings.TrimSpace(name.Origin) != ""
}
