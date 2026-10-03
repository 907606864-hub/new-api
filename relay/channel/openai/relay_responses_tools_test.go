package openai

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func nativeResponsesToolInfo() *relaycommon.RelayInfo {
	info := &relaycommon.RelayInfo{
		OriginModelName: "grok-test",
		DisablePing:     true,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenAI,
			UpstreamModelName: "grok-test",
		},
	}
	info.SetResponsesNamespaceTools(map[string]convmeta.NamespaceToolRef{
		"ns__edit": {Namespace: "ns", Name: "edit"},
		"fn__edit": {Namespace: "fn", Name: "edit"},
	})
	info.SetResponsesToolState(&convmeta.ResponsesToolState{CustomToolNames: map[string]struct{}{
		"ns__edit": {},
		"edit":     {},
	}})
	return info
}

const nativeResponsesToolsBody = `{
 "id":"resp_1","model":"grok-test","status":"completed",
 "output":[
  {"type":"function_call","id":"fn","call_id":"call_fn","name":"fn__edit","arguments":"{\"n\":1}","future":900719925474099312345},
  {"type":"function_call","id":"ns","call_id":"call_ns","name":"ns__edit","arguments":"{\"input\":\"patch\\n+line\"}","future":{"n":900719925474099312345}},
  {"type":"function_call","id":"top","call_id":"call_top","name":"edit","arguments":"{\"input\":\"top\"}"},
  {"type":"web_search_call","id":"web","action":{"type":"search","query":"x","unknown":900719925474099312345}},
  {"type":"x_search_call","id":"x","results":[{"id":900719925474099312345,"url":"https://x.com/a/status/1"}]},
  {"type":"reasoning","encrypted_content":"opaque","summary":[{"type":"summary_text","text":"reason"}],"unknown":[1, 2]},
  {"type":"message","role":"assistant","content":[{"type":"output_text","text":"answer","annotations":[{"type":"url_citation","url":"https://example.com","start_index":0,"end_index":6,"future":true}]}]},
  {"type":"function_call","name":"unmapped","arguments":"{}","future":1.2500},
  {"type":"function_call","name":"edit","arguments":""}
 ],
 "usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5,"future":900719925474099312345},
 "future":{"n":900719925474099312345,"decimal":1.23000,"spacing":[1,  2]}
}`

func TestNativeResponsesToolsNonStreamLossless(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(nativeResponsesToolsBody)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
	usage, apiErr := OaiResponsesHandler(c, nativeResponsesToolInfo(), resp)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 5, usage.TotalTokens)
	body := w.Body.String()
	require.True(t, gjson.Valid(body))
	assert.Equal(t, "function_call", gjson.Get(body, "output.0.type").String())
	assert.Equal(t, "edit", gjson.Get(body, "output.0.name").String())
	assert.Equal(t, "fn", gjson.Get(body, "output.0.namespace").String())
	assert.Equal(t, `{"n":1}`, gjson.Get(body, "output.0.arguments").String())
	assert.Equal(t, "custom_tool_call", gjson.Get(body, "output.1.type").String())
	assert.Equal(t, "ns", gjson.Get(body, "output.1.namespace").String())
	assert.Equal(t, "edit", gjson.Get(body, "output.1.name").String())
	assert.Equal(t, "patch\n+line", gjson.Get(body, "output.1.input").String())
	assert.False(t, gjson.Get(body, "output.1.arguments").Exists())
	assert.Equal(t, "call_ns", gjson.Get(body, "output.1.call_id").String())
	assert.Equal(t, "custom_tool_call", gjson.Get(body, "output.2.type").String())
	assert.Equal(t, "top", gjson.Get(body, "output.2.input").String())
	assert.False(t, gjson.Get(body, "output.2.namespace").Exists())
	assert.Equal(t, `""`, gjson.Get(body, "output.8.input").Raw)
	for _, path := range []string{"output.0.future", "output.1.future", "output.3", "output.4", "output.5", "output.6", "output.7", "usage", "future"} {
		assert.Equal(t, gjson.Get(nativeResponsesToolsBody, path).Raw, gjson.Get(body, path).Raw, path)
	}
}

func TestNativeResponsesToolsNoMappingsIsByteIdentical(t *testing.T) {
	for _, info := range []*relaycommon.RelayInfo{nil, {}} {
		assert.Equal(t, nativeResponsesToolsBody, string(rewriteResponsesOutputTools(info, []byte(nativeResponsesToolsBody), "output")))
		stream := newResponsesToolStream(info)
		for _, event := range []string{
			`{ "type":"response.output_item.added", "sequence_number":1,"item":{"type":"function_call","name":"ns__edit","arguments":""},"unknown":900719925474099312345 }`,
			`{"type":"response.function_call_arguments.delta","sequence_number":2,"item_id":"x","delta":"{\"input\":"}`,
			`{"type":"response.function_call_arguments.done","sequence_number":3,"name":"ns__edit","arguments":"{}"}`,
			`{"type":"response.completed","sequence_number":4,"response":` + nativeResponsesToolsBody + `}`,
			`{"type":"response.future","sequence_number":5,"unknown":1.23000}`,
		} {
			out := stream.rewrite([]byte(event))
			require.Len(t, out, 1)
			assert.Equal(t, event, string(out[0]))
		}
	}
	// Even with mappings configured, an unrelated output is not reserialized.
	unknown := `{"output":[ { "type":"function_call", "name":"other", "arguments":"{}", "n":900719925474099312345 } ]}`
	assert.Equal(t, unknown, string(rewriteResponsesOutputTools(nativeResponsesToolInfo(), []byte(unknown), "output")))
}

func TestNativeResponsesToolsResponseSnapshots(t *testing.T) {
	for _, kind := range []string{"response.created", "response.in_progress", "response.completed", "response.incomplete"} {
		t.Run(kind, func(t *testing.T) {
			payload := `{"type":"` + kind + `","sequence_number":7,"response":` + nativeResponsesToolsBody + `,"future":900719925474099312345}`
			out := newResponsesToolStream(nativeResponsesToolInfo()).rewrite([]byte(payload))
			require.Len(t, out, 1)
			assert.Equal(t, "custom_tool_call", gjson.GetBytes(out[0], "response.output.1.type").String())
			assert.Equal(t, "patch\n+line", gjson.GetBytes(out[0], "response.output.1.input").String())
			assert.Equal(t, "fn", gjson.GetBytes(out[0], "response.output.0.namespace").String())
			for _, path := range []string{"future", "response.usage", "response.output.3", "response.output.4", "response.output.5", "response.output.6"} {
				assert.Equal(t, gjson.Get(payload, path).Raw, gjson.GetBytes(out[0], path).Raw, path)
			}
		})
	}
}

func TestNativeResponsesOrdinaryFunctionDeltaIsUntouched(t *testing.T) {
	info := nativeResponsesToolInfo()
	info.SetResponsesToolState(nil)
	stream := newResponsesToolStream(info)
	stream.rewrite([]byte(`{"type":"response.output_item.added","item":{"type":"function_call","id":"fn","name":"fn__edit","arguments":""}}`))
	delta := `{ "type":"response.function_call_arguments.delta", "sequence_number":9,"item_id":"fn","delta":"{\"n\":", "future":900719925474099312345 }`
	require.Equal(t, [][]byte{[]byte(delta)}, stream.rewrite([]byte(delta)))
	// No root name: recover it from the earlier item announcement.
	out := stream.rewrite([]byte(`{"type":"response.function_call_arguments.done","sequence_number":9,"item_id":"fn","arguments":"{\"n\":1}"}`))
	require.Len(t, out, 1)
	assert.Equal(t, "edit", gjson.GetBytes(out[0], "name").String())
	assert.Equal(t, "fn", gjson.GetBytes(out[0], "namespace").String())
	assert.Equal(t, int64(9), gjson.GetBytes(out[0], "sequence_number").Int())
	assert.Equal(t, "response.function_call_arguments.done", gjson.GetBytes(out[0], "type").String())
}

func nativeToolTestJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := common.Marshal(value)
	require.NoError(t, err)
	return string(encoded)
}

func nativeToolInterleavedEvents(t *testing.T) []string {
	t.Helper()
	var events []string
	add := func(kind string, fields map[string]any) {
		fields["type"], fields["sequence_number"] = kind, len(events)
		events = append(events, nativeToolTestJSON(t, fields))
	}
	item := func(id, name, arguments, status string) map[string]any {
		return map[string]any{"type": "function_call", "id": id, "call_id": "call_" + id, "name": name, "arguments": arguments, "status": status}
	}
	delta := func(id string, index int, text string) {
		add("response.function_call_arguments.delta", map[string]any{"item_id": id, "output_index": index, "delta": text})
	}
	nsArgs := `{"input":"patch\n\u4e2d\"quoted\"\\tail"}`
	topArgs := `{"input":"top\npatch"}`
	add("response.created", map[string]any{"response": map[string]any{"id": "resp_1", "status": "in_progress", "output": []any{}}})
	add("response.output_item.added", map[string]any{"output_index": 0, "item": item("ns", "ns__edit", "", "in_progress")})
	add("response.output_item.added", map[string]any{"output_index": 1, "item": item("top", "edit", "", "in_progress")})
	add("response.output_item.added", map[string]any{"output_index": 2, "item": item("fn", "fn__edit", "", "in_progress")})
	// Split both the wrapper and a JSON escape; custom tools are interleaved.
	delta("ns", 0, nsArgs[:9])
	delta("top", 1, topArgs[:8])
	delta("fn", 2, `{"n":`)
	add("response.web_search_call.searching", map[string]any{"item_id": "web", "future": json.RawMessage(`{"n":900719925474099312345,"spacing":[1,  2]}`)})
	delta("ns", 0, nsArgs[9:16])
	delta("top", 1, topArgs[8:])
	delta("fn", 2, `1}`)
	delta("ns", 0, nsArgs[16:])
	// Missing arguments exercises the buffered fragments; item_id resolves name.
	add("response.function_call_arguments.done", map[string]any{"item_id": "top", "call_id": "call_top", "output_index": 1, "future": json.RawMessage(`900719925474099312345`)})
	add("response.x_search_call.done", map[string]any{"item_id": "x", "results": json.RawMessage(`[{"id":900719925474099312345,"url":"https://x.com/a/status/1"}]`)})
	add("response.function_call_arguments.done", map[string]any{"item_id": "ns", "call_id": "call_ns", "output_index": 0, "name": "ns__edit", "arguments": nsArgs})
	add("response.function_call_arguments.done", map[string]any{"item_id": "fn", "output_index": 2, "name": "fn__edit", "arguments": `{"n":1}`})
	add("response.output_item.done", map[string]any{"output_index": 1, "item": item("top", "edit", topArgs, "completed")})
	nsDone := item("ns", "ns__edit", nsArgs, "completed")
	delete(nsDone, "arguments")
	add("response.output_item.done", map[string]any{"output_index": 0, "item": nsDone})
	add("response.output_item.done", map[string]any{"output_index": 2, "item": item("fn", "fn__edit", `{"n":1}`, "completed")})
	web := map[string]any{"type": "web_search_call", "id": "web", "status": "completed", "action": map[string]any{"type": "search", "query": "x"}}
	add("response.output_item.done", map[string]any{"output_index": 3, "item": web})
	add("response.reasoning_text.delta", map[string]any{"delta": "reason", "obfuscation": "opaque", "future": json.RawMessage(`900719925474099312345`)})
	add("response.completed", map[string]any{"response": map[string]any{
		"id": "resp_1", "model": "grok-test", "status": "completed",
		"output": []any{item("ns", "ns__edit", nsArgs, "completed"), item("top", "edit", topArgs, "completed"), item("fn", "fn__edit", `{"n":1}`, "completed"), web},
		"usage":  json.RawMessage(`{"input_tokens":2,"output_tokens":3,"total_tokens":5,"future":900719925474099312345}`),
	}})
	return events
}

func TestNativeResponsesToolsInterleavedStreamAndOriginalBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	operation_setting.SetToolPriceForTest("edit", 5.0)
	t.Cleanup(func() { operation_setting.DeleteToolPriceForTest("edit") })

	upstream := nativeToolInterleavedEvents(t)
	var body strings.Builder
	for _, event := range upstream {
		body.WriteString("data: " + event + "\n\n")
	}
	body.WriteString("data: [DONE]\n\n")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(common.RequestIdKey, "native-tools-test")
	info := nativeResponsesToolInfo()
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body.String())), Header: http.Header{"Content-Type": []string{"text/event-stream"}}}
	usage, apiErr := OaiResponsesStreamHandler(c, info, resp)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 5, usage.TotalTokens)
	require.NotNil(t, info.ResponsesUsageInfo)
	require.Contains(t, info.ResponsesUsageInfo.BuiltInTools, "edit")
	assert.Equal(t, 3, info.ResponsesUsageInfo.BuiltInTools["edit"].CallCount, "both restored custom calls must still be billed as original upstream function calls")
	require.Contains(t, info.ResponsesUsageInfo.BuiltInTools, dto.BuildInToolWebSearchPreview)
	assert.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].CallCount)

	lastSequence := int64(-1)
	inputs, deltas := map[string]string{}, map[string]string{}
	items := map[string]gjson.Result{}
	var completed string
	ordinaryDeltas := 0
	var eventName string
	for line := range strings.SplitSeq(w.Body.String(), "\n") {
		if name, ok := strings.CutPrefix(line, "event: "); ok {
			eventName = name
			continue
		}
		payload, ok := strings.CutPrefix(line, "data: ")
		if !ok {
			continue
		}
		require.True(t, gjson.Valid(payload), payload)
		kind := gjson.Get(payload, "type").String()
		assert.Equal(t, kind, eventName, "SSE event name must match the rewritten JSON type")
		sequence := gjson.Get(payload, "sequence_number").Int()
		assert.Greater(t, sequence, lastSequence)
		lastSequence = sequence
		switch kind {
		case "response.function_call_arguments.delta":
			assert.Equal(t, "fn", gjson.Get(payload, "item_id").String())
			ordinaryDeltas++
		case "response.custom_tool_call_input.delta", "response.custom_tool_call_input.done":
			id := gjson.Get(payload, "item_id").String()
			assert.Equal(t, "call_"+id, gjson.Get(payload, "call_id").String())
			assert.False(t, gjson.Get(payload, "arguments").Exists())
			if id == "ns" {
				assert.Equal(t, int64(0), gjson.Get(payload, "output_index").Int())
				assert.Equal(t, "ns", gjson.Get(payload, "namespace").String())
				assert.Equal(t, "edit", gjson.Get(payload, "name").String())
			} else {
				assert.Equal(t, "top", id)
				assert.Equal(t, int64(1), gjson.Get(payload, "output_index").Int())
				assert.False(t, gjson.Get(payload, "namespace").Exists())
				assert.Equal(t, "900719925474099312345", gjson.Get(payload, "future").Raw)
			}
			if strings.HasSuffix(kind, ".delta") {
				deltas[id] += gjson.Get(payload, "delta").String()
			} else {
				inputs[id] = gjson.Get(payload, "input").String()
			}
		case "response.function_call_arguments.done":
			assert.Equal(t, "fn", gjson.Get(payload, "item_id").String())
			assert.Equal(t, "edit", gjson.Get(payload, "name").String())
			assert.Equal(t, "fn", gjson.Get(payload, "namespace").String())
		case "response.output_item.added":
			if gjson.Get(payload, "item.id").String() != "fn" {
				assert.Equal(t, "custom_tool_call", gjson.Get(payload, "item.type").String())
				assert.Equal(t, `""`, gjson.Get(payload, "item.input").Raw)
				assert.False(t, gjson.Get(payload, "item.arguments").Exists())
			}
		case "response.output_item.done":
			items[gjson.Get(payload, "item.id").String()] = gjson.Get(payload, "item")
		case "response.completed":
			completed = payload
		case "response.web_search_call.searching", "response.x_search_call.done", "response.reasoning_text.delta":
			for _, original := range upstream {
				if gjson.Get(original, "type").String() == kind {
					before, err := sjson.Delete(original, "sequence_number")
					require.NoError(t, err)
					after, err := sjson.Delete(payload, "sequence_number")
					require.NoError(t, err)
					assert.Equal(t, before, after, "only sequence_number may change on native events")
				}
			}
		}
	}
	assert.Equal(t, 2, ordinaryDeltas)
	assert.Equal(t, map[string]string{"ns": "patch\n中\"quoted\"\\tail", "top": "top\npatch"}, inputs)
	assert.Equal(t, inputs, deltas)
	require.NotEmpty(t, completed)
	for _, output := range gjson.Get(completed, "response.output").Array() {
		id := output.Get("id").String()
		require.Contains(t, items, id)
		assert.JSONEq(t, items[id].Raw, output.Raw, "output_item.done must agree with response.completed")
		if input, ok := inputs[id]; ok {
			assert.Equal(t, input, output.Get("input").String())
			assert.Equal(t, "custom_tool_call", output.Get("type").String())
			assert.Equal(t, "call_"+id, output.Get("call_id").String())
		}
	}
	assert.Equal(t, gjson.Get(upstream[len(upstream)-1], "response.usage").Raw, gjson.Get(completed, "response.usage").Raw)
}

func TestNativeResponsesCustomEmptyAndNonWrapperArguments(t *testing.T) {
	for _, arguments := range []string{"", "{}", "not-json"} {
		t.Run(arguments, func(t *testing.T) {
			payload := nativeToolTestJSON(t, map[string]any{"type": "response.function_call_arguments.done", "name": "ns__edit", "item_id": "ns", "sequence_number": 0, "arguments": arguments})
			out := newResponsesToolStream(nativeResponsesToolInfo()).rewrite([]byte(payload))
			require.NotEmpty(t, out)
			done := out[len(out)-1]
			assert.Equal(t, "response.custom_tool_call_input.done", gjson.GetBytes(done, "type").String())
			assert.Equal(t, arguments, gjson.GetBytes(done, "input").String())
			assert.Equal(t, "ns", gjson.GetBytes(done, "namespace").String())
			assert.False(t, gjson.GetBytes(done, "arguments").Exists())
			if arguments == "" {
				require.Len(t, out, 1)
				assert.Equal(t, `""`, gjson.GetBytes(done, "input").Raw)
			}
		})
	}
}
