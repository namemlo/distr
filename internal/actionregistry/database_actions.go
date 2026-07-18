package actionregistry

import "github.com/distr-sh/distr/internal/types"

func databaseActions() []types.ActionDefinition {
	common := databaseCommonProperties()
	return []types.ActionDefinition{
		{
			Type: "database.backup.create", Name: "Create database backup",
			Description: "Creates a bounded backup for one frozen database resource before mutation.",
			InputSchema: objectSchema(mergeSchemaProperties(common, map[string]any{
				"destinationReference": boundedKeySchema(256),
				"credentialsSecretRef": secretReferenceSchema(),
			}), []any{
				"databaseResourceKey", "databaseLockKey", "destinationReference",
				"idempotencyKey", "timeoutSeconds",
			}),
			OutputSchema: backupOutputSchema(),
		},
		{
			Type: "database.backup.verify", Name: "Verify database backup",
			Description:  "Verifies backup identity, checksum, and bounded evidence before mutation can start.",
			InputSchema:  backupVerifyInputSchema(common),
			OutputSchema: backupOutputSchema(),
		},
		{
			Type: "database.migration.apply", Name: "Apply database migration",
			Description: "Applies one checksum-bound migration with a stable retry key and exclusive database lock.",
			InputSchema: objectSchema(mergeSchemaProperties(common, map[string]any{
				"migrationId":           boundedKeySchema(128),
				"migrationChecksum":     checksumSchema(),
				"expectedSourceVersion": boundedStringSchema(128),
				"resultingVersion":      boundedStringSchema(128),
				"artifactDigest": map[string]any{
					"type":      "string",
					"pattern":   `^\S+@sha256:[A-Fa-f0-9]{64}$`,
					"maxLength": 1024,
				},
			}), []any{
				"migrationId", "migrationChecksum", "databaseResourceKey",
				"databaseLockKey", "expectedSourceVersion", "resultingVersion",
				"artifactDigest", "idempotencyKey", "timeoutSeconds",
			}),
			OutputSchema: migrationOutputSchema(),
		},
		{
			Type: "database.migration.validate", Name: "Validate database migration",
			Description: "Runs bounded precondition or postcondition probes and records an exact schema observation.",
			InputSchema: objectSchema(mergeSchemaProperties(common, map[string]any{
				"migrationId":           boundedKeySchema(128),
				"migrationChecksum":     checksumSchema(),
				"expectedSchemaVersion": boundedStringSchema(128),
				"probes":                migrationProbeArraySchema(),
			}), []any{
				"migrationId", "migrationChecksum", "databaseResourceKey",
				"databaseLockKey", "expectedSchemaVersion", "probes", "timeoutSeconds",
			}),
			OutputSchema: migrationOutputSchema(),
		},
		{
			Type: "database.migration.reverse", Name: "Reverse database migration",
			Description: "Runs only a declared reversible procedure from a recovery plan in reverse dependency order.",
			InputSchema: objectSchema(mergeSchemaProperties(common, map[string]any{
				"migrationId":           boundedKeySchema(128),
				"migrationChecksum":     checksumSchema(),
				"expectedSourceVersion": boundedStringSchema(128),
				"resultingVersion":      boundedStringSchema(128),
				"procedureReference":    boundedKeySchema(256),
			}), []any{
				"migrationId", "migrationChecksum", "databaseResourceKey",
				"databaseLockKey", "expectedSourceVersion", "resultingVersion",
				"procedureReference", "idempotencyKey", "timeoutSeconds",
			}),
			OutputSchema: migrationOutputSchema(),
		},
		{
			Type: "database.restore.execute", Name: "Execute database restore",
			Description: "Executes a manual restore only from a separately approved recovery plan with frozen data-loss inputs.",
			InputSchema: objectSchema(mergeSchemaProperties(common, map[string]any{
				"recoveryPlanId":           map[string]any{"type": "string", "format": "uuid"},
				"separateApprovalId":       boundedKeySchema(256),
				"backupId":                 boundedKeySchema(256),
				"backupChecksum":           checksumSchema(),
				"expectedDataLossBoundary": boundedStringSchema(128),
				"procedureVersion":         boundedKeySchema(128),
				"requiredApproverGroups":   boundedStringArraySchema(16, 128),
				"operatorScope":            boundedKeySchema(256),
				"validationProbes":         migrationProbeArraySchema(),
			}), []any{
				"recoveryPlanId", "separateApprovalId", "backupId", "backupChecksum",
				"databaseResourceKey", "databaseLockKey", "expectedDataLossBoundary",
				"procedureVersion", "requiredApproverGroups", "operatorScope",
				"validationProbes", "idempotencyKey", "timeoutSeconds",
			}),
			OutputSchema: restoreOutputSchema(),
		},
		{
			Type: "database.restore.verify", Name: "Verify database restore",
			Description: "Verifies an isolated restore drill and retains bounded schema and recovery evidence.",
			InputSchema: objectSchema(mergeSchemaProperties(common, map[string]any{
				"backupId":         boundedKeySchema(256),
				"backupChecksum":   checksumSchema(),
				"validationProbes": migrationProbeArraySchema(),
			}), []any{
				"backupId", "backupChecksum", "databaseResourceKey",
				"databaseLockKey", "validationProbes", "timeoutSeconds",
			}),
			OutputSchema: restoreOutputSchema(),
		},
	}
}

func backupVerifyInputSchema(common map[string]any) map[string]any {
	schema := objectSchema(mergeSchemaProperties(common, map[string]any{
		"backupId":          boundedKeySchema(256),
		"backupChecksum":    checksumSchema(),
		"backupReference":   boundedKeySchema(512),
		"verifierReference": boundedKeySchema(256),
		"probes":            migrationProbeArraySchema(),
	}), []any{
		"databaseResourceKey", "databaseLockKey", "verifierReference", "timeoutSeconds",
	})
	schema["oneOf"] = []any{
		map[string]any{"required": []any{"backupId", "backupChecksum"}},
		map[string]any{"required": []any{"backupReference"}},
	}
	return schema
}

func databaseCommonProperties() map[string]any {
	return map[string]any{
		"databaseResourceKey": boundedKeySchema(256),
		"targetLockKey":       boundedKeySchema(512),
		"databaseLockKey":     boundedKeySchema(512),
		"idempotencyKey":      boundedKeySchema(128),
		"timeoutSeconds": map[string]any{
			"type": "integer", "minimum": 1, "maximum": 86400,
		},
	}
}

func migrationProbeArraySchema() map[string]any {
	return map[string]any{
		"type": "array", "minItems": 1, "maxItems": 32,
		"items": objectSchema(map[string]any{
			"name":             boundedStringSchema(128),
			"reference":        boundedKeySchema(256),
			"expectedChecksum": checksumSchema(),
		}, []any{"name", "reference", "expectedChecksum"}),
	}
}

func backupOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"status":         statusSchema(),
		"backupId":       boundedKeySchema(256),
		"backupChecksum": checksumSchema(),
		"evidence":       evidenceArraySchema(),
	}, []any{"status", "backupId", "backupChecksum", "evidence"})
}

func migrationOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"status":         statusSchema(),
		"schemaVersion":  boundedStringSchema(128),
		"schemaChecksum": checksumSchema(),
		"retryKey":       boundedKeySchema(128),
		"evidence":       evidenceArraySchema(),
	}, []any{"status", "schemaVersion", "schemaChecksum", "evidence"})
}

func restoreOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"status":             statusSchema(),
		"schemaVersion":      boundedStringSchema(128),
		"schemaChecksum":     checksumSchema(),
		"operatorEvidenceId": boundedKeySchema(256),
		"evidence":           evidenceArraySchema(),
	}, []any{"status", "schemaVersion", "schemaChecksum", "operatorEvidenceId", "evidence"})
}

func evidenceArraySchema() map[string]any {
	return map[string]any{
		"type": "array", "maxItems": 32,
		"items": objectSchema(map[string]any{
			"type":      boundedKeySchema(64),
			"reference": boundedKeySchema(512),
			"checksum":  checksumSchema(),
			"redacted":  map[string]any{"const": true},
		}, []any{"type", "reference", "checksum", "redacted"}),
	}
}

func checksumSchema() map[string]any {
	return map[string]any{"type": "string", "pattern": `^sha256:[0-9a-f]{64}$`}
}

func statusSchema() map[string]any {
	return map[string]any{
		"type": "string",
		"enum": []any{"succeeded", "failed", "cancelled", "manual_intervention"},
	}
}

func boundedKeySchema(maxLength int) map[string]any {
	return map[string]any{
		"type": "string", "minLength": 1, "maxLength": maxLength,
		"pattern": `^[A-Za-z0-9][A-Za-z0-9._:/-]*$`,
	}
}

func secretReferenceSchema() map[string]any {
	return map[string]any{
		"type": "string", "minLength": 1, "maxLength": 128,
		"pattern": `^[A-Za-z0-9][A-Za-z0-9._:-]*$`,
	}
}

func boundedStringSchema(maxLength int) map[string]any {
	return map[string]any{"type": "string", "minLength": 1, "maxLength": maxLength}
}

func boundedStringArraySchema(maxItems, maxLength int) map[string]any {
	return map[string]any{
		"type": "array", "minItems": 1, "maxItems": maxItems, "uniqueItems": true,
		"items": boundedStringSchema(maxLength),
	}
}

func mergeSchemaProperties(base map[string]any, extra map[string]any) map[string]any {
	result := make(map[string]any, len(base)+len(extra))
	for key, value := range base {
		result[key] = value
	}
	for key, value := range extra {
		result[key] = value
	}
	return result
}
