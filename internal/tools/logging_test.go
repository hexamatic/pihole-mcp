package tools

import "testing"

func TestRedactCredentials(t *testing.T) {
	in := map[string]any{
		"instance": "primary",
		"password": "hunter2",
		"nested": map[string]any{
			"sid":           "abc123",
			"count":         5,
			"Authorization": "Bearer xyz",
		},
		"list": []any{
			map[string]any{"token": "secret-token", "ok": true},
		},
	}

	got := redactCredentials(in).(map[string]any)

	if got["instance"] != "primary" {
		t.Errorf("non-sensitive key changed: %v", got["instance"])
	}
	if got["password"] != "[redacted]" {
		t.Errorf("password not redacted: %v", got["password"])
	}
	nested := got["nested"].(map[string]any)
	if nested["sid"] != "[redacted]" || nested["Authorization"] != "[redacted]" {
		t.Errorf("nested sensitive keys not redacted: %v", nested)
	}
	if nested["count"] != 5 {
		t.Errorf("nested non-sensitive value changed: %v", nested["count"])
	}
	item := got["list"].([]any)[0].(map[string]any)
	if item["token"] != "[redacted]" || item["ok"] != true {
		t.Errorf("list item redaction wrong: %v", item)
	}

	// Original must be untouched (redaction returns a copy).
	if in["password"] != "hunter2" {
		t.Error("redactCredentials mutated the input")
	}
}

func TestRedactCredentials_PassthroughNonMap(t *testing.T) {
	if got := redactCredentials("plain"); got != "plain" {
		t.Errorf("string passthrough failed: %v", got)
	}
	if got := redactCredentials(42); got != 42 {
		t.Errorf("int passthrough failed: %v", got)
	}
}

func TestSendLog_NoServerIsNoop(t *testing.T) {
	// With no MCP server in context, sendLog must not panic.
	sendLog(t.Context(), "info", "test", map[string]any{"k": "v"})
}
