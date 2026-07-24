# Compatibility policy

After v1, semantic versioning applies to exported Go APIs and documented query
behavior. The `api/v1.txt` export baseline blocks incompatible Go API changes.

The following are public contracts and require migration planning: capability
names, types, operators, default fields, required execution fields, relationship
paths, default or tie-breaker sorts, null placement, page defaults and limits,
costs, schema revisions, cursor versions/policies, canonical plan bytes, error
codes and paths, transport names, and page envelope JSON.

Adding an optional capability can be backward compatible, but changing defaults
or cost rejection can alter existing requests. Removing or renaming anything,
tightening a bound below observed use, changing canonical output, or changing a
sort requires a schema revision. Cursor-incompatible changes also require a new
cursor protocol version or rejection of old tokens.

Deprecated declarations fail compilation rather than silently changing
meaning. Keep the old schema revision available for an announced window or
return `version_mismatch` and migration guidance at the application layer.
