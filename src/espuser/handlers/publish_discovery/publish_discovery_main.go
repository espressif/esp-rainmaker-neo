// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Command publish_discovery writes the JWKS built from the KMS signing key's public half to SSM at deploy, where the well-known Lambda serves it at /.well-known/jwks.json and every verifier reads it. Spec: espuser/docs/specs/oidc-oauth2.md.
package main

import (
	"context"
	"encoding/json"
	"os"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/kmsutil"
	"github.com/espressif/esp-rainmaker-neo/src/awsutils/ssmutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/jwtutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"

	"github.com/aws/aws-lambda-go/cfn"
	"github.com/aws/aws-lambda-go/lambda"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

type Config struct {
	JWKSParam string
	KMSKeyARN string
}

func main() {
	lambda.Start(cfn.LambdaWrap(handleCustomResource))
}

// handleCustomResource publishes on Create and Update (idempotent); Delete is a no-op. The stable PhysicalResourceID keeps it the same logical resource across updates.
func handleCustomResource(ctx context.Context, event cfn.Event) (physicalResourceID string, data map[string]any, err error) {
	physicalResourceID = "espuser-oauth-documents"

	if event.RequestType == cfn.RequestDelete {
		rlog.Info(ctx).Msg("Skipping JWKS publish (Delete)")
		return physicalResourceID, nil, nil
	}

	cfg := Config{
		JWKSParam: os.Getenv("USER_JWKS_PARA_NAME"),
		KMSKeyARN: os.Getenv("ESPUSER_KMS_SIGNING_KEY_ARN"),
	}

	if err := publish(ctx, cfg); err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to publish the JWKS")
		return physicalResourceID, nil, err
	}

	rlog.Info(ctx).Msg("JWKS published")
	return physicalResourceID, nil, nil
}

func publish(ctx context.Context, cfg Config) error {
	if cfg.JWKSParam == "" {
		return rmerror.NewRMError(nil, "JWKS publish: USER_JWKS_PARA_NAME is required")
	}
	if cfg.KMSKeyARN == "" {
		return rmerror.NewRMError(nil, "JWKS publish: ESPUSER_KMS_SIGNING_KEY_ARN is required")
	}

	pubKey, err := kmsutil.RSAPublicKey(ctx, cfg.KMSKeyARN)
	if err != nil {
		return err
	}
	jwksJSON, err := json.Marshal(jwtutil.BuildJWKS(jwtutil.BuildJWK(pubKey, jwtutil.RSAThumbprint(pubKey))))
	if err != nil {
		return rmerror.NewRMError(err, "JWKS publish: failed to marshal the JWKS")
	}
	// Public keys only, so a plain String parameter rather than SecureString.
	return ssmutil.StoreParameterWithType(ctx, cfg.JWKSParam, string(jwksJSON), ssmtypes.ParameterTypeString)
}
