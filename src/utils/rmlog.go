// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"io"
	"os"

	"github.com/rs/zerolog"
)

type RlogType struct {
	zerolog.Logger
}

var Rlog RlogType

/* Read the Rlog environment variable, it should be of the format:
 * RLOG={"level":"info","allow":{"filterX":"filterXValue","uid":"SomeUserId"}}
 *
 * Supported levels: trace < debug < info < warn <  [on-by-default:  error < fatal < panic ]
 * Everything "error" and above is always logged
 * Filters are optional, and are applicable only to stuff below error (i.e. error onwards everything is always logged)
 */
func init() {
	RlogEnv := os.Getenv("RLOG")
	RlogJSON := make(map[string]interface{})
	if RlogEnv != "" {
		if err := json.Unmarshal([]byte(RlogEnv), &RlogJSON); err != nil {
			fmt.Printf("Error unmarshalling RLOG environment variable: :%s: %s\n", RlogEnv, err)
			// Ignore the error, we are in init()
		}
	}

	zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	InitLogger(RlogJSON)
}

func InitLogger(logConfig map[string]interface{}) {
	// Create a writer that implements io.Writer interface with our filtering logic
	// Can consider switching to JSON based logging once we identify the benefits
	filterWriter := &FilterWriter{out: zerolog.ConsoleWriter{Out: os.Stderr}, allowFilters: make(map[string]string)}

	level, ok := logConfig["level"]
	if ok {
		levelStr := level.(string)
		level, err := zerolog.ParseLevel(levelStr)
		if err != nil {
			fmt.Printf("Error parsing level: %s\n", err)
			// Ignore the error, we are in init()
		}
		zerolog.SetGlobalLevel(level)
	}

	filters, ok := logConfig["allow"]
	if ok {
		for k, v := range filters.(map[string]interface{}) {
			filterWriter.AddAllowFilter(k, v.(string))
		}
	}

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	zerolog.ErrorStackMarshaler = RMErrorMarshaler

	Rlog = RlogType{zerolog.New(filterWriter).With().Timestamp().Caller().Stack().Logger()}
	zerolog.DefaultContextLogger = &Rlog.Logger
}

// FilterWriter implements custom filtering logic
type FilterWriter struct {
	out          io.Writer
	allowFilters map[string]string
}

func (w *FilterWriter) AddAllowFilter(key, value string) {
	w.allowFilters[key] = value
}

func (w *FilterWriter) Write(p []byte) (n int, err error) {
	return w.WriteLevel(zerolog.InfoLevel, p)
}

func (w *FilterWriter) WriteLevel(level zerolog.Level, p []byte) (n int, err error) {
	// If the log level is greater than error, no filtering is done
	if level >= zerolog.ErrorLevel {
		return w.out.Write(p)
	}

	// Try to parse the log entry
	var logEntry map[string]interface{}
	if err := json.Unmarshal(p, &logEntry); err != nil {
		// If we can't parse it, write it anyway
		return w.out.Write(p)
	}

	for k, v := range w.allowFilters {
		if log_value, exists := logEntry[k]; exists {
			// Only write if the value matches the filter
			if log_value == v {
				return w.out.Write(p)
			}
			// Don't write if the value doesn't match the filter
			return len(p), nil
		}
	}

	// Write if the filter 'key' doesn't exist in the log entry
	return w.out.Write(p)
}

// RMErrorMarshaler implements custom stack marshaling for rmerror.RMError
func RMErrorMarshaler(err error) interface{} {
	var frames []map[string]interface{}

	for err != nil {
		if rmErr, ok := err.(*rmerror.RMError); ok {
			frame := map[string]interface{}{
				"file":    rmErr.File,
				"line":    rmErr.Line,
				"message": rmErr.Message,
			}
			frames = append(frames, frame)
		} else {
			// For non-rmerror.RMError types, just add the error message
			frame := map[string]interface{}{
				"message": err.Error(),
			}
			frames = append(frames, frame)
		}
		err = errors.Unwrap(err)
	}

	return frames
}
