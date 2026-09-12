package mcp

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Field is one property of a tool's input schema.
//
// The builders below exist because this server declares around sixty tools:
// written out as nested maps, the declarations would be longer than the
// handlers they describe and a misplaced brace would be invisible.
type Field struct {
	Name     string
	Type     string
	Desc     string
	Enum     []string
	Items    string
	Required bool
	Default  interface{}
}

// Str declares an optional string property.
func Str(name, desc string) Field { return Field{Name: name, Type: "string", Desc: desc} }

// Int declares an optional integer property.
func Int(name, desc string) Field { return Field{Name: name, Type: "integer", Desc: desc} }

// Bool declares an optional boolean property.
func Bool(name, desc string) Field { return Field{Name: name, Type: "boolean", Desc: desc} }

// StrList declares an optional array-of-strings property.
func StrList(name, desc string) Field {
	return Field{Name: name, Type: "array", Items: "string", Desc: desc}
}

// Obj declares an optional free-form object property, for the places where
// the panel takes a payload this server has no business constraining.
func Obj(name, desc string) Field { return Field{Name: name, Type: "object", Desc: desc} }

// Req marks the field as required.
func (f Field) Req() Field { f.Required = true; return f }

// Of restricts the field to a fixed set of values.
func (f Field) Of(values ...string) Field { f.Enum = values; return f }

// Def records a default. It is advisory to the client — the handler still has
// to supply the same fallback, since a client may omit the field regardless.
func (f Field) Def(value interface{}) Field { f.Default = value; return f }

// Schema is a tool's input schema: an ordered list of properties.
type Schema struct {
	Fields []Field
}

// In builds a schema from fields.
func In(fields ...Field) Schema { return Schema{Fields: fields} }

// JSON renders the schema as a JSON Schema object, which is the shape
// tools/list has to return.
func (s Schema) JSON() map[string]interface{} {
	props := make(map[string]interface{}, len(s.Fields))
	required := make([]string, 0, len(s.Fields))

	for _, f := range s.Fields {
		prop := map[string]interface{}{"type": f.Type}
		if f.Desc != "" {
			prop["description"] = f.Desc
		}
		if len(f.Enum) > 0 {
			prop["enum"] = f.Enum
		}
		if f.Items != "" {
			prop["items"] = map[string]interface{}{"type": f.Items}
		}
		if f.Default != nil {
			prop["default"] = f.Default
		}
		props[f.Name] = prop
		if f.Required {
			required = append(required, f.Name)
		}
	}

	out := map[string]interface{}{
		"type":       "object",
		"properties": props,
	}
	// An empty "required" is legal but reads as a constraint that is there
	// and empty rather than absent, and some clients render it that way.
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

// Missing lists the required fields the caller left out or sent as null.
func (s Schema) Missing(args Args) []string {
	var missing []string
	for _, f := range s.Fields {
		if !f.Required {
			continue
		}
		raw, present := args[f.Name]
		if !present || string(raw) == "null" || len(raw) == 0 {
			missing = append(missing, f.Name)
		}
	}
	return missing
}

// Args is one call's arguments, still encoded.
//
// Kept raw so a handler can distinguish an argument that was not sent from
// one sent as null or as an empty string. For a panel that treats an empty
// description as "clear the description", those are three different requests.
type Args map[string]json.RawMessage

// Has reports whether the caller supplied the argument at all.
func (a Args) Has(name string) bool {
	raw, present := a[name]
	return present && len(raw) > 0 && string(raw) != "null"
}

// String returns a string argument, or fallback when it is absent.
//
// A number or boolean sent where a string was asked for is converted rather
// than rejected: models do send 25 for a page number declared as a string,
// and failing the call over it helps nobody.
func (a Args) String(name, fallback string) string {
	raw, present := a[name]
	if !present || len(raw) == 0 || string(raw) == "null" {
		return fallback
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return strings.Trim(string(raw), "\"")
}

// Int returns an integer argument, or fallback when it is absent or unusable.
func (a Args) Int(name string, fallback int) int {
	raw, present := a[name]
	if !present || len(raw) == 0 || string(raw) == "null" {
		return fallback
	}
	var n float64
	if err := json.Unmarshal(raw, &n); err == nil {
		return int(n)
	}
	// A model may quote a number. Accepting that costs one parse.
	if n, err := strconv.Atoi(strings.Trim(string(raw), "\"")); err == nil {
		return n
	}
	return fallback
}

// Bool returns a boolean argument, or fallback when it is absent or unusable.
func (a Args) Bool(name string, fallback bool) bool {
	raw, present := a[name]
	if !present || len(raw) == 0 || string(raw) == "null" {
		return fallback
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		return b
	}
	switch strings.ToLower(strings.Trim(string(raw), "\"")) {
	case "true", "yes", "1":
		return true
	case "false", "no", "0":
		return false
	}
	return fallback
}

// StringList returns an array-of-strings argument.
//
// A bare string is accepted as a one-element list. Deleting one file is the
// common case and models write it as a string about as often as a list.
func (a Args) StringList(name string) []string {
	raw, present := a[name]
	if !present || len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		return list
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil && single != "" {
		return []string{single}
	}
	return nil
}

// Object returns a free-form object argument.
func (a Args) Object(name string) (map[string]interface{}, error) {
	raw, present := a[name]
	if !present || len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("%s must be a JSON object: %w", name, err)
	}
	return out, nil
}

// Raw returns the argument exactly as it arrived.
func (a Args) Raw(name string) json.RawMessage { return a[name] }
