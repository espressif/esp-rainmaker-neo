// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mock

import (
	"context"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"net/http"
	"os"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/nodeadmin/bulk_container"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/httpclient"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
)

type HandlerFunc func() error

// testBulkContainerHandler is a wrapper that creates config from env vars and calls HandleContainer
func testBulkContainerHandler() error {
	config, err := bulk_container.NewContainerConfigFromEnv()
	if err != nil {
		return err
	}
	return bulk_container.HandleContainer(config)
}

var HandlerMap = map[string]HandlerFunc{
	"test_bulk_container": testBulkContainerHandler,
	// Add other handlers as needed
}

type ECSClientMock struct{}

func NewECSClientMock() *ECSClientMock {
	url := "http://169.254.170.2/v4"
	os.Setenv("ECS_CONTAINER_METADATA_URI_V4", url)

	httpClient := httpclient.Get().(*MockHTTPClient)
	httpClient.RegisterResponse(url+"/task", http.MethodGet, http.StatusOK, "{\"TaskARN\": \"arn:aws:ecs:us-east-1:123456789012:task/test-cluster/test-task\"}")

	return &ECSClientMock{}
}

func (m *ECSClientMock) RunTask(ctx context.Context, params *ecs.RunTaskInput, optFns ...func(*ecs.Options)) (*ecs.RunTaskOutput, error) {
	if len(params.Overrides.ContainerOverrides) == 0 {
		return nil, fmt.Errorf("no container overrides provided")
	}

	environmentVariablesToSet := awscommon.ConvertAwsMapToMap(params.Overrides.ContainerOverrides[0].Environment)

	handler, exists := HandlerMap[environmentVariablesToSet["HANDLER_TYPE"]]
	if !exists {
		return nil, fmt.Errorf("unknown handler type: %s", environmentVariablesToSet["HANDLER_TYPE"])
	}

	utils.SetTestEnvVars(environmentVariablesToSet)
	defer utils.ResetTestEnvVars(environmentVariablesToSet)

	err := handler() //RunTask is normally async, but in our mock it is synchronous
	if err != nil {
		return nil, err
	}

	return &ecs.RunTaskOutput{
		Tasks: []types.Task{
			{
				TaskArn: aws.String("arn:aws:ecs:us-east-1:123456789012:task/test-cluster/test-task"),
			},
		},
	}, nil
}
