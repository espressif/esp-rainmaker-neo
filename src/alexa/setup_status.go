// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package alexa_skill

import (
	"context"
	"encoding/json"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/ssmutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"

	ssm_types "github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

// Setup status values. A caller must poll these rather than the credential parameters, which keep the previous skill's values until a run's last step and so say nothing about the run in progress.
const (
	SetupStatusInProgress = "IN_PROGRESS"
	SetupStatusDone       = "DONE"
	SetupStatusFailed     = "FAILED"
)

// SetupStatus is the document stored at AlexaSSMConfigStatusParam and returned as config_status by
// GET /v1/admin/integrations/alexa/configuration. RefID is carried on a failure too, because
// nothing here can delete a skill Amazon has already created -- an operator needs its id.
type SetupStatus struct {
	Status    string `json:"status"`
	RefID     string `json:"ref_id,omitempty"`
	Stage     string `json:"stage,omitempty"`
	Error     string `json:"error,omitempty"`
	UpdatedAt string `json:"updated_at"`
}

// PublishSetupStatus records progress where both the dashboard and the CLI can poll it.
//
// It never returns an error: a lost status line costs a poller its early exit, while
// surfacing one here would replace the real failure with this one.
func PublishSetupStatus(ctx context.Context, status, refID, stage, errMsg string) {
	payload, err := json.Marshal(SetupStatus{
		Status:    status,
		RefID:     refID,
		Stage:     stage,
		Error:     errMsg,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("failed to marshal setup status")
		return
	}
	if err := ssmutil.StoreParameterWithType(ctx, AlexaSSMConfigStatusParam, string(payload), ssm_types.ParameterTypeString); err != nil {
		rlog.Error(ctx).Err(err).Str("status", status).Msg("failed to publish setup status")
	}
}

// ClearSetupStatus drops the last automatic setup's status once a manual setup replaces it, so pollers stop reporting a run that no longer describes the deployment. A status already gone is not an error.
func ClearSetupStatus(ctx context.Context) {
	if err := ssmutil.DeleteParameter(ctx, AlexaSSMConfigStatusParam); err != nil && !isParameterNotFound(err) {
		rlog.Warn(ctx).Err(err).Msg("failed to clear setup status")
	}
}

// GetSetupStatus returns the stored status document, or nil when no automatic setup has run (the normal state of a manually configured deployment).
func GetSetupStatus(ctx context.Context) *SetupStatus {
	// Uncached: a poller reads this every few seconds while the worker rewrites it.
	value, err := ssmutil.GetParameterWithCaching(ctx, AlexaSSMConfigStatusParam, false, false)
	if err != nil {
		// Any other failure also degrades to "no status" so the GET still serves the config, but is logged: it would otherwise hide a run in progress.
		if !isParameterNotFound(err) {
			rlog.Warn(ctx).Err(err).Msg("failed to read setup status")
		}
		return nil
	}
	var status SetupStatus
	if err := json.Unmarshal([]byte(value), &status); err != nil {
		rlog.Warn(ctx).Err(err).Msg("unreadable setup status")
		return nil
	}
	return &status
}
