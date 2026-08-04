// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package jobs_api centralises the read-side handlers shared by the
// registration and update Lambdas: status lookup and the paginated
// failed-nodes audit list. Each function takes the expected job_type so
// cross-flow scoping is preserved — a request looking up a registration
// job_id on /update-jobs/{id} still gets a 404 instead of the registration
// row.
//
// The re-uploadable, cert-bearing failed-rows CSV is written to S3 by the
// container at end-of-job (see docs/en/specs/node_reg.md §3.5.5); the status
// response carries its key and a presigned download URL. The bulk-job
// kick-off (POST .../jobs) lives in nodeadmin/bulk_job; this package handles
// everything after the job row exists.
package jobs_api

import (
	"errors"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"time"

	"github.com/aws/aws-lambda-go/events"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/s3util"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_reg_failed_nodes_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_reg_req_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngrequest"
)

// failedFileDownloadURLExpiry bounds the presigned GET URL returned on the
// status. Regenerated on every status call, so it never goes stale; short
// enough to limit the blast radius of a leaked link.
const failedFileDownloadURLExpiry = 15 * time.Minute

// ErrJobNotFound is returned by Status / ListFailedNodes when the
// parent job row is missing OR when the row belongs to a different
// bulk-job flow than the caller expected (cross-flow isolation).
// Callers translate this to a 404 with their preferred message.
var ErrJobNotFound = errors.New("job not found")

// StatusResponse is the body of GET .../jobs/{requestId}. Identical
// shape across register and update flows — JobType carries the
// distinction.
type StatusResponse struct {
	RequestID            string   `json:"request_id"`
	UserID               string   `json:"user_id,omitempty"`
	JobType              string   `json:"job_type,omitempty"`
	TotalCount           int      `json:"total_nodes"`
	SuccessCount         *int     `json:"success_count,omitempty"`
	FailedCount          *int     `json:"failed_count,omitempty"`
	CreatedAt            int64    `json:"created_at,omitempty"`
	LastUpdatedAt        int64    `json:"last_updated_at,omitempty"`
	Status               string   `json:"status"`
	Message              string   `json:"message,omitempty"`
	AdminGroupNames      []string `json:"admin_group_names,omitempty"`
	AdminParentGroupName string   `json:"admin_parent_group_name,omitempty"`
	Tags                 []string `json:"tags,omitempty"`
	CertFileS3Path       string   `json:"cert_file_s3_path,omitempty"`
	// FailedFileS3Path is the S3 key of the re-uploadable cert-bearing
	// failed-rows CSV (§3.5.5); FailedFileDownloadURL is a short-lived
	// presigned GET for it. Both present only when the job completed with
	// failures and the container's S3 write succeeded.
	FailedFileS3Path      string `json:"failed_file_s3_path,omitempty"`
	FailedFileDownloadURL string `json:"failed_file_download_url,omitempty"`
}

// ListFailedNodesResponse is the body of GET .../jobs/{requestId}/failed-nodes.
type ListFailedNodesResponse struct {
	FailedNodes []node_reg_failed_nodes_db.NodeRegFailedNodeEntry `json:"failed_nodes"`
	PageTotal   int                                               `json:"page_total"`
	NextKey     string                                            `json:"next_key,omitempty"`
}

// LoadJob fetches the parent job row for a request_id and enforces the
// cross-flow isolation: a job whose stored job_type doesn't match
// expectedJobType is reported as ErrJobNotFound rather than returning
// the row. node_reg_reqs is shared between flows and request_id is
// not flow-scoped — so an update job_id queried on /registration-jobs/
// must look like a 404, not a leak.
//
// Callers that only need existence can ignore the returned entry;
// callers that need fields like cert_file_s3_path use it.
func LoadJob(rctx *rmngctx.RmngContext, requestID, expectedJobType string) (*node_reg_req_db.NodeRegRequestsEntry, error) {
	entry, err := node_reg_req_db.NewNodeRegRequestsDB(rctx).GetNodeRegRequest(requestID)
	if err != nil {
		return nil, err
	}
	if entry == nil || entry.RequestID == "" {
		return nil, ErrJobNotFound
	}
	if node_reg_req_db.JobTypeOrDefault(entry.JobType) != expectedJobType {
		return nil, ErrJobNotFound
	}
	return entry, nil
}

// Status implements GET .../jobs/{requestId}. Returns ErrJobNotFound
// on a missing row or a cross-flow mismatch; caller translates to 404.
func Status(rctx *rmngctx.RmngContext, request events.APIGatewayProxyRequest, expectedJobType string) (StatusResponse, error) {
	rctx.SetAllow(utils.NodeAdminRegisterStatus, "*")

	requestID := request.PathParameters["requestId"]
	if requestID == "" {
		return StatusResponse{}, rmerror.NewRMError(nil, "request_id is required")
	}

	entry, err := LoadJob(rctx, requestID, expectedJobType)
	if err != nil {
		return StatusResponse{}, err
	}

	resp := entryToResponse(entry)

	// Mint a fresh presigned download URL for the failed-rows CSV when one
	// exists. A signing failure is non-fatal: the status (counts, S3 key)
	// is still returned, and a later poll can re-mint the URL.
	if resp.FailedFileS3Path != "" {
		if bucket, key, perr := s3util.GetBucketKey(resp.FailedFileS3Path); perr == nil {
			if url, uerr := s3util.GetPresignedDownloadURL(rctx.Context, bucket, key, failedFileDownloadURLExpiry); uerr == nil {
				resp.FailedFileDownloadURL = url
			} else {
				rlog.Error(rctx).Err(uerr).Msg("failed to presign failed-nodes CSV download URL")
			}
		} else {
			rlog.Error(rctx).Err(perr).Msg("invalid failed_file_s3_path on job row")
		}
	}

	return resp, nil
}

// ListFailedNodes implements GET .../jobs/{requestId}/failed-nodes.
func ListFailedNodes(rctx *rmngctx.RmngContext, request events.APIGatewayProxyRequest, expectedJobType string) (ListFailedNodesResponse, error) {
	rctx.SetAllow(utils.NodeAdminRegisterStatus, "*")

	requestID := request.PathParameters["requestId"]
	if requestID == "" {
		return ListFailedNodesResponse{}, rmerror.NewRMError(nil, "request_id is required")
	}
	if _, err := LoadJob(rctx, requestID, expectedJobType); err != nil {
		return ListFailedNodesResponse{}, err
	}

	limit := rmngrequest.ParsePageSize(request.QueryStringParameters)
	startKey := request.QueryStringParameters["start_key"]

	out, err := node_reg_failed_nodes_db.NewNodeRegFailedNodesDB(rctx).
		ListFailures(requestID, limit, startKey)
	if err != nil {
		return ListFailedNodesResponse{}, rmerror.NewRMError(err, "failed to list failed nodes")
	}

	return ListFailedNodesResponse{
		FailedNodes: out.Entries,
		PageTotal:   len(out.Entries),
		NextKey:     out.NextKey,
	}, nil
}

// EntryToResponse converts a node_reg_reqs DB row to the wire shape.
// Exported because the register Lambda's list endpoint maps over a
// slice of these.
func EntryToResponse(entry *node_reg_req_db.NodeRegRequestsEntry) StatusResponse {
	return entryToResponse(entry)
}

func entryToResponse(entry *node_reg_req_db.NodeRegRequestsEntry) StatusResponse {
	return StatusResponse{
		RequestID:            entry.RequestID,
		UserID:               entry.UserID,
		JobType:              node_reg_req_db.JobTypeOrDefault(entry.JobType),
		TotalCount:           entry.TotalCount,
		SuccessCount:         entry.SuccessCount,
		FailedCount:          entry.FailedCount,
		CreatedAt:            entry.CreatedAt,
		LastUpdatedAt:        entry.LastUpdatedAt,
		Status:               entry.Status,
		Message:              entry.Message,
		AdminGroupNames:      entry.AdminGroupNames,
		AdminParentGroupName: entry.AdminParentGroupName,
		Tags:                 entry.Tags,
		CertFileS3Path:       entry.CertFileS3Path,
		FailedFileS3Path:     entry.FailedFileS3Path,
	}
}
