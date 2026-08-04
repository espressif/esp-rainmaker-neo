// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"

	"github.com/espressif/esp-rainmaker-neo/src/alexa"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"

	"github.com/aws/aws-lambda-go/lambda"
)

func handler(ctx context.Context, request alexa_skill.AlexaRequest) (alexa_skill.AlexaResponse, error) {
	rlog.Info(ctx).Interface("request", request).Send()
	resp := alexa_skill.AlexaResponse{}
	err := fmt.Errorf("unsupported namespace: %s", request.Directive.Header.Namespace)

	switch request.Directive.Header.Namespace {
	case "Alexa.Authorization":
		resp, err = alexa_skill.HandleAcceptGrant(ctx, request)
	case "Alexa.Discovery":
		resp, err = alexa_skill.HandleDiscovery(ctx, request)
	case "Alexa.PowerController", "Alexa.BrightnessController",
		"Alexa.ColorController", "Alexa.ColorTemperatureController",
		"Alexa.ToggleController", "Alexa.ModeController":
		resp, err = alexa_skill.HandleControlDirective(ctx, request)
	case "Alexa":
		switch request.Directive.Header.Name {
		case "ReportState":
			resp, err = alexa_skill.HandleReportState(ctx, request)
		default:
			err = fmt.Errorf("unsupported name: %s", request.Directive.Header.Name)
		}
	}

	if err != nil {
		rlog.Error(ctx).Err(err).Send()
	} else {
		rlog.Info(ctx).Interface("resp", resp).Send()
	}
	return resp, err
}

func main() {
	lambda.Start(handler)
}
