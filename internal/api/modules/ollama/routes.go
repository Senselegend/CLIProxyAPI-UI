package ollama

import "github.com/gin-gonic/gin"

func (m *OllamaModule) registerRoutes(engine *gin.Engine, downstream gin.HandlerFunc) {
	api := engine.Group("/api")
	localOnly := m.localhostOnlyMiddleware()
	noCors := noCORSMiddleware()
	for _, path := range []string{"/chat", "/embeddings", "/show", "/tags"} {
		api.OPTIONS(path, noCors)
	}
	api.POST("/chat", noCors, localOnly, m.enabledMiddleware(), func(c *gin.Context) {
		ChatHandler(m.getModelMapper(), downstream)(c)
	})
	api.POST("/embeddings", noCors, localOnly, m.enabledMiddleware(), func(c *gin.Context) {
		EmbeddingsHandler(m.getEmbedProxy())(c)
	})
	api.POST("/show", noCors, localOnly, m.enabledMiddleware(), func(c *gin.Context) {
		ShowHandler(m.getModelMapper())(c)
	})
	api.GET("/version", VersionHandler())
	api.GET("/tags", noCors, localOnly, func(c *gin.Context) {
		TagsHandler(m.getModelMapper())(c)
	})
}
