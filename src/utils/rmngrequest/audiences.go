// SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package rmngrequest

import (
	"os"
	"strings"

	"github.com/aws/aws-lambda-go/events"
)

// GetAcceptedAudiencesFromAPIRequest names the API this request reached, as the audiences its access tokens may carry (RFC 9068 s4): the execute-api URL with its stage, which reaches the API whatever domain is mapped, plus the custom domain and base path the request came in on. Nil outside API Gateway.
func GetAcceptedAudiencesFromAPIRequest(request events.APIGatewayProxyRequest) []string {
	rc := request.RequestContext
	region := os.Getenv("AWS_REGION")
	if rc.APIID == "" || rc.Stage == "" || region == "" {
		return nil
	}
	executeAPIHost := rc.APIID + ".execute-api." + region + "." + awsDNSSuffix(region)
	audiences := []string{"https://" + executeAPIHost + "/" + rc.Stage}
	if rc.DomainName != "" && rc.DomainName != executeAPIHost {
		audiences = append(audiences, "https://"+rc.DomainName+basePath(rc.Path, rc.ResourcePath))
	}
	return audiences
}

// awsDNSSuffix is the partition's domain, which China regions do not share.
func awsDNSSuffix(region string) string {
	if strings.HasPrefix(region, "cn-") {
		return "amazonaws.com.cn"
	}
	return "amazonaws.com"
}

// basePath is what API Gateway's requestContext.path carries in front of the route: ("/auth/v1/users/me", "/v1/users/{userId}") -> "/auth"; a domain mapped at its root gives "".
func basePath(path, resourcePath string) string {
	pathSegments := splitPath(path)
	routeSegments := len(splitPath(resourcePath))
	if routeSegments >= len(pathSegments) {
		return ""
	}
	return "/" + strings.Join(pathSegments[:len(pathSegments)-routeSegments], "/")
}

func splitPath(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}
