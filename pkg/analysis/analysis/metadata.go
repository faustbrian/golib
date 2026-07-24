// Package analysis defines shared contracts used by every analyzer and report.
package analysis

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Category groups rules by the engineering policy they enforce.
type Category string

const (
	CategoryArchitecture  Category = "architecture"
	CategoryContext       Category = "context"
	CategoryHTTP          Category = "http"
	CategoryLifecycle     Category = "lifecycle"
	CategorySecurity      Category = "security"
	CategoryAPI           Category = "api"
	CategorySafety        Category = "safety"
	CategoryObservability Category = "observability"
)

// Severity determines how prominently a diagnostic is reported.
type Severity string

const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

// Status controls whether a rule informs developers or blocks a build.
type Status string

const (
	StatusDisabled Status = "disabled"
	StatusAdvisory Status = "advisory"
	StatusBlocking Status = "blocking"
)

// ConfigurationType is a primitive accepted by a rule's configuration.
type ConfigurationType string

const (
	ConfigurationString  ConfigurationType = "string"
	ConfigurationBoolean ConfigurationType = "boolean"
	ConfigurationInteger ConfigurationType = "integer"
	ConfigurationArray   ConfigurationType = "array"
	ConfigurationObject  ConfigurationType = "object"
)

// ConfigurationProperty documents one accepted rule configuration key.
type ConfigurationProperty struct {
	Type        ConfigurationType `json:"type"`
	Description string            `json:"description,omitempty"`
	Required    bool              `json:"required,omitempty"`
}

// ConfigurationSchema documents the complete rule-specific configuration.
type ConfigurationSchema struct {
	Properties map[string]ConfigurationProperty `json:"properties,omitempty"`
}

// Rule contains stable, machine-readable metadata for one policy rule.
type Rule struct {
	ID                string              `json:"id"`
	Category          Category            `json:"category"`
	Severity          Severity            `json:"severity"`
	DefaultStatus     Status              `json:"default_status"`
	Rationale         string              `json:"rationale"`
	Remediation       string              `json:"remediation"`
	IntroducedVersion string              `json:"introduced_version"`
	Configuration     ConfigurationSchema `json:"configuration"`
}

var (
	ruleIDPattern  = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*/[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
	versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
)

// Validate rejects incomplete or unstable rule metadata.
func (rule Rule) Validate() error {
	if !ruleIDPattern.MatchString(rule.ID) {
		return errors.New("rule ID must use category/kebab-case syntax")
	}
	if !validCategory(rule.Category) {
		return fmt.Errorf("unknown category %q", rule.Category)
	}
	if !validSeverity(rule.Severity) {
		return fmt.Errorf("unknown severity %q", rule.Severity)
	}
	if !validStatus(rule.DefaultStatus) {
		return fmt.Errorf("unknown default status %q", rule.DefaultStatus)
	}
	if strings.TrimSpace(rule.Rationale) == "" {
		return errors.New("rationale is required")
	}
	if strings.TrimSpace(rule.Remediation) == "" {
		return errors.New("remediation is required")
	}
	if !versionPattern.MatchString(rule.IntroducedVersion) {
		return errors.New("introduced version must be a semantic version")
	}

	return nil
}

func validCategory(category Category) bool {
	switch category {
	case CategoryArchitecture, CategoryContext, CategoryHTTP, CategoryLifecycle,
		CategorySecurity, CategoryAPI, CategorySafety, CategoryObservability:
		return true
	default:
		return false
	}
}

func validSeverity(severity Severity) bool {
	switch severity {
	case SeverityInfo, SeverityWarning, SeverityError:
		return true
	default:
		return false
	}
}

func validStatus(status Status) bool {
	switch status {
	case StatusDisabled, StatusAdvisory, StatusBlocking:
		return true
	default:
		return false
	}
}
