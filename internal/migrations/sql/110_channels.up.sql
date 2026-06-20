CREATE TABLE Channel (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  updated_at TIMESTAMP NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  application_id UUID NOT NULL REFERENCES Application(id) ON DELETE CASCADE,
  lifecycle_id UUID NOT NULL REFERENCES Lifecycle(id) ON DELETE RESTRICT,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  sort_order INTEGER NOT NULL DEFAULT 0 CHECK (sort_order >= 0),
  is_default BOOLEAN NOT NULL DEFAULT false,
  CONSTRAINT channel_organization_application_name_unique UNIQUE (organization_id, application_id, name)
);

CREATE UNIQUE INDEX Channel_organization_application_default_unique
  ON Channel (organization_id, application_id)
  WHERE is_default;

CREATE INDEX Channel_organization_application_sort_name
  ON Channel (organization_id, application_id, sort_order, name);

CREATE INDEX Channel_lifecycle
  ON Channel (lifecycle_id);
