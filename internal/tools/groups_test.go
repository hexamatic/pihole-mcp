package tools

import (
	"strings"
	"testing"
)

func TestGroupsList_Normal(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/groups": map[string]any{
			"groups": []any{
				map[string]any{"name": "default", "comment": "", "enabled": true, "id": 0},
				map[string]any{"name": "kids", "comment": "Child devices", "enabled": true, "id": 1},
			},
		},
	}))

	text := callTool(t, groupsListHandler, c, nil)
	if !strings.Contains(text, "2 groups") {
		t.Errorf("expected group count, got: %s", text)
	}
	if !strings.Contains(text, "default") {
		t.Errorf("expected 'default' group, got: %s", text)
	}
	if !strings.Contains(text, "kids") {
		t.Errorf("expected 'kids' group, got: %s", text)
	}
	if !strings.Contains(text, "Child devices") {
		t.Errorf("expected comment, got: %s", text)
	}
}

func TestGroupsList_Empty(t *testing.T) {
	c := newTestClient(t, piholeHandler(map[string]any{
		"/groups": map[string]any{"groups": []any{}},
	}))

	text := callTool(t, groupsListHandler, c, nil)
	if text != "No groups found." {
		t.Errorf("expected empty message, got: %s", text)
	}
}

func TestGroupsAdd_Success(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/groups": map[string]any{"groups": []any{}},
	})
	c := newTestClient(t, rec)

	text := callTool(t, groupsAddHandler, c, map[string]any{"name": "guests"})
	if !strings.Contains(text, "Created") {
		t.Errorf("expected 'Created' message, got: %s", text)
	}
	if !strings.Contains(text, "guests") {
		t.Errorf("expected group name in message, got: %s", text)
	}

	// A group is created by POSTing to the collection with the name in the
	// body. The confirmation text is built from the argument rather than the
	// reply, so it says "Created guests" even if the name never left the
	// process.
	req := rec.Only(t, "POST", "/groups")
	req.AssertRawPath(t, "/groups")
	req.AssertNoQueryString(t)
	// An add sends comment and enabled every time rather than leaving FTL's
	// defaults to decide them. FTL stores an empty comment as NULL, so the
	// empty string costs nothing.
	req.AssertBodyKeys(t, "name", "comment", "enabled")
	req.AssertField(t, "name", "guests")
	req.AssertField(t, "enabled", true)
}

// Mirror of the domains case: the comment and enabled branches of the add
// handler are unreachable in every other test, so a dropped or renamed key is
// invisible.
func TestGroupsAdd_SendsCommentAndExplicitDisable(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/groups": map[string]any{"groups": []any{}},
	})
	c := newTestClient(t, rec)

	callTool(t, groupsAddHandler, c, map[string]any{
		"name": "guests", "comment": "visitor devices", "enabled": false,
	})

	req := rec.Only(t, "POST", "/groups")
	req.AssertBodyKeys(t, "name", "comment", "enabled")
	req.AssertField(t, "name", "guests")
	req.AssertField(t, "comment", "visitor devices")
	req.AssertField(t, "enabled", false)
	req.AssertNoQueryString(t)
}

func TestGroupsUpdate_Success(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/groups/kids": map[string]any{"groups": []any{}},
	})
	c := newTestClient(t, rec)

	text := callTool(t, groupsUpdateHandler, c, map[string]any{"name": "kids", "comment": "updated"})
	if !strings.Contains(text, "Updated") {
		t.Errorf("expected 'Updated' message, got: %s", text)
	}

	// The group being edited is identified only by the path.
	req := rec.Only(t, "PUT", "/groups/kids")
	req.AssertRawPath(t, "/groups/kids")
	req.AssertNoQueryString(t)
	req.AssertField(t, "comment", "updated")
	// An edit that does not rename must not send a name key: FTL would read it
	// as a rename to whatever was sent.
	req.AssertNoField(t, "name")
	// The full key set is deliberately not pinned here. A comment-only update
	// sends no enabled key, which FTL reads as "enable this group", so editing
	// the comment on a disabled group silently re-enables it. Freezing that
	// shape would make the eventual fix look like the regression.
}

// A rename puts the old name in the path and the new one in the body. Getting
// that the wrong way round renames nothing, or renames a group that does not
// exist, and the confirmation text is identical either way because it is built
// from the argument.
func TestGroupsUpdate_RenameKeepsOldNameInPath(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/groups/kids": map[string]any{"groups": []any{}},
	})
	c := newTestClient(t, rec)

	callTool(t, groupsUpdateHandler, c, map[string]any{"name": "kids", "new_name": "teens"})

	req := rec.Only(t, "PUT", "/groups/kids")
	req.AssertRawPath(t, "/groups/kids")
	req.AssertField(t, "name", "teens")
}

// enabled only ever reaches the wire when it is false, so an explicit disable is
// the one group update whose body is complete today. Pinning it guards the value
// going out as a JSON boolean rather than the string "false".
func TestGroupsUpdate_ExplicitDisableSendsEnabledFalse(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/groups/kids": map[string]any{"groups": []any{}},
	})
	c := newTestClient(t, rec)

	callTool(t, groupsUpdateHandler, c, map[string]any{
		"name": "kids", "comment": "paused", "enabled": false,
	})

	req := rec.Only(t, "PUT", "/groups/kids")
	req.AssertBodyKeys(t, "comment", "enabled")
	req.AssertField(t, "enabled", false)
	req.AssertField(t, "comment", "paused")
}

func TestGroupsDelete_Success(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"/groups/kids": map[string]any{},
	})
	c := newTestClient(t, rec)

	text := callTool(t, groupsDeleteHandler, c, map[string]any{"name": "kids"})
	if !strings.Contains(text, "Deleted") {
		t.Errorf("expected 'Deleted' message, got: %s", text)
	}

	// Deleting a group detaches every client and domain assigned to it, so the
	// method and the name in the path are the whole request.
	req := rec.Only(t, "DELETE", "/groups/kids")
	req.AssertRawPath(t, "/groups/kids")
	req.AssertNoQueryString(t)
	req.AssertNoBody(t)
}

func TestGroupsBatchDelete_Success(t *testing.T) {
	const items = `[{"item":"kids"}]`

	rec := piholeHandler(map[string]any{
		"/groups:batchDelete": map[string]any{},
	})
	c := newTestClient(t, rec)

	text := callTool(t, groupsBatchDeleteHandler, c, map[string]any{"items": items})
	if !strings.Contains(text, "Batch delete completed") {
		t.Errorf("expected 'Batch delete completed' message, got: %s", text)
	}

	// The items string is handed to the client as pre-encoded JSON. If rawJSON
	// stopped implementing json.Marshaler the array would go out as a quoted
	// string, which FTL rejects, and the reply text would not change.
	req := rec.Only(t, "POST", "/groups:batchDelete")
	req.AssertRawPath(t, "/groups:batchDelete")
	req.AssertNoQueryString(t)
	req.AssertRawBody(t, items)
}

// ---------------------------------------------------------------------------
// Silent wrong writes
// ---------------------------------------------------------------------------

// Same full-replacement rule as domains and lists, verified against FTL v6.7:
// a comment-only PUT of a disabled group read back enabled, and an
// enabled-only PUT read back with a null comment.
func TestGroupsUpdate_CommentOnlyKeepsTheGroupDisabled(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"GET /groups/kids": map[string]any{"groups": []any{
			map[string]any{"name": "kids", "comment": "school hours", "enabled": false},
		}},
		"PUT /groups/kids": map[string]any{"groups": []any{}},
	})
	c := newTestClient(t, rec)

	callTool(t, groupsUpdateHandler, c, map[string]any{"name": "kids", "comment": "edited"})

	req := rec.Only(t, "PUT", "/groups/kids")
	req.AssertField(t, "comment", "edited")
	req.AssertField(t, "enabled", false)
}

func TestGroupsUpdate_EnabledOnlyKeepsTheComment(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"GET /groups/kids": map[string]any{"groups": []any{
			map[string]any{"name": "kids", "comment": "school hours", "enabled": true},
		}},
		"PUT /groups/kids": map[string]any{"groups": []any{}},
	})
	c := newTestClient(t, rec)

	callTool(t, groupsUpdateHandler, c, map[string]any{"name": "kids", "enabled": false})

	req := rec.Only(t, "PUT", "/groups/kids")
	req.AssertField(t, "comment", "school hours")
	req.AssertField(t, "enabled", false)
}

// A rename still carries both fields, and the new name is the only extra key.
func TestGroupsUpdate_RenameCarriesTheOtherFields(t *testing.T) {
	rec := piholeHandler(map[string]any{
		"GET /groups/kids": map[string]any{"groups": []any{
			map[string]any{"name": "kids", "comment": "school hours", "enabled": false},
		}},
		"PUT /groups/kids": map[string]any{"groups": []any{}},
	})
	c := newTestClient(t, rec)

	callTool(t, groupsUpdateHandler, c, map[string]any{"name": "kids", "new_name": "children"})

	req := rec.Only(t, "PUT", "/groups/kids")
	req.AssertBodyKeys(t, "name", "comment", "enabled")
	req.AssertField(t, "name", "children")
	req.AssertField(t, "comment", "school hours")
	req.AssertField(t, "enabled", false)
}

// A group name is free-form text, so it holds whatever the user typed. A '#'
// ends the path at the fragment and the request reaches a different group, or
// none, while the reply still says "Updated".
func TestGroupsUpdate_EscapesReservedCharactersInTheName(t *testing.T) {
	const name = "Kids #2"
	rec := piholeHandler(map[string]any{
		"/groups/" + name: map[string]any{"groups": []any{}},
	})
	c := newTestClient(t, rec)

	callTool(t, groupsUpdateHandler, c, map[string]any{"name": name, "comment": "edited"})

	rec.Only(t, "PUT", "/groups/"+name).AssertRawPath(t, "/groups/Kids%20%232")
}

func TestGroupsDelete_EscapesReservedCharactersInTheName(t *testing.T) {
	const name = "Kids #2"
	rec := piholeHandler(map[string]any{"/groups/" + name: map[string]any{}})
	c := newTestClient(t, rec)

	callTool(t, groupsDeleteHandler, c, map[string]any{"name": name})

	rec.Only(t, "DELETE", "/groups/"+name).AssertRawPath(t, "/groups/Kids%20%232")
}

func TestGroupsAdd_AlwaysSendsCommentAndEnabled(t *testing.T) {
	rec := piholeHandler(map[string]any{"POST /groups": map[string]any{"groups": []any{}}})
	c := newTestClient(t, rec)

	callTool(t, groupsAddHandler, c, map[string]any{"name": "kids"})

	req := rec.Only(t, "POST", "/groups")
	req.AssertBodyKeys(t, "name", "comment", "enabled")
	req.AssertField(t, "enabled", true)
}

func TestGroupsUpdate_ReadFailureLeavesTheGroupAlone(t *testing.T) {
	rec := piholeHandler(map[string]any{"PUT /groups/kids": map[string]any{"groups": []any{}}})
	c := newTestClient(t, rec)

	callToolExpectError(t, groupsUpdateHandler, c, map[string]any{"name": "kids", "comment": "edited"})
	rec.AssertNone(t, "PUT", "/groups/kids")
}

// The lookup filter is the same free-form name as the write path, so an
// unescaped '#' truncates it and the tool reports on a different group, or on
// none, as though that were the answer.
func TestGroupsList_EscapesTheNameFilter(t *testing.T) {
	const name = "Kids #2"
	rec := piholeHandler(map[string]any{
		"/groups/" + name: map[string]any{"groups": []any{
			map[string]any{"name": name, "comment": "after school", "enabled": true},
		}},
	})
	c := newTestClient(t, rec)

	callTool(t, groupsListHandler, c, map[string]any{"name": name})

	rec.Only(t, "GET", "/groups/"+name).AssertRawPath(t, "/groups/Kids%20%232")
}
