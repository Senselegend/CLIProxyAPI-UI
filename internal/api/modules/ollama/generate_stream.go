package ollama

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type OllamaGenerateWriter struct {
	gin.ResponseWriter
	model      string
	startedAt  time.Time
	wantStream bool

	mode       streamMode
	statusSet  bool
	statusCode int
	buf        bytes.Buffer
	finalized  bool
	sawDone    bool

	usage struct {
		promptTokens     int64
		completionTokens int64
	}
}

func NewOllamaGenerateWriter(w gin.ResponseWriter, model string, startedAt time.Time, wantStream bool) *OllamaGenerateWriter {
	return &OllamaGenerateWriter{ResponseWriter: w, model: model, startedAt: startedAt, wantStream: wantStream}
}

func (w *OllamaGenerateWriter) WriteHeader(code int) {
	if w.statusSet {
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
	w.ResponseWriter.WriteHeader(code)
}

func (w *OllamaGenerateWriter) Write(p []byte) (int, error) {
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

func (w *OllamaGenerateWriter) handleSSEEvent(event []byte) {
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
	if content := gjson.GetBytes(payload, "choices.0.delta.content"); content.Exists() && content.String() != "" {
		w.emitContentLine(content.String())
	}
	w.captureUsage(payload)
}

func (w *OllamaGenerateWriter) emitContentLine(content string) {
	line := map[string]any{
		"model":      w.model,
		"created_at": time.Now().UTC().Format(time.RFC3339Nano),
		"response":   content,
		"done":       false,
	}
	encoded, _ := json.Marshal(line)
	encoded = append(encoded, '\n')
	_, _ = w.ResponseWriter.Write(encoded)
	if f, ok := w.ResponseWriter.(interface{ Flush() }); ok {
		f.Flush()
	}
}

func (w *OllamaGenerateWriter) captureUsage(payload []byte) {
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

func (w *OllamaGenerateWriter) emitTerminal(errMsg []byte) {
	if w.finalized {
		return
	}
	w.finalized = true
	out := []byte(`{"done":true}`)
	out, _ = sjson.SetBytes(out, "model", w.model)
	out, _ = sjson.SetBytes(out, "created_at", time.Now().UTC().Format(time.RFC3339Nano))
	out, _ = sjson.SetBytes(out, "total_duration", int64(0))
	out, _ = sjson.SetBytes(out, "load_duration", int64(0))
	out, _ = sjson.SetBytes(out, "eval_duration", int64(0))
	if w.usage.promptTokens > 0 {
		out, _ = sjson.SetBytes(out, "prompt_eval_count", w.usage.promptTokens)
	}
	if w.usage.completionTokens > 0 {
		out, _ = sjson.SetBytes(out, "eval_count", w.usage.completionTokens)
	}
	if len(errMsg) > 0 {
		out, _ = sjson.SetBytes(out, "error", string(errMsg))
	}
	out = append(out, '\n')
	_, _ = w.ResponseWriter.Write(out)
	if f, ok := w.ResponseWriter.(interface{ Flush() }); ok {
		f.Flush()
	}
}

func (w *OllamaGenerateWriter) Finalize() {
	if w.finalized {
		return
	}
	if w.mode == modeBuffer {
		translated, err := OpenAIRespToOllamaGenerate(w.buf.Bytes(), w.model)
		if err != nil {
			w.ResponseWriter.WriteHeader(http.StatusBadGateway)
			_, _ = w.ResponseWriter.Write([]byte(`{"error":"` + err.Error() + `"}`))
			w.finalized = true
			return
		}
		_, _ = w.ResponseWriter.Write(translated)
		w.finalized = true
		return
	}
	if w.mode == modeError {
		_, _ = w.ResponseWriter.Write(w.buf.Bytes())
		w.finalized = true
		return
	}
	if !w.sawDone {
		w.emitTerminal(nil)
	}
}

func OpenAIRespToOllamaGenerate(body []byte, model string) ([]byte, error) {
	if !gjson.ValidBytes(body) {
		return nil, ErrInvalidGenerateResponse
	}
	content := gjson.GetBytes(body, "choices.0.message.content")
	if !content.Exists() {
		if errMsg := gjson.GetBytes(body, "error.message"); errMsg.Exists() {
			out, _ := sjson.SetBytes([]byte(`{"done":true}`), "error", errMsg.String())
			return out, nil
		}
		return nil, ErrInvalidGenerateResponse
	}

	out := []byte(`{}`)
	out, _ = sjson.SetBytes(out, "model", model)
	out, _ = sjson.SetBytes(out, "created_at", time.Now().UTC().Format(time.RFC3339Nano))
	out, _ = sjson.SetBytes(out, "response", content.String())
	out, _ = sjson.SetBytes(out, "done", true)
	if finish := gjson.GetBytes(body, "choices.0.finish_reason"); finish.Exists() {
		out, _ = sjson.SetBytes(out, "done_reason", finish.String())
	}
	if usage := gjson.GetBytes(body, "usage"); usage.IsObject() {
		if value := usage.Get("prompt_tokens"); value.Exists() {
			out, _ = sjson.SetBytes(out, "prompt_eval_count", value.Int())
		}
		if value := usage.Get("completion_tokens"); value.Exists() {
			out, _ = sjson.SetBytes(out, "eval_count", value.Int())
		}
	}
	out, _ = sjson.SetBytes(out, "total_duration", int64(0))
	out, _ = sjson.SetBytes(out, "load_duration", int64(0))
	out, _ = sjson.SetBytes(out, "eval_duration", int64(0))
	return out, nil
}

var ErrInvalidGenerateResponse = http.ErrBodyNotAllowed
