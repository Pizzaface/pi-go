package piapi

import (
	"context"
	"encoding/json"
	"testing"
)

func TestToolDescriptor_Validate(t *testing.T) {
	valid := ToolDescriptor{
		Name:        "greet",
		Label:       "Greet",
		Description: "Greet someone",
		Parameters:  json.RawMessage(`{"type":"object"}`),
		Execute: func(_ context.Context, _ ToolCall, _ UpdateFunc) (ToolResult, error) {
			return ToolResult{}, nil
		},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid tool failed validation: %v", err)
	}

	bad := valid
	bad.Name = "has spaces"
	if err := bad.Validate(); err == nil {
		t.Fatal("invalid name should fail validation")
	}

	noExec := valid
	noExec.Execute = nil
	if err := noExec.Validate(); err == nil {
		t.Fatal("missing Execute should fail validation")
	}

	noDesc := valid
	noDesc.Description = ""
	if err := noDesc.Validate(); err == nil {
		t.Fatal("missing Description should fail validation")
	}
}
