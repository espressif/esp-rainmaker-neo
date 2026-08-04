// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package push

import (
	"context"
	"encoding/json"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
)

type ListMobilePlatformInfo struct {
	Platform        string `json:"platform"`
	PlatformAppName string `json:"platform_app_name"`
}

// constructPlatformApplicationARN constructs the platform application ARN for the given platform type
func constructPlatformApplicationARN(platformType, platformName string) string {
	// Construct ARN: arn:aws:sns:region:accountId:app/PLATFORM_TYPE/RainMaker
	return awscommon.CreateAwsArnFromName("sns", "app/"+platformType, platformName)
}

// CreatePlatformApplication creates a platform application
func CreatePlatformApplication(ctx context.Context, name, platform string, attributes map[string]string) (string, error) {
	snsClient := awscommon.GetSNSClient()

	input := &sns.CreatePlatformApplicationInput{
		Name:       aws.String(name),
		Platform:   aws.String(platform),
		Attributes: attributes,
	}

	result, err := snsClient.CreatePlatformApplication(ctx, input)
	if err != nil {
		return "", rmerror.NewRMError(err, "Failed to create platform application")
	}

	return *result.PlatformApplicationArn, nil
}

// CreatePlatformEndpoint creates a platform endpoint for the given device token and platform type
func CreatePlatformEndpoint(ctx context.Context, platformType, platformName, deviceToken string) (string, error) {
	// Construct platform application ARN at runtime
	platformAppARN := constructPlatformApplicationARN(platformType, platformName)

	snsClient := awscommon.GetSNSClient()
	input := &sns.CreatePlatformEndpointInput{
		PlatformApplicationArn: aws.String(platformAppARN),
		Token:                  aws.String(deviceToken),
	}

	result, err := snsClient.CreatePlatformEndpoint(ctx, input)
	if err != nil {
		return "", rmerror.NewRMError(err, "Failed to create platform endpoint")
	}

	return *result.EndpointArn, nil
}

// DeletePlatformEndpoint deletes a platform endpoint
func DeletePlatformEndpoint(ctx context.Context, endpointArn string) error {
	snsClient := awscommon.GetSNSClient()

	input := &sns.DeleteEndpointInput{
		EndpointArn: aws.String(endpointArn),
	}

	_, err := snsClient.DeleteEndpoint(ctx, input)
	if err != nil {
		return rmerror.NewRMError(err, "Failed to delete platform endpoint")
	}

	return nil
}

// GetPlatformApplicationAttributes fetches a platform application's attributes from SNS.
// PlatformCredential is redacted before returning so no caller can leak stored secrets: a Google service-account JSON keeps its metadata but loses private_key; any other credential (e.g. an APNS .p8 key) is wholly secret and is dropped.
func GetPlatformApplicationAttributes(ctx context.Context, platformType, platformName string) (map[string]string, error) {
	snsClient := awscommon.GetSNSClient()
	platformAppARN := constructPlatformApplicationARN(platformType, platformName)

	input := &sns.GetPlatformApplicationAttributesInput{
		PlatformApplicationArn: aws.String(platformAppARN),
	}

	output, err := snsClient.GetPlatformApplicationAttributes(ctx, input)
	if err != nil {
		return nil, rmerror.NewRMError(err, "Failed to get platform application attributes")
	}

	attrs := output.Attributes
	if cred := attrs["PlatformCredential"]; cred != "" {
		if redacted, ok := redactServiceAccountJSON(cred); ok {
			attrs["PlatformCredential"] = redacted
		} else {
			delete(attrs, "PlatformCredential")
		}
	}

	return attrs, nil
}

// redactServiceAccountJSON strips private_key from a Google service-account JSON, preserving every other field. It reports false when the credential is not one (opaque secrets have no metadata worth keeping).
func redactServiceAccountJSON(cred string) (string, bool) {
	var sa map[string]interface{}
	if err := json.Unmarshal([]byte(cred), &sa); err != nil {
		return "", false
	}
	if _, hasKey := sa["private_key"]; !hasKey {
		return "", false
	}
	delete(sa, "private_key")

	redacted, err := json.Marshal(sa)
	if err != nil {
		return "", false
	}
	return string(redacted), true
}

// UpdatePlatformApplication updates a platform application's attributes
func UpdatePlatformApplication(ctx context.Context, platformType, platformName string, attributes map[string]string) error {
	snsClient := awscommon.GetSNSClient()
	platformAppARN := constructPlatformApplicationARN(platformType, platformName)

	input := &sns.SetPlatformApplicationAttributesInput{
		PlatformApplicationArn: aws.String(platformAppARN),
		Attributes:             attributes,
	}

	_, err := snsClient.SetPlatformApplicationAttributes(ctx, input)
	if err != nil {
		return rmerror.NewRMError(err, "Failed to update platform application")
	}

	return nil
}

// DeletePlatformApplication deletes a platform application
func DeletePlatformApplication(ctx context.Context, platformType, platformName string) error {
	snsClient := awscommon.GetSNSClient()
	platformAppARN := constructPlatformApplicationARN(platformType, platformName)

	input := &sns.DeletePlatformApplicationInput{
		PlatformApplicationArn: aws.String(platformAppARN),
	}

	_, err := snsClient.DeletePlatformApplication(ctx, input)
	if err != nil {
		return rmerror.NewRMError(err, "Failed to delete platform application")
	}

	return nil
}

// ListPlatformApplications lists all platform applications from AWS SNS
func ListPlatformApplications(ctx context.Context) ([]ListMobilePlatformInfo, error) {
	snsClient := awscommon.GetSNSClient()

	var platforms []ListMobilePlatformInfo

	// Start with empty NextToken for first request
	var nextToken *string

	for {
		input := &sns.ListPlatformApplicationsInput{
			NextToken: nextToken,
		}

		output, err := snsClient.ListPlatformApplications(ctx, input)
		if err != nil {
			return nil, rmerror.NewRMError(err, "Failed to list platform applications")
		}

		// Extract platform type and app name from each platform application
		for _, app := range output.PlatformApplications {
			if app.PlatformApplicationArn != nil {
				arn := *app.PlatformApplicationArn

				// Parse ARN to extract platform type and app name
				// ARN format: arn:aws:sns:region:account:app/PLATFORM_TYPE/app_name
				parts := strings.Split(arn, "/")
				if len(parts) >= 3 {
					platformType := parts[1] // This will be APNS, APNS_SANDBOX, GCM, etc.
					appName := parts[2]      // This will be the app name (e.g., RainMaker, MyApp, etc.)
					platforms = append(platforms, ListMobilePlatformInfo{
						Platform:        platformType,
						PlatformAppName: appName,
					})
				}
			}
		}

		// Check if there are more pages
		if output.NextToken == nil {
			break
		}
		nextToken = output.NextToken
	}

	return platforms, nil
}
