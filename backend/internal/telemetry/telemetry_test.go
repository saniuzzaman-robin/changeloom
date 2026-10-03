package telemetry

import "testing"

func TestQueryName(t *testing.T) {
	for stmt, want := range map[string]string{
		"-- name: ListTimeline :many\nWITH RECURSIVE ...": "ListTimeline",
		"  -- name: DeleteUser :exec\nDELETE FROM users":  "DeleteUser",
		"select 1":            "SELECT",
		"-- name: \nSELECT 1": "--",
		"":                    "UNKNOWN",
	} {
		if got := QueryName(stmt); got != want {
			t.Errorf("QueryName(%q) = %q, want %q", stmt, got, want)
		}
	}
}
