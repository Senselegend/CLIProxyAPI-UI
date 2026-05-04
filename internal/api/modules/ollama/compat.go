package ollama

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

func VersionHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"version": "0.4.0"})
	}
}

func TagsHandler(mapper *ModelMapper) gin.HandlerFunc {
	return func(c *gin.Context) {
		models := make([]gin.H, 0)
		for _, mapping := range mapper.Mappings() {
			models = append(models, gin.H{
				"name":        mapping.From,
				"model":       mapping.From,
				"modified_at": "1970-01-01T00:00:00Z",
				"size":        0,
				"digest":      "",
				"details": gin.H{
					"parent_model":       "",
					"format":             "gguf",
					"family":             "unknown",
					"families":           []string{"unknown"},
					"parameter_size":     "unknown",
					"quantization_level": "unknown",
				},
			})
		}
		c.JSON(http.StatusOK, gin.H{"models": models})
	}
}

func ShowHandler(mapper *ModelMapper) gin.HandlerFunc {
	return func(c *gin.Context) {
		body, err := c.GetRawData()
		if err != nil || !gjson.ValidBytes(body) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "request body is not valid JSON"})
			return
		}
		name := gjson.GetBytes(body, "name").String()
		if _, ok := mapper.Resolve(name); !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "ollama model is not mapped"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"modelfile":  "",
			"parameters": "",
			"template":   "",
			"details": gin.H{
				"parent_model":       "",
				"format":             "gguf",
				"family":             "unknown",
				"families":           []string{"unknown"},
				"parameter_size":     "unknown",
				"quantization_level": "unknown",
			},
			"model_info": gin.H{},
		})
	}
}
