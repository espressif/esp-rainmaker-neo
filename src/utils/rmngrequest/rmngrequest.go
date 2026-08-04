// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package rmngrequest

import (
	"encoding/base64"
	"encoding/json"
	"github.com/espressif/esp-rainmaker-neo/src/utils/ids"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"net/url"
	"reflect"
	"strconv"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/validation"

	"github.com/aws/aws-lambda-go/events"
)

// decodedBody returns the request body, base64-decoding it first when API Gateway delivered it encoded. Shared by the JSON (ExtractRequestStruct) and form (ExtractForm) paths so the base64 handling lives in one place.
func decodedBody(request events.APIGatewayProxyRequest) (string, error) {
	if request.IsBase64Encoded && request.Body != "" {
		decoded, err := base64.StdEncoding.DecodeString(request.Body)
		if err != nil {
			return "", rmerror.NewRMError(err, "failed to decode base64 encoded request body")
		}
		return string(decoded), nil
	}
	return request.Body, nil
}

// isFormRequest reports whether the request body is application/x-www-form-urlencoded (the OAuth token endpoint's encoding), read case-insensitively from Content-Type.
func isFormRequest(request events.APIGatewayProxyRequest) bool {
	ct := request.Headers["Content-Type"]
	if ct == "" {
		ct = request.Headers["content-type"]
	}
	return strings.Contains(strings.ToLower(ct), "application/x-www-form-urlencoded")
}

// inputStruct is a pointer to a struct. The body is decoded into it as JSON, or — when Content-Type is application/x-www-form-urlencoded — parsed as form values and mapped into the struct through the same path as query params. Query params overlay last.
func GetRequest(request events.APIGatewayProxyRequest, inputStruct any) error {
	body, err := decodedBody(request)
	if err != nil {
		return err
	}
	request.Body = body

	// First handle body if present
	if request.Body != "" {
		if isFormRequest(request) {
			values, err := url.ParseQuery(request.Body)
			if err != nil {
				return rmerror.NewRMError(err, "failed to parse form request body")
			}
			// url.Values is map[string][]string; flatten to single values so it maps to string struct fields the same way query params do.
			if err := utils.ConvertAnyToAny(flattenValues(values), inputStruct); err != nil {
				return rmerror.NewRMError(err, "failed to sanitize form request body")
			}
		} else {
			// For string fields in structs, whitespace within the values is preserved
			if err := json.Unmarshal([]byte(request.Body), inputStruct); err != nil {
				return rmerror.NewRMError(err, "failed to sanitize request body")
			}
		}
	}

	// Then overlay query parameters if present
	if request.QueryStringParameters != nil {
		if err := utils.ConvertAnyToAny(request.QueryStringParameters, inputStruct); err != nil {
			return rmerror.NewRMError(err, "failed to sanitize request query parameters")
		}
	}

	return nil
}

// flattenValues collapses url.Values (multi-valued) to a single-valued map, keeping the first value per key — matching how single-valued query params map into structs.
func flattenValues(values url.Values) map[string]string {
	out := make(map[string]string, len(values))
	for k, v := range values {
		if len(v) > 0 {
			out[k] = v[0]
		}
	}
	return out
}

// This should be used for all API requests
// Handles
// - Base64decode
// - Body structure parsing
// - QueryParam merging
// - user context setting
// - request validation

func ExtractRequestStruct(request events.APIGatewayProxyRequest, inputRequest any, customValidationFuncs ...validation.ValidationFunc) error {
	if err := GetRequest(request, inputRequest); err != nil {
		return err
	}

	// Canonicalize identity fields before validation so clients may send any case.
	formatUsernameFields(inputRequest)

	if err := validation.ValidateRequest(inputRequest, customValidationFuncs...); err != nil {
		return rmerror.NewRMError(err, "failed to validate request")
	}

	return nil
}

// formatUsernameFields canonicalizes, in place, each string field tagged validate email/email_or_phone.
func formatUsernameFields(inputRequest any) {
	v := reflect.ValueOf(inputRequest)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return
	}
	v = v.Elem()
	if v.Kind() != reflect.Struct {
		return
	}
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := v.Field(i)
		if field.Kind() != reflect.String || !field.CanSet() {
			continue
		}
		tag := t.Field(i).Tag.Get("validate")
		if strings.Contains(tag, "email_or_phone") || strings.Contains(tag, "email") {
			field.SetString(ids.FormatUsername(field.String()))
		}
	}
}

// TODO: ExtractMQTTRequestStruct

// authHeader returns the Authorization header value, read case-insensitively.
func authHeader(headers map[string]string) string {
	if v, ok := headers["authorization"]; ok {
		return v
	}
	return headers["Authorization"]
}

// ExtractAuthToken returns the bearer token from the Authorization header, or "" if absent.
func ExtractAuthToken(headers map[string]string) string {
	header := authHeader(headers)

	return strings.TrimPrefix(header, "Bearer ")
}

// Cookie returns the named cookie's value from the request's Cookie header, or "" if absent.
func Cookie(request events.APIGatewayProxyRequest, name string) string {
	var raw string
	for k, v := range request.Headers { // API Gateway header keys are case-insensitive.
		if strings.EqualFold(k, "Cookie") {
			raw = v
			break
		}
	}
	for _, pair := range strings.Split(raw, ";") {
		if k, v, ok := strings.Cut(strings.TrimSpace(pair), "="); ok && k == name {
			return v
		}
	}
	return ""
}

// BasicClientCreds decodes RFC 7617 Basic client credentials from the Authorization header; ok is false when it is absent or not Basic.
func BasicClientCreds(request events.APIGatewayProxyRequest) (clientID, clientSecret string, ok bool) {
	// API Gateway header keys are case-insensitive.
	var authz string
	for k, v := range request.Headers {
		if strings.EqualFold(k, "Authorization") {
			authz = v
			break
		}
	}
	const prefix = "Basic "
	if len(authz) <= len(prefix) || !strings.EqualFold(authz[:len(prefix)], prefix) {
		return "", "", false
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(authz[len(prefix):]))
	if err != nil {
		return "", "", false
	}
	id, secret, found := strings.Cut(string(decoded), ":")
	if !found {
		return "", "", false
	}
	return id, secret, true
}

// ParsePageSize parses the "page_size" query parameter from a query-param map. Returns the parsed value, or the default (20, or the first override if provided) if absent/invalid.
func ParsePageSize(params map[string]string, defaultPageSize ...int64) int64 {
	fallback := utils.GetOptional(defaultPageSize)
	if fallback == 0 {
		fallback = 20
	}
	pageSizeStr := params["page_size"]
	if pageSizeStr != "" {
		if parsed, err := strconv.ParseInt(pageSizeStr, 10, 64); err == nil && parsed > 0 {
			return parsed
		}
	}
	return fallback
}

// RedactForLog returns a copy of the request safe to log: the Authorization
// header (single- and multi-value forms, case-insensitive) is replaced with a
// placeholder so bearer tokens never land in the logs. The request is taken by
// value and the header maps are copied, so the caller's original is untouched
// and remains usable for token extraction.
func RedactForLog(request events.APIGatewayProxyRequest) events.APIGatewayProxyRequest {
	const redacted = "[REDACTED]"

	if len(request.Headers) > 0 {
		headers := make(map[string]string, len(request.Headers))
		for k, v := range request.Headers {
			if strings.EqualFold(k, "Authorization") {
				v = redacted
			}
			headers[k] = v
		}
		request.Headers = headers
	}

	if len(request.MultiValueHeaders) > 0 {
		multi := make(map[string][]string, len(request.MultiValueHeaders))
		for k, v := range request.MultiValueHeaders {
			if strings.EqualFold(k, "Authorization") {
				multi[k] = []string{redacted}
				continue
			}
			multi[k] = v
		}
		request.MultiValueHeaders = multi
	}

	// Bodies carry secrets too, so header-only redaction is not enough:
	// /v1/user/credentials takes an id_token, and the password / signout endpoints
	// take access and refresh tokens plus cleartext passwords.
	if body, redactedAny := redactJSONBody(request.Body, redacted); redactedAny {
		request.Body = body
	}

	return request
}

// sensitiveBodyFields are request-body keys whose values must never reach the logs.
// Listed here rather than at call sites so every handler using RedactForLog is covered.
var sensitiveBodyFields = map[string]bool{
	"id_token":      true,
	"access_token":  true,
	"refresh_token": true,
	"token":         true,
	"password":      true,
	"old_password":  true,
	"new_password":  true,
	"client_secret": true,
	"secret":        true,
}

// redactJSONBody replaces the values of sensitiveBodyFields in a top-level JSON
// object. Reports whether anything was redacted; on any parse failure it reports
// false and the caller leaves the body untouched, so a non-JSON or nested payload is
// never silently mangled.
func redactJSONBody(body string, redacted string) (string, bool) {
	trimmed := strings.TrimSpace(body)
	if !strings.HasPrefix(trimmed, "{") {
		return "", false
	}

	var fields map[string]any
	if err := json.Unmarshal([]byte(trimmed), &fields); err != nil {
		return "", false
	}

	redactedAny := false
	for key := range fields {
		if sensitiveBodyFields[strings.ToLower(key)] {
			fields[key] = redacted
			redactedAny = true
		}
	}
	if !redactedAny {
		return "", false
	}

	out, err := json.Marshal(fields)
	if err != nil {
		return "", false
	}
	return string(out), true
}
