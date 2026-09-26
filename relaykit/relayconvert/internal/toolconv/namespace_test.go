package toolconv

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFlattenResponsesNamespaceTools(t *testing.T) {
	t.Parallel()

	tools, err := kitutil.Marshal([]any{
		map[string]any{
			"type":        "namespace",
			"name":        "mcp__memory",
			"description": "Tools in the mcp__memory namespace.",
			"tools": []any{
				map[string]any{"type": "function", "name": "memory_search", "description": "search", "strict": false, "defer_loading": true, "parameters": map[string]any{"type": "object"}},
				map[string]any{"type": "function", "name": "memory_list", "parameters": map[string]any{"type": "object"}},
				map[string]any{"type": "web_search"},
			},
		},
		map[string]any{"type": "function", "name": "plain", "parameters": map[string]any{"type": "object"}},
	})
	require.NoError(t, err)
	input, err := kitutil.Marshal([]any{
		map[string]any{"type": "function_call", "name": "memory_search", "namespace": "mcp__memory", "call_id": "c1", "arguments": "{}"},
		map[string]any{"type": "custom_tool_call", "name": "memory_list", "namespace": "mcp__memory", "call_id": "c2", "input": "x"},
		map[string]any{"type": "function_call_output", "call_id": "c1", "output": "ok"},
	})
	require.NoError(t, err)
	choice, err := kitutil.Marshal(map[string]any{"type": "function", "name": "memory_search", "namespace": "mcp__memory"})
	require.NoError(t, err)

	req := &dto.OpenAIResponsesRequest{Model: "m", Tools: tools, Input: input, ToolChoice: choice}
	converted, set, err := ExtractRequest(types.RelayFormatOpenAIResponses, req)
	require.NoError(t, err)
	assert.JSONEq(t, string(input), string(req.Input), "the caller request must not be mutated")

	var functions []string
	var natives int
	for _, definition := range set.Definitions {
		switch definition.Kind {
		case KindFunction:
			require.NotNil(t, definition.Function)
			functions = append(functions, definition.Function.Name)
			assert.Equal(t, definition.Function.Name, definition.Name)
		default:
			natives++
		}
	}
	assert.Equal(t, []string{"mcp__memory__memory_search", "mcp__memory__memory_list", "plain"}, functions)
	assert.Equal(t, 0, natives, "non-function nested tools are dropped with their group")
	require.NotNil(t, set.Choice)
	assert.Equal(t, "mcp__memory__memory_search", set.Choice.Name)

	convertedRequest, ok := converted.(*dto.OpenAIResponsesRequest)
	require.True(t, ok)
	var items []map[string]any
	require.NoError(t, kitutil.Unmarshal(convertedRequest.Input, &items))
	assert.Equal(t, "mcp__memory__memory_search", items[0]["name"])
	assert.Equal(t, "mcp__memory__memory_list", items[1]["name"])
	_, hasNamespace := items[0]["namespace"]
	assert.False(t, hasNamespace, "rewritten history items must not keep a stale namespace")

	ref, ok := set.NamespaceRefs["mcp__memory__memory_search"]
	require.True(t, ok)
	assert.Equal(t, NamespaceRef{Namespace: "mcp__memory", Name: "memory_search"}, ref)
	_, ok = set.NamespaceRefs["plain"]
	assert.False(t, ok, "plain function tools must not enter the restore map")
}

func TestFlattenResponsesNamespaceNameLimitsAndCollisions(t *testing.T) {
	t.Parallel()

	// 61 chars: "ns__" + name exceeds the 64-char Gemini limit, so both
	// duplicates go through the shorten path and the second one must
	// disambiguate with a counter instead of spinning forever.
	longName := strings.Repeat("n", 61)
	tools, err := kitutil.Marshal([]any{
		map[string]any{"type": "namespace", "name": "ns", "tools": []any{
			map[string]any{"type": "function", "name": longName},
			map[string]any{"type": "function", "name": longName},
		}},
	})
	require.NoError(t, err)

	req := &dto.OpenAIResponsesRequest{Tools: tools}
	_, set, err := ExtractRequest(types.RelayFormatOpenAIResponses, req)
	require.NoError(t, err)
	require.Len(t, set.Definitions, 2)

	seen := map[string]bool{}
	for _, definition := range set.Definitions {
		name := definition.Function.Name
		assert.LessOrEqual(t, len(name), geminiFunctionNameLimit)
		assert.Regexp(t, `^[A-Za-z_][A-Za-z0-9_.:-]*$`, name)
		assert.Regexp(t, `^[a-zA-Z0-9_-]{1,128}$`, name)
		assert.False(t, seen[name], "flattened names must be unique, got %q twice", name)
		seen[name] = true
		ref, ok := set.NamespaceRefs[name]
		require.True(t, ok)
		assert.Equal(t, NamespaceRef{Namespace: "ns", Name: longName}, ref)
	}
	assert.NotEqual(t, set.Definitions[0].Function.Name, set.Definitions[1].Function.Name)
}

func TestExtractResponsesWithoutNamespaceIsUntouched(t *testing.T) {
	t.Parallel()

	tools, err := kitutil.Marshal([]any{map[string]any{"type": "function", "name": "plain", "parameters": map[string]any{"type": "object"}}})
	require.NoError(t, err)
	input, err := kitutil.Marshal([]any{map[string]any{"type": "function_call", "name": "plain", "call_id": "c1", "arguments": "{}"}})
	require.NoError(t, err)
	originalInput := append(json.RawMessage(nil), input...)

	req := &dto.OpenAIResponsesRequest{Tools: tools, Input: input}
	_, set, err := ExtractRequest(types.RelayFormatOpenAIResponses, req)
	require.NoError(t, err)
	assert.Empty(t, set.NamespaceRefs)
	assert.Equal(t, "plain", set.Definitions[0].Function.Name)
	assert.JSONEq(t, string(originalInput), string(req.Input))
}

func TestRewriteResponsesNamespacedPreservesNumbers(t *testing.T) {
	t.Parallel()

	req := &dto.OpenAIResponsesRequest{
		Tools: json.RawMessage(`[{"type":"namespace","name":"ns","tools":[` +
			`{"type":"function","name":"a","parameters":{"type":"object","properties":{"big":{"maximum":12345678901234567890}}}}]}]`),
		Input: json.RawMessage(`[
			{"type":"function_call","name":"a","namespace":"ns","call_id":"c1","arguments":"{}"},
			{"type":"message","role":"user","content":"hi","metadata":{"snowflake":12345678901234567890,"neg":-12345678901234567890}}
		]`),
	}
	_, set, err := ExtractRequest(types.RelayFormatOpenAIResponses, req)
	require.NoError(t, err)
	require.Contains(t, set.NamespaceRefs, "ns__a")

	rewritten, _ := rewriteResponsesNamespacedNames(req.Input, nil, set.NamespaceRefs)
	assert.Contains(t, string(rewritten), `"name":"ns__a"`)
	assert.Contains(t, string(rewritten), "12345678901234567890",
		"numbers in untouched sibling input items must survive the rewrite verbatim")
	assert.Contains(t, string(rewritten), "-12345678901234567890")
	require.Len(t, set.Definitions, 1)
	assert.Equal(t, "ns__a", set.Definitions[0].Function.Name)
	assert.Contains(t, string(set.Definitions[0].Raw), "12345678901234567890",
		"nested tool parameter numbers must survive the rewrite verbatim")
}
