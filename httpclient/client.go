package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
)

type Client struct {
	BaseURL   string
	AuthToken string
}

// RequestOptions holds optional parameters for requests
type RequestOptions struct {
	Headers     map[string]string
	QueryParams map[string]string
}

// Response represents a generic API response
type Response struct {
	StatusCode int
	Body       []byte
	Headers    http.Header
}

// APIError represents an error response from the API
type APIError struct {
	StatusCode int
	Message    string
	RawBody    []byte
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API error (status %d): %s", e.StatusCode, e.Message)
}

func New(baseURL, authToken string) *Client {
	return &Client{
		BaseURL:   baseURL,
		AuthToken: authToken,
	}
}

func (c *Client) doRequest(ctx context.Context, method, endpoint string, body interface{}, opts *RequestOptions) (*Response, error) {
	fullURL := fmt.Sprintf("%s%s", c.BaseURL, endpoint)

	// Add query parameters to the URL if options are provided
	if opts != nil && opts.QueryParams != nil {
		q := url.Values{}
		for key, value := range opts.QueryParams {
			q.Add(key, value)
		}

		fullURL = fmt.Sprintf("%s?%s", fullURL, q.Encode())
	}

	var jsonBody []byte

	var err error
	if body != nil {
		jsonBody, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set default headers
	req.Header.Set("Authorization", c.AuthToken)
	req.Header.Set("Content-Type", "application/json")

	// Set optional headers
	if opts != nil && opts.Headers != nil {
		for key, value := range opts.Headers {
			req.Header.Set(key, value)
		}
	}

	client := &http.Client{}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	defer resp.Body.Close()

	responseBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	response := &Response{
		StatusCode: resp.StatusCode,
		Body:       responseBody,
		Headers:    resp.Header,
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return response, &APIError{
			StatusCode: resp.StatusCode,
			Message:    http.StatusText(resp.StatusCode),
			RawBody:    responseBody,
		}
	}

	return response, nil
}

func (c *Client) Get(ctx context.Context, endpoint string, opts *RequestOptions) (*Response, error) {
	return c.doRequest(ctx, http.MethodGet, endpoint, nil, opts)
}

func (c *Client) Post(ctx context.Context, endpoint string, body interface{}, opts *RequestOptions) (*Response, error) {
	return c.doRequest(ctx, http.MethodPost, endpoint, body, opts)
}

func (c *Client) Put(ctx context.Context, endpoint string, body interface{}, opts *RequestOptions) (*Response, error) {
	return c.doRequest(ctx, http.MethodPut, endpoint, body, opts)
}

func (c *Client) Delete(ctx context.Context, endpoint string, opts *RequestOptions) (*Response, error) {
	return c.doRequest(ctx, http.MethodDelete, endpoint, nil, opts)
}

func (c *Client) Patch(ctx context.Context, endpoint string, body interface{}, opts *RequestOptions) (*Response, error) {
	return c.doRequest(ctx, http.MethodPatch, endpoint, body, opts)
}
