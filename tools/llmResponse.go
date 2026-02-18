package tools

import (
	"encoding/json"
)

type ResponseType string

const (
	ResponseTypeText          ResponseType = "text"
	ResponseTypeToolCallStart ResponseType = "toolcall_start"
	ResponseTypeToolCall      ResponseType = "toolcall"
)

type TextResponse struct {
	Type ResponseType `json:"type"`
	Data string       `json:"data"`
}

type ToolCallStartResponse struct {
	Type     ResponseType `json:"type"`
	Name     string       `json:"name"`
	IsServer bool         `json:"is_server,omitempty"`
	Message  string       `json:"message,omitempty"`
}

type ToolCallResponse struct {
	Type     ResponseType `json:"type"`
	Name     string       `json:"name"`
	Args     string       `json:"args,omitempty"`
	IsServer bool         `json:"is_server,omitempty"`
	Message  string       `json:"message,omitempty"`
	Success  bool         `json:"success,omitempty"`
	Result   string       `json:"result,omitempty"` // server-side tool execution result for client history
}

func NewTextResponse(text string) string {
	resp := TextResponse{
		Type: ResponseTypeText,
		Data: text,
	}
	b, _ := json.Marshal(resp)
	return string(b)
}

func StartToolCallResponse(name string) string {
	resp := ToolCallStartResponse{
		Type: ResponseTypeToolCallStart,
		Name: name,
	}
	b, _ := json.Marshal(resp)
	return string(b)
}

func NewToolCallResponse(name, args string) string {
	resp := ToolCallResponse{
		Type: ResponseTypeToolCall,
		Name: name,
		Args: args,
	}
	b, _ := json.Marshal(resp)
	return string(b)
}

func ServerToolStartResponse(name, message string) string {
	resp := ToolCallStartResponse{
		Type:     ResponseTypeToolCallStart,
		Name:     name,
		IsServer: true,
		Message:  message,
	}
	b, _ := json.Marshal(resp)
	return string(b)
}

func ServerToolEndResponse(name, args, result, message string, success bool) string {
	resp := ToolCallResponse{
		Type:     ResponseTypeToolCall,
		Name:     name,
		Args:     args,
		IsServer: true,
		Message:  message,
		Success:  success,
		Result:   result,
	}
	b, _ := json.Marshal(resp)
	return string(b)
}
