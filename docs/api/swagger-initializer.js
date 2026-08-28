window.onload = function() {
  //<editor-fold desc="Changeable Configuration Block">

  var urls = [
    { name: "RMNG API", url: "./Api_Swagger.yaml" },
    { name: "User API", url: "./User_Api_Swagger.yaml" },
    { name: "MCP API", url: "./MCP_Api_Swagger.yaml" },
  ];

  // Allow deep-linking to a specific spec tab via ?urls.primaryName=<name>
  // without enabling Swagger UI's queryConfigEnabled (which also honours
  // ?configUrl/?url and is a known XSS vector).
  var requestedName = new URLSearchParams(window.location.search).get("urls.primaryName");
  var primaryName = urls.find(function (u) { return u.name === requestedName; });

  var config = {
    urls: urls,
    dom_id: "#swagger-ui",
    deepLinking: true,
    presets: [SwaggerUIBundle.presets.apis, SwaggerUIStandalonePreset],
    plugins: [SwaggerUIBundle.plugins.DownloadUrl],
    layout: "StandaloneLayout",
  };
  if (primaryName) {
    config["urls.primaryName"] = primaryName.name;
  }

  window.ui = SwaggerUIBundle(config);

  //</editor-fold>
};