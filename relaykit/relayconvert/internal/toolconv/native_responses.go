package toolconv

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// PrepareNativeResponsesTools adapts client tool shapes without round-tripping
// hosted tools or their history through another protocol. Raw search settings,
// citations and provider-specific extensions therefore remain intact.
func PrepareNativeResponsesTools(request *dto.OpenAIResponsesRequest) (Set, error) {
	set := Set{Source: types.RelayFormatOpenAIResponses}
	var rawTools []json.RawMessage
	if len(request.Tools) > 0 {
		if err := kitutil.Unmarshal(request.Tools, &rawTools); err != nil {
			return set, fmt.Errorf("invalid Responses tools: %w", err)
		}
	}
	usedNames, historyRefs, err := nativeResponsesHistoryToolNames(request.Input)
	if err != nil {
		return set, err
	}
	definitions, refs, err := flattenResponsesNamespaceTools(rawTools, true, usedNames)
	if err != nil {
		return set, err
	}
	if refs == nil {
		for _, raw := range rawTools {
			definition, err := decodeOpenAIResponsesDefinition(raw)
			if err != nil {
				return set, err
			}
			definitions = append(definitions, definition)
		}
	}
	// A client can remove tools between turns while retaining their calls in
	// history. Keep those identities distinct without re-enabling any tool.
	knownRefs := make(map[NamespaceRef]bool, len(refs))
	for name, ref := range refs {
		usedNames[name] = struct{}{}
		knownRefs[ref] = true
	}
	for _, definition := range definitions {
		var tool map[string]json.RawMessage
		if kitutil.Unmarshal(definition.Raw, &tool) == nil {
			if name := rawToolName(tool); name != "" {
				usedNames[name] = struct{}{}
			}
		}
	}
	for _, ref := range historyRefs {
		if knownRefs[ref] {
			continue
		}
		if refs == nil {
			refs = make(map[string]NamespaceRef)
		}
		name := responsesFlattenedToolName(ref.Namespace, ref.Name, usedNames)
		refs[name] = ref
		knownRefs[ref] = true
	}
	set.Definitions, set.NamespaceRefs = definitions, refs
	customTools, _ := openAIChatCustomTools(set)
	if refs != nil || len(customTools) > 0 {
		rawTools = make([]json.RawMessage, 0, len(definitions))
		for index, definition := range definitions {
			raw := definition.Raw
			if custom, ok := customTools[index]; ok {
				if custom == nil {
					return set, fmt.Errorf("tools[%d]: custom tool name conflicts with another tool", index)
				}
				function := custom.chatTool().Function
				raw, err = kitutil.Marshal(map[string]any{
					"type": "function", "name": function.Name,
					"description": function.Description, "parameters": function.Parameters,
				})
				if err != nil {
					return set, err
				}
			}
			rawTools = append(rawTools, raw)
		}
		request.Tools, err = kitutil.Marshal(rawTools)
		if err != nil {
			return set, err
		}
	}
	request.Input, request.ToolChoice = rewriteResponsesNamespacedNames(request.Input, request.ToolChoice, refs)
	request.Input, err = rewriteNativeCustomHistory(request.Input)
	if err != nil {
		return set, err
	}
	request.ToolChoice = rewriteNativeCustomChoice(request.ToolChoice, OpenAIChatCustomToolNames(set))
	return set, nil
}

func nativeResponsesHistoryToolNames(input json.RawMessage) (map[string]struct{}, []NamespaceRef, error) {
	plainNames := make(map[string]struct{})
	if kitutil.GetJsonType(input) != "array" {
		return plainNames, nil, nil
	}
	var items []map[string]json.RawMessage
	if err := kitutil.Unmarshal(input, &items); err != nil {
		return nil, nil, err
	}
	var refs []NamespaceRef
	for _, item := range items {
		switch rawToolType(item) {
		case "function_call", "custom_tool_call", "function_call_output", "custom_tool_call_output":
		default:
			continue
		}
		name := rawToolName(item)
		if name == "" {
			continue
		}
		namespace := strings.TrimSpace(kitutil.JsonRawMessageToString(item["namespace"]))
		if namespace == "" {
			plainNames[name] = struct{}{}
		} else {
			refs = append(refs, NamespaceRef{Namespace: namespace, Name: name})
		}
	}
	return plainNames, refs, nil
}

func rewriteNativeCustomHistory(raw json.RawMessage) (json.RawMessage, error) {
	if kitutil.GetJsonType(raw) != "array" {
		return raw, nil
	}
	var items []json.RawMessage
	if err := kitutil.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	changed := false
	for index, itemRaw := range items {
		var item map[string]json.RawMessage
		if err := kitutil.Unmarshal(itemRaw, &item); err != nil {
			return nil, err
		}
		switch rawToolType(item) {
		case "custom_tool_call":
			input := item["input"]
			if len(input) == 0 {
				input = json.RawMessage(`""`)
			}
			arguments, err := kitutil.Marshal(map[string]json.RawMessage{convmeta.CustomToolInputArgument: input})
			if err != nil {
				return nil, err
			}
			item["type"] = json.RawMessage(`"function_call"`)
			item["arguments"], _ = kitutil.Marshal(string(arguments))
			delete(item, "input")
		case "custom_tool_call_output":
			item["type"] = json.RawMessage(`"function_call_output"`)
		default:
			continue
		}
		var err error
		items[index], err = kitutil.Marshal(item)
		if err != nil {
			return nil, err
		}
		changed = true
	}
	if !changed {
		return raw, nil
	}
	return kitutil.Marshal(items)
}

func rewriteNativeCustomChoice(raw json.RawMessage, names map[string]struct{}) json.RawMessage {
	if len(names) == 0 || kitutil.GetJsonType(raw) != "object" {
		return raw
	}
	var choice map[string]json.RawMessage
	if kitutil.Unmarshal(raw, &choice) != nil {
		return raw
	}
	switch rawToolType(choice) {
	case "custom":
		if _, ok := names[rawToolName(choice)]; !ok {
			return raw
		}
		choice["type"] = json.RawMessage(`"function"`)
	case "allowed_tools":
		var tools []json.RawMessage
		if kitutil.Unmarshal(choice["tools"], &tools) != nil {
			return raw
		}
		changed := false
		for index, tool := range tools {
			rewritten := rewriteNativeCustomChoice(tool, names)
			changed = changed || !bytes.Equal(tool, rewritten)
			tools[index] = rewritten
		}
		if !changed {
			return raw
		}
		choice["tools"], _ = kitutil.Marshal(tools)
	default:
		return raw
	}
	rewritten, err := kitutil.Marshal(choice)
	if err != nil {
		return raw
	}
	return rewritten
}
