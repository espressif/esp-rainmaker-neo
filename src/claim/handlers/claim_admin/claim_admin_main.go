// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

/*
Claim admin API: superadmin-only configuration and bootstrap of the claiming CA.

This backs POST/GET /v1/admin/claiming/config (set/read the certificate
configuration) and POST/GET /v1/admin/claiming/ca (mint the CA, or read it). It
mirrors the other admin configuration endpoints: every call is gated on the
caller being a superadmin, and a regular user is refused.

The claiming configuration (mode, per-claimant quota, subject, CA/leaf validity,
CA common name) is a single JSON document in SSM, shared with leaf issuance and
the initiate/verify handlers — it is the whole claiming feature's runtime
configuration, replacing the former deploy-time rmng-inputs.json block. Minting
is mint-once —
the first POST /ca signs and publishes the CA, a repeat reports it unchanged —
and a rotation is an explicit {"force": true} on that call. The signing key is
provisioned by CDK; this API configures and mints, it never creates the key.

See docs/en/specs/assisted-claiming.md §3.9.
*/
package main

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/ssmutil"
	"github.com/espressif/esp-rainmaker-neo/src/claim"
	"github.com/espressif/esp-rainmaker-neo/src/claim/ca_bootstrap"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngrequest"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

// API Gateway resource paths this one Lambda serves; handleRequest dispatches on
// request.Resource. These sit under the shared /v1/admin resource, alongside the
// other admin config APIs (gva/alexa).
const (
	configResource = "/v1/admin/claiming/config"
	caResource     = "/v1/admin/claiming/ca"
)

// caMintRequest is the optional body of POST /ca. force rotates an existing CA.
type caMintRequest struct {
	Force bool `json:"force"`
}

type configResponse struct {
	Config   ca_bootstrap.ClaimingConfig `json:"config"`
	CAMinted bool                        `json:"ca_minted"`
}

type caResponse struct {
	Minted        bool   `json:"minted"`
	CommonName    string `json:"common_name,omitempty"`
	CACertificate string `json:"ca_certificate,omitempty"`
}

// handleRequest routes by resource path, then logs once and builds the response
// once. route returns (status, body, error); a non-nil error is for the log
// only, never the caller.
func handleRequest(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	status, body, err := route(ctx, request)
	if err != nil {
		rlog.Error(ctx).Err(err).Send()
	}
	return utils.APIGwRespJSON(status, body), nil
}

func route(ctx context.Context, request events.APIGatewayProxyRequest) (int, interface{}, error) {
	rctx := user.NewContextWithAPIRequest(ctx, request)
	if rctx == nil || rctx.GetAccessor() == nil {
		return http.StatusForbidden, utils.NewAPIStatus("Forbidden"), nil
	}
	acc, ok := rctx.GetAccessor().(*user.User)
	if !ok || !acc.IsSuperAdmin(rctx) {
		return http.StatusForbidden, utils.NewAPIStatus("Forbidden"), nil
	}

	switch request.Resource {
	case configResource:
		return handleConfig(ctx, request)
	case caResource:
		return handleCA(ctx, request)
	default:
		return http.StatusNotFound, utils.NewAPIStatus("Not found"), nil
	}
}

func handleConfig(ctx context.Context, request events.APIGatewayProxyRequest) (int, interface{}, error) {
	switch request.HTTPMethod {
	case http.MethodPost:
		var cfg ca_bootstrap.ClaimingConfig
		if err := rmngrequest.ExtractRequestStruct(request, &cfg); err != nil {
			return http.StatusBadRequest, utils.NewAPIStatus("invalid configuration"), nil
		}
		if err := cfg.Validate(); err != nil {
			return http.StatusBadRequest, utils.NewAPIStatus(err.Error()), nil
		}
		// Mode is validated in the claim package, which owns the Variant type
		// (ca_bootstrap must not import claim). An empty mode is allowed and
		// leaves claiming configured off.
		if err := claim.ValidateConfiguredMode(cfg.Mode); err != nil {
			return http.StatusBadRequest, utils.NewAPIStatus(err.Error()), nil
		}
		if err := ca_bootstrap.StoreClaimingConfig(ctx, ca_bootstrap.ParamConfig, cfg); err != nil {
			return http.StatusInternalServerError, utils.NewAPIStatus("failed to store configuration"), err
		}
		return http.StatusOK, configResponse{Config: cfg, CAMinted: caMinted(ctx)}, nil

	case http.MethodGet:
		cfg, err := ca_bootstrap.LoadClaimingConfig(ctx, ca_bootstrap.ParamConfig, false)
		if err != nil {
			return http.StatusInternalServerError, utils.NewAPIStatus("failed to read configuration"), err
		}
		return http.StatusOK, configResponse{Config: cfg, CAMinted: caMinted(ctx)}, nil

	default:
		return http.StatusMethodNotAllowed, utils.NewAPIStatus("Method not allowed"), nil
	}
}

func handleCA(ctx context.Context, request events.APIGatewayProxyRequest) (int, interface{}, error) {
	switch request.HTTPMethod {
	case http.MethodPost:
		var req caMintRequest
		if request.Body != "" {
			if err := json.Unmarshal([]byte(request.Body), &req); err != nil {
				return http.StatusBadRequest, utils.NewAPIStatus("invalid request body"), nil
			}
		}

		cc, err := ca_bootstrap.LoadClaimingConfig(ctx, ca_bootstrap.ParamConfig, false)
		if err != nil {
			return http.StatusInternalServerError, utils.NewAPIStatus("failed to read configuration"), err
		}

		res, err := ca_bootstrap.BootstrapCA(ctx, ca_bootstrap.Config{
			KeyArnParam:   ca_bootstrap.ParamKeyArn,
			CertPemParam:  ca_bootstrap.ParamCertPem,
			CommonName:    cc.CACommonName,
			Subject:       cc.Subject,
			ValidityYears: cc.CAValidityYears,
			Force:         req.Force,
		})
		if err != nil {
			return http.StatusInternalServerError, utils.NewAPIStatus("failed to mint the claiming CA"), err
		}

		// AlreadyPresent only comes back on the mint-once path; a forced rotation
		// always reports a fresh mint.
		status := http.StatusCreated
		if res.AlreadyPresent {
			status = http.StatusOK
		}
		return status, caResponse{
			Minted:        !res.AlreadyPresent,
			CommonName:    res.CommonName,
			CACertificate: res.CertPEM,
		}, nil

	case http.MethodGet:
		pem, err := ssmutil.GetParameterWithCaching(ctx, ca_bootstrap.ParamCertPem, false, false)
		if err != nil || pem == "" {
			return http.StatusNotFound, utils.NewAPIStatus("the claiming CA has not been minted"), nil
		}
		return http.StatusOK, caResponse{Minted: true, CACertificate: pem}, nil

	default:
		return http.StatusMethodNotAllowed, utils.NewAPIStatus("Method not allowed"), nil
	}
}

// caMinted reports whether a CA certificate has been published, for the config
// status response. A read error is treated as "not minted" — the status is
// advisory, not a gate.
func caMinted(ctx context.Context) bool {
	pem, err := ssmutil.GetParameterWithCaching(ctx, ca_bootstrap.ParamCertPem, false, false)
	return err == nil && pem != ""
}

func main() {
	lambda.Start(handleRequest)
}
