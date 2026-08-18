// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"net/http"
	"os"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/iotutil"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_reg_req_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/nodeadmin/bulk_job"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/nodeadmin/jobs_api"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngrequest"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

// notFoundMessage is the 404 body used when this Lambda is asked about
// a request_id that doesn't exist OR that belongs to an update job
// (cross-flow scoping). Kept Lambda-local so the message reads right
// for clients hitting /registration-jobs.
const notFoundMessage = "registration job not found"

// RegisterSingleNodeRequest is for single node registration
type RegisterSingleNodeRequest struct {
	Cert                 string   `json:"cert" validate:"required,startswith=-----BEGIN,std_iot_cert"`
	CACert               string   `json:"ca_cert,omitempty"          validate:"omitempty,startswith=-----BEGIN,std_iot_cert"`
	Checksum             string   `json:"checksum,omitempty"         validate:"omitempty,md5"`
	CAChecksum           string   `json:"ca_checksum,omitempty"      validate:"omitempty,md5"`
	AdminGroupNames      []string `json:"admin_group_names,omitempty" validate:"omitempty,dive,required"`
	AdminParentGroupName string   `json:"admin_parent_group_name,omitempty"`
	Tags                 []string `json:"tags,omitempty"             validate:"omitempty,dive,required,std_tag_format"`
	Capabilities         []string `json:"capabilities,omitempty"     validate:"omitempty,dive,required"`
}

type RegisterSingleNodeResponse struct {
	NodeID string `json:"node_id"`
}

// RegisterBulkNodesRequest is for bulk node registration
type RegisterBulkNodesRequest struct {
	CertFileS3Path       string   `json:"cert_file_s3_path" validate:"required,startswith=s3://"`
	AdminGroupNames      []string `json:"admin_group_names,omitempty" validate:"omitempty,dive,required"`
	AdminParentGroupName string   `json:"admin_parent_group_name,omitempty"`
	Tags                 []string `json:"tags,omitempty"             validate:"omitempty,dive,required,std_tag_format"`
	Capabilities         []string `json:"capabilities,omitempty"     validate:"omitempty,dive,required"`
}

type RegisterBulkNodesResponse struct {
	RequestId string `json:"request_id"`
	Message   string `json:"message"`
}

// ListRegistrationJobsResponse is for the list endpoint. This list endpoint
// is register-Lambda-only (no update equivalent), so the response type stays
// here. Jobs uses the shared StatusResponse type.
type ListRegistrationJobsResponse struct {
	Jobs      []jobs_api.StatusResponse `json:"jobs"`
	PageTotal int                       `json:"page_total"`
	NextKey   string                    `json:"next_key,omitempty"`
}

// Test-facing aliases so the in-package _test.go file (also package main)
// keeps referring to the Lambda-local type names while the actual
// definitions live in jobs_api.
type (
	RegisterNodeStatusResponse = jobs_api.StatusResponse
	ListFailedNodesResponse    = jobs_api.ListFailedNodesResponse
)

func handleRegisterSingleNode(rctx *rmngctx.RmngContext, request events.APIGatewayProxyRequest) (RegisterSingleNodeResponse, error) {
	// Parse the request body
	var req RegisterSingleNodeRequest

	// TODO: Once we figure out rbac for admins, remove this
	rctx.SetAllow(utils.NodeAdminAll, "*")
	rctx.SetAllow(utils.NodeWriteShadow, "*")

	if err := rmngrequest.ExtractRequestStruct(request, &req); err != nil {
		return RegisterSingleNodeResponse{}, rmerror.NewRMError(err, "failed to extract request")
	}

	if len(req.AdminGroupNames) > 0 {
		err := node.CreateAdminGroupIfNotExists(rctx, req.AdminGroupNames, req.AdminParentGroupName)
		if err != nil {
			return RegisterSingleNodeResponse{}, rmerror.NewRMError(err, err.Error())
		}
	}

	nodeId, err := node.RegisterNodeInRmng(rctx, req.Cert, req.CACert, req.AdminGroupNames, req.Tags, rctx.GetID(), req.Capabilities)
	if err != nil {
		if errors.Is(err, node.ErrNodeAlreadyRegistered) {
			return RegisterSingleNodeResponse{}, rmerror.NewRMError(err, fmt.Sprintf("node %s is already registered", nodeId))
		}
		return RegisterSingleNodeResponse{}, rmerror.NewRMError(err, "failed to register node")
	}

	// Create KVS signaling channel for the node (non-blocking — log and continue on failure)
	if iotutil.HasCapability(req.Capabilities, "kvs") {
		if err := iotutil.CreateSignalingChannel(rctx.Context, "rmng-v1-"+nodeId); err != nil {
			rlog.Error(rctx).Err(err).Str("nodeId", nodeId).Msg("failed to create signaling channel during registration")
		}
	}

	return RegisterSingleNodeResponse{
		NodeID: nodeId,
	}, nil
}

func handleRegisterBulkNodes(rctx *rmngctx.RmngContext, request events.APIGatewayProxyRequest) (RegisterBulkNodesResponse, error) {
	// Parse the request body
	var req RegisterBulkNodesRequest

	// TODO: Once we figure out rbac for admins, remove this
	rctx.SetAllow(utils.NodeAdminAll, "*")
	rctx.SetAllow(utils.NodeWriteShadow, "*")

	if err := rmngrequest.ExtractRequestStruct(request, &req); err != nil {
		return RegisterBulkNodesResponse{}, rmerror.NewRMError(err, "failed to extract request")
	}

	if len(req.AdminGroupNames) > 0 {
		err := node.CreateAdminGroupIfNotExists(rctx, req.AdminGroupNames, req.AdminParentGroupName)
		if err != nil {
			return RegisterBulkNodesResponse{}, rmerror.NewRMError(err, err.Error())
		}
	}

	// Capability-gated policy names + the capabilities list itself are
	// register-only; ExtraEnv keeps them out of the shared launcher.
	requestID, err := bulk_job.Launch(rctx, bulk_job.LaunchParams{
		JobType:              node_reg_req_db.NODE_REG_JOB_TYPE_REGISTER,
		CertFileS3Path:       req.CertFileS3Path,
		AdminGroupNames:      req.AdminGroupNames,
		AdminParentGroupName: req.AdminParentGroupName,
		Tags:                 req.Tags,
		InitialStatusMessage: "Bulk registration request accepted, waiting for container to start",
		ExtraEnv: map[string]string{
			"CAPABILITIES":             strings.Join(req.Capabilities, ","),
			"DEVICE_FILE_POLICY_NAME":  os.Getenv("DEVICE_FILE_POLICY_NAME"),
			"DEVICE_VIDEO_POLICY_NAME": os.Getenv("DEVICE_VIDEO_POLICY_NAME"),
		},
	})
	if err != nil {
		return RegisterBulkNodesResponse{}, err
	}

	return RegisterBulkNodesResponse{
		RequestId: requestID,
		Message:   "Bulk registration request created",
	}, nil
}

// handleListRegistrationJobs is register-only (the update Lambda doesn't
// expose a list endpoint). Today it returns both register and update jobs
// — clients filter on job_type. A server-side ?job_type= filter is tracked
// as future work in docs/en/specs/node_reg.md §10.
func handleListRegistrationJobs(rctx *rmngctx.RmngContext, request events.APIGatewayProxyRequest) (ListRegistrationJobsResponse, error) {
	rctx.SetAllow(utils.NodeAdminRegisterStatus, "*")

	limit := rmngrequest.ParsePageSize(request.QueryStringParameters)
	startKey := request.QueryStringParameters["start_key"]
	statusFilter := request.QueryStringParameters["status"]

	dbClient := node_reg_req_db.NewNodeRegRequestsDB(rctx)
	result, err := dbClient.ListNodeRegRequests(limit, startKey, statusFilter)
	if err != nil {
		return ListRegistrationJobsResponse{}, rmerror.NewRMError(err, "failed to list registration jobs")
	}

	jobs := make([]jobs_api.StatusResponse, 0, len(result.Entries))
	for _, entry := range result.Entries {
		e := entry // avoid loop variable capture
		jobs = append(jobs, jobs_api.EntryToResponse(&e))
	}

	return ListRegistrationJobsResponse{
		Jobs:      jobs,
		PageTotal: len(jobs),
		NextKey:   result.NextKey,
	}, nil
}

func handleRequest(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	var err error
	var response interface{}

	rctx := user.NewContextWithAPIRequest(ctx, request)
	if rctx == nil || rctx.GetAccessor() == nil || rctx.GetAccessor().GetID() == "" {
		rlog.Error(ctx).Msg("Failed to resolve user context for admin nodes endpoint")
		return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus("Unauthorized")), nil
	}
	userAccessor, ok := rctx.GetAccessor().(*user.User)
	if !ok {
		rlog.Error(rctx).Msg("Accessor is not a user; rejecting admin nodes request")
		return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus("Unauthorized")), nil
	}
	if !userAccessor.IsSuperAdmin(rctx) {
		rlog.Error(rctx).Msg("User is not authorized for admin nodes endpoints")
		return utils.APIGwRespJSON(http.StatusForbidden, utils.NewAPIStatus("Forbidden")), nil
	}

	// Route based on path and method. Order matters: deeper sub-resource paths
	// (e.g. /failed-nodes) must be matched before the broader {requestId}
	// status route, otherwise the status handler would claim them.
	path := request.Path
	method := request.HTTPMethod

	switch {
	case path == "/v1/admin/nodes/registration-jobs" && method == "POST":
		response, err = handleRegisterBulkNodes(rctx, request)
		if err != nil {
			rlog.Error(rctx).Err(err).Send()
			return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus(err.Error())), nil
		}
		// Return 202 Accepted for async bulk registration
		return utils.APIGwRespJSON(http.StatusAccepted, response), nil

	case path == "/v1/admin/nodes/registration-jobs" && method == "GET":
		response, err = handleListRegistrationJobs(rctx, request)
		if err != nil {
			rlog.Error(rctx).Err(err).Send()
			return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus(err.Error())), nil
		}
		return utils.APIGwRespJSON(http.StatusOK, response), nil

	case strings.HasPrefix(path, "/v1/admin/nodes/registration-jobs/") &&
		strings.HasSuffix(path, "/failed-nodes") && method == "GET":
		response, err = jobs_api.ListFailedNodes(rctx, request, node_reg_req_db.NODE_REG_JOB_TYPE_REGISTER)
		if err != nil {
			if errors.Is(err, jobs_api.ErrJobNotFound) {
				return utils.APIGwRespJSON(http.StatusNotFound, utils.NewAPIStatus(notFoundMessage)), nil
			}
			rlog.Error(rctx).Err(err).Send()
			return utils.APIGwRespJSON(http.StatusInternalServerError, map[string]string{
				"status":  "error",
				"message": err.Error(),
			}), nil
		}
		return utils.APIGwRespJSON(http.StatusOK, response), nil

	case strings.HasPrefix(path, "/v1/admin/nodes/registration-jobs/") && method == "GET":
		// GET /v1/admin/nodes/registration-jobs/{requestId}
		response, err = jobs_api.Status(rctx, request, node_reg_req_db.NODE_REG_JOB_TYPE_REGISTER)
		if err != nil {
			if errors.Is(err, jobs_api.ErrJobNotFound) {
				return utils.APIGwRespJSON(http.StatusNotFound, utils.NewAPIStatus(notFoundMessage)), nil
			}
			rlog.Error(rctx).Err(err).Send()
			return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus(err.Error())), nil
		}
		return utils.APIGwRespJSON(http.StatusOK, response), nil

	case path == "/v1/admin/nodes":
		if method != "POST" {
			return utils.APIGwRespJSON(http.StatusMethodNotAllowed, utils.NewAPIStatus("Method not allowed")), nil
		}
		// POST /v1/admin/nodes (single node registration)
		response, err = handleRegisterSingleNode(rctx, request)
		if err != nil {
			if errors.Is(err, node.ErrNodeAlreadyRegistered) {
				rlog.Warn(rctx).Err(err).Send()
				return utils.APIGwRespJSON(http.StatusConflict, utils.NewAPIStatus(err.Error())), nil
			}
			rlog.Error(rctx).Err(err).Send()
			return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus(err.Error())), nil
		}
		return utils.APIGwRespJSON(http.StatusCreated, response), nil

	default:
		return utils.APIGwRespJSON(http.StatusNotFound, utils.NewAPIStatus("Not found")), nil
	}
}

func main() {
	lambda.Start(handleRequest)
}
