package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/dimetron/pi-go/internal/extension"
	"github.com/dimetron/pi-go/internal/extension/hostproto"
)

func main() {
	decoder := json.NewDecoder(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)

	for {
		var req hostproto.RPCRequest
		if err := decoder.Decode(&req); err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			_ = sendError(encoder, req.ID, -32700, fmt.Sprintf("decode error: %v", err))
			return
		}

		switch req.Method {
		case hostproto.MethodHandshake:
			if err := handleHandshake(encoder, req); err != nil {
				_ = sendError(encoder, req.ID, -32000, err.Error())
				return
			}
		case hostproto.MethodShutdown:
			_ = sendResult(encoder, req.ID, hostproto.ShutdownControl{Reason: "bye"})
			return
		default:
			_ = sendError(encoder, req.ID, -32601, "method not found")
		}
	}
}

func handleHandshake(encoder *json.Encoder, req hostproto.RPCRequest) error {
	var handshake hostproto.HandshakeRequest
	if err := json.Unmarshal(req.Params, &handshake); err != nil {
		return fmt.Errorf("invalid handshake payload: %w", err)
	}
	if err := hostproto.ValidateProtocolCompatibility(handshake.ProtocolVersion); err != nil {
		_ = sendResult(encoder, req.ID, hostproto.HandshakeResponse{
			ProtocolVersion: hostproto.ProtocolVersion,
			Accepted:        false,
			Message:         err.Error(),
		})
		return err
	}

	if err := sendResult(encoder, req.ID, hostproto.HandshakeResponse{
		ProtocolVersion: hostproto.ProtocolVersion,
		Accepted:        true,
		Message:         "hosted-hello ready",
	}); err != nil {
		return err
	}

	// Optional best-effort command registration request.
	_ = sendRequest(encoder, hostproto.MethodRegisterCommand, hostproto.CommandRegistration{
		Name:        "hello",
		Description: "Say hello from hosted extension",
		Prompt:      "Say hello from hosted extension. Extra: {{args}}",
	})

	// Optional best-effort UI status intent.
	statusIntent := extension.UIIntent{
		Type: extension.UIIntentStatus,
		Status: &extension.StatusIntent{
			Text: "hosted-hello connected",
		},
	}
	intentPayload, err := json.Marshal(statusIntent)
	if err != nil {
		return err
	}
	_ = sendRequest(encoder, hostproto.MethodIntent, hostproto.IntentEnvelope{
		Type:    string(extension.IntentUI),
		Payload: intentPayload,
	})

	return nil
}

func sendRequest(encoder *json.Encoder, method string, payload any) error {
	params, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return encoder.Encode(hostproto.RPCRequest{
		JSONRPC: hostproto.JSONRPCVersion,
		Method:  method,
		Params:  params,
	})
}

func sendResult(encoder *json.Encoder, id int64, payload any) error {
	result, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return encoder.Encode(hostproto.RPCResponse{
		JSONRPC: hostproto.JSONRPCVersion,
		ID:      id,
		Result:  result,
	})
}

func sendError(encoder *json.Encoder, id int64, code int, message string) error {
	return encoder.Encode(hostproto.RPCResponse{
		JSONRPC: hostproto.JSONRPCVersion,
		ID:      id,
		Error: &hostproto.RPCError{
			Code:    code,
			Message: message,
		},
	})
}
