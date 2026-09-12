package mcp

import (
	"encoding/json"
	"testing"
)

func argsFrom(t *testing.T, raw string) Args {
	t.Helper()
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatal(err)
	}
	return Args(decoded)
}

func TestArgsCoerceModelSlips(t *testing.T) {
	// Models send a quoted number for an integer field and a bare string
	// for a list often enough that failing the call over it helps nobody.
	args := argsFrom(t, `{"page":"3","wait":7.0,"confirm":"true","files":"latest.log"}`)

	if got := args.Int("page", 0); got != 3 {
		t.Errorf("quoted integer: got %d", got)
	}
	if got := args.Int("wait", 0); got != 7 {
		t.Errorf("float integer: got %d", got)
	}
	if !args.Bool("confirm", false) {
		t.Error("quoted boolean was not read")
	}
	if list := args.StringList("files"); len(list) != 1 || list[0] != "latest.log" {
		t.Errorf("bare string list: got %v", list)
	}
}

func TestArgsFallBackWhenAbsentOrNull(t *testing.T) {
	args := argsFrom(t, `{"explicit":"","nulled":null}`)

	if got := args.String("missing", "fallback"); got != "fallback" {
		t.Errorf("absent string: got %q", got)
	}
	if got := args.String("nulled", "fallback"); got != "fallback" {
		t.Errorf("null string: got %q", got)
	}
	// An empty string that was actually sent is a value, not an absence:
	// clearing a description is a real request.
	if got := args.String("explicit", "fallback"); got != "" {
		t.Errorf("explicit empty string: got %q", got)
	}
	if !args.Has("explicit") {
		t.Error("an explicitly empty string should count as supplied")
	}
	if args.Has("nulled") {
		t.Error("null should not count as supplied")
	}
}

func TestSchemaJSONOmitsEmptyRequired(t *testing.T) {
	schema := In(Str("optional", "nothing required here"))
	rendered := schema.JSON()
	if _, present := rendered["required"]; present {
		t.Error("an empty required list should be left out entirely")
	}

	properties := rendered["properties"].(map[string]interface{})
	prop := properties["optional"].(map[string]interface{})
	if prop["type"] != "string" || prop["description"] == "" {
		t.Errorf("property was rendered as %v", prop)
	}
}

func TestSchemaJSONCarriesEnumsItemsAndDefaults(t *testing.T) {
	schema := In(
		Str("signal", "power signal").Of("start", "stop").Req(),
		StrList("files", "names"),
		Int("max_lines", "cap").Def(200),
	)
	rendered := schema.JSON()
	properties := rendered["properties"].(map[string]interface{})

	signal := properties["signal"].(map[string]interface{})
	if len(signal["enum"].([]string)) != 2 {
		t.Errorf("enum was %v", signal["enum"])
	}

	files := properties["files"].(map[string]interface{})
	if files["type"] != "array" {
		t.Errorf("list type was %v", files["type"])
	}
	if files["items"].(map[string]interface{})["type"] != "string" {
		t.Errorf("list items were %v", files["items"])
	}

	if properties["max_lines"].(map[string]interface{})["default"] != 200 {
		t.Error("default was dropped")
	}

	required := rendered["required"].([]string)
	if len(required) != 1 || required[0] != "signal" {
		t.Errorf("required was %v", required)
	}
}

func TestMissingListsOnlyAbsentRequiredFields(t *testing.T) {
	schema := In(
		Str("server", "id").Req(),
		Bool("confirm", "yes").Req(),
		Str("note", "optional"),
	)

	missing := schema.Missing(argsFrom(t, `{"server":"abc","confirm":null}`))
	if len(missing) != 1 || missing[0] != "confirm" {
		t.Errorf("got %v", missing)
	}

	if left := schema.Missing(argsFrom(t, `{"server":"abc","confirm":true}`)); len(left) != 0 {
		t.Errorf("nothing should be missing, got %v", left)
	}
}

func TestObjectArgumentRejectsNonObjects(t *testing.T) {
	args := argsFrom(t, `{"renames":{"a":"b"},"broken":"not an object"}`)

	renames, err := args.Object("renames")
	if err != nil || renames["a"] != "b" {
		t.Fatalf("object arg: %v, %v", renames, err)
	}
	if _, err := args.Object("broken"); err == nil {
		t.Error("a string where an object was declared should be refused")
	}
	if value, err := args.Object("absent"); err != nil || value != nil {
		t.Errorf("an absent object should be nil with no error, got %v, %v", value, err)
	}
}
