// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mock

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
)

// MockHTTPClient is a mock implementation of the HTTPClient interface
type MockHTTPClient struct {
	// Store raw body bytes instead of Response objects
	responseBodies map[string]map[string][]byte
	responseCodes  map[string]map[string]int
	Requests       []*http.Request
}

// NewMockHTTPClient creates a new instance of MockHTTPClient with an empty response map
func NewMockHTTPClient() *MockHTTPClient {
	return &MockHTTPClient{
		responseBodies: make(map[string]map[string][]byte),
		responseCodes:  make(map[string]map[string]int),
		Requests:       make([]*http.Request, 0),
	}
}

// RegisterResponse registers a response for a specific URL and HTTP method
func (c *MockHTTPClient) RegisterResponse(url, method string, statusCode int, responseBody interface{}) error {
	if c.responseBodies == nil {
		c.responseBodies = make(map[string]map[string][]byte)
		c.responseCodes = make(map[string]map[string]int)
	}

	if _, ok := c.responseBodies[url]; !ok {
		c.responseBodies[url] = make(map[string][]byte)
		c.responseCodes[url] = make(map[string]int)
	}

	var body []byte
	var err error

	switch v := responseBody.(type) {
	case string:
		body = []byte(v)
	case []byte:
		body = v
	default:
		body, err = json.Marshal(responseBody)
		if err != nil {
			return err
		}
	}

	c.responseBodies[url][method] = body
	c.responseCodes[url][method] = statusCode

	return nil
}

// Do is a dummy implementation for the Do method
func (c *MockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	// Store the request for later verification
	c.Requests = append(c.Requests, req)

	// Return configured response
	if methodMap, ok := c.responseBodies[req.URL.String()]; ok {
		if body, ok := methodMap[req.Method]; ok {
			statusCode := c.responseCodes[req.URL.String()][req.Method]
			// Create a new response with a fresh buffer each time
			return &http.Response{
				StatusCode: statusCode,
				Body:       io.NopCloser(bytes.NewBuffer(body)),
			}, nil
		}
	}
	return &http.Response{
		StatusCode: http.StatusNotFound,
		Body:       io.NopCloser(bytes.NewBufferString("")),
	}, nil
}

// Get is a dummy implementation for the Get method
func (c *MockHTTPClient) Get(url string) (*http.Response, error) {

	return nil, nil
}

// Post is a dummy implementation for the Post method
func (c *MockHTTPClient) Post(url string, bodyType string, body io.Reader) (*http.Response, error) {
	return nil, nil
}

// PostForm is a dummy implementation for the PostForm method
func (c *MockHTTPClient) PostForm(url string, values url.Values) (*http.Response, error) {
	// Your dummy implementation here
	return nil, nil
}

// Head is a dummy implementation for the Head method
func (c *MockHTTPClient) Head(url string) (*http.Response, error) {
	// Your dummy implementation here
	return nil, nil
}
