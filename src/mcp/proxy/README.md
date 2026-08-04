# MCP + OAuth Proxy Stack

A self-contained, reusable package that adds [Model Context Protocol (MCP)](https://modelcontextprotocol.io/) support with OAuth 2.1 authentication to any AWS cloud application that authenticates end users through the ESP User OIDC issuer.

## What's Included

| Component | Description |
|---|---|
| **Go module** (`mcp-server`) | MCP protocol engine, ESP User OIDC JWT authentication, JSON-RPC dispatch |
| **OAuth Proxy Lambda** (`oauth_proxy/`) | OAuth 2.1 Authorization Server with PKCE and CIMD validation that brokers the authorization-code flow to the ESP User OIDC issuer |
| **CDK Construct** (`cdk/`) | Python CDK construct that deploys the full stack (API Gateway, Lambdas, routes, IAM roles) |

## Prerequisites

- Go 1.23+
- AWS CDK v2 (Python)
- A reachable ESP User OIDC issuer that:
  - Publishes its JWKS over HTTPS at `<issuer>/.well-known/jwks.json` and a discovery document at `<issuer>/.well-known/openid-configuration`
  - Mints RS256 access tokens whose `sub` claim is the opaque user_id
  - Has a seeded OIDC registry client (public PKCE client) whose id the proxy presents

## Go Module Integration

### 1. Add the module to your Go workspace

```
# go.work
use (
    .
    ./mcp/proxy
)
```

### 2. Import and create an MCP server

```go
package main

import (
    "context"
    "encoding/json"
    "os"

    mcpserver "mcp-server"
    "github.com/aws/aws-lambda-go/events"
    "github.com/aws/aws-lambda-go/lambda"
)

// Implement mcpserver.UserContext for your project
type myUserContext struct {
    userID string
    ctx    context.Context
}

func (u *myUserContext) GetUserID() string        { return u.userID }
func (u *myUserContext) GoContext() context.Context { return u.ctx }

// UserResolver looks up users by the OIDC `sub` (opaque user_id)
func resolveUser(ctx context.Context, userID string) (mcpserver.UserContext, error) {
    return &myUserContext{userID: userID, ctx: ctx}, nil
}

// Define tool handlers
func handleMyTool(user mcpserver.UserContext, id json.RawMessage, args json.RawMessage) (events.APIGatewayV2HTTPResponse, error) {
    result := mcpserver.ToolCallResult{
        Content: []mcpserver.ToolContent{
            {Type: "text", Text: `{"message": "hello"}`},
        },
    }
    return mcpserver.JSONRPCSuccessResponse(id, result), nil
}

func main() {
    // Verifies ESP User OIDC RS256 tokens (iss == USER_ISSUER) against the JWKS read from
    // the USER_JWKS_PARA_NAME SSM parameter, requiring aud == MCP_CLIENT_ID.
    auth := mcpserver.NewOIDCAuthenticator(
        os.Getenv("USER_ISSUER"),
        os.Getenv("MCP_CLIENT_ID"),
        os.Getenv("USER_JWKS_PARA_NAME"),
        resolveUser,
    )

    server := mcpserver.NewServer("my-mcp-server", "1.0.0", auth)
    server.RegisterTool(
        mcpserver.Tool{
            Name:        "my_tool",
            Description: "Does something useful",
            InputSchema: mcpserver.InputSchema{
                Type:       "object",
                Properties: map[string]interface{}{},
            },
        },
        handleMyTool,
    )

    lambda.Start(server.HandleRequest)
}
```

### Key Types

| Type | Description |
|---|---|
| `UserContext` | Interface your project implements to carry authenticated user info (`GetUserID()`, `GoContext()`) |
| `Authenticator` | `func(ctx, request) (UserContext, error)` — validates requests and returns a `UserContext` |
| `UserResolver` | `func(ctx, userID) (UserContext, error)` — resolves the OIDC `sub` (opaque user_id) to your project's `UserContext` |
| `Server` | MCP protocol server. Create with `NewServer()`, register tools with `RegisterTool()`, pass `HandleRequest` to `lambda.Start()` |
| `ToolHandler` | `func(user UserContext, id json.RawMessage, args json.RawMessage) (APIGatewayV2HTTPResponse, error)` |
| `Tool` | Tool definition with `Name`, `Description`, and `InputSchema` |
| `ToolCallResult` | Return type for tool handlers containing `[]ToolContent` |

### Helper Functions

- `JSONRPCSuccessResponse(id, result)` — builds a JSON-RPC 2.0 success response
- `JSONRPCErrorResponse(id, code, message, data)` — builds a JSON-RPC 2.0 error response
- `NewOIDCAuthenticator(issuer, clientID, jwksParamName, resolver)` — creates an `Authenticator` that validates ESP User OIDC RS256 JWTs (iss == `issuer`, aud == `clientID`) against the JWKS in the `jwksParamName` SSM parameter

## CDK Construct Integration

### 1. Import the construct

```python
from src.mcp.handlers.core import McpOAuthConstruct, McpOAuthConfig
```

### 2. Configure and deploy

```python
import os
from aws_cdk import Duration, aws_iam as iam

mcp_config = McpOAuthConfig(
    # Required: ESP User OIDC issuer the proxy brokers auth to, and the registry
    # client id it presents (public PKCE client).
    espuser_issuer="https://<issuer-host>",
    mcp_oidc_client_id="mcp-oauth-client",

    # Required: paths to pre-built Go binaries
    mcp_binary_path=os.path.join(os.path.dirname(__file__), "build", "mcp"),
    oauth_proxy_binary_path=os.path.join(os.path.dirname(__file__), "build", "oauth_proxy"),

    # Optional: extra environment variables for the MCP Lambda
    mcp_extra_env={"MY_TABLE": "my-table-name"},

    # Optional: extra IAM policies for the MCP Lambda
    mcp_extra_policies=[
        iam.PolicyStatement(
            actions=["dynamodb:GetItem", "dynamodb:Query"],
            resources=["arn:aws:dynamodb:*:*:table/my-table"],
        )
    ],

    # Optional: MCP Lambda timeout (default: 10 seconds)
    mcp_timeout=Duration.seconds(30),

    # Optional: enable test CIMD endpoint at /.well-known/test-cimd.json
    enable_test_cimd=False,
)

self.mcp_oauth = McpOAuthConstruct(self, "McpOAuth", mcp_config)

# Access the HTTP API if needed (e.g., for CfnOutput)
api_endpoint = self.mcp_oauth.api_endpoint
http_api = self.mcp_oauth.http_api
```

### Configuration Parameters

| Parameter | Required | Default | Description |
|---|---|---|---|
| `espuser_issuer` | Yes | — | ESP User OIDC issuer URL the proxy brokers end-user auth to |
| `mcp_oidc_client_id` | Yes | — | OIDC registry client id the proxy presents (public PKCE client) |
| `mcp_binary_path` | Yes | — | Path to the pre-built MCP Lambda binary directory |
| `oauth_proxy_binary_path` | Yes | — | Path to the pre-built OAuth Proxy Lambda binary directory |
| `mcp_extra_env` | No | `{}` | Extra environment variables for the MCP Lambda |
| `mcp_extra_policies` | No | `[]` | Extra IAM `PolicyStatement`s for the MCP Lambda role |
| `mcp_timeout` | No | `10s` | MCP Lambda timeout |
| `enable_test_cimd` | No | `False` | Enable `/.well-known/test-cimd.json` endpoint for testing |

### What the Construct Creates

- **HTTP API Gateway v2** with CORS (GET, POST from all origins)
- **OAuth Proxy Lambda** (ARM64, `provided.al2023`, 128 MB) that brokers to the ESP User OIDC issuer, presenting the seeded OIDC registry client id
- **Secrets Manager secret** holding a stable HMAC key used to sign the OAuth `state` parameter (`OAUTH_STATE_SECRET`)
- **MCP Lambda** (ARM64, `provided.al2023`, 128 MB) that verifies ESP User OIDC tokens against the issuer's JWKS fetched over HTTPS (no issuer-side credentials or SSM access required; add project-specific access via `mcp_extra_policies`)
- **Routes:**
  - `GET /.well-known/oauth-protected-resource`
  - `GET /.well-known/oauth-authorization-server`
  - `GET /.well-known/test-cimd.json` (if `enable_test_cimd=True`)
  - `GET /oauth2/authorize`
  - `GET /oauth2/callback`
  - `POST /oauth2/token`
  - `GET, POST /v1/mcp`

### Construct Outputs

| Property | Type | Description |
|---|---|---|
| `http_api` | `HttpApi` | The API Gateway v2 HTTP API (for adding routes or CfnOutput) |
| `api_endpoint` | `str` | The HTTP API endpoint URL |
| `mcp_function` | `Function` | The MCP Lambda function |
| `oauth_proxy_function` | `Function` | The OAuth Proxy Lambda function |

## Building

The Go binaries must be cross-compiled for AWS Lambda (Linux ARM64) before CDK deployment:

```bash
# Build the MCP Lambda
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build \
    -ldflags "-s -w" -tags lambda.norpc -trimpath \
    -o build/mcp/bootstrap ./path/to/your/mcp_main.go

# Build the OAuth Proxy Lambda
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build \
    -ldflags "-s -w" -tags lambda.norpc -trimpath \
    -o build/oauth_proxy/bootstrap ./mcp/proxy/handlers/mcp_oauth_proxy/mcp_oauth_proxy_main.go
```

Set `mcp_binary_path` and `oauth_proxy_binary_path` in `McpOAuthConfig` to the `build/mcp` and `build/oauth_proxy` directories respectively.

## Testing

### Testing the Authenticator

The authenticator verifies RS256 tokens against the issuer's JWKS, fetched over HTTPS from `ESPUSER_ISSUER + /.well-known/jwks.json`. A test stands up an `httptest` server publishing the test signing key's public JWK at that path, points `ESPUSER_ISSUER` at it, and mints a token signed by the matching private key.

### MCP Protocol

The MCP endpoint at `/v1/mcp` accepts JSON-RPC 2.0 over HTTP POST:

- `initialize` — returns server info and capabilities (no auth required)
- `notifications/initialized` — acknowledgment (no auth required)
- `tools/list` — lists available tools (auth required)
- `tools/call` — calls a tool by name (auth required)

A `GET /v1/mcp` returns HTTP 401 with a `WWW-Authenticate` header pointing to the OAuth Protected Resource Metadata (RFC 9728), which enables MCP clients to discover the OAuth authorization server automatically.

## Environment Variables

The following environment variables are set automatically by the CDK construct on the MCP Lambda:

| Variable | Description |
|---|---|
| `AWS_ACCOUNT_ID` | AWS Account ID |
| `ESPUSER_ISSUER` | ESP User OIDC issuer URL; tokens are verified against `<issuer>/.well-known/jwks.json` |

Anything in `mcp_extra_env` is merged onto the MCP Lambda's environment as well.

The OAuth Proxy Lambda receives:

| Variable | Description |
|---|---|
| `AWS_ACCOUNT_ID` | AWS Account ID |
| `MCP_BASE_URL` | HTTP API endpoint URL |
| `ESPUSER_ISSUER` | ESP User OIDC issuer the proxy brokers auth to (discovery + JWKS host) |
| `MCP_OIDC_CLIENT_ID` | OIDC registry client id the proxy presents (public PKCE client) |
| `OAUTH_STATE_SECRET` | HMAC key used to sign/verify the OAuth `state` parameter |
| `ENABLE_TEST_CIMD` | `"true"` if test CIMD is enabled |
