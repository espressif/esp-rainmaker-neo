// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package rlog provides a global logger for rmng.
// It wraps utils.Rlog and, in Lambda, upgrades the log level from SSM
// Parameter Store so that configuration persists across CDK deploys.
package rlog

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"io"
	"os"

	"github.com/espressif/esp-rainmaker-neo/src/utils"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/rs/zerolog"
)

func init() {
	// utils.init() has already run (this package imports utils), so the SSM
	// client is ready and the default ErrorLevel logger is set. If the RLOG
	// env var was present, utils.init() already configured the logger — nothing
	// to do. Otherwise, try SSM for persistent config.
	if os.Getenv("RLOG") != "" {
		return
	}
	funcName := os.Getenv("AWS_LAMBDA_FUNCTION_NAME")
	if funcName == "" {
		return
	}
	if rlogJSON := fetchRlogFromSSM(funcName); len(rlogJSON) > 0 {
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
		utils.InitLogger(rlogJSON)
	}
}

// fetchRlogFromSSM tries per-function then global SSM parameter for RLOG config.
// Returns empty map on any error (caller keeps the default ErrorLevel).
func fetchRlogFromSSM(funcName string) map[string]interface{} {
	client := awscommon.GetSSMClient()
	if client == nil {
		return nil
	}
	ctx := context.Background()

	// Per-function takes precedence over global
	for _, name := range []string{
		"/rmng/rlog/" + funcName,
		"/rmng/rlog/global",
	} {
		result, err := client.GetParameter(ctx, &ssm.GetParameterInput{
			Name: aws.String(name),
		})
		if err != nil {
			continue
		}
		if result.Parameter == nil || result.Parameter.Value == nil {
			continue
		}
		var rlogJSON map[string]interface{}
		if err := json.Unmarshal([]byte(*result.Parameter.Value), &rlogJSON); err != nil {
			fmt.Printf("Error unmarshalling RLOG from SSM parameter %s: %s\n", name, err)
			continue
		}
		return rlogJSON
	}
	return nil
}

// Output duplicates the global logger and sets w as its output.
func Output(w io.Writer) zerolog.Logger {
	return utils.Rlog.Output(w)
}

// With creates a child logger with the field added to its context.
func With() zerolog.Context {
	return utils.Rlog.With()
}

// Level creates a child logger with the minimum accepted level set to level.
func Level(level zerolog.Level) zerolog.Logger {
	return utils.Rlog.Level(level)
}

// Sample returns a logger with the s sampler.
func Sample(s zerolog.Sampler) zerolog.Logger {
	return utils.Rlog.Sample(s)
}

// Hook returns a logger with the h Hook.
func Hook(h zerolog.Hook) zerolog.Logger {
	return utils.Rlog.Hook(h)
}

func Err(ctx context.Context, err error) *zerolog.Event {
	return Ctx(ctx).Err(err)
}

func Trace(ctx context.Context) *zerolog.Event {
	return Ctx(ctx).Trace()
}

func Debug(ctx context.Context) *zerolog.Event {
	return Ctx(ctx).Debug()
}

func Info(ctx context.Context) *zerolog.Event {
	return Ctx(ctx).Info()
}

func Warn(ctx context.Context) *zerolog.Event {
	return Ctx(ctx).Warn()
}

func Error(ctx context.Context) *zerolog.Event {
	return Ctx(ctx).Error()
}

func Fatal(ctx context.Context) *zerolog.Event {
	return Ctx(ctx).Fatal()
}

func Panic(ctx context.Context) *zerolog.Event {
	return Ctx(ctx).Panic()
}

func WithLevel(ctx context.Context, level zerolog.Level) *zerolog.Event {
	return Ctx(ctx).WithLevel(level)
}

func Log(ctx context.Context) *zerolog.Event {
	return Ctx(ctx).Log()
}

func Print(ctx context.Context, v ...interface{}) {
	Ctx(ctx).Debug().CallerSkipFrame(1).Msg(fmt.Sprint(v...))
}

func Printf(ctx context.Context, format string, v ...interface{}) {
	Ctx(ctx).Debug().CallerSkipFrame(1).Msgf(format, v...)
}

// Ctx returns the Logger associated with the ctx. If ctx is nil or
// has no logger, the DefaultContextLogger (global) is returned.
func Ctx(ctx context.Context) *zerolog.Logger {
	if ctx == nil {
		return zerolog.DefaultContextLogger
	}
	return zerolog.Ctx(ctx)
}
