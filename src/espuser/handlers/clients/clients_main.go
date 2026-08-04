// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/clients"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/oauth_clients_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngrequest"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

// Superadmin OAuth client registry (admin authorizer + custom:super_admin). Spec: espuser/docs/en/specs/admin-clients.md.
const (
	pathClients    = "/v1/admin/clients"
	pathClientByID = "/v1/admin/clients/{client_id}"
)

// ClientWriteRequest is the create/patch body. Pointers let Patch tell "unset" from "false".
type ClientWriteRequest struct {
	ClientID     string    `json:"client_id,omitempty"`
	ClientName   *string   `json:"client_name,omitempty"`
	ClientType   string    `json:"client_type,omitempty"`
	RedirectURIs *[]string `json:"redirect_uris,omitempty"`
	GrantTypes   *[]string `json:"grant_types,omitempty"`
	Scopes       *[]string `json:"scopes,omitempty"`
	RequirePKCE  *bool     `json:"require_pkce,omitempty"`
}

// requireSuperAdmin gates on the custom:super_admin claim via auth.RequireSuperAdmin, mapping its errors to status codes.
func requireSuperAdmin(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, bool) {
	_, err := auth.RequireSuperAdmin(ctx, rmngrequest.ExtractAuthToken(request.Headers))
	switch {
	case err == nil:
		return events.APIGatewayProxyResponse{}, true
	case errors.Is(err, auth.ErrNotSuperAdmin):
		return utils.APIGwRespJSON(http.StatusForbidden, utils.NewAPIStatus("super_admin required")), false
	case errors.Is(err, auth.ErrMissingToken):
		return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus("Unauthorized")), false
	default:
		rlog.Error(ctx).Err(err).Msg("Admin token rejected")
		return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus("Unauthorized")), false
	}
}

func handleCreate(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	var req ClientWriteRequest
	if err := rmngrequest.ExtractRequestStruct(request, &req); err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to extract create-client request")
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid request body")), nil
	}

	result, err := clientsService(ctx).Create(clients.CreateInput{
		ClientID:     req.ClientID,
		ClientName:   derefStr(req.ClientName),
		ClientType:   req.ClientType,
		RedirectURIs: derefSlice(req.RedirectURIs),
		GrantTypes:   derefSlice(req.GrantTypes),
		Scopes:       derefSlice(req.Scopes),
		RequirePKCE:  req.RequirePKCE,
	})
	if err != nil {
		// A validation failure or a duplicate client_id is a 400 (client's fault).
		rlog.Error(ctx).Err(err).Msg("Failed to create client")
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus(err.Error())), nil
	}
	return utils.APIGwRespJSON(http.StatusCreated, result), nil
}

func handleList(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	// get_secret=true returns each client's plaintext secret (superadmin-only surface).
	getSecret := strings.EqualFold(request.QueryStringParameters["get_secret"], "true")
	list, err := clientsService(ctx).List(getSecret)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to list clients")
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Internal server error")), nil
	}
	return utils.APIGwRespJSON(http.StatusOK, map[string]any{"clients": list}), nil
}

func handlePut(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	var req ClientWriteRequest
	if err := rmngrequest.ExtractRequestStruct(request, &req); err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to extract put-client request")
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid request body")), nil
	}
	// client_id / client_type are immutable — reject rather than silently ignore.
	if req.ClientID != "" || req.ClientType != "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("client_id and client_type are immutable")), nil
	}

	// PUT is a full replace: the body is the complete desired state of the mutable fields.
	client, err := clientsService(ctx).Update(clientID(request), clients.UpdateInput{
		ClientName:   derefStr(req.ClientName),
		RedirectURIs: derefSlice(req.RedirectURIs),
		GrantTypes:   derefSlice(req.GrantTypes),
		Scopes:       derefSlice(req.Scopes),
		RequirePKCE:  req.RequirePKCE,
	})
	if err != nil {
		if errors.Is(err, oauth_clients_db.ErrOAuthClientNotFound) {
			return utils.APIGwRespJSON(http.StatusNotFound, utils.NewAPIStatus("Not found")), nil
		}
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus(err.Error())), nil
	}
	return utils.APIGwRespJSON(http.StatusOK, client), nil
}

func handleDelete(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	if err := clientsService(ctx).Delete(clientID(request)); err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to delete client")
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Internal server error")), nil
	}
	return utils.APIGwRespJSON(http.StatusOK, utils.NewAPIStatus("Client deleted")), nil
}

func clientsService(ctx context.Context) *clients.Service {
	return clients.NewService(rmngctx.NewRmngContextWithCtx(ctx, nil))
}

func clientID(request events.APIGatewayProxyRequest) string {
	return request.PathParameters["client_id"]
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefSlice(s *[]string) []string {
	if s == nil {
		return nil
	}
	return *s
}

func handleClientsRequest(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	if resp, ok := requireSuperAdmin(ctx, request); !ok {
		return resp, nil
	}

	switch request.Resource {
	case pathClients:
		switch request.HTTPMethod {
		case http.MethodPost:
			return handleCreate(ctx, request)
		case http.MethodGet:
			return handleList(ctx, request)
		}
	case pathClientByID:
		switch request.HTTPMethod {
		case http.MethodPut:
			return handlePut(ctx, request)
		case http.MethodDelete:
			return handleDelete(ctx, request)
		}
	}
	return utils.APIGwRespJSON(http.StatusMethodNotAllowed, utils.NewAPIStatus("Method not allowed")), nil
}

func main() {
	lambda.Start(handleClientsRequest)
}
