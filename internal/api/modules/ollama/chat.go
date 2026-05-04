package ollama

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

func ChatHandler(mapper *ModelMapper, downstream gin.HandlerFunc) gin.HandlerFunc {
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

		translated, err := OllamaChatToOpenAI(body, mappedModel)
		if err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, ErrVisionNotSupported) {
				status = http.StatusBadRequest
			}
			c.JSON(status, gin.H{"error": err.Error()})
			return
		}

		writer := NewOllamaStreamWriter(c.Writer, model, time.Now(), wantStream)
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
