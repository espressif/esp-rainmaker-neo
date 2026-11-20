// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("APIErrorRespJSON", func() {
	It("answers with the client status and message an RMError carries, even when wrapped", func() {
		inner := rmerror.NewRMError(nil, "grant missing").WithClientStatus(http.StatusForbidden, "No access")
		resp := APIErrorRespJSON(fmt.Errorf("handler: %w", rmerror.NewRMError(inner, "load grant")))
		Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		Expect(resp.Body).To(MatchJSON(`{"message":"No access"}`))
	})

	It("answers 500 Internal Server Error for an RMError with no client status", func() {
		resp := APIErrorRespJSON(rmerror.NewRMError(errors.New("throttled"), "query groups"))
		Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))
		Expect(resp.Body).To(MatchJSON(`{"message":"Internal Server Error"}`))
	})

	It("answers 500 Internal Server Error for a plain error without leaking its text", func() {
		resp := APIErrorRespJSON(errors.New("sts internal detail"))
		Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))
		Expect(resp.Body).To(MatchJSON(`{"message":"Internal Server Error"}`))
	})
})
