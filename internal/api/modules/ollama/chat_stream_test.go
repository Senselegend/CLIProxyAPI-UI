package ollama

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type flushRecorder struct {
	*httptest.ResponseRecorder
	flushed int
}

func (r *flushRecorder) Flush() {
	r.flushed++
}

func newFakeGinWriter() (*flushRecorder, gin.ResponseWriter) {
	rec := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	c, _ := gin.CreateTestContext(rec)
	return rec, c.Writer
}

func TestStreamSSELineBuffering(t *testing.T) {
	rec, gw := newFakeGinWriter()
	w := NewOllamaStreamWriter(gw, "gemma4:e2b", time.Now(), true)
	w.WriteHeader(200)
	w.Write([]byte(`data: {"choices":[{"delta":{"content":"He`))
	if rec.Body.Len() > 0 {
		t.Fatalf("emitted before boundary: %s", rec.Body.String())
	}
	w.Write([]byte(`llo"}}]}` + "\n\n"))
	w.Write([]byte("data: [DONE]\n\n"))
	w.Finalize()

	body := rec.Body.String()
	if !strings.Contains(body, `"content":"Hello"`) {
		t.Fatalf("expected Hello in output:\n%s", body)
	}
	if !strings.Contains(body, `"done":true`) {
		t.Fatalf("expected terminal done:true:\n%s", body)
	}
}

func TestStreamCRLFAndMetadataLines(t *testing.T) {
	rec, gw := newFakeGinWriter()
	w := NewOllamaStreamWriter(gw, "m", time.Now(), true)
	w.WriteHeader(200)
	w.Write([]byte("event: completion\r\ndata: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\r\n\r\n"))
	w.Write([]byte("data: [DONE]\r\n\r\n"))
	w.Finalize()

	body := rec.Body.String()
	if !strings.Contains(body, `"content":"hi"`) || !strings.Contains(body, `"done":true`) {
		t.Fatalf("CRLF SSE not handled:\n%s", body)
	}
}

func TestStreamBasicNDJSON(t *testing.T) {
	rec, gw := newFakeGinWriter()
	w := NewOllamaStreamWriter(gw, "m", time.Now(), true)
	w.WriteHeader(200)
	w.Write([]byte(`data: {"choices":[{"delta":{"content":"He"}}]}` + "\n\n"))
	w.Write([]byte(`data: {"choices":[{"delta":{"content":"llo"}}]}` + "\n\n"))
	w.Write([]byte("data: [DONE]\n\n"))
	w.Finalize()

	lines := bytes.Split(bytes.TrimRight(rec.Body.Bytes(), "\n"), []byte("\n"))
	if len(lines) < 3 {
		t.Fatalf("want >=3 NDJSON lines, got %d:\n%s", len(lines), rec.Body.String())
	}
	if !bytes.Contains(lines[0], []byte(`"content":"He"`)) {
		t.Fatalf("line 0: %s", lines[0])
	}
	if !bytes.Contains(lines[1], []byte(`"content":"llo"`)) {
		t.Fatalf("line 1: %s", lines[1])
	}
	if !bytes.Contains(lines[len(lines)-1], []byte(`"done":true`)) {
		t.Fatalf("last line not done:true: %s", lines[len(lines)-1])
	}
	if rec.flushed < 3 {
		t.Fatalf("expected at least 3 Flush calls, got %d", rec.flushed)
	}
}

func TestStreamToolCallAccumulator(t *testing.T) {
	rec, gw := newFakeGinWriter()
	w := NewOllamaStreamWriter(gw, "m", time.Now(), true)
	w.WriteHeader(200)
	w.Write([]byte(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"a","function":{"name":"get_weather"}}]}}]}` + "\n\n"))
	w.Write([]byte(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"loc"}}]}}]}` + "\n\n"))
	w.Write([]byte(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\":\"P\"}"}}]}}]}` + "\n\n"))
	w.Write([]byte("data: [DONE]\n\n"))
	w.Finalize()

	body := rec.Body.String()
	if !strings.Contains(body, `"name":"get_weather"`) || !strings.Contains(body, `"loc":"P"`) || !strings.Contains(body, `"id":"a"`) {
		t.Fatalf("tool call not accumulated correctly:\n%s", body)
	}
}

func TestStreamParallelToolCalls(t *testing.T) {
	rec, gw := newFakeGinWriter()
	w := NewOllamaStreamWriter(gw, "m", time.Now(), true)
	w.WriteHeader(200)
	w.Write([]byte(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"a","function":{"name":"f","arguments":"{}"}},{"index":1,"id":"b","function":{"name":"g","arguments":"{}"}}]}}]}` + "\n\n"))
	w.Write([]byte("data: [DONE]\n\n"))
	w.Finalize()

	body := rec.Body.String()
	if !strings.Contains(body, `"name":"f"`) || !strings.Contains(body, `"name":"g"`) {
		t.Fatalf("parallel tool calls missing:\n%s", body)
	}
}

func TestStreamToolCallGarbageArgs(t *testing.T) {
	rec, gw := newFakeGinWriter()
	w := NewOllamaStreamWriter(gw, "m", time.Now(), true)
	w.WriteHeader(200)
	w.Write([]byte(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"a","function":{"name":"f","arguments":"not json"}}]}}]}` + "\n\n"))
	w.Write([]byte("data: [DONE]\n\n"))
	w.Finalize()
	if !strings.Contains(rec.Body.String(), `"arguments":{}`) {
		t.Fatalf("garbage args should become {}:\n%s", rec.Body.String())
	}
}

func TestStreamInterleavedContentAndTools(t *testing.T) {
	rec, gw := newFakeGinWriter()
	w := NewOllamaStreamWriter(gw, "m", time.Now(), true)
	w.WriteHeader(200)
	w.Write([]byte(`data: {"choices":[{"delta":{"content":"thinking..."}}]}` + "\n\n"))
	w.Write([]byte(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"a","function":{"name":"f","arguments":"{}"}}]}}]}` + "\n\n"))
	w.Write([]byte("data: [DONE]\n\n"))
	w.Finalize()

	lines := bytes.Split(bytes.TrimRight(rec.Body.Bytes(), "\n"), []byte("\n"))
	if bytes.Contains(lines[0], []byte("tool_calls")) {
		t.Fatalf("tool_calls should not appear in first content line: %s", lines[0])
	}
	last := lines[len(lines)-1]
	if !bytes.Contains(last, []byte(`"done":true`)) || !bytes.Contains(last, []byte(`"name":"f"`)) {
		t.Fatalf("final line should contain tool call: %s", last)
	}
}

func TestStreamUsageStats(t *testing.T) {
	rec, gw := newFakeGinWriter()
	w := NewOllamaStreamWriter(gw, "m", time.Now(), true)
	w.WriteHeader(200)
	w.Write([]byte(`data: {"choices":[{"delta":{"content":"hi"}}],"usage":{"prompt_tokens":7,"completion_tokens":3}}` + "\n\n"))
	w.Write([]byte("data: [DONE]\n\n"))
	w.Finalize()

	body := rec.Body.String()
	for _, want := range []string{`"prompt_eval_count":7`, `"eval_count":3`, `"total_duration":`, `"load_duration":0`, `"eval_duration":`} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s in output:\n%s", want, body)
		}
	}
}

func TestBufferModeTranslatesOpenAIResponse(t *testing.T) {
	rec, gw := newFakeGinWriter()
	w := NewOllamaStreamWriter(gw, "ollama-model", time.Now(), false)
	w.WriteHeader(200)
	w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hello"}}],"usage":{"prompt_tokens":2,"completion_tokens":1}}`))
	w.Finalize()

	body := rec.Body.String()
	for _, want := range []string{`"model":"ollama-model"`, `"content":"hello"`, `"done":true`, `"prompt_eval_count":2`, `"eval_count":1`} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s in output:\n%s", want, body)
		}
	}
}

func TestErrorModePreservesUpstreamBody(t *testing.T) {
	rec, gw := newFakeGinWriter()
	w := NewOllamaStreamWriter(gw, "m", time.Now(), true)
	w.WriteHeader(400)
	w.Write([]byte(`{"error":{"message":"bad request"}}`))
	w.Finalize()

	if got := rec.Code; got != 400 {
		t.Fatalf("status = %d, want 400", got)
	}
	if body := rec.Body.String(); !strings.Contains(body, `"bad request"`) {
		t.Fatalf("upstream error body not preserved:\n%s", body)
	}
}
