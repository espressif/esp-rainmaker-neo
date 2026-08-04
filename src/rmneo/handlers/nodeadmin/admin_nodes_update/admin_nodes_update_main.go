// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"net/http"
	"strings"

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
// a request_id that doesn't exist OR that belongs to a register job
// (cross-flow scoping). Kept Lambda-local so the message reads right
// for clients hitting /update-jobs.
const notFoundMessage = "update job not found"

// BulkUpdateNodesRequest is the payload for POST /v1/admin/nodes/update-jobs.
//
// The CSV at CertFileS3Path must contain a node_id column. A `certs` column
// is optional per row: empty leaves the cert binding alone; non-empty
// triggers a cloud-side cert update (replacing a mistakenly-registered PEM
// with the correct one — replace-and-deactivate semantics, no device-side
// push). The JSON field name keeps cert_file_s3_path in sync with registration
// so the eager failed-rows CSV (§3.5.5) and re-submission work identically
// across both flows.
type BulkUpdateNodesRequest struct {
	CertFileS3Path       string   `json:"cert_file_s3_path" validate:"required,startswith=s3://"`
	AdminGroupNames      []string `json:"admin_group_names,omitempty" validate:"omitempty,dive,required"`
	AdminParentGroupName string   `json:"admin_parent_group_name,omitempty"`
	Tags                 []string `json:"tags,omitempty" validate:"omitempty,dive,required,std_tag_format"`
}

type BulkUpdateNodesResponse struct {
	Status    string `json:"status"`
	RequestId string `json:"request_id"`
	Message   string `json:"message,omitempty"`
}

// Test-facing aliases so the in-package _test.go file (also package main)
// keeps referring to the Lambda-local type names while the actual definitions
// live in jobs_api. Same wire shape either way.
type (
	UpdateNodeStatusResponse = jobs_api.StatusResponse
	ListFailedNodesResponse  = jobs_api.ListFailedNodesResponse
)

func handleBulkUpdateNodes(rctx *rmngctx.RmngContext, request events.APIGatewayProxyRequest) (BulkUpdateNodesResponse, error) {
	var req BulkUpdateNodesRequest

	// TODO: Once we figure out rbac for admins, remove this
	rctx.SetAllow(utils.NodeAdminAll, "*")
	rctx.SetAllow(utils.NodeWriteShadow, "*")
	rctx.SetAllow(utils.NodeAll, "*")

	if err := rmngrequest.ExtractRequestStruct(request, &req); err != nil {
		return BulkUpdateNodesResponse{}, rmerror.NewRMError(err, "failed to extract request")
	}

	// Phase 1 must do something useful per row — reject jobs whose request body
	// has no defaults for tags or admin_group_names. Per-row CSV columns can
	// still add more, but a body with no defaults plus a CSV with only
	// node_id rows would be a no-op. Accept and let the per-row logic decide.
	if len(req.AdminGroupNames) == 0 && len(req.Tags) == 0 {
		// Soft warning rather than hard reject — the CSV may still carry
		// per-row metadata via extra columns / admin_groups column.
		rlog.Warn(rctx).Msg("update job started with no request-level admin_group_names or tags; per-row CSV columns must supply the updates")
	}

	if len(req.AdminGroupNames) > 0 {
		err := node.CreateAdminGroupIfNotExists(rctx, req.AdminGroupNames, req.AdminParentGroupName)
		if err != nil {
			return BulkUpdateNodesResponse{}, rmerror.NewRMError(err, err.Error())
		}
	}

	// Reuses the shared container image and task definition with the
	// registration flow; the JOB_TYPE env var inside LaunchParams is what
	// tells the container which per-row code path to take.
	requestID, err := bulk_job.Launch(rctx, bulk_job.LaunchParams{
		JobType:              node_reg_req_db.NODE_REG_JOB_TYPE_UPDATE,
		CertFileS3Path:       req.CertFileS3Path,
		AdminGroupNames:      req.AdminGroupNames,
		AdminParentGroupName: req.AdminParentGroupName,
		Tags:                 req.Tags,
		InitialStatusMessage: "Bulk update request accepted, waiting for container to start",
	})
	if err != nil {
		return BulkUpdateNodesResponse{}, err
	}

	return BulkUpdateNodesResponse{
		Status:    "success",
		RequestId: requestID,
		Message:   "Bulk update request created",
	}, nil
}

func handleRequest(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	var err error
	var response interface{}

	rctx := user.NewContextWithAPIRequest(ctx, request)
	// Fail closed: never fall back to a SystemActor. A nil/empty context means
	// the caller's identity did not resolve to a real user; a SystemActor holds
	// *:* permissions and would bypass the super-admin gate below.
	if rctx == nil || rctx.GetAccessor() == nil || rctx.GetAccessor().GetID() == "" {
		rlog.Error(ctx).Msg("Failed to resolve user context for admin update request")
		return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus("Unauthorized")), nil
	}

	// Super-admin gate rejects by default: a non-*user.User accessor (e.g. a
	// system actor) must not be allowed through.
	userAccessor, ok := rctx.GetAccessor().(*user.User)
	if !ok || !userAccessor.IsSuperAdmin(rctx) {
		rlog.Error(rctx).Msg("User is not authorized")
		return utils.APIGwRespJSON(http.StatusForbidden, utils.NewAPIStatus("Forbidden")), nil
	}

	path := request.Path
	method := request.HTTPMethod

	switch {
	case path == "/v1/admin/nodes/update-jobs" && method == "POST":
		response, err = handleBulkUpdateNodes(rctx, request)
		if err != nil {
			rlog.Error(rctx).Err(err).Send()
			return utils.APIGwRespJSON(http.StatusInternalServerError, BulkUpdateNodesResponse{
				Status:  "error",
				Message: err.Error(),
			}), nil
		}
		return utils.APIGwRespJSON(http.StatusAccepted, response), nil

	case strings.HasPrefix(path, "/v1/admin/nodes/update-jobs/") &&
		strings.HasSuffix(path, "/failed-nodes") && method == "GET":
		response, err = jobs_api.ListFailedNodes(rctx, request, node_reg_req_db.NODE_REG_JOB_TYPE_UPDATE)
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

	case strings.HasPrefix(path, "/v1/admin/nodes/update-jobs/") && method == "GET":
		response, err = jobs_api.Status(rctx, request, node_reg_req_db.NODE_REG_JOB_TYPE_UPDATE)
		if err != nil {
			if errors.Is(err, jobs_api.ErrJobNotFound) {
				return utils.APIGwRespJSON(http.StatusNotFound, utils.NewAPIStatus(notFoundMessage)), nil
			}
			rlog.Error(rctx).Err(err).Send()
			return utils.APIGwRespJSON(http.StatusInternalServerError, map[string]interface{}{
				"status":  "error",
				"message": err.Error(),
			}), nil
		}
		return utils.APIGwRespJSON(http.StatusOK, response), nil

	default:
		return utils.APIGwRespJSON(http.StatusNotFound, utils.NewAPIStatus("Not found")), nil
	}
}

func main() {
	lambda.Start(handleRequest)
}
