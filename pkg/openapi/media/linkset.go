package media

import (
	"errors"
	"net/url"
	"strings"
	"unicode"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
)

var (
	// ErrInvalidLinkset reports data that does not follow the RFC 9264 JSON
	// linkset model or cannot be represented by application/linkset.
	ErrInvalidLinkset = errors.New("invalid RFC 9264 linkset")
	// ErrLinksetLimit reports a link-count or serialized-byte bound.
	ErrLinksetLimit = errors.New("RFC 9264 linkset limit exceeded")
)

// ValidateLinksetJSON validates a bounded application/linkset+json value. The
// root object, contexts, relations, targets, and target attributes follow the
// RFC 9264 model. No URI is retrieved.
func ValidateLinksetJSON(
	value jsonvalue.Value,
	maxLinks int,
	maxBytes int,
) error {
	return visitLinkset(value, maxLinks, maxBytes, nil)
}

// SerializeLinkset converts the RFC 9264 JSON linkset model to its equivalent
// application/linkset representation. Links retain source order and are
// separated by a comma and newline; no URI is retrieved.
func SerializeLinkset(
	value jsonvalue.Value,
	maxLinks int,
	maxBytes int,
) (string, error) {
	var output boundedLinksetBuilder
	output.maximum = maxBytes
	err := visitLinkset(value, maxLinks, maxBytes, func(
		anchor string,
		relation string,
		target jsonvalue.Value,
	) error {
		if output.links > 0 && !output.write(",\n") {
			return ErrLinksetLimit
		}
		output.links++
		href, _ := linksetStringMember(target, "href")
		if !output.write("<" + href + ">; rel=" + quotedLinkset(relation)) {
			return ErrLinksetLimit
		}
		if anchor != "" && !output.write("; anchor="+quotedLinkset(anchor)) {
			return ErrLinksetLimit
		}
		members, _ := target.Members()
		for _, member := range members {
			if member.Name == "href" {
				continue
			}
			if err := writeLinksetAttribute(&output, member); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return output.String(), nil
}

type linksetVisitor func(
	anchor string,
	relation string,
	target jsonvalue.Value,
) error

func visitLinkset(
	value jsonvalue.Value,
	maxLinks int,
	maxBytes int,
	visit linksetVisitor,
) error {
	if maxLinks < 1 || maxBytes < 1 || value.Kind() != jsonvalue.ObjectKind {
		return ErrInvalidLinkset
	}
	raw, _ := value.MarshalJSON()
	if len(raw) > maxBytes {
		return ErrLinksetLimit
	}
	root, _ := value.Members()
	if len(root) != 1 || root[0].Name != "linkset" ||
		root[0].Value.Kind() != jsonvalue.ArrayKind {
		return ErrInvalidLinkset
	}
	contexts, _ := root[0].Value.Elements()
	links := 0
	for _, context := range contexts {
		if context.Kind() != jsonvalue.ObjectKind {
			return ErrInvalidLinkset
		}
		members, _ := context.Members()
		anchor := ""
		hasRelation := false
		for _, member := range members {
			if member.Name == "anchor" {
				var valid bool
				anchor, valid = member.Value.Text()
				if !valid || !validLinksetURIReference(anchor) {
					return ErrInvalidLinkset
				}
				continue
			}
			hasRelation = true
			if !validLinkRelation(member.Name) ||
				member.Value.Kind() != jsonvalue.ArrayKind {
				return ErrInvalidLinkset
			}
			targets, _ := member.Value.Elements()
			if len(targets) == 0 {
				return ErrInvalidLinkset
			}
			for _, target := range targets {
				if !validLinkTarget(target) {
					return ErrInvalidLinkset
				}
				links++
				if links > maxLinks {
					return ErrLinksetLimit
				}
				if visit != nil {
					if err := visit(anchor, member.Name, target); err != nil {
						return err
					}
				}
			}
		}
		if !hasRelation {
			return ErrInvalidLinkset
		}
	}
	return nil
}

func validLinkTarget(target jsonvalue.Value) bool {
	if target.Kind() != jsonvalue.ObjectKind {
		return false
	}
	href, exists := linksetStringMember(target, "href")
	if !exists || !validLinksetURIReference(href) {
		return false
	}
	members, _ := target.Members()
	for _, member := range members {
		if member.Name == "href" {
			continue
		}
		if !validLinkAttribute(member) {
			return false
		}
	}
	return true
}

func validLinkAttribute(member jsonvalue.Member) bool {
	switch member.Name {
	case "media", "title", "type":
		value, valid := member.Value.Text()
		return valid && validASCIIFieldText(value)
	case "hreflang":
		return validLinkStringArray(member.Value)
	}
	if !validLinkParameterName(member.Name) ||
		member.Value.Kind() != jsonvalue.ArrayKind {
		return false
	}
	values, _ := member.Value.Elements()
	if len(values) == 0 {
		return false
	}
	if strings.HasSuffix(member.Name, "*") {
		for _, value := range values {
			if !validInternationalLinkAttribute(value) {
				return false
			}
		}
		return true
	}
	for _, value := range values {
		text, valid := value.Text()
		if !valid || !validASCIIFieldText(text) {
			return false
		}
	}
	return true
}

func validLinkStringArray(value jsonvalue.Value) bool {
	if value.Kind() != jsonvalue.ArrayKind {
		return false
	}
	values, _ := value.Elements()
	if len(values) == 0 {
		return false
	}
	for _, item := range values {
		text, valid := item.Text()
		if !valid || !validASCIIFieldText(text) {
			return false
		}
	}
	return true
}

func validInternationalLinkAttribute(value jsonvalue.Value) bool {
	if value.Kind() != jsonvalue.ObjectKind {
		return false
	}
	members, _ := value.Members()
	if len(members) < 1 || len(members) > 2 {
		return false
	}
	content, exists := linksetStringMember(value, "value")
	if !exists || strings.IndexFunc(content, unicode.IsControl) >= 0 {
		return false
	}
	for _, member := range members {
		if member.Name != "value" && member.Name != "language" {
			return false
		}
	}
	if language, exists := value.Lookup("language"); exists {
		text, valid := language.Text()
		if !valid || !validASCIIFieldText(text) {
			return false
		}
	}
	return true
}

func validLinkRelation(value string) bool {
	if value != "" && validLinkParameterName(value) &&
		!strings.HasSuffix(value, "*") {
		return true
	}
	parsed, err := url.Parse(value)
	return err == nil && parsed.IsAbs() && validLinksetURIReference(value)
}

func validLinksetURIReference(value string) bool {
	if !validASCIIFieldText(value) || strings.ContainsAny(value, " <>\"\\") {
		return false
	}
	_, err := url.Parse(value)
	return err == nil
}

func validLinkParameterName(value string) bool {
	for index := range len(value) {
		character := value[index]
		if character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' ||
			strings.ContainsRune("!#$%&'*+-.^_`|~", rune(character)) {
			continue
		}
		return false
	}
	return true
}

func validASCIIFieldText(value string) bool {
	for index := range len(value) {
		character := value[index]
		if character < 0x20 || character > 0x7e {
			return false
		}
	}
	return true
}

func linksetStringMember(
	value jsonvalue.Value,
	name string,
) (string, bool) {
	member, exists := value.Lookup(name)
	if !exists {
		return "", false
	}
	return member.Text()
}

type boundedLinksetBuilder struct {
	strings.Builder
	maximum int
	links   int
}

func (builder *boundedLinksetBuilder) write(value string) bool {
	if len(value) > builder.maximum-builder.Len() {
		return false
	}
	_, _ = builder.WriteString(value)
	return true
}

func writeLinksetAttribute(
	output *boundedLinksetBuilder,
	member jsonvalue.Member,
) error {
	if member.Name == "media" || member.Name == "title" ||
		member.Name == "type" {
		value, _ := member.Value.Text()
		if !output.write("; " + member.Name + "=" + quotedLinkset(value)) {
			return ErrLinksetLimit
		}
		return nil
	}
	values, _ := member.Value.Elements()
	for _, value := range values {
		var serialized string
		if strings.HasSuffix(member.Name, "*") {
			content, _ := linksetStringMember(value, "value")
			language, _ := linksetStringMember(value, "language")
			serialized = "UTF-8'" + language + "'" +
				encodeInternationalLinkAttribute(content)
		} else {
			text, _ := value.Text()
			serialized = quotedLinkset(text)
		}
		if !output.write("; " + member.Name + "=" + serialized) {
			return ErrLinksetLimit
		}
	}
	return nil
}

func quotedLinkset(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	return "\"" + value + "\""
}

func encodeInternationalLinkAttribute(value string) string {
	const hexadecimal = "0123456789ABCDEF"
	var encoded strings.Builder
	for index := range len(value) {
		character := value[index]
		if character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' ||
			strings.ContainsRune("!#$&+-.^_`|~", rune(character)) {
			encoded.WriteByte(character)
			continue
		}
		encoded.WriteByte('%')
		encoded.WriteByte(hexadecimal[character>>4])
		encoded.WriteByte(hexadecimal[character&0x0f])
	}
	return encoded.String()
}
