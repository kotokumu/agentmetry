package sqlite

import "testing"

// TestSQLiteDSN lives in the internal package because sqliteDSN is unexported;
// the other store tests use the external sqlite_test package.
func TestSQLiteDSN(t *testing.T) {
	const writeQuery = "_busy_timeout=5000&_foreign_keys=on&_journal_mode=wal&_synchronous=full"
	const readQuery = "_busy_timeout=5000&_foreign_keys=on&_journal_mode=wal&_query_only=1&_synchronous=full"
	tests := []struct {
		name     string
		path     string
		readOnly bool
		want     string
	}{
		{
			name: "relative",
			path: "data/agentmetry.db",
			want: "file:data/agentmetry.db?" + writeQuery,
		},
		{
			name:     "absolute read-only with reserved characters",
			path:     "/var/lib/agentmetry db?#.db",
			readOnly: true,
			want:     "file:/var/lib/agentmetry%20db%3F%23.db?" + readQuery,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Check the entire DSN against a fixed value to prevent relative paths from becoming URI authorities again.
			if got := sqliteDSN(tt.path, tt.readOnly); got != tt.want {
				t.Fatalf("sqliteDSN(%q, %t) = %q, want %q", tt.path, tt.readOnly, got, tt.want)
			}
		})
	}
}
