package pihole

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// HistoryClients maps a client address to its metadata in a client history.
//
// FTL v6.7 answers with an empty array rather than an object when the privacy
// level hides clients (src/api/history.c), which a map cannot decode, so the
// tools failed with a JSON error instead of saying the history was hidden.
type HistoryClients map[string]ClientHistoryInfo

// UnmarshalJSON accepts an object, or the empty array FTL v6.7 sends.
func (h *HistoryClients) UnmarshalJSON(data []byte) error {
	if trimmed := bytes.TrimSpace(data); len(trimmed) > 0 && trimmed[0] == '[' {
		var list []json.RawMessage
		if err := json.Unmarshal(trimmed, &list); err != nil {
			return err
		}
		if len(list) > 0 {
			return fmt.Errorf("history clients: expected an object, got an array of %d", len(list))
		}
		*h = nil
		return nil
	}
	var m map[string]ClientHistoryInfo
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	*h = m
	return nil
}
