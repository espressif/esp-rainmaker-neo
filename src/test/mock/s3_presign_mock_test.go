// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mock_test

import (
	"context"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"io"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/test/mock"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("S3PresignClientMock", func() {
	var (
		client     *mock.MockS3PresignClient
		mockS3     *mock.S3ClientMock
		ctx        context.Context
		testBucket string
	)

	BeforeEach(func() {
		client = mock.NewS3PresignClientMock()
		mockS3 = mock.NewS3ClientMock()
		awscommon.SetS3Client(mockS3)

		ctx = context.Background()
		testBucket = "test-bucket"
		mockS3.CreateBucketDirect(testBucket)
	})

	Describe("NewS3PresignClientMock", func() {
		It("should create a new presign client mock", func() {
			Expect(client).NotTo(BeNil())
		})
	})

	Describe("PresignPutObject", func() {
		var (
			input *s3.PutObjectInput
		)

		Context("when the bucket exists", func() {
			BeforeEach(func() {
				bucket := testBucket
				key := "test-key"
				content := "test content"

				input = &s3.PutObjectInput{
					Bucket: &bucket,
					Key:    &key,
					Body:   strings.NewReader(content),
				}
			})

			It("should generate a valid presigned URL and store the object", func() {
				result, err := client.PresignPutObject(ctx, input)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).NotTo(BeNil())

				// Verify URL components
				Expect(result.URL).To(ContainSubstring(*input.Bucket))
				Expect(result.URL).To(ContainSubstring(*input.Key))
				Expect(result.URL).To(ContainSubstring("X-Amz-Algorithm=AWS4-HMAC-SHA256"))
				Expect(result.URL).To(ContainSubstring("mock-signature"))

				// Verify object was stored in the bucket
				Expect(mockS3.Buckets).To(HaveKey(*input.Bucket))
				Expect(mockS3.Buckets[*input.Bucket]).To(HaveKey(*input.Key))

				// Verify stored content
				storedObj := mockS3.Buckets[*input.Bucket][*input.Key]
				Expect(storedObj).NotTo(BeNil())

				reader, ok := storedObj.(io.Reader)
				Expect(ok).To(BeTrue(), "stored object should be an io.Reader")

				content, err := io.ReadAll(reader)
				Expect(err).NotTo(HaveOccurred())
				Expect(string(content)).To(Equal("test content"))
			})

			It("should handle multiple presign requests for the same object", func() {
				// First presign request
				result1, err := client.PresignPutObject(ctx, input)
				Expect(err).NotTo(HaveOccurred())
				Expect(result1).NotTo(BeNil())

				// Second presign request with different content
				newContent := "updated content"
				input.Body = strings.NewReader(newContent)
				result2, err := client.PresignPutObject(ctx, input)
				Expect(err).NotTo(HaveOccurred())
				Expect(result2).NotTo(BeNil())

				// Verify the latest content in the bucket
				storedObj := mockS3.Buckets[*input.Bucket][*input.Key]
				reader, ok := storedObj.(io.Reader)
				Expect(ok).To(BeTrue())

				content, err := io.ReadAll(reader)
				Expect(err).NotTo(HaveOccurred())
				Expect(string(content)).To(Equal(newContent))
			})
		})

		Context("when the bucket does not exist", func() {
			BeforeEach(func() {
				bucket := "non-existent-bucket"
				key := "test-key"

				input = &s3.PutObjectInput{
					Bucket: &bucket,
					Key:    &key,
					Body:   strings.NewReader("test content"),
				}
			})

			It("should return an error and not store the object", func() {
				result, err := client.PresignPutObject(ctx, input)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to put object"))
				Expect(result).To(BeNil())

				// Verify bucket was not created
				Expect(mockS3.Buckets).NotTo(HaveKey(*input.Bucket))
			})
		})
	})

	Describe("PresignGetObject", func() {
		It("should generate a signed URL scoped to the requested object", func() {
			bucket := testBucket
			key := "downloads/failed.csv"
			input := &s3.GetObjectInput{Bucket: &bucket, Key: &key}

			result, err := client.PresignGetObject(ctx, input)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.URL).To(ContainSubstring(bucket))
			Expect(result.URL).To(ContainSubstring(key))
			Expect(result.URL).To(ContainSubstring("X-Amz-Signature"))
		})

		It("does not require the object to exist (signing is read-only)", func() {
			// No bucket created, no object stored — signing a GET must still
			// succeed; existence is enforced only when the URL is fetched.
			bucket := "never-created-bucket"
			key := "missing.csv"
			input := &s3.GetObjectInput{Bucket: &bucket, Key: &key}

			result, err := client.PresignGetObject(ctx, input)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.URL).To(ContainSubstring(key))
			Expect(mockS3.Buckets).NotTo(HaveKey(bucket))
		})
	})
})
