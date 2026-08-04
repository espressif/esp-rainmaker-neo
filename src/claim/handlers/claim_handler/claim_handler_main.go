// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

/*
Claim handler lambda: POST /v1/claim/initiate and POST /v1/claim/verify.

A single Lambda backs both claim routes and dispatches on the API Gateway
resource path — the one-handler-per-subsystem shape the rest of the codebase
uses (e.g. the admin-files handler serving several routes from one function).
Keeping initiate and verify in one Lambda avoids a second function, role, log
group and set of permissions in CloudFormation for what is one small subsystem.

initiate mints the node ID before any certificate exists, so certificate
identity is server-determined: verify reads the node ID from the reservation and
never from anything the caller submits.

See docs/en/specs/assisted-claiming.md.
*/
package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/kmsutil"
	"github.com/espressif/esp-rainmaker-neo/src/awsutils/ssmutil"
	"github.com/espressif/esp-rainmaker-neo/src/claim"
	"github.com/espressif/esp-rainmaker-neo/src/claim/ca_bootstrap"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_id_reservation_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/certissuer"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngrequest"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

// The API Gateway resource paths the two claim routes map to. handleRequest
// dispatches on request.Resource so one Lambda serves both.
const (
	claimInitiateResource = "/v1/claim/initiate"
	claimVerifyResource   = "/v1/claim/verify"
)

// handleRequest routes to the initiate or verify logic by resource path, then
// logs once and builds the API Gateway response once. initiate and verify each
// return (status, body, error) and neither logs nor builds the response, so a
// single rmlog and a single APIGwRespJSON live here.
func handleRequest(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	var (
		status int
		body   interface{}
		err    error
	)

	switch request.Resource {
	case claimInitiateResource:
		status, body, err = initiate(ctx, request)
	case claimVerifyResource:
		status, body, err = verify(ctx, request)
	default:
		status, body = http.StatusNotFound, utils.NewAPIStatus("Not found")
	}

	if err != nil {
		rlog.Error(ctx).Err(err).Send()
	}
	return utils.APIGwRespJSON(status, body), nil
}

// resolveCaller returns the request context and the caller's ID. A non-zero
// status means the request should be rejected with it. Shared by initiate and
// verify: the caller dimension of the claim key is what keeps one caller's
// claim from replacing the certificate on another caller's device.
//
// Variants that confer nothing still authenticate the caller.
func resolveCaller(ctx context.Context, request events.APIGatewayProxyRequest, variant claim.Variant) (*rmngctx.RmngContext, string, int) {
	if !variant.RequiresCaller() {
		return rmngctx.NewRmngContextWithCtx(ctx, user.NewUser("")), "", 0
	}

	rctx := user.NewContextWithAPIRequest(ctx, request)
	if rctx == nil || rctx.GetAccessor() == nil || rctx.GetAccessor().GetID() == "" {
		return nil, "", http.StatusUnauthorized
	}
	if _, ok := rctx.GetAccessor().(*user.User); !ok {
		return nil, "", http.StatusUnauthorized
	}
	return rctx, rctx.GetID(), 0
}

// ----------------------------------------------------------------------------
// initiate — assign a node ID to a device MAC address.
// ----------------------------------------------------------------------------

// ClaimInitiateRequest is the request body.
type ClaimInitiateRequest struct {
	MacAddr string `json:"mac_addr" validate:"required"`
}

// ClaimInitiateResponse carries only the server-generated node ID. The
// request's mac_addr is deliberately not echoed (Req 2.8).
type ClaimInitiateResponse struct {
	NodeID string `json:"node_id"`
}

// errQuotaExceeded is mapped to 403 by the caller.
var errQuotaExceeded = errors.New("node quota exceeded")

// initiate performs the claim and returns the response status and body. It
// neither logs nor builds the API Gateway response, so handleRequest keeps a
// single rmlog call and a single APIGwRespJSON.
//
// A non-nil error is for the log only; the status and body returned alongside
// it are what the caller sees, so internal detail never reaches the client.
func initiate(ctx context.Context, request events.APIGatewayProxyRequest) (int, interface{}, error) {
	// Fail closed when claiming is disabled. The route only exists in
	// deployments that enable it, so this guards direct invocation.
	variant := claim.CurrentVariant(ctx)
	if !variant.IsValid() {
		return http.StatusNotFound, utils.NewAPIStatus("Not found"), nil
	}

	if request.HTTPMethod != http.MethodPost {
		return http.StatusMethodNotAllowed, utils.NewAPIStatus("Method not allowed"), nil
	}

	rctx, callerID, status := resolveCaller(ctx, request, variant)
	if status != 0 {
		return status, utils.NewAPIStatus("Unauthorized"), nil
	}

	var req ClaimInitiateRequest
	if err := rmngrequest.ExtractRequestStruct(request, &req); err != nil {
		return http.StatusBadRequest, utils.NewAPIStatus("mac_addr is required"), nil
	}

	key, err := claim.NewKey(variant, req.MacAddr, callerID)
	if err != nil {
		if errors.Is(err, claim.ErrUnknownVariant) {
			// A variant the binary does not understand is a misconfigured
			// deployment, not a bad request.
			return http.StatusInternalServerError, utils.NewAPIStatus("Claiming is misconfigured"), err
		}
		return http.StatusBadRequest, utils.NewAPIStatus(err.Error()), nil
	}

	// Scoped, not "*": initiate reserves and reads this one device's reservation
	// (keyed by MAC) and counts this caller's quota (keyed by claimant).
	rctx.SetAllow(utils.NodeAdminReserveID, key.MacAddr)
	rctx.SetAllow(utils.NodeAdminGetReservation, key.MacAddr)
	rctx.SetAllow(utils.NodeAdminCountReservations, key.ClaimantID)

	nodeID, created, err := reserveNodeID(rctx, variant, key)
	if err != nil {
		if errors.Is(err, errQuotaExceeded) {
			max, _ := claim.Quota(ctx, variant)
			return http.StatusForbidden,
				utils.NewAPIStatus(fmt.Sprintf("node quota reached (max %d)", max)), nil
		}
		return http.StatusInternalServerError, utils.NewAPIStatus("Failed to initiate claim"), err
	}

	if created {
		return http.StatusCreated, ClaimInitiateResponse{NodeID: nodeID}, nil
	}
	return http.StatusOK, ClaimInitiateResponse{NodeID: nodeID}, nil
}

// reserveNodeID is a get-or-create against the reservation table. The bool
// result reports whether a new reservation was created (201) or an existing
// one returned (200).
func reserveNodeID(rctx *rmngctx.RmngContext, variant claim.Variant, key claim.Key) (string, bool, error) {
	db := node_id_reservation_db.NewNodeIDReservationsDB(rctx)

	existing, err := db.GetReservation(key.MacAddr, key.ClaimantID)
	if err != nil {
		return "", false, err
	}
	if existing != nil {
		// Idempotent repeat — never counts against quota, so re-provisioning
		// an already-reserved device keeps working even at the limit.
		return existing.NodeID, false, nil
	}

	if max, applies := claim.Quota(rctx, variant); applies {
		count, err := db.CountReservationsForClaimant(key.ClaimantID)
		if err != nil {
			return "", false, err
		}
		if count >= max {
			return "", false, errQuotaExceeded
		}
	}

	nodeID, err := claim.GenerateNodeID()
	if err != nil {
		return "", false, err
	}

	entry := node_id_reservation_db.ReservationEntry{
		MacAddr:    key.MacAddr,
		ClaimantID: key.ClaimantID,
		NodeID:     nodeID,
	}
	if err := db.CreateReservation(entry); err != nil {
		// The create is conditional on the item not existing, so a concurrent
		// request for the same key can win the race. Return that request's
		// node ID rather than an error: both callers asked the same question
		// and must get the same answer.
		if winner, getErr := db.GetReservation(key.MacAddr, key.ClaimantID); getErr == nil && winner != nil {
			return winner.NodeID, false, nil
		}
		return "", false, err
	}
	return nodeID, true, nil
}

// ----------------------------------------------------------------------------
// verify — exchange a device CSR for a signed certificate and bind it.
// ----------------------------------------------------------------------------

// maxCSRPEMBytes bounds a submitted CSR. A P-256 CSR is a few hundred bytes in
// PEM form, so this generous cap rejects oversized input before it reaches the
// parser (Req 3.1).
const maxCSRPEMBytes = 8 * 1024

// caIDEnv names the issuing-CA identifier recorded on each reservation. It is a
// value, not an SSM parameter path — the CA material's parameter paths are
// ca_bootstrap constants (ParamKeyArn / ParamCertPem / ParamConfig).
const caIDEnv = "CLAIMING_CA_ID"

// errNotClaimed is returned when the caller holds no reservation for the
// requested device; mapped to 403.
var errNotClaimed = errors.New("node not claimed")

// errInvalidCSR marks a malformed or unusable CSR; mapped to 400.
var errInvalidCSR = errors.New("invalid csr")

type ClaimVerifyRequest struct {
	MacAddr      string   `json:"mac_addr" validate:"required"`
	CSR          string   `json:"csr" validate:"required,startswith=-----BEGIN"`
	Capabilities []string `json:"capabilities,omitempty" validate:"omitempty,dive,required"`
}

type ClaimVerifyResponse struct {
	NodeID        string `json:"node_id"`
	Certificate   string `json:"certificate"`
	CACertificate string `json:"ca_certificate"`
}

// verify runs the claim and returns the response status and body. It neither
// logs nor builds the API Gateway response, so handleRequest keeps a single
// rmlog call and a single APIGwRespJSON. A non-nil error is for the log only.
func verify(ctx context.Context, request events.APIGatewayProxyRequest) (int, interface{}, error) {
	variant := claim.CurrentVariant(ctx)
	if !variant.IsValid() {
		return http.StatusNotFound, utils.NewAPIStatus("Not found"), nil
	}
	if request.HTTPMethod != http.MethodPost {
		return http.StatusMethodNotAllowed, utils.NewAPIStatus("Method not allowed"), nil
	}

	rctx, callerID, status := resolveCaller(ctx, request, variant)
	if status != 0 {
		return status, utils.NewAPIStatus("Unauthorized"), nil
	}

	var req ClaimVerifyRequest
	if err := rmngrequest.ExtractRequestStruct(request, &req); err != nil {
		return http.StatusBadRequest, utils.NewAPIStatus("mac_addr and csr are required"), nil
	}

	key, err := claim.NewKey(variant, req.MacAddr, callerID)
	if err != nil {
		if errors.Is(err, claim.ErrUnknownVariant) {
			return http.StatusInternalServerError, utils.NewAPIStatus("Claiming is misconfigured"), err
		}
		return http.StatusBadRequest, utils.NewAPIStatus(err.Error()), nil
	}

	pub, err := parseCSRPublicKey(req.CSR)
	if err != nil {
		return http.StatusBadRequest, utils.NewAPIStatus(err.Error()), nil
	}

	// Reservation-table access is keyed on the caller's own MAC, so scope these
	// grants to it rather than every device. GetReservation needs the read
	// grant now; SetCAID (after a successful bind) needs the reserve grant.
	rctx.SetAllow(utils.NodeAdminGetReservation, key.MacAddr)
	rctx.SetAllow(utils.NodeAdminReserveID, key.MacAddr)

	reservationDB := node_id_reservation_db.NewNodeIDReservationsDB(rctx)
	reservation, err := reservationDB.GetReservation(key.MacAddr, key.ClaimantID)
	if err != nil {
		// Fail closed: an unreadable reservation is never treated as absent
		// permission to proceed.
		return http.StatusInternalServerError, utils.NewAPIStatus("Failed to validate claim"), err
	}
	if reservation == nil || reservation.NodeID == "" {
		return http.StatusForbidden, utils.NewAPIStatus(errNotClaimed.Error()), nil
	}

	// The certificate is bound to exactly the node reserved for this caller, so
	// scope the node-level grants the register/update helpers need to that one
	// node ID — never "*". Only the reserved node ID reaches the bind path, so
	// this confines the request to the caller's own device and keeps the RBAC
	// layer a real backstop rather than a rubber stamp.
	rctx.SetAllow(utils.NodeAdminAll, reservation.NodeID)
	rctx.SetAllow(utils.NodeAll, reservation.NodeID)
	rctx.SetAllow(utils.NodeWriteShadow, reservation.NodeID)

	issuer, caID, err := buildIssuer(ctx)
	if err != nil {
		return http.StatusInternalServerError, utils.NewAPIStatus("Failed to issue certificate"), err
	}

	subject, validity, err := leafProfileDefaults(ctx)
	if err != nil {
		return http.StatusInternalServerError, utils.NewAPIStatus("Claiming is misconfigured"), err
	}

	result, err := issuer.Issue(ctx, pub, certissuer.Profile{
		CAID:       caID,
		CommonName: reservation.NodeID,
		Subject:    subject,
		Validity:   validity,
	})
	if err != nil {
		return http.StatusInternalServerError, utils.NewAPIStatus("Failed to issue certificate"), err
	}

	// Provenance is stamped by the server, never taken from the request — see
	// claim.ProvenanceTags.
	tags := claim.ProvenanceTags(variant, callerID)

	if err := bindCertificate(rctx, reservation.NodeID, result.CertPEM, req.Capabilities, tags, callerID); err != nil {
		// The certificate was signed but never bound, so it is not returned:
		// handing back key material for an unbound Thing would leave the
		// caller holding a credential the cloud does not recognize.
		return http.StatusInternalServerError, utils.NewAPIStatus("Failed to register certificate"), err
	}

	if err := reservationDB.SetCAID(key.MacAddr, key.ClaimantID, caID); err != nil {
		// The node is bound and usable; only the bookkeeping of which CA
		// signed it failed. Log it rather than failing a completed claim.
		rlog.Error(ctx).Err(err).Msg("failed to record issuing CA on reservation")
	}

	return http.StatusCreated, ClaimVerifyResponse{
		NodeID:        reservation.NodeID,
		Certificate:   result.CertPEM,
		CACertificate: result.ChainPEM,
	}, nil
}

// parseCSRPublicKey validates the CSR and returns its public key. Nothing else
// from the CSR is used — in particular its subject is discarded, so the
// certificate's identity cannot be influenced by the caller.
func parseCSRPublicKey(csrPEM string) (*ecdsa.PublicKey, error) {
	if len(csrPEM) > maxCSRPEMBytes {
		return nil, errInvalidCSR
	}
	block, _ := pem.Decode([]byte(csrPEM))
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		return nil, errInvalidCSR
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, errInvalidCSR
	}
	// Proves the requester holds the private key for the public key it is
	// asking us to certify.
	if err := csr.CheckSignature(); err != nil {
		return nil, errInvalidCSR
	}
	pub, ok := csr.PublicKey.(*ecdsa.PublicKey)
	if !ok || pub.Curve != elliptic.P256() {
		return nil, rmerror.NewRMError(nil, "csr public key must be ECDSA P-256")
	}
	return pub, nil
}

// bindCertificate attaches the issued certificate to the node's Thing,
// creating the Thing on a first claim and replacing the previous certificate
// on a re-claim.
//
// Re-claim is the ordinary path here — a factory-erased device comes back and
// asks again — so the replacement carries the claim's capabilities rather than
// silently reverting the node to the base policy, and re-stamps provenance so
// the tags survive too.
func bindCertificate(rctx *rmngctx.RmngContext, nodeID, certPEM string, capabilities, tags []string, adminID string) error {
	err := node.UpdateNodeInRmng(rctx, nodeID, certPEM, nil, tags, capabilities)
	if errors.Is(err, node.ErrNodeNotFound) {
		// First claim only. Re-stamping on every re-claim would turn
		// "registered at" into "last claimed at" and lose the date the node
		// actually entered the fleet; the replacement certificate's own
		// notBefore already records when it was re-issued.
		firstTags := append(append([]string{}, tags...), claim.RegisteredAtTag(time.Now()))
		_, err = node.RegisterNodeInRmng(rctx, certPEM, "", nil, firstTags, adminID, capabilities)
	}
	return err
}

// caMaterial is resolved once per execution environment: the CA certificate
// and key ARN do not change for the life of a deployment.
var (
	caOnce     sync.Once
	caCertPEM  string
	caKeyARN   string
	caLoadErr  error
	caIDCached string
)

// leafProfileDefaults resolves the operator-configured leaf subject and
// validity from the certificate configuration (§3.9), set through the superadmin
// config API and read from SSM at issuance. Both are optional: an unset subject
// leaves the leaf with just its CN (the node ID), and a zero validity falls back
// to certissuer.DeviceCertValidity. The read is cached, so a configuration
// change takes effect on subsequent issuances without a redeploy.
func leafProfileDefaults(ctx context.Context) (certissuer.Subject, time.Duration, error) {
	cc, err := ca_bootstrap.LoadClaimingConfig(ctx, ca_bootstrap.ParamConfig, true)
	if err != nil {
		return certissuer.Subject{}, 0, err
	}
	var validity time.Duration
	if cc.LeafValidityYears > 0 {
		validity = time.Duration(cc.LeafValidityYears) * 365 * 24 * time.Hour
	}
	return cc.Subject, validity, nil
}

// buildIssuer is a package var so tests can supply an issuer without SSM or KMS.
var buildIssuer = defaultBuildIssuer

func defaultBuildIssuer(ctx context.Context) (certissuer.Issuer, string, error) {
	caOnce.Do(func() {
		if caKeyARN, caLoadErr = ssmutil.GetParameterWithCaching(ctx, ca_bootstrap.ParamKeyArn, false); caLoadErr != nil {
			return
		}
		if caCertPEM, caLoadErr = ssmutil.GetParameterWithCaching(ctx, ca_bootstrap.ParamCertPem, false); caLoadErr != nil {
			return
		}
		caIDCached = os.Getenv(caIDEnv)
		if caIDCached == "" {
			caIDCached = "claiming-ca"
		}
	})
	if caLoadErr != nil {
		return nil, "", caLoadErr
	}

	// The signer is built per request so the KMS calls carry this request's
	// context; kmsutil caches the public key, so this costs no round trip.
	signer, err := kmsutil.NewSigner(ctx, caKeyARN)
	if err != nil {
		return nil, "", err
	}
	issuer, err := certissuer.NewSigningIssuer(caCertPEM, signer)
	if err != nil {
		return nil, "", err
	}
	return issuer, caIDCached, nil
}

func main() {
	lambda.Start(handleRequest)
}
