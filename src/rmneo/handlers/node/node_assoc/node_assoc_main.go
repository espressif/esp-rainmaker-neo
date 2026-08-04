// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	crand "crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"github.com/espressif/esp-cloud-common/go/rbac/rbac"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/assoc_request_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/ids"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"net/http"
	"strings"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/lambdautil"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngrequest"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

func handleRequest(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	if request.HTTPMethod != "POST" {
		return utils.APIGwRespJSON(http.StatusMethodNotAllowed, utils.NewAPIStatus("Method not allowed")), nil
	}

	switch request.Resource {
	case "/v1/groups/{groupId}/node-assoc-requests":
		return handleInitiate(ctx, request)

	case "/v1/groups/{groupId}/node-assoc-requests/{requestId}/verify":
		return handleVerify(ctx, request)

	case "/v1/groups/{groupId}/node-assoc-requests/{requestId}/confirm":
		return handleConfirm(ctx, request)

	default:
		return utils.APIGwRespJSON(http.StatusNotFound, utils.NewAPIStatus("Not found")), nil
	}
}

func handleInitiate(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	rmngCtx := user.NewContextWithAPIRequest(ctx, request)

	requestID := ids.GenerateRequestId()
	userID := rmngCtx.GetAccessor().(*user.User).UserID
	groupID := request.PathParameters["groupId"]
	if groupID == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Missing groupId")), nil
	}

	// Load the group to check capabilities
	loadedGroup, err := group.GetGroupByID(rmngCtx, groupID)
	if err != nil {
		rlog.Error(rmngCtx).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusNotFound, utils.NewAPIStatus("Group not found")), nil
	}

	// Check if this is a Matter-capable group
	isMatterGroup := group.ContainsMatterCapability(loadedGroup.Capabilities)

	// For Matter groups, challenge must be 32 bytes (64 hex chars) to serve as CSR nonce
	// For non-Matter groups, use standard alphanumeric challenge
	var challenge string
	if isMatterGroup {
		challenge, err = group.GenerateMatterChallenge()
		if err != nil {
			rlog.Error(rmngCtx).Err(err).Send()
			return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to generate challenge")), nil
		}
	} else {
		challenge = ids.GenerateChallenge()
	}

	// Store the association request
	assocDB := assoc_request_db.NewAssocRequestDB(rmngCtx)
	entry := &assoc_request_db.AssocRequestEntry{
		RequestID:     requestID,
		Challenge:     challenge,
		UserID:        userID,
		GroupID:       groupID,
		IsMatterGroup: isMatterGroup,
		Status:        assoc_request_db.AssocStatusPending,
	}

	err = assocDB.StoreAssocRequest(entry)
	if err != nil {
		rlog.Error(rmngCtx).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to store data")), nil
	}

	responseBody := struct {
		RequestID string `json:"request_id"`
		Challenge string `json:"challenge"`
	}{
		RequestID: requestID,
		Challenge: challenge,
	}

	return utils.APIGwRespJSON(http.StatusCreated, responseBody), nil
}

func handleVerify(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	rmngCtx := user.NewContextWithAPIRequest(ctx, request)
	requestID := request.PathParameters["requestId"]
	pathGroupID := request.PathParameters["groupId"]
	if requestID == "" || pathGroupID == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Missing requestId or groupId")), nil
	}

	// Parse request body
	// Either challenge_response OR nocsr_elements must be provided (mutually exclusive)
	var requestBody struct {
		ChallengeResponse    string `json:"challenge_response,omitempty"`
		NodeID               string `json:"node_id,omitempty"`
		NOCSRElements        string `json:"nocsr_elements,omitempty"`
		AttestationChallenge string `json:"attestation_challenge,omitempty"`
		AttestationSignature string `json:"attestation_signature,omitempty"`
	}
	err := rmngrequest.ExtractRequestStruct(request, &requestBody)
	if err != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid request body")), nil
	}

	// Mutual exclusivity check: exactly one of challenge_response or nocsr_elements must be provided
	if requestBody.ChallengeResponse != "" && requestBody.NOCSRElements != "" {
		return utils.APIGwRespJSON(http.StatusBadRequest,
			utils.NewAPIStatus("challenge_response and nocsr_elements are mutually exclusive")), nil
	}
	if requestBody.ChallengeResponse == "" && requestBody.NOCSRElements == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest,
			utils.NewAPIStatus("Either challenge_response or nocsr_elements is required")), nil
	}

	userID := rmngCtx.GetAccessor().(*user.User).UserID

	// Get item from DynamoDB
	assocDB := assoc_request_db.NewAssocRequestDB(rmngCtx)
	storedEntry, err := assocDB.GetAssocRequestByID(requestID)
	if err != nil || storedEntry == nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid request ID")), nil
	}

	// Validate that the user_id matches
	if userID != storedEntry.UserID {
		return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus("User ID mismatch")), nil
	}

	// Validate that the group_id from path matches the stored group_id
	if pathGroupID != storedEntry.GroupID {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Group ID mismatch")), nil
	}

	// Verify must consume a pending request exactly once: the Matter branch below leaves the row in place for confirm, so without this a single initiate would mint NOCs for unlimited CSRs.
	if err := assocRequestUsable(storedEntry, assoc_request_db.AssocStatusPending); err != nil {
		rlog.Warn(rmngCtx).Err(err).Str("request_id", requestID).Msg("rejecting association request")
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Association request is expired or already used")), nil
	}

	challenge := storedEntry.Challenge
	isMatterGroup := storedEntry.IsMatterGroup

	// Branch based on authentication method
	if requestBody.NOCSRElements != "" {
		// NOCSRElements flow: Matter attestation verification
		// This flow is only valid for Matter groups
		if !isMatterGroup {
			return utils.APIGwRespJSON(http.StatusBadRequest,
				utils.NewAPIStatus("nocsr_elements is only valid for Matter-capable groups")), nil
		}

		// Validate that attestation fields are provided
		if requestBody.AttestationChallenge == "" || requestBody.AttestationSignature == "" {
			return utils.APIGwRespJSON(http.StatusBadRequest,
				utils.NewAPIStatus("When nocsr_elements is provided, attestation_challenge and attestation_signature are required")), nil
		}

		// Decode hex inputs
		nocsrElements, err := hex.DecodeString(requestBody.NOCSRElements)
		if err != nil {
			return utils.APIGwRespJSON(http.StatusBadRequest,
				utils.NewAPIStatus("Invalid nocsr_elements hex encoding")), nil
		}

		// Parse NOCSRElements to validate CSRNonce matches the stored challenge
		nocsrFields, err := group.ParseNOCSRElements(nocsrElements)
		if err != nil {
			return utils.APIGwRespJSON(http.StatusBadRequest,
				utils.NewAPIStatus("Invalid nocsr_elements TLV structure")), nil
		}

		// Convert stored challenge (hex string) to bytes and compare with CSRNonce
		expectedNonce, err := hex.DecodeString(challenge)
		if err != nil {
			rlog.Error(rmngCtx).Err(err).Msg("Failed to decode stored challenge")
			return utils.APIGwRespJSON(http.StatusInternalServerError,
				utils.NewAPIStatus("Internal error validating challenge")), nil
		}

		if !bytes.Equal(nocsrFields.CSRNonce, expectedNonce) {
			rlog.Debug(rmngCtx).
				Str("expectedNonce", hex.EncodeToString(expectedNonce)).
				Str("receivedNonce", hex.EncodeToString(nocsrFields.CSRNonce)).
				Msg("CSRNonce mismatch")
			return utils.APIGwRespJSON(http.StatusBadRequest,
				utils.NewAPIStatus("CSRNonce does not match the challenge from initiate")), nil
		}

		attestationChallenge, err := hex.DecodeString(requestBody.AttestationChallenge)
		if err != nil {
			return utils.APIGwRespJSON(http.StatusBadRequest,
				utils.NewAPIStatus("Invalid attestation_challenge hex encoding")), nil
		}

		attestationSignature, err := hex.DecodeString(requestBody.AttestationSignature)
		if err != nil {
			return utils.APIGwRespJSON(http.StatusBadRequest,
				utils.NewAPIStatus("Invalid attestation_signature hex encoding")), nil
		}

		// Verify attestation signature
		attestationInput := &MatterAttestationInput{
			NOCSRElements:        nocsrElements,
			AttestationChallenge: attestationChallenge,
			AttestationSignature: attestationSignature,
		}

		attestationResult, err := verifyMatterAttestation(ctx, attestationInput)
		if err != nil {
			rlog.Error(rmngCtx).Err(err).Msg("Attestation verification failed")
			return utils.APIGwRespJSON(http.StatusUnauthorized,
				utils.NewAPIStatus("Attestation signature verification failed")), nil
		}

		// Determine node_id: use extracted from vendor_reserved1, or generate random for pure Matter nodes
		nodeID := attestationResult.NodeID
		if attestationResult.UseGeneratedID {
			// Generate a random 8-byte (64-bit) hex string that can be directly used as a Matter Node ID
			randBytes := make([]byte, 8)
			if _, err := crand.Read(randBytes); err != nil {
				rlog.Error(rmngCtx).Err(err).Msg("Failed to generate random node ID")
				return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to generate node ID")), nil
			}
			nodeID = strings.ToUpper(hex.EncodeToString(randBytes))
			rlog.Debug(rmngCtx).Str("generatedNodeID", nodeID).Msg("Generated random node_id for pure Matter node")
		}

		// Convert extracted DER CSR to PEM format
		csrPEM := "-----BEGIN CERTIFICATE REQUEST-----\n" +
			base64Encode(attestationResult.CSR) +
			"\n-----END CERTIFICATE REQUEST-----"

		rlog.Debug(rmngCtx).Msg("Successfully extracted CSR from NOCSRElements and verified attestation")

		// Process Matter verification with the CSR
		return handleMatterVerifyWithCSR(ctx, rmngCtx, assocDB, requestID, nodeID, csrPEM, storedEntry.GroupID)
	}

	// challenge_response flow: traditional signature verification
	// Require node_id when using challenge_response
	if requestBody.NodeID == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest,
			utils.NewAPIStatus("node_id is required when using challenge_response")), nil
	}

	// Create a new Node object
	n := node.NewNode(requestBody.NodeID)

	// Fetch certificates for the node
	err = n.LoadCertificates(ctx)
	if err != nil {
		rlog.Error(rmngCtx).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to fetch certificates for thing")), nil
	}

	// Compute the SHA256 hash of the challenge and verify signature
	challengeHash := sha256.Sum256([]byte(challenge))
	signature, err := hex.DecodeString(requestBody.ChallengeResponse)
	if err != nil {
		rlog.Error(rmngCtx).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid challenge response format")), nil
	}

	err = n.VerifySignature(challengeHash[:], signature)
	if err != nil {
		rlog.Error(rmngCtx).Err(err).Send()
		// Delete the request to prevent replay attacks - challenge is burned. A failed delete leaves the challenge reusable, so surface it as a warning rather than swallowing it.
		if delErr := assocDB.DeleteAssocRequest(requestID); delErr != nil {
			rlog.Warn(rmngCtx).Err(delErr).Str("request_id", requestID).Msg("failed to burn assoc request after signature verification failure")
		}
		return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus("Invalid challenge response")), nil
	}

	// For both Matter and non-Matter groups using challenge_response:
	// The node is verified and added to the group directly (no NOC generation).
	// A challenge_response node is a registered RainMaker device, so it is tagged "rmng"
	// (no "matter" — NOC generation requires the nocsr_elements flow).
	err = confirmNodeAssociation(rmngCtx, assocDB, requestID, requestBody.NodeID, storedEntry.GroupID, []string{group.NodeCapabilityRMNG})
	if err != nil {
		rlog.Error(rmngCtx).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to confirm node association")), nil
	}

	return utils.APIGwRespJSON(http.StatusOK, utils.NewAPIStatus("success")), nil
}

// assocRequestUsable rejects replayed and stale association requests. A missing expiry is treated as expired, not eternal: every write path sets it, so its absence means the row is not one we produced.
func assocRequestUsable(entry *assoc_request_db.AssocRequestEntry, wantStatus string) error {
	if entry.Status != wantStatus {
		return fmt.Errorf("assoc request status is %q, want %q", entry.Status, wantStatus)
	}
	if entry.ExpirationTime <= 0 || time.Now().Unix() > entry.ExpirationTime {
		return fmt.Errorf("assoc request expired at %d", entry.ExpirationTime)
	}
	return nil
}

// MatterAttestationInput contains the input parameters for Matter CSR attestation verification
type MatterAttestationInput struct {
	NOCSRElements        []byte
	AttestationChallenge []byte
	AttestationSignature []byte // Raw r||s format (64 bytes)
}

// MatterAttestationResult contains the result of Matter CSR attestation verification
type MatterAttestationResult struct {
	CSR            []byte // Extracted from NOCSRElements for NOC generation
	NodeID         string // Extracted from vendor_reserved1 (if present)
	UseGeneratedID bool   // true if random node_id should be generated (pure Matter node)
}

// verifyMatterAttestation verifies Matter CSR attestation signature.
// If vendor_reserved1 contains a nodeID and that node has registered certificates,
// the signature is verified against those certificates. Otherwise, verification is skipped
// (pure Matter node flow).
func verifyMatterAttestation(ctx context.Context, input *MatterAttestationInput) (*MatterAttestationResult, error) {
	// 1. Parse NOCSRElements to get CSR and vendor_reserved1
	fields, err := group.ParseNOCSRElements(input.NOCSRElements)
	if err != nil {
		return nil, fmt.Errorf("failed to parse NOCSRElements: %w", err)
	}

	result := &MatterAttestationResult{
		CSR: fields.CSR, // Always extract CSR for NOC generation
	}

	// 2. If no vendor_reserved1, treat as pure Matter node (generate random ID)
	if len(fields.VendorReserved1) == 0 {
		rlog.Debug(ctx).Msg("No vendor_reserved1 in NOCSRElements, treating as pure Matter node")
		result.UseGeneratedID = true
		return result, nil
	}

	// 3. Extract nodeID from vendor_reserved1
	result.NodeID = string(fields.VendorReserved1)
	rlog.Debug(ctx).Str("nodeID", result.NodeID).Msg("Extracted nodeID from vendor_reserved1")

	// 4. Load certificates for this nodeID (thingName)
	n := node.NewNode(result.NodeID)
	err = n.LoadCertificates(ctx)
	if err != nil {
		rlog.Debug(ctx).Err(err).Msg("Failed to load certificates, treating as pure Matter node")
		result.UseGeneratedID = true
		return result, nil // Treat as pure Matter node
	}

	if len(n.Certificates) == 0 {
		rlog.Debug(ctx).Msg("No certificates found for node, treating as pure Matter node")
		result.UseGeneratedID = true
		return result, nil // Treat as pure Matter node
	}

	// 5. Build TBS (to-be-signed) and compute hash
	// TBS = NOCSRElements || AttestationChallenge
	tbs := append(input.NOCSRElements, input.AttestationChallenge...)
	hash := sha256.Sum256(tbs)

	// 6. Convert raw signature to DER
	derSig, err := group.ConvertRawSignatureToDER(input.AttestationSignature)
	if err != nil {
		return nil, fmt.Errorf("failed to convert signature to DER: %w", err)
	}

	// 7. Verify with any registered certificate
	verified := false
	for _, cert := range n.Certificates {
		if cert.CertificatePem == nil {
			continue
		}

		block, _ := pem.Decode([]byte(*cert.CertificatePem))
		if block == nil {
			continue
		}

		parsedCert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}

		pubKey, ok := parsedCert.PublicKey.(*ecdsa.PublicKey)
		if !ok {
			continue // Skip non-ECDSA certificates
		}

		if ecdsa.VerifyASN1(pubKey, hash[:], derSig) {
			verified = true
			break
		}
	}

	if !verified {
		return nil, fmt.Errorf("attestation signature verification failed: no matching certificate found")
	}

	rlog.Debug(ctx).Str("nodeID", result.NodeID).Msg("Attestation signature verified successfully")
	return result, nil
}

// handleMatterVerifyWithCSR handles the Matter verification flow with a PEM-encoded CSR.
// This is a helper function that generates the NOC and updates the request status.
func handleMatterVerifyWithCSR(ctx context.Context, rmngCtx *rmngctx.RmngContext, assocDB *assoc_request_db.AssocRequestDB,
	requestID, nodeID, csrPEM, groupID string) (events.APIGatewayProxyResponse, error) {

	// Load the Matter group
	matterGroup, err := group.LoadMatterGroupFromGrpID(rmngCtx, groupID)
	if err != nil {
		rlog.Error(rmngCtx).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to load Matter group")), nil
	}

	// Generate Device NOC
	nocResult, err := matterGroup.GenerateDeviceNOC(csrPEM, nodeID)
	if err != nil {
		rlog.Error(rmngCtx).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to generate device NOC")), nil
	}

	// Update the assoc request status to "verified" (don't delete yet, wait for confirm)
	err = assocDB.UpdateAssocRequestStatus(requestID, assoc_request_db.AssocStatusVerified, nodeID, nocResult.MatterNodeID)
	if err != nil {
		rlog.Error(rmngCtx).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to update request status")), nil
	}

	// Return NOC and Matter Node ID in response
	responseBody := struct {
		Message      string `json:"message"`
		NOC          string `json:"noc"`
		MatterNodeID string `json:"matter_node_id"`
		NodeID       string `json:"node_id"`
	}{
		Message:      "success",
		NOC:          nocResult.NOC,
		MatterNodeID: nocResult.MatterNodeID,
		NodeID:       nodeID,
	}

	return utils.APIGwRespJSON(http.StatusOK, responseBody), nil
}

// base64Encode encodes bytes to base64 string with line breaks every 64 characters
func base64Encode(data []byte) string {
	encoded := make([]byte, base64.StdEncoding.EncodedLen(len(data)))
	base64.StdEncoding.Encode(encoded, data)

	// Insert line breaks every 64 characters for PEM format
	var result strings.Builder
	for i := 0; i < len(encoded); i += 64 {
		end := i + 64
		if end > len(encoded) {
			end = len(encoded)
		}
		result.Write(encoded[i:end])
		if end < len(encoded) {
			result.WriteByte('\n')
		}
	}
	return result.String()
}

func confirmNodeAssociation(rmngCtx *rmngctx.RmngContext, assocDB *assoc_request_db.AssocRequestDB, requestID, nodeID, groupID string, capabilities []string) error {
	// Delete the assoc request
	if err := assocDB.DeleteAssocRequest(requestID); err != nil {
		return rmerror.NewRMError(err, "failed to delete assoc request")
	}

	// Since the node is verified and confirmed, this user has full access to the node
	err := rmngCtx.SetAllow(utils.NodeAll, nodeID)
	if err != nil {
		return rmerror.NewRMError(err, "failed to set allow")
	}

	// Add node to group, tagging its base capabilities (rmng / matter) on the group-node
	// row in the same write. Feature capabilities are appended later by their
	// capability lambdas.
	err = node.ShadowNodeAddToGroup(rmngCtx, nodeID, groupID, capabilities)
	if err != nil {
		return rmerror.NewRMError(err, "failed to add node to group")
	}

	return nil
}

type ConfirmRequestBody struct {
	Capabilities []string `json:"capabilities"`
}

// handleConfirm handles the final confirmation step for Matter groups
// This is called after the device has received and installed the NOC
func handleConfirm(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	rmngCtx := user.NewContextWithAPIRequest(ctx, request)

	requestID := request.PathParameters["requestId"]
	pathGroupID := request.PathParameters["groupId"]
	if requestID == "" || pathGroupID == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Missing requestId or groupId")), nil
	}

	userID := rmngCtx.GetAccessor().(*user.User).UserID

	// Get item from DynamoDB
	assocDB := assoc_request_db.NewAssocRequestDB(rmngCtx)
	storedEntry, err := assocDB.GetAssocRequestByID(requestID)
	if err != nil || storedEntry == nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid request ID")), nil
	}

	// Validate that the user_id matches
	if userID != storedEntry.UserID {
		return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus("User ID mismatch")), nil
	}

	// Validate that the group_id from path matches the stored group_id
	if pathGroupID != storedEntry.GroupID {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Group ID mismatch")), nil
	}

	if err := assocRequestUsable(storedEntry, assoc_request_db.AssocStatusVerified); err != nil {
		rlog.Warn(rmngCtx).Err(err).Str("request_id", requestID).Msg("rejecting association request")
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Request not in verified status")), nil
	}

	// Get node ID from stored data
	if storedEntry.NodeID == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Node ID not found in request")), nil
	}

	// Parse request body for capabilities
	var requestBody ConfirmRequestBody
	if request.Body != "" {
		if err := json.Unmarshal([]byte(request.Body), &requestBody); err != nil {
			rlog.Warn(rmngCtx).Err(err).Msg("Failed to parse request body, continuing without capabilities")
		}
	}

	// This path always issued a device NOC, so the node is a Matter (fabric) node. A pure
	// Matter node was stored under its generated Matter node id (node_id == matter_node_id);
	// a registered RainMaker device that joined the fabric keeps its thing-name node_id and
	// is additionally tagged "rmng".
	nodeCapabilities := []string{group.MatterCapabilityName}
	if storedEntry.MatterNodeID != "" && storedEntry.NodeID != storedEntry.MatterNodeID {
		nodeCapabilities = append([]string{group.NodeCapabilityRMNG}, nodeCapabilities...)
	}

	// Confirm the node association
	err = confirmNodeAssociation(rmngCtx, assocDB, requestID, storedEntry.NodeID, storedEntry.GroupID, nodeCapabilities)
	if err != nil {
		rlog.Error(rmngCtx).Err(err).Msg("Failed to confirm node association")
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to confirm node association")), nil
	}

	// TODO: We could also model this as a generic event stream via SNS/SQS, so any downstream stacks could process this event for taking any stack specific actions.
	// Invoke capability lambdas for each requested capability
	if len(requestBody.Capabilities) > 0 {
		invokeCapabilityLambda(ctx, requestBody.Capabilities, storedEntry, rmngCtx.Accessor.GetPermissions())
	}

	return utils.APIGwRespJSON(http.StatusOK, utils.NewAPIStatus("success")), nil
}

// invokeCapabilityLambda async-invokes each capability's Lambda by convention: capability "<cap>" maps to the deployed Lambda "rmng-<cap>-capability". The invoke no-ops if that Lambda isn't deployed.
func invokeCapabilityLambda(ctx context.Context, capabilities []string, dbStoredEntry *assoc_request_db.AssocRequestEntry, permissions *rbac.EntityPermissions) {
	payload := map[string]interface{}{
		"assoc_request_db": dbStoredEntry,
		"permissions":      permissions,
	}

	for _, capability := range capabilities {
		fn := fmt.Sprintf("rmng-%s-capability", capability)
		if err := lambdautil.InvokeAsync(ctx, fn, payload); err != nil {
			rlog.Warn(ctx).Err(err).Str("capability", capability).Msg("failed to invoke capability lambda")
			continue
		}
	}
}

func main() {
	lambda.Start(handleRequest)
}
