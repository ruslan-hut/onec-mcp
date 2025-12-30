package mcp

import "encoding/json"

const JSONRPCVersion = "2.0"

// Standard JSON-RPC 2.0 error codes
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
	CodeUnauthorized   = -32000
)

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      any             `json:"id,omitempty"`
}

type Response struct {
	JSONRPC string `json:"jsonrpc"`
	Result  any    `json:"result,omitempty"`
	Error   *Error `json:"error,omitempty"`
	ID      any    `json:"id,omitempty"`
}

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func NewResponse(id any, result any) *Response {
	return &Response{
		JSONRPC: JSONRPCVersion,
		Result:  result,
		ID:      id,
	}
}

func NewErrorResponse(id any, code int, message string, data any) *Response {
	return &Response{
		JSONRPC: JSONRPCVersion,
		Error: &Error{
			Code:    code,
			Message: message,
			Data:    data,
		},
		ID: id,
	}
}

func ParseError(id any) *Response {
	return NewErrorResponse(id, CodeParseError, "Parse error", nil)
}

func InvalidRequest(id any) *Response {
	return NewErrorResponse(id, CodeInvalidRequest, "Invalid Request", nil)
}

func MethodNotFound(id any, method string) *Response {
	return NewErrorResponse(id, CodeMethodNotFound, "Method not found", method)
}

func InvalidParams(id any, details string) *Response {
	return NewErrorResponse(id, CodeInvalidParams, "Invalid params", details)
}

func InternalError(id any, details string) *Response {
	return NewErrorResponse(id, CodeInternalError, "Internal error", details)
}

func Unauthorized(id any) *Response {
	return NewErrorResponse(id, CodeUnauthorized, "Unauthorized", nil)
}
