// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package ssmutil

import (
	"context"
	"errors"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"os"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/utils"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssm_types "github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

// IsParameterNotFound reports whether err is the SSM "parameter does not exist"
// error, so callers can detect an absent parameter without importing the SDK
// error types themselves.
func IsParameterNotFound(err error) bool {
	var notFound *ssm_types.ParameterNotFound
	return errors.As(err, &notFound)
}

// StoreParameter stores a parameter in AWS SSM Parameter Store as a SecureString
func StoreParameter(ctx context.Context, name, value string) error {
	return StoreParameterWithType(ctx, name, value, ssm_types.ParameterTypeSecureString)
}

// StoreParameterWithType stores a parameter in AWS SSM Parameter Store with specified type
func StoreParameterWithType(ctx context.Context, name, value string, paramType ssm_types.ParameterType) error {
	ssmClient := awscommon.GetSSMClient()

	_, err := ssmClient.PutParameter(ctx, &ssm.PutParameterInput{
		Name:      aws.String(name),
		Value:     aws.String(value),
		Type:      paramType,
		Overwrite: aws.Bool(true),
	})
	if err != nil {
		return rmerror.NewRMError(err, fmt.Sprintf("failed to store parameter %s", name))
	}
	return nil
}

// ErrParameterExists reports that StoreParameterIfAbsent found the parameter
// already set and left it untouched.
var ErrParameterExists = errors.New("parameter already exists")

// StoreParameterIfAbsent writes a parameter only when it does not already
// exist, returning ErrParameterExists otherwise.
//
// Overwrite:false makes this a single atomic operation rather than a
// read-then-write, which matters for write-once material: a check followed by
// a put has a window in which two callers both observe "absent" and the second
// silently replaces the first's value.
func StoreParameterIfAbsent(ctx context.Context, name, value string, paramType ssm_types.ParameterType) error {
	ssmClient := awscommon.GetSSMClient()

	_, err := ssmClient.PutParameter(ctx, &ssm.PutParameterInput{
		Name:      aws.String(name),
		Value:     aws.String(value),
		Type:      paramType,
		Overwrite: aws.Bool(false),
	})
	if err != nil {
		var exists *ssm_types.ParameterAlreadyExists
		if errors.As(err, &exists) {
			return ErrParameterExists
		}
		return rmerror.NewRMError(err, fmt.Sprintf("failed to store parameter %s", name))
	}
	return nil
}

// GetParameter retrieves a parameter from AWS SSM Parameter Store
func getParameter(ctx context.Context, name string, withDecryption bool) (string, error) {
	ssmClient := awscommon.GetSSMClient()

	result, err := ssmClient.GetParameter(ctx, &ssm.GetParameterInput{
		Name:           aws.String(name),
		WithDecryption: aws.Bool(withDecryption),
	})
	if err != nil {
		return "", rmerror.NewRMError(err, fmt.Sprintf("failed to get parameter %s", name))
	}
	return *result.Parameter.Value, nil
}

// getCacheKey generates the environment variable name for caching SSM parameters
func getCacheKey(name string) string {
	return "SSM_" + strings.ToUpper(name)
}

// GetParameterWithCaching retrieves a parameter from AWS SSM Parameter Store with caching.
// It caches the parameter value in an environment variable with SSM_ prefix (uppercase).
// cacheParam defaults to true if not provided.
func GetParameterWithCaching(ctx context.Context, name string, withDecryption bool, cacheParam ...bool) (string, error) {
	shouldCache := utils.GetOptional(cacheParam)
	if len(cacheParam) == 0 {
		shouldCache = true
	}

	cacheKey := getCacheKey(name)
	if shouldCache {
		if cachedValue := os.Getenv(cacheKey); cachedValue != "" {
			return cachedValue, nil
		}
	}

	value, err := getParameter(ctx, name, withDecryption)
	if err != nil {
		return "", err
	}

	if shouldCache && value != "" {
		os.Setenv(cacheKey, value)
	}

	return value, nil
}

// ClearCachedParameter drops a parameter's cached value so the next GetParameterWithCaching
// re-reads it from SSM. Callers that delete or overwrite a cached parameter must use this;
// otherwise the stale value is served for the rest of the process's life.
func ClearCachedParameter(name string) {
	os.Unsetenv(getCacheKey(name))
}

// DeleteParameter deletes a parameter from AWS SSM Parameter Store
// If you have cached the parameter, clear the cached env vars before deleting the parameter.
func DeleteParameter(ctx context.Context, name string) error {
	ssmClient := awscommon.GetSSMClient()

	_, err := ssmClient.DeleteParameter(ctx, &ssm.DeleteParameterInput{
		Name: aws.String(name),
	})
	if err != nil {
		return rmerror.NewRMError(err, fmt.Sprintf("failed to delete parameter %s", name))
	}
	return nil
}
