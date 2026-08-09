package tenancypostgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/faustbrian/golib/pkg/tenancy"
)

const (
	defaultCleanupTimeout = 2 * time.Second
	maximumCleanupTimeout = 30 * time.Second
	resetSessionSQL       = "SELECT set_config($1, '', false)"
	setTransactionSQL     = "SELECT set_config($1, $2, true)"
	readSettingSQL        = "SELECT current_setting($1, true)"
)

var (
	// ErrInvalidConfig reports unsafe setting names or cleanup bounds.
	ErrInvalidConfig = errors.New("tenancy postgres: invalid config")
	// ErrInvalidOperation reports missing manager, context, connector, or callback.
	ErrInvalidOperation = errors.New("tenancy postgres: invalid operation")
	// ErrScopeVerification reports a setting readback mismatch or failure.
	ErrScopeVerification = errors.New("tenancy postgres: scope verification failed")
	// ErrSessionReset reports failure to clear a leased session before pool reuse.
	ErrSessionReset = errors.New("tenancy postgres: session reset failed")
)

// Connector is the database/sql connection-leasing seam implemented by *sql.DB.
type Connector interface {
	Conn(context.Context) (*sql.Conn, error)
}

// Config controls the custom setting, cleanup lifetime, and transaction mode.
type Config struct {
	Setting        string
	CleanupTimeout time.Duration
	TxOptions      sql.TxOptions
}

// Manager owns transaction-local setting and leased-session cleanup policy. It
// is immutable and safe for concurrent use.
type Manager struct {
	setting        string
	cleanupTimeout time.Duration
	txOptions      sql.TxOptions
}

// NewManager validates and copies PostgreSQL isolation policy.
func NewManager(config Config) (*Manager, error) {
	setting := config.Setting
	if setting == "" {
		setting = DefaultSetting
	}
	cleanupTimeout := config.CleanupTimeout
	if cleanupTimeout == 0 {
		cleanupTimeout = defaultCleanupTimeout
	}
	if !validSetting(setting) || cleanupTimeout < time.Millisecond ||
		cleanupTimeout > maximumCleanupTimeout {
		return nil, ErrInvalidConfig
	}
	return &Manager{
		setting: setting, cleanupTimeout: cleanupTimeout, txOptions: config.TxOptions,
	}, nil
}

// WithTenant leases one connection, clears stale session state, installs and
// verifies a transaction-local tenant ID, invokes operation, and resets the
// same leased session before pool reuse.
func (manager *Manager) WithTenant(
	ctx context.Context,
	database Connector,
	scope tenancy.Scope,
	operation func(context.Context, *sql.Tx) error,
) error {
	if !scope.Valid() || scope.Kind() != tenancy.ScopeTenant {
		return tenancy.ErrTenantScopeRequired
	}
	if !manager.validOperation(ctx, database, operation) {
		return ErrInvalidOperation
	}
	return manager.execute(ctx, database, scope, scope.TenantID().Value(), operation)
}

// WithSystem requires explicit system scope and verifies an empty tenant
// setting. It records intent but does not grant PostgreSQL role privileges or
// application authorization.
func (manager *Manager) WithSystem(
	ctx context.Context,
	database Connector,
	scope tenancy.Scope,
	operation func(context.Context, *sql.Tx) error,
) error {
	if !scope.Valid() || scope.Kind() != tenancy.ScopeSystem {
		return tenancy.ErrSystemScopeRequired
	}
	if !manager.validOperation(ctx, database, operation) {
		return ErrInvalidOperation
	}
	return manager.execute(ctx, database, scope, "", operation)
}

func (manager *Manager) execute(
	ctx context.Context,
	database Connector,
	scope tenancy.Scope,
	expected string,
	operation func(context.Context, *sql.Tx) error,
) (resultErr error) {
	scoped, err := tenancy.WithScope(ctx, scope)
	if err != nil {
		return err
	}
	connection, err := database.Conn(ctx)
	if err != nil {
		return err
	}
	if connection == nil {
		return ErrInvalidOperation
	}
	if _, err := connection.ExecContext(ctx, resetSessionSQL, manager.setting); err != nil {
		discardConnection(connection)
		return errors.Join(fmt.Errorf("%w: %w", ErrSessionReset, err), connection.Close())
	}
	defer func() {
		resultErr = errors.Join(resultErr, manager.resetAndClose(ctx, connection))
	}()

	tx, err := connection.BeginTx(ctx, &manager.txOptions)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, setTransactionSQL, manager.setting, expected); err != nil {
		return err
	}
	var observed sql.NullString
	if err := tx.QueryRowContext(ctx, readSettingSQL, manager.setting).Scan(&observed); err != nil {
		return fmt.Errorf("%w: %w", ErrScopeVerification, err)
	}
	if !observed.Valid || observed.String != expected {
		return ErrScopeVerification
	}
	if err := operation(scoped, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (manager *Manager) resetAndClose(ctx context.Context, connection *sql.Conn) error {
	cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), manager.cleanupTimeout)
	defer cancel()
	_, resetErr := connection.ExecContext(cleanupContext, resetSessionSQL, manager.setting)
	if resetErr != nil {
		discardConnection(connection)
	}
	closeErr := connection.Close()
	if resetErr != nil {
		return errors.Join(fmt.Errorf("%w: %w", ErrSessionReset, resetErr), closeErr)
	}
	return closeErr
}

func (manager *Manager) validOperation(
	ctx context.Context,
	database Connector,
	operation func(context.Context, *sql.Tx) error,
) bool {
	return manager != nil && manager.setting != "" && ctx != nil &&
		!nilConnector(database) && operation != nil
}

func nilConnector(database Connector) bool {
	if database == nil {
		return true
	}
	value := reflect.ValueOf(database)
	return value.Kind() == reflect.Pointer && value.IsNil()
}

func discardConnection(connection *sql.Conn) {
	_ = connection.Raw(func(any) error { return driver.ErrBadConn })
}
