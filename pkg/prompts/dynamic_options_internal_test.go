package prompts

import "testing"

func TestValidateDynamicOptionsRequiresIdentityAndLabel(t *testing.T) {
	t.Parallel()

	for name, option := range map[string]Option[int]{
		"identity": {label: "Label"},
		"label":    {id: "option"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := validateDynamicOptions([]Option[int]{option}, 1); err == nil {
				t.Fatal("validateDynamicOptions() returned nil error")
			}
		})
	}
}
