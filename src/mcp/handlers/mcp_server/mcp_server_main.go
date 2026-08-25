// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"os"

	mcpserver "mcp-server"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-lambda-go/lambda"
)

const (
	serverName    = "rainmaker-mcp"
	serverVersion = "1.0.0"
)

// rmngUserContext adapts rmngctx.RmngContext to the mcpserver.UserContext interface.
type rmngUserContext struct {
	rmngCtx *rmngctx.RmngContext
}

func (u *rmngUserContext) GetUserID() string {
	return u.rmngCtx.GetID()
}

func (u *rmngUserContext) GoContext() context.Context {
	return u.rmngCtx.Context
}

func resolveRmngUser(ctx context.Context, userID string) (mcpserver.UserContext, error) {
	newUser := user.NewUser(userID)
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, newUser)
	return &rmngUserContext{rmngCtx}, nil
}

// createServerAuth is a var so tests can override the authenticator.
var createServerAuth = func() mcpserver.Authenticator {
	return mcpserver.NewOIDCAuthenticator(
		os.Getenv("USER_ISSUER"),
		os.Getenv("MCP_CLIENT_ID"),
		os.Getenv("USER_JWKS_PARA_NAME"),
		resolveRmngUser,
	)
}

func createServer() *mcpserver.Server {
	server := mcpserver.NewServer(serverName, serverVersion, createServerAuth())
	registerTools(server)
	return server
}

func main() {
	lambda.Start(createServer().HandleRequest)
}
