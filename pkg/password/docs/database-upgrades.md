# Database upgrades and concurrency

`password` does not own an ORM or repository. Persist upgrades with an
optimistic comparison against the exact encoded value that was verified.

## PostgreSQL

```sql
UPDATE users
SET password_hash = $1,
    password_hash_updated_at = CURRENT_TIMESTAMP
WHERE id = $2
  AND password_hash = $3;
```

Parameters are replacement, stable user ID, and expected old encoding. Exactly
one affected row means success. Zero rows means a concurrent writer won; it is
not permission to retry with an unconditional update.

## Go pattern

```go
upgrade := result.Upgrade()
if upgrade.Required() {
	tag, err := pool.Exec(ctx, `
		UPDATE users
		SET password_hash = $1
		WHERE id = $2 AND password_hash = $3`,
		upgrade.Replacement().String(),
		result.Subject(),
		upgrade.Expected().String(),
	)
	if err != nil {
		return err
	}
	_ = tag.RowsAffected() // 0 is a benign concurrent update
}
```

Do not place password bytes, expected hashes, or replacement hashes in query
logs, tracing attributes, metrics, or error text. Parameterize all values.

## Transaction choices

A single conditional `UPDATE` is atomic and normally sufficient. A larger
transaction may include audit metadata, but never hold a database transaction
open while running Argon2id/bcrypt. Verify and hash outside the transaction,
then perform the short CAS. The conditional predicate prevents stale overwrite.
