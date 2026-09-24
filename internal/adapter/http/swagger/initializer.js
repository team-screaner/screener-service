window.ui = SwaggerUIBundle({
  url: "/openapi.json",
  dom_id: "#swagger-ui",
  deepLinking: true,
  filter: true,
  displayRequestDuration: true,
  docExpansion: "list",
  persistAuthorization: false,
  validatorUrl: null,
  presets: [SwaggerUIBundle.presets.apis],
  layout: "BaseLayout"
});
