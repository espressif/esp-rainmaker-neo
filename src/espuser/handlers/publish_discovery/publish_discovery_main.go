// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Command publish_discovery builds the static OIDC/OAuth discovery documents and uploads them to S3, once, at deploy. Spec: espuser/docs/en/specs/oidc-aouth2.md.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"os"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/kmsutil"
	"github.com/espressif/esp-rainmaker-neo/src/awsutils/s3util"
	"github.com/espressif/esp-rainmaker-neo/src/awsutils/ssmutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/jwtutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"

	"github.com/aws/aws-lambda-go/cfn"
	"github.com/aws/aws-lambda-go/lambda"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

const (
	jsonContentType      = "application/json"
	metadataCacheControl = "public, max-age=86400"
	jwksCacheControl     = "public, max-age=86400"
)

type Config struct {
	Issuer    string
	APIBase   string
	Bucket    string
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
		rlog.Info(ctx).Msg("Skipping discovery publish (Delete)")
		return physicalResourceID, nil, nil
	}

	cfg := Config{
		Issuer:    os.Getenv("USER_ISSUER"),
		APIBase:   os.Getenv("ESPUSER_API_BASE"),
		Bucket:    os.Getenv("ESPUSER_DISCOVERY_BUCKET"),
		JWKSParam: os.Getenv("USER_JWKS_PARA_NAME"),
		KMSKeyARN: os.Getenv("ESPUSER_KMS_SIGNING_KEY_ARN"),
	}

	if err := publish(ctx, cfg); err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to publish discovery documents")
		return physicalResourceID, nil, err
	}

	rlog.Info(ctx).Msg("Discovery documents published")
	return physicalResourceID, nil, nil
}

func publish(ctx context.Context, cfg Config) error {
	if cfg.Issuer == "" || cfg.APIBase == "" || cfg.Bucket == "" || cfg.JWKSParam == "" {
		return rmerror.NewRMError(nil, "discovery publish: issuer, api base, bucket and jwks param are all required")
	}
	if cfg.KMSKeyARN == "" {
		return rmerror.NewRMError(nil, "discovery publish: ESPUSER_KMS_SIGNING_KEY_ARN is required")
	}

	pubKey, err := kmsutil.RSAPublicKey(ctx, cfg.KMSKeyARN)
	if err != nil {
		return err
	}
	jwks := jwtutil.BuildJWKS(jwtutil.BuildJWK(pubKey, jwtutil.RSAThumbprint(pubKey)))

	documents := []struct {
		key          string
		body         any
		cacheControl string
	}{
		{oidc.OIDCDiscoveryPath, oidc.BuildOIDCMetadata(cfg.Issuer, cfg.APIBase), metadataCacheControl},
		{oidc.OAuthASMetaPath, oidc.BuildAuthServerMetadata(cfg.Issuer, cfg.APIBase), metadataCacheControl},
		{oidc.JWKSPath, jwks, jwksCacheControl},
	}

	for _, doc := range documents {
		// Object keys mirror the URL paths, minus the leading slash.
		objectKey := doc.key[1:]
		if err := putJSON(ctx, cfg.Bucket, objectKey, doc.body, doc.cacheControl); err != nil {
			return err
		}
		rlog.Trace(ctx).Str("bucket", cfg.Bucket).Str("key", objectKey).Msg("Published discovery document")
	}

	// Snapshot the JWKS to SSM so consumers can read the keys from a parameter (admin-parallel), not only fetch S3.
	jwksJSON, err := json.Marshal(jwks)
	if err != nil {
		return rmerror.NewRMError(err, "discovery publish: failed to marshal jwks for ssm")
	}
	// JWKS is public (public keys only), so a plain String parameter — not SecureString.
	if err := ssmutil.StoreParameterWithType(ctx, cfg.JWKSParam, string(jwksJSON), ssmtypes.ParameterTypeString); err != nil {
		return err
	}

	return nil
}

func putJSON(ctx context.Context, bucket, key string, body any, cacheControl string) error {
	data, err := json.Marshal(body)
	if err != nil {
		return rmerror.NewRMError(err, "discovery publish: failed to marshal "+key)
	}
	return s3util.PutObjectWithHeaders(ctx, bucket, key, bytes.NewReader(data), jsonContentType, cacheControl)
}
