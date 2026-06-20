ALTER TABLE Channel
  ADD COLUMN allowed_version_ranges TEXT[] NOT NULL DEFAULT '{}',
  ADD COLUMN allowed_prerelease_patterns TEXT[] NOT NULL DEFAULT '{}',
  ADD COLUMN allowed_source_branches TEXT[] NOT NULL DEFAULT '{}',
  ADD COLUMN allowed_source_tags TEXT[] NOT NULL DEFAULT '{}';
