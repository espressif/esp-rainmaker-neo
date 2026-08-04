// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package rlog

import (
	"bytes"
	"context"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/utils"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssm_types "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	. "github.com/onsi/ginkgo/v2"
	g "github.com/onsi/gomega"
	"github.com/rs/zerolog"
)

func TestRlog(t *testing.T) {
	g.RegisterFailHandler(Fail)
	RunSpecs(t, "Rlog Suite")
}

// stubSSM is a minimal SSM mock scoped to these tests.
type stubSSM struct {
	params   map[string]string
	getError error
}

func (s *stubSSM) PutParameter(ctx context.Context, params *ssm.PutParameterInput, optFns ...func(*ssm.Options)) (*ssm.PutParameterOutput, error) {
	return nil, fmt.Errorf("not implemented")
}
func (s *stubSSM) GetParameter(ctx context.Context, params *ssm.GetParameterInput, optFns ...func(*ssm.Options)) (*ssm.GetParameterOutput, error) {
	if s.getError != nil {
		return nil, s.getError
	}
	v, ok := s.params[*params.Name]
	if !ok {
		return nil, &ssm_types.ParameterNotFound{}
	}
	return &ssm.GetParameterOutput{
		Parameter: &ssm_types.Parameter{
			Name:  params.Name,
			Value: aws.String(v),
		},
	}, nil
}
func (s *stubSSM) GetParametersByPath(ctx context.Context, params *ssm.GetParametersByPathInput, optFns ...func(*ssm.Options)) (*ssm.GetParametersByPathOutput, error) {
	return nil, fmt.Errorf("not implemented")
}
func (s *stubSSM) DeleteParameter(ctx context.Context, params *ssm.DeleteParameterInput, optFns ...func(*ssm.Options)) (*ssm.DeleteParameterOutput, error) {
	return nil, fmt.Errorf("not implemented")
}

var _ = Describe("fetchRlogFromSSM", func() {
	var origClient awscommon.SSMClientInterface

	BeforeEach(func() {
		origClient = awscommon.GetSSMClient()
	})
	AfterEach(func() {
		awscommon.SetSSMClient(origClient)
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	})

	It("returns per-function config when present", func() {
		awscommon.SetSSMClient(&stubSSM{params: map[string]string{
			"/rmng/rlog/my-func": `{"level":"debug"}`,
			"/rmng/rlog/global":  `{"level":"info"}`,
		}})

		result := fetchRlogFromSSM("my-func")
		g.Expect(result).To(g.HaveKeyWithValue("level", "debug"))
	})

	It("falls back to global when per-function is absent", func() {
		awscommon.SetSSMClient(&stubSSM{params: map[string]string{
			"/rmng/rlog/global": `{"level":"trace"}`,
		}})

		result := fetchRlogFromSSM("other-func")
		g.Expect(result).To(g.HaveKeyWithValue("level", "trace"))
	})

	It("returns nil when both parameters are absent", func() {
		awscommon.SetSSMClient(&stubSSM{params: map[string]string{}})

		result := fetchRlogFromSSM("no-config-func")
		g.Expect(result).To(g.BeNil())
	})

	It("returns nil when SSM client is nil", func() {
		awscommon.SetSSMClient(nil)

		result := fetchRlogFromSSM("any-func")
		g.Expect(result).To(g.BeNil())
	})

	It("skips malformed JSON and tries next parameter", func() {
		awscommon.SetSSMClient(&stubSSM{params: map[string]string{
			"/rmng/rlog/bad-func": `not-json`,
			"/rmng/rlog/global":   `{"level":"warn"}`,
		}})

		result := fetchRlogFromSSM("bad-func")
		g.Expect(result).To(g.HaveKeyWithValue("level", "warn"))
	})

	It("returns nil on SSM error", func() {
		awscommon.SetSSMClient(&stubSSM{
			params:   map[string]string{},
			getError: fmt.Errorf("access denied"),
		})

		result := fetchRlogFromSSM("err-func")
		g.Expect(result).To(g.BeNil())
	})

	It("preserves allow filters from SSM config", func() {
		awscommon.SetSSMClient(&stubSSM{params: map[string]string{
			"/rmng/rlog/my-func": `{"level":"debug","allow":{"uid":"user123"}}`,
		}})

		result := fetchRlogFromSSM("my-func")
		g.Expect(result).To(g.HaveKeyWithValue("level", "debug"))
		g.Expect(result).To(g.HaveKey("allow"))
		allowMap := result["allow"].(map[string]interface{})
		g.Expect(allowMap).To(g.HaveKeyWithValue("uid", "user123"))
	})
})

var _ = Describe("Ctx", func() {
	var origLevel zerolog.Level

	BeforeEach(func() {
		origLevel = zerolog.GlobalLevel()
		zerolog.SetGlobalLevel(zerolog.TraceLevel)
	})
	AfterEach(func() {
		zerolog.SetGlobalLevel(origLevel)
	})

	It("returns enriched logger when context carries one", func() {
		var buf bytes.Buffer
		logger := zerolog.New(&buf).With().Str("uid", "user-42").Logger()
		ctx := logger.WithContext(context.Background())

		Ctx(ctx).Info().Msg("hello")
		g.Expect(buf.String()).To(g.ContainSubstring(`"uid":"user-42"`))
		g.Expect(buf.String()).To(g.ContainSubstring(`"message":"hello"`))
	})

	It("falls back to global logger when context has no logger", func() {
		ctx := context.Background()
		logger := Ctx(ctx)
		g.Expect(logger).NotTo(g.BeNil())
		g.Expect(logger.GetLevel()).NotTo(g.Equal(zerolog.Disabled))
	})

	It("does not bleed logger fields across different contexts", func() {
		var buf1, buf2 bytes.Buffer
		l1 := zerolog.New(&buf1).With().Str("uid", "A").Logger()
		l2 := zerolog.New(&buf2).With().Str("uid", "B").Logger()
		ctx1 := l1.WithContext(context.Background())
		ctx2 := l2.WithContext(context.Background())

		Ctx(ctx1).Info().Msg("from1")
		Ctx(ctx2).Info().Msg("from2")

		g.Expect(buf1.String()).To(g.ContainSubstring(`"uid":"A"`))
		g.Expect(buf1.String()).NotTo(g.ContainSubstring(`"uid":"B"`))
		g.Expect(buf2.String()).To(g.ContainSubstring(`"uid":"B"`))
		g.Expect(buf2.String()).NotTo(g.ContainSubstring(`"uid":"A"`))
	})
})

var _ = Describe("InitLogger via SSM config", func() {
	AfterEach(func() {
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	})

	It("sets global level when called with SSM config", func() {
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
		utils.InitLogger(map[string]interface{}{"level": "debug"})
		g.Expect(zerolog.GlobalLevel()).To(g.Equal(zerolog.DebugLevel))
	})

	It("keeps ErrorLevel with empty config", func() {
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
		utils.InitLogger(map[string]interface{}{})
		g.Expect(zerolog.GlobalLevel()).To(g.Equal(zerolog.ErrorLevel))
	})
})
