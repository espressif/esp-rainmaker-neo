// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package httpclient

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("isPublicIP", func() {
	DescribeTable("rejects addresses that are not publicly routable",
		func(addr string) {
			ip := net.ParseIP(addr)
			Expect(ip).NotTo(BeNil(), "test fixture %q is not a valid IP", addr)

			Expect(isPublicIP(ip)).To(BeFalse())
		},
		Entry("Lambda runtime API host", "127.0.0.1"),
		Entry("loopback, non-canonical", "127.1.2.3"),
		Entry("unspecified", "0.0.0.0"),
		Entry("RFC1918 10/8", "10.0.0.5"),
		Entry("RFC1918 172.16/12", "172.16.31.4"),
		Entry("RFC1918 192.168/16", "192.168.1.1"),
		Entry("IMDS link-local", "169.254.169.254"),
		Entry("ECS task metadata link-local", "169.254.170.2"),
		Entry("CGNAT 100.64/10", "100.64.0.1"),
		Entry("CGNAT upper bound", "100.127.255.255"),
		Entry("IPv4 multicast", "224.0.0.1"),
		Entry("IPv6 loopback", "::1"),
		Entry("IPv6 unspecified", "::"),
		Entry("IPv6 ULA fc00::/7", "fd00::1"),
		Entry("IPv6 link-local", "fe80::1"),
		Entry("IPv6 multicast", "ff02::1"),
		Entry("IPv4-mapped IPv6 loopback", "::ffff:127.0.0.1"),
		Entry("IPv4-mapped IPv6 private", "::ffff:10.0.0.1"),
	)

	DescribeTable("allows publicly routable addresses",
		func(addr string) {
			ip := net.ParseIP(addr)
			Expect(ip).NotTo(BeNil(), "test fixture %q is not a valid IP", addr)

			Expect(isPublicIP(ip)).To(BeTrue())
		},
		Entry("public IPv4", "93.184.216.34"),
		Entry("just below the CGNAT block", "100.63.255.255"),
		Entry("just above the CGNAT block", "100.128.0.0"),
		Entry("not RFC1918 despite the 172 prefix", "172.32.0.1"),
		Entry("public IPv6", "2606:2800:220:1:248:1893:25c8:1946"),
	)
})

var _ = Describe("guardDialAddress", func() {
	DescribeTable("blocks non-public dial addresses",
		func(address string) {
			err := guardDialAddress(address)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("refusing to connect to non-public address"))
		},
		Entry("Lambda runtime API", "127.0.0.1:9001"),
		Entry("IMDS", "169.254.169.254:80"),
		Entry("RFC1918", "10.1.2.3:8080"),
		Entry("IPv6 loopback", "[::1]:9001"),
	)

	It("allows a public address", func() {
		Expect(guardDialAddress("93.184.216.34:443")).To(Succeed())
	})

	It("rejects an address with no port", func() {
		err := guardDialAddress("127.0.0.1")

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("cannot parse dial address"))
	})

	It("rejects an address whose host is not an IP, since Control only ever sees resolved addresses", func() {
		err := guardDialAddress("attacker.example:443")

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("is not an IP"))
	})
})

var _ = Describe("GetSSRFSafe", func() {
	defaultClient := func() *http.Client {
		client, ok := GetSSRFSafe().(*http.Client)
		Expect(ok).To(BeTrue(), "expected the default SSRF-safe client to be an *http.Client")
		return client
	}

	It("refuses to connect to a loopback address", func() {
		reached := false
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			reached = true
		}))
		defer server.Close()

		_, err := GetSSRFSafe().Get(server.URL)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("refusing to connect to non-public address"))
		Expect(reached).To(BeFalse())
	})

	It("does not follow a redirect, so a redirect target is never reached", func() {
		internalReached := false
		internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			internalReached = true
			_, _ = io.WriteString(w, `{"client_id":"internal-only"}`)
		}))
		defer internal.Close()

		// Stands in for the attacker-controlled origin named by client_id.
		redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, internal.URL, http.StatusFound)
		}))
		defer redirector.Close()

		// The dial guard alone would block both loopback servers, so this asserts the redirect policy independently: same CheckRedirect, permissive dialer.
		client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: refuseRedirects}

		resp, err := client.Get(redirector.URL)

		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusFound))
		Expect(internalReached).To(BeFalse())
	})

	It("wires the redirect policy into the default client", func() {
		Expect(defaultClient().CheckRedirect(nil, nil)).To(MatchError(http.ErrUseLastResponse))
	})

	It("is swappable so callers can be unit tested without network access", func() {
		original := GetSSRFSafe()
		defer SetSSRFSafe(original)

		SetSSRFSafe(nil)

		Expect(GetSSRFSafe()).To(BeNil())
	})
})
