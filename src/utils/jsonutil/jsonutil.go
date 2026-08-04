// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package jsonutils converts between a dotted key path ("0x1.c.s.0x6.a.0x0")
// and its nested map form ({"0x1":{"c":{"s":{"0x6":{"a":{"0x0":<value>}}}}}}).
// Maps are built directly, not via JSON text, so values keep their exact Go type.
package jsonutil

import (
	"fmt"
	"strings"
)

// SplitPath splits a dotted path into segments, erroring on an empty path or
// segment. The single source of truth for path validity.
func SplitPath(path string) ([]string, error) {
	if path == "" {
		return nil, fmt.Errorf("path is empty")
	}
	segments := strings.Split(path, ".")
	for _, seg := range segments {
		if seg == "" {
			return nil, fmt.Errorf("path %q has an empty segment", path)
		}
	}
	return segments, nil
}

// ToJson expands a dotted path and leaf value into a nested map, one nesting
// key per segment.
func ToJson(path string, value interface{}) (map[string]interface{}, error) {
	segments, err := SplitPath(path)
	if err != nil {
		return nil, err
	}

	root := map[string]interface{}{}
	node := root
	for _, seg := range segments[:len(segments)-1] {
		child := map[string]interface{}{}
		node[seg] = child
		node = child
	}
	node[segments[len(segments)-1]] = value
	return root, nil
}

// ToPath flattens a nested map into dotted-path keys, the inverse of ToJson. An
// empty map is kept as a leaf.
func ToPath(nested map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	flatten("", nested, out)
	return out
}

func flatten(prefix string, value interface{}, out map[string]interface{}) {
	m, ok := value.(map[string]interface{})
	if !ok || len(m) == 0 {
		out[prefix] = value
		return
	}
	for k, v := range m {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		flatten(key, v, out)
	}
}
