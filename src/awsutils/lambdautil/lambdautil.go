// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package lambdautil

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

// InvokeAsync invokes the named Lambda function asynchronously with the given payload.
func InvokeAsync(ctx context.Context, functionName string, payload interface{}) error {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	lambdaClient := awscommon.GetLambdaClient()
	_, err = lambdaClient.Invoke(ctx, &lambda.InvokeInput{
		FunctionName:   aws.String(functionName),
		InvocationType: types.InvocationTypeEvent,
		Payload:        payloadBytes,
	})
	return err
}

// InvokeSync invokes the named Lambda synchronously (RequestResponse) and returns
// an error if the invocation or the function itself failed. A
// ResourceNotFoundException — the function is not deployed — is treated as a no-op
// and returns nil, so callers can invoke optional hooks provided by a
// separately-deployed stack without knowing whether that stack exists.
// InvokeSync invokes functionName synchronously and returns its response
// payload. A not-yet-deployed function is treated as a no-op: (nil, nil). A
// function error is surfaced as an error.
func InvokeSync(ctx context.Context, functionName string, payload interface{}) ([]byte, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	lambdaClient := awscommon.GetLambdaClient()
	out, err := lambdaClient.Invoke(ctx, &lambda.InvokeInput{
		FunctionName:   aws.String(functionName),
		InvocationType: types.InvocationTypeRequestResponse,
		Payload:        payloadBytes,
	})
	if err != nil {
		var notFound *types.ResourceNotFoundException
		if errors.As(err, &notFound) {
			return nil, nil
		}
		return nil, err
	}
	if out.FunctionError != nil {
		return nil, fmt.Errorf("lambda %q returned a function error: %s", functionName, string(out.Payload))
	}
	return out.Payload, nil
}
