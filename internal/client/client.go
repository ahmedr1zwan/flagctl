// Package client implements the HTTP API shared by flagctl and the
// Terraform provider. It does not access database files.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/ahmedr1zwan/flagctl/internal/flags"
	"github.com/ahmedr1zwan/flagctl/internal/security"
)

const DefaultServer = "http://127.0.0.1:8080"
const maxResponseBytes = 8 << 20

// Config contains file paths only, never raw credentials. Empty CAFile uses system trust.
type Config struct {
	Server    string
	Timeout   time.Duration
	TokenFile string
	CAFile    string
}

type Client struct {
	token   security.Token
	baseURL url.URL
	http    *http.Client
}

// New preserves the unauthenticated loopback workflow.
func New(server string, timeout time.Duration) (*Client, error) {
	return NewWithConfig(Config{Server: server, Timeout: timeout})
}

// NewWithConfig loads credentials once and never sends them over HTTP. Remote
// origins require HTTPS and an explicit token file. Redirects and proxies are off.
func NewWithConfig(config Config) (*Client, error) {
	base, err := parseServer(config.Server)
	if err != nil {
		return nil, err
	}
	if config.Timeout <= 0 {
		return nil, errors.New("--timeout must be greater than zero")
	}
	if base.Scheme != "https" && (config.TokenFile != "" || config.CAFile != "") {
		return nil, errors.New("token and CA files require an HTTPS server")
	}
	if !security.IsLoopback(base) && config.TokenFile == "" {
		return nil, errors.New("remote HTTPS servers require a token file")
	}
	var token security.Token
	if config.TokenFile != "" {
		token, err = security.ReadTokenFile(config.TokenFile)
		if err != nil {
			return nil, err
		}
	}
	tlsConfig, err := security.ClientTLS(config.CAFile)
	if err != nil {
		return nil, err
	}
	timeout := config.Timeout

	transport := &http.Transport{
		// Never route flag data or credentials through environment-configured proxies.
		TLSClientConfig:        tlsConfig,
		Proxy:                  nil,
		DialContext:            (&net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:    timeout,
		ResponseHeaderTimeout:  timeout,
		MaxResponseHeaderBytes: 16 << 10,
		MaxIdleConns:           4,
		IdleConnTimeout:        30 * time.Second,
	}
	return &Client{baseURL: *base, token: token, http: &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}, nil
}

func (c *Client) Close() {
	c.http.CloseIdleConnections()
}

// ValidateServer checks an origin without reading credential files or making requests.
func ValidateServer(server string) error {
	_, err := parseServer(server)
	return err
}

func parseServer(server string) (*url.URL, error) {
	base, err := security.ParseOrigin(server)
	if err != nil {
		return nil, err
	}
	if base.Scheme == "http" && !security.IsLoopback(base) {
		return nil, errors.New("HTTP is allowed only for loopback servers; remote access requires HTTPS")
	}
	return base, nil
}

func (c *Client) Create(ctx context.Context, environment string, input flags.CreateInput) (flags.Flag, error) {
	if err := input.Validate(environment); err != nil {
		return flags.Flag{}, err
	}
	request := struct {
		Key         string `json:"key"`
		Description string `json:"description"`
		Enabled     bool   `json:"enabled"`
	}{input.Key, input.Description, input.Enabled}
	var response flagResponse
	if err := c.do(ctx, http.MethodPost, flagsPath(environment), request, http.StatusCreated, &response); err != nil {
		return flags.Flag{}, err
	}
	flag, err := response.flag(environment)
	if err != nil || flag.Key != input.Key {
		return flags.Flag{}, errors.New("flag service returned an invalid creation response; list flags before retrying")
	}
	return flag, nil
}

func (c *Client) List(ctx context.Context, environment string) ([]flags.Flag, error) {
	if err := flags.ValidateEnvironment(environment); err != nil {
		return nil, err
	}
	var response struct {
		Flags []flagResponse `json:"flags"`
	}
	if err := c.do(ctx, http.MethodGet, flagsPath(environment), nil, http.StatusOK, &response); err != nil {
		return nil, err
	}
	if response.Flags == nil {
		return nil, errors.New("flag service returned an invalid list response")
	}
	result := make([]flags.Flag, 0, len(response.Flags))
	for _, item := range response.Flags {
		flag, err := item.flag(environment)
		if err != nil {
			return nil, errors.New("flag service returned an invalid flag in its list response")
		}
		result = append(result, flag)
	}
	return result, nil
}

func (c *Client) Get(ctx context.Context, environment, key string) (flags.Flag, error) {
	if err := flags.ValidateIdentity(environment, key); err != nil {
		return flags.Flag{}, err
	}
	var response flagResponse
	if err := c.do(ctx, http.MethodGet, flagsPath(environment)+"/"+key, nil, http.StatusOK, &response); err != nil {
		return flags.Flag{}, err
	}
	flag, err := response.flag(environment)
	if err != nil || flag.Key != key {
		return flags.Flag{}, errors.New("flag service returned an invalid get response")
	}
	return flag, nil
}

// Update sets only supplied fields. Explicit false and empty descriptions must
// be sent, while omitted fields must remain untouched by the service.
func (c *Client) Update(ctx context.Context, environment, key string, input flags.UpdateInput) (flags.Flag, error) {
	if err := input.Validate(environment, key); err != nil {
		return flags.Flag{}, err
	}
	request := struct {
		Description *string `json:"description,omitempty"`
		Enabled     *bool   `json:"enabled,omitempty"`
	}{input.Description, input.Enabled}
	var response flagResponse
	if err := c.do(ctx, http.MethodPatch, flagsPath(environment)+"/"+key, request, http.StatusOK, &response); err != nil {
		return flags.Flag{}, err
	}
	flag, err := response.flag(environment)
	if err != nil || flag.Key != key ||
		(input.Enabled != nil && flag.Enabled != *input.Enabled) ||
		(input.Description != nil && flag.Description != *input.Description) {
		return flags.Flag{}, errors.New("flag service returned an invalid update response; use flags get to confirm the current state")
	}
	return flag, nil
}

func (c *Client) Delete(ctx context.Context, environment, key string) error {
	if err := flags.ValidateIdentity(environment, key); err != nil {
		return err
	}
	return c.do(ctx, http.MethodDelete, flagsPath(environment)+"/"+key, nil, http.StatusNoContent, nil)
}

func flagsPath(environment string) string {
	return "/v1/environments/" + environment + "/flags"
}

func (c *Client) do(ctx context.Context, method, path string, payload any, expectedStatus int, result any) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return errors.New("could not encode flag request")
		}
		body = bytes.NewReader(encoded)
	}
	endpoint := c.baseURL
	endpoint.Path = path
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return errors.New("could not construct flag request")
	}
	request.Header.Set("Accept", "application/json")
	if c.token.IsSet() {
		request.Header.Set("Authorization", c.token.Authorization())
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return requestError(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return requestError(err)
	}
	if len(data) > maxResponseBytes {
		return errors.New("flag service response exceeds the 8 MiB limit")
	}
	if response.StatusCode != expectedStatus {
		return parseAPIError(response.StatusCode, data)
	}
	// DELETE acknowledges success with 204, without a JSON body or content type.
	if expectedStatus == http.StatusNoContent {
		return nil
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" || json.Unmarshal(data, result) != nil {
		return errors.New("flag service returned an invalid JSON response")
	}
	return nil
}

func requestError(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return errors.New("flag request canceled")
	case errors.Is(err, context.DeadlineExceeded):
		return errors.New("flag request timed out; check the service or increase --timeout")
	default:
		// Transport errors may include URLs or server-provided content. Never
		// print them verbatim, nor raw response bodies or remote error messages.
		return errors.New("could not communicate with flag service; check --server, ensure flagd is running, and check TLS trust if using HTTPS")
	}
}

// APIError exposes stable status/code information for callers without reflecting
// a potentially sensitive or terminal-active remote error message.
type APIError struct {
	StatusCode int
	Code       string
}

var errorMessages = map[string]string{
	"unauthorized":           "flag service rejected authentication; check the token file",
	"invalid_request":        "flag service rejected the request",
	"forbidden":              "flag service refused access; check the service address",
	"not_found":              "flag or API route was not found",
	"method_not_allowed":     "flag service does not support this operation",
	"already_exists":         "a flag with this key already exists in this environment",
	"request_too_large":      "flag request is too large",
	"unsupported_media_type": "flag service requires a supported JSON content type",
	"internal_error":         "flag service could not complete the operation",
}

func (e *APIError) Error() string {
	if message, ok := errorMessages[e.Code]; ok {
		return fmt.Sprintf("%s (HTTP %d, %s)", message, e.StatusCode, e.Code)
	}
	if e.StatusCode >= 300 && e.StatusCode < 400 {
		return fmt.Sprintf("flag service returned HTTP %d; redirects are not followed", e.StatusCode)
	}
	return fmt.Sprintf("flag service returned unexpected HTTP %d", e.StatusCode)
}

func parseAPIError(status int, body []byte) error {
	var response struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	result := &APIError{StatusCode: status}
	if json.Unmarshal(body, &response) == nil {
		if _, known := errorMessages[response.Error.Code]; known {
			result.Code = response.Error.Code
		}
	}
	return result
}

type flagResponse struct {
	Key         string    `json:"key"`
	Environment string    `json:"environment"`
	Description *string   `json:"description"`
	Enabled     *bool     `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (response flagResponse) flag(environment string) (flags.Flag, error) {
	invalid := errors.New("invalid flag response")
	if response.Environment != environment || response.Description == nil || response.Enabled == nil ||
		response.CreatedAt.IsZero() || response.UpdatedAt.IsZero() {
		return flags.Flag{}, invalid
	}
	input := flags.CreateInput{Key: response.Key, Description: *response.Description, Enabled: *response.Enabled}
	if input.Validate(environment) != nil {
		return flags.Flag{}, invalid
	}
	return flags.Flag{
		Key: input.Key, Environment: environment, Description: input.Description,
		Enabled: input.Enabled, CreatedAt: response.CreatedAt, UpdatedAt: response.UpdatedAt,
	}, nil
}
