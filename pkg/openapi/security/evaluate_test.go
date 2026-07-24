package security_test

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/security"
)

func TestSatisfiedAppliesAlternativeAndCombinedRequirements(t *testing.T) {
	t.Parallel()

	requirements := objectValue(t,
		jsonvalue.Member{Name: "ApiKey", Value: arrayValue(t)},
		jsonvalue.Member{Name: "OAuth", Value: arrayValue(t, stringValue(t, "read"))},
	)
	alternatives := arrayValue(t,
		requirements,
		objectValue(t, jsonvalue.Member{Name: "MutualTLS", Value: arrayValue(t)}),
	)
	for _, test := range []struct {
		name        string
		credentials security.Credentials
		want        bool
	}{
		{name: "combined", credentials: security.Credentials{
			"ApiKey": {}, "OAuth": {"read", "write"},
		}, want: true},
		{name: "missing combined scheme", credentials: security.Credentials{
			"OAuth": {"read"},
		}},
		{name: "alternative", credentials: security.Credentials{
			"MutualTLS": {},
		}, want: true},
		{name: "missing scope", credentials: security.Credentials{
			"ApiKey": {}, "OAuth": {"write"},
		}},
	} {
		actual, err := security.Satisfied(
			alternatives, test.credentials, security.DefaultLimits(),
		)
		if err != nil {
			t.Fatal(err)
		}
		if actual != test.want {
			t.Fatalf("%s satisfied = %t, want %t", test.name, actual, test.want)
		}
	}
}

func TestSatisfiedHandlesOptionalAndDisabledSecurity(t *testing.T) {
	t.Parallel()

	for _, requirements := range []jsonvalue.Value{
		arrayValue(t),
		arrayValue(t, objectValue(t)),
	} {
		actual, err := security.Satisfied(
			requirements, nil, security.DefaultLimits(),
		)
		if err != nil || !actual {
			t.Fatalf("Satisfied() = %t, %v", actual, err)
		}
	}
}

func TestSatisfiedEvaluatesRequiredRoleLabels(t *testing.T) {
	t.Parallel()

	requirements := arrayValue(t, objectValue(t,
		jsonvalue.Member{
			Name:  "ApiKey",
			Value: arrayValue(t, stringValue(t, "admin")),
		},
	))
	for _, test := range []struct {
		credentials security.Credentials
		want        bool
	}{
		{credentials: security.Credentials{"ApiKey": {"admin", "editor"}}, want: true},
		{credentials: security.Credentials{"ApiKey": {"editor"}}},
	} {
		actual, err := security.Satisfied(
			requirements, test.credentials, security.DefaultLimits(),
		)
		if err != nil {
			t.Fatal(err)
		}
		if actual != test.want {
			t.Fatalf("Satisfied() = %t, want %t", actual, test.want)
		}
	}
}

func TestSatisfiedRejectsMalformedAndBoundedRequirements(t *testing.T) {
	t.Parallel()

	invalid := []jsonvalue.Value{
		jsonvalue.Null(),
		arrayValue(t, jsonvalue.Null()),
		arrayValue(t, objectValue(t,
			jsonvalue.Member{Name: "OAuth", Value: jsonvalue.Null()},
		)),
		arrayValue(t, objectValue(t,
			jsonvalue.Member{Name: "OAuth", Value: arrayValue(t, jsonvalue.Null())},
		)),
	}
	for _, requirements := range invalid {
		if _, err := security.Satisfied(
			requirements, nil, security.DefaultLimits(),
		); !errors.Is(err, security.ErrInvalidRequirements) {
			t.Fatalf("malformed requirement error = %v", err)
		}
	}
	limits := security.DefaultLimits()
	limits.MaxAlternatives = 1
	if _, err := security.Satisfied(
		arrayValue(t, objectValue(t), objectValue(t)), nil, limits,
	); !errors.Is(err, security.ErrLimitExceeded) {
		t.Fatalf("alternative limit error = %v", err)
	}
	for _, invalidLimits := range []security.Limits{
		{MaxAlternatives: -1},
		{MaxSchemes: -1},
		{MaxScopes: -1},
	} {
		if _, err := security.Satisfied(
			arrayValue(t), nil, invalidLimits,
		); !errors.Is(err, security.ErrInvalidRequirements) {
			t.Fatalf("negative limits error = %v", err)
		}
	}

	twoSchemes := arrayValue(t, objectValue(t,
		jsonvalue.Member{Name: "First", Value: arrayValue(t)},
		jsonvalue.Member{Name: "Second", Value: arrayValue(t)},
	))
	if _, err := security.Satisfied(twoSchemes, nil, security.Limits{
		MaxAlternatives: 1, MaxSchemes: 1, MaxScopes: 1,
	}); !errors.Is(err, security.ErrLimitExceeded) {
		t.Fatalf("scheme limit error = %v", err)
	}
	twoScopes := arrayValue(t, objectValue(t,
		jsonvalue.Member{Name: "OAuth", Value: arrayValue(t,
			stringValue(t, "read"), stringValue(t, "write"),
		)},
	))
	if _, err := security.Satisfied(twoScopes, nil, security.Limits{
		MaxAlternatives: 1, MaxSchemes: 1, MaxScopes: 1,
	}); !errors.Is(err, security.ErrLimitExceeded) {
		t.Fatalf("scope limit error = %v", err)
	}
}

func TestSatisfiedAppliesZeroValueDefaultLimits(t *testing.T) {
	t.Parallel()

	actual, err := security.Satisfied(arrayValue(t, objectValue(t)), nil,
		security.Limits{})
	if err != nil || !actual {
		t.Fatalf("Satisfied() = %t, %v", actual, err)
	}
}

func TestSatisfiedAcceptsExactResourceLimits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		requirements jsonvalue.Value
		credentials  security.Credentials
	}{
		{
			name: "alternatives",
			requirements: arrayValue(t,
				objectValue(t),
				objectValue(t),
			),
		},
		{
			name: "schemes",
			requirements: arrayValue(t, objectValue(t,
				jsonvalue.Member{Name: "First", Value: arrayValue(t)},
				jsonvalue.Member{Name: "Second", Value: arrayValue(t)},
			)),
			credentials: security.Credentials{"First": {}, "Second": {}},
		},
		{
			name: "scopes",
			requirements: arrayValue(t, objectValue(t,
				jsonvalue.Member{Name: "OAuth", Value: arrayValue(t,
					stringValue(t, "read"),
					stringValue(t, "write"),
				)},
			)),
			credentials: security.Credentials{"OAuth": {"read", "write"}},
		},
	}
	for _, test := range tests {
		actual, err := security.Satisfied(
			test.requirements,
			test.credentials,
			security.Limits{MaxAlternatives: 2, MaxSchemes: 2, MaxScopes: 2},
		)
		if err != nil || !actual {
			t.Fatalf("%s exact limit = %t, %v", test.name, actual, err)
		}
	}
}

func stringValue(t *testing.T, raw string) jsonvalue.Value {
	t.Helper()
	value, err := jsonvalue.String(raw)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func arrayValue(t *testing.T, values ...jsonvalue.Value) jsonvalue.Value {
	t.Helper()
	value, err := jsonvalue.Array(values)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func objectValue(t *testing.T, members ...jsonvalue.Member) jsonvalue.Value {
	t.Helper()
	value, err := jsonvalue.Object(members)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
