package piext

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSchemaFromStruct(t *testing.T) {
	type input struct {
		Name string `json:"name" jsonschema:"description=Name to greet"`
	}
	schema := SchemaFromStruct(input{})
	var parsed map[string]any
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Fatalf("schema not JSON: %v", err)
	}
	s := string(schema)
	if !strings.Contains(s, `"name"`) {
		t.Fatalf("schema missing name property: %s", s)
	}
	if !strings.Contains(s, `"object"`) {
		t.Fatalf("schema missing object type: %s", s)
	}
}
