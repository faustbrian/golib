package authhttp

import (
	"fmt"
	"sort"
	"strings"

	authentication "github.com/faustbrian/golib/pkg/authentication"
)

// FormatChallenge serializes a challenge for a WWW-Authenticate field value.
func FormatChallenge(challenge authentication.Challenge) (string, error) {
	parameters := challenge.Parameters()
	validated, err := authentication.NewChallenge(challenge.Scheme(), parameters)
	if err != nil {
		return "", err
	}
	if len(parameters) == 0 {
		return validated.Scheme(), nil
	}

	names := make([]string, 0, len(parameters))
	for name := range parameters {
		names = append(names, name)
	}
	sort.Strings(names)
	formatted := make([]string, 0, len(names))
	for _, name := range names {
		formatted = append(formatted, fmt.Sprintf(`%s="%s"`, name, escapeQuoted(parameters[name])))
	}
	return validated.Scheme() + " " + strings.Join(formatted, ", "), nil
}

func escapeQuoted(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	return strings.ReplaceAll(value, `"`, `\"`)
}
