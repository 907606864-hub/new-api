package relayconvert

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/require"
)

// Exercise the public request/stream route, not native Responses fixtures. A
// mid-stream finish_reason is the supported boundary for a new reasoning item;
// completed tool indexes are deliberately not reused in the second segment.
func TestUpgrade20261017ChatResponsesReasoningNamespaceCustomStream(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name            string
		customTool      string
		customUpstream  string
		customNamespace string
	}{
		{
			name:           "bare_custom_same_leaf",
			customTool:     `{"type":"custom","name":"exec","format":{"type":"text"}}`,
			customUpstream: "exec",
		},
		{
			name:            "namespaced_custom_same_leaf",
			customTool:      `{"type":"namespace","name":"patches","tools":[{"type":"custom","name":"exec","format":{"type":"text"}}]}`,
			customUpstream:  "patches__exec",
			customNamespace: "patches",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			info := &convmeta.Values{}
			request := &dto.OpenAIResponsesRequest{
				Model: "gpt-test", Input: json.RawMessage(`"hello"`),
				Tools: json.RawMessage(`[
					{"type":"namespace","name":"ops","tools":[{"type":"function","name":"exec","parameters":{"type":"object","properties":{"cmd":{"type":"string"},"dir":{"type":"string"}}}}]},
					` + tt.customTool + `]`),
			}
			converted, err := ConvertRequest(ctx, info, types.RelayFormatOpenAI, request)
			require.NoError(t, err)
			chat, ok := converted.Value.(*dto.GeneralOpenAIRequest)
			require.True(t, ok)
			require.Len(t, chat.Tools, 2)
			require.Equal(t, "ops__exec", chat.Tools[0].Function.Name)
			require.Equal(t, tt.customUpstream, chat.Tools[1].Function.Name)
			require.NotNil(t, info.ResponsesToolState())
			require.False(t, info.ResponsesToolState().IsCustomTool("ops__exec"))
			require.True(t, info.ResponsesToolState().IsCustomTool(tt.customUpstream))
			require.Equal(t, convmeta.NamespaceToolRef{Namespace: "ops", Name: "exec"}, info.ResponsesNamespaceTools()["ops__exec"])
			if tt.customNamespace != "" {
				require.Equal(t, convmeta.NamespaceToolRef{Namespace: tt.customNamespace, Name: "exec"}, info.ResponsesNamespaceTools()[tt.customUpstream])
			}

			const responseID = "resp_upgrade_20261017"
			const functionID, customID = "call_upgrade_ns", "call_upgrade_custom"
			const functionArguments = `{"cmd":"ls","dir":"."}`
			const customInput = "echo \"hi\"\n第二行"
			wantReasoning := map[string]string{
				responseID + "_reasoning_0": "plan one",
				responseID + "_reasoning_1": "plan two",
			}
			wantText := map[string]string{
				responseID + "_msg_0": "calling tools",
				responseID + "_msg_1": "done",
			}
			wantOrder := []string{
				responseID + "_reasoning_0", responseID + "_msg_0", customID, functionID,
				responseID + "_reasoning_1", responseID + "_msg_1",
			}
			wantTypes := map[string]string{
				wantOrder[0]: "reasoning", wantOrder[1]: "message", customID: "custom_tool_call",
				functionID: "function_call", wantOrder[4]: "reasoning", wantOrder[5]: "message",
			}
			state, err := NewResponseStreamState(types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, ResponseStreamOptions{
				ID: responseID, Model: "gpt-test", Created: 1700000000, EmitSequenceNumber: true,
			})
			require.NoError(t, err)

			checkItem := func(item *dto.ResponsesOutput, raw json.RawMessage, added bool) {
				t.Helper()
				require.NotNil(t, item)
				require.Equal(t, wantTypes[item.ID], item.Type)
				status := "completed"
				if added {
					status = "in_progress"
				}
				require.Equal(t, status, item.Status)
				var fields map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(raw, &fields))
				switch item.Type {
				case "reasoning":
					if added {
						// Inspect the serialized token: both missing and null must fail.
						require.Equal(t, "[]", string(fields["summary"]), "reasoning added %s", item.ID)
					} else {
						require.Equal(t, []dto.ResponsesReasoningSummaryPart{{Type: "summary_text", Text: wantReasoning[item.ID]}}, item.Summary)
					}
				case "function_call", "custom_tool_call":
					require.Equal(t, item.ID, item.CallId)
					require.Equal(t, "exec", item.Name)
					require.NotContains(t, fields, "summary")
					var value string
					if item.Type == "function_call" {
						require.Equal(t, "ops", item.Namespace)
						require.NotContains(t, fields, "input")
						require.NoError(t, json.Unmarshal(fields["arguments"], &value))
						if !added {
							require.Equal(t, functionArguments, value)
						}
					} else {
						require.Equal(t, tt.customNamespace, item.Namespace)
						if tt.customNamespace == "" {
							require.NotContains(t, fields, "namespace")
						}
						require.NotContains(t, fields, "arguments")
						require.NotContains(t, fields, "content")
						require.NotContains(t, fields, "role")
						require.NoError(t, json.Unmarshal(fields["input"], &value))
						if !added {
							require.Equal(t, customInput, value)
						}
					}
					if added {
						require.Empty(t, value)
					}
				case "message":
					require.Equal(t, "assistant", item.Role)
					if !added {
						require.Len(t, item.Content, 1)
						require.Equal(t, "output_text", item.Content[0].Type)
						require.Equal(t, wantText[item.ID], item.Content[0].Text)
					}
				default:
					t.Fatalf("unexpected output item: %s", raw)
				}
			}

			indexes, fieldDone := map[string]int{}, map[string]int{}
			partAdded, partDone := map[string]int{}, map[string]int{}
			open := map[string]bool{}
			doneItems := map[string]json.RawMessage{}
			reasoning, text := map[string]string{}, map[string]string{}
			var addedOrder, reasoningAdded, functionDeltas, customDeltas []string
			nextSequence, createdCount, completedCount := 0, 0, 0
			consume := func(results []ResponseResult) {
				t.Helper()
				for _, result := range results {
					require.True(t, result.Stream)
					event, ok := result.Value.(ChatToResponsesStreamEvent)
					require.True(t, ok)
					// Serialize before feeding the next Chat chunk, as the SSE writer does.
					encoded, err := json.Marshal(event.Payload)
					require.NoError(t, err)
					var p dto.ResponsesStreamResponse
					var raw map[string]json.RawMessage
					require.NoError(t, json.Unmarshal(encoded, &p))
					require.NoError(t, json.Unmarshal(encoded, &raw))
					require.Equal(t, event.Type, p.Type)
					require.NotNil(t, p.SequenceNumber)
					require.Equal(t, nextSequence, *p.SequenceNumber)
					nextSequence++

					id := p.ItemID
					if p.Item != nil {
						if id != "" {
							require.Equal(t, p.Item.ID, id)
						}
						id = p.Item.ID
					}
					if p.Type == "response.output_item.added" {
						require.NotNil(t, p.OutputIndex)
						require.NotEmpty(t, id)
						require.NotContains(t, indexes, id)
						require.Equal(t, len(addedOrder), *p.OutputIndex)
						indexes[id], open[id] = *p.OutputIndex, true
						addedOrder = append(addedOrder, id)
					} else if p.Type != "response.created" && p.Type != "response.completed" {
						require.NotNil(t, p.OutputIndex, "%s", p.Type)
						require.Contains(t, indexes, id)
						require.Equal(t, indexes[id], *p.OutputIndex)
						require.True(t, open[id], "%s for closed item %s", p.Type, id)
					}
					if strings.HasPrefix(p.Type, "response.reasoning_summary_") {
						require.NotNil(t, p.SummaryIndex)
						require.Zero(t, *p.SummaryIndex)
					}
					switch p.Type {
					case "response.created":
						createdCount++
						require.Equal(t, 0, *p.SequenceNumber)
						require.NotNil(t, p.Response)
						require.Equal(t, responseID, p.Response.ID)
						require.Empty(t, p.Response.Output)
					case "response.output_item.added":
						checkItem(p.Item, raw["item"], true)
						if p.Item.Type == "reasoning" {
							reasoningAdded = append(reasoningAdded, id)
						}
					case "response.output_item.done":
						checkItem(p.Item, raw["item"], false)
						require.Equal(t, 1, fieldDone[id], "content done must precede item done")
						require.NotContains(t, doneItems, id)
						doneItems[id] = raw["item"]
						delete(open, id)
					case "response.reasoning_summary_part.added", "response.reasoning_summary_part.done":
						require.NotNil(t, p.Part)
						require.Equal(t, "summary_text", p.Part.Type)
						if p.Type == "response.reasoning_summary_part.added" {
							partAdded[id]++
							require.Empty(t, p.Part.Text)
						} else {
							partDone[id]++
							require.Equal(t, wantReasoning[id], p.Part.Text)
							require.Equal(t, reasoning[id], p.Part.Text)
						}
					case "response.reasoning_summary_text.delta":
						reasoning[id] += p.Delta
					case "response.reasoning_summary_text.done":
						require.NotNil(t, p.Text)
						require.Equal(t, wantReasoning[id], *p.Text)
						require.Equal(t, reasoning[id], *p.Text)
						fieldDone[id]++
					case "response.output_text.delta":
						require.NotNil(t, p.ContentIndex)
						require.Zero(t, *p.ContentIndex)
						text[id] += p.Delta
					case "response.output_text.done":
						require.NotNil(t, p.Text)
						require.Equal(t, wantText[id], *p.Text)
						require.Equal(t, text[id], *p.Text)
						fieldDone[id]++
					case "response.function_call_arguments.delta":
						require.Equal(t, functionID, id)
						functionDeltas = append(functionDeltas, p.Delta)
					case "response.function_call_arguments.done":
						require.Equal(t, functionID, id)
						require.Equal(t, "exec", p.Name)
						require.Equal(t, "ops", p.Namespace)
						require.NotNil(t, p.Arguments)
						require.Equal(t, functionArguments, *p.Arguments)
						require.Equal(t, strings.Join(functionDeltas, ""), *p.Arguments)
						fieldDone[id]++
					case "response.custom_tool_call_input.delta":
						require.Equal(t, customID, id)
						customDeltas = append(customDeltas, p.Delta)
					case "response.custom_tool_call_input.done":
						require.Equal(t, customID, id)
						require.NotNil(t, p.Input)
						require.Equal(t, customInput, *p.Input)
						require.Equal(t, strings.Join(customDeltas, ""), *p.Input)
						fieldDone[id]++
					case "response.completed":
						completedCount++
						require.Empty(t, open)
						require.NotNil(t, p.Response)
						require.Equal(t, responseID, p.Response.ID)
						require.Equal(t, `"completed"`, string(p.Response.Status))
						var response struct{ Output []json.RawMessage }
						require.NoError(t, json.Unmarshal(raw["response"], &response))
						require.Len(t, response.Output, len(wantOrder))
						for i, item := range p.Response.Output {
							require.Equal(t, wantOrder[i], item.ID)
							require.Equal(t, i, indexes[item.ID])
							checkItem(&item, response.Output[i], false)
							require.JSONEq(t, string(doneItems[item.ID]), string(response.Output[i]), "done/completed item %s", item.ID)
						}
					default:
						t.Fatalf("unexpected converted frame: %s", encoded)
					}
				}
			}

			for _, raw := range []string{
				`{"id":"chatcmpl_upgrade","model":"gpt-test","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"plan "}}]}`,
				`{"choices":[{"index":0,"delta":{"reasoning_content":"one","content":"calling tools"}}]}`,
				// Index 0 arrives nameless; index 1 must be announced ahead of it.
				`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_upgrade_ns","type":"function","function":{"arguments":"{\"cmd\":"}}]}}]}`,
				`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_upgrade_custom","type":"function","function":{"name":"CUSTOM_UPSTREAM","arguments":"{\"input\":"}}]}}]}`,
				`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":"ops__exec","arguments":"\"ls\","}}]}}]}`,
				`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"dir\":\".\"}"}},{"index":1,"function":{"arguments":"\"echo \\\"hi\\\"\\n第二行\"}"}}]},"finish_reason":"tool_calls"}]}`,
				`{"choices":[{"index":0,"delta":{"reasoning_content":"plan "}}]}`,
				`{"choices":[{"index":0,"delta":{"reasoning_content":"two","content":"done"},"finish_reason":"stop"}]}`,
			} {
				chunk := chatStreamChunk(strings.ReplaceAll(raw, "CUSTOM_UPSTREAM", tt.customUpstream))
				results, err := ConvertStreamResponseChunk(ctx, info, state, chunk)
				require.NoError(t, err)
				consume(results)
			}
			require.Zero(t, completedCount, "finish_reason closes items, not the response stream")
			finals, err := FinalizeStreamResponse(ctx, info, state)
			require.NoError(t, err)
			consume(finals)
			repeated, err := FinalizeStreamResponse(ctx, info, state)
			require.NoError(t, err)
			require.Empty(t, repeated)
			require.Equal(t, 1, createdCount)
			require.Equal(t, 1, completedCount)
			require.Equal(t, wantOrder, addedOrder)
			require.Equal(t, []string{wantOrder[0], wantOrder[4]}, reasoningAdded)
			require.Equal(t, map[string]int{wantOrder[0]: 1, wantOrder[4]: 1}, partAdded)
			require.Equal(t, partAdded, partDone)
			require.Len(t, doneItems, len(wantOrder))
			require.Equal(t, []string{`{"cmd":"ls",`, `"dir":"."}`}, functionDeltas)
			require.Equal(t, []string{customInput}, customDeltas)
			t.Logf("serialized %d frames; two reasoning added summaries were []; output order=%v", nextSequence, addedOrder)
		})
	}
}
