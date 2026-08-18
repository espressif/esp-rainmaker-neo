// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package node

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"github.com/espressif/esp-cloud-common/go/rbac/rbac"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/nodes_online_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/certutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"sort"
	"strings"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/iotdputil"
	"github.com/espressif/esp-rainmaker-neo/src/awsutils/iotutil"
	"github.com/espressif/esp-rainmaker-neo/src/awsutils/s3util"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_details_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node/nodelifecycle"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/config"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	iot_types "github.com/aws/aws-sdk-go-v2/service/iot/types"
	"github.com/aws/aws-sdk-go-v2/service/iotdataplane"
	"github.com/aws/smithy-go"
)

type Node struct {
	ThingName    string
	Permissions  rbac.EntityPermissions
	Certificates []*iot_types.CertificateDescription
	GroupID      string
	SubGroupIDs  []string
}

// DataToDevice is the data to be sent to the device's topic from the cloud
type DataToDevice struct {
	Event []string
	Data  map[string]interface{}
	Node  *Node
}

func NewDataToDevice(node *Node) *DataToDevice {
	return &DataToDevice{
		Event: []string{},
		Data:  map[string]interface{}{},
		Node:  node,
	}
}

// GetParams retrieves device-specific parameters from the node's shadow data
// Returns the device parameters as a map, or an empty map if the device is not found
func (n *Node) GetParams(ctx *rmngctx.RmngContext, deviceName string) (map[string]interface{}, error) {
	// Get current device state from IoT shadow
	shadowData, err := n.ReadFromReportedShadow(ctx)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get device shadow data")
	}

	// Extract device data from shadow params
	if shadowData.Params == nil {
		return map[string]interface{}{}, nil
	}

	deviceData, ok := shadowData.Params[deviceName]
	if !ok {
		return map[string]interface{}{}, nil
	}

	deviceDataMap, ok := deviceData.(map[string]interface{})
	if !ok {
		return nil, rmerror.NewRMError(fmt.Errorf("invalid device data format for device %s", deviceName), "invalid device data format")
	}

	return deviceDataMap, nil
}

func (d *DataToDevice) Send(ctx context.Context) error {
	if len(d.Event) > 0 {
		d.Data["event"] = d.Event
		rmngContext := rmngctx.NewRmngContextWithCtx(ctx, utils.NewSystemActor())
		err := d.Node.PublishToDeviceFromCloud(rmngContext, d.Data)
		if err != nil {
			return rmerror.NewRMError(err, "failed to publish to device from cloud")
		}
	}
	return nil
}

func NewNode(thingName string) *Node {
	n := &Node{
		ThingName: thingName,
	}
	// A node has all permissions to itself
	n.Permissions.SetAllow(utils.NodeAll.String(), thingName)
	return n
}

func (n *Node) GetID() string {
	return n.ThingName
}

func (n *Node) GetPermissions() *rbac.EntityPermissions {
	return &n.Permissions
}

// rollbackTracker tracks resources that need to be cleaned up in case of failure
type rollbackTracker struct {
	certID    string
	thingName string
	certArn   string
	cleanup   bool
}

func (r *rollbackTracker) rollback(ctx context.Context) {
	if !r.cleanup {
		return
	}

	if r.thingName != "" {
		if err := iotutil.DeleteThing(ctx, r.thingName); err != nil {
			rlog.Error(ctx).Err(err).Msg("failed to rollback thing creation")
		}
	}

	if r.certID != "" {
		if r.certArn != "" && r.thingName != "" {
			if err := iotutil.DetachThingPrincipal(ctx, r.thingName, r.certArn); err != nil {
				rlog.Error(ctx).Err(err).Msg("failed to rollback certificate attachment")
			}
		}
		if err := iotutil.DeleteCertificate(ctx, r.certID); err != nil {
			rlog.Error(ctx).Err(err).Msg("failed to rollback certificate registration")
		}
	}
}

// registerCertOrRecoverARN registers certPEM in IoT Core. On
// ResourceAlreadyExistsException (the retry-of-partial-success path, or the
// same-cert update path) it recovers the canonical ARN via DescribeCertificate
// and reports alreadyExisted = true. Callers use that flag to decide whether
// to track the cert for rollback — only the call that actually registered
// the cert should clean it up.
func registerCertOrRecoverARN(ctx context.Context, certPEM string) (arn string, alreadyExisted bool, err error) {
	rawArn, regErr := iotutil.RegisterCertificate(ctx, certPEM)
	if regErr == nil {
		return rawArn, false, nil
	}
	var apiErr smithy.APIError
	if !errors.As(regErr, &apiErr) || apiErr.ErrorCode() != "ResourceAlreadyExistsException" {
		return "", false, rmerror.NewRMError(regErr, "failed to register certificate")
	}
	rlog.Debug(ctx).Msg("certificate already registered, recovering ARN")
	certID, idErr := iotutil.GetCertIDFromPEM(certPEM)
	if idErr != nil {
		return "", false, rmerror.NewRMError(idErr, "failed to compute cert ID for already-registered certificate")
	}
	descArn, descErr := iotutil.DescribeCertificateARN(ctx, certID)
	if descErr != nil {
		return "", false, rmerror.NewRMError(descErr, "failed to recover ARN for already-registered certificate")
	}
	return descArn, true, nil
}

// registerInIotCore provisions the cert/thing/policies and fires the
// node-register hook. It returns the node_type the hook assigned (empty when
// no hook is deployed or none is assigned) so the caller can persist it on the
// node_details row.
func (n *Node) registerInIotCore(rmngCtx *rmngctx.RmngContext, certPEM string, capabilities []string) (string, error) {
	// Initialize rollback tracker
	rb := &rollbackTracker{cleanup: true}
	defer rb.rollback(rmngCtx.Context)

	certArn, alreadyExisted, err := registerCertOrRecoverARN(rmngCtx.Context, certPEM)
	if err != nil {
		return "", err
	}

	// Only mark the cert for rollback if THIS call registered it. A cert that
	// pre-existed (retry path) belongs to a prior attempt and must not be
	// deleted by this attempt's rollback.
	if !alreadyExisted {
		certID, _ := iotutil.ExtractCertIDFromARN(certArn)
		rb.certID = certID
		rb.certArn = certArn
	}

	// Create a new thing in IoT Core. As with the cert, mark for rollback only
	// if THIS call created the Thing — a Thing left over from a prior attempt
	// must not be deleted by this attempt's rollback.
	createdThingHere := true
	err = iotutil.CreateThing(rmngCtx.Context, n.ThingName)
	if err != nil {
		var awsErr smithy.APIError
		if errors.As(err, &awsErr) && awsErr.ErrorCode() == "ResourceAlreadyExistsException" {
			rlog.Debug(rmngCtx).Msg("thing already exists")
			createdThingHere = false
		} else {
			return "", rmerror.NewRMError(err, "failed to create thing")
		}
	}
	if createdThingHere {
		rb.thingName = n.ThingName
	}

	// Attach the certificate to the thing
	err = iotutil.AttachCertificateToThing(rmngCtx.Context, n.ThingName, certArn)
	if err != nil {
		return "", rmerror.NewRMError(err, "failed to attach certificate to thing")
	}

	err = iotutil.AttachDefaultPolicy(rmngCtx.Context, certArn, capabilities)
	if err != nil {
		var awsErr smithy.APIError
		if errors.As(err, &awsErr) && awsErr.ErrorCode() == "ResourceAlreadyExistsException" {
			rlog.Debug(rmngCtx).Msg("default policy already attached to thing")
		} else {
			return "", rmerror.NewRMError(err, "failed to attach default policy to thing")
		}
	}

	// Fire the node-register lifecycle hook synchronously so an optional stack
	// (if deployed) can attach capability-specific IoT policies before the device
	// connects and classify the node (node_type). A hook failure fails
	// registration; the deferred rollback then removes the half-provisioned
	// cert/thing.
	nodeType, err := nodelifecycle.OnNodeRegister(rmngCtx, n.ThingName, capabilities, certArn)
	if err != nil {
		return "", rmerror.NewRMError(err, "node-register hook failed")
	}

	// If we reach here, everything succeeded
	rb.cleanup = false
	return nodeType, nil
}

func (n *Node) LoadCertificates(ctx context.Context) error {
	certificates, err := iotutil.GetThingCertificates(ctx, n.ThingName)
	if err != nil {
		return rmerror.NewRMError(err, "failed to get thing certificates")
	}
	n.Certificates = certificates

	return nil
}

func (n *Node) VerifySignature(challengeHash []byte, signature []byte) error {
	for _, cert := range n.Certificates {
		block, _ := pem.Decode([]byte(*cert.CertificatePem))
		if block == nil {
			continue
		}

		parsedCert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}

		switch publicKey := parsedCert.PublicKey.(type) {
		case *rsa.PublicKey:
			err = rsa.VerifyPSS(publicKey, crypto.SHA256, challengeHash, signature, nil)
		case *ecdsa.PublicKey:
			if ecdsa.VerifyASN1(publicKey, challengeHash, signature) {
				err = nil
			} else {
				err = fmt.Errorf("ECDSA signature verification failed")
			}
		default:
			continue
		}

		if err == nil {
			return nil
		}
	}
	return fmt.Errorf("invalid signature: no matching certificate found")
}

func (n *Node) fetchGroups(rmngCtx *rmngctx.RmngContext) error {
	groupNodeDB := group_node_db.NewGroupNodeDB(rmngCtx)
	nodesGroups, err := groupNodeDB.GetNodesGroup(n.ThingName)
	if err != nil {
		return err
	}
	n.GroupID = nodesGroups.Group
	n.SubGroupIDs = nodesGroups.SubGroups[:]
	return nil
}

func (n *Node) GetNodesGroups(rmngCtx *rmngctx.RmngContext) (group_node_db.NodesGroups, error) {
	if n.GroupID == "" {
		err := n.fetchGroups(rmngCtx)
		if err != nil {
			return group_node_db.NodesGroups{}, err
		}
	}
	return group_node_db.NodesGroups{
		Group:     n.GroupID,
		SubGroups: n.SubGroupIDs,
	}, nil
}

// GetShadowNameForNodeGroups generates a shadow name based on the node's group and subgroups
// The format is "params-{groupID}-{subgroup1}-{subgroup2}-..." where subgroups are sorted alphabetically
func GetShadowNameForNodeGroups(groups group_node_db.NodesGroups) string {
	s := fmt.Sprintf("params-%s", groups.Group)
	sortedSubGroups := make([]string, len(groups.SubGroups))
	copy(sortedSubGroups, groups.SubGroups)
	sort.Strings(sortedSubGroups)
	for _, subGroupID := range sortedSubGroups {
		s += fmt.Sprintf("-%s", subGroupID)
	}
	return s
}

// Function to get indexed shadow name for the node
func (n *Node) getIndexedShadowName(rmngCtx *rmngctx.RmngContext) (string, error) {
	return "iparams", nil
}

func (n *Node) getShadowName(rmngCtx *rmngctx.RmngContext) (string, error) {
	if n.GroupID == "" {
		err := n.fetchGroups(rmngCtx)
		if err != nil {
			return "", rmerror.NewRMError(err, fmt.Sprintf("failed to fetch groups for node %s", n.ThingName))
		}
	}
	if n.GroupID == "" {
		return "", rmerror.NewRMError(fmt.Errorf("node %s is not associated with any group", n.ThingName), "")
	}
	return GetShadowNameForNodeGroups(group_node_db.NodesGroups{Group: n.GroupID, SubGroups: n.SubGroupIDs}), nil
}

func (n *Node) WriteToReportedShadow(rmngCtx *rmngctx.RmngContext, data ReportedOrDesiredShadow) error {
	return n.WriteToShadow(rmngCtx, "reported", data)
}

/* We never actually write to the desired shadow. This API is just for completeness. Please use PublishToDeviceDesired() instead */
func (n *Node) WriteToDesiredShadow(rmngCtx *rmngctx.RmngContext, data ReportedOrDesiredShadow) error {
	return n.WriteToShadow(rmngCtx, "desired", data)
}

func (n *Node) WriteToShadow(rmngCtx *rmngctx.RmngContext, target string, data ReportedOrDesiredShadow) error {
	if err := rmngCtx.IsAuthorized(utils.NodeWriteShadow, n.ThingName); err != nil {
		return rmerror.NewRMError(err, "user does not have write access to the node")
	}

	shadowName, err := n.getShadowName(rmngCtx)
	if err != nil {
		return rmerror.NewRMError(err, "failed to get shadow name")
	}
	return n.writeToShadow(rmngCtx, shadowName, target, data)
}

// WriteToIndexedReportedShadow writes the data to the 'reported' indexed shadow of the node
func (n *Node) WriteToIndexedReportedShadow(rmngCtx *rmngctx.RmngContext, data ReportedOrDesiredShadow) error {
	if err := rmngCtx.IsAuthorized(utils.NodeWriteShadow, n.ThingName); err != nil {
		return rmerror.NewRMError(err, "user does not have write access to the node")
	}

	shadowName, err := n.getIndexedShadowName(rmngCtx)
	if err != nil {
		return rmerror.NewRMError(err, "failed to get indexed shadow name")
	}
	return n.writeToShadow(rmngCtx, shadowName, "reported", data)
}

// ClearUserTags clears user tags from the iparams shadow.
func (n *Node) ClearUserTags(rmngCtx *rmngctx.RmngContext) error {
	if err := rmngCtx.IsAuthorized(utils.NodeWriteShadow, n.ThingName); err != nil {
		return rmerror.NewRMError(err, "user does not have write access to the node")
	}

	shadowName, err := n.getIndexedShadowName(rmngCtx)
	if err != nil {
		return rmerror.NewRMError(err, "failed to get indexed shadow name")
	}

	// Explicit null using raw JSONfor "t" so the shadow merge deletes the tags.
	// Because WriteToIndexedReportedShadow() uses TagsObj.Tags which has omitempty, which would silently drop a nil map instead of emitting "t": null.
	payload := []byte(`{"state":{"reported":{"data":{"user":{"t":null}}}}}`)
	return iotdputil.UpdateThingShadow(rmngCtx.Context, n.ThingName, shadowName, payload)
}

// writeToShadow is a helper function to write data to a specific shadow
func (n *Node) writeToShadow(rmngCtx *rmngctx.RmngContext, shadowName string, target string, data ReportedOrDesiredShadow) error {
	var updatePayload IoTNodeShadow
	if target == "reported" {
		updatePayload = IoTNodeShadow{
			State: &ShadowState{
				Reported: &data,
			},
		}
	} else if target == "desired" {
		updatePayload = IoTNodeShadow{
			State: &ShadowState{
				Desired: &data,
			},
		}
	} else {
		return rmerror.NewRMError(fmt.Errorf("invalid shadow target"), "")
	}

	payload, err := json.Marshal(updatePayload)
	if err != nil {
		return err
	}

	err = iotdputil.UpdateThingShadow(rmngCtx.Context, n.ThingName, shadowName, payload)
	if err != nil {
		return rmerror.NewRMError(err, "failed to publish to shadow")
	}

	return nil
}

// PublishToDeviceFromCloud publishes the data to the device's topic from the cloud
func (n *Node) PublishToDeviceFromCloud(rmngCtx *rmngctx.RmngContext, data map[string]interface{}) error {
	return n.PublishToDevice(rmngCtx, fmt.Sprintf("rainmaker/nodes/%s/from_cloud", n.ThingName), data)
}

func (n *Node) PublishToDeviceDesired(rmngCtx *rmngctx.RmngContext, data map[string]interface{}) error {
	sub_topic_name, err := n.getShadowName(rmngCtx)
	if err != nil {
		return rmerror.NewRMError(err, "failed to get shadow name")
	}
	return n.PublishToDevice(rmngCtx, fmt.Sprintf("rainmaker/nodes/%s/user/%s/params", n.ThingName, sub_topic_name), data)
}

// PublishToDevice publishes the data to the device's topic
func (n *Node) PublishToDevice(rmngCtx *rmngctx.RmngContext, topic string, data map[string]interface{}) error {
	if err := rmngCtx.IsAuthorized(utils.NodePublishToDevice, n.ThingName); err != nil {
		return rmerror.NewRMError(err, "user does not have publish to device access")
	}

	iotClient := awscommon.GetIoTDataPlaneClient()
	responseJSON, err := json.Marshal(data)
	if err != nil {
		return rmerror.NewRMError(err, "error marshaling response data")
	}

	rlog.Info(rmngCtx).Msgf("Publishing to topic: %s", topic)
	_, err = iotClient.Publish(rmngCtx.Context, &iotdataplane.PublishInput{
		Topic:   &topic,
		Payload: responseJSON,
		Qos:     1,
	})
	if err != nil {
		return rmerror.NewRMError(err, "failed to publish to device from cloud")
	}

	return nil
}

func (n *Node) NotifyCleanupGroupAddSync(rmngCtx *rmngctx.RmngContext, oldGroups group_node_db.NodesGroups, groupID string) error {
	n.GroupID = groupID
	// reset subgroups to empty
	n.SubGroupIDs = []string{}

	// Set the group_id attribute on the IoT tßhing so the device can subscribe to group
	// control topics via the static policy variable ${iot:Connection.Thing.Attributes[group_id]}.
	// Best-effort: attribute update failure must not block the group membership operation.
	if err := iotutil.UpdateThingAttribute(rmngCtx.Context, n.ThingName, "group_id", groupID); err != nil {
		rlog.Error(rmngCtx).Err(err).Str("nodeID", n.ThingName).Str("groupID", groupID).Msg("failed to set group_id attribute on IoT thing")
	}

	err := n.MoveShadowFrom(rmngCtx, oldGroups)
	if err != nil {
		// Log error but continue
		rlog.Error(rmngCtx).Err(err).Send()
	}

	// Clear old user tags when moving from a different group
	if oldGroups.Group != "" {
		if err := n.ClearUserTags(rmngCtx); err != nil {
			rlog.Error(rmngCtx).Err(err).Send()
		}
	}

	return n.SendGetGroupInfo(rmngCtx.Context)
}

func (n *Node) NotifySubGroupUpdate(rmngCtx *rmngctx.RmngContext, oldGroups group_node_db.NodesGroups, subGroupID string, operationType group_node_db.SubGroupOperationType) error {
	n.GroupID = oldGroups.Group
	if operationType == group_node_db.SubGroupOperationTypeAdd {
		// Add subGroupID to n.SubGroupIDs
		n.SubGroupIDs = append(oldGroups.SubGroups, subGroupID)
	} else {
		// Remove subGroupID, building a fresh slice so oldGroups.SubGroups (used
		// below to compute the old shadow name) is not mutated in place.
		n.SubGroupIDs = nil
		for _, subGroup := range oldGroups.SubGroups {
			if subGroup != subGroupID {
				n.SubGroupIDs = append(n.SubGroupIDs, subGroup)
			}
		}
	}

	err := n.MoveShadowFrom(rmngCtx, oldGroups)
	if err != nil {
		// Log error but continue
		rlog.Error(rmngCtx).Err(err).Send()
	}
	return n.SendGetGroupInfo(rmngCtx.Context)
}

func (n *Node) NotifyCleaupNodeFromGroupSync(rmngCtx *rmngctx.RmngContext, oldGroups group_node_db.NodesGroups) error {
	// Clear group information since node is no longer in any group
	n.GroupID = ""
	n.SubGroupIDs = []string{}

	if err := iotutil.UpdateThingAttribute(rmngCtx.Context, n.ThingName, "group_id", ""); err != nil {
		rlog.Warn(rmngCtx).Err(err).Str("nodeID", n.ThingName).Msg("failed to clear group_id attribute on IoT thing")
	}

	// Delete the old group shadow
	err := n.DeleteShadow(rmngCtx, oldGroups)
	if err != nil {
		// Log error but continue
		rlog.Error(rmngCtx).Err(err).Send()
	}

	// Clear user tags from iparams shadow
	err = n.ClearUserTags(rmngCtx)
	if err != nil {
		rlog.Error(rmngCtx).Err(err).Send()
	}

	// Send notification to device that it's no longer part of any group
	return n.SendGetGroupInfo(rmngCtx.Context)
}

func (n *Node) CopyShadow(rmngCtx *rmngctx.RmngContext, oldShadowName, newShadowName string) error {
	iotClient := awscommon.GetIoTDataPlaneClient()

	getShadowInput := &iotdataplane.GetThingShadowInput{
		ShadowName: &oldShadowName,
		ThingName:  &n.ThingName,
	}

	getShadowOutput, err := iotClient.GetThingShadow(rmngCtx.Context, getShadowInput)
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "ResourceNotFoundException" {
			return nil
		}
		return rmerror.NewRMError(err, "failed to get old shadow")
	}

	// Parse old shadow
	var shadowState IoTNodeShadow
	err = json.Unmarshal(getShadowOutput.Payload, &shadowState)
	if err != nil {
		// No sane shadow to copy
		return nil
	}

	// Only keep the state field
	if shadowState.State != nil {
		shadowStateToUpdate := IoTNodeShadow{
			State: shadowState.State,
		}

		shadowBytes, err := json.Marshal(shadowStateToUpdate)
		if err != nil {
			return rmerror.NewRMError(err, "failed to marshal shadow state")
		}

		err = iotdputil.UpdateThingShadow(rmngCtx.Context, n.ThingName, newShadowName, shadowBytes)
		if err != nil {
			return rmerror.NewRMError(err, "failed to update new shadow")
		}
	}

	return nil
}

// MoveShadow moves the contents from old shadow name to new shadow name and deletes the old shadow
func (n *Node) MoveShadowFrom(rmngCtx *rmngctx.RmngContext, oldGroup group_node_db.NodesGroups) error {
	newGroup, err := n.GetNodesGroups(rmngCtx)
	if err != nil {
		return rmerror.NewRMError(err, "failed to get new group")
	}
	return n.MoveShadow(rmngCtx, oldGroup, newGroup)
}

// MoveShadow moves the contents from old shadow name to new shadow name and deletes the old shadow
// This separate API is maintained for simplification in testing purposes
func (n *Node) MoveShadow(rmngCtx *rmngctx.RmngContext, oldGroup, newGroup group_node_db.NodesGroups) error {
	oldShadowName := GetShadowNameForNodeGroups(oldGroup)
	newShadowName := GetShadowNameForNodeGroups(newGroup)

	if oldGroup.Group == newGroup.Group {
		// Previous shadow is copied only when we are in the same group
		// This is to avoid scenarios where a device changed ownership, and the new owner
		// gets the old shadow
		err := n.CopyShadow(rmngCtx, oldShadowName, newShadowName)
		if err != nil {
			return err
		}
	}

	// Skip deletion if shadow names are the same
	if oldShadowName == newShadowName {
		return nil
	}

	return n.deleteShadowByName(rmngCtx, oldShadowName)
}

// DeleteShadow deletes the shadow associated with the old group configuration
func (n *Node) DeleteShadow(rmngCtx *rmngctx.RmngContext, oldGroups group_node_db.NodesGroups) error {
	if oldGroups.Group == "" {
		return nil
	}
	return n.deleteShadowByName(rmngCtx, GetShadowNameForNodeGroups(oldGroups))
}

// deleteShadowByName deletes a named shadow. ResourceNotFoundException is treated as success.
func (n *Node) deleteShadowByName(rmngCtx *rmngctx.RmngContext, shadowName string) error {
	iotClient := awscommon.GetIoTDataPlaneClient()
	_, err := iotClient.DeleteThingShadow(rmngCtx.Context, &iotdataplane.DeleteThingShadowInput{
		ShadowName: &shadowName,
		ThingName:  &n.ThingName,
	})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "ResourceNotFoundException" {
			return nil
		}
		return rmerror.NewRMError(err, "failed to delete shadow")
	}
	return nil
}

// SendAlexaEnabled sends a notification to the device about its Alexa enabled status
func (n *Node) SendAlexaEnabled(ctx context.Context) error {
	data := NewDataToDevice(n)
	AppendGetAlexaEn(data, true)
	err := data.Send(ctx)
	if err != nil {
		return err
	}
	return nil
}

// UpdateAlexaEnabled updates the Alexa enabled status for the node
func (n *Node) UpdateAlexaEnabled(ctx context.Context, enabled bool) error {
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, n)
	nodeDetailsDB := node_details_db.NewNodeDetailsDB(rmngCtx)
	return nodeDetailsDB.UpdateServiceData("alexa_en", enabled)
}

// GetAlexaEnStatus gets the Alexa enabled status for the node
func (n *Node) GetAlexaEnStatus(ctx context.Context) (*bool, error) {
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, n)
	nodeDetailsDB := node_details_db.NewNodeDetailsDB(rmngCtx)

	data, err := nodeDetailsDB.GetServiceData(n.ThingName, "alexa_en")
	if err != nil {
		return nil, err
	}

	if data == nil {
		return nil, nil
	}

	enabled, ok := data.(bool)
	if !ok {
		return nil, rmerror.NewRMError(nil, "invalid Alexa enabled status type")
	}

	return &enabled, nil
}

// HandleGetAlexaEn handles the getAlexaEn event with pre-fetched NodeDetails
func HandleGetAlexaEnWithNodeDetails(ctx context.Context, n *Node, responseData *DataToDevice, nodeDetails *node_details_db.NodeDetails) error {
	alexaEnabled := false
	if nodeDetails != nil {
		enabled, err := nodeDetails.GetServiceData("alexa_en")
		if err == nil && enabled != nil {
			if enabledBool, ok := enabled.(bool); ok {
				alexaEnabled = enabledBool
			}
		}
	}

	AppendGetAlexaEn(responseData, alexaEnabled)
	return nil
}

// AppendGetAlexaEn appends Alexa enabled status to the response
func AppendGetAlexaEn(data *DataToDevice, alexaEnabled bool) {
	data.Event = append(data.Event, "getAlexaEn")
	data.Data["getAlexaEn"] = map[string]interface{}{
		"enabled": alexaEnabled,
	}
}

// SendGVAEnabled sends a notification to the device about its GVA enabled status
func (n *Node) SendGVAEnabled(ctx context.Context) error {
	data := NewDataToDevice(n)
	AppendGetGVAEn(data, true)
	err := data.Send(ctx)
	if err != nil {
		return err
	}
	return nil
}

// UpdateGVAEnabled updates the GVA enabled status for the node
func (n *Node) UpdateGVAEnabled(ctx context.Context, enabled bool) error {
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, n)
	nodeDetailsDB := node_details_db.NewNodeDetailsDB(rmngCtx)
	return nodeDetailsDB.UpdateServiceData("gva_en", enabled)
}

// GetGVAEnStatus gets the GVA enabled status for the node
func (n *Node) GetGVAEnStatus(ctx context.Context) (*bool, error) {
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, n)
	nodeDetailsDB := node_details_db.NewNodeDetailsDB(rmngCtx)

	data, err := nodeDetailsDB.GetServiceData(n.ThingName, "gva_en")
	if err != nil {
		return nil, err
	}

	if data == nil {
		return nil, nil
	}

	enabled, ok := data.(bool)
	if !ok {
		return nil, rmerror.NewRMError(nil, "invalid GVA enabled status type")
	}

	return &enabled, nil
}

// HandleGetGVAEnWithNodeDetails handles the getGVAEn event with pre-fetched NodeDetails
func HandleGetGVAEnWithNodeDetails(ctx context.Context, n *Node, responseData *DataToDevice, nodeDetails *node_details_db.NodeDetails) error {
	gvaEnabled := false
	if nodeDetails != nil {
		enabled, err := nodeDetails.GetServiceData("gva_en")
		if err == nil && enabled != nil {
			if enabledBool, ok := enabled.(bool); ok {
				gvaEnabled = enabledBool
			}
		}
	}

	AppendGetGVAEn(responseData, gvaEnabled)
	return nil
}

// AppendGetGVAEn appends GVA enabled status to the response
func AppendGetGVAEn(data *DataToDevice, gvaEnabled bool) {
	data.Event = append(data.Event, "getGVAEn")
	data.Data["getGVAEn"] = map[string]interface{}{
		"enabled": gvaEnabled,
	}
}

// SendGetGroupInfo generates the group info for the node and sends it to the device
func (n *Node) SendGetGroupInfo(ctx context.Context) error {
	data := NewDataToDevice(n)
	err := n.HandleGetGroupInfo(ctx, data)
	if err != nil {
		return err
	}
	return data.Send(ctx)
}

// HandleGetGroupInfo gets the group info for the node and adds it to the response
func (n *Node) HandleGetGroupInfo(ctx context.Context, response *DataToDevice) error {
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, n)
	// TODO: redundant read, an earlier func in the notify path already fetched this node's group info, optimise
	groups, err := n.GetNodesGroups(rmngCtx)
	if err != nil {
		return rmerror.NewRMError(err, "error getting group for node")
	}

	grpInfo := map[string]interface{}{}
	if len(groups.SubGroups) == 0 {
		if groups.Group != "" {
			// Only encode if group exists, otherwise its an empty map
			grpInfo["pgrp"] = groups.Group
		}
	} else {
		// If subgroups are present, then group obviously exists
		grpInfo["pgrp"] = groups.Group
		grpInfo["subgrps"] = groups.SubGroups
	}

	response.Event = append(response.Event, "getGroupInfo")
	response.Data["getGroupInfo"] = grpInfo
	return nil
}

func (n *Node) HandleSetNodeConfig(ctx context.Context, nodeConfig map[string]interface{}, response *DataToDevice) error {
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, n)
	configService := config.NewConfigService()

	err := configService.SetNodeConfig(rmngCtx, nodeConfig)
	if err != nil {
		response.Event = append(response.Event, "setNodeConfig")
		response.Data["setNodeConfig"] = map[string]interface{}{
			"status": "error",
			"error":  err.Error(),
		}
		return err
	}

	response.Event = append(response.Event, "setNodeConfig")
	response.Data["setNodeConfig"] = map[string]interface{}{
		"status": "success",
	}
	return nil
}

// ReadFromReportedShadow reads the data from the 'reported' shadow of the node
func (n *Node) ReadFromReportedShadow(rmngCtx *rmngctx.RmngContext) (ReportedOrDesiredShadow, error) {
	return n.ReadFromShadow(rmngCtx, "reported")
}

func (n *Node) ReadFromDesiredShadow(rmngCtx *rmngctx.RmngContext) (ReportedOrDesiredShadow, error) {
	return n.ReadFromShadow(rmngCtx, "desired")
}

func (n *Node) ReadFromShadow(rmngCtx *rmngctx.RmngContext, target string) (ReportedOrDesiredShadow, error) {
	if err := rmngCtx.IsAuthorized(utils.NodeReadShadow, n.ThingName); err != nil {
		return ReportedOrDesiredShadow{}, rmerror.NewRMError(err, "user does not have read access to the node")
	}

	shadowName, err := n.getShadowName(rmngCtx)
	if err != nil {
		return ReportedOrDesiredShadow{}, rmerror.NewRMError(err, "failed to get shadow name")
	}

	iotClient := awscommon.GetIoTDataPlaneClient()
	getThingShadowInput := &iotdataplane.GetThingShadowInput{
		ThingName:  &n.ThingName,
		ShadowName: &shadowName,
	}

	output, err := iotClient.GetThingShadow(rmngCtx.Context, getThingShadowInput)
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "ResourceNotFoundException" {
			return ReportedOrDesiredShadow{}, nil
		}
		return ReportedOrDesiredShadow{}, rmerror.NewRMError(err, "failed to get shadow")
	}

	var shadowState IoTNodeShadow
	err = json.Unmarshal(output.Payload, &shadowState)
	if err != nil {
		return ReportedOrDesiredShadow{}, rmerror.NewRMError(err, "failed to unmarshal shadow state")
	}

	if shadowState.State == nil {
		return ReportedOrDesiredShadow{}, nil
	}

	switch target {
	case "reported":
		if shadowState.State.Reported == nil {
			return ReportedOrDesiredShadow{}, nil
		}
		return *shadowState.State.Reported, nil
	case "desired":
		if shadowState.State.Desired == nil {
			return ReportedOrDesiredShadow{}, nil
		}
		return *shadowState.State.Desired, nil
	default:
		return ReportedOrDesiredShadow{}, rmerror.NewRMError(nil, "invalid target state")
	}
}

// HandleGetSchedVer handles the getSchedVer event with pre-fetched NodeDetails
func HandleGetSchedVerWithNodeDetails(ctx context.Context, n *Node, responseData *DataToDevice, nodeDetails *node_details_db.NodeDetails) error {
	var version int64 = 0
	if nodeDetails != nil {
		versionPtr, err := nodeDetails.GetServiceVersion("schedule")
		if err == nil && versionPtr != nil {
			version = *versionPtr
		}
	}

	AppendGetSchedVer(responseData, &version)
	return nil
}

// AppendGetSchedVer appends schedule version to the response
func AppendGetSchedVer(data *DataToDevice, version *int64) {
	data.Event = append(data.Event, "getSchedVer")
	data.Data["getSchedVer"] = map[string]interface{}{
		"version": version,
	}
}

// AppendGetTimeSync appends the current server time (epoch ms) to the response.
// Lets devices set their wall clock over the existing MQTT connection instead
// of waiting for SNTP to converge; accuracy is bounded by delivery latency.
func AppendGetTimeSync(data *DataToDevice) {
	data.Event = append(data.Event, "getTimeSync")
	data.Data["getTimeSync"] = map[string]interface{}{
		"time": time.Now().UnixMilli(),
	}
}

// HandleGetSchedDetails handles the getSchedDetails event with pre-fetched NodeDetails
func HandleGetSchedDetailsWithNodeDetails(ctx context.Context, n *Node, responseData *DataToDevice, nodeDetails *node_details_db.NodeDetails) error {
	scheduleMap := make(map[string]interface{})
	var version int64 = 0

	if nodeDetails != nil {
		scheduleData, err := nodeDetails.GetServiceData("schedule")
		if err == nil && scheduleData != nil {
			if scheduleDataMap, ok := scheduleData.(map[string]interface{}); ok {
				scheduleMap = scheduleDataMap
			}
		}

		// Get version
		versionPtr, err := nodeDetails.GetServiceVersion("schedule")
		if err == nil && versionPtr != nil {
			version = *versionPtr
		}
	}

	AppendGetSchedDetails(ctx, responseData, scheduleMap, &version)
	return nil
}

// AppendGetSchedDetails appends schedule details to the response
func AppendGetSchedDetails(ctx context.Context, data *DataToDevice, scheduleData map[string]interface{}, version *int64) {
	rlog.Debug(ctx).Interface("scheduleData", scheduleData).Int64("version", *version).Msg("Appending schedule details to response")
	data.Event = append(data.Event, "getSchedDetails")

	// Create response with version field
	response := make(map[string]interface{})
	for k, v := range scheduleData {
		response[k] = v
	}
	response["version"] = version

	data.Data["getSchedDetails"] = response
}

// SendScheduleDetails sends the schedule details to the device
func (n *Node) SendScheduleDetails(ctx context.Context) error {
	data := NewDataToDevice(n)
	rlog.Debug(ctx).Msgf("Preparing schedule details for device %s", n.ThingName)

	err := HandleGetSchedDetails(ctx, n, data)
	if err != nil {
		rlog.Error(ctx).Err(err).Msgf("Failed to get schedule details for device %s", n.ThingName)
		return err
	}

	err = data.Send(ctx)
	if err != nil {
		rlog.Error(ctx).Err(err).Msgf("Failed to send schedule details to device %s", n.ThingName)
		return err
	}
	rlog.Debug(ctx).Msgf("Successfully sent schedule details to device %s", n.ThingName)
	return nil
}

// HandleGetAlexaEn handles the getAlexaEn event from the device
func HandleGetAlexaEn(ctx context.Context, n *Node, responseData *DataToDevice) error {
	enabled, err := n.GetAlexaEnStatus(ctx)
	if err != nil {
		return rmerror.NewRMError(err, "failed to get Alexa enabled status")
	}

	alexaEnabled := false
	if enabled != nil {
		alexaEnabled = *enabled
	}

	AppendGetAlexaEn(responseData, alexaEnabled)
	return nil
}

// HandleGetSchedVer handles the getSchedVer event from the device
func HandleGetSchedVer(ctx context.Context, n *Node, responseData *DataToDevice) error {
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, n)
	nodeDetailsDB := node_details_db.NewNodeDetailsDB(rmngCtx)

	version, err := nodeDetailsDB.GetServiceVersion(n.ThingName, "schedule")
	if err != nil {
		return rmerror.NewRMError(err, "failed to get schedule version")
	}

	AppendGetSchedVer(responseData, version)
	return nil
}

// HandleGetSchedDetails handles the getSchedDetails event from the device
func HandleGetSchedDetails(ctx context.Context, n *Node, responseData *DataToDevice) error {
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, n)
	nodeDetailsDB := node_details_db.NewNodeDetailsDB(rmngCtx)

	scheduleData, err := nodeDetailsDB.GetServiceData(n.ThingName, "schedule")
	if err != nil {
		return rmerror.NewRMError(err, "failed to get schedule data")
	}

	var scheduleMap map[string]interface{}
	if scheduleData != nil {
		scheduleMap, _ = scheduleData.(map[string]interface{})
	}
	if scheduleMap == nil {
		scheduleMap = make(map[string]interface{})
	}

	// Get version
	version, err := nodeDetailsDB.GetServiceVersion(n.ThingName, "schedule")
	if err != nil {
		return rmerror.NewRMError(err, "failed to get schedule version")
	}

	AppendGetSchedDetails(ctx, responseData, scheduleMap, version)
	return nil
}

// HandleGetTriggerVerWithNodeDetails handles the getTriggerVer event with pre-fetched NodeDetails
func HandleGetTriggerVerWithNodeDetails(ctx context.Context, n *Node, responseData *DataToDevice, nodeDetails *node_details_db.NodeDetails) error {
	var version int64 = 0
	if nodeDetails != nil {
		versionPtr, err := nodeDetails.GetServiceVersion("trigger")
		if err == nil && versionPtr != nil {
			version = *versionPtr
		}
	}

	AppendGetTriggerVer(responseData, &version)
	return nil
}

// AppendGetTriggerVer appends trigger version to the response
func AppendGetTriggerVer(data *DataToDevice, version *int64) {
	data.Event = append(data.Event, "getTriggerVer")
	data.Data["getTriggerVer"] = map[string]interface{}{
		"version": version,
	}
}

// HandleGetTriggerDetailsWithNodeDetails handles the getTriggerDetails event with pre-fetched NodeDetails
func HandleGetTriggerDetailsWithNodeDetails(ctx context.Context, n *Node, responseData *DataToDevice, nodeDetails *node_details_db.NodeDetails) error {
	triggerMap := make(map[string]interface{})
	var version int64 = 0

	if nodeDetails != nil {
		triggerData, err := nodeDetails.GetServiceData("trigger")
		if err == nil && triggerData != nil {
			if data, ok := triggerData.(map[string]interface{}); ok {
				triggerMap = data
			}
		}

		// Get version
		versionPtr, err := nodeDetails.GetServiceVersion("trigger")
		if err == nil && versionPtr != nil {
			version = *versionPtr
		}
	}

	AppendGetTriggerDetails(ctx, responseData, triggerMap, &version)
	return nil
}

// AppendGetTriggerDetails appends trigger details to the response
func AppendGetTriggerDetails(ctx context.Context, data *DataToDevice, triggerData map[string]interface{}, version *int64) {
	rlog.Debug(ctx).Interface("triggerData", triggerData).Int64("version", *version).Msg("Appending trigger details to response")
	data.Event = append(data.Event, "getTriggerDetails")

	// Create response with version field
	response := make(map[string]interface{})
	for k, v := range triggerData {
		response[k] = v
	}
	response["version"] = version

	data.Data["getTriggerDetails"] = response
}

// SendTriggerDetails sends the trigger details to the device
func (n *Node) SendTriggerDetails(ctx context.Context) error {
	data := NewDataToDevice(n)
	rlog.Debug(ctx).Msgf("Preparing trigger details for device %s", n.ThingName)

	err := HandleGetTriggerDetails(ctx, n, data)
	if err != nil {
		rlog.Error(ctx).Err(err).Msgf("Failed to get trigger details for device %s", n.ThingName)
		return err
	}

	err = data.Send(ctx)
	if err != nil {
		rlog.Error(ctx).Err(err).Msgf("Failed to send trigger details to device %s", n.ThingName)
		return err
	}
	rlog.Debug(ctx).Msgf("Successfully sent trigger details to device %s", n.ThingName)
	return nil
}

// HandleGetTriggerVer handles the getTriggerVer event from the device
func HandleGetTriggerVer(ctx context.Context, n *Node, responseData *DataToDevice) error {
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, n)
	nodeDetailsDB := node_details_db.NewNodeDetailsDB(rmngCtx)

	version, err := nodeDetailsDB.GetServiceVersion(n.ThingName, "trigger")
	if err != nil {
		return rmerror.NewRMError(err, "failed to get trigger version")
	}

	AppendGetTriggerVer(responseData, version)
	return nil
}

// HandleGetTriggerDetails handles the getTriggerDetails event from the device
func HandleGetTriggerDetails(ctx context.Context, n *Node, responseData *DataToDevice) error {
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, n)
	nodeDetailsDB := node_details_db.NewNodeDetailsDB(rmngCtx)

	triggerData, err := nodeDetailsDB.GetServiceData(n.ThingName, "trigger")
	if err != nil {
		return rmerror.NewRMError(err, "failed to get trigger data")
	}

	var triggerMap map[string]interface{}
	if triggerData != nil {
		triggerMap, _ = triggerData.(map[string]interface{})
	}
	if triggerMap == nil {
		triggerMap = make(map[string]interface{})
	}

	// Get version
	version, err := nodeDetailsDB.GetServiceVersion(n.ThingName, "trigger")
	if err != nil {
		return rmerror.NewRMError(err, "failed to get trigger version")
	}

	AppendGetTriggerDetails(ctx, responseData, triggerMap, version)
	return nil
}

// clientOutputsLocation returns the public-assets bucket and key that
// upload_rmng_outputs.py writes the sanitized client outputs to. The bucket is
// account-scoped and always lives in us-east-1; the object is region-partitioned.
func clientOutputsLocation() (string, string) {
	bucket := "rmng-public-assets-" + awscommon.GetAccountId()
	key := awscommon.GetRmngRegion() + "/rmng-client-outputs.json"
	return bucket, key
}

func HandleGetServerConfig(ctx context.Context, n *Node, responseData *DataToDevice) error {
	bucket, key := clientOutputsLocation()

	content, err := s3util.GetObjectContent(ctx, bucket, key)
	if err != nil {
		return rmerror.NewRMError(err, "failed to fetch server config from S3")
	}

	outputs := make(map[string]interface{})
	if len(content) > 0 {
		if err := json.Unmarshal(content, &outputs); err != nil {
			return rmerror.NewRMError(err, "failed to parse server config")
		}
	}

	AppendGetServerConfig(responseData, outputs)
	return nil
}

func AppendGetServerConfig(data *DataToDevice, outputs map[string]interface{}) {
	data.Event = append(data.Event, "getServerConfig")
	data.Data["getServerConfig"] = outputs
}

func GetGroupIDFromShadowName(shadowName string) (string, []string, error) {
	groupID := ""
	subGroupIDs := []string{}
	if strings.HasPrefix(shadowName, "params-") {
		parts := strings.Split(shadowName, "-")
		if len(parts) > 1 {
			groupID = parts[1]
			for i := 2; i < len(parts); i++ {
				subGroupIDs = append(subGroupIDs, parts[i])
			}
		}
	}
	return groupID, subGroupIDs, nil
}

// CreateAdminGroupIfNotExists creates admin groups if they don't already exist.
// If parentGroupName is provided, validates hierarchy and creates parent/child as needed.
// If parentGroupName is empty, groups are created flat (no parent validation).
func CreateAdminGroupIfNotExists(ctx context.Context, groupNames []string, parentGroupName string) error {
	parentExists := true // no parent to create when flat
	if parentGroupName != "" {
		parentDesc, err := iotutil.DescribeThingGroup(ctx, parentGroupName)
		if err != nil {
			return rmerror.NewRMError(err, fmt.Sprintf("failed to describe parent group %s", parentGroupName))
		}
		parentExists = parentDesc != nil
	}

	for _, groupName := range groupNames {
		desc, err := iotutil.DescribeThingGroup(ctx, groupName)
		if err != nil {
			return rmerror.NewRMError(err, fmt.Sprintf("failed to describe thing group %s", groupName))
		}

		if desc != nil {
			// Group exists — if a parent was requested, validate hierarchy
			if parentGroupName != "" {
				existingParent := ""
				if desc.ThingGroupMetadata != nil && desc.ThingGroupMetadata.ParentGroupName != nil {
					existingParent = *desc.ThingGroupMetadata.ParentGroupName
				}
				if existingParent != parentGroupName {
					return rmerror.NewRMError(
						fmt.Errorf("group %s already exists under parent %q, expected parent %q", groupName, existingParent, parentGroupName),
						"admin group parent mismatch",
					)
				}
			}
			continue
		}

		// Group does not exist — ensure parent exists first (if specified)
		if parentGroupName != "" && !parentExists {
			err = iotutil.CreateThingGroup(ctx, parentGroupName, "")
			if err != nil {
				return rmerror.NewRMError(err, fmt.Sprintf("failed to create parent group %s", parentGroupName))
			}
			parentExists = true
		}

		err = iotutil.CreateThingGroup(ctx, groupName, parentGroupName)
		if err != nil {
			return rmerror.NewRMError(err, fmt.Sprintf("failed to create thing group %s", groupName))
		}
	}
	return nil
}

// AddToAdminGroups adds the node to a thing group
func (n *Node) AddToAdminGroups(rmngCtx *rmngctx.RmngContext, groupNames []string) error {
	if rmngCtx == nil {
		return rmerror.NewRMError(nil, "context is nil, cannot add to admin groups")
	}
	// Then add the thing to the group
	for _, groupName := range groupNames {
		if groupName == "" {
			continue
		}
		err := iotutil.AddThingToGroup(rmngCtx.Context, n.ThingName, groupName)
		if err != nil {
			return rmerror.NewRMError(err, fmt.Sprintf("failed to add thing to group %s", groupName))
		}
	}
	return nil
}

// ReadFromIndexedReportedShadow reads the 'reported' section of the indexed shadow (iparams)
func (n *Node) ReadFromIndexedReportedShadow(rmngCtx *rmngctx.RmngContext) (ReportedOrDesiredShadow, error) {
	if err := rmngCtx.IsAuthorized(utils.NodeReadShadow, n.ThingName); err != nil {
		return ReportedOrDesiredShadow{}, rmerror.NewRMError(err, "user does not have read access to the node")
	}

	shadowName, err := n.getIndexedShadowName(rmngCtx)
	if err != nil {
		return ReportedOrDesiredShadow{}, rmerror.NewRMError(err, "failed to get indexed shadow name")
	}

	iotClient := awscommon.GetIoTDataPlaneClient()
	getThingShadowInput := &iotdataplane.GetThingShadowInput{
		ThingName:  &n.ThingName,
		ShadowName: &shadowName,
	}

	output, err := iotClient.GetThingShadow(rmngCtx.Context, getThingShadowInput)
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "ResourceNotFoundException" {
			return ReportedOrDesiredShadow{}, nil
		}
		return ReportedOrDesiredShadow{}, rmerror.NewRMError(err, "failed to get indexed shadow")
	}

	var shadowState IoTNodeShadow
	err = json.Unmarshal(output.Payload, &shadowState)
	if err != nil {
		return ReportedOrDesiredShadow{}, rmerror.NewRMError(err, "failed to unmarshal indexed shadow state")
	}

	if shadowState.State == nil || shadowState.State.Reported == nil {
		return ReportedOrDesiredShadow{}, nil
	}

	return *shadowState.State.Reported, nil
}

// UpdateTagsMap writes tags from a map to the indexed shadow. Values can be strings or nil (to delete).
func (n *Node) UpdateTagsMap(rmngCtx *rmngctx.RmngContext, tags map[string]interface{}, tagsType TagType) error {
	if len(tags) == 0 {
		return nil
	}

	var shadow ReportedOrDesiredShadow
	tagsObj := &TagsObj{Tags: tags}

	switch tagsType {
	case TagTypeAdmin:
		shadow = ReportedOrDesiredShadow{Data: &Data{Admin: tagsObj}}
	case TagTypeDevice:
		shadow = ReportedOrDesiredShadow{Data: &Data{Device: tagsObj}}
	case TagTypeUser:
		shadow = ReportedOrDesiredShadow{Data: &Data{User: tagsObj}}
	default:
		return rmerror.NewRMError(fmt.Errorf("invalid tag type: %s", tagsType), "")
	}

	err := n.WriteToIndexedReportedShadow(rmngCtx, shadow)
	if err != nil {
		return rmerror.NewRMError(err, "failed to write tags to indexed shadow")
	}

	return nil
}

// AddTags parses "key:value" strings and writes them to the indexed shadow.
func (n *Node) AddTags(rmngCtx *rmngctx.RmngContext, tags []string, tagsType TagType) error {
	if len(tags) == 0 {
		return nil
	}

	tagsMap := make(map[string]interface{})
	for _, tagNameValue := range tags {
		parts := strings.SplitN(tagNameValue, ":", 2)
		if len(parts) < 2 {
			rlog.Error(rmngCtx).Msgf("Invalid tag format: %s", tagNameValue)
			continue
		}
		tagsMap[parts[0]] = parts[1]
	}

	return n.UpdateTagsMap(rmngCtx, tagsMap, tagsType)
}

func GetNodeIdFromCert(cert *x509.Certificate) string {
	return cert.Subject.CommonName
}

func ValidateCAAndGetNodeId(nodeCert string, caCert string) (string, error) {
	certBlock, _ := pem.Decode([]byte(nodeCert))
	if certBlock == nil {
		return "", rmerror.NewRMError(fmt.Errorf("failed to parse the uploaded certificate"), "")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return "", rmerror.NewRMError(fmt.Errorf("failed to parse certificate"), "")
	}

	if caCert != "" {
		caCertBlock, _ := pem.Decode([]byte(caCert))
		if caCertBlock == nil {
			return "", rmerror.NewRMError(fmt.Errorf("failed to parse the uploaded ca certificate"), "")
		}

		caCert, err := x509.ParseCertificate(caCertBlock.Bytes)
		if err != nil {
			return "", rmerror.NewRMError(err, "failed to parse CA certificate")
		}

		if err := certutil.VerifyCertificateChain(cert, caCert); err != nil {
			return "", rmerror.NewRMError(err, "Certificate chain verification failed")
		}
	}

	return GetNodeIdFromCert(cert), nil
}

// applyMetadata adds the node to admin groups and writes tags. Shared
// between the register and update flows — both apply the same idempotent
// calls with the same error wrapping. Each block is a no-op when its input
// slice is empty.
func (n *Node) applyMetadata(rmngCtx *rmngctx.RmngContext, adminGroupNames []string, tags []string) error {
	if len(adminGroupNames) > 0 {
		if err := n.AddToAdminGroups(rmngCtx, adminGroupNames); err != nil {
			return rmerror.NewRMError(err, "failed to add to admin groups")
		}
	}
	if len(tags) > 0 {
		if err := n.AddTags(rmngCtx, tags, TagTypeAdmin); err != nil {
			return rmerror.NewRMError(err, "failed to add tags")
		}
	}
	return nil
}

func RegisterNodeInRmng(rmngCtx *rmngctx.RmngContext, cert string, caCert string, adminGroupNames []string, tags []string, adminId string, capabilities []string) (string, error) {
	if rmngCtx == nil {
		return "", rmerror.NewRMError(nil, "context is nil, cannot register node")
	}
	nodeId, err := ValidateCAAndGetNodeId(cert, caCert)
	if err != nil {
		return "", rmerror.NewRMError(err, "failed to validate ca or cert and get node id")
	}
	rmngCtx.EnrichLogger("nid", nodeId)

	n := NewNode(nodeId)

	nodeType, err := n.registerInIotCore(rmngCtx, cert, capabilities)
	if err != nil {
		return nodeId, rmerror.NewRMError(err, "failed to register")
	}

	// AddNode's conditional put is what detects a re-registration. Other write
	// failures stay non-fatal: the row is opportunistic and downstream lookups
	// treat its absence like any other missing row.
	if err := node_details_db.NewNodeDetailsDB(rmngCtx).
		AddNode(node_details_db.NodeDetailsEntry{NodeID: nodeId, RegTs: time.Now().Unix(), AdminId: adminId, NodeType: nodeType}); err != nil {
		if db.IsConditionalCheckFailedException(err) {
			return nodeId, ErrNodeAlreadyRegistered
		}
		rlog.Warn(rmngCtx).Err(err).Msg("failed to record node details row")
	}

	if err := n.applyMetadata(rmngCtx, adminGroupNames, tags); err != nil {
		return nodeId, err
	}

	rlog.Info(rmngCtx).Msgf("Node registered successfully")
	return nodeId, nil
}

// ErrNodeNotFound is returned by UpdateNodeInRmng when the target node has no
// row in node_details. Callers (the bulk update container) record this as a
// failure with a clean reason rather than treating it as a transport error.
var ErrNodeNotFound = errors.New("node not found")

// ErrNodeAlreadyRegistered is returned by RegisterNodeInRmng when the node is
// already registered. Handlers map it to HTTP 409; use UpdateNodeInRmng to
// modify an existing node.
var ErrNodeAlreadyRegistered = errors.New("node already registered")

// UpdateNodeInRmng applies metadata updates (admin groups, tags) and
// optionally a cert update to an existing registered node.
//
// newCertPEM == "" means the cert binding is left untouched. When non-empty,
// the new cert is registered (idempotent — recovers ARN if already
// registered), attached to the Thing, given the default policy, and any
// previously-attached certs are detached and deactivated. This is the
// "operator correction" use case: a wrong PEM was registered and needs to be
// replaced with the right one. Note: cloud-side only — nothing here pushes
// the new cert to the device, which is what distinguishes this from key
// rotation.
//
// Existence is checked against node_details (the project's source of truth
// for "is this node ours?"); a missing row is reported as ErrNodeNotFound so
// the caller can record it cleanly.
//
// All operations are idempotent: re-running this for the same nodeID with
// the same inputs is a no-op — making update-jobs safe to retry by the same
// failed-nodes-export workflow as registration.
func UpdateNodeInRmng(rmngCtx *rmngctx.RmngContext, nodeID string, newCertPEM string, adminGroupNames []string, tags []string, capabilities []string) error {
	if rmngCtx == nil {
		return rmerror.NewRMError(nil, "context is nil, cannot update node")
	}
	rmngCtx.EnrichLogger("nid", nodeID)

	details, err := node_details_db.NewNodeDetailsDB(rmngCtx).GetNodeDetails(nodeID)
	if err != nil {
		return rmerror.NewRMError(err, "failed to look up node")
	}
	if details == nil {
		return ErrNodeNotFound
	}

	n := NewNode(nodeID)

	if newCertPEM != "" {
		if err := replaceNodeCert(rmngCtx, n, newCertPEM, capabilities); err != nil {
			return rmerror.NewRMError(err, "failed to update node certificate")
		}
	}

	if err := n.applyMetadata(rmngCtx, adminGroupNames, tags); err != nil {
		return err
	}

	return nil
}

// replaceNodeCert replaces the cert binding for an existing Thing. The new
// cert is registered (idempotent), attached, given the default policy, and
// then any previously-attached certs are detached and deactivated.
//
// Replace-and-deactivate (rather than "leave the old cert active alongside")
// is the right semantic for the operator-correction use case — leaving the
// wrong cert active alongside a new one would defeat the operator's intent.
//
// If the new cert is already the only one attached, this function is a clean
// no-op: the registration / attach / policy steps are absorbed as already-
// exists, the detach loop skips the new cert.
func replaceNodeCert(rmngCtx *rmngctx.RmngContext, n *Node, newCertPEM string, capabilities []string) error {
	if _, err := ValidateCAAndGetNodeId(newCertPEM, ""); err != nil {
		return rmerror.NewRMError(err, "invalid cert PEM")
	}

	// Compute the new cert's ID locally so the detach loop can recognize
	// "this one is already the new cert" without an extra DescribeCertificate.
	newCertID, err := iotutil.GetCertIDFromPEM(newCertPEM)
	if err != nil {
		return rmerror.NewRMError(err, "failed to compute cert ID for new cert")
	}

	// Snapshot the existing cert principals BEFORE we attach the new one,
	// so the detach loop knows exactly what to clean up.
	existingCerts, err := iotutil.GetThingCertificates(rmngCtx.Context, n.ThingName)
	if err != nil {
		return rmerror.NewRMError(err, "failed to list existing thing certificates")
	}

	// Register the new cert. registerCertOrRecoverARN absorbs the expected
	// ResourceAlreadyExistsException on retry of a partially-completed update
	// (or when the operator submits the cert that's already registered for
	// this node) and recovers the canonical ARN.
	newCertArn, _, err := registerCertOrRecoverARN(rmngCtx.Context, newCertPEM)
	if err != nil {
		return err
	}

	// Attach the new cert and the default policy. Both wrappers are idempotent.
	if err := iotutil.AttachCertificateToThing(rmngCtx.Context, n.ThingName, newCertArn); err != nil {
		return rmerror.NewRMError(err, "failed to attach new cert to thing")
	}
	// Attach the base policy plus every capability-gated policy implied by
	// `capabilities`, and fire the same registration hook a first-time
	// registration fires.
	//
	// Callers that pass nil get the base policy only, which is correct for an
	// operator correcting a wrong PEM on a node whose capabilities they are
	// not restating. Assisted claiming passes the capabilities from the claim
	// request, because there a re-claim after a factory erase is the normal
	// path, not a rare correction: silently dropping the capability-gated
	// policies would leave the device to fail later at the credential
	// provider with AccessDenied rather than here, visibly.
	if err := iotutil.AttachDefaultPolicy(rmngCtx.Context, newCertArn, capabilities); err != nil {
		return rmerror.NewRMError(err, "failed to attach default policy to new cert")
	}
	// The node_type the hook returns is ignored on a cert replacement: this
	// path does not touch the node_details row, whose node_type was set at
	// first registration and does not change on a cert swap.
	if _, err := nodelifecycle.OnNodeRegister(rmngCtx, n.ThingName, capabilities, newCertArn); err != nil {
		return rmerror.NewRMError(err, "node register hook failed for replacement cert")
	}

	// Detach + deactivate any cert that isn't the new one. On a "same cert"
	// re-run the loop is empty after the skip; on a real replacement the old
	// cert ends up detached and INACTIVE.
	for _, cert := range existingCerts {
		if cert == nil || cert.CertificateId == nil {
			continue
		}
		oldCertID := *cert.CertificateId
		if oldCertID == newCertID {
			continue
		}
		oldCertArn := ""
		if cert.CertificateArn != nil {
			oldCertArn = *cert.CertificateArn
		}
		if oldCertArn != "" {
			if err := iotutil.DetachThingPrincipal(rmngCtx.Context, n.ThingName, oldCertArn); err != nil {
				return rmerror.NewRMError(err, fmt.Sprintf("failed to detach old cert %s", oldCertID))
			}
		}
		if err := iotutil.UpdateCertificate(rmngCtx.Context, oldCertID, iot_types.CertificateStatusInactive); err != nil {
			return rmerror.NewRMError(err, fmt.Sprintf("failed to deactivate old cert %s", oldCertID))
		}
	}

	return nil
}

// External contract — json tags must remain camelCase to match the AWS IoT Lifecycle Events payload.
type PresenceEvent struct {
	ClientID                  string `json:"clientId"`
	ThingName                 string `json:"thingName,omitempty"`
	EventType                 string `json:"eventType"`
	Timestamp                 int64  `json:"timestamp"`
	SessionID                 string `json:"sessionIdentifier"`
	PrincipalID               string `json:"principalIdentifier"`
	IPAddress                 string `json:"ipAddress"`
	VersionNumber             int    `json:"versionNumber"`
	ClientInitiatedDisconnect bool   `json:"clientInitiatedDisconnect,omitempty"`
	DisconnectReason          string `json:"disconnectReason,omitempty"`
}

// SessionMatches reports whether the stored session entry matches the
// incoming event. Both fields are checked because MQTT persistent
// sessions keep the same SessionID across reconnects — only
// VersionNumber increments.
func SessionMatches(current nodes_online_db.NodesOnlineEntry, event PresenceEvent) bool {
	return current.SessionID == event.SessionID && current.VersionNumber == event.VersionNumber
}

// BuildDisconnectShadow constructs the reported shadow payload for a
// node that has gone offline, including disconnect metadata when present.
func BuildDisconnectShadow(event PresenceEvent) ReportedOrDesiredShadow {
	shadow := ReportedOrDesiredShadow{Online: utils.Ptr(false)}
	if event.DisconnectReason != "" || event.Timestamp != 0 {
		shadow.DisconnectInfo = &DisconnectInfo{
			LastDisconnectReason:    event.DisconnectReason,
			LastDisconnectTimestamp: event.Timestamp,
		}
	}
	return shadow
}
