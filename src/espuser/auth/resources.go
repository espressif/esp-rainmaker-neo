// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"errors"
	"fmt"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/clients"
	"github.com/espressif/esp-rainmaker-neo/src/utils/collections"
)

// Each refusal wraps ErrInvalidTarget, which the authorize redirect keys on.
var (
	ErrResourceNotAllowed      = fmt.Errorf("%w: resource not registered for the client", ErrInvalidTarget)
	ErrResourceRequired        = fmt.Errorf("%w: no resource requested and the client has no default", ErrInvalidTarget)
	ErrOneResourcePerToken     = fmt.Errorf("%w: more than one resource requested for one token", ErrInvalidTarget)
	ErrResourceNotAuthorized   = fmt.Errorf("%w: resource not part of the authorization", ErrInvalidTarget)
	ErrResourceAmbiguous       = fmt.Errorf("%w: several resources authorized and none named", ErrInvalidTarget)
	ErrResourceNoLongerAllowed = fmt.Errorf("%w: resource no longer allowed for the client", ErrInvalidTarget)
)

// ResourceRefusal returns the sentence a client is shown for an RFC 8707 refusal; ok is false for any other error.
func ResourceRefusal(err error) (description string, ok bool) {
	switch {
	case errors.Is(err, ErrResourceNotAllowed):
		return "This client is not registered for the requested resource.", true
	case errors.Is(err, ErrResourceRequired):
		return "A resource is required: this client has no default resource.", true
	case errors.Is(err, ErrOneResourcePerToken):
		return "Only one resource may be requested; a token is never issued for two audiences.", true
	case errors.Is(err, ErrResourceNotAuthorized):
		return "The requested resource is not part of this authorization.", true
	case errors.Is(err, ErrResourceAmbiguous):
		return "This authorization covers several resources; name one with the resource parameter.", true
	case errors.Is(err, ErrResourceNoLongerAllowed):
		return "This login was issued for a resource the client may no longer request. Start a new authorization.", true
	default:
		return "", false
	}
}

// namedResources drops the resource values sent empty, which RFC 6749 s3.1 and s3.2 treat as omitted: ["", "https://a"] -> ["https://a"].
func namedResources(values []string) []string {
	named := make([]string, 0, len(values))
	for _, v := range values {
		if v != "" {
			named = append(named, v)
		}
	}
	return named
}

// RequestedResourceForTokenRequest returns the one resource a token request names, or "" for none (RFC 8707 s2.2): ["", "https://a"] -> "https://a".
func RequestedResourceForTokenRequest(resourceList []string) (string, error) {
	named := namedResources(resourceList)
	switch len(named) {
	case 0:
		return "", nil
	case 1:
		return named[0], nil
	default:
		return "", ErrOneResourcePerToken
	}
}

// authorizedResources returns the resources one sign-in covers: those requested (RFC 8707 s2.1), else the client's default (RFC 9068 s3).
func authorizedResources(client *clients.ClientResponse, requested []string) ([]string, error) {
	requested = namedResources(requested)
	if len(requested) == 0 {
		if client.DefaultResource == "" {
			return nil, ErrResourceRequired
		}
		return []string{client.DefaultResource}, nil
	}
	resources := make([]string, 0, len(requested))
	for _, r := range requested {
		if !client.AllowsResource(r) {
			return nil, ErrResourceNotAllowed
		}
		if !collections.Contains(resources, r) {
			resources = append(resources, r)
		}
	}
	return resources, nil
}

// SetTokenAudienceFromResource picks the one resource an access token is for (RFC 8707 s2.2): the one requested, else the only one authorized, else the client's default. Authorized [neo, account] with nothing requested and default neo -> neo.
func SetTokenAudienceFromResource(client *clients.ClientResponse, authorized []string, requested string) (string, error) {
	audience := requested
	switch {
	case requested != "":
		if !collections.Contains(authorized, requested) {
			return "", ErrResourceNotAuthorized
		}
	case len(authorized) == 1:
		audience = authorized[0]
	case client.DefaultResource != "" && collections.Contains(authorized, client.DefaultResource):
		audience = client.DefaultResource
	default:
		return "", ErrResourceAmbiguous
	}
	// Re-checked on every mint, so taking a resource off the client also stops renewals already in flight.
	if !client.AllowsResource(audience) {
		return "", ErrResourceNoLongerAllowed
	}
	return audience, nil
}
