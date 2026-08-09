package opensearch

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"

	"github.com/faustbrian/golib/pkg/search"
)

const MaximumIndexPatterns = 32

var indexPattern = regexp.MustCompile(`^[a-z0-9*][a-z0-9*._-]{0,254}$`)

func validPhysicalName(name string) bool {
	return indexTargetPattern.MatchString(name) && name != "." && name != ".."
}

// PutIndexTemplate installs one authorized composable template. The supplied
// definition owns settings, mappings, and analyzers; the definition name is
// not used as a physical index by this operation.
func (c *Client) PutIndexTemplate(ctx context.Context, tenant, name string, patterns []string, priority int, definition search.IndexDefinition) error {
	if tenant == "" || !validPhysicalName(name) || len(patterns) == 0 || len(patterns) > MaximumIndexPatterns || priority < 0 || definition.Fingerprint() == "" {
		return ErrUnsafeIndexTarget
	}
	resources := make([]string, 1, len(patterns)+1)
	resources[0] = name
	for _, pattern := range patterns {
		if !indexPattern.MatchString(pattern) {
			return ErrUnsafeIndexTarget
		}
		resources = append(resources, pattern)
	}
	if err := c.authorizeLifecycle(ctx, tenant, resources...); err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]any{
		"index_patterns": append([]string(nil), patterns...), "priority": priority,
		"template": map[string]any{"settings": definition.Settings(), "mappings": definition.Mappings()},
		"_meta":    map[string]any{"definition_fingerprint": definition.Fingerprint()},
	})
	response, err := c.execute(ctx, OperationTemplate, http.MethodPut, "/_index_template/"+name, body, http.StatusOK)
	if err != nil {
		return err
	}
	return requireAcknowledged(OperationTemplate, response)
}

// DeleteIndexTemplate idempotently removes one authorized template.
func (c *Client) DeleteIndexTemplate(ctx context.Context, tenant, name string) error {
	if tenant == "" || !validPhysicalName(name) {
		return ErrUnsafeIndexTarget
	}
	if err := c.authorizeLifecycle(ctx, tenant, name); err != nil {
		return err
	}
	response, err := c.execute(ctx, OperationTemplate, http.MethodDelete, "/_index_template/"+name, nil, http.StatusOK, http.StatusNotFound)
	if err != nil {
		return err
	}
	if len(response) == 0 {
		return nil
	}
	return requireAcknowledged(OperationTemplate, response)
}

func requireAcknowledged(operation Operation, body []byte) error {
	var response struct {
		Acknowledged bool `json:"acknowledged"`
	}
	if json.Unmarshal(body, &response) != nil || !response.Acknowledged {
		return malformedFailure(operation, ErrLifecycleRejected)
	}
	return nil
}
