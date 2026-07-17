package migrations

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/distr-sh/distr/internal/types"
	"github.com/jackc/pgx/v5"
)

const (
	timestampDirtyRecoveryPredecessorCatalogChecksum = "sha256:" +
		"ac8a968863ebc7cc2b62e0484aa95ba43cb9e10d405d171b8843bdb9843bafd2"
	timestampDirtyRecoveryExpandCatalogChecksum = "sha256:" +
		"19124d220dc26cc364de69a8f61e522832b2d51ed8fd9e9a3eb9974f9a1e59ef"
	timestampDirtyRecoveryCatalogIdentityLimit = 16
)

type timestampDirtyRecoveryCatalog struct {
	Shape    types.TimestampRecoveryCatalogShape
	Checksum string
	Records  []types.TimestampRecoveryCatalogRecord
}

const timestampDirtyRecoveryCatalogSQL = `
WITH owned_relation AS (
  SELECT relation.oid, relation.relname
  FROM pg_catalog.pg_class relation
  JOIN pg_catalog.pg_namespace namespace_row
    ON namespace_row.oid = relation.relnamespace
  WHERE namespace_row.nspname = current_schema()
    AND relation.relname LIKE 'externalexecutiontimestamp%'
    AND relation.relkind NOT IN ('i', 'I', 't')
),
source_relation AS (
  SELECT relation.oid, relation.relname
  FROM pg_catalog.pg_class relation
  JOIN pg_catalog.pg_namespace namespace_row
    ON namespace_row.oid = relation.relnamespace
  WHERE namespace_row.nspname = current_schema()
    AND relation.relkind = 'r'
    AND relation.relname IN ('externalexecution', 'externalexecutionevent')
),
catalog_record AS (
  SELECT
    'RELATION'::text AS category,
    ''::text AS relation_name,
    relation.relname AS object_name,
    concat_ws(
      '|',
      'kind=' || relation.relkind::text,
      'persistence=' || relation.relpersistence::text,
      'rowsecurity=' || relation.relrowsecurity::text,
      'forcerowsecurity=' || relation.relforcerowsecurity::text,
      'ispartition=' || relation.relispartition::text,
      'parent_count=' || (
        SELECT count(*)
        FROM pg_catalog.pg_inherits inheritance_row
        WHERE inheritance_row.inhrelid = relation.oid
      )::text,
      'child_count=' || (
        SELECT count(*)
        FROM pg_catalog.pg_inherits inheritance_row
        WHERE inheritance_row.inhparent = relation.oid
      )::text,
      'rule_count=' || (
        SELECT count(*)
        FROM pg_catalog.pg_rewrite rule_row
        WHERE rule_row.ev_class = relation.oid
      )::text
    ) AS definition
  FROM pg_catalog.pg_class relation
  JOIN pg_catalog.pg_namespace namespace_row
    ON namespace_row.oid = relation.relnamespace
  WHERE namespace_row.nspname = current_schema()
    AND (
      relation.relname LIKE 'externalexecutiontimestamp%'
      OR relation.oid IN (SELECT oid FROM source_relation)
    )
    AND relation.relkind NOT IN ('i', 'I', 't')

  UNION ALL

  SELECT
    'COLUMN',
    relation.relname,
    attribute_row.attname,
    concat_ws(
      '|',
      'type=' || pg_catalog.format_type(
        attribute_row.atttypid,
        attribute_row.atttypmod
      ),
      'notnull=' || attribute_row.attnotnull::text,
      'identity=' || attribute_row.attidentity::text,
      'generated=' || attribute_row.attgenerated::text,
      'default=' || COALESCE(
        pg_get_expr(
          attribute_default.adbin,
          attribute_default.adrelid,
          true
        ),
        '<none>'
      )
    )
  FROM pg_catalog.pg_attribute attribute_row
  JOIN pg_catalog.pg_class relation
    ON relation.oid = attribute_row.attrelid
  JOIN pg_catalog.pg_namespace namespace_row
    ON namespace_row.oid = relation.relnamespace
  LEFT JOIN pg_catalog.pg_attrdef attribute_default
    ON attribute_default.adrelid = attribute_row.attrelid
   AND attribute_default.adnum = attribute_row.attnum
  WHERE namespace_row.nspname = current_schema()
    AND attribute_row.attnum > 0
    AND NOT attribute_row.attisdropped
    AND (
      relation.oid IN (SELECT oid FROM owned_relation)
      OR (
        relation.relname = 'externalexecution'
        AND (
          attribute_row.attname IN (
            'created_at',
            'updated_at',
            'started_at',
            'completed_at',
            'callback_deadline_at'
          )
          OR attribute_row.attname LIKE '%_instant%'
        )
      )
      OR (
        relation.relname = 'externalexecutionevent'
        AND (
          attribute_row.attname = 'created_at'
          OR attribute_row.attname LIKE '%_instant%'
        )
      )
    )

  UNION ALL

  SELECT
    'CONSTRAINT',
    relation.relname,
    constraint_row.conname,
    concat_ws(
      '|',
      'type=' || constraint_row.contype::text,
      'validated=' || constraint_row.convalidated::text,
      'deferrable=' || constraint_row.condeferrable::text,
      'deferred=' || constraint_row.condeferred::text,
      'definition=' ||
        regexp_replace(
          pg_get_constraintdef(constraint_row.oid, true),
          '\s+',
          ' ',
          'g'
        )
    )
  FROM pg_catalog.pg_constraint constraint_row
  JOIN pg_catalog.pg_class relation
    ON relation.oid = constraint_row.conrelid
  WHERE constraint_row.contype <> 'n'
    AND (
      relation.oid IN (SELECT oid FROM owned_relation)
      OR relation.oid IN (SELECT oid FROM source_relation)
    )

  UNION ALL

  SELECT
    'INDEX',
    source.relname,
    index_relation.relname,
    concat_ws(
      '|',
      'method=' || access_method.amname,
      'unique=' || index_row.indisunique::text,
      'nullsnotdistinct=' || index_row.indnullsnotdistinct::text,
      'primary=' || index_row.indisprimary::text,
      'valid=' || index_row.indisvalid::text,
      'ready=' || index_row.indisready::text,
      'key_count=' || index_row.indnkeyatts::text,
      'attribute_count=' || index_row.indnatts::text,
      'keys=' || COALESCE(index_keys.keys, ''),
      'includes=' || COALESCE(index_includes.includes, ''),
      'predicate=' || COALESCE(
        regexp_replace(
          pg_get_expr(index_row.indpred, index_row.indrelid, true),
          '\s+',
          ' ',
          'g'
        ),
        '<none>'
      )
    )
  FROM pg_catalog.pg_index index_row
  JOIN pg_catalog.pg_class source
    ON source.oid = index_row.indrelid
  JOIN pg_catalog.pg_class index_relation
    ON index_relation.oid = index_row.indexrelid
  JOIN pg_catalog.pg_am access_method
    ON access_method.oid = index_relation.relam
  LEFT JOIN LATERAL (
    SELECT string_agg(
      regexp_replace(
        pg_get_indexdef(index_row.indexrelid, key_number, true),
        '\s+',
        ' ',
        'g'
      ),
      ',' ORDER BY key_number
    ) AS keys
    FROM generate_series(1, index_row.indnkeyatts) key_number
  ) index_keys ON true
  LEFT JOIN LATERAL (
    SELECT string_agg(
      regexp_replace(
        pg_get_indexdef(index_row.indexrelid, attribute_number, true),
        '\s+',
        ' ',
        'g'
      ),
      ',' ORDER BY attribute_number
    ) AS includes
    FROM generate_series(
      index_row.indnkeyatts + 1,
      index_row.indnatts
    ) attribute_number
  ) index_includes ON true
  WHERE source.oid IN (SELECT oid FROM owned_relation)
     OR source.oid IN (SELECT oid FROM source_relation)

  UNION ALL

  SELECT
    'TRIGGER',
    relation.relname,
    trigger_row.tgname,
    concat_ws(
      '|',
      'type=' || trigger_row.tgtype::text,
      'enabled=' || trigger_row.tgenabled::text,
      'function_namespace=' ||
        CASE
          WHEN function_namespace.nspname = current_schema()
            THEN '<current_schema>'
          ELSE function_namespace.nspname
        END,
      'function=' || function_row.proname,
      'function_arguments=' ||
        pg_get_function_identity_arguments(function_row.oid),
      'columns=' || COALESCE(trigger_columns.columns, ''),
      'when=' || COALESCE(
        regexp_replace(
          pg_get_expr(trigger_row.tgqual, trigger_row.tgrelid, true),
          '\s+',
          ' ',
          'g'
        ),
        '<none>'
      ),
      'arguments=' || encode(trigger_row.tgargs, 'hex')
    )
  FROM pg_catalog.pg_trigger trigger_row
  JOIN pg_catalog.pg_class relation
    ON relation.oid = trigger_row.tgrelid
  JOIN pg_catalog.pg_proc function_row
    ON function_row.oid = trigger_row.tgfoid
  JOIN pg_catalog.pg_namespace function_namespace
    ON function_namespace.oid = function_row.pronamespace
  LEFT JOIN LATERAL (
    SELECT string_agg(
      attribute_row.attname,
      ',' ORDER BY trigger_column.ordinality
    ) AS columns
    FROM unnest(trigger_row.tgattr::smallint[])
         WITH ORDINALITY trigger_column(attribute_number, ordinality)
    JOIN pg_catalog.pg_attribute attribute_row
      ON attribute_row.attrelid = trigger_row.tgrelid
     AND attribute_row.attnum = trigger_column.attribute_number
  ) trigger_columns ON true
  WHERE NOT trigger_row.tgisinternal
    AND (
      relation.oid IN (SELECT oid FROM owned_relation)
      OR relation.oid IN (SELECT oid FROM source_relation)
    )

  UNION ALL

  SELECT
    'FUNCTION',
    '',
    function_row.proname,
    concat_ws(
      '|',
      'arguments=' || pg_get_function_identity_arguments(function_row.oid),
      'result=' || pg_get_function_result(function_row.oid),
      'language=' || language_row.lanname,
      'volatile=' || function_row.provolatile::text,
      'strict=' || function_row.proisstrict::text,
      'securitydefiner=' || function_row.prosecdef::text,
      'config=' || COALESCE(array_to_string(function_row.proconfig, ','), ''),
      'body=' || regexp_replace(btrim(function_row.prosrc), '\s+', ' ', 'g')
    )
  FROM pg_catalog.pg_proc function_row
  JOIN pg_catalog.pg_namespace namespace_row
    ON namespace_row.oid = function_row.pronamespace
  JOIN pg_catalog.pg_language language_row
    ON language_row.oid = function_row.prolang
  WHERE namespace_row.nspname = current_schema()
    AND (
      function_row.proname LIKE 'external_execution_timestamp_%'
      OR function_row.proname = 'external_execution_lifecycle_pair_one_shot'
    )
)
SELECT category, relation_name, object_name, definition
FROM catalog_record
ORDER BY category, relation_name, object_name, definition`

const timestampDirtyRecoveryNotNullConstraintValidationSQL = `
WITH owned_relation AS (
  SELECT relation.oid, relation.relname
  FROM pg_catalog.pg_class relation
  JOIN pg_catalog.pg_namespace namespace_row
    ON namespace_row.oid = relation.relnamespace
  WHERE namespace_row.nspname = current_schema()
    AND relation.relname LIKE 'externalexecutiontimestamp%'
    AND relation.relkind NOT IN ('i', 'I', 't')
),
source_relation AS (
  SELECT relation.oid, relation.relname
  FROM pg_catalog.pg_class relation
  JOIN pg_catalog.pg_namespace namespace_row
    ON namespace_row.oid = relation.relnamespace
  WHERE namespace_row.nspname = current_schema()
    AND relation.relkind = 'r'
    AND relation.relname IN ('externalexecution', 'externalexecutionevent')
),
catalog_relation AS (
  SELECT oid, relname FROM owned_relation
  UNION
  SELECT oid, relname FROM source_relation
)
SELECT relation.relname
FROM catalog_relation relation
LEFT JOIN LATERAL (
  SELECT count(*) AS attribute_count
  FROM pg_catalog.pg_attribute attribute_row
  WHERE attribute_row.attrelid = relation.oid
    AND attribute_row.attnum > 0
    AND NOT attribute_row.attisdropped
    AND attribute_row.attnotnull
) not_null_attribute ON true
LEFT JOIN LATERAL (
  SELECT
    count(*) AS constraint_count,
    count(*) FILTER (WHERE
      constraint_row.convalidated
      AND COALESCE(
        (to_jsonb(constraint_row)->>'conenforced')::boolean,
        TRUE
      )
      AND NOT constraint_row.condeferrable
      AND NOT constraint_row.condeferred
      AND constraint_row.conislocal
      AND NOT constraint_row.connoinherit
      AND constraint_row.coninhcount = 0
      AND constraint_row.conparentid = 0
      AND cardinality(constraint_row.conkey) = 1
      AND constrained_attribute.attnum > 0
      AND NOT constrained_attribute.attisdropped
      AND constrained_attribute.attnotnull
    ) AS valid_constraint_count,
    count(DISTINCT constrained_attribute.attnum) AS constrained_attribute_count
  FROM pg_catalog.pg_constraint constraint_row
  LEFT JOIN pg_catalog.pg_attribute constrained_attribute
    ON constrained_attribute.attrelid = constraint_row.conrelid
   AND constrained_attribute.attnum = (constraint_row.conkey)[1]
  WHERE constraint_row.conrelid = relation.oid
    AND constraint_row.contype = 'n'
) not_null_constraint ON true
WHERE CASE
  WHEN current_setting('server_version_num')::integer < 180000
    THEN not_null_constraint.constraint_count <> 0
  ELSE NOT (
    not_null_constraint.constraint_count = not_null_attribute.attribute_count
    AND not_null_constraint.valid_constraint_count =
      not_null_constraint.constraint_count
    AND not_null_constraint.constrained_attribute_count =
      not_null_constraint.constraint_count
  )
END
ORDER BY relation.relname
LIMIT 17`

func classifyTimestampDirtyRecoveryCatalog(
	ctx context.Context,
	tx pgx.Tx,
) (timestampDirtyRecoveryCatalog, error) {
	if err := validateTimestampDirtyRecoveryNotNullConstraintCatalog(
		ctx,
		tx,
	); err != nil {
		return timestampDirtyRecoveryCatalog{}, err
	}
	rows, err := tx.Query(ctx, timestampDirtyRecoveryCatalogSQL)
	if err != nil {
		return timestampDirtyRecoveryCatalog{}, fmt.Errorf(
			"read timestamp dirty recovery catalog: %w",
			err,
		)
	}
	defer rows.Close()

	records := make([]types.TimestampRecoveryCatalogRecord, 0, 128)
	for rows.Next() {
		var category string
		var record types.TimestampRecoveryCatalogRecord
		if err := rows.Scan(
			&category,
			&record.RelationName,
			&record.ObjectName,
			&record.Definition,
		); err != nil {
			return timestampDirtyRecoveryCatalog{}, fmt.Errorf(
				"scan timestamp dirty recovery catalog: %w",
				err,
			)
		}
		record.Category = types.TimestampRecoveryCatalogCategory(category)
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return timestampDirtyRecoveryCatalog{}, fmt.Errorf(
			"iterate timestamp dirty recovery catalog: %w",
			err,
		)
	}
	checksum, err := ComputeTimestampRecoveryCatalogChecksum(records)
	if err != nil {
		return timestampDirtyRecoveryCatalog{}, err
	}
	catalog := timestampDirtyRecoveryCatalog{
		Checksum: checksum,
		Records:  records,
	}
	switch checksum {
	case timestampDirtyRecoveryPredecessorCatalogChecksum:
		catalog.Shape = types.TimestampRecoveryCatalogShapePredecessor137
	case timestampDirtyRecoveryExpandCatalogChecksum:
		catalog.Shape = types.TimestampRecoveryCatalogShapeExpand138
	default:
		identities := make([]string, 0, timestampDirtyRecoveryCatalogIdentityLimit+1)
		for index, record := range records {
			if index == timestampDirtyRecoveryCatalogIdentityLimit {
				identities = append(
					identities,
					fmt.Sprintf(
						"... %d additional object identities omitted",
						len(records)-timestampDirtyRecoveryCatalogIdentityLimit,
					),
				)
				break
			}
			identities = append(
				identities,
				fmt.Sprintf(
					"%s/%s/%s",
					record.Category,
					record.RelationName,
					record.ObjectName,
				),
			)
		}
		return timestampDirtyRecoveryCatalog{}, errors.New(
			"timestamp dirty recovery catalog is partial, mixed, extra, or mutated: " +
				checksum + "\n" + strings.Join(identities, "\n"),
		)
	}
	return catalog, nil
}

func validateTimestampDirtyRecoveryNotNullConstraintCatalog(
	ctx context.Context,
	tx pgx.Tx,
) error {
	rows, err := tx.Query(
		ctx,
		timestampDirtyRecoveryNotNullConstraintValidationSQL,
	)
	if err != nil {
		return fmt.Errorf(
			"validate timestamp dirty recovery NOT NULL constraints: %w",
			err,
		)
	}
	defer rows.Close()
	relations := make(
		[]string,
		0,
		timestampDirtyRecoveryCatalogIdentityLimit+1,
	)
	for rows.Next() {
		var relation string
		if err := rows.Scan(&relation); err != nil {
			return fmt.Errorf(
				"scan timestamp dirty recovery NOT NULL constraint validation: %w",
				err,
			)
		}
		relations = append(relations, relation)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf(
			"iterate timestamp dirty recovery NOT NULL constraint validation: %w",
			err,
		)
	}
	if len(relations) == 0 {
		return nil
	}
	if len(relations) > timestampDirtyRecoveryCatalogIdentityLimit {
		omitted := len(relations) - timestampDirtyRecoveryCatalogIdentityLimit
		relations = append(
			relations[:timestampDirtyRecoveryCatalogIdentityLimit],
			fmt.Sprintf("... %d additional relations omitted", omitted),
		)
	}
	return errors.New(
		"timestamp dirty recovery catalog is partial, mixed, extra, or mutated: " +
			"invalid NOT NULL constraint metadata for " +
			strings.Join(relations, ", "),
	)
}
