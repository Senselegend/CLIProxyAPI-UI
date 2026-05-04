package ollama

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func GenerateHandler(mapper *ModelMapper, downstream gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
			return
		}
		if !gjson.ValidBytes(body) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "request body is not valid JSON"})
			return
		}

		model := gjson.GetBytes(body, "model").String()
		mappedModel, ok := mapper.Resolve(model)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "ollama model is not mapped"})
			return
		}
		wantStream := true
		if stream := gjson.GetBytes(body, "stream"); stream.Exists() {
			wantStream = stream.Bool()
		}

		translated, err := OllamaGenerateToOpenAI(body, mappedModel)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		writer := NewOllamaGenerateWriter(c.Writer, model, time.Now(), wantStream)
		originalWriter := c.Writer
		originalPath := c.Request.URL.Path
		c.Writer = writer
		c.Request.Body = io.NopCloser(bytes.NewReader(translated))
		c.Request.ContentLength = int64(len(translated))
		c.Request.URL.Path = "/v1/chat/completions"

		downstream(c)
		writer.Finalize()

		c.Writer = originalWriter
		c.Request.URL.Path = originalPath
	}
}

func OllamaGenerateToOpenAI(body []byte, mappedModel string) ([]byte, error) {
	if !gjson.ValidBytes(body) {
		return nil, fmt.Errorf("ollama: request body is not valid JSON")
	}
	if gjson.GetBytes(body, "images").Exists() {
		return nil, ErrVisionNotSupported
	}

	out := []byte(`{"model":"","messages":[]}`)
	var err error
	out, err = sjson.SetBytes(out, "model", mappedModel)
	if err != nil {
		return nil, fmt.Errorf("ollama: set model: %w", err)
	}

	wantStream := true
	if stream := gjson.GetBytes(body, "stream"); stream.Exists() {
		wantStream = stream.Bool()
	}
	out, err = sjson.SetBytes(out, "stream", wantStream)
	if err != nil {
		return nil, fmt.Errorf("ollama: set stream: %w", err)
	}
	out, err = applyStreamOptions(out)
	if err != nil {
		return nil, err
	}

	messages := make([]map[string]any, 0, 2)
	if system := gjson.GetBytes(body, "system"); system.Exists() && system.String() != "" {
		messages = append(messages, map[string]any{"role": "system", "content": system.String()})
	}
	if prompt := gjson.GetBytes(body, "prompt"); prompt.Exists() && prompt.String() != "" {
		messages = append(messages, map[string]any{"role": "user", "content": prompt.String()})
	}
	out, err = sjson.SetBytes(out, "messages", messages)
	if err != nil {
		return nil, fmt.Errorf("ollama: set messages: %w", err)
	}

	if format := gjson.GetBytes(body, "format"); format.Exists() {
		if format.Type == gjson.String && format.String() == "json" {
			out, err = sjson.SetBytes(out, "response_format", map[string]string{"type": "json_object"})
			if err != nil {
				return nil, err
			}
		}
	}
	if think := gjson.GetBytes(body, "think"); think.Exists() && think.Bool() {
		out, err = sjson.SetBytes(out, "reasoning_effort", "medium")
		if err != nil {
			return nil, err
		}
	}
	if options := gjson.GetBytes(body, "options"); options.IsObject() {
		if v := options.Get("temperature"); v.Exists() {
			out, err = sjson.SetBytes(out, "temperature", v.Float())
			if err != nil {
				return nil, err
			}
		}
		if v := options.Get("top_p"); v.Exists() {
			out, err = sjson.SetBytes(out, "top_p", v.Float())
			if err != nil {
				return nil, err
			}
		}
		if v := options.Get("seed"); v.Exists() {
			out, err = sjson.SetBytes(out, "seed", v.Int())
			if err != nil {
				return nil, err
			}
		}
	}

	return out, nil
}
