package jsonapi

import "testing"

func TestIncludedIdentityFollowsValidationContext(t *testing.T) {
	t.Parallel()

	for context, want := range map[ValidationContext]identityRequirement{
		GenericDocument:           identityEither,
		Response:                  identityID,
		CreateRequest:             identityEither,
		UpdateRequest:             identityEither,
		ToOneRelationshipRequest:  identityEither,
		ToManyRelationshipRequest: identityEither,
	} {
		validator := documentValidator{options: ValidationOptions{Context: context}}
		if got := validator.includedIdentity(); got != want {
			t.Fatalf("context %d uses identity requirement %d, want %d", context, got, want)
		}
	}
}

func TestExplicitEmptyLinkMembersAreValidated(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		link Link
		path string
		code string
	}{
		"relation": {
			link: URI("/").WithRel(""),
			path: "/link/rel",
			code: "link-relation",
		},
		"media type": {
			link: URI("/").WithType(""),
			path: "/link/type",
			code: "media-type",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			validator := documentValidator{}
			validator.validateLink(test.link, "/link")
			if !hasViolation(
				&ValidationError{Violations: validator.violations},
				test.path,
				test.code,
			) {
				t.Fatalf("explicit empty member escaped validation: %#v", validator.violations)
			}
		})
	}
}

func TestURIGrammarExactBoundaries(t *testing.T) {
	t.Parallel()

	references := map[string]struct {
		absolute bool
		valid    bool
	}{
		"mailto:user@example.com": {absolute: true, valid: true},
		"a:/path":                 {absolute: true, valid: true},
		"/path:a":                 {valid: true},
		"relative":                {valid: true},
		"http:///path":            {absolute: true, valid: true},
		"http://[::1":             {absolute: false, valid: false},
	}
	for value, want := range references {
		absolute, valid := parseURIReference(value)
		if absolute != want.absolute || valid != want.valid {
			t.Fatalf(
				"URI-reference %q classified as absolute=%v valid=%v, want absolute=%v valid=%v",
				value,
				absolute,
				valid,
				want.absolute,
				want.valid,
			)
		}
	}

	for value, want := range map[string]bool{
		"":     false,
		"v1.a": true,
		"Vf.z": true,
		"v.a":  false,
		"x1.a": false,
		"v1.":  false,
	} {
		if got := validIPLiteral(value); got != want {
			t.Fatalf("IP literal %q validity = %v, want %v", value, got, want)
		}
	}
	for value, want := range map[string]bool{
		"scheme": true,
		"a":      true,
		"1bad":   false,
		"-bad":   false,
	} {
		if got := validURIScheme(value); got != want {
			t.Fatalf("URI scheme %q validity = %v, want %v", value, got, want)
		}
	}
}

func TestExtensionMemberNameClassificationBoundaries(t *testing.T) {
	t.Parallel()

	for value, want := range map[string]bool{
		"ext:value":      true,
		"value":          false,
		"bad-name:value": false,
		"ext:@value":     false,
	} {
		if got := validExtensionMemberName(value); got != want {
			t.Fatalf("extension member %q validity = %v, want %v", value, got, want)
		}
	}
}

func TestURIComponentPercentEncodingBoundaries(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"%00", "%af", "%AF", "a%20b", "%20%21"} {
		if !validURIComponent(value, "") {
			t.Fatalf("valid encoded component rejected: %q", value)
		}
	}
	for _, value := range []string{"%", "%0", "%0G", "%G0", "%20%"} {
		if validURIComponent(value, "") {
			t.Fatalf("invalid encoded component accepted: %q", value)
		}
	}
}

func TestHTTPStatusGrammarExactBoundaries(t *testing.T) {
	t.Parallel()

	for _, status := range []string{"100", "200", "599"} {
		if !validHTTPStatus(status) {
			t.Fatalf("valid HTTP status rejected: %q", status)
		}
	}
	for _, status := range []string{"099", "600", "1/0", "1:0", "10/", "10:"} {
		if validHTTPStatus(status) {
			t.Fatalf("invalid HTTP status accepted: %q", status)
		}
	}
}

func TestErrorIDAloneSatisfiesRequiredMemberRule(t *testing.T) {
	t.Parallel()

	if err := (Document{Errors: []ErrorObject{{ID: "error-1"}}}).Validate(); err != nil {
		t.Fatalf("error object containing only an id was rejected: %v", err)
	}
}

func TestRelationshipIdentifierTraversalClassifiesEveryShape(t *testing.T) {
	t.Parallel()

	one := Identifier{Type: "people", ID: "1"}
	many := []Identifier{{Type: "people", ID: "2"}, {Type: "people", ID: "3"}}
	identifiers := relationshipIdentifiers(Relationships{
		"missing": {},
		"null":    {Data: NullRelationship()},
		"one":     {Data: ToOne(one)},
		"many":    {Data: ToMany(many...)},
		"@ignored": {
			Data: ToOne(Identifier{Type: "people", ID: "ignored"}),
		},
	})
	ids := make(map[string]struct{}, len(identifiers))
	for _, identifier := range identifiers {
		ids[identifier.ID] = struct{}{}
	}
	if len(identifiers) != 3 || len(ids) != 3 {
		t.Fatalf("unexpected relationship identifier traversal: %#v", identifiers)
	}
	for _, id := range []string{"1", "2", "3"} {
		if _, exists := ids[id]; !exists {
			t.Fatalf("relationship identifier %q was not traversed: %#v", id, identifiers)
		}
	}
}

func TestValidationHelpersRejectIncompleteSingularData(t *testing.T) {
	t.Parallel()

	validator := documentValidator{}
	validator.validateResourceMutation(
		&PrimaryData{kind: primaryDataOne},
		"/data",
		identityID,
	)
	if !hasViolation(
		&ValidationError{Violations: validator.violations},
		"/data",
		"shape",
	) {
		t.Fatalf("incomplete mutation data escaped validation: %#v", validator.violations)
	}

	if got := validator.primaryLinkage(&PrimaryData{kind: primaryDataOne}); got != nil {
		t.Fatalf("incomplete primary data produced linkage: %#v", got)
	}
}
