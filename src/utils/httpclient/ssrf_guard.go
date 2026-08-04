// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package httpclient

import (
	"fmt"
	"net"
	"net/http"
	"syscall"
	"time"
)

// cgnatBlock is 100.64.0.0/10 (RFC 6598). net.IP has no predicate for it, unlike the other non-public ranges.
var cgnatBlock = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

var ssrfSafeClient Client = newSSRFSafeClient()

// GetSSRFSafe returns a client for fetching URLs an untrusted caller controls. Unlike Get it refuses redirects and refuses to connect to any non-public address, so it must not be used for deliberate link-local calls such as the ECS task-metadata endpoint.
func GetSSRFSafe() Client {
	return ssrfSafeClient
}

func SetSSRFSafe(client Client) {
	ssrfSafeClient = client
}

func newSSRFSafeClient() *http.Client {
	dialer := &net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			return guardDialAddress(address)
		},
	}

	return &http.Client{
		Timeout:       10 * time.Second,
		Transport:     &http.Transport{DialContext: dialer.DialContext},
		CheckRedirect: refuseRedirects,
	}
}

// refuseRedirects stops the client at the 3xx rather than following it: the scheme and host of a redirect target are chosen by whoever served the redirect, so following one silently voids every check the caller made on the original URL.
func refuseRedirects(_ *http.Request, _ []*http.Request) error {
	return http.ErrUseLastResponse
}

// guardDialAddress runs as the dialer's Control hook, i.e. on the address actually being connected to, after DNS resolution. Checking here rather than on the URL blocks a hostname that resolves to a private IP, including one that re-resolves between check and connect (DNS rebinding).
func guardDialAddress(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("cannot parse dial address %q: %w", address, err)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("dial address %q is not an IP", address)
	}
	if !isPublicIP(ip) {
		return fmt.Errorf("refusing to connect to non-public address %s", ip)
	}
	return nil
}

func isPublicIP(ip net.IP) bool {
	if ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return false
	}
	if ip4 := ip.To4(); ip4 != nil {
		return !cgnatBlock.Contains(ip4)
	}
	return true
}
