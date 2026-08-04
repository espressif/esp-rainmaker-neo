// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package iotutil

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"os"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iot"
	"github.com/aws/aws-sdk-go-v2/service/iot/types"
	"github.com/aws/aws-sdk-go-v2/service/kinesisvideo"
	kvs_types "github.com/aws/aws-sdk-go-v2/service/kinesisvideo/types"
	"github.com/aws/smithy-go"
)

// RegisterCertificate registers a certificate with AWS IoT Core
func RegisterCertificate(ctx context.Context, certPEM string) (string, error) {
	iotClient := awscommon.GetIoTClient()

	// Register the certificate
	registerInput := &iot.RegisterCertificateWithoutCAInput{
		CertificatePem: aws.String(certPEM),
		Status:         "ACTIVE",
	}

	resp, err := iotClient.RegisterCertificateWithoutCA(ctx, registerInput)
	if err != nil {
		return "", rmerror.NewRMError(err, "failed to register certificate")
	}

	rlog.Debug(ctx).Str("certificateId", *resp.CertificateId).Msg("certificate registered successfully")
	return *resp.CertificateArn, nil
}

// CreateThing creates a new thing in AWS IoT Core
func CreateThing(ctx context.Context, thingName string) error {
	return CreateThingWithAttributes(ctx, thingName, nil)
}

// CreateThingWithAttributes creates an IoT Thing with the provided
// attribute payload. Pass a nil/empty map for no attributes (identical
// to CreateThing). Used to create child Things with custom attributes.
func CreateThingWithAttributes(ctx context.Context, thingName string, attributes map[string]string) error {
	iotClient := awscommon.GetIoTClient()

	createThingInput := &iot.CreateThingInput{
		ThingName: aws.String(thingName),
	}
	if len(attributes) > 0 {
		createThingInput.AttributePayload = &types.AttributePayload{
			Attributes: attributes,
		}
	}

	_, err := iotClient.CreateThing(ctx, createThingInput)
	if err != nil {
		return rmerror.NewRMError(err, "failed to create thing: thingName: "+thingName)
	}
	rlog.Debug(ctx).Str("thingName", thingName).Msg("thing created successfully")
	return nil
}

// AttachCertificateToThing attaches a certificate to a thing
func AttachCertificateToThing(ctx context.Context, thingName, certArn string) error {
	iotClient := awscommon.GetIoTClient()

	// Attach the certificate to the thing
	attachInput := &iot.AttachThingPrincipalInput{
		ThingName: aws.String(thingName),
		Principal: aws.String(certArn),
	}

	_, err := iotClient.AttachThingPrincipal(ctx, attachInput)
	if err != nil {
		var awsErr smithy.APIError
		if errors.As(err, &awsErr) && awsErr.ErrorCode() == "ResourceAlreadyExistsException" {
			rlog.Debug(ctx).Str("certArn", certArn).Str("thingName", thingName).Msg("certificate already attached to thing")
			return nil
		}
		return rmerror.NewRMError(err, "failed to attach certificate to thing")
	}

	rlog.Debug(ctx).Str("certArn", certArn).Str("thingName", thingName).Msg("certificate attached to thing successfully")
	return nil
}

// attachPolicy attaches a named IoT policy to a certificate.
// Returns nil if the policy is already attached (idempotent).
func attachPolicy(ctx context.Context, policyName, certArn string) error {
	iotClient := awscommon.GetIoTClient()

	_, err := iotClient.AttachPolicy(ctx, &iot.AttachPolicyInput{
		PolicyName: aws.String(policyName),
		Target:     aws.String(certArn),
	})
	if err != nil {
		var awsErr smithy.APIError
		if errors.As(err, &awsErr) && awsErr.ErrorCode() == "ResourceAlreadyExistsException" {
			rlog.Debug(ctx).Str("policyName", policyName).Str("certArn", certArn).Msg("policy already attached to certificate")
			return nil
		}
		return rmerror.NewRMError(err, fmt.Sprintf("failed to attach policy '%s' to certificate: %v", policyName, err))
	}

	rlog.Info(ctx).Str("policyName", policyName).Str("certArn", certArn).Msg("policy attached to certificate successfully")
	return nil
}

// AttachDefaultPolicy attaches the default thing policy to a certificate.
// Per-capability additions owned by core are attached on top of the base policy:
//   - "s3"  → rmng-node-file-policy   (S3 file access via IoT Credential Provider)
//   - "kvs" → rmng-node-video-policy  (KVS access via IoT Credential Provider)
//
// Capability policies owned by a separately-deployed optional stack are
// attached out-of-band by the node-register lifecycle hook
// (nodelifecycle.OnNodeRegister), not here.
func AttachDefaultPolicy(ctx context.Context, certArn string, capabilities []string) error {
	policyName := os.Getenv("DEFAULT_THING_POLICY_NAME")
	if policyName == "" {
		policyName = "rmng-base-node-policy"
		rlog.Warn(ctx).Str("policyName", policyName).Msg("DEFAULT_THING_POLICY_NAME not set, using fallback policy name")
	}

	if err := attachPolicy(ctx, policyName, certArn); err != nil {
		return err
	}

	// Only attach the device file policy if "s3" capability is requested
	if HasCapability(capabilities, "s3") {
		deviceFilePolicyName := os.Getenv("DEVICE_FILE_POLICY_NAME")
		if deviceFilePolicyName == "" {
			deviceFilePolicyName = "rmng-node-file-policy"
			rlog.Warn(ctx).Str("policyName", deviceFilePolicyName).Msg("DEVICE_FILE_POLICY_NAME not set, using fallback policy name")
		}

		if err := attachPolicy(ctx, deviceFilePolicyName, certArn); err != nil {
			return err
		}
	}

	// Attach the device video policy for KVS access via IoT Credential Provider
	if HasCapability(capabilities, "kvs") {
		deviceVideoPolicyName := os.Getenv("DEVICE_VIDEO_POLICY_NAME")
		if deviceVideoPolicyName == "" {
			deviceVideoPolicyName = "rmng-node-video-policy"
			rlog.Warn(ctx).Str("policyName", deviceVideoPolicyName).Msg("DEVICE_VIDEO_POLICY_NAME not set, using fallback policy name")
		}

		if err := attachPolicy(ctx, deviceVideoPolicyName, certArn); err != nil {
			return err
		}
	}

	return nil
}

// CreateSignalingChannel creates a KVS signaling channel with the given name.
// Returns nil if the channel already exists (idempotent).
func CreateSignalingChannel(ctx context.Context, channelName string) error {
	kvsClient := awscommon.GetKVSClient()

	_, err := kvsClient.CreateSignalingChannel(ctx, &kinesisvideo.CreateSignalingChannelInput{
		ChannelName: aws.String(channelName),
		ChannelType: kvs_types.ChannelTypeSingleMaster,
	})
	if err != nil {
		var resourceInUse *kvs_types.ResourceInUseException
		if errors.As(err, &resourceInUse) {
			rlog.Debug(ctx).Str("channelName", channelName).Msg("signaling channel already exists")
			return nil
		}
		return rmerror.NewRMError(err, fmt.Sprintf("failed to create signaling channel '%s'", channelName))
	}

	return nil
}

// HasCapability checks if a specific capability is present in the capabilities slice.
func HasCapability(capabilities []string, target string) bool {
	for _, c := range capabilities {
		if c == target {
			return true
		}
	}
	return false
}

func ListThingGroups(ctx context.Context) ([]string, error) {
	iotClient := awscommon.GetIoTClient()

	listGroupsInput := &iot.ListThingGroupsInput{}
	groups, err := iotClient.ListThingGroups(ctx, listGroupsInput)
	if err != nil {
		return nil, fmt.Errorf("failed to list thing groups: %v", err)
	}

	groupNames := make([]string, 0)
	for _, group := range groups.ThingGroups {
		groupNames = append(groupNames, *group.GroupName)
	}
	return groupNames, nil
}

// DescribeThingGroup returns metadata for a thing group, or nil if it does not exist.
func DescribeThingGroup(ctx context.Context, groupName string) (*iot.DescribeThingGroupOutput, error) {
	iotClient := awscommon.GetIoTClient()

	out, err := iotClient.DescribeThingGroup(ctx, &iot.DescribeThingGroupInput{
		ThingGroupName: aws.String(groupName),
	})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "ResourceNotFoundException" {
			return nil, nil
		}
		return nil, rmerror.NewRMError(err, "failed to describe thing group")
	}
	return out, nil
}

// CreateThingGroup creates a thing group, optionally under a parent group.
func CreateThingGroup(ctx context.Context, groupName string, parentGroupName string) error {
	iotClient := awscommon.GetIoTClient()

	createGroupInput := &iot.CreateThingGroupInput{
		ThingGroupName: aws.String(groupName),
	}
	if parentGroupName != "" {
		createGroupInput.ParentGroupName = aws.String(parentGroupName)
	}

	_, err := iotClient.CreateThingGroup(ctx, createGroupInput)
	if err != nil {
		return rmerror.NewRMError(err, "failed to create thing group")
	}

	rlog.Info(ctx).Str("groupName", groupName).Str("parentGroupName", parentGroupName).Msg("thing group created successfully")
	return nil
}

// AddThingToGroup adds a thing to a thing group
func AddThingToGroup(ctx context.Context, thingName, groupName string) error {
	iotClient := awscommon.GetIoTClient()

	// Add the thing to the group
	addToGroupInput := &iot.AddThingToThingGroupInput{
		ThingName:      aws.String(thingName),
		ThingGroupName: aws.String(groupName),
	}

	_, err := iotClient.AddThingToThingGroup(ctx, addToGroupInput)
	if err != nil {
		return rmerror.NewRMError(err, "failed to add thing to group")
	}

	rlog.Info(ctx).Str("groupName", groupName).Msg("thing added to group successfully")
	return nil
}

func GetThingCertificates(ctx context.Context, thingName string) ([]*types.CertificateDescription, error) {
	iotClient := awscommon.GetIoTClient()

	// List the principals (certificates) attached to the thing
	listPrincipalsInput := &iot.ListThingPrincipalsInput{
		ThingName: aws.String(thingName),
	}
	principalsOutput, err := iotClient.ListThingPrincipals(ctx, listPrincipalsInput)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to list principals for thing")
	}

	certificates := make([]*types.CertificateDescription, 0)
	// For each principal, get the certificate details
	for _, principal := range principalsOutput.Principals {

		certID, err := ExtractCertIDFromARN(principal)
		if err != nil {
			return nil, rmerror.NewRMError(err, "failed to extract certificate ID from ARN")
		}

		describeCertInput := &iot.DescribeCertificateInput{
			CertificateId: aws.String(certID),
		}
		certOutput, err := iotClient.DescribeCertificate(ctx, describeCertInput)
		if err != nil {
			return nil, rmerror.NewRMError(fmt.Errorf("failed to describe certificate %s: %v", certID, err), "")
		}
		// Only append certificates with Active status
		if certOutput.CertificateDescription.Status == types.CertificateStatusActive {
			certificates = append(certificates, certOutput.CertificateDescription)
		}
	}

	return certificates, nil
}

func ExtractCertIDFromARN(arn string) (string, error) {
	parts := strings.Split(arn, "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid ARN format")
	}
	return parts[1], nil
}

// GetCertIDFromPEM derives the AWS IoT certificate ID for a PEM-encoded
// certificate without making an API call. AWS IoT defines the cert ID as the
// lowercase hex SHA-256 of the certificate's DER bytes; computing it locally
// lets us recover the cert's identity (and ARN, via DescribeCertificate) when
// RegisterCertificate returns ResourceAlreadyExistsException on retry.
func GetCertIDFromPEM(certPEM string) (string, error) {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return "", rmerror.NewRMError(fmt.Errorf("failed to parse PEM certificate"), "")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", rmerror.NewRMError(err, "failed to parse PEM certificate")
	}
	hash := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(hash[:]), nil
}

// DescribeCertificateARN returns the canonical ARN for an existing AWS IoT
// certificate. Used by the registration retry path to recover an ARN for a
// cert that was registered by an earlier (partial) attempt.
func DescribeCertificateARN(ctx context.Context, certID string) (string, error) {
	iotClient := awscommon.GetIoTClient()
	out, err := iotClient.DescribeCertificate(ctx, &iot.DescribeCertificateInput{
		CertificateId: aws.String(certID),
	})
	if err != nil {
		return "", rmerror.NewRMError(err, "failed to describe certificate")
	}
	if out == nil || out.CertificateDescription == nil || out.CertificateDescription.CertificateArn == nil {
		return "", rmerror.NewRMError(fmt.Errorf("DescribeCertificate returned no ARN for %s", certID), "")
	}
	return *out.CertificateDescription.CertificateArn, nil
}

func UpdateCertificate(ctx context.Context, certID string, status types.CertificateStatus) error {
	iotClient := awscommon.GetIoTClient()

	updateInput := &iot.UpdateCertificateInput{
		CertificateId: aws.String(certID),
		NewStatus:     status,
	}
	_, err := iotClient.UpdateCertificate(ctx, updateInput)
	if err != nil {
		return rmerror.NewRMError(err, "failed to update certificate")
	}
	return nil
}

// DeleteCertificate deletes a certificate from AWS IoT Core
func DeleteCertificate(ctx context.Context, certID string) error {
	iotClient := awscommon.GetIoTClient()

	err := UpdateCertificate(ctx, certID, types.CertificateStatusInactive)
	if err != nil {
		return rmerror.NewRMError(err, "failed to deactivate certificate")
	}

	// Then delete the certificate
	deleteInput := &iot.DeleteCertificateInput{
		CertificateId: aws.String(certID),
	}
	_, err = iotClient.DeleteCertificate(ctx, deleteInput)
	if err != nil {
		return rmerror.NewRMError(err, "failed to delete certificate")
	}

	rlog.Debug(ctx).Str("certificateId", certID).Msg("certificate deleted successfully")
	return nil
}

// DeleteThing deletes a thing from AWS IoT Core. Idempotent — returns
// nil if the thing does not exist. The node-registration rollback path
// (and other cleanup flows) rely on this being safe to call twice.
func DeleteThing(ctx context.Context, thingName string) error {
	iotClient := awscommon.GetIoTClient()

	deleteInput := &iot.DeleteThingInput{
		ThingName: aws.String(thingName),
	}

	_, err := iotClient.DeleteThing(ctx, deleteInput)
	if err != nil {
		var awsErr smithy.APIError
		if errors.As(err, &awsErr) && awsErr.ErrorCode() == "ResourceNotFoundException" {
			rlog.Debug(ctx).Str("thingName", thingName).Msg("thing already absent; delete is a no-op")
			return nil
		}
		return rmerror.NewRMError(err, "failed to delete thing")
	}

	rlog.Debug(ctx).Str("thingName", thingName).Msg("thing deleted successfully")
	return nil
}

// UpdateThingAttribute sets a single attribute on an IoT thing.
// Pass an empty string as attributeValue to effectively clear the attribute.
// This is a best-effort operation; callers should log but not propagate errors.
func UpdateThingAttribute(ctx context.Context, thingName, attributeKey, attributeValue string) error {
	iotClient := awscommon.GetIoTClient()

	updateInput := &iot.UpdateThingInput{
		ThingName: aws.String(thingName),
		AttributePayload: &types.AttributePayload{
			Attributes: map[string]string{
				attributeKey: attributeValue,
			},
			Merge: true,
		},
	}

	_, err := iotClient.UpdateThing(ctx, updateInput)
	if err != nil {
		return rmerror.NewRMError(err, "failed to update thing attribute")
	}

	rlog.Debug(ctx).Str("thingName", thingName).Str("key", attributeKey).Msg("thing attribute updated")
	return nil
}

// DetachThingPrincipal detaches a certificate from a thing
func DetachThingPrincipal(ctx context.Context, thingName, principal string) error {
	iotClient := awscommon.GetIoTClient()

	detachInput := &iot.DetachThingPrincipalInput{
		ThingName: aws.String(thingName),
		Principal: aws.String(principal),
	}

	_, err := iotClient.DetachThingPrincipal(ctx, detachInput)
	if err != nil {
		return rmerror.NewRMError(err, "failed to detach principal from thing")
	}

	rlog.Debug(ctx).Str("principal", principal).Msg("principal detached from thing successfully")
	return nil
}
