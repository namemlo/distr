ALTER TABLE DeploymentProcess
  ADD CONSTRAINT deploymentprocess_id_application_organization_unique UNIQUE (id, application_id, organization_id);

ALTER TABLE DeploymentProcessRevision
  ADD CONSTRAINT deploymentprocessrevision_id_process_organization_unique UNIQUE (id, deployment_process_id, organization_id);

CREATE TABLE ProcessSnapshot (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  application_id UUID NOT NULL,
  deployment_process_id UUID NOT NULL,
  deployment_process_revision_id UUID NOT NULL,
  revision_number INTEGER NOT NULL CHECK (revision_number > 0),
  canonical_checksum TEXT NOT NULL,
  canonical_payload BYTEA NOT NULL,
  CONSTRAINT processsnapshot_revision_unique UNIQUE (deployment_process_revision_id),
  CONSTRAINT processsnapshot_id_application_organization_unique UNIQUE (id, application_id, organization_id),
  CONSTRAINT processsnapshot_process_application_organization_fk
    FOREIGN KEY (deployment_process_id, application_id, organization_id)
    REFERENCES DeploymentProcess(id, application_id, organization_id)
    ON UPDATE RESTRICT
    ON DELETE RESTRICT,
  CONSTRAINT processsnapshot_revision_process_organization_fk
    FOREIGN KEY (deployment_process_revision_id, deployment_process_id, organization_id)
    REFERENCES DeploymentProcessRevision(id, deployment_process_id, organization_id)
    ON UPDATE RESTRICT
    ON DELETE RESTRICT
);

CREATE INDEX ProcessSnapshot_organization_application_created
  ON ProcessSnapshot (organization_id, application_id, created_at, id);

ALTER TABLE ReleaseBundle
  ADD COLUMN process_snapshot_id UUID,
  ADD CONSTRAINT releasebundle_process_snapshot_application_organization_fk
    FOREIGN KEY (process_snapshot_id, application_id, organization_id)
    REFERENCES ProcessSnapshot(id, application_id, organization_id)
    ON UPDATE RESTRICT
    ON DELETE RESTRICT;

CREATE INDEX ReleaseBundle_process_snapshot_idx
  ON ReleaseBundle (process_snapshot_id);
