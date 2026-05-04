package ollama

import (
	"bytes"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

type streamMode int

const (
	modeUndecided streamMode = iota
	modeStream
	modeBuffer
	modeError
)

type toolCallAcc struct {
	id   string
	name string
	args strings.Builder
}

type OllamaStreamWriter struct {
	gin.ResponseWriter
	model      string
	startedAt  time.Time
	wantStream bool

	mode       streamMode
	statusSet  bool
	statusCode int

	mu        sync.Mutex
	buf       bytes.Buffer
	toolCalls map[int]*toolCallAcc
	finalized bool
	sawDone   bool

	usage struct {
		promptTokens     int64
		completionTokens int64
	}
}

func NewOllamaStreamWriter(w gin.ResponseWriter, model string, startedAt time.Time, wantStream bool) *OllamaStreamWriter {
	return &OllamaStreamWriter{ResponseWriter: w, model: model, startedAt: startedAt, wantStream: wantStream, toolCalls: map[int]*toolCallAcc{}}
}

func (w *OllamaStreamWriter) WriteHeader(code int) {
	w.mu.Lock()
	if w.statusSet {
		w.mu.Unlock()
		return
	}
	w.statusSet = true
	w.statusCode = code
	switch {
	case code >= 400:
		w.mode = modeError
		w.ResponseWriter.Header().Set("Content-Type", "application/json")
	case w.wantStream:
		w.mode = modeStream
		w.ResponseWriter.Header().Set("Content-Type", "application/x-ndjson")
	default:
		w.mode = modeBuffer
		w.ResponseWriter.Header().Set("Content-Type", "application/json")
	}
	w.mu.Unlock()
	w.ResponseWriter.WriteHeader(code)
}

func (w *OllamaStreamWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.statusSet {
		w.statusSet = true
		w.statusCode = http.StatusOK
		if w.wantStream {
			w.mode = modeStream
			w.ResponseWriter.Header().Set("Content-Type", "application/x-ndjson")
		} else {
			w.mode = modeBuffer
			w.ResponseWriter.Header().Set("Content-Type", "application/json")
		}
	}
	if w.mode != modeStream {
		return w.buf.Write(p)
	}
	return w.writeStream(p)
}

func (w *OllamaStreamWriter) writeStream(p []byte) (int, error) {
	w.buf.Write(p)
	for {
		raw := w.buf.Bytes()
		idx, delimiterLen := nextSSEEventBoundary(raw)
		if idx < 0 {
			break
		}
		event := raw[:idx]
		w.buf.Next(idx + delimiterLen)
		w.handleSSEEvent(event)
	}
	return len(p), nil
}

func nextSSEEventBoundary(raw []byte) (int, int) {
	lf := bytes.Index(raw, []byte("\n\n"))
	crlf := bytes.Index(raw, []byte("\r\n\r\n"))
	switch {
	case lf < 0:
		return crlf, 4
	case crlf < 0:
		return lf, 2
	case crlf < lf:
		return crlf, 4
	default:
		return lf, 2
	}
}

func (w *OllamaStreamWriter) handleSSEEvent(event []byte) {
	if w.finalized {
		return
	}
	payload := sseDataPayload(event)
	if len(payload) == 0 {
		return
	}
	payload = bytes.TrimSpace(payload)
	if bytes.Equal(payload, []byte("[DONE]")) {
		w.sawDone = true
		w.emitTerminal(nil)
		return
	}
	if !gjson.ValidBytes(payload) {
		return
	}
	if errMsg := gjson.GetBytes(payload, "error.message"); errMsg.Exists() {
		w.emitTerminal([]byte(errMsg.String()))
		return
	}
	delta := gjson.GetBytes(payload, "choices.0.delta")
	if content := delta.Get("content"); content.Exists() && content.String() != "" {
		w.emitContentLine(content.String())
	}
	w.accumulateToolCalls(delta)
	w.captureUsage(payload)
}

func sseDataPayload(event []byte) []byte {
	lines := bytes.Split(bytes.ReplaceAll(event, []byte("\r\n"), []byte("\n")), []byte("\n"))
	var payload bytes.Buffer
	for _, line := range lines {
		line = bytes.TrimSuffix(line, []byte("\r"))
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(line[len("data:"):])
		if payload.Len() > 0 {
			payload.WriteByte('\n')
		}
		payload.Write(data)
	}
	return payload.Bytes()
}

func (w *OllamaStreamWriter) emitContentLine(content string) {
	line := map[string]any{
		"model":      w.model,
		"created_at": time.Now().UTC().Format(time.RFC3339Nano),
		"message": map[string]any{
			"role":    "assistant",
			"content": content,
		},
		"done": false,
	}
	encoded, _ := json.Marshal(line)
	encoded = append(encoded, '\n')
	_, _ = w.ResponseWriter.Write(encoded)
	if f, ok := w.ResponseWriter.(interface{ Flush() }); ok {
		f.Flush()
	}
}

func (w *OllamaStreamWriter) accumulateToolCalls(delta gjson.Result) {
	toolCalls := delta.Get("tool_calls")
	if !toolCalls.IsArray() {
		return
	}
	for _, toolCall := range toolCalls.Array() {
		idx := int(toolCall.Get("index").Int())
		acc := w.toolCalls[idx]
		if acc == nil {
			acc = &toolCallAcc{}
			w.toolCalls[idx] = acc
		}
		if id := toolCall.Get("id").String(); id != "" {
			acc.id = id
		}
		if name := toolCall.Get("function.name").String(); name != "" {
			acc.name = name
		}
		if args := toolCall.Get("function.arguments"); args.Exists() {
			acc.args.WriteString(args.String())
		}
	}
}

func (w *OllamaStreamWriter) captureUsage(payload []byte) {
	usage := gjson.GetBytes(payload, "usage")
	if !usage.IsObject() {
		return
	}
	if value := usage.Get("prompt_tokens"); value.Exists() {
		w.usage.promptTokens = value.Int()
	}
	if value := usage.Get("completion_tokens"); value.Exists() {
		w.usage.completionTokens = value.Int()
	}
}

func (w *OllamaStreamWriter) ollamaToolCalls() []map[string]any {
	indexes := make([]int, 0, len(w.toolCalls))
	for idx := range w.toolCalls {
		indexes = append(indexes, idx)
	}
	sort.Ints(indexes)
	out := make([]map[string]any, 0, len(indexes))
	for _, idx := range indexes {
		acc := w.toolCalls[idx]
		args := map[string]any{}
		if raw := acc.args.String(); raw != "" {
			var decoded any
			if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
				log.Warnf("ollama: stream tool_call[%d] arguments parse failed, substituting {}", idx)
			} else if decodedMap, ok := decoded.(map[string]any); ok {
				args = decodedMap
			}
		}
		out = append(out, map[string]any{
			"id":   acc.id,
			"type": "function",
			"function": map[string]any{
				"name":      acc.name,
				"arguments": args,
			},
		})
	}
	return out
}

func (w *OllamaStreamWriter) emitTerminal(errMsg []byte) {
	if w.finalized {
		return
	}
	w.finalized = true
	message := map[string]any{
		"role":    "assistant",
		"content": "",
	}
	if len(w.toolCalls) > 0 {
		message["tool_calls"] = w.ollamaToolCalls()
	}
	totalDuration := time.Since(w.startedAt).Nanoseconds()
	out := map[string]any{
		"model":             w.model,
		"created_at":        time.Now().UTC().Format(time.RFC3339Nano),
		"message":           message,
		"done":              true,
		"total_duration":    totalDuration,
		"load_duration":     int64(0),
		"prompt_eval_count": w.usage.promptTokens,
		"eval_count":        w.usage.completionTokens,
		"eval_duration":     totalDuration,
	}
	if errMsg != nil {
		out["error"] = string(errMsg)
	}
	encoded, _ := json.Marshal(out)
	encoded = append(encoded, '\n')
	_, _ = w.ResponseWriter.Write(encoded)
	if f, ok := w.ResponseWriter.(interface{ Flush() }); ok {
		f.Flush()
	}
}

func (w *OllamaStreamWriter) Finalize() {
	w.mu.Lock()
	defer w.mu.Unlock()
	switch w.mode {
	case modeStream:
		if !w.finalized {
			log.Warn("ollama: stream mode finalized without [DONE]; emitting terminal frame")
			w.emitTerminal([]byte("upstream stream interrupted"))
		}
	case modeBuffer:
		translated, err := OpenAIRespToOllamaChat(w.buf.Bytes(), w.model)
		if err != nil {
			log.Warnf("ollama: buffered response translation failed: %v", err)
			translated = []byte(`{"done":true,"error":"upstream response translation failed"}`)
		}
		_, _ = w.ResponseWriter.Write(translated)
	case modeError:
		_, _ = w.ResponseWriter.Write(w.buf.Bytes())
	}
}
