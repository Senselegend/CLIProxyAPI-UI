package ollama

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/tidwall/gjson"
)

func TestRewriteToolCallsArgsToString(t *testing.T) {
	in := []byte(`{"messages":[{"role":"assistant","tool_calls":[{"function":{"name":"x","arguments":{"a":1,"b":"c"}}}]}]}`)
	out, err := rewriteToolCallsArgsToString(in, "messages")
	if err != nil {
		t.Fatalf("rewrite failed: %v", err)
	}
	args := gjson.GetBytes(out, "messages.0.tool_calls.0.function.arguments")
	if args.Type != gjson.String {
		t.Fatalf("arguments should be string, got %v raw=%s", args.Type, args.Raw)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(args.String()), &parsed); err != nil {
		t.Fatalf("arguments string is not JSON: %v", err)
	}
	if parsed["a"].(float64) != 1 || parsed["b"].(string) != "c" {
		t.Fatalf("bad parsed args: %#v", parsed)
	}
}

func TestRewriteToolCallsArgsToStringEmptyAndMissing(t *testing.T) {
	in := []byte(`{"messages":[{"role":"assistant","tool_calls":[{"function":{"name":"empty","arguments":{}}},{"function":{"name":"missing"}}]}]}`)
	out, err := rewriteToolCallsArgsToString(in, "messages")
	if err != nil {
		t.Fatalf("rewrite failed: %v", err)
	}
	if got := gjson.GetBytes(out, "messages.0.tool_calls.0.function.arguments").String(); got != "{}" {
		t.Fatalf("empty arguments got %q, want {}", got)
	}
	if gjson.GetBytes(out, "messages.0.tool_calls.1.function.arguments").Exists() {
		t.Fatalf("missing arguments field should remain missing: %s", out)
	}
}

func TestRewriteToolCallsArgsToDict(t *testing.T) {
	in := []byte(`{"tool_calls":[{"function":{"name":"x","arguments":"{\"a\":1}"}}]}`)
	out, err := rewriteToolCallsArgsToDict(in, "tool_calls")
	if err != nil {
		t.Fatalf("rewrite failed: %v", err)
	}
	args := gjson.GetBytes(out, "tool_calls.0.function.arguments")
	if !args.IsObject() {
		t.Fatalf("arguments should be object, got %v raw=%s", args.Type, args.Raw)
	}
	if got := args.Get("a").Int(); got != 1 {
		t.Fatalf("args.a = %d, want 1", got)
	}
}

func TestRewriteToolCallsArgsToDictInvalidAndEmpty(t *testing.T) {
	in := []byte(`{"tool_calls":[{"function":{"name":"bad","arguments":"not json{"}},{"function":{"name":"empty","arguments":""}}]}`)
	out, err := rewriteToolCallsArgsToDict(in, "tool_calls")
	if err != nil {
		t.Fatalf("rewrite failed: %v", err)
	}
	if raw := gjson.GetBytes(out, "tool_calls.0.function.arguments").Raw; raw != "{}" {
		t.Fatalf("bad args should become {}, got %s", raw)
	}
	if raw := gjson.GetBytes(out, "tool_calls.1.function.arguments").Raw; raw != "{}" {
		t.Fatalf("empty args should become {}, got %s", raw)
	}
}

func TestToolCallArgumentsRoundTrip(t *testing.T) {
	in := []byte(`{"messages":[{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"x","arguments":{"a":1,"b":"c"}}}]}]}`)
	openai, err := rewriteToolCallsArgsToString(in, "messages")
	if err != nil {
		t.Fatalf("to string: %v", err)
	}
	wrapped := []byte(`{"tool_calls":` + gjson.GetBytes(openai, "messages.0.tool_calls").Raw + `}`)
	ollama, err := rewriteToolCallsArgsToDict(wrapped, "tool_calls")
	if err != nil {
		t.Fatalf("to dict: %v", err)
	}
	args := gjson.GetBytes(ollama, "tool_calls.0.function.arguments")
	if got := args.Get("b").String(); got != "c" {
		t.Fatalf("round trip lost args: %s", ollama)
	}
}

func TestRewriteToolCallsMixedHistoryScope(t *testing.T) {
	in := []byte(`{"messages":[{"role":"user","content":"leave {\"a\":1} alone"},{"role":"assistant","content":"same content","tool_calls":[{"function":{"name":"x","arguments":{"a":1}}}]},{"role":"user","content":"after"}]}`)
	out, err := rewriteToolCallsArgsToString(in, "messages")
	if err != nil {
		t.Fatalf("rewrite failed: %v", err)
	}
	if got := gjson.GetBytes(out, "messages.0.content").String(); got != `leave {"a":1} alone` {
		t.Fatalf("user content mutated: %q", got)
	}
	if got := gjson.GetBytes(out, "messages.1.content").String(); got != "same content" {
		t.Fatalf("assistant content mutated: %q", got)
	}
	if gjson.GetBytes(out, "messages.1.tool_calls.0.function.arguments").Type != gjson.String {
		t.Fatalf("tool_call arguments not stringified: %s", out)
	}
}

func TestTranslateRequestToolsPassthrough(t *testing.T) {
	in := []byte(`{"tools":[{"type":"function","function":{"name":"search","parameters":{"type":"object"}}}]}`)
	out, err := translateRequestTools(in)
	if err != nil {
		t.Fatalf("translate tools failed: %v", err)
	}
	if string(out) != string(in) {
		t.Fatalf("tools should pass through unchanged\nin:  %s\nout: %s", in, out)
	}
}

func TestToolCallIDAssignsMissingAssistantID(t *testing.T) {
	in := []byte(`{"messages":[{"role":"assistant","tool_calls":[{"function":{"name":"x","arguments":{}}}]}]}`)
	out, err := resolveToolCallIDs(in, "messages")
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if got := gjson.GetBytes(out, "messages.0.tool_calls.0.id").String(); got == "" {
		t.Fatalf("missing assistant tool_call id was not assigned: %s", out)
	}
}

func TestToolCallIDAssignsFollowingToolMessage(t *testing.T) {
	in := []byte(`{"messages":[{"role":"assistant","tool_calls":[{"id":"call_a","function":{"name":"x","arguments":{}}}]},{"role":"tool","content":"result"}]}`)
	out, err := resolveToolCallIDs(in, "messages")
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if got := gjson.GetBytes(out, "messages.1.tool_call_id").String(); got != "call_a" {
		t.Fatalf("tool_call_id = %q, want call_a", got)
	}
}

func TestToolCallIDPreservesExistingToolMessageID(t *testing.T) {
	in := []byte(`{"messages":[{"role":"assistant","tool_calls":[{"id":"call_a","function":{"name":"x","arguments":{}}}]},{"role":"tool","tool_call_id":"existing","content":"result"}]}`)
	out, err := resolveToolCallIDs(in, "messages")
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if got := gjson.GetBytes(out, "messages.1.tool_call_id").String(); got != "existing" {
		t.Fatalf("existing tool_call_id should be preserved, got %q", got)
	}
}

func TestToolCallIDSynthesizesOrphanToolMessage(t *testing.T) {
	in := []byte(`{"messages":[{"role":"tool","content":"orphan"}]}`)
	out, err := resolveToolCallIDs(in, "messages")
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if got := gjson.GetBytes(out, "messages.0.tool_call_id").String(); got == "" {
		t.Fatalf("orphan tool message should receive synthesized id: %s", out)
	}
}

func TestToolCallIDQueueResetsOnUserTurn(t *testing.T) {
	in := []byte(`{"messages":[{"role":"assistant","tool_calls":[{"id":"call_a","function":{"name":"x","arguments":{}}}]},{"role":"user","content":"new turn"},{"role":"tool","content":"orphan"}]}`)
	out, err := resolveToolCallIDs(in, "messages")
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if got := gjson.GetBytes(out, "messages.2.tool_call_id").String(); got == "" || got == "call_a" {
		t.Fatalf("queue should reset on user turn; got %q body=%s", got, out)
	}
}

func TestFieldPolicyFormatJSON(t *testing.T) {
	out, err := applyFieldPolicy([]byte(`{"format":"json","model":"x","messages":[]}`))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := gjson.GetBytes(out, "response_format.type").String(); got != "json_object" {
		t.Fatalf("response_format.type = %q", got)
	}
	if gjson.GetBytes(out, "format").Exists() {
		t.Fatalf("format should be removed")
	}
}

func TestFieldPolicyVisionRejected(t *testing.T) {
	if _, err := applyFieldPolicy([]byte(`{"images":["x"]}`)); !errors.Is(err, ErrVisionNotSupported) {
		t.Fatalf("top-level images err = %v", err)
	}
	if _, err := applyFieldPolicy([]byte(`{"messages":[{"role":"user","images":["x"]}]}`)); !errors.Is(err, ErrVisionNotSupported) {
		t.Fatalf("message images err = %v", err)
	}
}

func TestFieldPolicyOptionsLiftedAndDropped(t *testing.T) {
	out, err := applyFieldPolicy([]byte(`{"options":{"temperature":0,"top_p":0.9,"seed":42,"top_k":40,"num_ctx":8192}}`))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !gjson.GetBytes(out, "temperature").Exists() || gjson.GetBytes(out, "temperature").Float() != 0 {
		t.Fatalf("temperature should be lifted as 0: %s", out)
	}
	if got := gjson.GetBytes(out, "top_p").Float(); got != 0.9 {
		t.Fatalf("top_p = %v", got)
	}
	if got := gjson.GetBytes(out, "seed").Int(); got != 42 {
		t.Fatalf("seed = %d", got)
	}
	if gjson.GetBytes(out, "options").Exists() || gjson.GetBytes(out, "top_k").Exists() || gjson.GetBytes(out, "num_ctx").Exists() {
		t.Fatalf("options/top_k/num_ctx leaked: %s", out)
	}
}

func TestFieldPolicyThinkAndThinking(t *testing.T) {
	out, err := applyFieldPolicy([]byte(`{"think":true,"messages":[{"role":"assistant","thinking":"secret","content":"answer"}]}`))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := gjson.GetBytes(out, "reasoning_effort").String(); got != "medium" {
		t.Fatalf("reasoning_effort = %q", got)
	}
	if gjson.GetBytes(out, "think").Exists() || gjson.GetBytes(out, "messages.0.thinking").Exists() {
		t.Fatalf("think/thinking should be removed: %s", out)
	}
}

func TestFieldPolicyKeepAliveAndSchemaFormatDropped(t *testing.T) {
	out, err := applyFieldPolicy([]byte(`{"keep_alive":"5m","format":{"type":"object"}}`))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if gjson.GetBytes(out, "keep_alive").Exists() || gjson.GetBytes(out, "format").Exists() || gjson.GetBytes(out, "response_format").Exists() {
		t.Fatalf("unexpected fields after policy: %s", out)
	}
}

func TestOllamaChatToOpenAIIntegration(t *testing.T) {
	in := []byte(`{
		"model":"gemma4:e2b",
		"messages":[
			{"role":"user","content":"plan dinner"},
			{"role":"assistant","tool_calls":[{"function":{"name":"search","arguments":{"q":"recipes"}}}]},
			{"role":"tool","content":"results..."}
		],
		"stream":true,
		"think":true,
		"options":{"num_ctx":8192,"temperature":0.0},
		"tools":[{"type":"function","function":{"name":"search"}}]
	}`)
	out, err := OllamaChatToOpenAI(in, "kimi-k2-0905-preview")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := gjson.GetBytes(out, "model").String(); got != "kimi-k2-0905-preview" {
		t.Fatalf("model = %q", got)
	}
	if !gjson.GetBytes(out, "stream").Bool() {
		t.Fatalf("stream should remain true")
	}
	if got := gjson.GetBytes(out, "reasoning_effort").String(); got != "medium" {
		t.Fatalf("reasoning_effort = %q", got)
	}
	args := gjson.GetBytes(out, "messages.1.tool_calls.0.function.arguments")
	if args.Type != gjson.String {
		t.Fatalf("tool args should be JSON string: %s", out)
	}
	if id := gjson.GetBytes(out, "messages.2.tool_call_id").String(); id == "" {
		t.Fatalf("tool_call_id should be assigned: %s", out)
	}
	if gjson.GetBytes(out, "options").Exists() || gjson.GetBytes(out, "think").Exists() {
		t.Fatalf("options/think should be removed: %s", out)
	}
}

func TestOllamaChatToOpenAIStreamDefault(t *testing.T) {
	out, err := OllamaChatToOpenAI([]byte(`{"model":"gemma4:e2b","messages":[]}`), "x")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := gjson.GetBytes(out, "stream").Type; got != gjson.True {
		t.Fatalf("stream default should be true, got %v", got)
	}
	if !gjson.GetBytes(out, "stream_options.include_usage").Bool() {
		t.Fatalf("stream_options.include_usage should default true for streaming: %s", out)
	}
}

func TestOllamaChatToOpenAIStreamOptionsPreserveExplicitValue(t *testing.T) {
	out, err := OllamaChatToOpenAI([]byte(`{"model":"m","stream":true,"stream_options":{"include_usage":false},"messages":[]}`), "x")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if gjson.GetBytes(out, "stream_options.include_usage").Bool() {
		t.Fatalf("explicit include_usage:false should be preserved: %s", out)
	}
}

func TestOllamaChatToOpenAINonStreamDoesNotAddStreamOptions(t *testing.T) {
	out, err := OllamaChatToOpenAI([]byte(`{"model":"m","stream":false,"messages":[]}`), "x")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if gjson.GetBytes(out, "stream_options").Exists() {
		t.Fatalf("non-stream request should not get stream_options: %s", out)
	}
}

func TestOpenAIRespToOllamaChatBasic(t *testing.T) {
	in := []byte(`{"id":"x","choices":[{"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":3}}`)
	out, err := OpenAIRespToOllamaChat(in, "gemma4:e2b")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := gjson.GetBytes(out, "model").String(); got != "gemma4:e2b" {
		t.Fatalf("model = %q", got)
	}
	if got := gjson.GetBytes(out, "message.content").String(); got != "hello" {
		t.Fatalf("content = %q", got)
	}
	if !gjson.GetBytes(out, "done").Bool() {
		t.Fatalf("done should be true")
	}
	if got := gjson.GetBytes(out, "prompt_eval_count").Int(); got != 12 {
		t.Fatalf("prompt_eval_count = %d", got)
	}
	if got := gjson.GetBytes(out, "eval_count").Int(); got != 3 {
		t.Fatalf("eval_count = %d", got)
	}
	if !gjson.GetBytes(out, "created_at").Exists() {
		t.Fatalf("created_at should be set")
	}
}

func TestOpenAIRespToOllamaChatToolCalls(t *testing.T) {
	in := []byte(`{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"a","type":"function","function":{"name":"f","arguments":"{\"x\":1}"}}]}}]}`)
	out, err := OpenAIRespToOllamaChat(in, "model")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	args := gjson.GetBytes(out, "message.tool_calls.0.function.arguments")
	if !args.IsObject() || args.Get("x").Int() != 1 {
		t.Fatalf("args should be dict: %s", out)
	}
	if got := gjson.GetBytes(out, "message.content").String(); got != "" {
		t.Fatalf("null content should normalize empty, got %q", got)
	}
}
