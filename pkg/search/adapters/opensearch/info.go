package opensearch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"unicode"
	"unicode/utf8"
)

const maximumClusterInfoIdentityBytes = 1_024

var (
	// ErrTransport identifies a request for which no usable response exists.
	ErrTransport = errors.New("search/opensearch: transport failed")
	// ErrMalformedResponse identifies invalid or incomplete OpenSearch JSON.
	ErrMalformedResponse = errors.New("search/opensearch: response is malformed")
	// ErrResponseTooLarge identifies a response above its configured byte bound.
	ErrResponseTooLarge = errors.New("search/opensearch: response is too large")
	// ErrUnexpectedStatus identifies an HTTP response not accepted by the
	// operation-specific contract.
	ErrUnexpectedStatus = errors.New("search/opensearch: unexpected response status")
)

// ClusterInfo is the bounded identity needed for compatibility and diagnostics.
type ClusterInfo struct {
	Node        string
	Cluster     string
	ClusterUUID string
	Version     string
}

// Info reads the OpenSearch root identity without retaining the response body.
func (c *Client) Info(ctx context.Context) (info ClusterInfo, err error) {
	if ctx == nil {
		return ClusterInfo{}, ErrContextRequired
	}
	if err := ctx.Err(); err != nil {
		return ClusterInfo{}, cancelledFailure(OperationInfo, err)
	}
	if err := c.begin(); err != nil {
		return ClusterInfo{}, err
	}
	requestCtx, cancel := context.WithTimeout(withOperation(ctx, OperationInfo), c.timeout)
	defer cancel()

	request, _ := http.NewRequestWithContext(requestCtx, http.MethodGet, "/", nil)
	response, transportErr := c.client.Stream(request)
	if response == nil {
		return ClusterInfo{}, transportFailure(OperationInfo, transportErr)
	}
	defer func() { _ = response.Body.Close() }()
	if transportErr != nil {
		return ClusterInfo{}, transportFailure(OperationInfo, transportErr)
	}
	if response.StatusCode != http.StatusOK {
		body, err := readBounded(response.Body, c.maximumResponseBytes)
		if err != nil {
			return ClusterInfo{}, malformedFailure(OperationInfo, err)
		}

		return ClusterInfo{}, responseFailure(OperationInfo, response.StatusCode, body)
	}

	body, err := readBounded(response.Body, c.maximumResponseBytes)
	if err != nil {
		return ClusterInfo{}, malformedFailure(OperationInfo, err)
	}
	var payload struct {
		Name        string `json:"name"`
		ClusterName string `json:"cluster_name"`
		ClusterUUID string `json:"cluster_uuid"`
		Version     struct {
			Number string `json:"number"`
		} `json:"version"`
	}
	if err := json.Unmarshal(body, &payload); err != nil ||
		!validClusterInfoIdentity(payload.Name) || !validClusterInfoIdentity(payload.ClusterName) ||
		!validClusterInfoIdentity(payload.ClusterUUID) || !validClusterInfoIdentity(payload.Version.Number) {
		return ClusterInfo{}, malformedFailure(OperationInfo, ErrMalformedResponse)
	}

	return ClusterInfo{
		Node: payload.Name, Cluster: payload.ClusterName,
		ClusterUUID: payload.ClusterUUID, Version: payload.Version.Number,
	}, nil
}

func validClusterInfoIdentity(value string) bool {
	if value == "" || len(value) > maximumClusterInfoIdentityBytes {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func (c *Client) begin() error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.mu.closed {
		return ErrClosed
	}

	return nil
}

func readBounded(reader io.Reader, maximum int64) ([]byte, error) {
	if maximum == math.MaxInt64 {
		return nil, ErrResponseTooLarge
	}
	limited := io.LimitReader(reader, maximum)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, ErrMalformedResponse
	}
	if int64(len(body)) == maximum {
		extra, err := io.ReadAll(io.LimitReader(reader, 1))
		if err != nil {
			return nil, ErrMalformedResponse
		}
		if len(extra) != 0 {
			return nil, ErrResponseTooLarge
		}
	}
	if !utf8.Valid(body) {
		return nil, ErrMalformedResponse
	}

	return body, nil
}

type statusError struct{ status int }

func (err *statusError) Error() string {
	return fmt.Sprintf("%s: HTTP %d", ErrUnexpectedStatus, err.status)
}

func (err *statusError) Unwrap() error { return ErrUnexpectedStatus }
