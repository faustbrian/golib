package authn

import (
	"encoding/json"
	"errors"
	"math"
	"net/netip"
	"testing"
	"time"

	authorization "github.com/faustbrian/golib/pkg/authorization"
)

type principalStub struct {
	anonymous bool
	subject   string
	claims    map[string]any
}

func (principal principalStub) IsAnonymous() bool      { return principal.anonymous }
func (principal principalStub) Subject() string        { return principal.subject }
func (principal principalStub) Claims() map[string]any { return principal.claims }

func TestSubjectMapsExplicitClaimsAndGroups(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	principal := principalStub{subject: "user-1", claims: map[string]any{
		"department":       "finance",
		"active":           true,
		"level":            int32(7),
		"quota":            uint16(9),
		"ratio":            float32(1.5),
		"authenticated_at": now,
		"address":          netip.MustParseAddr("192.0.2.1"),
		"labels":           []string{"b", "a", "a"},
		"none":             nil,
		"groups":           []any{"reviewers", "operators"},
	}}
	subject, err := Subject(principal, Config{
		Kind: authorization.SubjectUser,
		AttributeClaims: map[authorization.AttributeName]string{
			"department": "department", "active": "active", "level": "level",
			"quota": "quota", "ratio": "ratio", "authenticated_at": "authenticated_at",
			"address": "address", "labels": "labels", "none": "none",
		},
		GroupsClaim: "groups",
	})
	if err != nil {
		t.Fatalf("Subject() error = %v", err)
	}
	if subject.Kind != authorization.SubjectUser || subject.ID != "user-1" ||
		len(subject.Groups) != 2 || subject.Groups[0] != "reviewers" {
		t.Errorf("Subject() = %+v", subject)
	}
	if value, _ := subject.Attributes["department"].String(); value != "finance" {
		t.Errorf("department = %q", value)
	}
	if value, _ := subject.Attributes["level"].Int(); value != 7 {
		t.Errorf("level = %d", value)
	}
	if value, _ := subject.Attributes["quota"].Int(); value != 9 {
		t.Errorf("quota = %d", value)
	}
	if value, _ := subject.Attributes["ratio"].Float(); value != 1.5 {
		t.Errorf("ratio = %f", value)
	}
	if value, _ := subject.Attributes["labels"].StringSet(); len(value) != 2 || value[0] != "a" {
		t.Errorf("labels = %v", value)
	}
	if subject.Attributes["none"].Kind() != authorization.ValueNull {
		t.Errorf("none kind = %d", subject.Attributes["none"].Kind())
	}
}

func TestSubjectFailsClosed(t *testing.T) {
	t.Parallel()

	valid := principalStub{subject: "user-1", claims: map[string]any{"value": "ok"}}
	tests := map[string]struct {
		principal Principal
		config    Config
		want      error
	}{
		"nil principal":     {config: Config{Kind: authorization.SubjectUser}, want: ErrNilPrincipal},
		"anonymous":         {principal: principalStub{anonymous: true}, config: Config{Kind: authorization.SubjectUser}, want: ErrAnonymousPrincipal},
		"empty subject":     {principal: principalStub{}, config: Config{Kind: authorization.SubjectUser}, want: ErrInvalidPrincipal},
		"kind":              {principal: valid, config: Config{}, want: ErrInvalidConfig},
		"attribute name":    {principal: valid, config: Config{Kind: authorization.SubjectUser, AttributeClaims: map[authorization.AttributeName]string{"": "value"}}, want: ErrInvalidConfig},
		"claim name":        {principal: valid, config: Config{Kind: authorization.SubjectUser, AttributeClaims: map[authorization.AttributeName]string{"value": ""}}, want: ErrInvalidConfig},
		"missing claim":     {principal: valid, config: Config{Kind: authorization.SubjectUser, AttributeClaims: map[authorization.AttributeName]string{"missing": "missing"}}, want: ErrMissingClaim},
		"unsupported claim": {principal: principalStub{subject: "u", claims: map[string]any{"value": map[string]any{}}}, config: Config{Kind: authorization.SubjectUser, AttributeClaims: map[authorization.AttributeName]string{"value": "value"}}, want: ErrUnsupportedClaim},
		"mixed collection":  {principal: principalStub{subject: "u", claims: map[string]any{"value": []any{"one", 2}}}, config: Config{Kind: authorization.SubjectUser, AttributeClaims: map[authorization.AttributeName]string{"value": "value"}}, want: ErrUnsupportedClaim},
		"uint overflow":     {principal: principalStub{subject: "u", claims: map[string]any{"value": uint64(math.MaxUint64)}}, config: Config{Kind: authorization.SubjectUser, AttributeClaims: map[authorization.AttributeName]string{"value": "value"}}, want: ErrUnsupportedClaim},
		"invalid float":     {principal: principalStub{subject: "u", claims: map[string]any{"value": math.Inf(1)}}, config: Config{Kind: authorization.SubjectUser, AttributeClaims: map[authorization.AttributeName]string{"value": "value"}}, want: authorization.ErrInvalidFloat},
		"groups type":       {principal: principalStub{subject: "u", claims: map[string]any{"groups": "admins"}}, config: Config{Kind: authorization.SubjectUser, GroupsClaim: "groups"}, want: ErrInvalidGroups},
		"groups element":    {principal: principalStub{subject: "u", claims: map[string]any{"groups": []any{"admins", 1}}}, config: Config{Kind: authorization.SubjectUser, GroupsClaim: "groups"}, want: ErrInvalidGroups},
		"groups missing":    {principal: valid, config: Config{Kind: authorization.SubjectUser, GroupsClaim: "groups"}, want: ErrMissingClaim},
		"groups empty":      {principal: principalStub{subject: "u", claims: map[string]any{"groups": []string{""}}}, config: Config{Kind: authorization.SubjectUser, GroupsClaim: "groups"}, want: ErrInvalidGroups},
		"groups limit":      {principal: principalStub{subject: "u", claims: map[string]any{"groups": []string{"a", "b"}}}, config: Config{Kind: authorization.SubjectUser, GroupsClaim: "groups", MaxGroups: 1}, want: ErrGroupLimitExceeded},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := Subject(test.principal, test.config); !errors.Is(err, test.want) {
				t.Errorf("Subject() error = %v, want %v", err, test.want)
			}
		})
	}

	if _, err := claimValue(json.Number("12")); err != nil {
		t.Errorf("claimValue(json.Number) error = %v", err)
	}
}

func TestClaimValueSupportsEveryBoundedScalar(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value any
		kind  authorization.ValueKind
	}{
		{nil, authorization.ValueNull},
		{"value", authorization.ValueString},
		{true, authorization.ValueBool},
		{int(1), authorization.ValueInt},
		{int8(1), authorization.ValueInt},
		{int16(1), authorization.ValueInt},
		{int32(1), authorization.ValueInt},
		{int64(1), authorization.ValueInt},
		{uint(1), authorization.ValueInt},
		{uint8(1), authorization.ValueInt},
		{uint16(1), authorization.ValueInt},
		{uint32(1), authorization.ValueInt},
		{uint64(1), authorization.ValueInt},
		{float32(1), authorization.ValueFloat},
		{float64(1), authorization.ValueFloat},
		{json.Number("1.5"), authorization.ValueFloat},
		{time.Now(), authorization.ValueTime},
		{netip.MustParseAddr("192.0.2.1"), authorization.ValueIP},
		{[]string{"a"}, authorization.ValueStringSet},
		{[]any{"a"}, authorization.ValueStringSet},
	}
	for _, test := range tests {
		value, err := claimValue(test.value)
		if err != nil || value.Kind() != test.kind {
			t.Errorf("claimValue(%T) = (kind %d, %v), want %d", test.value, value.Kind(), err, test.kind)
		}
	}
	if _, err := claimValue(json.Number("invalid")); !errors.Is(err, ErrUnsupportedClaim) {
		t.Errorf("claimValue(invalid number) error = %v, want ErrUnsupportedClaim", err)
	}
}
