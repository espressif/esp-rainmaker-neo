// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package principal

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/espressif/esp-cloud-common/go/rbac/rbac"
	"github.com/espressif/esp-rainmaker-neo/src/awsutils/iotutil"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
)

// DeviceActor is the RBAC principal for a device calling with its IoT credential-provider credentials.
type DeviceActor struct {
	NodeID string
	// GroupID is the group the node belonged to when the context was built; empty for a node in no group.
	GroupID     string
	Permissions rbac.EntityPermissions
}

func (d *DeviceActor) GetID() string { return d.NodeID }

func (d *DeviceActor) GetPermissions() *rbac.EntityPermissions { return &d.Permissions }

// ScopedNodeID and ScopedGroupID satisfy rmngctx.NodeScopedAccessor, so package group can resolve a device without importing this package.
func (d *DeviceActor) ScopedNodeID() string  { return d.NodeID }
func (d *DeviceActor) ScopedGroupID() string { return d.GroupID }

// The IoT credential provider names the role session after the caller's certificate ID, the lowercase hex SHA-256 of the certificate.
var certificateIDPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// A certificate stays attached to the thing it was registered with, so the lookup is cached for the container's life.
var thingByCertificateID sync.Map

// callerCertificate returns the IoT certificate behind a credential-provider session: "arn:aws:sts::111122223333:assumed-role/<role>/<cert id>" -> ("<cert id>", "arn:aws:iot:<AWS_REGION>:111122223333:cert/<cert id>"). Any other caller returns ("", "").
func callerCertificate(userArn string) (certID, certArn string) {
	parsed, err := arn.Parse(userArn)
	if err != nil || parsed.Service != "sts" {
		return "", ""
	}
	parts := strings.Split(parsed.Resource, "/")
	if len(parts) != 3 || parts[0] != "assumed-role" || !certificateIDPattern.MatchString(parts[2]) {
		return "", ""
	}
	return parts[2], fmt.Sprintf("arn:%s:iot:%s:%s:cert/%s", parsed.Partition, os.Getenv("AWS_REGION"), parsed.AccountID, parts[2])
}

func nodeForCertificate(ctx context.Context, certID, certArn string) (string, error) {
	if nodeID, ok := thingByCertificateID.Load(certID); ok {
		return nodeID.(string), nil
	}
	nodeID, err := iotutil.GetThingForCertificate(ctx, certArn)
	if err != nil {
		return "", rmerror.NewRMError(err, "failed to resolve the calling device")
	}
	thingByCertificateID.Store(certID, nodeID)
	return nodeID, nil
}

func fromDeviceIdentity(ctx context.Context, identity events.APIGatewayRequestIdentity) *rmngctx.RmngContext {
	certID, certArn := callerCertificate(identity.UserArn)
	if certID == "" {
		rlog.Warn(ctx).Str("caller", identity.UserArn).Msg("IAM caller is not an IoT credential-provider session")
		return nil
	}
	nodeID, err := nodeForCertificate(ctx, certID, certArn)
	if err != nil {
		rlog.Warn(ctx).Err(err).Str("certId", certID).Msg("no device for the calling certificate")
		return nil
	}
	return newDeviceContext(ctx, nodeID)
}

// newDeviceContext grants group read on the group the node belongs to now, so a moved node follows the move on its next request, as MQTT does via the group_id attribute. Node access comes later from loading the group's nodes; only the device role's GET-only execute-api grant keeps it read-only (controller.md §3.5).
func newDeviceContext(ctx context.Context, nodeID string) *rmngctx.RmngContext {
	actor := &DeviceActor{NodeID: nodeID}
	// The membership lookup needs read on the device's own node.
	actor.Permissions.SetAllow(utils.NodeGet.String(), nodeID)
	rmngCtx := rmngctx.NewRmngContextWithNode(ctx, actor, nodeID)

	membership, err := group.GetNodesGroup(rmngCtx, nodeID)
	if err != nil {
		rlog.Error(rmngCtx).Err(err).Msg("failed to resolve device group membership")
		return nil
	}
	if membership.Group == "" {
		rlog.Warn(rmngCtx).Msg("device belongs to no group")
		return nil
	}
	actor.GroupID = membership.Group
	if err := rmngCtx.SetAllowMultiple(utils.GetGroupPermissions(utils.GroupDeviceAccess), actor.GroupID); err != nil {
		return nil
	}
	return rmngCtx
}
