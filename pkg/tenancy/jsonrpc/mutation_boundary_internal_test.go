package tenancyjsonrpc

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestInternalJSONTokenValidationRejectsEachConflictingState(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("token error")
	for name, test := range map[string]struct {
		token json.Token
		err   error
		want  bool
	}{
		"matching":   {json.Delim('{'), nil, true},
		"mismatched": {json.Delim('['), nil, false},
		"error":      {json.Delim('{'), wantErr, false},
	} {
		if got := matchingDelimiter(test.token, test.err, json.Delim('{')); got != test.want {
			t.Fatalf("matchingDelimiter(%s) = %t, want %t", name, got, test.want)
		}
	}

	for name, test := range map[string]struct {
		token json.Token
		err   error
		want  bool
	}{
		"string":     {"tenant_id", nil, true},
		"non-string": {json.Delim('{'), nil, false},
		"error":      {"tenant_id", wantErr, false},
	} {
		_, got := stringToken(test.token, test.err)
		if got != test.want {
			t.Fatalf("stringToken(%s) valid = %t, want %t", name, got, test.want)
		}
	}
}
