package tenancypostgres_test

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/tenancy"
	tenancypostgres "github.com/faustbrian/golib/pkg/tenancy/postgres"
)

func TestPredicateRequiresTenantScopeAndQuotesOwnedIdentifier(t *testing.T) {
	t.Parallel()

	scope, _ := tenancy.NewTenantScope(tenancy.MustTenantID("tenant-a"), tenancy.Metadata{})
	predicate, err := tenancypostgres.Predicate(scope, "tenant_id", 3)
	if err != nil {
		t.Fatalf("Predicate() error = %v", err)
	}
	if predicate.Clause != `"tenant_id" = $3` || !reflect.DeepEqual(predicate.Arguments, []any{"tenant-a"}) {
		t.Fatalf("predicate = %#v", predicate)
	}

	reason, _ := tenancy.NewAdministrativeReason("operator", "maintenance", "")
	system, _ := tenancy.NewSystemScope(tenancy.NewSystemCapability(reason), tenancy.Metadata{})
	if _, err := tenancypostgres.Predicate(system, "tenant_id", 1); !errors.Is(err, tenancy.ErrTenantScopeRequired) {
		t.Fatalf("Predicate(system) error = %v", err)
	}
	if _, err := tenancypostgres.Predicate(scope, `tenant_id" OR true --`, 1); !errors.Is(err, tenancypostgres.ErrInvalidIdentifier) {
		t.Fatalf("Predicate(injection) error = %v", err)
	}
	if _, err := tenancypostgres.Predicate(scope, "tenant_id", 0); !errors.Is(err, tenancypostgres.ErrInvalidParameter) {
		t.Fatalf("Predicate(parameter) error = %v", err)
	}
	for _, parameter := range []int{1, 65535} {
		predicate, err := tenancypostgres.Predicate(scope, "tenant_id", parameter)
		if err != nil || predicate.Clause != `"tenant_id" = $`+strconv.Itoa(parameter) {
			t.Fatalf("Predicate(parameter %d) = %#v, %v", parameter, predicate, err)
		}
	}
	if _, err := tenancypostgres.Predicate(scope, "tenant_id", 65536); !errors.Is(err, tenancypostgres.ErrInvalidParameter) {
		t.Fatalf("Predicate(parameter above maximum) error = %v", err)
	}
}

func TestRLSPlanFailsClosedAndUsesTransactionLocalSetting(t *testing.T) {
	t.Parallel()

	plan, err := tenancypostgres.NewRLSPlan(tenancypostgres.RLSOptions{
		Table: "shipping.orders", Column: "tenant_id", Policy: "tenant_isolation",
	})
	if err != nil {
		t.Fatalf("NewRLSPlan() error = %v", err)
	}
	wantExpression := `"tenant_id" = NULLIF(current_setting('app.tenant_id', true), '')`
	if plan.Enable != `ALTER TABLE "shipping"."orders" ENABLE ROW LEVEL SECURITY` ||
		plan.Force != `ALTER TABLE "shipping"."orders" FORCE ROW LEVEL SECURITY` ||
		plan.Create != `CREATE POLICY "tenant_isolation" ON "shipping"."orders" USING (`+wantExpression+`) WITH CHECK (`+wantExpression+`)` ||
		plan.Drop != `DROP POLICY IF EXISTS "tenant_isolation" ON "shipping"."orders"` {
		t.Fatalf("RLS plan = %#v", plan)
	}
	if plan.Setting != tenancypostgres.DefaultSetting {
		t.Fatalf("Setting = %q", plan.Setting)
	}

	invalid := []tenancypostgres.RLSOptions{
		{},
		{Table: "orders; DROP TABLE users", Column: "tenant_id", Policy: "tenant_isolation"},
		{Table: "catalog.shipping.orders", Column: "tenant_id", Policy: "tenant_isolation"},
		{Table: "orders", Column: "tenant id", Policy: "tenant_isolation"},
		{Table: "orders", Column: "tenant_id", Policy: "bad policy"},
		{Table: "orders", Column: "tenant_id", Policy: "tenant_isolation", Setting: "bad'setting"},
		{Table: "orders", Column: "tenant_id", Policy: "tenant_isolation", Setting: "app.bad-setting"},
	}
	for _, options := range invalid {
		if _, err := tenancypostgres.NewRLSPlan(options); !errors.Is(err, tenancypostgres.ErrInvalidRLSOptions) {
			t.Fatalf("NewRLSPlan(%#v) error = %v", options, err)
		}
	}
}

func TestPostgreSQLIdentifierAndSettingBoundaries(t *testing.T) {
	t.Parallel()

	scope, _ := tenancy.NewTenantScope(tenancy.MustTenantID("tenant-a"), tenancy.Metadata{})
	validIdentifiers := []string{
		strings.Repeat("a", 63),
		"_tenant",
		"a0z9_AZ",
		"ztenant",
		"Atenant",
		"Ztenant",
	}
	for _, identifier := range validIdentifiers {
		if _, err := tenancypostgres.Predicate(scope, identifier, 1); err != nil {
			t.Fatalf("Predicate(%q) error = %v", identifier, err)
		}
	}
	invalidIdentifiers := []string{
		strings.Repeat("a", 64),
		"0tenant",
		"@tenant",
		"[tenant",
		"`tenant",
		"{tenant",
		"tenant/",
		"tenant:",
	}
	for _, identifier := range invalidIdentifiers {
		if _, err := tenancypostgres.Predicate(scope, identifier, 1); !errors.Is(err, tenancypostgres.ErrInvalidIdentifier) {
			t.Fatalf("Predicate(%q) error = %v", identifier, err)
		}
	}

	validSetting := "a." + strings.Repeat("b", 62)
	if _, err := tenancypostgres.NewManager(tenancypostgres.Config{Setting: validSetting}); err != nil {
		t.Fatalf("NewManager(64-byte setting) error = %v", err)
	}
	invalidSetting := "a." + strings.Repeat("b", 63)
	if _, err := tenancypostgres.NewManager(tenancypostgres.Config{Setting: invalidSetting}); !errors.Is(err, tenancypostgres.ErrInvalidConfig) {
		t.Fatalf("NewManager(65-byte setting) error = %v", err)
	}
}
