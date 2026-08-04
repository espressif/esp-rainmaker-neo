// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package ca_bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/ssmutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/certissuer"

	ssm_types "github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

// Default certificate lifetimes in years, mirroring the Go issuer defaults
// (certissuer.DeviceCertValidity is 100 years; DefaultCAValidity is leaf + 20).
// Used only to validate operator input; the authoritative defaults apply when
// these are left unset.
const (
	defaultLeafValidityYears = 100
	defaultCAValidityYears   = defaultLeafValidityYears + 20
)

// ClaimingConfig is the operator-set claiming configuration, held as a single
// JSON document in SSM (ParamConfig) and read by every part of the feature at
// runtime: CA minting, leaf issuance, the initiate/verify handlers, and the
// admin config API. Every field is optional; an empty one falls back to the
// built-in default. The subject is applied to both the CA and every leaf; the
// leaf Common Name is always the node ID and never comes from here.
//
// This is the single source of truth for claiming — it deliberately replaces
// the deploy-time rmng-inputs.json block, so the claim stack group carries no
// dependency on that file. Deploying the group stands up the infrastructure;
// this document (plus a minted CA) is what turns claiming on.
type ClaimingConfig struct {
	// Mode is the claiming variant (e.g. "user_authenticated"). Empty means
	// claiming is configured off: the handlers fail closed until it is set.
	// Its value is validated against the implemented variants at the admin API
	// (in the claim package, which owns the Variant type) rather than here, to
	// avoid an import cycle.
	Mode string `json:"mode"`
	// MaxNodesPerClaimant is the per-caller reservation quota for the
	// user_authenticated variant; 0 means the built-in default applies.
	MaxNodesPerClaimant int                `json:"max_nodes_per_claimant"`
	Subject             certissuer.Subject `json:"subject"`
	CACommonName        string             `json:"ca_common_name"`
	CAValidityYears     int                `json:"ca_validity_years"`
	LeafValidityYears   int                `json:"leaf_validity_years"`
}

// Validate rejects operator input that would produce an unusable certificate or
// an out-of-range quota, so a bad configuration fails at the config API rather
// than silently at issuance. The leaf<=CA check reasons about effective values
// (0 means the default) — the issuer also clamps a leaf to the CA's expiry, so
// this only surfaces the mistake earlier and more clearly. Mode is validated by
// the caller via claim.ValidateConfiguredMode (see the type doc).
func (c ClaimingConfig) Validate() error {
	if c.Subject.Country != "" && len(c.Subject.Country) != 2 {
		return rmerror.NewRMError(nil, "subject country must be a two-letter ISO 3166 code")
	}
	if c.CAValidityYears < 0 || c.LeafValidityYears < 0 {
		return rmerror.NewRMError(nil, "certificate validity years must not be negative")
	}
	if c.MaxNodesPerClaimant < 0 {
		return rmerror.NewRMError(nil, "max_nodes_per_claimant must not be negative")
	}

	effectiveLeaf := c.LeafValidityYears
	if effectiveLeaf == 0 {
		effectiveLeaf = defaultLeafValidityYears
	}
	effectiveCA := c.CAValidityYears
	if effectiveCA == 0 {
		effectiveCA = defaultCAValidityYears
	}
	if effectiveLeaf > effectiveCA {
		return rmerror.NewRMError(nil, "leaf validity cannot exceed CA validity")
	}
	return nil
}

// LoadClaimingConfig reads the claiming configuration from SSM. A configuration
// that has never been set (parameter absent) is not an error: it returns the
// zero ClaimingConfig so every consumer falls back to defaults (and claiming
// reads as off). cached enables the process-lifetime cache for hot read paths
// like leaf issuance and per-request variant/quota lookups.
func LoadClaimingConfig(ctx context.Context, param string, cached bool) (ClaimingConfig, error) {
	raw, err := ssmutil.GetParameterWithCaching(ctx, param, false, cached)
	if err != nil {
		var notFound *ssm_types.ParameterNotFound
		if errors.As(err, &notFound) {
			return ClaimingConfig{}, nil
		}
		return ClaimingConfig{}, rmerror.NewRMError(err, "could not read the claiming configuration")
	}
	if raw == "" {
		return ClaimingConfig{}, nil
	}

	var cfg ClaimingConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return ClaimingConfig{}, rmerror.NewRMError(err, "the stored claiming configuration is not valid JSON")
	}
	return cfg, nil
}

// StoreClaimingConfig writes the claiming configuration to SSM as a plain-string
// JSON document. It holds no secret — the private key is a non-exportable KMS
// key and never appears here — so it is a String, not a SecureString.
func StoreClaimingConfig(ctx context.Context, param string, cfg ClaimingConfig) error {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return rmerror.NewRMError(err, "could not encode the claiming configuration")
	}
	return ssmutil.StoreParameterWithType(ctx, param, string(raw), ssm_types.ParameterTypeString)
}
