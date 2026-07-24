package structplan_test

import (
	"errors"
	"math"
	"strings"
	"testing"

	validation "github.com/faustbrian/golib/pkg/validation"
	"github.com/faustbrian/golib/pkg/validation/structplan"
)

func TestCompileRejectsInvalidLimitsAndUnsupportedRoot(t *testing.T) {
	limits := validation.DefaultLimits()
	limits.MaxDepth = 0
	builder := structplan.New[account](limits)
	if _, err := builder.Compile(); !errors.Is(err, validation.ErrInvalidLimit) {
		t.Fatalf("typed compile error = %v", err)
	}
	if _, err := structplan.CompileTags[int](validation.DefaultLimits()); !errors.Is(err, structplan.ErrUnsupportedKind) {
		t.Fatalf("root error = %v", err)
	}
}

func TestTagCompilerEnforcesStructuralBudgetsAndVisibility(t *testing.T) {
	t.Run("depth", func(t *testing.T) {
		type leaf struct {
			Value string `validate:"required"`
		}
		type middle struct{ Leaf leaf }
		type root struct{ Middle middle }
		limits := validation.DefaultLimits()
		limits.MaxDepth = 1
		if _, err := structplan.CompileTags[root](limits); !errors.Is(err, validation.ErrLimitExceeded) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("tag length", func(t *testing.T) {
		type value struct {
			Name string `validate:"required"`
		}
		limits := validation.DefaultLimits()
		limits.MaxTagLength = 2
		if _, err := structplan.CompileTags[value](limits); !errors.Is(err, validation.ErrLimitExceeded) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("field count", func(t *testing.T) {
		type value struct {
			A string `validate:"required"`
			B string `validate:"required"`
		}
		limits := validation.DefaultLimits()
		limits.MaxStructFields = 1
		if _, err := structplan.CompileTags[value](limits); !errors.Is(err, validation.ErrLimitExceeded) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("visited field count", func(t *testing.T) {
		type value struct {
			A string
			B string
		}
		limits := validation.DefaultLimits()
		limits.MaxStructFields = 1
		if _, err := structplan.CompileTags[value](limits); !errors.Is(err, validation.ErrLimitExceeded) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("inaccessible", func(t *testing.T) {
		type value struct {
			hidden string `validate:"required"`
		}
		_ = value{hidden: "used"}
		if _, err := structplan.CompileTags[value](validation.DefaultLimits()); !errors.Is(err, structplan.ErrInvalidTag) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("ignored and plain", func(t *testing.T) {
		type value struct {
			Ignored string `validate:"-"`
			Plain   string
			hidden  string
		}
		_ = value{hidden: "used"}
		plan, err := structplan.CompileTags[value](validation.DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		if report := plan.Validate(contextFor(t), value{}); !report.Empty() {
			t.Fatalf("report = %v", report)
		}
	})
}

func TestTagGrammarRejectsEveryMalformedParameterShape(t *testing.T) {
	tags := []struct {
		name string
		run  func() error
	}{
		{"empty", func() error {
			type v struct {
				X string `validate:"required,"`
			}
			_, err := structplan.CompileTags[v](validation.DefaultLimits())
			return err
		}},
		{"required parameter", func() error {
			type v struct {
				X string `validate:"required=x"`
			}
			_, err := structplan.CompileTags[v](validation.DefaultLimits())
			return err
		}},
		{"email parameter", func() error {
			type v struct {
				X string `validate:"email=x"`
			}
			_, err := structplan.CompileTags[v](validation.DefaultLimits())
			return err
		}},
		{"negative", func() error {
			type v struct {
				X string `validate:"min=-1"`
			}
			_, err := structplan.CompileTags[v](validation.DefaultLimits())
			return err
		}},
	}
	for _, tt := range tags {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(); !errors.Is(err, structplan.ErrInvalidTag) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestTagRulesCoverAliasesPointersCollectionsAndNumbers(t *testing.T) {
	type alias string
	type values struct {
		Text    alias          `validate:"required,min=2,max=3"`
		Array   [2]int         `validate:"min=1,max=3"`
		Map     map[string]int `validate:"min=1,max=2"`
		Slice   []int          `validate:"min=1,max=2"`
		Integer int            `validate:"min=1,max=2"`
		Uint    uint           `validate:"min=1,max=2"`
		Float   float64        `validate:"min=1,max=2"`
		Bool    bool           `validate:"min=1"`
		Pointer *string        `validate:"required"`
		Email   int            `validate:"email"`
	}
	plan, err := structplan.CompileTags[values](validation.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	report := plan.Validate(contextFor(t), values{
		Text: "long", Map: map[string]int{}, Slice: []int{1, 2, 3},
		Integer: 0, Uint: 3, Float: 1.5,
	})
	for _, code := range []string{"max", "min", "required", "email"} {
		if !report.HasCode(code) {
			t.Errorf("missing %q in %#v", code, report.Violations())
		}
	}
	text := "ok"
	valid := values{Text: "ok", Array: [2]int{}, Map: map[string]int{"a": 1},
		Slice: []int{1}, Integer: 1, Uint: 1, Float: 1,
		Pointer: &text, Email: 0}
	validReport := plan.Validate(contextFor(t), valid)
	if !validReport.HasCode("min") || !validReport.HasCode("email") {
		t.Fatalf("unsupported rules not reported = %#v", validReport.Violations())
	}
}

func TestReflectiveNumericBoundsDoNotLosePrecisionOrAcceptNaN(t *testing.T) {
	type value struct {
		Large uint64  `validate:"min=9007199254740993"`
		Float float64 `validate:"min=0,max=1"`
	}
	plan, err := structplan.CompileTags[value](validation.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	report := plan.Validate(contextFor(t), value{
		Large: 9007199254740992,
		Float: math.NaN(),
	})
	if !report.HasCode("min") || !report.HasCode("max") || report.Len() != 3 {
		t.Fatalf("numeric report = %#v", report.Violations())
	}
}

func TestReflectiveNumericBoundsAreInclusive(t *testing.T) {
	type values struct {
		Integer int     `validate:"min=1,max=1"`
		Uint    uint64  `validate:"min=1,max=1"`
		Float   float64 `validate:"min=1,max=1"`
	}
	plan, err := structplan.CompileTags[values](validation.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if report := plan.Validate(contextFor(t), values{1, 1, 1}); !report.Empty() {
		t.Fatalf("inclusive bounds = %#v", report.Violations())
	}
}

func TestNestedNilPointersAndPathLimitFailPredictably(t *testing.T) {
	type child struct {
		Name     string `validate:"required"`
		Nickname string `validate:"max=5"`
	}
	type parent struct{ Child *child }
	plan, err := structplan.CompileTags[parent](validation.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if report := plan.Validate(contextFor(t), parent{}); !report.HasCode("required") || report.Len() != 1 {
		t.Fatalf("nil nested report = %#v", report.Violations())
	}
	if report := plan.Validate(contextFor(t), parent{Child: &child{Name: "ok"}}); !report.Empty() {
		t.Fatalf("nested pointer report = %#v", report.Violations())
	}
	limits := validation.DefaultLimits()
	limits.MaxPathLength = 2
	ctx, err := validation.NewContext(limits)
	if err != nil {
		t.Fatal(err)
	}
	if report := plan.Validate(ctx, parent{}); !report.HasCode("path_limit") {
		t.Fatalf("path report = %#v", report.Violations())
	}
}

func TestReflectiveEmailAcceptsBareMailbox(t *testing.T) {
	type value struct {
		Email string `validate:"email"`
	}
	plan, err := structplan.CompileTags[value](validation.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if report := plan.Validate(contextFor(t), value{Email: "a@example.com"}); !report.Empty() {
		t.Fatalf("email report = %#v", report.Violations())
	}
}

func TestReflectiveRequiredMatchesTypedArraySemantics(t *testing.T) {
	type value struct {
		Items [1]int `validate:"required"`
	}
	plan, err := structplan.CompileTags[value](validation.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if report := plan.Validate(contextFor(t), value{}); !report.Empty() {
		t.Fatalf("non-empty zero array = %#v", report.Violations())
	}
}

func TestReflectiveRequiredUsesDynamicInterfaceValue(t *testing.T) {
	type value struct {
		Dynamic any `validate:"required"`
	}
	plan, err := structplan.CompileTags[value](validation.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if report := plan.Validate(contextFor(t), value{Dynamic: ""}); !report.HasCode("required") {
		t.Fatalf("empty dynamic value = %#v", report.Violations())
	}
	if report := plan.Validate(contextFor(t), value{Dynamic: "ok"}); !report.Empty() {
		t.Fatalf("non-empty dynamic value = %#v", report.Violations())
	}
}

func TestReflectiveRulesDereferenceNonNilPointers(t *testing.T) {
	type value struct {
		Email *string `validate:"required,email"`
	}
	email := "a@example.com"
	plan, err := structplan.CompileTags[value](validation.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if report := plan.Validate(contextFor(t), value{Email: &email}); !report.Empty() {
		t.Fatalf("pointer email = %#v", report.Violations())
	}
}

func TestReflectiveCollectionsHonorRuntimeCollectionLimit(t *testing.T) {
	type value struct {
		Items []int `validate:"min=1"`
	}
	plan, err := structplan.CompileTags[value](validation.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	limits := validation.DefaultLimits()
	limits.MaxCollectionSize = 1
	ctx, err := validation.NewContext(limits)
	if err != nil {
		t.Fatal(err)
	}
	report := plan.Validate(ctx, value{Items: []int{1, 2}})
	if !report.HasCode("collection_limit") || report.Len() != 1 {
		t.Fatalf("oversized collection = %#v", report.Violations())
	}
}

func TestReflectiveStringsHonorRuntimeStringLimit(t *testing.T) {
	type value struct {
		Name string `validate:"required,min=1"`
	}
	plan, err := structplan.CompileTags[value](validation.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	limits := validation.DefaultLimits()
	limits.MaxStringLength = 3
	ctx, err := validation.NewContext(limits)
	if err != nil {
		t.Fatal(err)
	}
	report := plan.Validate(ctx, value{Name: "abcd"})
	if !report.HasCode("string_limit") || report.Len() != 1 {
		t.Fatalf("oversized string = %#v", report.Violations())
	}
}

func TestCacheRejectsAdditionalTypesAndCompilationFailures(t *testing.T) {
	limits := validation.DefaultLimits()
	limits.MaxCompiledPlans = 1
	cache := structplan.NewCache(limits)
	if _, err := structplan.CompileCached[tagged](cache); err != nil {
		t.Fatal(err)
	}
	type another struct {
		Name string `validate:"required"`
	}
	if _, err := structplan.CompileCached[another](cache); !errors.Is(err, validation.ErrLimitExceeded) {
		t.Fatalf("cache limit error = %v", err)
	}
	badLimits := validation.DefaultLimits()
	badLimits.MaxDepth = 0
	bad := structplan.NewCache(badLimits)
	if _, err := structplan.CompileCached[tagged](bad); err == nil || !strings.Contains(err.Error(), "compile tag plan") {
		t.Fatalf("compile error = %v", err)
	}
}
