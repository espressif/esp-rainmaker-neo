// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mock

import (
	"context"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"

	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// MockS3PresignClient is a mock implementation of the S3PresignClientInterface
type MockS3PresignClient struct {
}

func NewS3PresignClientMock() *MockS3PresignClient {
	return &MockS3PresignClient{}
}

// PresignPutObject mocks the PresignPutObject method
func (m *MockS3PresignClient) PresignPutObject(ctx context.Context, input *s3.PutObjectInput, optFns ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error) {
	// Construct a mock S3 presigned URL that includes bucket and key information
	bucket := *input.Bucket
	key := *input.Key

	//This is a hackish way for testing putobject via presigned url. In case of s3, invoking the url stores the file, here we are storing beforehand as we will have to setup an http server to handle the presigned url.
	mockS3Client := awscommon.GetS3Client()
	_, err := mockS3Client.PutObject(ctx, input)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to put object")
	}

	return &v4.PresignedHTTPRequest{
		URL: "https://" + bucket + ".s3.amazonaws.com/" + key + "?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=mock-credential&X-Amz-SignedHeaders=host&X-Amz-Signature=mock-signature",
	}, nil
}

// PresignGetObject mocks the PresignGetObject method. Unlike the put path it
// does not touch the object store — signing a download URL is a read-only
// operation and does not require the object to already exist in the mock.
func (m *MockS3PresignClient) PresignGetObject(ctx context.Context, input *s3.GetObjectInput, optFns ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error) {
	bucket := *input.Bucket
	key := *input.Key

	return &v4.PresignedHTTPRequest{
		URL: "https://" + bucket + ".s3.amazonaws.com/" + key + "?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=mock-credential&X-Amz-SignedHeaders=host&X-Amz-Signature=mock-signature&response-content-disposition=attachment",
	}, nil
}
