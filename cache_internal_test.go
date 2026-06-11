package thing

import "testing"

func TestCacheCandidateColumnsUsesChangedColumnsForUpdates(t *testing.T) {
	fieldValues := map[string]interface{}{
		"name":  "Alice",
		"email": "alice@example.com",
	}
	changedColumns := map[string]bool{"email": true}

	got := cacheCandidateColumns(fieldValues, changedColumns, false, false, false)

	if len(got) != 1 || !got["email"] {
		t.Fatalf("expected only changed columns for update, got %#v", got)
	}
}

func TestCacheCandidateColumnsUsesAllModelColumnsForCreateAndDelete(t *testing.T) {
	fieldValues := map[string]interface{}{
		"name":  "Alice",
		"email": "alice@example.com",
	}
	changedColumns := map[string]bool{"email": true}

	createColumns := cacheCandidateColumns(fieldValues, changedColumns, true, false, false)
	deleteColumns := cacheCandidateColumns(fieldValues, changedColumns, false, true, false)

	for name, got := range map[string]map[string]bool{
		"create": createColumns,
		"delete": deleteColumns,
	} {
		if len(got) != len(fieldValues) || !got["name"] || !got["email"] {
			t.Fatalf("expected all model columns for %s, got %#v", name, got)
		}
	}
}

func TestCacheCandidateColumnsUsesAllModelColumnsWhenForced(t *testing.T) {
	fieldValues := map[string]interface{}{
		"name":  "Alice",
		"email": "alice@example.com",
	}
	changedColumns := map[string]bool{"email": true}

	got := cacheCandidateColumns(fieldValues, changedColumns, false, false, true)

	if len(got) != len(fieldValues) || !got["name"] || !got["email"] {
		t.Fatalf("expected all model columns when forced, got %#v", got)
	}
}
