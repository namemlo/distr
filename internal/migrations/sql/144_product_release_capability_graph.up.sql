ALTER TABLE ReleaseBundle
  ADD CONSTRAINT releasebundle_product_version_length_check
  CHECK (
    kind <> 'product'
    OR octet_length(release_number) BETWEEN 1 AND 128
  );

CREATE TABLE ProductReleaseComponent (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  product_release_bundle_id UUID NOT NULL,
  organization_id UUID NOT NULL,
  component_release_bundle_id UUID NOT NULL,
  component_release_checksum TEXT NOT NULL,
  component_key TEXT NOT NULL,
  component_version TEXT NOT NULL,
  contract_snapshot JSONB NOT NULL,
  CONSTRAINT productreleasecomponent_product_fk
    FOREIGN KEY (product_release_bundle_id, organization_id)
    REFERENCES ReleaseBundle(id, organization_id)
    ON DELETE CASCADE,
  CONSTRAINT productreleasecomponent_component_fk
    FOREIGN KEY (component_release_bundle_id, organization_id)
    REFERENCES ReleaseBundle(id, organization_id)
    ON DELETE RESTRICT,
  CONSTRAINT productreleasecomponent_product_key_unique
    UNIQUE (product_release_bundle_id, component_key),
  CONSTRAINT productreleasecomponent_product_release_unique
    UNIQUE (product_release_bundle_id, component_release_bundle_id),
  CONSTRAINT productreleasecomponent_id_product_organization_unique
    UNIQUE (id, product_release_bundle_id, organization_id),
  CONSTRAINT productreleasecomponent_checksum_check
    CHECK (component_release_checksum ~ '^sha256:[0-9a-f]{64}$'),
  CONSTRAINT productreleasecomponent_key_check
    CHECK (component_key ~ '^[a-z0-9][a-z0-9._-]{0,127}$'),
  CONSTRAINT productreleasecomponent_version_length_check
    CHECK (octet_length(component_version) BETWEEN 1 AND 128),
  CONSTRAINT productreleasecomponent_contract_object_check
    CHECK (jsonb_typeof(contract_snapshot) = 'object'),
  CONSTRAINT productreleasecomponent_contract_schema_check
    CHECK (contract_snapshot ->> 'schema' = 'distr.component-release/v2')
);

CREATE INDEX productreleasecomponent_product_idx
  ON ProductReleaseComponent (organization_id, product_release_bundle_id, component_key);

CREATE INDEX productreleasecomponent_component_idx
  ON ProductReleaseComponent (organization_id, component_release_bundle_id);

CREATE TABLE ProductReleaseCapabilityEdge (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  product_release_bundle_id UUID NOT NULL,
  organization_id UUID NOT NULL,
  edge_key TEXT NOT NULL,
  from_node_key TEXT NOT NULL,
  to_node_key TEXT NOT NULL,
  consumer_component_key TEXT NOT NULL,
  provider_component_key TEXT,
  capability_name TEXT NOT NULL,
  version_range TEXT NOT NULL,
  provider_version TEXT NOT NULL DEFAULT '',
  resolution_stage TEXT NOT NULL,
  allowed_modes TEXT[] NOT NULL DEFAULT '{}',
  ordering TEXT NOT NULL DEFAULT '',
  CONSTRAINT productreleasecapabilityedge_product_fk
    FOREIGN KEY (product_release_bundle_id, organization_id)
    REFERENCES ReleaseBundle(id, organization_id)
    ON DELETE CASCADE,
  CONSTRAINT productreleasecapabilityedge_product_edge_unique
    UNIQUE (product_release_bundle_id, edge_key),
  CONSTRAINT productreleasecapabilityedge_indexed_values_check
    CHECK (
      octet_length(edge_key) BETWEEN 1 AND 512
      AND octet_length(from_node_key) BETWEEN 1 AND 512
      AND octet_length(to_node_key) BETWEEN 1 AND 512
      AND octet_length(consumer_component_key) BETWEEN 1 AND 128
      AND (
        provider_component_key IS NULL
        OR octet_length(provider_component_key) BETWEEN 1 AND 128
      )
      AND octet_length(capability_name) BETWEEN 1 AND 128
      AND octet_length(version_range) BETWEEN 1 AND 256
      AND octet_length(provider_version) <= 128
      AND octet_length(ordering) <= 64
    ),
  CONSTRAINT productreleasecapabilityedge_stage_check
    CHECK (resolution_stage IN ('product', 'target')),
  CONSTRAINT productreleasecapabilityedge_capability_check
    CHECK (capability_name ~ '^[a-z0-9][a-z0-9._-]{0,127}$'),
  CONSTRAINT productreleasecapabilityedge_modes_check
    CHECK (
      allowed_modes <@ ARRAY[
        'included',
        'pinned_existing',
        'shared_provider',
        'approved_external',
        'feature_disabled'
      ]::TEXT[]
    ),
  CONSTRAINT productreleasecapabilityedge_resolution_check
    CHECK (
      (
        resolution_stage = 'product'
        AND provider_component_key IS NOT NULL
        AND provider_version <> ''
        AND cardinality(allowed_modes) = 0
        AND ordering = 'provider_deploy_and_health_before_consumer'
      )
      OR (
        resolution_stage = 'target'
        AND provider_component_key IS NULL
        AND provider_version = ''
        AND cardinality(allowed_modes) > 0
        AND ordering = ''
      )
    )
);

CREATE INDEX productreleasecapabilityedge_product_idx
  ON ProductReleaseCapabilityEdge (
    organization_id,
    product_release_bundle_id,
    from_node_key,
    to_node_key
  );

CREATE INDEX productreleasecapabilityedge_capability_idx
  ON ProductReleaseCapabilityEdge (
    organization_id,
    capability_name,
    resolution_stage
  );
