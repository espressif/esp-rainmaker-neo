// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mock_test

import (
	"context"
	"errors"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"io"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/test/mock"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("S3ClientMock", func() {
	var (
		client *mock.S3ClientMock
		ctx    context.Context
	)

	BeforeEach(func() {
		client = mock.NewS3ClientMock()
		ctx = context.Background()
		presignClient := mock.NewS3PresignClientMock()
		awscommon.SetS3PresignClient(presignClient)
	})

	Describe("NewS3ClientMock", func() {
		It("should create a new S3 client mock with initialized fields", func() {
			Expect(client).NotTo(BeNil())
			Expect(client.Buckets).NotTo(BeNil())
			Expect(client.PresignClient).NotTo(BeNil())
		})
	})

	Describe("PutObject", func() {
		var (
			input *s3.PutObjectInput
		)

		Context("when the bucket exists", func() {
			BeforeEach(func() {
				bucket := "test-bucket"
				key := "test-key"
				client.Buckets[bucket] = make(map[string]any)

				input = &s3.PutObjectInput{
					Bucket: &bucket,
					Key:    &key,
					Body:   strings.NewReader("test content"),
				}
			})

			It("should successfully put the object and store content", func() {
				_, err := client.PutObject(ctx, input)
				Expect(err).NotTo(HaveOccurred())

				// Verify bucket and key existence
				Expect(client.Buckets).To(HaveKey(*input.Bucket))
				Expect(client.Buckets[*input.Bucket]).To(HaveKey(*input.Key))

				// Verify stored content
				storedObj := client.Buckets[*input.Bucket][*input.Key]
				Expect(storedObj).NotTo(BeNil())

				// Read and verify content
				reader, ok := storedObj.(io.Reader)
				Expect(ok).To(BeTrue(), "stored object should be an io.Reader")

				content, err := io.ReadAll(reader)
				Expect(err).NotTo(HaveOccurred())
				Expect(string(content)).To(Equal("test content"))
			})

			It("should overwrite existing object with new content", func() {
				// First put
				_, err := client.PutObject(ctx, input)
				Expect(err).NotTo(HaveOccurred())

				// Second put with different content
				newContent := "updated content"
				input.Body = strings.NewReader(newContent)
				_, err = client.PutObject(ctx, input)
				Expect(err).NotTo(HaveOccurred())

				// Verify updated content
				storedObj := client.Buckets[*input.Bucket][*input.Key]
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

			It("should return a NoSuchBucket error and not store the object", func() {
				_, err := client.PutObject(ctx, input)
				Expect(err).To(HaveOccurred())

				// Check for proper AWS S3 error type
				var noSuchBucket *types.NoSuchBucket
				Expect(errors.As(err, &noSuchBucket)).To(BeTrue())
				Expect(noSuchBucket.ErrorCode()).To(Equal("NoSuchBucket"))

				// Verify bucket was not created
				Expect(client.Buckets).NotTo(HaveKey(*input.Bucket))
			})
		})
	})

	Describe("GetObject", func() {
		var (
			input *s3.GetObjectInput
		)

		Context("when the bucket and key exist", func() {
			BeforeEach(func() {
				bucket := "test-bucket"
				key := "test-key"
				client.Buckets[bucket] = make(map[string]any)

				// First put an object
				putInput := &s3.PutObjectInput{
					Bucket: &bucket,
					Key:    &key,
					Body:   strings.NewReader("test content"),
				}
				_, err := client.PutObject(ctx, putInput)
				Expect(err).NotTo(HaveOccurred())

				input = &s3.GetObjectInput{
					Bucket: &bucket,
					Key:    &key,
				}
			})

			It("should successfully retrieve the object with correct content", func() {
				output, err := client.GetObject(ctx, input)
				Expect(err).NotTo(HaveOccurred())
				Expect(output).NotTo(BeNil())
				Expect(output.Body).NotTo(BeNil())

				// Read and verify content
				content, err := io.ReadAll(output.Body)
				Expect(err).NotTo(HaveOccurred())
				Expect(string(content)).To(Equal("test content"))

				// Ensure body can be closed
				err = output.Body.Close()
				Expect(err).NotTo(HaveOccurred())
			})

			It("should retrieve updated content after overwrite", func() {
				// Update the object
				updatedContent := "updated test content"
				putInput := &s3.PutObjectInput{
					Bucket: input.Bucket,
					Key:    input.Key,
					Body:   strings.NewReader(updatedContent),
				}
				_, err := client.PutObject(ctx, putInput)
				Expect(err).NotTo(HaveOccurred())

				// Retrieve and verify updated content
				output, err := client.GetObject(ctx, input)
				Expect(err).NotTo(HaveOccurred())

				content, err := io.ReadAll(output.Body)
				Expect(err).NotTo(HaveOccurred())
				Expect(string(content)).To(Equal(updatedContent))
			})
		})

		Context("when the bucket does not exist", func() {
			BeforeEach(func() {
				bucket := "non-existent-bucket"
				key := "test-key"

				input = &s3.GetObjectInput{
					Bucket: &bucket,
					Key:    &key,
				}
			})

			It("should return a NoSuchBucket error", func() {
				output, err := client.GetObject(ctx, input)
				Expect(err).To(HaveOccurred())

				// Check for proper AWS S3 error type
				var noSuchBucket *types.NoSuchBucket
				Expect(errors.As(err, &noSuchBucket)).To(BeTrue())
				Expect(noSuchBucket.ErrorCode()).To(Equal("NoSuchBucket"))
				Expect(output).To(BeNil())
			})
		})

		Context("when the bucket exists but the key does not exist", func() {
			BeforeEach(func() {
				bucket := "test-bucket"
				key := "non-existent-key"
				client.Buckets[bucket] = make(map[string]any)

				input = &s3.GetObjectInput{
					Bucket: &bucket,
					Key:    &key,
				}
			})

			It("should return a NoSuchKey error", func() {
				output, err := client.GetObject(ctx, input)
				Expect(err).To(HaveOccurred())

				// Check for proper AWS S3 error type
				var noSuchKey *types.NoSuchKey
				Expect(errors.As(err, &noSuchKey)).To(BeTrue())
				Expect(noSuchKey.ErrorCode()).To(Equal("NoSuchKey"))
				Expect(output).To(BeNil())
			})
		})

		Context("when retrieving from empty bucket", func() {
			BeforeEach(func() {
				bucket := "empty-bucket"
				key := "test-key"
				client.Buckets[bucket] = make(map[string]any)

				input = &s3.GetObjectInput{
					Bucket: &bucket,
					Key:    &key,
				}
			})

			It("should return a NoSuchKey error", func() {
				output, err := client.GetObject(ctx, input)
				Expect(err).To(HaveOccurred())

				// Check for proper AWS S3 error type
				var noSuchKey *types.NoSuchKey
				Expect(errors.As(err, &noSuchKey)).To(BeTrue())
				Expect(noSuchKey.ErrorCode()).To(Equal("NoSuchKey"))
				Expect(output).To(BeNil())
			})
		})

		Context("when retrieving multiple different objects", func() {
			var bucket string

			BeforeEach(func() {
				bucket = "multi-object-bucket"
				client.Buckets[bucket] = make(map[string]any)

				// Put multiple objects
				objects := map[string]string{
					"file1.txt": "content of file 1",
					"file2.txt": "content of file 2",
					"file3.txt": "content of file 3",
				}

				for key, content := range objects {
					putInput := &s3.PutObjectInput{
						Bucket: &bucket,
						Key:    &key,
						Body:   strings.NewReader(content),
					}
					_, err := client.PutObject(ctx, putInput)
					Expect(err).NotTo(HaveOccurred())
				}
			})

			It("should retrieve each object with correct content", func() {
				expectedObjects := map[string]string{
					"file1.txt": "content of file 1",
					"file2.txt": "content of file 2",
					"file3.txt": "content of file 3",
				}

				for key, expectedContent := range expectedObjects {
					input := &s3.GetObjectInput{
						Bucket: &bucket,
						Key:    &key,
					}

					output, err := client.GetObject(ctx, input)
					Expect(err).NotTo(HaveOccurred())

					content, err := io.ReadAll(output.Body)
					Expect(err).NotTo(HaveOccurred())
					Expect(string(content)).To(Equal(expectedContent))

					err = output.Body.Close()
					Expect(err).NotTo(HaveOccurred())
				}
			})
		})
	})

	Describe("CreateBucketDirect", func() {
		It("should create a bucket with empty content map", func() {
			bucketName := "direct-created-bucket"
			client.CreateBucketDirect(bucketName)

			Expect(client.Buckets).To(HaveKey(bucketName))
			Expect(client.Buckets[bucketName]).NotTo(BeNil())
			Expect(client.Buckets[bucketName]).To(BeEmpty())
		})
	})

	Describe("AWS Error Compatibility", func() {
		It("should return AWS-compatible error types with proper ErrorCode and ErrorMessage methods", func() {
			// Test NoSuchBucket error
			_, err := client.GetObject(ctx, &s3.GetObjectInput{
				Bucket: aws.String("non-existent-bucket"),
				Key:    aws.String("test-key"),
			})
			Expect(err).To(HaveOccurred())

			var noSuchBucket *types.NoSuchBucket
			Expect(errors.As(err, &noSuchBucket)).To(BeTrue())
			Expect(noSuchBucket.ErrorCode()).To(Equal("NoSuchBucket"))
			Expect(noSuchBucket.ErrorMessage()).To(Equal("The specified bucket does not exist"))
			Expect(noSuchBucket.ErrorFault()).To(Equal(smithy.FaultClient))

			// Test NoSuchKey error
			client.CreateBucketDirect("test-bucket")
			_, err = client.GetObject(ctx, &s3.GetObjectInput{
				Bucket: aws.String("test-bucket"),
				Key:    aws.String("non-existent-key"),
			})
			Expect(err).To(HaveOccurred())

			var noSuchKey *types.NoSuchKey
			Expect(errors.As(err, &noSuchKey)).To(BeTrue())
			Expect(noSuchKey.ErrorCode()).To(Equal("NoSuchKey"))
			Expect(noSuchKey.ErrorMessage()).To(Equal("The specified key does not exist"))
			Expect(noSuchKey.ErrorFault()).To(Equal(smithy.FaultClient))

			// Test PreconditionFailed error for IfNoneMatch condition
			_, err = client.PutObject(ctx, &s3.PutObjectInput{
				Bucket: aws.String("test-bucket"),
				Key:    aws.String("existing-key"),
				Body:   strings.NewReader("content"),
			})
			Expect(err).NotTo(HaveOccurred())

			// Try to put again with IfNoneMatch="*" (should fail)
			ifNoneMatch := "*"
			_, err = client.PutObject(ctx, &s3.PutObjectInput{
				Bucket:      aws.String("test-bucket"),
				Key:         aws.String("existing-key"),
				Body:        strings.NewReader("new content"),
				IfNoneMatch: &ifNoneMatch,
			})
			Expect(err).To(HaveOccurred())

			var apiError smithy.APIError
			Expect(errors.As(err, &apiError)).To(BeTrue())
			Expect(apiError.ErrorCode()).To(Equal("PreconditionFailed"))
			Expect(apiError.ErrorMessage()).To(Equal("At least one of the preconditions you specified did not hold"))
			Expect(apiError.ErrorFault()).To(Equal(smithy.FaultClient))
		})
	})
})
