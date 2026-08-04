// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

/*
Package claim holds the logic shared by the claim-initiate and claim-verify
Lambdas: the configured variant, MAC normalization, claim-key construction,
and the per-caller quota.

The variant and the quota are read at runtime from the claiming configuration
document in SSM (ca_bootstrap.ParamConfig), set through the superadmin config
API — not from deploy-time inputs. Deploying the claim stack group stands up
the infrastructure; the configuration (plus a minted CA) is what turns claiming
on. A configuration with no variant reads as off, and the handlers fail closed.

A claim vends a certificate and assigns a node ID. It confers no permission
over the node. Device control comes from the primary user-node mapping,
established separately by challenge-response — a proof of physical possession.
The reservation is therefore not an authorization record: it exists to make
node IDs stable and idempotent, to bound how many a caller can mint, and to
stop one caller's claim from disturbing another's device. Anything later
scoped to a node's owner must key on the primary mapping, never on this table.

The claim key is what carries that last property. It decides which reservation
— and therefore which node ID — a request resolves to, and its shape differs
by variant:

  - The variant that cannot prove possession (user_authenticated) keys on
    {mac_addr, caller}. Without the caller dimension, one caller claiming
    another caller's MAC would resolve to the same node ID and replace the
    certificate on a device already in service — an unauthenticated denial of
    service against someone else's hardware.
  - The variant that can prove possession (device_attested) keys on mac_addr
    alone. The caller dimension would buy nothing there and would cost
    correctness: a device changing hands should keep one identity, not
    accumulate an orphaned node per previous owner.

See docs/en/specs/assisted-claiming.md.
*/
package claim

import (
	"context"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"strings"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/claim/ca_bootstrap"
)

// Variant is the claiming mode. Its string values are the ones accepted by the
// admin config API (validated via ValidateConfiguredMode) and stored as the
// configuration's Mode.
type Variant string

const (
	// VariantUserAuthenticated gates the claim on an authenticated caller.
	// This is the only variant that vends certificates to users, and it is
	// what any deployment enabling claiming gets.
	VariantUserAuthenticated Variant = "user_authenticated"
	// VariantDeviceAttested gates the claim on the device proving itself
	// against the secrets service, with no caller dimension.
	VariantDeviceAttested Variant = "device_attested"
)

// DefaultMaxNodesPerClaimant bounds how many node IDs one caller can mint.
//
// This is a lifetime cap, not a cap on nodes currently held: reservations are
// never deleted, so removing a node from an account does not return its slot.
// That is deliberate — a reclaimable quota would be no bound at all, since a
// caller could mint, release, and mint again without limit (requirements
// Req 14).
const DefaultMaxNodesPerClaimant = 20

// deviceAttestedClaimantSentinel is the claimant prefix used in the
// device_attested variant, which has no per-caller dimension. The claimant is
// this prefix joined to the MAC (see NewKey), not the bare prefix. It is not a
// user ID and deliberately cannot be produced by the user-ID minting path, so
// a real caller can never collide with it.
const deviceAttestedClaimantSentinel = "__device_attested__"

// macSeparators are stripped before a MAC is stored or looked up.
const macSeparators = ":-."

// Provenance tag keys. These reuse the keys the admin dashboard already
// registers as searchable fleet-index fields ("Registered From", "Created By"),
// so a claimed node is findable by exactly the same filters as one registered
// through the dashboard. Without them, claimed nodes would be the only nodes in
// the fleet absent from a `registered_from` search rather than merely untagged.
const (
	TagKeyRegisteredFrom = "registered_from"
	TagKeyCreatedBy      = "created_by"
	TagKeyRegisteredAt   = "registered_at"
)

// Provenance values for TagKeyRegisteredFrom, one per variant. Distinct values
// because the two say very different things about a node: one was asked for by
// a user, the other was proven by the hardware.
const (
	registeredFromUserAuthenticated = "claim"
	registeredFromDeviceAttested    = "auth-claim"
)

// RegisteredFrom returns the provenance value recorded for this variant, or ""
// if the variant is unknown.
func (v Variant) RegisteredFrom() string {
	switch v {
	case VariantUserAuthenticated:
		return registeredFromUserAuthenticated
	case VariantDeviceAttested:
		return registeredFromDeviceAttested
	}
	return ""
}

// RegisteredAtTag records when a node first entered the fleet.
//
// Nothing else exposes this to an administrator: reg_ts is written to the node
// row but the fleet index the console searches reads shadows, not DynamoDB, and
// iot:DescribeThing carries no creation date at all. As a tag it becomes
// visible and searchable alongside the other provenance.
//
// RFC 3339 UTC so it sorts lexicographically in a fleet-index range query. The
// value contains colons, which is fine — tags split on the first one only.
func RegisteredAtTag(t time.Time) string {
	return TagKeyRegisteredAt + ":" + t.UTC().Format(time.RFC3339)
}

// ProvenanceTags returns the "key:value" tags to stamp on a claimed node.
//
// These are set by the server and are deliberately not accepted from the
// request. The caller is an end user; letting them supply `registered_from`
// would let them label their node as having come from the dashboard, which is
// exactly the claim an administrator needs to be able to trust.
//
// callerID is the authenticated user who drove the claim, which is not the
// same thing as the claim key's claimant. Under device_attested the key is the
// MAC alone — possession is proven, so a caller dimension in the *key* would be
// wrong — but a user may still have initiated the claim, and recording who did
// is worth having for support and audit. Passing the caller rather than the key
// means that tag appears there too, without the key ever gaining a user.
//
// The sentinel is filtered defensively: it is not a user, and it must never
// surface in a column labelled "Created By".
func ProvenanceTags(v Variant, callerID string) []string {
	from := v.RegisteredFrom()
	if from == "" {
		return nil
	}
	tags := []string{TagKeyRegisteredFrom + ":" + from}
	if callerID != "" && !strings.HasPrefix(callerID, deviceAttestedClaimantSentinel) {
		tags = append(tags, TagKeyCreatedBy+":"+callerID)
	}
	// registered_at is deliberately NOT added here. These tags are stamped on
	// every claim, but registered_at means "when the node first entered the
	// fleet" and must be written exactly once. The verify handler appends
	// RegisteredAtTag only on the first-claim path (RegisterNodeInRmng), never
	// on a re-claim, so a factory-erased device keeps its original date rather
	// than having it overwritten with the re-claim time.
	return tags
}

// ErrUnknownVariant is returned when the configured variant is unrecognized.
// Callers map it to a 500: an unknown variant means a misconfigured
// deployment, never a bad request.
var ErrUnknownVariant = fmt.Errorf("unknown claiming variant")

// CurrentVariant reads the configured variant from the claiming configuration
// document in SSM (cached — this is a hot per-request path). Returns "" when
// claiming is unconfigured or the configuration cannot be read, so a claim
// fails closed rather than proceeding on a stale or absent variant.
func CurrentVariant(ctx context.Context) Variant {
	cfg, err := ca_bootstrap.LoadClaimingConfig(ctx, ca_bootstrap.ParamConfig, true)
	if err != nil {
		return ""
	}
	return Variant(strings.TrimSpace(cfg.Mode))
}

// IsValid reports whether v names a variant this build understands.
func (v Variant) IsValid() bool {
	switch v {
	case VariantUserAuthenticated, VariantDeviceAttested:
		return true
	}
	return false
}

// IsImplemented reports whether v is not merely recognized but actually built.
// A known-but-unimplemented variant (device_attested today) must never be
// accepted as configuration: its entitlement check does not exist yet, so
// vending under its name would be weaker than the name implies.
func (v Variant) IsImplemented() bool {
	return v == VariantUserAuthenticated
}

// ValidateConfiguredMode checks an operator-supplied mode before it is stored
// as the claiming configuration. An empty mode is allowed and means claiming is
// configured off. A non-empty mode must name a variant that is both recognized
// and implemented; anything else is rejected so a bad value fails at the config
// API rather than silently at claim time. This lives here, not in ca_bootstrap,
// because it needs the Variant type (and ca_bootstrap must not import claim).
func ValidateConfiguredMode(mode string) error {
	trimmed := strings.TrimSpace(mode)
	if trimmed == "" {
		return nil
	}
	v := Variant(trimmed)
	if !v.IsValid() {
		return rmerror.NewRMError(nil, fmt.Sprintf("unknown claiming mode %q", trimmed))
	}
	if !v.IsImplemented() {
		return rmerror.NewRMError(nil, fmt.Sprintf("claiming mode %q is recognized but not implemented yet", trimmed))
	}
	return nil
}

// RequiresCaller reports whether the variant needs an authenticated caller.
// device_attested authenticates the device, not a user.
func (v Variant) RequiresCaller() bool {
	return v == VariantUserAuthenticated
}

// NormalizeMacAddr strips separators, upper-cases, and validates that what
// remains is 12 or 16 hexadecimal characters.
//
// Normalizing is not cosmetic. The MAC is what identifies a device in the
// reservation key, so "AA:BB:CC:DD:EE:FF", "aa-bb-cc-dd-ee-ff" and
// "AABBCCDDEEFF" must resolve to one reservation. Storing them verbatim would
// give the same physical device three node IDs, three certificates, and three
// quota slots, and would make the idempotence guarantee depend on a client's
// formatting convention rather than on the device (requirements Req 2.2, 2.3).
func NormalizeMacAddr(macAddr string) (string, error) {
	var b strings.Builder
	b.Grow(len(macAddr))
	for _, r := range strings.TrimSpace(macAddr) {
		if strings.ContainsRune(macSeparators, r) {
			continue
		}
		b.WriteRune(r)
	}
	normalized := strings.ToUpper(b.String())

	if len(normalized) != 12 && len(normalized) != 16 {
		return "", rmerror.NewRMError(nil, "mac_addr must be 12 or 16 hexadecimal characters")
	}
	for _, r := range normalized {
		isDigit := r >= '0' && r <= '9'
		isHexUpper := r >= 'A' && r <= 'F'
		if !isDigit && !isHexUpper {
			return "", rmerror.NewRMError(nil, "mac_addr must be hexadecimal")
		}
	}
	return normalized, nil
}

// Key identifies the reservation a claim resolves to. Its fields map directly
// onto the reservation table's partition and sort keys.
type Key struct {
	MacAddr string
	// ClaimantID is who the claim is recorded against: the authenticated
	// caller's internal user ID, or the device sentinel under
	// device_attested. It is not a tenant — this deployment model has no
	// tenancy concept.
	ClaimantID string
}

// NewKey builds the claim key for a variant.
//
// callerID is the authenticated caller's resolved internal user ID; it is
// ignored for device_attested, which has no caller dimension.
func NewKey(v Variant, macAddr, callerID string) (Key, error) {
	normalized, err := NormalizeMacAddr(macAddr)
	if err != nil {
		return Key{}, err
	}

	switch v {
	case VariantUserAuthenticated:
		if callerID == "" {
			return Key{}, rmerror.NewRMError(nil, "claim requires an authenticated caller")
		}
		return Key{MacAddr: normalized, ClaimantID: callerID}, nil
	case VariantDeviceAttested:
		// No caller dimension, but the claimant is still the reservation
		// table's partition key, so it must stay high-cardinality. A single
		// fixed sentinel value would funnel every device_attested write onto
		// one partition — unbounded here, since this variant has no quota and
		// reservations are never deleted. Sharding the sentinel by MAC spreads
		// the writes while preserving one-device-one-identity: the same MAC
		// always derives the same claimant. Any future variant that lacks a
		// caller MUST shard its claimant the same way rather than reintroduce a
		// fixed value, or the hot partition returns.
		return Key{MacAddr: normalized, ClaimantID: deviceAttestedClaimantSentinel + "#" + normalized}, nil
	default:
		return Key{}, ErrUnknownVariant
	}
}

// Quota returns the per-caller reservation limit for the variant and whether a
// limit applies at all.
//
// device_attested has no user quota, and could not have one: its claim key
// carries no caller, so there is nothing to count per user. It does not need
// one either — the device secrets service is the abuse control there, and a
// stronger one, since it bounds issuance to devices that were actually
// manufactured rather than to a number someone picked (Req 6.5, 14.4).
func Quota(ctx context.Context, v Variant) (int, bool) {
	if v != VariantUserAuthenticated {
		return 0, false
	}
	// Cached: the same hot per-request read as CurrentVariant. A missing or
	// unreadable configuration (n <= 0) falls back to the built-in default
	// rather than removing the bound — the quota is an abuse control, so its
	// failure mode must never be "no limit".
	cfg, err := ca_bootstrap.LoadClaimingConfig(ctx, ca_bootstrap.ParamConfig, true)
	if err == nil && cfg.MaxNodesPerClaimant > 0 {
		return cfg.MaxNodesPerClaimant, true
	}
	return DefaultMaxNodesPerClaimant, true
}
