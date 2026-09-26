package toolconv

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

// NamespaceRef is the original (namespace, name) pair a flattened tool name
// stands for. The flattened name is what upstream models see; the ref is what
// the Responses client must get back, because clients such as Codex resolve
// tool calls as ToolName(namespace, name) and miss calls that lack namespace.
type NamespaceRef struct {
	Namespace string
	Name      string
}

const (
	claudeFunctionNameLimit = 128
	geminiFunctionNameLimit = 64
	namespaceHashLength     = 8
)

// flattenResponsesNamespaceTools expands type=namespace groups into one
// function definition per nested function, named "<namespace>__<name>".
// Non-function nested tools are dropped with their group, exactly as before
// namespace groups were understood. Tools without any namespace group return
// a nil map, which keeps their output byte-identical.
func flattenResponsesNamespaceTools(rawTools []json.RawMessage) ([]Definition, map[string]NamespaceRef, error) {
	if len(rawTools) == 0 {
		return nil, nil, nil
	}
	tools := make([]map[string]json.RawMessage, len(rawTools))
	hasNamespace := false
	for index, rawTool := range rawTools {
		if err := kitutil.Unmarshal(rawTool, &tools[index]); err != nil {
			return nil, nil, err
		}
		if rawToolType(tools[index]) == "namespace" {
			hasNamespace = true
		}
	}
	if !hasNamespace {
		return nil, nil, nil
	}

	// Flattened names must not collide with any peer tool name, whatever the
	// peer's type, so register them all before flattening.
	used := map[string]struct{}{}
	for _, tool := range tools {
		if name := rawToolName(tool); name != "" {
			used[name] = struct{}{}
		}
	}

	var definitions []Definition
	refs := map[string]NamespaceRef{}
	for index, tool := range tools {
		if rawToolType(tool) != "namespace" {
			definition, err := decodeOpenAIResponsesDefinition(rawTools[index])
			if err != nil {
				return nil, nil, err
			}
			definitions = append(definitions, definition)
			continue
		}
		namespace := rawToolName(tool)
		var nested []json.RawMessage
		if rawNested := tool["tools"]; len(rawNested) > 0 {
			if err := kitutil.Unmarshal(rawNested, &nested); err != nil {
				return nil, nil, fmt.Errorf("tools[%d].tools must be an array", index)
			}
		}
		for _, nestedRaw := range nested {
			var nestedTool map[string]json.RawMessage
			if err := kitutil.Unmarshal(nestedRaw, &nestedTool); err != nil {
				return nil, nil, fmt.Errorf("tools[%d].tools contains a non-object entry", index)
			}
			if rawToolType(nestedTool) != "function" {
				continue
			}
			original := rawToolName(nestedTool)
			flat := responsesFlattenedToolName(namespace, original, used)
			rewritten, err := patchNamespacedName(nestedTool, flat)
			if err != nil {
				return nil, nil, err
			}
			definition, err := decodeOpenAIResponsesDefinition(rewritten)
			if err != nil {
				return nil, nil, err
			}
			definitions = append(definitions, definition)
			refs[flat] = NamespaceRef{Namespace: namespace, Name: original}
		}
	}
	return definitions, refs, nil
}

func rawToolType(tool map[string]json.RawMessage) string {
	return strings.TrimSpace(kitutil.JsonRawMessageToString(tool["type"]))
}

func rawToolName(tool map[string]json.RawMessage) string {
	return strings.TrimSpace(kitutil.JsonRawMessageToString(tool["name"]))
}

// patchNamespacedName returns item re-encoded with name set to flat and the
// namespace member removed. Every other member keeps its original bytes, so
// numbers and other values survive the rewrite untouched.
func patchNamespacedName(item map[string]json.RawMessage, flat string) (json.RawMessage, error) {
	encoded, err := kitutil.Marshal(flat)
	if err != nil {
		return nil, err
	}
	item["name"] = encoded
	delete(item, "namespace")
	return kitutil.Marshal(item)
}

func responsesFlattenedToolName(namespace string, name string, used map[string]struct{}) string {
	flat := namespace + "__" + name
	if _, taken := used[flat]; !taken && responsesToolNameAccepted(flat) {
		used[flat] = struct{}{}
		return flat
	}
	sum := sha256.Sum256([]byte(flat))
	hash := hex.EncodeToString(sum[:])[:namespaceHashLength]
	base := flat
	for len(base)+1+namespaceHashLength > geminiFunctionNameLimit && len(base) > 0 {
		base = base[:len(base)-1]
	}
	base = strings.TrimRight(base, "_")
	var builder strings.Builder
	for _, r := range base {
		switch {
		case r <= unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-'):
			builder.WriteRune(r)
		default:
			builder.WriteByte('_')
		}
	}
	sanitized := strings.Trim(builder.String(), "_")
	if sanitized == "" || !unicode.IsLetter(rune(sanitized[0])) && sanitized[0] != '_' {
		sanitized = "t_" + sanitized
	}
	shortened := sanitized + "_" + hash
	if len(shortened) > geminiFunctionNameLimit {
		shortened = shortened[:geminiFunctionNameLimit]
	}
	candidate := shortened
	for counter := 1; ; counter++ {
		if _, taken := used[candidate]; !taken {
			used[candidate] = struct{}{}
			return candidate
		}
		// Trim the base so the counter digits always fit inside the limit;
		// truncating the composed candidate instead could fold every retry
		// back onto the same taken name and spin forever.
		suffix := hash + strconv.FormatInt(int64(counter), 16)
		trimmed := sanitized
		for len(trimmed)+1+len(suffix) > geminiFunctionNameLimit && len(trimmed) > 0 {
			trimmed = trimmed[:len(trimmed)-1]
		}
		candidate = trimmed + "_" + suffix
	}
}

func responsesToolNameAccepted(name string) bool {
	if name == "" || len(name) > claudeFunctionNameLimit || len(name) > geminiFunctionNameLimit {
		return false
	}
	for index, r := range name {
		if r > unicode.MaxASCII {
			return false
		}
		if index == 0 && !unicode.IsLetter(r) && r != '_' {
			return false
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

// rewriteResponsesNamespacedNames applies the flattened-name map to input
// items and a function tool_choice that carry a namespace. Items and choices
// without a namespace are returned unchanged, byte for byte.
func rewriteResponsesNamespacedNames(input json.RawMessage, toolChoice json.RawMessage, refs map[string]NamespaceRef) (json.RawMessage, json.RawMessage) {
	if len(refs) == 0 {
		return input, toolChoice
	}
	byRef := make(map[NamespaceRef]string, len(refs))
	for flat, ref := range refs {
		byRef[ref] = flat
	}
	return rewriteResponsesNamespacedInput(input, byRef), rewriteResponsesNamespacedChoice(toolChoice, byRef)
}

func rewriteResponsesNamespacedInput(input json.RawMessage, byRef map[NamespaceRef]string) json.RawMessage {
	if len(input) == 0 || kitutil.GetJsonType(input) != "array" {
		return input
	}
	var items []json.RawMessage
	if err := kitutil.Unmarshal(input, &items); err != nil {
		return input
	}
	changed := false
	for index, rawItem := range items {
		var item map[string]json.RawMessage
		if err := kitutil.Unmarshal(rawItem, &item); err != nil {
			continue
		}
		itemType := strings.TrimSpace(kitutil.JsonRawMessageToString(item["type"]))
		if itemType != "function_call" && itemType != "custom_tool_call" && itemType != "function_call_output" {
			continue
		}
		namespace := strings.TrimSpace(kitutil.JsonRawMessageToString(item["namespace"]))
		name := strings.TrimSpace(kitutil.JsonRawMessageToString(item["name"]))
		if namespace == "" || name == "" {
			continue
		}
		flat, ok := byRef[NamespaceRef{Namespace: namespace, Name: name}]
		if !ok {
			continue
		}
		rewritten, err := patchNamespacedName(item, flat)
		if err != nil {
			continue
		}
		items[index] = rewritten
		changed = true
	}
	if !changed {
		return input
	}
	encoded, err := kitutil.Marshal(items)
	if err != nil {
		return input
	}
	return encoded
}

func rewriteResponsesNamespacedChoice(raw json.RawMessage, byRef map[NamespaceRef]string) json.RawMessage {
	if len(raw) == 0 || kitutil.GetJsonType(raw) != "object" {
		return raw
	}
	var choice map[string]json.RawMessage
	if err := kitutil.Unmarshal(raw, &choice); err != nil {
		return raw
	}
	if strings.TrimSpace(kitutil.JsonRawMessageToString(choice["type"])) != "function" {
		return raw
	}
	namespace := strings.TrimSpace(kitutil.JsonRawMessageToString(choice["namespace"]))
	name := strings.TrimSpace(kitutil.JsonRawMessageToString(choice["name"]))
	if namespace == "" || name == "" {
		return raw
	}
	flat, ok := byRef[NamespaceRef{Namespace: namespace, Name: name}]
	if !ok {
		return raw
	}
	rewritten, err := patchNamespacedName(choice, flat)
	if err != nil {
		return raw
	}
	return rewritten
}
