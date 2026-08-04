// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package bulk_job centralises the steps the registration and update
// Lambdas share when kicking off a bulk job: generate a request ID,
// write the node_reg_reqs row in `requested` state, and dispatch the
// shared Fargate task with the per-job env var set. Both Lambdas
// supply per-job specifics (job_type, status message, extra env vars
// like CAPABILITIES) via LaunchParams; everything else is identical
// and lives here.
package bulk_job

import (
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"os"
	"strings"

	"github.com/google/uuid"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/ecsutil"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_reg_req_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

// LaunchParams is the per-job input to Launch. JobType, the initial
// status message stored on the new node_reg_reqs row, and ExtraEnv are
// the only fields that differ between register and update flows.
type LaunchParams struct {
	JobType              string // node_reg_req_db.NODE_REG_JOB_TYPE_REGISTER or _UPDATE
	CertFileS3Path       string
	AdminGroupNames      []string
	AdminParentGroupName string
	Tags                 []string

	// InitialStatusMessage is the human-readable message stored on the
	// node_reg_reqs row at creation, e.g. "Bulk registration request
	// accepted, waiting for container to start".
	InitialStatusMessage string

	// ExtraEnv is merged on top of the base env var set sent to the
	// Fargate task. Used by the register flow to pass CAPABILITIES and
	// the optional DEVICE_FILE_POLICY_NAME / DEVICE_VIDEO_POLICY_NAME;
	// empty for the update flow.
	ExtraEnv map[string]string
}

// Launch writes the request row and runs the Fargate task. Returns the
// generated request_id so the caller can echo it to the client.
//
// On any failure, the request row may have been written but the task
// not started — the row's status stays at REQUESTED and operators see
// a job that never progresses. This matches the prior inline behaviour
// in each Lambda; addressing it (e.g. by deleting the row on RunTask
// failure or marking it FAILED) is a separate concern.
func Launch(rctx *rmngctx.RmngContext, p LaunchParams) (string, error) {
	requestID := uuid.New().String()

	if err := node_reg_req_db.NewNodeRegRequestsDB(rctx).
		CreateNodeRegRequest(node_reg_req_db.NodeRegRequestsEntry{
			RequestID:            requestID,
			JobType:              p.JobType,
			CertFileS3Path:       p.CertFileS3Path,
			UserID:               rctx.GetID(),
			AdminGroupNames:      p.AdminGroupNames,
			AdminParentGroupName: p.AdminParentGroupName,
			Tags:                 p.Tags,
			Status:               node_reg_req_db.NODE_REG_STATUS_REQUESTED,
			Message:              p.InitialStatusMessage,
		}); err != nil {
		return "", rmerror.NewRMError(err, "failed to create bulk-job request entry")
	}

	env := map[string]string{
		"JOB_TYPE":                  p.JobType,
		"CERT_FILE_S3_PATH":         p.CertFileS3Path,
		"USER_ID":                   rctx.GetID(),
		"ADMIN_GROUP_NAMES":         strings.Join(p.AdminGroupNames, ","),
		"TAGS":                      strings.Join(p.Tags, ","),
		"REQUEST_ID":                requestID,
		"HANDLER_TYPE":              "test_bulk_container", // ignored in prod; surfaced for the test path
		"DEFAULT_THING_POLICY_NAME": os.Getenv("DEFAULT_THING_POLICY_NAME"),
	}
	for k, v := range p.ExtraEnv {
		env[k] = v
	}

	taskDefinitionArn := os.Getenv("BULK_NODE_REG_TASK_TASK_DEFINITION_ARN")
	clusterArn := os.Getenv("BULK_NODE_REG_TASK_CLUSTER_ARN")
	subnetIds := strings.Split(os.Getenv("BULK_NODE_REG_TASK_SUBNET_IDS"), ",")
	securityGroupId := os.Getenv("BULK_NODE_REG_TASK_SECURITY_GROUP_ID")

	if _, err := ecsutil.RunTask(rctx, taskDefinitionArn, subnetIds, securityGroupId, clusterArn, env); err != nil {
		return "", rmerror.NewRMError(err, "failed to start bulk-job task")
	}
	return requestID, nil
}
