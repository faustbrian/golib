package opensearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/faustbrian/golib/pkg/search"
)

// Bulk applies one bounded single-tenant mutation unit. It never retries and
// always attributes a returned item to its original operation position.
func (c *Client) Bulk(ctx context.Context, request search.BulkRequest) (search.BulkResult, error) {
	if ctx == nil {
		return search.BulkResult{}, ErrContextRequired
	}
	if c.search == nil {
		return search.BulkResult{}, ErrSearchDisabled
	}
	capabilities := search.Capabilities{ExternalVersion: true, UpdateExisting: false, BulkPartialOutcomes: true}
	if err := request.Validate(capabilities, c.search.Limits); err != nil {
		return search.BulkResult{}, err
	}

	targets := make([]IndexTarget, len(request.Operations))
	for position, operation := range request.Operations {
		target, err := c.search.Resolver.Resolve(ctx, operation.Tenant, operation.Index, IndexWrite)
		if err != nil {
			return search.BulkResult{}, ErrUnsafeIndexTarget
		}
		if !validIndexTarget(target) {
			return search.BulkResult{}, ErrUnsafeIndexTarget
		}
		targets[position] = target
	}

	body := encodeBulkRequest(request.Operations, targets)
	if len(body) > c.search.Limits.MaxBulkBytes {
		return search.BulkResult{}, search.ErrBulkLimit
	}
	path := "/_bulk"
	switch request.Refresh {
	case search.RefreshWaitFor:
		path += "?refresh=wait_for"
	case search.RefreshImmediate:
		path += "?refresh=true"
	}
	responseBody, err := c.executeContent(ctx, OperationBulk, http.MethodPost, path, body, "application/x-ndjson", http.StatusOK)
	if err != nil {
		return unknownBulkResult(request.Operations), markUnknownOutcome(err)
	}
	result, decodeErr := decodeBulkResponse(request.Operations, responseBody)
	if decodeErr != nil {
		return unknownBulkResult(request.Operations), &Failure{Operation: OperationBulk, Category: FailureMalformed, OutcomeKnown: false, cause: decodeErr}
	}
	return result, nil
}

func encodeBulkRequest(operations []search.WriteOperation, targets []IndexTarget) []byte {
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	encoder.SetEscapeHTML(false)
	for position, operation := range operations {
		action := "index"
		if operation.Action == search.ActionDelete {
			action = "delete"
		}
		metadata := map[string]any{
			"_index": targets[position].Name, "_id": operation.ID,
			"version": operation.Version, "version_type": "external", "require_alias": true,
		}
		_ = encoder.Encode(map[string]any{action: metadata})
		if operation.Action != search.ActionDelete {
			_, _ = body.Write(operation.Source)
			_ = body.WriteByte('\n')
		}
	}
	return body.Bytes()
}

func decodeBulkResponse(operations []search.WriteOperation, body []byte) (search.BulkResult, error) {
	type responseItem struct {
		ID      string `json:"_id"`
		Version uint64 `json:"_version"`
		Status  int    `json:"status"`
		Result  string `json:"result"`
		Error   *struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	var payload struct {
		Took   int64                     `json:"took"`
		Errors bool                      `json:"errors"`
		Items  []map[string]responseItem `json:"items"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.Took < 0 || len(payload.Items) != len(operations) {
		return search.BulkResult{}, ErrMalformedResponse
	}
	outcomes := make([]search.ItemOutcome, len(operations))
	hasErrors := false
	for position, item := range payload.Items {
		if len(item) != 1 {
			return search.BulkResult{}, ErrMalformedResponse
		}
		var decoded responseItem
		responseAction := ""
		for action, value := range item {
			responseAction = action
			decoded = value
		}
		operation := operations[position]
		expectedAction := "index"
		if operation.Action == search.ActionDelete {
			expectedAction = "delete"
		}
		if responseAction != expectedAction || decoded.ID != operation.ID || decoded.Status < 200 || decoded.Status > 599 {
			return search.BulkResult{}, ErrMalformedResponse
		}
		if decoded.Status >= 300 {
			hasErrors = true
		} else if decoded.Version != operation.Version || decoded.Error != nil {
			return search.BulkResult{}, ErrMalformedResponse
		}
		state, retryable := classifyBulkItem(operation.Action, decoded.Status, decoded.Error)
		code := ""
		if decoded.Error != nil {
			code = decoded.Error.Type
			if !safeErrorCode(code) {
				code = "unknown"
			}
		}
		outcomes[position] = search.ItemOutcome{Position: position, ID: operation.ID, Action: operation.Action, State: state, Version: decoded.Version, Code: code, Retryable: retryable}
	}
	if payload.Errors != hasErrors {
		return search.BulkResult{}, ErrMalformedResponse
	}
	return search.NewBulkResult(outcomes)
}

func classifyBulkItem(action search.WriteAction, status int, failure *struct {
	Type string `json:"type"`
}) (search.OutcomeState, bool) {
	switch {
	case status >= 200 && status < 300:
		return search.OutcomeApplied, false
	case status == http.StatusNotFound && action == search.ActionDelete:
		return search.OutcomeNotFound, false
	case status == http.StatusConflict:
		return search.OutcomeVersionConflict, false
	case status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable:
		return search.OutcomeRejected, true
	case failure != nil:
		return search.OutcomeFailed, false
	default:
		return search.OutcomeUnknown, false
	}
}

func unknownBulkResult(operations []search.WriteOperation) search.BulkResult {
	items := make([]search.ItemOutcome, len(operations))
	for position, operation := range operations {
		items[position] = search.ItemOutcome{Position: position, ID: operation.ID, Action: operation.Action, State: search.OutcomeUnknown, Retryable: true}
	}
	result, _ := search.NewBulkResult(items)
	return result
}

func markUnknownOutcome(err error) error {
	var failure *Failure
	if errors.As(err, &failure) {
		copyFailure := *failure
		copyFailure.OutcomeKnown = false
		return &copyFailure
	}
	return unknownTransportFailure(OperationBulk, err)
}
