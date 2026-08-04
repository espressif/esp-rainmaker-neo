// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package ca_bootstrap mints and publishes the assisted-claiming CA and holds
// the operator certificate configuration it mints from. It is the logic behind
// the superadmin CA bootstrap API (see docs/en/specs/assisted-claiming.md §3.9).
//
// The CA certificate cannot be produced at synth time: it has to be signed by
// the KMS key, which only exists after the base stack is deployed. Mint-once is
// enforced by the write itself (SSM no-overwrite), so a repeated mint cannot
// replace an existing CA; an explicit force (rotation) is the only overwrite.
package ca_bootstrap

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/kmsutil"
	"github.com/espressif/esp-rainmaker-neo/src/awsutils/ssmutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/certissuer"

	ssm_types "github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

// DefaultCAValidity exceeds the leaf lifetime rather than matching it.
//
// The issuer clamps every leaf to the CA's expiry, so a CA with exactly the
// leaf lifetime would hand out steadily shorter certificates as it aged. The
// headroom means leaves get their full term for the first two decades of the
// CA's life, and the clamp remains the backstop that guarantees a leaf can
// never outlive its issuer. Used when Config.ValidityYears is unset.
const DefaultCAValidity = certissuer.DeviceCertValidity + 20*365*24*time.Hour

// commonNameBase prefixes the derived CA subject. On its own it would be
// identical in every deployment, so deriveCommonName appends account and region.
const commonNameBase = "RMNG Claiming CA"

// SSM parameter paths for the claiming CA material and certificate config.
// Defined here as constants — the way the GVA and Alexa admin config APIs name
// their parameters — rather than threaded through Lambda env vars. Keep these in
// sync with src/base_res_constants.py, which the CDK uses to create ParamKeyArn
// and to grant ssm access on these exact names.
const (
	ParamKeyArn  = "/rmng/base/claiming-ca-key-arn"
	ParamCertPem = "/rmng/base/claiming-ca-cert-pem"
	ParamConfig  = "/rmng/base/claiming-config"
)

// Config is the resolved input for a bootstrap run.
type Config struct {
	KeyArnParam  string
	CertPemParam string
	// CommonName overrides the derived CA subject. Empty means derive.
	CommonName string
	// Subject adds operator-configured organizational attributes to the CA
	// subject (country, organization, etc.). Optional.
	Subject certissuer.Subject
	// ValidityYears sets the CA lifetime; 0 means DefaultCAValidity.
	ValidityYears int
	// Force overwrites an existing CA (rotation). When false, minting is
	// mint-once: an existing CA is left untouched. Rotation invalidates the
	// chain of every certificate the previous CA signed, so it is deliberately
	// opt-in and never the default.
	Force bool
}

// deriveCommonName builds a CA subject that identifies the deployment it
// belongs to, from the signing key's ARN
// (arn:aws:kms:<region>:<account>:key/<id>).
//
// Without this every deployment would mint a CA with the identical subject but
// a different key, which makes chain building ambiguous for any trust store
// holding more than one.
func deriveCommonName(keyARN string) string {
	parts := strings.Split(keyARN, ":")
	if len(parts) < 6 {
		return commonNameBase
	}
	region, account := parts[3], parts[4]
	if region == "" || account == "" {
		return commonNameBase
	}
	// Well inside the 64-character ub-common-name bound.
	return fmt.Sprintf("%s %s/%s", commonNameBase, account, region)
}

// Result reports what a run did, so the caller can tell "minted" from
// "already present" without inspecting SSM again.
type Result struct {
	AlreadyPresent bool
	CertPEM        string
	KeyARN         string
	CommonName     string
}

// BootstrapCA mints and publishes the claiming CA, or reports that one is
// already present. It is safe to run repeatedly.
func BootstrapCA(ctx context.Context, cfg Config) (Result, error) {
	// Caching off: this is a one-shot operation, and a stale cached value
	// would be worse than a round trip.
	keyARN, err := ssmutil.GetParameterWithCaching(ctx, cfg.KeyArnParam, false, false)
	if err != nil {
		return Result{}, fmt.Errorf("could not read the claiming CA key ARN from %s "+
			"(is the base stack deployed with claiming enabled?): %w", cfg.KeyArnParam, err)
	}
	if keyARN == "" {
		return Result{}, fmt.Errorf("%s is empty; claiming does not appear to be enabled on this deployment", cfg.KeyArnParam)
	}

	commonName := cfg.CommonName
	if commonName == "" {
		commonName = deriveCommonName(keyARN)
	}

	validity := DefaultCAValidity
	if cfg.ValidityYears > 0 {
		validity = time.Duration(cfg.ValidityYears) * 365 * 24 * time.Hour
	}

	signer, err := kmsutil.NewSigner(ctx, keyARN)
	if err != nil {
		return Result{}, fmt.Errorf("could not use the claiming CA key: %w", err)
	}

	caPEM, err := certissuer.NewSelfSignedCA(commonName, cfg.Subject, signer, validity)
	if err != nil {
		return Result{}, fmt.Errorf("could not mint the CA certificate: %w", err)
	}

	// Rotation: overwrite unconditionally. The caller has explicitly asked to
	// replace the CA, accepting that every certificate the old one signed stops
	// chaining to the published CA.
	if cfg.Force {
		if err := ssmutil.StoreParameterWithType(ctx, cfg.CertPemParam, caPEM, ssm_types.ParameterTypeString); err != nil {
			return Result{}, fmt.Errorf("could not publish the rotated CA certificate: %w", err)
		}
		return Result{CertPEM: caPEM, KeyARN: keyARN, CommonName: commonName}, nil
	}

	// The write is the guard. Minting above is pure computation with no side
	// effects, so producing a certificate we then discard costs nothing and
	// keeps this a single atomic decision point.
	err = ssmutil.StoreParameterIfAbsent(ctx, cfg.CertPemParam, caPEM, ssm_types.ParameterTypeString)
	if errors.Is(err, ssmutil.ErrParameterExists) {
		existing, getErr := ssmutil.GetParameterWithCaching(ctx, cfg.CertPemParam, false, false)
		if getErr != nil {
			return Result{}, fmt.Errorf("a CA is already present but could not be read back: %w", getErr)
		}
		return Result{AlreadyPresent: true, CertPEM: existing, KeyARN: keyARN, CommonName: commonName}, nil
	}
	if err != nil {
		return Result{}, fmt.Errorf("could not publish the CA certificate: %w", err)
	}

	return Result{CertPEM: caPEM, KeyARN: keyARN, CommonName: commonName}, nil
}
