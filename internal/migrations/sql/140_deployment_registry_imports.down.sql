LOCK TABLE
  RegistryImport,
  RegistryImportRoot,
  RegistryImportPlacement,
  RegistryImportDecision
IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM RegistryImport) THEN
    RAISE EXCEPTION
      'downgrade crossing 140 is forbidden while registry import evidence exists';
  END IF;
END;
$$;

DROP TRIGGER RegistryImportDecision_append_only ON RegistryImportDecision;
DROP FUNCTION registry_import_decision_append_only();
DROP TABLE RegistryImportDecision;
DROP TABLE RegistryImportPlacement;
DROP TRIGGER RegistryImportRoot_validate_org_references ON RegistryImportRoot;
DROP TABLE RegistryImportRoot;
DROP FUNCTION registry_import_root_validate_org_references();
DROP TABLE RegistryImport;
