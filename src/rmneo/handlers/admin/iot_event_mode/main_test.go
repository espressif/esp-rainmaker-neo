// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/admin_config_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iot"
	iottypes "github.com/aws/aws-sdk-go-v2/service/iot/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

var _ = Describe("iot_event_mode lambda", func() {
	var (
		ctx        context.Context
		iotMock    *mock.IoTClientMock
		presence   ruleConfig
		publish    ruleConfig
		offlineSql = "SELECT * FROM '$aws/events/presence/disconnected/#'"
		toCloudSql = "SELECT topic(3) as thing_name, * as data FROM 'rainmaker/things/+/to_cloud'"
		errorAct   = &iottypes.Action{
			CloudwatchLogs: &iottypes.CloudwatchLogsAction{
				LogGroupName: aws.String("IoTRules/Test"),
				RoleArn:      aws.String("arn:aws:iam::123:role/error"),
			},
		}
	)

	BeforeEach(func() {
		ctx = context.Background()
		test_utils.TestSetup()
		iotMock = mock.NewIoTClientMock()
		awscommon.SetIoTClient(iotMock)
		os.Setenv("PRESENCE_LAMBDA_ARN", "arn:aws:lambda:us-east-1:123:function:presence_event_handler")
		os.Setenv("NODE_CONN_QUEUE_URL", "https://sqs.us-east-1.amazonaws.com/123/node-conn-queue")
		os.Setenv("PRESENCE_IOT_RULE_ROLE_ARN", "arn:aws:iam::123:role/presence-iot-rule")
		os.Setenv("PUBLISH_INPUT_LAMBDA_ARN", "arn:aws:lambda:us-east-1:123:function:publish_input_event_handler")
		os.Setenv("NODE_TO_CLOUD_QUEUE_URL", "https://sqs.us-east-1.amazonaws.com/123/node-to-cloud-queue")
		os.Setenv("PUBLISH_INPUT_IOT_RULE_ROLE_ARN", "arn:aws:iam::123:role/publish-input-iot-rule")

		presence = ruleConfig{
			name:       presenceRuleName,
			lambdaArn:  "arn:aws:lambda:us-east-1:123:function:presence_event_handler",
			queueURL:   "https://sqs.us-east-1.amazonaws.com/123/node-conn-queue",
			iotRoleArn: "arn:aws:iam::123:role/presence-iot-rule",
		}
		publish = ruleConfig{
			name:       publishInputRuleName,
			lambdaArn:  "arn:aws:lambda:us-east-1:123:function:publish_input_event_handler",
			queueURL:   "https://sqs.us-east-1.amazonaws.com/123/node-to-cloud-queue",
			iotRoleArn: "arn:aws:iam::123:role/publish-input-iot-rule",
		}

		// Seed both rules in lambda-direct mode (matching the default deploy state).
		iotMock.SetTopicRuleDirect(presenceRuleName, &iottypes.TopicRulePayload{
			Sql:         aws.String(offlineSql),
			Actions:     []iottypes.Action{{Lambda: &iottypes.LambdaAction{FunctionArn: aws.String(presence.lambdaArn)}}},
			ErrorAction: errorAct,
		})
		iotMock.SetTopicRuleDirect(publishInputRuleName, &iottypes.TopicRulePayload{
			Sql:              aws.String(toCloudSql),
			AwsIotSqlVersion: aws.String("2016-03-23"),
			Actions:          []iottypes.Action{{Lambda: &iottypes.LambdaAction{FunctionArn: aws.String(publish.lambdaArn)}}},
			ErrorAction:      errorAct,
		})
	})

	Describe("buildAction", func() {
		It("builds a Lambda action for direct mode", func() {
			act, err := buildAction(presence, modeDirect)
			Expect(err).To(BeNil())
			Expect(act.Lambda).ToNot(BeNil())
			Expect(*act.Lambda.FunctionArn).To(Equal(presence.lambdaArn))
			Expect(act.Sqs).To(BeNil())
		})

		It("builds an SQS action for sqs mode", func() {
			act, err := buildAction(presence, modeSQS)
			Expect(err).To(BeNil())
			Expect(act.Sqs).ToNot(BeNil())
			Expect(*act.Sqs.QueueUrl).To(Equal(presence.queueURL))
			Expect(*act.Sqs.RoleArn).To(Equal(presence.iotRoleArn))
			Expect(*act.Sqs.UseBase64).To(BeFalse())
			Expect(act.Lambda).To(BeNil())
		})

		It("rejects unknown mode", func() {
			_, err := buildAction(presence, "bogus")
			Expect(err).ToNot(BeNil())
		})

		It("rejects sqs mode when wiring is missing", func() {
			cfg := presence
			cfg.queueURL = ""
			_, err := buildAction(cfg, modeSQS)
			Expect(err).ToNot(BeNil())
		})
	})

	Describe("detectMode", func() {
		It("returns direct when first action is Lambda", func() {
			mode, err := detectMode(ctx, presenceRuleName)
			Expect(err).To(BeNil())
			Expect(mode).To(Equal(modeDirect))
		})

		It("returns sqs when first action is SQS", func() {
			iotMock.SetTopicRuleDirect(presenceRuleName, &iottypes.TopicRulePayload{
				Sql: aws.String(offlineSql),
				Actions: []iottypes.Action{{Sqs: &iottypes.SqsAction{
					QueueUrl: aws.String(presence.queueURL),
					RoleArn:  aws.String(presence.iotRoleArn),
				}}},
			})
			mode, err := detectMode(ctx, presenceRuleName)
			Expect(err).To(BeNil())
			Expect(mode).To(Equal(modeSQS))
		})

		It("returns an error when the rule is missing", func() {
			_, err := detectMode(ctx, "no_such_rule")
			Expect(err).ToNot(BeNil())
		})
	})

	Describe("flipRule", func() {
		It("flips direct → sqs while preserving SQL and error_action", func() {
			err := flipRule(ctx, presence, modeSQS)
			Expect(err).To(BeNil())

			payload, ok := iotMock.GetTopicRuleDirect(presenceRuleName)
			Expect(ok).To(BeTrue())
			Expect(*payload.Sql).To(Equal(offlineSql))
			Expect(payload.ErrorAction).To(Equal(errorAct))
			Expect(payload.Actions).To(HaveLen(1))
			Expect(payload.Actions[0].Sqs).ToNot(BeNil())
			Expect(payload.Actions[0].Lambda).To(BeNil())
		})

		It("flips sqs → direct while preserving SQL and error_action", func() {
			// Pre-flip into sqs mode.
			iotMock.SetTopicRuleDirect(presenceRuleName, &iottypes.TopicRulePayload{
				Sql: aws.String(offlineSql),
				Actions: []iottypes.Action{{Sqs: &iottypes.SqsAction{
					QueueUrl: aws.String(presence.queueURL),
					RoleArn:  aws.String(presence.iotRoleArn),
				}}},
				ErrorAction: errorAct,
			})

			err := flipRule(ctx, presence, modeDirect)
			Expect(err).To(BeNil())

			payload, ok := iotMock.GetTopicRuleDirect(presenceRuleName)
			Expect(ok).To(BeTrue())
			Expect(*payload.Sql).To(Equal(offlineSql))
			Expect(payload.ErrorAction).To(Equal(errorAct))
			Expect(payload.Actions[0].Lambda).ToNot(BeNil())
			Expect(*payload.Actions[0].Lambda.FunctionArn).To(Equal(presence.lambdaArn))
			Expect(payload.Actions[0].Sqs).To(BeNil())
		})

		It("preserves AwsIotSqlVersion across flips", func() {
			err := flipRule(ctx, publish, modeSQS)
			Expect(err).To(BeNil())

			payload, _ := iotMock.GetTopicRuleDirect(publishInputRuleName)
			Expect(*payload.AwsIotSqlVersion).To(Equal("2016-03-23"))
		})

		It("returns an error if the rule does not exist", func() {
			missing := ruleConfig{name: "no_such_rule", lambdaArn: presence.lambdaArn}
			err := flipRule(ctx, missing, modeDirect)
			Expect(err).ToNot(BeNil())
		})
	})

	Describe("GetTopicRule round-trip with iot client", func() {
		It("uses the underlying client to read rule state", func() {
			out, err := awscommon.GetIoTClient().GetTopicRule(ctx, &iot.GetTopicRuleInput{
				RuleName: aws.String(presenceRuleName),
			})
			Expect(err).To(BeNil())
			Expect(out.Rule).ToNot(BeNil())
			Expect(*out.Rule.Sql).To(Equal(offlineSql))
		})
	})

	Describe("isReapplyEvent", func() {
		It("returns true for {\"action\":\"reapply\"}", func() {
			Expect(isReapplyEvent([]byte(`{"action":"reapply"}`))).To(BeTrue())
		})

		It("returns false for an API Gateway proxy event", func() {
			raw, _ := json.Marshal(map[string]interface{}{
				"httpMethod": "GET",
				"path":       "/v1/admin/iot-event-mode",
			})
			Expect(isReapplyEvent(raw)).To(BeFalse())
		})

		It("returns false for malformed JSON", func() {
			Expect(isReapplyEvent([]byte(`not json`))).To(BeFalse())
		})

		It("returns false for {\"action\":\"something_else\"}", func() {
			Expect(isReapplyEvent([]byte(`{"action":"flip"}`))).To(BeFalse())
		})
	})

	Describe("handleReapply", func() {
		writeStoredRow := func(presence, publishInput string) {
			adminDB := admin_config_db.NewAdminConfigDB(rmngctx.NewRmngContextWithCtx(ctx, utils.NewSystemActor()))
			err := adminDB.SetIoTEventMode(presence, publishInput, "test-system")
			Expect(err).To(BeNil())
		}

		It("is a no-op when the row is missing (fresh stack)", func() {
			resp, err := handleReapply(ctx)
			Expect(err).To(BeNil())
			Expect(resp.Status).To(Equal("noop"))
			// Both rules are still in lambda-direct mode (the seed state).
			payload, _ := iotMock.GetTopicRuleDirect(presenceRuleName)
			Expect(payload.Actions[0].Lambda).ToNot(BeNil())
		})

		It("flips both rules to sqs when the row says sqs", func() {
			writeStoredRow(modeSQS, modeSQS)
			resp, err := handleReapply(ctx)
			Expect(err).To(BeNil())
			Expect(resp.Status).To(Equal("applied"))
			Expect(resp.Presence).To(Equal(modeSQS))
			Expect(resp.PublishInput).To(Equal(modeSQS))

			// Verify both live rules now have an SQS action.
			pres, _ := iotMock.GetTopicRuleDirect(presenceRuleName)
			Expect(pres.Actions[0].Sqs).ToNot(BeNil())
			Expect(*pres.Sql).To(Equal(offlineSql))
			Expect(pres.ErrorAction).To(Equal(errorAct))
			pub, _ := iotMock.GetTopicRuleDirect(publishInputRuleName)
			Expect(pub.Actions[0].Sqs).ToNot(BeNil())
			Expect(*pub.AwsIotSqlVersion).To(Equal("2016-03-23"))
		})

		It("flips back to direct when the row says direct (e.g., after manual revert)", func() {
			// Pre-state: both rules in sqs.
			iotMock.SetTopicRuleDirect(presenceRuleName, &iottypes.TopicRulePayload{
				Sql: aws.String(offlineSql),
				Actions: []iottypes.Action{{Sqs: &iottypes.SqsAction{
					QueueUrl: aws.String(presence.queueURL),
					RoleArn:  aws.String(presence.iotRoleArn),
				}}},
				ErrorAction: errorAct,
			})
			iotMock.SetTopicRuleDirect(publishInputRuleName, &iottypes.TopicRulePayload{
				Sql:              aws.String(toCloudSql),
				AwsIotSqlVersion: aws.String("2016-03-23"),
				Actions: []iottypes.Action{{Sqs: &iottypes.SqsAction{
					QueueUrl: aws.String(publish.queueURL),
					RoleArn:  aws.String(publish.iotRoleArn),
				}}},
				ErrorAction: errorAct,
			})

			writeStoredRow(modeDirect, modeDirect)
			resp, err := handleReapply(ctx)
			Expect(err).To(BeNil())
			Expect(resp.Status).To(Equal("applied"))

			pres, _ := iotMock.GetTopicRuleDirect(presenceRuleName)
			Expect(pres.Actions[0].Lambda).ToNot(BeNil())
			pub, _ := iotMock.GetTopicRuleDirect(publishInputRuleName)
			Expect(pub.Actions[0].Lambda).ToNot(BeNil())
		})
	})

	Describe("AdminConfigDB", func() {
		systemCtx := func() *rmngctx.RmngContext {
			return rmngctx.NewRmngContextWithCtx(ctx, utils.NewSystemActor())
		}

		It("round-trips presence and publish_input through DynamoDB", func() {
			adminDB := admin_config_db.NewAdminConfigDB(systemCtx())
			err := adminDB.SetIoTEventMode(modeSQS, modeDirect, "test-user-id")
			Expect(err).To(BeNil())

			cfg, err := adminDB.GetIoTEventMode()
			Expect(err).To(BeNil())
			Expect(cfg).ToNot(BeNil())
			Expect(cfg.Presence).To(Equal(modeSQS))
			Expect(cfg.PublishInput).To(Equal(modeDirect))
			Expect(cfg.UpdatedBy).To(Equal("test-user-id"))
			Expect(cfg.UpdatedAt).To(BeNumerically(">", int64(0)))
		})

		It("returns nil cfg when no row exists", func() {
			adminDB := admin_config_db.NewAdminConfigDB(systemCtx())
			cfg, err := adminDB.GetIoTEventMode()
			Expect(err).To(BeNil())
			Expect(cfg).To(BeNil())
		})

		It("rejects writes from a context without AdminConfigSet permission", func() {
			// Plain user context (only has GroupCreate granted by NewUser).
			plain := rmngctx.NewRmngContextWithCtx(ctx, user.NewUser("regular-user"))
			adminDB := admin_config_db.NewAdminConfigDB(plain)
			err := adminDB.SetIoTEventMode(modeSQS, modeSQS, "regular-user")
			Expect(err).ToNot(BeNil())
		})

		It("rejects reads from a context without AdminConfigGet permission", func() {
			plain := rmngctx.NewRmngContextWithCtx(ctx, user.NewUser("regular-user"))
			adminDB := admin_config_db.NewAdminConfigDB(plain)
			_, err := adminDB.GetIoTEventMode()
			Expect(err).ToNot(BeNil())
		})

		It("accepts a context that has been explicitly granted the action", func() {
			plainUser := user.NewUser("granted-user")
			grantCtx := rmngctx.NewRmngContextWithCtx(ctx, plainUser)
			Expect(grantCtx.SetAllow(utils.AdminConfigSet, admin_config_db.IoTEventModeConfigKey)).To(BeNil())
			Expect(grantCtx.SetAllow(utils.AdminConfigGet, admin_config_db.IoTEventModeConfigKey)).To(BeNil())

			adminDB := admin_config_db.NewAdminConfigDB(grantCtx)
			Expect(adminDB.SetIoTEventMode(modeDirect, modeDirect, "granted-user")).To(BeNil())
			cfg, err := adminDB.GetIoTEventMode()
			Expect(err).To(BeNil())
			Expect(cfg).ToNot(BeNil())
		})
	})
})

func TestIoTEventMode(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "IoTEventMode Suite")
}
