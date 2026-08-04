// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mock

import (
	"context"
	"encoding/json"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

var MockLambdaClient = &LambdaMock{}

type LambdaHandler func(ctx context.Context, payload []byte) ([]byte, error)

var LambdaHandlerMap = make(map[string]LambdaHandler)

type LambdaMock struct {
	AddPermissionError    error
	RemovePermissionError error
	Permissions           map[string]lambda.AddPermissionInput
	InvokeError           error
	InvokeCalls           []lambda.InvokeInput
}

func NewLambdaMock() *LambdaMock {
	return &LambdaMock{
		Permissions: make(map[string]lambda.AddPermissionInput),
		InvokeCalls: make([]lambda.InvokeInput, 0),
	}
}

func (m *LambdaMock) AddPermission(ctx context.Context, params *lambda.AddPermissionInput, optFns ...func(*lambda.Options)) (*lambda.AddPermissionOutput, error) {
	if m.AddPermissionError != nil {
		return nil, m.AddPermissionError
	}

	if m.Permissions == nil {
		m.Permissions = make(map[string]lambda.AddPermissionInput)
	}
	m.Permissions[*params.FunctionName] = *params

	return &lambda.AddPermissionOutput{}, nil
}

func (m *LambdaMock) RemovePermission(ctx context.Context, params *lambda.RemovePermissionInput, optFns ...func(*lambda.Options)) (*lambda.RemovePermissionOutput, error) {
	if m.RemovePermissionError != nil {
		return nil, m.RemovePermissionError
	}

	if m.Permissions != nil {
		// Remove permission by finding the function name
		for funcName, perm := range m.Permissions {
			if aws.ToString(perm.StatementId) == aws.ToString(params.StatementId) {
				delete(m.Permissions, funcName)
				break
			}
		}
	}

	return &lambda.RemovePermissionOutput{}, nil
}

func (m *LambdaMock) Invoke(ctx context.Context, params *lambda.InvokeInput, optFns ...func(*lambda.Options)) (*lambda.InvokeOutput, error) {
	if m.InvokeError != nil {
		return nil, m.InvokeError
	}

	if m.InvokeCalls == nil {
		m.InvokeCalls = make([]lambda.InvokeInput, 0)
	}
	m.InvokeCalls = append(m.InvokeCalls, *params)

	handler, exists := LambdaHandlerMap[*params.FunctionName]
	if !exists {
		// Match real Lambda: invoking a function that does not exist returns a
		// ResourceNotFoundException error, not a FunctionError response. Callers
		// that treat an absent function as a no-op (e.g. optional lifecycle hooks
		// via lambdautil.InvokeSync) depend on this shape.
		return nil, &types.ResourceNotFoundException{
			Message: aws.String("Function not found: " + *params.FunctionName),
		}
	}

	responsePayload, err := handler(ctx, params.Payload)
	if err != nil {
		statusCode := int32(500)
		errorResponse := map[string]string{
			"errorMessage": err.Error(),
			"errorType":    "HandlerError",
		}
		errorPayload, _ := json.Marshal(errorResponse)
		return &lambda.InvokeOutput{
			StatusCode:    statusCode,
			Payload:       errorPayload,
			FunctionError: aws.String("Unhandled"),
		}, nil
	}

	statusCode := int32(200)
	return &lambda.InvokeOutput{
		StatusCode: statusCode,
		Payload:    responsePayload,
	}, nil
}
