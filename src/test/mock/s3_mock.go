// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mock

import (
	"context"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

type S3ClientMock struct {
	PresignClient awscommon.S3PresignClientInterface
	Buckets       map[string]map[string]any //map of bucket name to map of key to object
	// ForcePutObjectErr, when non-nil, makes every PutObject return it.
	// Lets tests exercise write-failure paths (e.g. the container's
	// non-fatal failed-nodes CSV write) without an HTTP layer.
	ForcePutObjectErr error
}

func NewS3ClientMock() *S3ClientMock {
	return &S3ClientMock{
		PresignClient: &MockS3PresignClient{},
		Buckets:       make(map[string]map[string]any),
	}
}

// GetPresignClient returns a mock presign client
func (m *S3ClientMock) GetPresignClient() awscommon.S3PresignClientInterface {
	if m.PresignClient == nil {
		m.PresignClient = awscommon.GetS3PresignClient()
	}
	return m.PresignClient
}

func (m *S3ClientMock) PutObject(ctx context.Context, input *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	if m.ForcePutObjectErr != nil {
		return nil, m.ForcePutObjectErr
	}

	bucket := *input.Bucket
	key := *input.Key

	if _, exists := m.Buckets[bucket]; !exists {
		return nil, &types.NoSuchBucket{
			Message: aws.String("The specified bucket does not exist"),
		}
	}

	// IfNoneMatch="*" means "only proceed if the object does NOT exist"
	if input.IfNoneMatch != nil && *input.IfNoneMatch == "*" && m.Buckets[bucket][key] != nil {
		// AWS returns a PreconditionFailed error for failed conditional requests
		return nil, &smithy.GenericAPIError{
			Code:    "PreconditionFailed",
			Message: "At least one of the preconditions you specified did not hold",
			Fault:   smithy.FaultClient,
		}
	}

	if m.Buckets[bucket] == nil {
		m.Buckets[bucket] = make(map[string]any)
	}
	m.Buckets[bucket][key] = input.Body

	return nil, nil
}

func (m *S3ClientMock) GetObject(ctx context.Context, input *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	bucket := *input.Bucket
	key := *input.Key

	if _, exists := m.Buckets[bucket]; !exists {
		return nil, &types.NoSuchBucket{
			Message: aws.String("The specified bucket does not exist"),
		}
	}

	obj, exists := m.Buckets[bucket][key]
	if !exists {
		return nil, &types.NoSuchKey{
			Message: aws.String("The specified key does not exist"),
		}
	}

	// Convert the stored object back to an io.ReadCloser
	var body io.ReadCloser
	if reader, ok := obj.(io.Reader); ok {
		// If it's already a Reader, we need to convert it to ReadCloser
		if readCloser, ok := reader.(io.ReadCloser); ok {
			body = readCloser
		} else {
			// If it's just a Reader, wrap it in a ReadCloser
			body = io.NopCloser(reader)
		}
	} else {
		// If it's stored as something else, try to convert to string and create a reader
		content := obj.(string)
		body = io.NopCloser(strings.NewReader(content))
	}

	return &s3.GetObjectOutput{
		Body: body,
	}, nil
}

func (m *S3ClientMock) CreateBucketDirect(bucketName string) {
	m.Buckets[bucketName] = make(map[string]any)
}

func (m *S3ClientMock) ListBuckets(ctx context.Context, input *s3.ListBucketsInput, optFns ...func(*s3.Options)) (*s3.ListBucketsOutput, error) {
	bucketNames := make([]types.Bucket, 0)
	for bucketName := range m.Buckets {
		bucketNames = append(bucketNames, types.Bucket{Name: aws.String(bucketName)})
	}
	return &s3.ListBucketsOutput{
		Buckets: bucketNames,
	}, nil
}
