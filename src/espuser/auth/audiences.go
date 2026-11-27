// SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"os"
	"strings"
)

type acceptedAudiencesKey struct{}

// AddAcceptedAudiencesToCtx carries the names of the API a request reached, which the access tokens verified under ctx must carry.
func AddAcceptedAudiencesToCtx(ctx context.Context, audiences []string) context.Context {
	if len(audiences) == 0 {
		return ctx
	}
	return context.WithValue(ctx, acceptedAudiencesKey{}, audiences)
}

// GetAcceptedAudiences prefers the request's names; ACCEPTED_AUDIENCES serves a Lambda invoked without one (Alexa, SmartThings).
func GetAcceptedAudiences(ctx context.Context) []string {
	if audiences, ok := ctx.Value(acceptedAudiencesKey{}).([]string); ok {
		return audiences
	}
	return strings.FieldsFunc(os.Getenv("ACCEPTED_AUDIENCES"), func(r rune) bool { return r == ',' })
}
