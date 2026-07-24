CREATE TABLE authorization_policy_manifests (
    singleton smallint PRIMARY KEY CHECK (singleton = 1),
    revision bigint NOT NULL CHECK (revision > 0),
    manifest jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
