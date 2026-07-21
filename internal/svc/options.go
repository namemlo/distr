package svc

import "github.com/distr-sh/distr/internal/auditexport"

type RegistryOption func(*Registry)

func ExecDbMigration(migrate bool) RegistryOption {
	return func(reg *Registry) {
		reg.execDbMigrations = migrate
	}
}

// ControlPlaneAuditExportAdapters configures explicitly allowlisted production
// transports and the secret-reference resolver they require. Omitting either
// dependency keeps audit export fail-closed.
func ControlPlaneAuditExportAdapters(
	factories auditexport.ProductionSinkFactories,
	resolveSecret auditexport.SecretReferenceResolver,
) RegistryOption {
	return func(reg *Registry) {
		reg.auditExportSinkFactories = make(
			auditexport.ProductionSinkFactories,
			len(factories),
		)
		for kind, factory := range factories {
			reg.auditExportSinkFactories[kind] = factory
		}
		reg.auditExportSecretResolver = resolveSecret
	}
}
