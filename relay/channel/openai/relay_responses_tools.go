package openai

import (
	"strconv"
	"strings"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Patch only tool fields: native search results, usage, citations and unknown
// fields must survive byte-for-byte, including integers larger than float64.
func rewriteResponsesOutputTools(info *relaycommon.RelayInfo, payload []byte, outputPath string) []byte {
	for i := range gjson.GetBytes(payload, outputPath).Array() {
		payload = rewriteResponsesToolItem(info, payload, outputPath+"."+strconv.Itoa(i))
	}
	return payload
}

func rewriteResponsesToolName(info *relaycommon.RelayInfo, payload []byte, prefix, name string) []byte {
	if ref, ok := info.ResponsesNamespaceTools()[name]; ok {
		payload = setResponsesToolField(payload, prefix+"name", ref.Name)
		payload = setResponsesToolField(payload, prefix+"namespace", ref.Namespace)
	}
	return payload
}

func rewriteResponsesToolItem(info *relaycommon.RelayInfo, payload []byte, path string) []byte {
	item := gjson.GetBytes(payload, path)
	kind := item.Get("type").String()
	if kind != "function_call" && kind != "custom_tool_call" {
		return payload
	}
	name := item.Get("name").String()
	payload = rewriteResponsesToolName(info, payload, path+".", name)
	// Test the upstream name, before namespace restoration. A top-level custom
	// tool can have the same leaf name as a namespaced ordinary function.
	if kind == "function_call" && info.ResponsesToolState().IsCustomTool(name) {
		input := relayconvert.CustomToolInputFromArguments(item.Get("arguments").String())
		payload = setResponsesToolField(payload, path+".type", "custom_tool_call")
		payload = setResponsesToolField(payload, path+".input", input)
		payload = deleteResponsesToolField(payload, path+".arguments")
	}
	return payload
}

func setResponsesToolField(payload []byte, path string, value any) []byte {
	if patched, err := sjson.SetBytes(payload, path, value); err == nil {
		return patched
	}
	return payload
}

func deleteResponsesToolField(payload []byte, path string) []byte {
	if patched, err := sjson.DeleteBytes(payload, path); err == nil {
		return patched
	}
	return payload
}

type responsesToolCall struct {
	name      string
	arguments strings.Builder
}

type responsesToolStream struct {
	info         *relaycommon.RelayInfo
	calls        map[string]*responsesToolCall
	lastSequence int64
	sawSequence  bool
}

func newResponsesToolStream(info *relaycommon.RelayInfo) *responsesToolStream {
	return &responsesToolStream{info: info, calls: make(map[string]*responsesToolCall)}
}

func (s *responsesToolStream) call(payload []byte, name string) *responsesToolCall {
	id := gjson.GetBytes(payload, "item_id").String()
	if id == "" {
		id = gjson.GetBytes(payload, "item.id").String()
	}
	index := gjson.GetBytes(payload, "output_index")
	key := "id:" + id
	if id == "" {
		key = "index:" + index.Raw
	}
	call := s.calls[key]
	if call == nil && index.Exists() {
		call = s.calls["index:"+index.Raw]
	}
	if call == nil {
		call = &responsesToolCall{}
	}
	s.calls[key] = call
	if index.Exists() {
		s.calls["index:"+index.Raw] = call
	}
	if name != "" {
		call.name = name
	}
	return call
}

func (s *responsesToolStream) rewrite(payload []byte) [][]byte {
	tools := s.info.ResponsesToolState()
	if len(s.info.ResponsesNamespaceTools()) == 0 && (tools == nil || len(tools.CustomToolNames) == 0) {
		return [][]byte{payload}
	}
	switch kind := gjson.GetBytes(payload, "type").String(); kind {
	case "response.output_item.added", "response.output_item.done":
		item := gjson.GetBytes(payload, "item")
		if item.Get("type").String() == "function_call" {
			call := s.call(payload, item.Get("name").String())
			arguments := item.Get("arguments")
			if kind == "response.output_item.added" && arguments.Exists() {
				call.arguments.Reset()
				call.arguments.WriteString(arguments.String())
			}
			if !arguments.Exists() && tools.IsCustomTool(call.name) {
				payload = setResponsesToolField(payload, "item.arguments", call.arguments.String())
			}
		}
		payload = rewriteResponsesToolItem(s.info, payload, "item")
	case "response.created", "response.in_progress", "response.completed", "response.done", "response.incomplete":
		payload = rewriteResponsesOutputTools(s.info, payload, "response.output")
	case "response.function_call_arguments.delta", "response.function_call_arguments.done":
		call := s.call(payload, gjson.GetBytes(payload, "name").String())
		if kind == "response.function_call_arguments.delta" {
			if tools.IsCustomTool(call.name) {
				// JSON string escapes can cross fragments. Do not send the
				// {"input": ...} transport wrapper as freeform input.
				call.arguments.WriteString(gjson.GetBytes(payload, "delta").String())
				return nil
			}
			break
		}
		payload = rewriteResponsesToolName(s.info, payload, "", call.name)
		if tools.IsCustomTool(call.name) {
			if arguments := gjson.GetBytes(payload, "arguments"); arguments.Exists() {
				call.arguments.Reset()
				call.arguments.WriteString(arguments.String())
			}
			input := relayconvert.CustomToolInputFromArguments(call.arguments.String())
			payload = deleteResponsesToolField(payload, "arguments")
			payload = setResponsesToolField(payload, "type", "response.custom_tool_call_input.done")
			payload = setResponsesToolField(payload, "input", input)
			if input != "" {
				// Derive the extra event from the raw done event, preserving its
				// item_id/call_id/output_index and any upstream extension fields.
				delta := deleteResponsesToolField(payload, "input")
				delta = setResponsesToolField(delta, "type", "response.custom_tool_call_input.delta")
				delta = setResponsesToolField(delta, "delta", input)
				return s.sequence([][]byte{delta, payload})
			}
		}
	}
	return s.sequence([][]byte{payload})
}

func (s *responsesToolStream) sequence(events [][]byte) [][]byte {
	tools := s.info.ResponsesToolState()
	if tools == nil || len(tools.CustomToolNames) == 0 {
		return events
	}
	for i, payload := range events {
		sequence := gjson.GetBytes(payload, "sequence_number")
		if !sequence.Exists() {
			continue
		}
		next := sequence.Int()
		if s.sawSequence && next <= s.lastSequence {
			next = s.lastSequence + 1
			events[i] = setResponsesToolField(payload, "sequence_number", next)
		}
		s.lastSequence, s.sawSequence = next, true
	}
	return events
}
