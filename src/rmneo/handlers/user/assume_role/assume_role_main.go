// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/principal"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngrequest"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	sts_types "github.com/aws/aws-sdk-go-v2/service/sts/types"
)

type Request struct {
	Tags map[string]string `json:"tags"`
	// Services selects the per-node services to grant; required on the per-node route.
	Services []string `json:"services,omitempty"`
}

type Response struct {
	AccessKey    string `json:"access_key"`
	SecretKey    string `json:"secret_key"`
	SessionToken string `json:"session_token"`
	Expiration   *int   `json:"expiration"`
}

// maxSessionPolicyChars is STS's cap on the AssumeRole Policy parameter. The AWS API model does not declare it, so nothing catches an overrun client-side; live STS rejects a 2238-char document before evaluating the trust policy.
const maxSessionPolicyChars = 2048

const (
	iamPolicyVersion = "2012-10-17"
	serviceS3        = "s3"
	serviceKVS       = "kvs"
	noNodeAccessMsg  = "User does not have access to this node"
	noGroupAccessMsg = "User does not have access to this group"
)

var supportedServices = []string{serviceS3, serviceKVS}

// route is the path the request arrived on: no ids for every group the caller can reach, groupID (and optionally subGroupID) for one group or subgroup, groupID and nodeID for per-node services.
type route struct {
	groupID    string
	subGroupID string
	nodeID     string
}

func (r route) isPerNode() bool {
	return r.nodeID != ""
}

type iamPolicyDocument struct {
	Version   string         `json:"Version"`
	Statement []iamStatement `json:"Statement"`
}

// iamStatement fields are `any` because IAM accepts either a scalar or a list for Action, Resource and Condition.
type iamStatement struct {
	Effect    string `json:"Effect"`
	Action    any    `json:"Action"`
	Resource  any    `json:"Resource"`
	Condition any    `json:"Condition,omitempty"`
}

func handleRequest(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	rctx := principal.FromAPIRequest(ctx, request)
	if rctx == nil {
		rlog.Error(ctx).Str("auth_provider", request.RequestContext.Identity.CognitoAuthenticationProvider).Str("identity_id", request.RequestContext.Identity.CognitoIdentityID).Msg("Failed to create request context")
		return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus("Unauthorized")), nil
	}

	r := route{
		groupID:    request.PathParameters["groupId"],
		subGroupID: request.PathParameters["subGroupId"],
		nodeID:     request.PathParameters["nodeId"],
	}
	creds, err := issueCredentials(rctx, request, r)
	if err != nil {
		rlog.Error(rctx).Err(err).Str("group_id", r.groupID).Str("subgroup_id", r.subGroupID).Str("node_id", r.nodeID).Msg("Unable to issue credentials")
		return utils.APIErrorRespJSON(err), nil
	}
	return utils.APIGwRespJSON(http.StatusOK, creds), nil
}

func issueCredentials(rctx *rmngctx.RmngContext, request events.APIGatewayProxyRequest, r route) (Response, error) {
	var req Request
	if err := rmngrequest.ExtractRequestStruct(request, &req); err != nil {
		return Response{}, rmerror.NewRMError(err, "failed to read request")
	}
	if err := validateRequest(req, r); err != nil {
		return Response{}, rmerror.NewRMError(err, "invalid request for this route").WithClientStatus(http.StatusBadRequest, err.Error())
	}

	roleARN := os.Getenv("IOT_USER_ROLE_ARN")
	if roleARN == "" {
		return Response{}, rmerror.NewRMError(nil, "IOT_USER_ROLE_ARN is not set")
	}

	stsClient, err := newSTSClient(rctx)
	if err != nil {
		return Response{}, err
	}
	identity, err := stsClient.GetCallerIdentity(rctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return Response{}, rmerror.NewRMError(err, "Error getting caller identity")
	}

	policy, err := buildSessionPolicy(rctx, aws.ToString(identity.Account), req, r)
	if err != nil {
		return Response{}, err
	}
	return assumeIoTRole(rctx, stsClient, roleARN, policy, req.Tags)
}

func validateRequest(req Request, r route) error {
	if !r.isPerNode() {
		if len(req.Services) > 0 {
			return rmerror.NewRMError(nil, "services requires the /v1/groups/{group_id}/nodes/{node_id}/assumed-roles route")
		}
		return nil
	}
	if r.groupID == "" {
		return rmerror.NewRMError(nil, "group_id and node_id are required")
	}
	if len(req.Services) == 0 {
		return rmerror.NewRMError(nil, "services is required")
	}
	for _, service := range req.Services {
		if !slices.Contains(supportedServices, service) {
			return rmerror.NewRMError(nil, fmt.Sprintf("unsupported service: %s", service))
		}
	}
	return nil
}

func newSTSClient(ctx context.Context) (awscommon.STSClientInterface, error) {
	if client := awscommon.GetSTSClient(); client != nil {
		return client, nil
	}
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, rmerror.NewRMError(err, "Error loading Lambda credentials")
	}
	return sts.NewFromConfig(cfg), nil
}

func buildSessionPolicy(rctx *rmngctx.RmngContext, accountID string, req Request, r route) (string, error) {
	switch {
	case r.isPerNode():
		return nodePolicy(rctx, accountID, r, req.Services)
	case r.groupID == "":
		return allGroupsPolicy(rctx, accountID)
	default:
		return scopedPolicy(rctx, accountID, r)
	}
}

func nodePolicy(rctx *rmngctx.RmngContext, accountID string, r route, services []string) (string, error) {
	if err := user.LoadNodePermissions(rctx, r.groupID, r.nodeID); err != nil {
		return "", rmerror.NewRMError(err, "failed to authorize node access")
	}
	return servicesPolicy(accountID, r.nodeID, services)
}

func allGroupsPolicy(rctx *rmngctx.RmngContext, accountID string) (string, error) {
	groups, err := group.ListGroupsForUser(rctx, false)
	if err != nil {
		return "", rmerror.NewRMError(err, "Error fetching user groups")
	}
	// STS rejects a statement with an empty Resource list, so a caller with no group gets no MQTT credentials until it joins or creates one.
	if !coversAny(groups...) {
		return "", rmerror.NewRMError(nil, "User has no groups").WithClientStatus(http.StatusConflict, "User has no groups")
	}
	policy, err := mqttPolicy(accountID, callerClientIDs(rctx), groups)
	if err != nil {
		return "", err
	}
	return policy, checkPolicySize(policy, http.StatusInternalServerError, "Unable to issue credentials: too many accessible groups to encode in a session policy")
}

// scopedPolicy covers one group, or one subgroup when the path names it. Only a subentity member asking for the whole group can overrun, and the per-subgroup route always fits, so the overrun points the client there.
func scopedPolicy(rctx *rmngctx.RmngContext, accountID string, r route) (string, error) {
	groupDB := group_db.NewGroupDB(rctx)
	g, err := callerGroup(rctx, groupDB, r.groupID)
	if err != nil {
		return "", err
	}
	if g, err = g.NarrowToSubGroup(rctx, r.subGroupID); err != nil {
		return "", rmerror.NewRMError(err, "failed to narrow to subgroup")
	}
	policy, err := mqttPolicy(accountID, callerClientIDs(rctx), []group.Group{g})
	if err != nil {
		return "", err
	}
	return policy, checkPolicySize(policy, http.StatusUnprocessableEntity, "Unable to issue credentials: too many shared subgroups to encode in a session policy; request per subgroup")
}

// checkPolicySize rejects an oversized policy here, because STS fails it with a ValidationError that reads like a transient fault.
func checkPolicySize(policy string, status int, message string) error {
	if len(policy) <= maxSessionPolicyChars {
		return nil
	}
	return rmerror.NewRMError(nil, fmt.Sprintf("session policy is %d chars, STS allows %d", len(policy), maxSessionPolicyChars)).WithClientStatus(status, message)
}

func assumeIoTRole(ctx context.Context, stsClient awscommon.STSClientInterface, roleARN, policy string, tags map[string]string) (Response, error) {
	stsTags := make([]sts_types.Tag, 0, len(tags))
	for key, value := range tags {
		stsTags = append(stsTags, sts_types.Tag{Key: aws.String(key), Value: aws.String(value)})
	}

	out, err := stsClient.AssumeRole(ctx, &sts.AssumeRoleInput{
		RoleArn:         aws.String(roleARN),
		RoleSessionName: aws.String("AssumeIoTRoleSession"),
		Tags:            stsTags,
		Policy:          aws.String(policy),
	})
	if err != nil {
		return Response{}, rmerror.NewRMError(err, "Error assuming role")
	}

	creds := out.Credentials
	resp := Response{
		AccessKey:    aws.ToString(creds.AccessKeyId),
		SecretKey:    aws.ToString(creds.SecretAccessKey),
		SessionToken: aws.ToString(creds.SessionToken),
	}
	if creds.Expiration != nil {
		expiration := int(creds.Expiration.Unix())
		resp.Expiration = &expiration
	}
	return resp, nil
}

// callerGroup is the caller's access on groupID: any existing group for an admin, otherwise only the caller's own grant.
func callerGroup(rctx *rmngctx.RmngContext, groupDB *group_db.GroupDB, groupID string) (group.Group, error) {
	if isAdmin(rctx) {
		if _, err := groupDB.GetGroupByID(groupID); err != nil {
			return group.Group{}, rmerror.NewRMError(err, "failed to load group for admin")
		}
		return group.Group{GroupID: groupID, AccessType: utils.GroupPrimaryAccess}, nil
	}

	g, err := group.GetUserGroupSubgroupAccessWithData(rctx, groupID)
	if err != nil {
		return group.Group{}, rmerror.NewRMError(err, "failed to read caller's grant")
	}
	return g, nil
}

func coversAny(groups ...group.Group) bool {
	return slices.ContainsFunc(groups, func(g group.Group) bool {
		return g.HasWholeGroupAccess() || len(g.SubGroups) > 0
	})
}

func isAdmin(rctx *rmngctx.RmngContext) bool {
	caller, ok := rctx.GetAccessor().(*user.User)
	return ok && caller.IsAdmin(rctx)
}

// callerClientIDs are the caller's own ids that may appear in its MQTT client id, so scoping iot:Connect to them confines the session to the caller.
func callerClientIDs(rctx *rmngctx.RmngContext) []string {
	caller, ok := rctx.GetAccessor().(*user.User)
	if !ok {
		return nil
	}
	return slices.DeleteFunc([]string{caller.UserInfo.Email, caller.UserInfo.PhoneNumber}, func(id string) bool { return id == "" })
}

// mqttPolicy builds the IoT-only session policy: whole-group wildcards for each whole group, per-subgroup ARNs for each subentity group.
func mqttPolicy(accountID string, clientIDs []string, groups []group.Group) (string, error) {
	region := os.Getenv("AWS_REGION")
	iotARN := fmt.Sprintf("arn:aws:iot:%s:%s:", region, accountID)

	// Only per-session ids "user:<email|phone>:<session>" are granted, never the bare "user:<id>": a user's own sessions never collide and no client can claim the canonical id. ':' cannot appear in an email or phone number, so the wildcard matches only the caller's sessions.
	connectResources := make([]string, 0, len(clientIDs))
	for _, clientID := range clientIDs {
		connectResources = append(connectResources, iotARN+"client/user:"+clientID+":*")
	}

	publishResources := []string{}
	shadowResources := []string{}
	for _, g := range groups {
		if !g.HasWholeGroupAccess() {
			continue
		}
		publishResources = append(publishResources,
			iotARN+"topic/rainmaker/nodes/*/user/params-"+g.GroupID+"*/*",
			iotARN+"topic/rainmaker/nodes/groups/"+g.GroupID+"/control",
			iotARN+"topic/rainmaker/nodes/groups/"+g.GroupID+"/subgroups/*/control",
		)
		shadowResources = append(shadowResources, iotARN+"*/$aws/things/*/shadow/name/params-"+g.GroupID+"*/*")
	}
	subGroupScoped := slices.DeleteFunc(slices.Clone(groups), func(g group.Group) bool { return g.HasWholeGroupAccess() })
	slices.SortFunc(subGroupScoped, func(a, b group.Group) int { return strings.Compare(a.GroupID, b.GroupID) })
	for _, g := range subGroupScoped {
		for _, sg := range g.SubGroups {
			publishResources = append(publishResources,
				iotARN+"topic/rainmaker/nodes/*/user/params-"+g.GroupID+"*"+sg.SubGroupID+"*/*",
				iotARN+"topic/rainmaker/nodes/groups/"+g.GroupID+"/subgroups/"+sg.SubGroupID+"/control",
			)
			shadowResources = append(shadowResources, iotARN+"*/$aws/things/*/shadow/name/params-"+g.GroupID+"*"+sg.SubGroupID+"*/*")
		}
	}

	return marshalPolicy([]iamStatement{
		{Effect: "Allow", Action: []string{"iot:Publish"}, Resource: publishResources},
		{Effect: "Allow", Action: []string{"iot:Subscribe", "iot:Receive", "iot:Publish"}, Resource: shadowResources},
		{Effect: "Allow", Action: []string{"cognito-identity:GetCredentialsForIdentity"}, Resource: []string{fmt.Sprintf("arn:aws:cognito-identity:%s:%s::identitypool/*:identity/${cognito-identity.amazonaws.com:sub}", region, accountID)}},
		{Effect: "Allow", Action: []string{"iot:Connect"}, Resource: connectResources},
	})
}

// servicesPolicy builds the S3/KVS-only session policy for one node; it carries no IoT statements.
func servicesPolicy(accountID, nodeID string, services []string) (string, error) {
	statements := []iamStatement{}

	if bucket := os.Getenv("FILES_BUCKET_NAME"); bucket != "" && slices.Contains(services, serviceS3) {
		statements = append(statements,
			iamStatement{
				Effect:    "Allow",
				Action:    "s3:ListBucket",
				Resource:  "arn:aws:s3:::" + bucket,
				Condition: map[string]any{"StringLike": map[string]any{"s3:prefix": []string{"node-data/" + nodeID + "/*"}}},
			},
			iamStatement{
				Effect:   "Allow",
				Action:   []string{"s3:GetObject", "s3:DeleteObject"},
				Resource: []string{"arn:aws:s3:::" + bucket + "/node-data/" + nodeID + "/*"},
			},
		)
	}

	if slices.Contains(services, serviceKVS) {
		statements = append(statements, iamStatement{
			Effect: "Allow",
			Action: []string{
				"kinesisvideo:ConnectAsViewer",
				"kinesisvideo:GetSignalingChannelEndpoint",
				"kinesisvideo:DescribeSignalingChannel",
				"kinesisvideo:GetIceServerConfig",
			},
			Resource: []string{fmt.Sprintf("arn:aws:kinesisvideo:%s:%s:channel/rmng-v1-%s/*", os.Getenv("AWS_REGION"), accountID, nodeID)},
		})
	}

	return marshalPolicy(statements)
}

func marshalPolicy(statements []iamStatement) (string, error) {
	policy, err := json.Marshal(iamPolicyDocument{Version: iamPolicyVersion, Statement: statements})
	if err != nil {
		return "", rmerror.NewRMError(err, "Error building policy document")
	}
	return string(policy), nil
}

func main() {
	lambda.Start(handleRequest)
}
