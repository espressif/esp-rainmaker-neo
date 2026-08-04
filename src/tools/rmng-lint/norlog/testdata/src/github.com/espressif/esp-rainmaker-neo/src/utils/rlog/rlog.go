// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package rlog

import "context"

type Logger struct{}

func Err(ctx context.Context, err error) *Logger             { return nil }
func Trace(ctx context.Context) *Logger                      { return nil }
func Debug(ctx context.Context) *Logger                      { return nil }
func Info(ctx context.Context) *Logger                       { return nil }
func Warn(ctx context.Context) *Logger                       { return nil }
func Error(ctx context.Context) *Logger                      { return nil }
func Fatal(ctx context.Context) *Logger                      { return nil }
func Panic(ctx context.Context) *Logger                      { return nil }
func WithLevel(ctx context.Context, l int) *Logger           { return nil }
func Log(ctx context.Context) *Logger                        { return nil }
func Print(ctx context.Context, v ...interface{})            {}
func Printf(ctx context.Context, f string, v ...interface{}) {}

func Ctx(ctx context.Context) *Logger { return nil }
func With() *Logger                   { return nil }

func (l *Logger) Msg(msg string)                  {}
func (l *Logger) Msgf(f string, v ...interface{}) {}
func (l *Logger) Send()                           {}
func (l *Logger) Err(err error) *Logger           { return l }
func (l *Logger) Str(k, v string) *Logger         { return l }
func (l *Logger) Error() *Logger                  { return l }
func (l *Logger) Info() *Logger                   { return l }
func (l *Logger) Warn() *Logger                   { return l }
func (l *Logger) Debug() *Logger                  { return l }
