DROP TABLE IF EXISTS ReleaseBundleIdempotencyKey;

CREATE OR REPLACE FUNCTION pg_temp.release_bundle_go_json_string(value TEXT)
RETURNS TEXT
LANGUAGE SQL
IMMUTABLE
AS $$
    SELECT replace(
        replace(
            replace(
                replace(
                    replace(to_json(value)::text, '<', '\u003c'),
                    '>',
                    '\u003e'
                ),
                '&',
                '\u0026'
            ),
            U&'\2028',
            '\u2028'
        ),
        U&'\2029',
        '\u2029'
    )
$$;

WITH canonical_components AS (
    SELECT
        release_bundle_id,
        '[' || string_agg(component_payload, ',' ORDER BY component_key, component_id) || ']' AS components_payload
    FROM (
        SELECT
            rbc.release_bundle_id,
            rbc.key AS component_key,
            rbc.id AS component_id,
            '{' ||
            '"key":' || pg_temp.release_bundle_go_json_string(rbc.key) ||
            ',"name":' || pg_temp.release_bundle_go_json_string(rbc.name) ||
            ',"type":' || pg_temp.release_bundle_go_json_string(rbc.component_type) ||
            ',"version":' || pg_temp.release_bundle_go_json_string(rbc.version) ||
            CASE
                WHEN rbc.application_version_id IS NOT NULL
                THEN ',"applicationVersionId":' || pg_temp.release_bundle_go_json_string(rbc.application_version_id::text)
                ELSE ''
            END ||
            CASE
                WHEN rbc.package_ref <> ''
                THEN ',"packageRef":' || pg_temp.release_bundle_go_json_string(rbc.package_ref)
                ELSE ''
            END ||
            CASE
                WHEN rbc.digest <> ''
                THEN ',"digest":' || pg_temp.release_bundle_go_json_string(rbc.digest)
                ELSE ''
            END ||
            CASE
                WHEN rbc.checksum <> ''
                THEN ',"checksum":' || pg_temp.release_bundle_go_json_string(rbc.checksum)
                ELSE ''
            END ||
            CASE
                WHEN rbc.child_release_bundle_id IS NOT NULL
                THEN ',"childReleaseBundleId":' || pg_temp.release_bundle_go_json_string(rbc.child_release_bundle_id::text)
                ELSE ''
            END ||
            '}' AS component_payload
        FROM ReleaseBundleComponent rbc
    ) components
    GROUP BY release_bundle_id
),
repaired AS (
    SELECT
        rb.id,
        convert_to(
            '{' ||
            '"applicationId":' || pg_temp.release_bundle_go_json_string(rb.application_id::text) ||
            ',"channelId":' || pg_temp.release_bundle_go_json_string(rb.channel_id::text) ||
            ',"releaseNumber":' || pg_temp.release_bundle_go_json_string(rb.release_number) ||
            ',"releaseNotes":' || pg_temp.release_bundle_go_json_string(rb.release_notes) ||
            ',"sourceRevision":' || pg_temp.release_bundle_go_json_string(rb.source_revision) ||
            ',"components":' || COALESCE(cc.components_payload, '[]') ||
            '}',
            'UTF8'
        ) AS canonical_payload
    FROM ReleaseBundle rb
    LEFT JOIN canonical_components cc ON cc.release_bundle_id = rb.id
)
UPDATE ReleaseBundle rb
SET
    canonical_payload = repaired.canonical_payload,
    canonical_checksum = 'sha256:' || encode(sha256(repaired.canonical_payload), 'hex')
FROM repaired
WHERE rb.id = repaired.id;

ALTER TABLE ReleaseBundle
    DROP COLUMN IF EXISTS source_repository,
    DROP COLUMN IF EXISTS source_branch,
    DROP COLUMN IF EXISTS source_tag,
    DROP COLUMN IF EXISTS ci_provider,
    DROP COLUMN IF EXISTS ci_run_id,
    DROP COLUMN IF EXISTS ci_run_url;
