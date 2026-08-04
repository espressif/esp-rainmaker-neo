// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package iotdputil

import (
	"context"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"

	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iotdataplane"
)

// UpdateThingShadowWithTags updates the thing's shadow with tags
func UpdateThingShadow(ctx context.Context, thingName, shadowName string, shadowDoc []byte) error {
	iotDataClient := awscommon.GetIoTDataPlaneClient()

	// Update the thing's shadow (classic shadow, no shadow name required)
	updateInput := &iotdataplane.UpdateThingShadowInput{
		ThingName:  aws.String(thingName),
		ShadowName: aws.String(shadowName),
		Payload:    []byte(shadowDoc),
	}

	_, err := iotDataClient.UpdateThingShadow(ctx, updateInput)
	if err != nil {
		return rmerror.NewRMError(err, "failed to update thing shadow")
	}

	rlog.Info(ctx).Str("thingName", thingName).Msg("thing shadow updated successfully")
	return nil
}
