// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package ecsutil

import (
	"context"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"os"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
)

func RunTask(ctx context.Context, taskDefinitionArn string, subnetIds []string, securityGroupId string, clusterArn string, environmentVariables map[string]string) (string, error) {
	ecsClient := awscommon.GetECSClient()

	envVars := awscommon.ConvertMapToAwsMap(environmentVariables)

	runTaskInput := &ecs.RunTaskInput{
		TaskDefinition: aws.String(taskDefinitionArn),
		Cluster:        aws.String(clusterArn),
		LaunchType:     types.LaunchTypeFargate,
		NetworkConfiguration: &types.NetworkConfiguration{
			AwsvpcConfiguration: &types.AwsVpcConfiguration{
				Subnets:        subnetIds,
				SecurityGroups: []string{securityGroupId},
				AssignPublicIp: types.AssignPublicIpEnabled,
			},
		},
		Overrides: &types.TaskOverride{
			ContainerOverrides: []types.ContainerOverride{
				{
					Name:        aws.String(os.Getenv("BULK_NODE_REG_TASK_CONTAINER_NAME")),
					Environment: envVars,
				},
			},
		},
	}

	out, err := ecsClient.RunTask(ctx, runTaskInput)
	if err != nil {
		return "", rmerror.NewRMError(err, fmt.Sprintf("failed to run task: taskDefinitionArn %s, subnetIds %s, securityGroupId %s, clusterArn %s, environmentVariables %v", taskDefinitionArn, subnetIds, securityGroupId, clusterArn, environmentVariables))
	}
	if len(out.Tasks) == 0 {
		return "", rmerror.NewRMError(fmt.Errorf("no tasks started"), "")
	}
	if len(out.Failures) > 0 {
		return "", rmerror.NewRMError(fmt.Errorf("failed to run task: %s", fmt.Sprintf("%+v", out.Failures)), "")
	}

	rlog.Info(ctx).Msgf("task started: %s", GetTaskId(*out.Tasks[0].TaskArn))
	return GetTaskId(*out.Tasks[0].TaskArn), nil
}

// ECS task ARN format: arn:aws:ecs:region:account-id:task/cluster-name/task-id
func GetTaskId(taskArn string) string {
	if len(strings.Split(taskArn, "/")) == 3 {
		return strings.Split(taskArn, "/")[2]
	}
	return ""
}
