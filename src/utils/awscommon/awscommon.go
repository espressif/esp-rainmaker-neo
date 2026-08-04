// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package awscommon

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
)

func GetPartition() string {
	if strings.Contains(GetRmngRegion(), "cn") {
		return "aws-cn"
	}
	return "aws"
}

// GetRmngRegion returns the AWS region for data-plane calls (DynamoDB, IoT, SSM, etc.).
// When RMNG_REGION is set (e.g. for the alexa_skill Lambda in a separate region), that is used
// so the Lambda can access resources in the rmng deployment region. Otherwise AWS_REGION is used.
func GetRmngRegion() string {
	if r := os.Getenv("RMNG_REGION"); r != "" {
		return r
	}
	return os.Getenv("AWS_REGION")
}

func GetAccountId() string {
	return os.Getenv("AWS_ACCOUNT_ID")
}

var SERVICE_RESOURCE_ID_COLON_SEPARATOR = map[string]bool{
	"lambda":         true,
	"logs":           true,
	"secretsmanager": true,
}

var SERVICE_GLOBAL = map[string]bool{
	"iam": true,
	"s3":  true,
}

var SERVICE_UNIQUE_ACROSS_ACCOUNTS = map[string]bool{
	"s3": true,
}

// Example Output: arn:aws:iam:region:123456789012:role/esprm-analytics-opensearch-admin-role-ap-south-1
func CreateAwsArnFromName(serviceName, resourceType, resourceName string) string {
	region := GetRmngRegion()
	if SERVICE_GLOBAL[serviceName] {
		region = ""
	}

	accountId := GetAccountId()
	if SERVICE_UNIQUE_ACROSS_ACCOUNTS[serviceName] {
		accountId = ""
	}

	var resourceId string
	if resourceType == "" {
		resourceId = resourceName
	} else {
		if SERVICE_RESOURCE_ID_COLON_SEPARATOR[serviceName] {
			resourceId = resourceType + ":" + resourceName
		} else {
			resourceId = resourceType + "/" + resourceName
		}
	}

	return arn.ARN{
		Partition: GetPartition(),
		Service:   serviceName,
		Region:    region,
		AccountID: accountId,
		Resource:  resourceId,
	}.String()
}

func ConvertMapToAwsMap(mapStr map[string]string) []types.KeyValuePair {
	awsMap := make([]types.KeyValuePair, 0, len(mapStr))
	for key, value := range mapStr {
		awsMap = append(awsMap, types.KeyValuePair{
			Name:  &key,
			Value: &value,
		})
	}
	return awsMap
}

func ConvertAwsMapToMap(awsMap []types.KeyValuePair) map[string]string {
	mapStr := make(map[string]string, len(awsMap))
	for _, kv := range awsMap {
		if kv.Name != nil && kv.Value != nil {
			mapStr[*kv.Name] = *kv.Value
		}
	}
	return mapStr
}

// IsSQSEvent reports whether the raw Lambda payload is an SQS event-source
// invocation. It detects the top-level "Records" key, which only SQS carries.
// Presence of the key (not its value) is the discriminator — an empty SQS
// batch still carries the key and must return an SQSEventResponse.
func IsSQSEvent(raw json.RawMessage) bool {
	var probe struct {
		Records *json.RawMessage `json:"Records"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return false
	}
	return probe.Records != nil
}
