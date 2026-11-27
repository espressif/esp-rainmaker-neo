// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package hostedui

import (
	"testing"
	"testing/fstest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestHostedUI(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Hosted UI Suite")
}

// index.html as Vite writes it, trimmed to what loadPage reads.
const viteIndex = `<script type="application/json">{{.PageData}}</script>` +
	`<script type="module" crossorigin src="./assets/app.js"></script>` +
	`<link rel="stylesheet" crossorigin href="./assets/app.css">`

func bundleWith(script, style string) fstest.MapFS {
	return fstest.MapFS{
		"index.html":     {Data: []byte(viteIndex)},
		"assets/app.js":  {Data: []byte(script)},
		"assets/app.css": {Data: []byte(style)},
	}
}

var _ = Describe("the page", func() {
	use := func(fsys fstest.MapFS) {
		p, err := loadPage(fsys)
		previous, previousErr := built, builtErr
		built, builtErr = p, err
		DeferCleanup(func() { built, builtErr = previous, previousErr })
	}

	It("carries the bundle's script and stylesheet inline, and a CSP admitting exactly those bytes", func() {
		use(bundleWith("console.log('{{not a template}}')", "a{color:red}"))
		Expect(Err()).NotTo(HaveOccurred())

		body, err := Shell([]byte(`{"a":1}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(body).To(Equal(`<script type="application/json">{"a":1}</script>` +
			`<script type="module">console.log('{{not a template}}')</script><style>a{color:red}</style>`))
		Expect(CSP()).To(HavePrefix("default-src 'none'; script-src " + sourceHash("console.log('{{not a template}}')") + "; style-src " + sourceHash("a{color:red}") + ";"))
	})

	DescribeTable("refuses a bundle it cannot inline faithfully, instead of serving a page that breaks (negative)",
		func(fsys fstest.MapFS, why string) {
			_, err := loadPage(fsys)
			Expect(err).To(MatchError(ContainSubstring(why)))
		},
		Entry("a script that would close its own element", bundleWith("x='</SCRIPT>'", "a{}"), "cannot be inlined"),
		Entry("a stylesheet that would close its own element", bundleWith("x", "a{}</style>"), "cannot be inlined"),
		Entry("a file index.html names but the bundle lacks", fstest.MapFS{"index.html": {Data: []byte(viteIndex)}}, "missing assets/app.js"),
		Entry("an index.html that loads no script", fstest.MapFS{"index.html": {Data: []byte(`<p>{{.PageData}}</p>`)}}, "exactly one script"),
	)

	It("reports a bundle with no index.html instead of panicking, so the Lambda still starts (negative)", func() {
		var err error
		Expect(func() { _, err = loadPage(fstest.MapFS{}) }).NotTo(Panic())
		Expect(err).To(MatchError(ContainSubstring("bundle missing")))
	})

	It("refuses to render while the bundle is missing, and serves no CSP for it (negative)", func() {
		use(fstest.MapFS{})
		_, err := Shell([]byte(`{}`))
		Expect(err).To(MatchError(ContainSubstring("bundle missing")))
		Expect(Err()).To(MatchError(ContainSubstring("bundle missing")))
		Expect(CSP()).To(BeEmpty())
	})
})
