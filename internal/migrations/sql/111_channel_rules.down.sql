ALTER TABLE Channel
  DROP COLUMN allowed_version_ranges,
  DROP COLUMN allowed_prerelease_patterns,
  DROP COLUMN allowed_source_branches,
  DROP COLUMN allowed_source_tags;
