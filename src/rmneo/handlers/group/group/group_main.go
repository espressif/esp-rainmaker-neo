// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"net/http"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngrequest"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/google/uuid"
)

type CreateGroupRequest struct {
	GroupName    string   `json:"group_name"`
	Capabilities []string `json:"capabilities,omitempty"`
}

type CreateGroupResponse struct {
	GroupID string                 `json:"group_id"`
	Matter  map[string]interface{} `json:"matter,omitempty"`
}

type ListGroupsResponse struct {
	Groups []GroupInfo `json:"groups"`
}

type GroupInfo struct {
	GroupID     string                        `json:"group_id"`
	GroupName   string                        `json:"group_name"`
	AccessType  string                        `json:"access_type"`
	NodeIDs     []string                      `json:"node_ids,omitempty"`
	NodeDetails map[string]NodeCapabilityInfo `json:"node_details,omitempty"`
	Subgroups   []SubGroupInfo                `json:"subgroups,omitempty"`
	Matter      map[string]interface{}        `json:"matter,omitempty"`
}

type SubGroupInfo struct {
	SubgroupID   string   `json:"subgroup_id"`
	SubgroupName string   `json:"subgroup_name"`
	NodeIDs      []string `json:"node_ids,omitempty"`
}

type NodeCapabilityInfo struct {
	Capabilities      []string               `json:"capabilities,omitempty"`
	CapabilityDetails map[string]interface{} `json:"capability_details,omitempty"`
}

type CreateSubgroupRequest struct {
	SubgroupName string `json:"subgroup_name"`
}

type CreateSubgroupResponse struct {
	SubgroupID string `json:"subgroup_id"`
}

type UpdateGroupRequest struct {
	GroupName string `json:"group_name" validate:"required"`
}

type UpdateSubgroupRequest struct {
	SubgroupName string `json:"subgroup_name" validate:"required"`
}

type GroupEditResponse struct {
	Message string `json:"message"`
}

// CreateSharingRequestRequest names the invitee by username — their email address
// or E.164 phone number, the same identifier signup and signin take. Internal user
// IDs are not accepted here: the owner knows the person by what they sign in with.
type CreateSharingRequestRequest struct {
	Username   string                `json:"username" validate:"required"`
	AccessType utils.GroupAccessType `json:"access_type,omitempty"`
}

const sharingRequestAcceptedMessage = "Invite sent if the user is registered with ESP RainMaker Neo"

type CreateSharingRequestResponse struct {
	RequestID string `json:"request_id"`
	Message   string `json:"message"`
}

type SharingRequestInfo struct {
	SharingRequestID   string `json:"sharing_request_id"`
	GroupID            string `json:"group_id"`
	SubgroupID         string `json:"subgroup_id"`
	AccessType         string `json:"access_type"`
	PrimaryUserID      string `json:"primary_user_id"`
	PrimaryEmail       string `json:"primary_email"`
	PrimaryPhoneNumber string `json:"primary_phone_number"`
}

type ListSharingRequestsResponse struct {
	SharingRequests []SharingRequestInfo `json:"sharing_requests"`
}

type ListGroupUsersResponse struct {
	Users []GroupUserInfoResponse `json:"users"`
}

type GroupUserInfoResponse struct {
	UserID     string   `json:"user_id"`
	Email      string   `json:"email,omitempty"`
	Phone      string   `json:"phone_number,omitempty"`
	AccessType string   `json:"access_type"`
	Subgroups  []string `json:"subgroups,omitempty"`
}

// MatterNOCRequest is the request body for generating a Matter NOC
type MatterNOCRequest struct {
	CSR string `json:"csr"`
}

// MatterNOCResponse is the response for the Matter NOC API
type MatterNOCResponse struct {
	NOC          string `json:"noc"`
	MatterNodeID string `json:"matter_node_id"`
}

// handleMatterNOC generates a Node Operational Certificate for the current user in a Matter-enabled group.
// The user must provide a CSR (Certificate Signing Request) in the request body.
func handleMatterNOC(context context.Context, request events.APIGatewayProxyRequest, groupID string) (events.APIGatewayProxyResponse, error) {
	ctx := user.NewContextWithAPIRequest(context, request)

	// Parse request body
	var req MatterNOCRequest
	err := rmngrequest.ExtractRequestStruct(request, &req)
	if err != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid request body")), nil
	}

	if req.CSR == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Missing CSR")), nil
	}

	// Load the Matter group (verifies user access and Matter capability)
	matterGroup, err := group.LoadMatterGroupFromGrpID(ctx, groupID)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to load Matter group")
		// Determine appropriate error response
		if errors.Is(err, group.ErrNotMatterCapable) {
			return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Group does not have Matter capability")), nil
		}
		return utils.APIGwRespJSON(http.StatusNotFound, utils.NewAPIStatus("Group not found")), nil
	}

	// Generate the NOC
	result, err := matterGroup.GetNOC(ctx, req.CSR)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to generate NOC")
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to generate NOC")), nil
	}

	response := MatterNOCResponse{
		NOC:          result.NOC,
		MatterNodeID: result.MatterNodeID,
	}

	return utils.APIGwRespJSON(http.StatusOK, response), nil
}

// AddGroupCapabilitiesRequest enables one or more capabilities on an existing group.
type AddGroupCapabilitiesRequest struct {
	Capabilities []string `json:"capabilities"`
}

// AddGroupCapabilitiesResponse is returned after capabilities are enabled on a group.
type AddGroupCapabilitiesResponse struct {
	Matter map[string]interface{} `json:"matter,omitempty"`
}

// handleAddGroupCapabilities enables the capabilities listed in the request body on an
// existing group. Enabling "matter" turns the group into a Matter fabric. The request is
// rejected when the group already has a requested capability or when the caller is not
// the group owner.
func handleAddGroupCapabilities(context context.Context, request events.APIGatewayProxyRequest, groupID string) (events.APIGatewayProxyResponse, error) {
	ctx := user.NewContextWithAPIRequest(context, request)

	if groupID == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Missing group ID")), nil
	}

	var req AddGroupCapabilitiesRequest
	if err := rmngrequest.ExtractRequestStruct(request, &req); err != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid request body")), nil
	}
	if len(req.Capabilities) == 0 {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Missing capabilities")), nil
	}

	updatedGroup, err := group.AddCapabilitiesToGroup(ctx, groupID, req.Capabilities)
	if err != nil {
		rlog.Error(ctx).Err(err).Send()
		if errors.Is(err, group.ErrGroupAccessDenied) {
			return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Group not found or not accessible")), nil
		}
		if errors.Is(err, group.ErrCapabilityAlreadyExists) {
			return utils.APIGwRespJSON(http.StatusConflict, utils.NewAPIStatus("Group already has the requested capability")), nil
		}
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to update group capabilities")), nil
	}

	response := AddGroupCapabilitiesResponse{}

	// Include matter data only when the matter capability is now present.
	if _, hasMatter := updatedGroup.CapabilityData[group.MatterCapabilityName]; hasMatter {
		if cap, ok := group.GetCapability(group.MatterCapabilityName); ok {
			matterData, mErr := cap.GetResponseData(ctx, updatedGroup)
			if mErr != nil {
				rlog.Error(ctx).Err(mErr).Msg("Failed to get matter capability data for group")
			} else {
				response.Matter = matterData
			}
		}
	}

	return utils.APIGwRespJSON(http.StatusOK, response), nil
}

func handleAcceptSharingRequest(context context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	ctx := user.NewContextWithAPIRequest(context, request)
	requestID := request.PathParameters["requestId"]
	if requestID == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Missing requestId")), nil
	}
	err := group.ApproveSharingRequest(ctx, requestID)
	if err != nil {
		rlog.Error(ctx).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to accept sharing request")), nil
	}
	return utils.APIGwRespJSON(http.StatusOK, utils.NewAPIStatus("Request accepted successfully")), nil
}

func handleRejectSharingRequest(context context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	ctx := user.NewContextWithAPIRequest(context, request)
	requestID := request.PathParameters["requestId"]
	if requestID == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Missing requestId")), nil
	}
	err := group.RejectSharingRequest(ctx, requestID)
	if err != nil {
		rlog.Error(ctx).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to reject sharing request")), nil
	}
	return utils.APIGwRespJSON(http.StatusOK, utils.NewAPIStatus("Request rejected successfully")), nil
}

func handleListSharingRequests(context context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	ctx := user.NewContextWithAPIRequest(context, request)

	sharingRequests, err := group.GetMySharingRequests(ctx)
	if err != nil {
		rlog.Error(ctx).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to list sharing requests")), nil
	}

	sharingRequestInfos := make([]SharingRequestInfo, 0, len(sharingRequests))
	for _, sharingRequest := range sharingRequests {
		sharingRequestInfos = append(sharingRequestInfos, SharingRequestInfo{
			SharingRequestID:   sharingRequest.SharingRequestID,
			GroupID:            sharingRequest.GroupID,
			SubgroupID:         sharingRequest.SubEntityID,
			AccessType:         sharingRequest.AccessType,
			PrimaryUserID:      sharingRequest.PrimaryUserID,
			PrimaryEmail:       sharingRequest.PrimaryEmail,
			PrimaryPhoneNumber: sharingRequest.PrimaryPhoneNumber,
		})
	}
	response := ListSharingRequestsResponse{
		SharingRequests: sharingRequestInfos,
	}

	return utils.APIGwRespJSON(http.StatusOK, response), nil
}

func handleCreateGroup(context context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	ctx := user.NewContextWithAPIRequest(context, request)

	// Creating a main group
	var req CreateGroupRequest
	err := rmngrequest.ExtractRequestStruct(request, &req)
	if err != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid request body")), nil
	}

	// Build options from request
	var opts *group.CreateGroupOptions
	if len(req.Capabilities) > 0 {
		opts = &group.CreateGroupOptions{
			Capabilities: req.Capabilities,
		}
	}

	createdGroup, err := group.CreateGroupForUserWithOptions(ctx, req.GroupName, opts)
	if err != nil {
		rlog.Error(ctx).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to create group")), nil
	}

	response := CreateGroupResponse{
		GroupID: createdGroup.GroupID,
	}

	// If matter capability is present, populate response.Matter
	if _, hasMatter := createdGroup.CapabilityData[group.MatterCapabilityName]; hasMatter {
		cap, _ := group.GetCapability(group.MatterCapabilityName)
		matterData, err := cap.GetResponseData(ctx, createdGroup)
		if err != nil {
			rlog.Error(ctx).Err(err).Msg("Failed to get matter capability data for group")
		} else {
			response.Matter = matterData
		}
	}

	return utils.APIGwRespJSON(http.StatusCreated, response), nil
}

func handleCreateSubgroup(context context.Context, request events.APIGatewayProxyRequest, groupID string) (events.APIGatewayProxyResponse, error) {
	ctx := user.NewContextWithAPIRequest(context, request)

	var req CreateSubgroupRequest
	err := rmngrequest.ExtractRequestStruct(request, &req)
	if err != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid request body")), nil
	}

	// Create the subgroup
	createdSubgroup, err := group.CreateSubGroup(ctx, groupID, req.SubgroupName)
	if err != nil {
		rlog.Error(ctx).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to create subgroup")), nil
	}

	response := CreateSubgroupResponse{
		SubgroupID: createdSubgroup.SubGroupID,
	}

	return utils.APIGwRespJSON(http.StatusCreated, response), nil
}

func handleListGroups(context context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	ctx := user.NewContextWithAPIRequest(context, request)

	groups, err := group.ListGroupsForUser(ctx, true)
	if err != nil {
		rlog.Error(ctx).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to list groups")), nil
	}

	groupInfos := make([]GroupInfo, 0, len(groups))
	for _, grp := range groups {
		groupInfo, err := convertToGroupInfo(ctx, &grp)
		if err != nil {
			rlog.Error(ctx).Err(err).Send()
			continue
		}
		groupInfos = append(groupInfos, groupInfo)
	}

	response := ListGroupsResponse{
		Groups: groupInfos,
	}

	return utils.APIGwRespJSON(http.StatusOK, response), nil
}

func convertToGroupInfo(ctx *rmngctx.RmngContext, grp *group.Group) (GroupInfo, error) {
	subgroupInfos := make([]SubGroupInfo, 0, len(grp.SubGroups))
	for _, subgroup := range grp.SubGroups {
		subgroupInfos = append(subgroupInfos, SubGroupInfo{
			SubgroupID:   subgroup.SubGroupID,
			SubgroupName: subgroup.SubGroupName,
			NodeIDs:      group_node_db.GetNodeIDs(subgroup.NodeGroupEntries),
		})
	}

	accessType := string(grp.AccessType)
	if grp.AccessType == utils.GroupSubEntityAccess {
		accessType = "subgroup"
	}

	groupInfo := GroupInfo{
		GroupID:     grp.GroupID,
		GroupName:   grp.GroupName,
		AccessType:  accessType,
		NodeIDs:     group_node_db.GetNodeIDs(grp.NodeGroupEntries),
		NodeDetails: make(map[string]NodeCapabilityInfo),
		Subgroups:   subgroupInfos,
	}

	// Populate node_details: the node's capability names plus per-capability detail data.
	for nodeID, groupNode := range grp.NodeGroupEntries {
		capabilities := groupNode.Capabilities
		if len(capabilities) == 0 {
			// the pure-Matter node id format (16-hex) implies "matter"
			if group.IsMatterNodeIDFormat(groupNode.NodeID) {
				capabilities = []string{group.MatterCapabilityName}
			} else {
				// No way to know if a node is pure rmng or rmng+matter, so assume rmng only
				capabilities = []string{group.NodeCapabilityRMNG}
			}
		}

		capabilityInfo := NodeCapabilityInfo{
			Capabilities:      capabilities,
			CapabilityDetails: make(map[string]interface{}),
		}

		for _, capName := range capabilities {
			capData, err := getCapabilityData(groupNode, capName)
			if err != nil {
				rlog.Error(ctx).Err(err).Str("capability", capName).Str("node_id", nodeID).Msg("Failed to parse capability data")
				continue
			}
			if capData != nil {
				capabilityInfo.CapabilityDetails[capName] = capData
			}
		}

		if len(capabilityInfo.CapabilityDetails) == 0 {
			capabilityInfo.CapabilityDetails = nil
		}
		groupInfo.NodeDetails[nodeID] = capabilityInfo
	}

	// If matter capability is present, populate Matter data
	if _, hasMatter := grp.CapabilityData[group.MatterCapabilityName]; hasMatter {
		cap, _ := group.GetCapability(group.MatterCapabilityName)
		matterData, err := cap.GetResponseData(ctx, grp)
		if err != nil {
			rlog.Error(ctx).Err(err).Msg("Failed to get matter capability data for group")
		} else {
			groupInfo.Matter = matterData
		}
	}

	return groupInfo, nil
}

func getCapabilityData(groupNode *group_node_db.GroupNode, capabilityName string) (map[string]interface{}, error) {
	switch capabilityName {
	case group.MatterCapabilityName:
		// matter_node_id is derived from the node_id, not stored.
		return map[string]interface{}{
			"matter_node_id": group.MatterNodeIDFromThingName(groupNode.NodeID),
		}, nil
	default:
		// Detail for non-core capabilities is supplied verbatim by the optional
		// module that owns the capability (written to capability_data at
		// association time); core does not interpret it.
		return groupNode.CapabilityData[capabilityName], nil
	}
}

func handlePostGroup(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	// Check if we're creating a subgroup
	groupID := request.PathParameters["groupId"]
	if groupID != "" {
		// Creating a subgroup
		return handleCreateSubgroup(ctx, request, groupID)
	} else {
		return handleCreateGroup(ctx, request)
	}
}

func handlePatchGroup(context context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	ctx := user.NewContextWithAPIRequest(context, request)

	groupID := request.PathParameters["groupId"]
	if groupID == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Missing group ID")), nil
	}

	var req UpdateGroupRequest
	if err := rmngrequest.ExtractRequestStruct(request, &req); err != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid request body")), nil
	}

	if err := group.UpdateGroup(ctx, groupID, req.GroupName); err != nil {
		rlog.Error(ctx).Err(err).Send()
		if errors.Is(err, group.ErrGroupAccessDenied) {
			return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Group not found or not accessible")), nil
		}
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to update group name")), nil
	}

	return utils.APIGwRespJSON(http.StatusOK, utils.NewAPIStatus("success")), nil
}

func handlePatchSubGroup(context context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	ctx := user.NewContextWithAPIRequest(context, request)

	groupID := request.PathParameters["groupId"]
	subgroupID := request.PathParameters["subGroupId"]
	if groupID == "" || subgroupID == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Missing group ID or subgroup ID")), nil
	}

	var req UpdateSubgroupRequest
	if err := rmngrequest.ExtractRequestStruct(request, &req); err != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid request body")), nil
	}

	if err := group.UpdateSubGroup(ctx, groupID, subgroupID, req.SubgroupName); err != nil {
		rlog.Error(ctx).Err(err).Send()
		if errors.Is(err, group.ErrSubGroupAccessDenied) {
			return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Subgroup not found or not accessible")), nil
		}
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to update subgroup name")), nil
	}

	return utils.APIGwRespJSON(http.StatusOK, utils.NewAPIStatus("success")), nil
}

func handleDeleteGroup(context context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	ctx := user.NewContextWithAPIRequest(context, request)

	groupID := request.PathParameters["groupId"]

	// Load the user's group permissions into the authorization context so that
	// GetGroupNodes (which checks GroupListSubEntities) can succeed.
	if _, err := group.GetUserGroupAccess(ctx, groupID); err != nil {
		rlog.Error(ctx).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Group not found or not accessible")), nil
	}

	if err := group.DeleteGroup(ctx, groupID); err != nil {
		rlog.Error(ctx).Err(err).Send()
		if errors.Is(err, group.ErrGroupNotEmpty) {
			return utils.APIGwRespJSON(http.StatusConflict, utils.NewAPIStatus("group not empty")), nil
		}
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to delete group")), nil
	}

	return utils.APIGwRespJSON(http.StatusOK, utils.NewAPIStatus("Group deleted successfully")), nil
}

// toGroupUserInfoResponses maps enriched group users to the wire response. The
// subgroup list is exposed only for subgroup-access members; primary and
// secondary members carry no subgroups field.
func toGroupUserInfoResponses(users []group.GroupUserInfo) []GroupUserInfoResponse {
	userInfos := make([]GroupUserInfoResponse, 0, len(users))
	for _, u := range users {
		info := GroupUserInfoResponse{
			UserID:     u.UserID,
			Email:      u.Email,
			Phone:      u.Phone,
			AccessType: string(u.AccessType),
		}
		if u.AccessType == utils.GroupSubEntityAccess {
			info.AccessType = "subgroup"
			info.Subgroups = u.SubEntityIDs
		}
		userInfos = append(userInfos, info)
	}
	return userInfos
}

func handleListGroupUsers(context context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	ctx := user.NewContextWithAPIRequest(context, request)

	groupID := request.PathParameters["groupId"]
	if groupID == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Missing group ID")), nil
	}

	users, err := group.ListUsersForGroup(ctx, groupID)
	if err != nil {
		rlog.Error(ctx).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to list group users")), nil
	}

	return utils.APIGwRespJSON(http.StatusOK, ListGroupUsersResponse{Users: toGroupUserInfoResponses(users)}), nil
}

func handleDeleteSubgroup(context context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	ctx := user.NewContextWithAPIRequest(context, request)

	groupID := request.PathParameters["groupId"]
	subGroupID := request.PathParameters["subGroupId"]

	err := group.DeleteSubGroup(ctx, groupID, subGroupID)
	if err != nil {
		rlog.Error(ctx).Err(err).Send()
		if errors.Is(err, group.ErrSubGroupAccessDenied) {
			return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Group not found or not accessible")), nil
		}
		if errors.Is(err, group.ErrSubGroupDeleteForbidden) {
			return utils.APIGwRespJSON(http.StatusForbidden, utils.NewAPIStatus("Insufficient permissions to delete subgroup")), nil
		}
		if errors.Is(err, group.ErrSubGroupNotEmpty) {
			return utils.APIGwRespJSON(http.StatusConflict, utils.NewAPIStatus("subgroup not empty")), nil
		}
		if errors.Is(err, db.ErrSubGroupNotFound) {
			return utils.APIGwRespJSON(http.StatusNotFound, utils.NewAPIStatus("Subgroup not found")), nil
		}
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to delete subgroup")), nil
	}

	return utils.APIGwRespJSON(http.StatusOK, utils.NewAPIStatus("Subgroup deleted successfully")), nil
}

// resolveUserID resolves a {userId} path segment to a real user ID, confirming
// the user exists. Returns the empty string if it does not resolve.
//
// Only a real user ID is accepted here — GET /v1/groups/{groupId}/users hands
// those out, so removal always works straight off the member listing. User names
// are the share-side identifier only.
func resolveUserID(ctx *rmngctx.RmngContext, userID string) string {
	if targetUser, err := user.NewUserFromUserID(ctx, userID); err == nil {
		return targetUser.UserID
	}
	return ""
}

func handleDeleteGroupUser(context context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	ctx := user.NewContextWithAPIRequest(context, request)

	groupID := request.PathParameters["groupId"]
	userID := request.PathParameters["userId"]
	if groupID == "" || userID == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Missing group ID or user ID")), nil
	}

	// Handle "me" as current user ID
	if userID == "me" {
		userID = ctx.Accessor.GetID()
	}

	// Confirm the user ID from the path resolves to a real user.
	userID = resolveUserID(ctx, userID)
	if userID == "" {
		return utils.APIGwRespJSON(http.StatusNotFound, utils.NewAPIStatus("User not found")), nil
	}

	err := group.UnshareGroup(ctx, groupID, userID)
	if err != nil {
		rlog.Error(ctx).Err(err).Send()
		if errors.Is(err, db.ErrLastPrimaryUser) {
			return utils.APIGwRespJSON(http.StatusConflict, utils.NewAPIStatus("Cannot remove the last primary user from the group")), nil
		}
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to remove member")), nil
	}

	return utils.APIGwRespJSON(http.StatusOK, utils.NewAPIStatus("Member removed successfully")), nil
}

func handleSharingRequest(context context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	ctx := user.NewContextWithAPIRequest(context, request)

	var req CreateSharingRequestRequest
	err := rmngrequest.ExtractRequestStruct(request, &req)
	if err != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid request body")), nil
	}

	if req.Username == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Missing username")), nil
	}

	groupID := request.PathParameters["groupId"]
	if groupID == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Missing group ID")), nil
	}

	// An unresolved username gets a byte-identical 201 with a decoy UUID; any
	// future sharer-side lookup of request_id must fake decoys too, or it becomes
	// an enumeration oracle.
	targetUser, err := user.NewUserFromUserName(ctx, req.Username)
	if err != nil {
		rlog.Error(ctx).Err(err).Send()
		if errors.Is(err, user.ErrInvalidUserName) {
			return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("username must be an email address or an E.164 phone number")), nil
		}
		return utils.APIGwRespJSON(http.StatusCreated, CreateSharingRequestResponse{
			RequestID: uuid.New().String(),
			Message:   sharingRequestAcceptedMessage,
		}), nil
	}
	targetUserID := targetUser.UserID

	subgroupID := request.PathParameters["subGroupId"]
	var requestID string
	var shareErr error

	primaryUserInfo := ctx.Accessor.(*user.User).UserInfo

	// Share subgroup
	if subgroupID != "" {
		requestID, shareErr = group.ShareSubGroup(ctx, groupID, subgroupID, targetUserID, primaryUserInfo)
	} else { // Share group
		// This route grants group-level access only, subgroup shares take the branch above and carry the subgroup in the path. Collapsing everything but "primary" to the least-privilege default keeps a subgroup access type from ever reaching the DB layer, where it would name no subgroup.
		accessType := req.AccessType
		if accessType != utils.GroupPrimaryAccess {
			accessType = utils.GroupSecondaryAccess
		}
		requestID, shareErr = group.ShareGroup(ctx, groupID, targetUserID, accessType, primaryUserInfo)

	}

	if shareErr != nil {
		rlog.Error(ctx).Err(shareErr).Send()
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to create sharing request")), nil
	}

	return utils.APIGwRespJSON(http.StatusCreated, CreateSharingRequestResponse{
		RequestID: requestID,
		Message:   sharingRequestAcceptedMessage,
	}), nil
}

func handleDeleteSubgroupUser(context context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	ctx := user.NewContextWithAPIRequest(context, request)

	groupID := request.PathParameters["groupId"]
	subgroupID := request.PathParameters["subGroupId"]
	userID := request.PathParameters["userId"]
	if groupID == "" || subgroupID == "" || userID == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Missing group ID, subgroup ID, or user ID")), nil
	}

	// Handle "me" as current user ID
	if userID == "me" {
		userID = ctx.Accessor.GetID()
	}

	// Confirm the user ID from the path resolves to a real user.
	userID = resolveUserID(ctx, userID)
	if userID == "" {
		return utils.APIGwRespJSON(http.StatusNotFound, utils.NewAPIStatus("User not found")), nil
	}

	err := group.UnshareSubGroup(ctx, groupID, subgroupID, userID)
	if err != nil {
		rlog.Error(ctx).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to remove member")), nil
	}

	return utils.APIGwRespJSON(http.StatusOK, utils.NewAPIStatus("Member removed successfully")), nil
}

func handleUpdateSubgroupNodeOperation(context context.Context, request events.APIGatewayProxyRequest, groupID string, subGroupID string, nodeID string, action string) (events.APIGatewayProxyResponse, error) {
	ctx := user.NewContextWithAPIRequest(context, request)
	var err_str string

	var operationType group_node_db.SubGroupOperationType
	if action == "add" {
		operationType = group_node_db.SubGroupOperationTypeAdd
		err_str = "Failed to add node to subgroup"
	} else if action == "remove" {
		operationType = group_node_db.SubGroupOperationTypeRemove
		err_str = "Failed to remove node from subgroup"
	}
	err := node.ShadowNodeUpdateSubGroup(ctx, nodeID, groupID, subGroupID, operationType)
	if err != nil {
		rlog.Error(ctx).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus(err_str)), nil
	}

	return utils.APIGwRespJSON(http.StatusOK, GroupEditResponse{Message: "success"}), nil
}

func handleRemoveNodeFromGroup(context context.Context, request events.APIGatewayProxyRequest, groupID string, nodeID string) (events.APIGatewayProxyResponse, error) {
	// Child nodes (IDs containing "--") are lifecycle-managed by their parent
	// node's flow, not by end users. Direct removal here would bypass the
	// parent's own teardown (e.g. its child registry + IoT Thing cleanup),
	// leaving orphaned state, so reject it. The parent's system-flow removal
	// (ShadowNodeRemoveFromGroupAuthorized) is unguarded and still works.
	if group.IsChildNode(nodeID) {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("child nodes are managed by their parent node and cannot be removed from a group directly")), nil
	}
	ctx := user.NewContextWithAPIRequest(context, request)

	err := node.ShadowNodeRemoveFromGroup(ctx, nodeID, groupID)
	if err != nil {
		rlog.Error(ctx).Err(err).Send()
		if errors.Is(err, group.ErrGroupAccessDenied) {
			return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Group not found or not accessible")), nil
		}
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to remove node from group")), nil
	}

	return utils.APIGwRespJSON(http.StatusOK, GroupEditResponse{Message: "success"}), nil
}

func handleListSubgroupUsers(context context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	ctx := user.NewContextWithAPIRequest(context, request)

	groupID := request.PathParameters["groupId"]
	subGroupID := request.PathParameters["subGroupId"]

	if groupID == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Missing group ID")), nil
	}

	if subGroupID == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Missing subGroup ID")), nil
	}

	users, err := group.ListUsersForSubGroup(ctx, groupID, subGroupID)
	if err != nil {
		rlog.Error(ctx).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to list subgroup users")), nil
	}

	return utils.APIGwRespJSON(http.StatusOK, ListGroupUsersResponse{Users: toGroupUserInfoResponses(users)}), nil
}

func handleRequest(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	// These are safe to access even if the key doesn't exist (returns empty string)
	groupID := request.PathParameters["groupId"]
	subgroupID := request.PathParameters["subGroupId"]
	nodeID := request.PathParameters["nodeId"]

	switch request.Resource {

	// --- Sharing Requests (Global) ---
	case "/v1/sharing-requests/received":
		if request.HTTPMethod == "GET" {
			return handleListSharingRequests(ctx, request)
		}
	case "/v1/sharing-requests/{requestId}/accept":
		if request.HTTPMethod == "POST" {
			return handleAcceptSharingRequest(ctx, request)
		}
	case "/v1/sharing-requests/{requestId}/reject":
		if request.HTTPMethod == "POST" {
			return handleRejectSharingRequest(ctx, request)
		}

	// --- Matter NOC ---
	case "/v1/groups/{groupId}/matter-nocs":
		if request.HTTPMethod == "POST" {
			// pathParts[2] in your old code corresponds to groupID here
			return handleMatterNOC(ctx, request, groupID)
		}

	// --- Group Capabilities (e.g. enabling Matter to convert a group to a fabric) ---
	case "/v1/groups/{groupId}/capabilities":
		if request.HTTPMethod == "POST" {
			return handleAddGroupCapabilities(ctx, request, groupID)
		}

	// --- Group & Subgroup Sharing Requests ---
	case "/v1/groups/{groupId}/sharing-requests":
		if request.HTTPMethod == "POST" {
			return handleSharingRequest(ctx, request)
		}
	case "/v1/groups/{groupId}/subgroups/{subGroupId}/sharing-requests":
		if request.HTTPMethod == "POST" {
			return handleSharingRequest(ctx, request)
		}

	// --- Group Users ---
	case "/v1/groups/{groupId}/users":
		if request.HTTPMethod == "GET" {
			return handleListGroupUsers(ctx, request)
		}
	case "/v1/groups/{groupId}/users/{userId}":
		if request.HTTPMethod == "DELETE" {
			return handleDeleteGroupUser(ctx, request)
		}
	case "/v1/groups/{groupId}/subgroups/{subGroupId}/users/{userId}":
		if request.HTTPMethod == "DELETE" {
			return handleDeleteSubgroupUser(ctx, request)
		}

	// --- Node Operations ---
	case "/v1/groups/{groupId}/nodes/{nodeId}":
		switch request.HTTPMethod {
		case "DELETE":
			return handleRemoveNodeFromGroup(ctx, request, groupID, nodeID)
		}

	// --- Subgroup Node Operations ---
	// TODO: check if GET for /v1/groups/{groupId}/subgroups/{subGroupId}/nodes/{nodeId} is needed to retrieve node configuration within a subgroup
	case "/v1/groups/{groupId}/subgroups/{subGroupId}/nodes/{nodeId}":
		switch request.HTTPMethod {
		case "PUT":
			return handleUpdateSubgroupNodeOperation(ctx, request, groupID, subgroupID, nodeID, "add")
		case "DELETE":
			return handleUpdateSubgroupNodeOperation(ctx, request, groupID, subgroupID, nodeID, "remove")
		}

	// --- General Group Operations ---
	case "/v1/groups":
		switch request.HTTPMethod {
		case "GET":
			return handleListGroups(ctx, request)
		case "POST":
			return handlePostGroup(ctx, request)
		}

	case "/v1/groups/{groupId}":
		switch request.HTTPMethod {
		case "PATCH":
			return handlePatchGroup(ctx, request)
		case "DELETE":
			return handleDeleteGroup(ctx, request)
		}

	case "/v1/groups/{groupId}/subgroups":
		if request.HTTPMethod == "POST" {
			return handlePostGroup(ctx, request)
		}

	case "/v1/groups/{groupId}/subgroups/{subGroupId}":
		switch request.HTTPMethod {
		case "PATCH":
			return handlePatchSubGroup(ctx, request)
		case "DELETE":
			return handleDeleteSubgroup(ctx, request)
		}

	case "/v1/groups/{groupId}/subgroups/{subGroupId}/users":
		if request.HTTPMethod == "GET" {
			return handleListSubgroupUsers(ctx, request)
		}

	default:
		return utils.APIGwRespJSON(http.StatusNotFound, utils.NewAPIStatus("Not found")), nil
	}

	return utils.APIGwRespJSON(http.StatusMethodNotAllowed, utils.NewAPIStatus("Method not allowed")), nil
}

func main() {
	lambda.Start(handleRequest)
}
