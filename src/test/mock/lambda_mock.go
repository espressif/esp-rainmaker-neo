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
	// Permissions is keyed by PermissionKey: Lambda scopes a statement id to its function, so one function can hold several.
	Permissions map[string]lambda.AddPermissionInput
	InvokeError error
	InvokeCalls []lambda.InvokeInput
}

// PermissionKey is the Permissions key for a function's statement: ("fn", "Stmt") -> "fn/Stmt".
func PermissionKey(functionName, statementID string) string {
	return functionName + "/" + statementID
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
	m.Permissions[PermissionKey(aws.ToString(params.FunctionName), aws.ToString(params.StatementId))] = *params

	return &lambda.AddPermissionOutput{}, nil
}

func (m *LambdaMock) RemovePermission(ctx context.Context, params *lambda.RemovePermissionInput, optFns ...func(*lambda.Options)) (*lambda.RemovePermissionOutput, error) {
	if m.RemovePermissionError != nil {
		return nil, m.RemovePermissionError
	}

	delete(m.Permissions, PermissionKey(aws.ToString(params.FunctionName), aws.ToString(params.StatementId)))

	return &lambda.RemovePermissionOutput{}, nil
}

// GetPolicy renders the function's statements the way Lambda does, and answers ResourceNotFoundException when it has none.
func (m *LambdaMock) GetPolicy(ctx context.Context, params *lambda.GetPolicyInput, optFns ...func(*lambda.Options)) (*lambda.GetPolicyOutput, error) {
	type statement struct {
		Sid string `json:"Sid"`
	}
	var statements []statement
	for _, perm := range m.Permissions {
		if aws.ToString(perm.FunctionName) == aws.ToString(params.FunctionName) {
			statements = append(statements, statement{Sid: aws.ToString(perm.StatementId)})
		}
	}
	if len(statements) == 0 {
		return nil, &types.ResourceNotFoundException{Message: aws.String("The resource you requested does not exist.")}
	}
	policy, err := json.Marshal(map[string]interface{}{"Version": "2012-10-17", "Statement": statements})
	if err != nil {
		return nil, err
	}
	return &lambda.GetPolicyOutput{Policy: aws.String(string(policy))}, nil
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
