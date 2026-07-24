package jsonapi

import (
	"errors"
	"strconv"
	"strings"
)

// CursorPage supplies the evidence needed to validate one paginated data
// instance without coupling pagination to a storage implementation.
type CursorPage struct {
	Request     CursorPageRequest
	Links       Links
	Meta        Meta
	Items       []Meta
	HasMore     bool
	HasPrevious bool
	HasNext     bool
}

// Validate checks a top-level paginated data instance.
func (page CursorPage) Validate() error {
	return page.ValidateAt("")
}

// ValidateAt checks paginated data at a top-level or relationship object path.
// The supplied path identifies the object containing links, meta, and data.
func (page CursorPage) ValidateAt(path string) error {
	validator := documentValidator{}
	pageMember, memberErr := cursorPageMember(page.Request.PageMember)
	if memberErr != nil {
		validator.add(path+"/meta", "member-name", memberErr.Error())
		pageMember = "page"
	}
	pageMetaPath := path + "/meta/" + escapePointerToken(pageMember)
	linksPath := path + "/links"
	if _, exists := page.Links["prev"]; !exists {
		validator.add(linksPath+"/prev", "required", "cursor pagination requires a prev link")
	}
	if _, exists := page.Links["next"]; !exists {
		validator.add(linksPath+"/next", "required", "cursor pagination requires a next link")
	}
	validator.validateLinks(page.Links, linksPath)
	if next, exists := page.Links["next"]; exists && !page.Request.BeforePresent {
		validator.validateCursorLinkState(next, page.HasNext, linksPath+"/next", "next")
	}
	if previous, exists := page.Links["prev"]; exists && !page.Request.AfterPresent {
		validator.validateCursorLinkState(previous, page.HasPrevious, linksPath+"/prev", "previous")
	}

	dataPath := path + "/data"
	if page.Request.Size < 1 {
		validator.add(dataPath, "page-size", "used page size must be positive")
	}
	if len(page.Items) > page.Request.Size {
		validator.add(dataPath, "page-size", "page contains more items than the used page size")
	}
	if page.HasMore && len(page.Items) != page.Request.Size {
		validator.add(dataPath, "page-size", "a non-final page must fill the used page size")
	}
	if !page.Request.AfterPresent && !page.Request.BeforePresent && page.HasPrevious {
		validator.add(dataPath, "page-start", "an initial page must start with the first result")
	}

	metadata, _, metaErr := ParseCursorPageMetaAs(page.Meta, pageMember)
	validator.appendCursorMetaError(metaErr, path+"/meta")
	if metaErr == nil {
		if page.Request.Range {
			if page.HasMore &&
				(metadata.RangeTruncated == nil || !*metadata.RangeTruncated) {
				validator.add(
					pageMetaPath+"/rangeTruncated",
					"required",
					"truncated range page must declare rangeTruncated true",
				)
			}
			if !page.HasMore && metadata.RangeTruncated != nil && *metadata.RangeTruncated {
				validator.add(
					pageMetaPath+"/rangeTruncated",
					"inconsistent",
					"rangeTruncated true requires more matching results than fit on the page",
				)
			}
		}
		if !page.Request.Range && metadata.RangeTruncated != nil && *metadata.RangeTruncated {
			validator.add(
				pageMetaPath+"/rangeTruncated",
				"forbidden",
				"rangeTruncated applies only to range pagination",
			)
		}
	}

	for index, itemMeta := range page.Items {
		_, _, itemErr := ParseCursorItemMetaAs(itemMeta, pageMember)
		validator.appendCursorMetaError(
			itemErr,
			dataPath+"/"+strconv.Itoa(index)+"/meta",
		)
	}

	if len(validator.violations) == 0 {
		return nil
	}
	return &ValidationError{Violations: validator.violations}
}

func (validator *documentValidator) validateCursorLinkState(
	link Link,
	hasPage bool,
	path string,
	direction string,
) {
	if hasPage && link.null {
		validator.add(path, "link-state", direction+" page exists, so its link must not be null")
	}
	if !hasPage && !link.null {
		validator.add(path, "link-state", direction+" page does not exist, so its link must be null")
	}
}

func (validator *documentValidator) appendCursorMetaError(err error, targetMetaPath string) {
	if err == nil {
		return
	}
	var validationError *ValidationError
	if !errors.As(err, &validationError) {
		validator.add(targetMetaPath, "invalid", err.Error())
		return
	}
	for _, violation := range validationError.Violations {
		violation.Path = targetMetaPath + strings.TrimPrefix(violation.Path, "/meta")
		validator.violations = append(validator.violations, violation)
	}
}
