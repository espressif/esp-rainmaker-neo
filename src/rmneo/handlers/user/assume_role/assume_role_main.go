// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/user_group_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"net/http"
	"os"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
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
	// Services selects which per-node services to include. Required on the
	// per-node route. Supported values: "s3", "kvs".
	Services []string `json:"services,omitempty"`
}

type Response struct {
	AccessKey    string `json:"access_key"`
	SecretKey    string `json:"secret_key"`
	SessionToken string `json:"session_token"`
	Expiration   *int   `json:"expiration"`
}

// maxSessionPolicyChars is STS's hard cap on the AssumeRole Policy parameter.
// Verified against live STS: a 2238-char document is rejected with
// "at 'policy' failed to satisfy constraint: Member must have length less than
// or equal to 2048" — before the trust policy is even evaluated. The AWS API
// model does not declare this max, so nothing catches it client-side.
const maxSessionPolicyChars = 2048

// supportedGroupsPerSession is the largest number of full-access groups whose
// ARNs still fit the budget: ~343 chars per group on ~503 of fixed overhead.
// A caller above this cannot be issued MQTT credentials at all.
const supportedGroupsPerSession = 4

// groupAccess is the set of groups (full access) and per-group subgroups the
// caller may reach, used to scope the MQTT session policy.
type groupAccess struct {
	Groups    []string
	Subgroups map[string][]string
}

// mqttClientIdentity holds the caller's own identifiers that may appear in its
// MQTT client id ("user:<id>"). Every value is the caller's own, so scoping
// iot:Connect to these keeps the session confined to the caller.
type mqttClientIdentity struct {
	Email string
	Phone string
}

// iamPolicyDocument / iamStatement model an inline session policy so it can be
// built with typed fields and json.Marshal instead of a map[string]interface{}
// mutated through unchecked assertions. Action/Resource/Condition are `any`
// because IAM accepts either a scalar or a list in those positions.
type iamPolicyDocument struct {
	Version   string         `json:"Version"`
	Statement []iamStatement `json:"Statement"`
}

type iamStatement struct {
	Effect    string `json:"Effect"`
	Action    any    `json:"Action"`
	Resource  any    `json:"Resource"`
	Condition any    `json:"Condition,omitempty"`
}

func handleRequest(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	var req Request
	if err := rmngrequest.ExtractRequestStruct(request, &req); err != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid request body")), nil
	}

	pathGroupID := request.PathParameters["groupId"]
	pathSubGroupID := request.PathParameters["subGroupId"]
	pathNodeID := request.PathParameters["nodeId"]
	servicesMode := pathNodeID != ""

	if errResp := validateRequest(req, servicesMode, pathGroupID, pathNodeID); errResp != nil {
		return *errResp, nil
	}

	rmng_context := user.NewContextWithAPIRequest(ctx, request)

	lambdaCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		rlog.Error(rmng_context).Err(err).Msg("Error loading default config")
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Error loading Lambda credentials")), nil
	}

	stsClient := awscommon.GetSTSClient()
	if stsClient == nil {
		stsClient = sts.NewFromConfig(lambdaCfg)
	}

	callerIdentity, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		rlog.Error(rmng_context).Err(err).Msg("Error getting caller identity")
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Error getting caller identity")), nil
	}

	// Scope iot:Connect to the caller's own per-session client ids only, so a
	// user cannot connect as another id and force its holder off (MQTT enforces
	// unique client ids per endpoint). Clients connect as
	// "user:<email|phone>:<session>"; the bare form is not granted (see
	// createMQTTPolicyDocument), so every session is isolated.
	var clientIdentity mqttClientIdentity
	if accessor, ok := rmng_context.GetAccessor().(*user.User); ok && accessor != nil {
		clientIdentity = mqttClientIdentity{
			Email: accessor.UserInfo.Email,
			Phone: accessor.UserInfo.PhoneNumber,
		}
	}

	var policyDocument string
	if servicesMode {
		errResp := authorizeNodeAccess(ctx, rmng_context, pathGroupID, pathNodeID)
		if errResp != nil {
			return *errResp, nil
		}
		policyDocument, err = createServicesPolicyDocument(*callerIdentity.Account, pathNodeID, req.Services)
	} else {
		access, errResp := resolveAssumeRoleGroupAccess(ctx, rmng_context, request, pathGroupID, pathSubGroupID)
		if errResp != nil {
			return *errResp, nil
		}
		policyDocument, err = createMQTTPolicyDocument(*callerIdentity.Account, clientIdentity, access)
	}
	if err != nil {
		rlog.Error(rmng_context).Err(err).Msg("Error building session policy document")
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Error building policy document")), nil
	}

	// The document grows ~343 chars per full-access group, so a caller with 5+
	// groups overruns the STS cap and AssumeRole fails. Reject here instead:
	// STS's ValidationError is indistinguishable from a transient fault in the
	// logs, which is why this went unnoticed. See the MR for the structural fix.
	if len(policyDocument) > maxSessionPolicyChars {
		rlog.Error(rmng_context).
			Int("policy_chars", len(policyDocument)).
			Int("limit", maxSessionPolicyChars).
			Msg("Session policy exceeds the STS size limit; caller has too many accessible groups to encode")
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Unable to issue credentials: too many accessible groups to encode in a session policy")), nil
	}

	tags := make([]sts_types.Tag, 0, len(req.Tags))
	for key, value := range req.Tags {
		tags = append(tags, sts_types.Tag{Key: aws.String(key), Value: aws.String(value)})
	}

	iotUserRoleArn := os.Getenv("IOT_USER_ROLE_ARN")
	if iotUserRoleArn == "" {
		rlog.Error(rmng_context).Msg("IOT_USER_ROLE_ARN environment variable is not set")
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Internal server error")), nil
	}

	assumeRoleInput := &sts.AssumeRoleInput{
		RoleArn:         aws.String(iotUserRoleArn),
		RoleSessionName: aws.String("AssumeIoTRoleSession"),
		Tags:            tags,
		Policy:          aws.String(policyDocument),
	}

	assumeRoleOutput, err := stsClient.AssumeRole(ctx, assumeRoleInput)
	if err != nil {
		rlog.Error(rmng_context).Err(err).Msg("Error assuming role")
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Error assuming role")), nil
	}

	var expirationInt *int
	if assumeRoleOutput.Credentials.Expiration != nil {
		expirationUnix := int(assumeRoleOutput.Credentials.Expiration.Unix())
		expirationInt = &expirationUnix
	}
	resp := Response{
		AccessKey:    *assumeRoleOutput.Credentials.AccessKeyId,
		SecretKey:    *assumeRoleOutput.Credentials.SecretAccessKey,
		SessionToken: *assumeRoleOutput.Credentials.SessionToken,
		Expiration:   expirationInt,
	}

	return utils.APIGwRespJSON(http.StatusOK, resp), nil
}

func validateRequest(req Request, servicesMode bool, pathGroupID, pathNodeID string) *events.APIGatewayProxyResponse {
	if !servicesMode {
		if len(req.Services) > 0 {
			resp := utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("services requires the /v1/groups/{group_id}/nodes/{node_id}/assumed-roles route"))
			return &resp
		}
		return nil
	}
	if pathGroupID == "" || pathNodeID == "" {
		resp := utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("group_id and node_id are required"))
		return &resp
	}
	if len(req.Services) == 0 {
		resp := utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("services is required"))
		return &resp
	}
	for _, s := range req.Services {
		if s != "s3" && s != "kvs" {
			resp := utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus(fmt.Sprintf("unsupported service: %s", s)))
			return &resp
		}
	}
	return nil
}

// authorizeNodeAccess verifies the requesting user (or admin) has access to
// nodeID via groupID. Lookups are O(1):
//   - one GetItem on group_device_mapping to confirm node belongs to group,
//   - one point Query on user_group_mapping for the user's row on this group.
func authorizeNodeAccess(ctx context.Context, rmng_context *rmngctx.RmngContext, groupID, nodeID string) *events.APIGatewayProxyResponse {
	systemActor := utils.NewSystemActor()
	systemContext := rmngctx.NewRmngContextWithCtx(ctx, systemActor)

	groupNode, err := group_node_db.NewGroupNodeDB(systemContext).GetGroupNode(groupID, nodeID)
	if err != nil || groupNode == nil {
		rlog.Error(rmng_context).Err(err).Str("node_id", nodeID).Str("group_id", groupID).Msg("Node not found in group")
		resp := utils.APIGwRespJSON(http.StatusForbidden, utils.NewAPIStatus("User does not have access to this node"))
		return &resp
	}

	// Super-admins are allowed to assume role for any node.
	if accessor, ok := rmng_context.GetAccessor().(*user.User); ok && accessor != nil && accessor.IsSuperAdmin(rmng_context) {
		return nil
	}

	userGroupEntries, err := user_group_db.NewUserGroupDB(rmng_context).ListGroupsForUser(groupID)
	if err != nil {
		rlog.Error(rmng_context).Err(err).Str("group_id", groupID).Msg("Error fetching user-group entry")
		resp := utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Error fetching user groups"))
		return &resp
	}
	if len(userGroupEntries) == 0 {
		resp := utils.APIGwRespJSON(http.StatusForbidden, utils.NewAPIStatus("User does not have access to this node"))
		return &resp
	}

	entry := userGroupEntries[0]
	// Decide on access_type before falling back to sub_entity_ids: a subentity row with an empty list must deny, not be read as full-group access.
	if entry.AccessType != utils.GroupSubEntityAccess && len(entry.SubEntityIDs) == 0 {
		// Full-group access — covers every node in the group.
		return nil
	}
	if nodeInSharedSubgroups(groupNode, entry.SubEntityIDs) {
		return nil
	}
	resp := utils.APIGwRespJSON(http.StatusForbidden, utils.NewAPIStatus("User does not have access to this node"))
	return &resp
}

// nodeInSharedSubgroups returns true when at least one of the node's
// subgroup tags is in the set of subgroups shared with the user.
func nodeInSharedSubgroups(groupNode *group_node_db.GroupNode, sharedSubgroups []string) bool {
	nodeSubgroups := []string{groupNode.SubGrp1, groupNode.SubGrp2, groupNode.SubGrp3}
	for _, ns := range nodeSubgroups {
		if ns == "" {
			continue
		}
		for _, ss := range sharedSubgroups {
			if ns == ss {
				return true
			}
		}
	}
	return false
}

// createMQTTPolicyDocument builds the IoT/MQTT-only session policy for the user's groups.
func createMQTTPolicyDocument(accountId string, clientIdentity mqttClientIdentity, access groupAccess) (string, error) {
	region := os.Getenv("AWS_REGION")

	// iot:Connect is allowed only for the caller's own per-session client ids
	// "user:<email|phone>:<session>". The bare "user:<email|phone>" form is
	// deliberately NOT granted: every client must append a per-session suffix so
	// a user's own sessions (phone, dashboard) never collide, and no client can
	// claim the canonical bare id. The ':' delimiter cannot appear in an email
	// or phone number, so the wildcard only ever matches the caller's own
	// sessions. Kept minimal for the 2048-char inline session-policy limit.
	connectResources := make([]string, 0, 2)
	for _, clientID := range []string{clientIdentity.Email, clientIdentity.Phone} {
		if clientID == "" {
			continue
		}
		connectResources = append(connectResources,
			fmt.Sprintf("arn:aws:iot:%s:%s:client/user:%s:*", region, accountId, clientID),
		)
	}

	topicResources := []string{}
	topicFilterResources := []string{}

	// Per-group wildcards already cover every subgroup of that group, so we skip
	// per-subgroup ARNs for groups the user has full access to.
	fullGroups := make(map[string]struct{}, len(access.Groups))
	for _, grp := range access.Groups {
		fullGroups[grp] = struct{}{}
		shadowFilterResource := fmt.Sprintf("arn:aws:iot:%s:%s:*/$aws/things/*/shadow/name/params-%s*/*", region, accountId, grp)
		unicastPublishResource := fmt.Sprintf("arn:aws:iot:%s:%s:topic/rainmaker/nodes/*/user/params-%s*/*", region, accountId, grp)
		groupControlResource := fmt.Sprintf("arn:aws:iot:%s:%s:topic/rainmaker/nodes/groups/%s/control", region, accountId, grp)
		subgroupControlResource := fmt.Sprintf("arn:aws:iot:%s:%s:topic/rainmaker/nodes/groups/%s/subgroups/*/control", region, accountId, grp)

		topicResources = append(topicResources, unicastPublishResource, groupControlResource, subgroupControlResource)
		topicFilterResources = append(topicFilterResources, shadowFilterResource)
	}

	for grp, subgroups := range access.Subgroups {
		if _, ok := fullGroups[grp]; ok {
			continue
		}
		for _, subgroup := range subgroups {
			shadowFilterResource := fmt.Sprintf("arn:aws:iot:%s:%s:*/$aws/things/*/shadow/name/params-%s*%s*/*", region, accountId, grp, subgroup)
			subgroupControlResource := fmt.Sprintf("arn:aws:iot:%s:%s:topic/rainmaker/nodes/groups/%s/subgroups/%s/control", region, accountId, grp, subgroup)

			topicResources = append(topicResources, subgroupControlResource)
			topicFilterResources = append(topicFilterResources, shadowFilterResource)
		}
	}

	policyDocument := iamPolicyDocument{
		Version: "2012-10-17",
		Statement: []iamStatement{
			{Effect: "Allow", Action: []string{"iot:Publish"}, Resource: topicResources},
			{Effect: "Allow", Action: []string{"iot:Subscribe", "iot:Receive", "iot:Publish"}, Resource: topicFilterResources},
			{Effect: "Allow", Action: []string{"cognito-identity:GetCredentialsForIdentity"}, Resource: []string{fmt.Sprintf("arn:aws:cognito-identity:%s:%s::identitypool/*:identity/${cognito-identity.amazonaws.com:sub}", region, accountId)}},
			{Effect: "Allow", Action: []string{"iot:Connect"}, Resource: connectResources},
		},
	}

	jsonPolicy, err := json.Marshal(policyDocument)
	if err != nil {
		return "", rmerror.NewRMError(err, "failed to marshal MQTT policy document")
	}
	return string(jsonPolicy), nil
}

// createServicesPolicyDocument builds an S3/KVS-only session policy scoped to a single node.
// No IoT/MQTT statements are emitted; the IoT base policy lives in createMQTTPolicyDocument.
func createServicesPolicyDocument(accountId string, nodeID string, services []string) (string, error) {
	region := os.Getenv("AWS_REGION")
	statements := []iamStatement{}

	if includesService(services, "s3") {
		if filesBucketName := os.Getenv("FILES_BUCKET_NAME"); filesBucketName != "" {
			prefix := fmt.Sprintf("node-data/%s/*", nodeID)
			objectArn := fmt.Sprintf("arn:aws:s3:::%s/node-data/%s/*", filesBucketName, nodeID)

			statements = append(statements,
				iamStatement{
					Effect:   "Allow",
					Action:   "s3:ListBucket",
					Resource: fmt.Sprintf("arn:aws:s3:::%s", filesBucketName),
					Condition: map[string]any{
						"StringLike": map[string]any{
							"s3:prefix": []string{prefix},
						},
					},
				},
				iamStatement{
					Effect:   "Allow",
					Action:   []string{"s3:GetObject", "s3:DeleteObject"},
					Resource: []string{objectArn},
				},
			)
		}
	}

	if includesService(services, "kvs") {
		kvsArn := fmt.Sprintf("arn:aws:kinesisvideo:%s:%s:channel/rmng-v1-%s/*", region, accountId, nodeID)
		statements = append(statements, iamStatement{
			Effect: "Allow",
			Action: []string{
				"kinesisvideo:ConnectAsViewer",
				"kinesisvideo:GetSignalingChannelEndpoint",
				"kinesisvideo:DescribeSignalingChannel",
				"kinesisvideo:GetIceServerConfig",
			},
			Resource: []string{kvsArn},
		})
	}

	policyDocument := iamPolicyDocument{
		Version:   "2012-10-17",
		Statement: statements,
	}
	jsonPolicy, err := json.Marshal(policyDocument)
	if err != nil {
		return "", rmerror.NewRMError(err, "failed to marshal services policy document")
	}
	return string(jsonPolicy), nil
}

func resolveAssumeRoleGroupAccess(ctx context.Context, rmng_context *rmngctx.RmngContext, request events.APIGatewayProxyRequest, groupID, subGroupID string) (groupAccess, *events.APIGatewayProxyResponse) {
	if groupID != "" {
		accessor := rmng_context.GetAccessor()
		adminUser, ok := accessor.(*user.User)
		if !ok || adminUser == nil {
			rlog.Error(rmng_context).Str("auth_provider", request.RequestContext.Identity.CognitoAuthenticationProvider).Str("identity_id", request.RequestContext.Identity.CognitoIdentityID).Msg("Admin assume_role request missing user context")
			resp := utils.APIGwRespJSON(http.StatusForbidden, utils.NewAPIStatus("Admin privileges required"))
			return groupAccess{}, &resp
		}

		isSuperAdmin := adminUser.IsSuperAdmin(rmng_context)
		if !isSuperAdmin {
			rlog.Error(rmng_context).Str("user_id", adminUser.GetID()).Msg("User is not a super admin but tried to use admin assume_role")
			resp := utils.APIGwRespJSON(http.StatusForbidden, utils.NewAPIStatus("Admin privileges required"))
			return groupAccess{}, &resp
		}

		systemActor := utils.NewSystemActor()
		systemContext := rmngctx.NewRmngContextWithCtx(ctx, systemActor)

		groupDB := group_db.NewGroupDB(systemContext)
		_, err := groupDB.GetGroupByID(groupID)
		if err != nil {
			rlog.Error(rmng_context).Err(err).Str("group_id", groupID).Msg("Group not found")
			resp := utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Group not found"))
			return groupAccess{}, &resp
		}

		if subGroupID != "" {
			isSubGroup, err := groupDB.IsSubGroup(groupID, subGroupID)
			if err != nil || !isSubGroup {
				rlog.Error(rmng_context).Err(err).Str("subgroup_id", subGroupID).Str("group_id", groupID).Msg("Subgroup not found in group")
				resp := utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Subgroup not found"))
				return groupAccess{}, &resp
			}

			return groupAccess{
				Groups:    []string{},
				Subgroups: map[string][]string{groupID: {subGroupID}},
			}, nil
		}

		return groupAccess{
			Groups:    []string{groupID},
			Subgroups: map[string][]string{},
		}, nil
	}

	rawAccess, err := group.ListUserAccessableGroups(rmng_context)
	if err != nil {
		rlog.Error(rmng_context).Err(err).Msg("Error fetching groups for user")
		resp := utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Error fetching user groups"))
		return groupAccess{}, &resp
	}

	return toGroupAccess(rawAccess), nil
}

// toGroupAccess converts the untyped map returned by group.ListUserAccessableGroups
// into the typed groupAccess. Comma-ok guards each field so a malformed map yields
// empty slices/maps instead of panicking.
func toGroupAccess(raw map[string]interface{}) groupAccess {
	access := groupAccess{Groups: []string{}, Subgroups: map[string][]string{}}
	if groups, ok := raw["groups"].([]string); ok {
		access.Groups = groups
	}
	if subgroups, ok := raw["subgroups"].(map[string][]string); ok {
		access.Subgroups = subgroups
	}
	return access
}

func includesService(services []string, target string) bool {
	for _, s := range services {
		if s == target {
			return true
		}
	}
	return false
}

func main() {
	lambda.Start(handleRequest)
}
