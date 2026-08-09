package opensearch

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/faustbrian/golib/pkg/search"
)

var (
	ErrLifecycleRejected = errors.New("search/opensearch: lifecycle response rejected")
	ErrLifecycleDisabled = errors.New("search/opensearch: lifecycle is not configured")
	ErrLifecycleDenied   = errors.New("search/opensearch: lifecycle operation denied")
)

type LifecycleAuthorizer interface {
	Authorize(context.Context, string, []string) error
}
type LifecycleAuthorizerFunc func(context.Context, string, []string) error

func (authorize LifecycleAuthorizerFunc) Authorize(ctx context.Context, tenant string, resources []string) error {
	return authorize(ctx, tenant, append([]string(nil), resources...))
}

type LifecycleConfig struct{ Authorizer LifecycleAuthorizer }

func cloneLifecycleConfig(config *LifecycleConfig) *LifecycleConfig {
	if config == nil {
		return nil
	}
	copyConfig := *config
	return &copyConfig
}

func (client *Client) CreateIndex(ctx context.Context, tenant string, definition search.IndexDefinition) error {
	if tenant == "" || !indexTargetPattern.MatchString(definition.Name()) {
		return ErrUnsafeIndexTarget
	}
	if err := client.authorizeLifecycle(ctx, tenant, definition.Name()); err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]any{"settings": definition.Settings(), "mappings": definition.Mappings()})
	response, err := client.execute(ctx, OperationCreateIndex, http.MethodPut, "/"+definition.Name(), body, http.StatusOK)
	if err != nil {
		return err
	}
	var acknowledged struct {
		Acknowledged       bool `json:"acknowledged"`
		ShardsAcknowledged bool `json:"shards_acknowledged"`
	}
	if json.Unmarshal(response, &acknowledged) != nil || !acknowledged.Acknowledged || !acknowledged.ShardsAcknowledged {
		return malformedFailure(OperationCreateIndex, ErrLifecycleRejected)
	}
	return nil
}

func (client *Client) Reindex(ctx context.Context, tenant, source, target, cursor string) (string, bool, error) {
	if tenant == "" || !indexTargetPattern.MatchString(source) || !indexTargetPattern.MatchString(target) {
		return cursor, false, ErrUnsafeIndexTarget
	}
	if err := client.authorizeLifecycle(ctx, tenant, source, target); err != nil {
		return cursor, false, err
	}
	if cursor == "" {
		body, _ := json.Marshal(map[string]any{"source": map[string]any{"index": source}, "dest": map[string]any{"index": target, "version_type": "external"}, "conflicts": "abort"})
		response, err := client.execute(ctx, OperationReindex, http.MethodPost, "/_reindex?refresh=true&wait_for_completion=false", body, http.StatusOK)
		if err != nil {
			return "", false, err
		}
		var started struct {
			Task string `json:"task"`
		}
		if json.Unmarshal(response, &started) != nil || started.Task == "" || len(started.Task) > 512 {
			return "", false, malformedFailure(OperationReindex, ErrLifecycleRejected)
		}
		return started.Task, false, nil
	}
	if len(cursor) > 512 || containsUnsafePath(cursor) {
		return cursor, false, ErrLifecycleRejected
	}
	response, err := client.execute(ctx, OperationReindex, http.MethodGet, "/_tasks/"+cursor, nil, http.StatusOK)
	if err != nil {
		return cursor, false, err
	}
	var task struct {
		Completed bool `json:"completed"`
		Response  *struct {
			Total            uint64            `json:"total"`
			Created          uint64            `json:"created"`
			Updated          uint64            `json:"updated"`
			VersionConflicts uint64            `json:"version_conflicts"`
			Failures         []json.RawMessage `json:"failures"`
		} `json:"response"`
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(response, &task) != nil {
		return cursor, false, malformedFailure(OperationReindex, ErrLifecycleRejected)
	}
	if !task.Completed {
		return cursor, false, nil
	}
	if task.Response == nil || len(task.Error) > 0 || task.Response.VersionConflicts != 0 || len(task.Response.Failures) != 0 ||
		task.Response.Created > task.Response.Total || task.Response.Updated != task.Response.Total-task.Response.Created {
		return cursor, false, ErrLifecycleRejected
	}
	return cursor, true, nil
}

func (client *Client) VerifyIndex(ctx context.Context, tenant, source, target string) (search.VerificationReport, error) {
	if tenant == "" || !indexTargetPattern.MatchString(source) || !indexTargetPattern.MatchString(target) {
		return search.VerificationReport{}, ErrUnsafeIndexTarget
	}
	if err := client.authorizeLifecycle(ctx, tenant, source, target); err != nil {
		return search.VerificationReport{}, err
	}
	sourceCount, err := client.countIndex(ctx, source)
	if err != nil {
		return search.VerificationReport{}, err
	}
	targetCount, err := client.countIndex(ctx, target)
	if err != nil {
		return search.VerificationReport{}, err
	}
	report := search.VerificationReport{SourceCount: sourceCount, TargetCount: targetCount}
	report.Drift = max(sourceCount, targetCount) - min(sourceCount, targetCount)
	report.Verified = report.Drift == 0
	return report, nil
}

func (client *Client) countIndex(ctx context.Context, index string) (uint64, error) {
	response, err := client.execute(ctx, OperationVerifyIndex, http.MethodGet, "/"+index+"/_count", nil, http.StatusOK)
	if err != nil {
		return 0, err
	}
	var count struct {
		Count  uint64                                  `json:"count"`
		Shards struct{ Total, Successful, Failed int } `json:"_shards"`
	}
	if json.Unmarshal(response, &count) != nil || count.Shards.Total <= 0 || count.Shards.Failed != 0 || count.Shards.Successful != count.Shards.Total {
		return 0, malformedFailure(OperationVerifyIndex, ErrLifecycleRejected)
	}
	return count.Count, nil
}

func (client *Client) ResolveAlias(ctx context.Context, tenant, alias string) (string, error) {
	if tenant == "" || !indexTargetPattern.MatchString(alias) {
		return "", ErrUnsafeIndexTarget
	}
	if err := client.authorizeLifecycle(ctx, tenant, alias); err != nil {
		return "", err
	}
	response, err := client.execute(ctx, OperationResolveAlias, http.MethodGet, "/_alias/"+alias, nil, http.StatusOK)
	if err != nil {
		return "", err
	}
	var payload map[string]struct {
		Aliases map[string]json.RawMessage `json:"aliases"`
	}
	if json.Unmarshal(response, &payload) != nil || len(payload) != 1 {
		return "", malformedFailure(OperationResolveAlias, ErrLifecycleRejected)
	}
	var index string
	for index = range payload {
	}
	value := payload[index]
	if !indexTargetPattern.MatchString(index) {
		return "", ErrUnsafeIndexTarget
	}
	if _, exists := value.Aliases[alias]; !exists {
		return "", malformedFailure(OperationResolveAlias, ErrLifecycleRejected)
	}
	return index, nil
}

func (client *Client) SwapAlias(ctx context.Context, tenant, alias, from, to string) error {
	if tenant == "" || !indexTargetPattern.MatchString(alias) || !indexTargetPattern.MatchString(from) || !indexTargetPattern.MatchString(to) || from == to {
		return ErrUnsafeIndexTarget
	}
	if err := client.authorizeLifecycle(ctx, tenant, alias, from, to); err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]any{"actions": []any{map[string]any{"remove": map[string]any{"index": from, "alias": alias, "must_exist": true}}, map[string]any{"add": map[string]any{"index": to, "alias": alias, "is_write_index": true}}}})
	response, err := client.execute(ctx, OperationSwapAlias, http.MethodPost, "/_aliases", body, http.StatusOK)
	if err != nil {
		return err
	}
	var acknowledged struct {
		Acknowledged bool `json:"acknowledged"`
	}
	if json.Unmarshal(response, &acknowledged) != nil || !acknowledged.Acknowledged {
		return malformedFailure(OperationSwapAlias, ErrLifecycleRejected)
	}
	return nil
}

// AddAlias creates an authorized alias for a physical generation. It is used
// for initial bootstrap; later generation changes use atomic SwapAlias.
func (client *Client) AddAlias(ctx context.Context, tenant, alias, index string, write bool) error {
	if tenant == "" || !validPhysicalName(alias) || !validPhysicalName(index) {
		return ErrUnsafeIndexTarget
	}
	if err := client.authorizeLifecycle(ctx, tenant, alias, index); err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]any{"actions": []any{map[string]any{"add": map[string]any{"index": index, "alias": alias, "is_write_index": write}}}})
	response, err := client.execute(ctx, OperationSwapAlias, http.MethodPost, "/_aliases", body, http.StatusOK)
	if err != nil {
		return err
	}
	return requireAcknowledged(OperationSwapAlias, response)
}

func (client *Client) DeleteIndex(ctx context.Context, tenant, index string) error {
	if tenant == "" || !indexTargetPattern.MatchString(index) {
		return ErrUnsafeIndexTarget
	}
	if err := client.authorizeLifecycle(ctx, tenant, index); err != nil {
		return err
	}
	response, err := client.execute(ctx, OperationDeleteIndex, http.MethodDelete, "/"+index, nil, http.StatusOK)
	if err != nil {
		return err
	}
	var acknowledged struct {
		Acknowledged bool `json:"acknowledged"`
	}
	if json.Unmarshal(response, &acknowledged) != nil || !acknowledged.Acknowledged {
		return malformedFailure(OperationDeleteIndex, ErrLifecycleRejected)
	}
	return nil
}

func (client *Client) authorizeLifecycle(ctx context.Context, tenant string, resources ...string) error {
	if ctx == nil {
		return ErrContextRequired
	}
	if err := ctx.Err(); err != nil {
		return cancelledFailure(OperationLifecycle, err)
	}
	if client.lifecycle == nil {
		return ErrLifecycleDisabled
	}
	if err := client.lifecycle.Authorizer.Authorize(ctx, tenant, append([]string(nil), resources...)); err != nil {
		return ErrLifecycleDenied
	}
	return nil
}

func containsUnsafePath(value string) bool {
	for _, character := range value {
		if character == '/' || character == '\\' || character == '?' || character == '#' || character == '\x00' || character == '\r' || character == '\n' {
			return true
		}
	}
	return false
}
