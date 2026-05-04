package ollama

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

var ErrVisionNotSupported = errors.New("ollama: vision requests not supported")

func OllamaChatToOpenAI(body []byte, mappedModel string) ([]byte, error) {
	if !gjson.ValidBytes(body) {
		return nil, fmt.Errorf("ollama: request body is not valid JSON")
	}
	out := body
	if !gjson.GetBytes(out, "stream").Exists() {
		next, err := sjson.SetBytes(out, "stream", true)
		if err != nil {
			return nil, err
		}
		out = next
	}
	var err error
	out, err = applyStreamOptions(out)
	if err != nil {
		return nil, err
	}
	out, err = applyFieldPolicy(out)
	if err != nil {
		return nil, err
	}
	out, err = translateRequestTools(out)
	if err != nil {
		return nil, err
	}
	out, err = resolveToolCallIDs(out, "messages")
	if err != nil {
		return nil, err
	}
	out, err = rewriteToolCallsArgsToString(out, "messages")
	if err != nil {
		return nil, err
	}
	next, err := sjson.SetBytes(out, "model", mappedModel)
	if err != nil {
		return nil, fmt.Errorf("ollama: set model: %w", err)
	}
	return next, nil
}

func OpenAIRespToOllamaChat(body []byte, model string) ([]byte, error) {
	if !gjson.ValidBytes(body) {
		return nil, fmt.Errorf("ollama: response body is not valid JSON")
	}
	message := gjson.GetBytes(body, "choices.0.message")
	if !message.IsObject() {
		if errMsg := gjson.GetBytes(body, "error.message"); errMsg.Exists() {
			out, _ := sjson.SetBytes([]byte(`{"done":true}`), "error", errMsg.String())
			return out, nil
		}
		return nil, fmt.Errorf("ollama: upstream response missing choices[0].message")
	}

	out := []byte(`{}`)
	out, _ = sjson.SetBytes(out, "model", model)
	out, _ = sjson.SetBytes(out, "created_at", time.Now().UTC().Format(time.RFC3339Nano))
	out, _ = sjson.SetBytes(out, "done", true)
	if finish := gjson.GetBytes(body, "choices.0.finish_reason"); finish.Exists() {
		out, _ = sjson.SetBytes(out, "done_reason", finish.String())
	}
	out, _ = sjson.SetRawBytes(out, "message", []byte(message.Raw))
	if message.Get("tool_calls").IsArray() {
		translated, err := rewriteToolCallsArgsToDict(out, "message.tool_calls")
		if err != nil {
			return nil, err
		}
		out = translated
	}
	if !gjson.GetBytes(out, "message.role").Exists() {
		out, _ = sjson.SetBytes(out, "message.role", "assistant")
	}
	if content := gjson.GetBytes(out, "message.content"); !content.Exists() || content.Type == gjson.Null {
		out, _ = sjson.SetBytes(out, "message.content", "")
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

func applyStreamOptions(body []byte) ([]byte, error) {
	stream := gjson.GetBytes(body, "stream")
	if !stream.Exists() || !stream.Bool() {
		return body, nil
	}
	if gjson.GetBytes(body, "stream_options.include_usage").Exists() {
		return body, nil
	}
	out, err := sjson.SetBytes(body, "stream_options.include_usage", true)
	if err != nil {
		return nil, fmt.Errorf("ollama: set stream_options.include_usage: %w", err)
	}
	return out, nil
}

func applyFieldPolicy(body []byte) ([]byte, error) {
	out := body
	if gjson.GetBytes(out, "images").Exists() {
		return nil, ErrVisionNotSupported
	}
	if messages := gjson.GetBytes(out, "messages"); messages.IsArray() {
		for _, message := range messages.Array() {
			if message.Get("images").Exists() {
				return nil, ErrVisionNotSupported
			}
		}
	}

	if format := gjson.GetBytes(out, "format"); format.Exists() {
		if format.Type == gjson.String && format.String() == "json" {
			next, err := sjson.SetBytes(out, "response_format", map[string]string{"type": "json_object"})
			if err != nil {
				return nil, err
			}
			out = next
		} else {
			log.Warnf("ollama: format=%s not mapped, dropping", format.Raw)
		}
		out, _ = sjson.DeleteBytes(out, "format")
	}

	if gjson.GetBytes(out, "keep_alive").Exists() {
		out, _ = sjson.DeleteBytes(out, "keep_alive")
	}

	if think := gjson.GetBytes(out, "think"); think.Exists() {
		if think.Bool() {
			next, err := sjson.SetBytes(out, "reasoning_effort", "medium")
			if err != nil {
				return nil, err
			}
			out = next
		}
		out, _ = sjson.DeleteBytes(out, "think")
	}

	if options := gjson.GetBytes(out, "options"); options.IsObject() {
		if v := options.Get("temperature"); v.Exists() {
			next, err := sjson.SetBytes(out, "temperature", v.Float())
			if err != nil {
				return nil, err
			}
			out = next
		}
		if v := options.Get("top_p"); v.Exists() {
			next, err := sjson.SetBytes(out, "top_p", v.Float())
			if err != nil {
				return nil, err
			}
			out = next
		}
		if v := options.Get("seed"); v.Exists() {
			next, err := sjson.SetBytes(out, "seed", v.Int())
			if err != nil {
				return nil, err
			}
			out = next
		}
		if options.Get("top_k").Exists() {
			log.Warnf("ollama: options.top_k=%s dropped (no OpenAI equivalent)", options.Get("top_k").Raw)
		}
		out, _ = sjson.DeleteBytes(out, "options")
	}

	if messages := gjson.GetBytes(out, "messages"); messages.IsArray() {
		for mi, message := range messages.Array() {
			if message.Get("thinking").Exists() {
				path := fmt.Sprintf("messages.%d.thinking", mi)
				out, _ = sjson.DeleteBytes(out, path)
			}
		}
	}
	return out, nil
}

func rewriteToolCallsArgsToString(body []byte, messagesPath string) ([]byte, error) {
	messages := gjson.GetBytes(body, messagesPath)
	if !messages.IsArray() {
		return body, nil
	}
	out := body
	for mi, msg := range messages.Array() {
		toolCalls := msg.Get("tool_calls")
		if !toolCalls.IsArray() {
			continue
		}
		for ti, toolCall := range toolCalls.Array() {
			args := toolCall.Get("function.arguments")
			if !args.Exists() {
				continue
			}
			path := fmt.Sprintf("%s.%d.tool_calls.%d.function.arguments", messagesPath, mi, ti)
			var encoded string
			switch {
			case args.IsObject() || args.IsArray():
				encoded = args.Raw
			case args.Type == gjson.String:
				encoded = args.String()
			default:
				encoded = args.Raw
			}
			next, err := sjson.SetBytes(out, path, encoded)
			if err != nil {
				return nil, fmt.Errorf("ollama: rewrite tool_call arguments to string: %w", err)
			}
			out = next
		}
	}
	return out, nil
}

func translateRequestTools(body []byte) ([]byte, error) {
	return body, nil
}

func generateCallID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "call_" + hex.EncodeToString(b[:])
}

func resolveToolCallIDs(body []byte, messagesPath string) ([]byte, error) {
	messages := gjson.GetBytes(body, messagesPath)
	if !messages.IsArray() {
		return body, nil
	}
	out := body
	for mi, msg := range messages.Array() {
		if msg.Get("role").String() != "assistant" {
			continue
		}
		toolCalls := msg.Get("tool_calls")
		if !toolCalls.IsArray() {
			continue
		}
		for ti, toolCall := range toolCalls.Array() {
			if toolCall.Get("id").String() != "" {
				continue
			}
			path := fmt.Sprintf("%s.%d.tool_calls.%d.id", messagesPath, mi, ti)
			next, err := sjson.SetBytes(out, path, generateCallID())
			if err != nil {
				return nil, fmt.Errorf("ollama: assign tool_call id: %w", err)
			}
			out = next
		}
	}

	messages = gjson.GetBytes(out, messagesPath)
	var queue []string
	for mi, msg := range messages.Array() {
		switch msg.Get("role").String() {
		case "user", "system":
			queue = queue[:0]
		case "assistant":
			toolCalls := msg.Get("tool_calls")
			if toolCalls.IsArray() {
				queue = queue[:0]
				for _, toolCall := range toolCalls.Array() {
					if id := toolCall.Get("id").String(); id != "" {
						queue = append(queue, id)
					}
				}
			}
		case "tool":
			if msg.Get("tool_call_id").String() != "" {
				continue
			}
			var id string
			if len(queue) > 0 {
				id = queue[0]
				queue = queue[1:]
			} else {
				id = generateCallID()
				log.Warnf("ollama: tool message at index %d has no nearest-prior assistant tool_call; synthesized %s", mi, id)
			}
			path := fmt.Sprintf("%s.%d.tool_call_id", messagesPath, mi)
			next, err := sjson.SetBytes(out, path, id)
			if err != nil {
				return nil, fmt.Errorf("ollama: assign tool_call_id: %w", err)
			}
			out = next
		}
	}
	return out, nil
}

func rewriteToolCallsArgsToDict(body []byte, toolCallsPath string) ([]byte, error) {
	toolCalls := gjson.GetBytes(body, toolCallsPath)
	if !toolCalls.IsArray() {
		return body, nil
	}
	out := body
	for ti, toolCall := range toolCalls.Array() {
		args := toolCall.Get("function.arguments")
		if !args.Exists() {
			continue
		}
		path := fmt.Sprintf("%s.%d.function.arguments", toolCallsPath, ti)
		raw := "{}"
		if args.Type == gjson.String && args.String() != "" {
			var decoded any
			if err := json.Unmarshal([]byte(args.String()), &decoded); err != nil {
				log.Warnf("ollama: tool_call[%d] arguments parse failed, substituting {}", ti)
			} else if marshaled, err := json.Marshal(decoded); err == nil {
				raw = string(marshaled)
			}
		} else if args.IsObject() {
			raw = args.Raw
		}
		next, err := sjson.SetRawBytes(out, path, []byte(raw))
		if err != nil {
			return nil, fmt.Errorf("ollama: rewrite tool_call arguments to dict: %w", err)
		}
		out = next
	}
	return out, nil
}
