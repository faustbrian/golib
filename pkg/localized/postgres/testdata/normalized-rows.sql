-- Example extraction only; localized does not own schema migrations.
SELECT entity_id, locale, text
FROM localized_text_rows
ORDER BY entity_id, locale;
