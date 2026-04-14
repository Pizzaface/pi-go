package piext

import (
	"encoding/json"

	"github.com/invopop/jsonschema"
)

// SchemaFromStruct generates a JSON Schema (draft-2020-12) for a Go
// struct. Use struct tags to annotate:
//
//	type args struct {
//	    Name string `json:"name" jsonschema:"description=Name to greet"`
//	}
//	pi.RegisterTool(piapi.ToolDescriptor{
//	    Parameters: piext.SchemaFromStruct(args{}),
//	    ...
//	})
//
// Panics if the schema cannot be generated (should be impossible for
// well-formed structs; callers can treat it as programmer error).
func SchemaFromStruct(v any) json.RawMessage {
	r := &jsonschema.Reflector{
		ExpandedStruct: true,
		DoNotReference: true,
	}
	schema := r.Reflect(v)
	b, err := json.Marshal(schema)
	if err != nil {
		panic("piext: SchemaFromStruct: " + err.Error())
	}
	return json.RawMessage(b)
}
