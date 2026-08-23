package api

import (
	_ "embed"
	"net/http"
)

// openapiSpec is generated from the route table and the apitypes package by
// `make openapi`; TestOpenAPIDocumentIsUpToDate fails if it is stale.
//
//go:embed openapi.yaml
var openapiSpec []byte

// handleOpenAPISpec serves the OpenAPI specification as JSON.
func (s *Server) handleOpenAPISpec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(openapiSpec)
}

// handleSwaggerUI serves the Swagger UI HTML page.
func (s *Server) handleSwaggerUI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(swaggerUIHTML))
}

const swaggerUIHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Marionette API Documentation</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5.9.0/swagger-ui.css" />
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5.9.0/index.css" />
  <style>
    body {
      margin: 0;
      padding: 0;
    }
    .swagger-ui .info {
      margin: 20px 0;
    }
  </style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5.9.0/swagger-ui-bundle.js"></script>
  <script src="https://unpkg.com/swagger-ui-dist@5.9.0/swagger-ui-standalone-preset.js"></script>
  <script>
    window.onload = function() {
      SwaggerUIBundle({
        // Relative, so the page works wherever the server is reached from.
        // It used to also list the admin spec at a hardcoded localhost:8081,
        // which was broken in every deployment but a developer's laptop.
        url: "/openapi.yaml",
        dom_id: '#swagger-ui',
        presets: [
          SwaggerUIBundle.presets.apis,
          SwaggerUIStandalonePreset
        ],
        plugins: [
          SwaggerUIBundle.plugins.DownloadUrl
        ],
        layout: "StandaloneLayout",
        deepLinking: true,
        showExtensions: true,
        showCommonExtensions: true
      });
    };
  </script>
</body>
</html>`
