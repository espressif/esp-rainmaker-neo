// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package file

import (
	"context"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"io"
	"os"
	"strings"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/s3util"

	"github.com/google/uuid"
)

const file_upload_url_expiry = 10 * time.Minute

type File struct {
	FileS3Bucket string
	FileS3Key    string
}

// GetUniqueFileName returns a unique file name with an optional extension.
// Always generates a unique path by prepending a UUID prefix to prevent overwrites.
func GetUniqueFileName(fileName, subFolder string, extension string) string {
	prefix := uuid.New().String()[:8]
	if fileName == "" {
		fileName = uuid.New().String()
	}
	// Strip existing extension if we're adding one
	if extension != "" && strings.HasSuffix(fileName, "."+extension) {
		fileName = strings.TrimSuffix(fileName, "."+extension)
	}
	fileName = prefix + "_" + fileName
	if subFolder != "" {
		fileName = subFolder + "/" + fileName
	}
	if extension != "" {
		fileName = fileName + "." + extension
	}
	return fileName
}

func NewFile(bucketName string, key string) (*File, error) {
	return &File{
		FileS3Bucket: bucketName,
		FileS3Key:    key,
	}, nil
}

// New creates a new File instance with the given parameters
func NewUserFile(userId string, fileName string) (*File, error) {
	key := "USER_" + userId + "/" + fileName
	bucketName := os.Getenv("FILE_BUCKET_NAME")

	return &File{
		FileS3Bucket: bucketName,
		FileS3Key:    key,
	}, nil
}

func NewSystemFile(fileName string) (*File, error) {
	key := "system/" + fileName
	bucketName := os.Getenv("FILE_BUCKET_NAME")

	return &File{
		FileS3Bucket: bucketName,
		FileS3Key:    key,
	}, nil
}

// Validate checks if the file attributes are valid
func (f *File) Validate() error {
	if f.FileS3Bucket == "" {
		return rmerror.NewRMError(nil, "file s3 bucket is required")
	}
	if f.FileS3Key == "" {
		return rmerror.NewRMError(nil, "file s3 key is required")
	}
	return nil
}

// GetUploadUrl generates a pre-signed URL for file upload
func (f *File) GetUploadUrl(ctx context.Context) (string, error) {
	uploadURL, err := s3util.GetPresignedURL(ctx, f.FileS3Bucket, f.FileS3Key, file_upload_url_expiry, false)
	if err != nil {
		return "", rmerror.NewRMError(err, "failed to generate upload URL")
	}

	return uploadURL, nil
}

func (f *File) ReadContent(ctx context.Context) ([]byte, error) {
	return s3util.GetObjectContent(ctx, f.FileS3Bucket, f.FileS3Key)
}

func (f *File) GetS3Path() string {
	return s3util.GetS3Path(f.FileS3Bucket, f.FileS3Key)
}

func (f *File) WriteContent(ctx context.Context, content io.Reader) error {
	return s3util.PutObject(ctx, f.FileS3Bucket, f.FileS3Key, content)
}
