// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"net/http"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/file"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/notification/push"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngrequest"
	"github.com/espressif/esp-rainmaker-neo/src/utils/validation"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/go-playground/validator/v10"
)

const (
	FILE_TYPE_NODE_CERT        = "node_cert"
	FILE_TYPE_PUSH_TEXT_CONFIG = "push_text_config"
)

var ValidateFileTypes = validation.ValidationFunc{
	Name: "std_file_types",
	Fn: func(fl validator.FieldLevel) bool {
		fileType := fl.Field().String()
		return fileType == FILE_TYPE_NODE_CERT || fileType == FILE_TYPE_PUSH_TEXT_CONFIG
	},
}

type GetFileUploadUrlRequest struct {
	FileType string `json:"file_type" validate:"required,std_file_types"`
	FileName string `json:"file_name,omitempty" validate:"omitempty,min=1,max=50"` // Should be unique
}

type GetFileUploadUrlResponse struct {
	UploadURL string `json:"upload_url"`
	S3Path    string `json:"s3_path"`
}

func handleGetFileUploadUrl(ctx context.Context, request events.APIGatewayProxyRequest) (GetFileUploadUrlResponse, error) {
	var req GetFileUploadUrlRequest
	if err := rmngrequest.ExtractRequestStruct(request, &req, ValidateFileTypes); err != nil {
		return GetFileUploadUrlResponse{}, rmerror.NewRMError(err, "failed to extract request struct")
	}
	var f *file.File
	var err error

	switch req.FileType {
	case FILE_TYPE_NODE_CERT:
		filename := file.GetUniqueFileName(req.FileName, "node_certs", "csv")
		f, err = file.NewSystemFile(filename)
		if err != nil {
			return GetFileUploadUrlResponse{}, rmerror.NewRMError(err, "failed to create file object")
		}
	case FILE_TYPE_PUSH_TEXT_CONFIG:
		f, err = file.NewSystemFile(push.PushTextConfigKey)
		if err != nil {
			return GetFileUploadUrlResponse{}, rmerror.NewRMError(err, "failed to create file object")
		}
	default:
		return GetFileUploadUrlResponse{}, rmerror.NewRMError(nil, "invalid file type")
	}
	if err := f.Validate(); err != nil {
		return GetFileUploadUrlResponse{}, rmerror.NewRMError(err, "failed to validate file")
	}

	uploadURL, err := f.GetUploadUrl(ctx)
	if err != nil {
		return GetFileUploadUrlResponse{}, rmerror.NewRMError(err, "failed to get upload url")
	}

	return GetFileUploadUrlResponse{
		UploadURL: uploadURL,
		S3Path:    f.GetS3Path(),
	}, nil
}

func handleGetFileTemplate(_ context.Context, request events.APIGatewayProxyRequest) (push.PushTextConfig, error) {
	templateType := request.PathParameters["templateType"]
	if templateType == "" {
		return push.PushTextConfig{}, fmt.Errorf("template type not found")
	}

	if templateType == FILE_TYPE_PUSH_TEXT_CONFIG {
		defaults := push.PushTextConfig{}
		push.LoadPushTextConfigFromDefaults(&defaults)
		return defaults, nil
	}

	return push.PushTextConfig{}, fmt.Errorf("template type not found: %s", templateType)
}

func handleRequest(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	var err error
	var response interface{}
	var statusCode int = http.StatusOK
	rctx := user.NewContextWithAPIRequest(ctx, request)
	isAuthorized := rctx.GetAccessor().(*user.User).IsSuperAdmin(rctx)
	if !isAuthorized {
		rlog.Error(rctx).Bool("isAuthorized", isAuthorized).Msg("User is not authorized")
		return utils.APIGwRespJSON(http.StatusForbidden, utils.NewAPIStatus("Forbidden")), nil
	}

	switch request.Resource {
	case "/v1/admin/files/upload-urls":
		if request.HTTPMethod == http.MethodPost {
			response, err = handleGetFileUploadUrl(ctx, request)
			statusCode = http.StatusCreated
		} else {
			return utils.APIGwRespJSON(http.StatusMethodNotAllowed, utils.NewAPIStatus("Method not allowed")), nil
		}

	case "/v1/admin/file-templates/{templateType}":
		if request.HTTPMethod == http.MethodGet {
			response, err = handleGetFileTemplate(ctx, request)
			if err != nil {
				// Specific handling for 404
				if strings.Contains(err.Error(), "not found") {
					return utils.APIGwRespJSON(http.StatusNotFound, utils.NewAPIStatus(err.Error())), nil
				}
			}
		} else {
			return utils.APIGwRespJSON(http.StatusMethodNotAllowed, utils.NewAPIStatus("Method not allowed")), nil
		}

	default:
		return utils.APIGwRespJSON(http.StatusNotFound, utils.NewAPIStatus("Not Found")), nil
	}

	if err != nil {
		rlog.Error(rctx).Err(err).Send()
		// Use err.Error() so the client knows what happened
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus(err.Error())), nil
	}

	return utils.APIGwRespJSON(statusCode, response), nil
}

func main() {
	lambda.Start(handleRequest)
}
