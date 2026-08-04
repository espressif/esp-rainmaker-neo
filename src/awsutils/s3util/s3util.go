// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package s3util

import (
	"context"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// GetPresignedURL generates a presigned URL for the given operation
func GetPresignedURL(ctx context.Context, bucketName, key string, expires time.Duration, overwrite bool) (string, error) {
	presignClient := awscommon.GetS3PresignClient()

	input := &s3.PutObjectInput{
		Bucket: &bucketName,
		Key:    &key,
	}

	if !overwrite {
		//Prevent overwriting the file if it already exists
		input.IfNoneMatch = aws.String("*")
	}

	result, err := presignClient.PresignPutObject(ctx, input, s3.WithPresignExpires(expires))
	if err != nil {
		return "", rmerror.NewRMError(err, "failed to generate presigned URL: "+bucketName+" / "+key)
	}
	return result.URL, nil
}

// GetPresignedDownloadURL generates a presigned GET URL for downloading an
// object. The signer only mints the URL; the actual read happens client-side
// under the caller's existing `s3:GetObject` permission, so no separate read
// path is required. The URL is scoped to this single object and valid for
// `expires`.
func GetPresignedDownloadURL(ctx context.Context, bucketName, key string, expires time.Duration) (string, error) {
	presignClient := awscommon.GetS3PresignClient()

	input := &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(key),
	}

	result, err := presignClient.PresignGetObject(ctx, input, s3.WithPresignExpires(expires))
	if err != nil {
		return "", rmerror.NewRMError(err, "failed to generate presigned download URL: "+bucketName+" / "+key)
	}
	return result.URL, nil
}

func GetS3Path(bucketName, key string) string {
	return "s3://" + bucketName + "/" + key
}

// GetObjectContent fetches an object from S3 and returns its content as bytes
func GetObjectContent(ctx context.Context, bucketName, key string) ([]byte, error) {
	s3Client := awscommon.GetS3Client()

	input := &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(key),
	}

	result, err := s3Client.GetObject(ctx, input)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get object from S3: "+bucketName+" / "+key)
	}
	defer result.Body.Close()

	content, err := io.ReadAll(result.Body)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to read object content: "+bucketName+" / "+key)
	}

	return content, nil
}

func PutObject(ctx context.Context, bucketName, key string, body io.Reader) error {
	s3Client := awscommon.GetS3Client()

	input := &s3.PutObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(key),
		Body:   body,
	}

	_, err := s3Client.PutObject(ctx, input)
	if err != nil {
		return rmerror.NewRMError(err, "failed to put object in S3: "+bucketName+" / "+key)
	}

	return nil
}

// PutObjectWithHeaders writes an object with Content-Type and Cache-Control set; empty values are omitted.
func PutObjectWithHeaders(ctx context.Context, bucketName, key string, body io.Reader, contentType, cacheControl string) error {
	s3Client := awscommon.GetS3Client()

	input := &s3.PutObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(key),
		Body:   body,
	}
	if contentType != "" {
		input.ContentType = aws.String(contentType)
	}
	if cacheControl != "" {
		input.CacheControl = aws.String(cacheControl)
	}

	_, err := s3Client.PutObject(ctx, input)
	if err != nil {
		return rmerror.NewRMError(err, "failed to put object in S3: "+bucketName+" / "+key)
	}

	return nil
}

func ListBuckets() ([]string, error) {
	client := awscommon.GetS3Client()

	result, err := client.ListBuckets(context.TODO(), &s3.ListBucketsInput{})
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to list buckets")
	}

	bucketNames := make([]string, 0)
	for _, bucket := range result.Buckets {
		bucketNames = append(bucketNames, aws.ToString(bucket.Name))
	}
	return bucketNames, nil
}

// GetBucketKey extracts the bucket name and key from an S3 path
func GetBucketKey(s3Path string) (string, string, error) {
	if !strings.HasPrefix(s3Path, "s3://") {
		return "", "", rmerror.NewRMError(nil, "invalid S3 path format")
	}
	s3Path = strings.TrimPrefix(s3Path, "s3://")
	parts := strings.SplitN(s3Path, "/", 2)
	if len(parts) != 2 {
		return "", "", rmerror.NewRMError(nil, "invalid S3 path format")
	}
	return parts[0], parts[1], nil
}
