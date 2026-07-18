package governance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/distr-sh/distr/internal/conditions"
	"github.com/distr-sh/distr/internal/deploymentregistry"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

const (
	maxPolicyDurationSeconds = 366 * 24 * 60 * 60
	maxPolicyCollectionSize  = 256
)

var canonicalPolicyKeyPattern = regexp.MustCompile(`^[a-z0-9]+([._-][a-z0-9]+)*$`)

var shortenableOverrideGateKeys = map[string]struct{}{
	"approval-wait":    {},
	"campaign-bake":    {},
	"maintenance-wait": {},
	"optional-test":    {},
}

func ValidateDeploymentPolicyVersion(version types.DeploymentPolicyVersion) []types.ValidationIssue {
	issues := make([]types.ValidationIssue, 0)
	document := version.Document
	if document.Schema != types.DeploymentPolicySchemaV1 {
		issues = append(issues, policyIssue(
			"policy.schema.invalid",
			"document.schema",
			"schema must be distr.deployment-policy/v1",
		))
	}
	issues = append(issues, validateApprovalRules(document.ApprovalRules)...)
	issues = append(issues, validateRiskGates(document.RiskGates)...)
	issues = append(issues, validateAdmissionRules(document.AdmissionRules)...)
	issues = append(issues, validateCampaignRules(document.CampaignRules)...)
	issues = append(issues, validateOverrideRules(document.OverrideRules)...)
	issues = append(issues, validateRequiredEvidence(document.RequiredEvidence)...)
	issues = append(issues, validateBootstrapRules(
		document.BootstrapRules,
		document.ApprovalRules,
		document.RiskGates,
	)...)
	sortValidationIssues(issues)
	return issues
}

func CanonicalizeDeploymentPolicyDocument(
	document types.DeploymentPolicyDocument,
) (types.DeploymentPolicyDocument, json.RawMessage, string, error) {
	normalized := NormalizeDeploymentPolicyDocument(document)
	payload, err := json.Marshal(normalized)
	if err != nil {
		return types.DeploymentPolicyDocument{}, nil, "", fmt.Errorf(
			"marshal canonical deployment policy document: %w",
			err,
		)
	}
	sum := sha256.Sum256(payload)
	return normalized, payload, "sha256:" + hex.EncodeToString(sum[:]), nil
}

func NormalizeDeploymentPolicyDocument(
	document types.DeploymentPolicyDocument,
) types.DeploymentPolicyDocument {
	normalized := document
	normalized.Schema = strings.TrimSpace(document.Schema)
	normalized.ApprovalRules = cloneApprovalRules(document.ApprovalRules, false)
	normalized.RiskGates = cloneRiskGates(document.RiskGates, false)
	normalized.AdmissionRules = cloneAdmissionRules(document.AdmissionRules)
	normalized.OverrideRules = cloneOverrideRules(document.OverrideRules, false)
	normalized.RequiredEvidence = normalizedStrings(document.RequiredEvidence)
	normalized.BootstrapRules = cloneBootstrapRules(document.BootstrapRules)

	sort.Slice(normalized.ApprovalRules, func(i, j int) bool {
		left, right := normalized.ApprovalRules[i], normalized.ApprovalRules[j]
		if left.Key != right.Key {
			return left.Key < right.Key
		}
		return left.PrincipalGroupID.String() < right.PrincipalGroupID.String()
	})
	sort.Slice(normalized.RiskGates, func(i, j int) bool {
		left, right := normalized.RiskGates[i], normalized.RiskGates[j]
		if left.Key != right.Key {
			return left.Key < right.Key
		}
		return left.Condition < right.Condition
	})
	return normalized
}

func ComposeEffectivePolicy(
	owner types.PolicySet,
	subscribers []types.PolicySet,
) (types.EffectivePolicy, []types.ValidationIssue) {
	effective := emptyEffectivePolicy()
	issues := make([]types.ValidationIssue, 0)
	if owner.AuthorityKind != types.PolicyAuthorityOwner || owner.AuthorityID == uuid.Nil {
		issues = append(issues, policyIssue(
			"policy.owner.invalid",
			"owner",
			"owner policy authority must have kind owner and a non-empty ID",
		))
	}

	orderedSubscribers := slices.Clone(subscribers)
	sort.Slice(orderedSubscribers, func(i, j int) bool {
		return orderedSubscribers[i].AuthorityID.String() < orderedSubscribers[j].AuthorityID.String()
	})
	seenSubscribers := make(map[uuid.UUID]struct{}, len(orderedSubscribers))
	subscriberRows := make([]types.DeploymentUnitSubscriber, 0, len(orderedSubscribers))
	for index, subscriber := range orderedSubscribers {
		if subscriber.AuthorityKind != types.PolicyAuthoritySubscriber ||
			subscriber.AuthorityID == uuid.Nil {
			issues = append(issues, policyIssue(
				"policy.subscriber.invalid",
				fmt.Sprintf("subscribers.%d", index),
				"subscriber policy authority must have kind subscriber and a non-empty ID",
			))
			continue
		}
		if _, exists := seenSubscribers[subscriber.AuthorityID]; exists {
			issues = append(issues, policyIssue(
				"policy.subscriber.duplicate",
				fmt.Sprintf("subscribers.%d.authorityId", index),
				"subscriber policy authorities must be unique",
			))
			continue
		}
		seenSubscribers[subscriber.AuthorityID] = struct{}{}
		subscriberRows = append(subscriberRows, types.DeploymentUnitSubscriber{
			CustomerOrganizationID: subscriber.AuthorityID,
		})
	}
	effective.SubscriberSetChecksum = deploymentregistry.SubscriberSetChecksum(subscriberRows)
	if owner.SubscriberSetChecksum != "" &&
		owner.SubscriberSetChecksum != effective.SubscriberSetChecksum {
		issues = append(issues, policyIssue(
			"policy.subscriber_set.checksum_mismatch",
			"owner.subscriberSetChecksum",
			"subscriber authority set does not match the frozen subscriber-set checksum",
		))
	}

	policySets := make([]types.PolicySet, 0, len(orderedSubscribers)+1)
	policySets = append(policySets, owner)
	policySets = append(policySets, orderedSubscribers...)

	var modeIntersection map[types.RequirementResolutionMode]struct{}
	var windowIntersection map[uuid.UUID]struct{}
	windowConstraintCount := 0
	versionIDs := map[uuid.UUID]struct{}{}
	freezeIDs := map[uuid.UUID]struct{}{}
	requiredEvidence := map[string]struct{}{}
	bootstrapMode := types.BootstrapModeAllowAfterPreflight
	validPolicyCount := 0

	for _, policySet := range policySets {
		versions := slices.Clone(policySet.Versions)
		sort.Slice(versions, func(i, j int) bool {
			return versions[i].ID.String() < versions[j].ID.String()
		})
		if len(versions) == 0 {
			issues = append(issues, policyIssue(
				"policy.authority.unbound",
				"authorities."+policySet.AuthorityID.String(),
				"every owner and subscriber authority requires at least one published policy",
			))
			continue
		}
		for _, version := range versions {
			versionIssues := ValidateDeploymentPolicyVersion(version)
			if version.State != types.DeploymentPolicyVersionStatePublished {
				versionIssues = append(versionIssues, policyIssue(
					"policy.version.not_published",
					"versions."+version.ID.String()+".state",
					"effective policy composition accepts only published immutable versions",
				))
			}
			if version.ID == uuid.Nil {
				versionIssues = append(versionIssues, policyIssue(
					"policy.version.id_required",
					"versions.id",
					"policy version ID is required",
				))
			}
			if len(versionIssues) != 0 {
				issues = append(issues, versionIssues...)
				continue
			}

			normalized := NormalizeDeploymentPolicyDocument(version.Document)
			validPolicyCount++
			versionIDs[version.ID] = struct{}{}
			for _, rule := range normalized.ApprovalRules {
				rule.PolicyVersionID = version.ID
				rule.AuthorityKind = policySet.AuthorityKind
				rule.AuthorityID = policySet.AuthorityID
				effective.ApprovalRules = append(effective.ApprovalRules, rule)
			}
			for _, gate := range normalized.RiskGates {
				gate.PolicyVersionID = version.ID
				gate.AuthorityKind = policySet.AuthorityKind
				gate.AuthorityID = policySet.AuthorityID
				effective.RiskGates = append(effective.RiskGates, gate)
			}
			modeIntersection = intersectResolutionModes(
				modeIntersection,
				normalized.AdmissionRules.AllowedResolutionModes,
			)
			if len(normalized.AdmissionRules.MaintenanceWindowVersionIDs) > 0 {
				windowConstraintCount++
				windowIntersection = intersectUUIDs(
					windowIntersection,
					normalized.AdmissionRules.MaintenanceWindowVersionIDs,
				)
			}
			for _, freezeID := range normalized.AdmissionRules.FreezeRuleVersionIDs {
				freezeIDs[freezeID] = struct{}{}
			}
			effective.AdmissionRules.MinimumBakeSeconds = max(
				effective.AdmissionRules.MinimumBakeSeconds,
				normalized.AdmissionRules.MinimumBakeSeconds,
			)
			effective.AdmissionRules.MaximumWaitSeconds = max(
				effective.AdmissionRules.MaximumWaitSeconds,
				normalized.AdmissionRules.MaximumWaitSeconds,
			)
			composeCampaignRules(&effective.CampaignRules, normalized.CampaignRules, validPolicyCount == 1)
			override := normalized.OverrideRules
			override.PolicyVersionID = version.ID
			override.AuthorityKind = policySet.AuthorityKind
			override.AuthorityID = policySet.AuthorityID
			effective.OverrideRules = append(effective.OverrideRules, override)
			for _, evidence := range normalized.RequiredEvidence {
				requiredEvidence[evidence] = struct{}{}
			}
			bootstrapMode = stricterBootstrapMode(
				bootstrapMode,
				normalized.BootstrapRules.Mode,
			)
			for _, key := range normalized.BootstrapRules.ApprovalRuleKeys {
				effective.BootstrapRules.ApprovalRules = append(
					effective.BootstrapRules.ApprovalRules,
					types.EffectivePolicyReference{
						Key:             key,
						PolicyVersionID: version.ID,
						AuthorityKind:   policySet.AuthorityKind,
						AuthorityID:     policySet.AuthorityID,
					},
				)
			}
			for _, key := range normalized.BootstrapRules.RequiredGateKeys {
				effective.BootstrapRules.RequiredGates = append(
					effective.BootstrapRules.RequiredGates,
					types.EffectivePolicyReference{
						Key:             key,
						PolicyVersionID: version.ID,
						AuthorityKind:   policySet.AuthorityKind,
						AuthorityID:     policySet.AuthorityID,
					},
				)
			}
		}
	}

	effective.VersionIDs = sortedUUIDSet(versionIDs)
	effective.AdmissionRules.AllowedResolutionModes = sortedResolutionModeSet(modeIntersection)
	effective.AdmissionRules.MaintenanceWindowVersionIDs = sortedUUIDSet(windowIntersection)
	effective.AdmissionRules.FreezeRuleVersionIDs = sortedUUIDSet(freezeIDs)
	effective.RequiredEvidence = sortedStringSet(requiredEvidence)
	effective.BootstrapRules.Mode = bootstrapMode
	sortEffectivePolicy(&effective)

	if validPolicyCount == 0 {
		issues = append(issues, policyIssue(
			"policy.effective.empty",
			"versions",
			"effective policy requires at least one valid published version",
		))
	}
	if validPolicyCount > 0 && len(effective.AdmissionRules.AllowedResolutionModes) == 0 {
		issues = append(issues, policyIssue(
			"policy.resolution_mode.no_common",
			"admissionRules.allowedResolutionModes",
			"applicable policies have no common allowed requirement resolution mode",
		))
	}
	if windowConstraintCount > 1 && len(effective.AdmissionRules.MaintenanceWindowVersionIDs) == 0 {
		issues = append(issues, policyIssue(
			"policy.window.no_common",
			"admissionRules.maintenanceWindowVersionIds",
			"applicable policies have no common maintenance window",
		))
	}
	checksum, err := effectivePolicyChecksum(effective)
	if err != nil {
		issues = append(issues, policyIssue(
			"policy.effective.canonicalization_failed",
			"effectivePolicy",
			err.Error(),
		))
	} else {
		effective.Checksum = checksum
	}
	sortValidationIssues(issues)
	return effective, issues
}

func validateApprovalRules(rules []types.ApprovalRule) []types.ValidationIssue {
	issues := make([]types.ValidationIssue, 0)
	if len(rules) > maxPolicyCollectionSize {
		return []types.ValidationIssue{policyIssue(
			"policy.approval.too_many",
			"document.approvalRules",
			"approvalRules must contain at most 256 items",
		)}
	}
	seen := make(map[string]struct{}, len(rules))
	for index, rule := range rules {
		field := fmt.Sprintf("document.approvalRules.%d", index)
		if !isCanonicalPolicyKey(rule.Key) {
			issues = append(issues, policyIssue(
				"policy.approval.key_invalid",
				field+".key",
				"approval rule key must be canonical lowercase text",
			))
		} else if _, exists := seen[rule.Key]; exists {
			issues = append(issues, policyIssue(
				"policy.approval.key_duplicate",
				field+".key",
				"approval rule keys must be unique",
			))
		}
		seen[rule.Key] = struct{}{}
		if rule.PrincipalGroupID == uuid.Nil {
			issues = append(issues, policyIssue(
				"policy.approval.group_required",
				field+".principalGroupId",
				"principal group ID is required",
			))
		}
		if rule.Quorum < 1 || rule.Quorum > 100 {
			issues = append(issues, policyIssue(
				"policy.approval.quorum_invalid",
				field+".quorum",
				"approval quorum must be between 1 and 100",
			))
		}
		if rule.PolicyVersionID != uuid.Nil ||
			rule.AuthorityKind != "" ||
			rule.AuthorityID != uuid.Nil {
			issues = append(issues, policyIssue(
				"policy.approval.authority_forbidden",
				field,
				"document approval rules must not contain derived authority fields",
			))
		}
		seenConstraints := map[types.SeparationConstraint]struct{}{}
		for constraintIndex, constraint := range rule.SeparationConstraints {
			if !constraint.IsValid() {
				issues = append(issues, policyIssue(
					"policy.approval.separation_invalid",
					fmt.Sprintf("%s.separationConstraints.%d", field, constraintIndex),
					"separation constraint is invalid",
				))
			}
			if _, exists := seenConstraints[constraint]; exists {
				issues = append(issues, policyIssue(
					"policy.approval.separation_duplicate",
					fmt.Sprintf("%s.separationConstraints.%d", field, constraintIndex),
					"separation constraints must be unique",
				))
			}
			seenConstraints[constraint] = struct{}{}
		}
	}
	return issues
}

func validateRiskGates(gates []types.PolicyRiskGate) []types.ValidationIssue {
	issues := make([]types.ValidationIssue, 0)
	if len(gates) > maxPolicyCollectionSize {
		return []types.ValidationIssue{policyIssue(
			"policy.risk_gate.too_many",
			"document.riskGates",
			"riskGates must contain at most 256 items",
		)}
	}
	seen := make(map[string]struct{}, len(gates))
	for index, gate := range gates {
		field := fmt.Sprintf("document.riskGates.%d", index)
		if gate.PolicyVersionID != uuid.Nil ||
			gate.AuthorityKind != "" ||
			gate.AuthorityID != uuid.Nil {
			issues = append(issues, policyIssue(
				"policy.risk_gate.authority_forbidden",
				field,
				"document risk gates must not contain derived authority fields",
			))
		}
		if !isCanonicalPolicyKey(gate.Key) {
			issues = append(issues, policyIssue(
				"policy.risk_gate.key_invalid",
				field+".key",
				"risk gate key must be canonical lowercase text",
			))
		} else if _, exists := seen[gate.Key]; exists {
			issues = append(issues, policyIssue(
				"policy.risk_gate.key_duplicate",
				field+".key",
				"risk gate keys must be unique",
			))
		}
		seen[gate.Key] = struct{}{}
		if err := conditions.Validate(gate.Condition); err != nil {
			issues = append(issues, policyIssue(
				"policy.risk_gate.expression_invalid",
				field+".condition",
				"risk gate condition is not in the restricted expression language",
			))
		}
	}
	return issues
}

func validateAdmissionRules(rules types.AdmissionRules) []types.ValidationIssue {
	issues := make([]types.ValidationIssue, 0)
	if len(rules.AllowedResolutionModes) == 0 {
		issues = append(issues, policyIssue(
			"policy.resolution_mode.required",
			"document.admissionRules.allowedResolutionModes",
			"at least one allowed requirement resolution mode is required",
		))
	}
	seenModes := map[types.RequirementResolutionMode]struct{}{}
	for index, mode := range rules.AllowedResolutionModes {
		if !mode.IsValid() {
			issues = append(issues, policyIssue(
				"policy.resolution_mode.invalid",
				fmt.Sprintf("document.admissionRules.allowedResolutionModes.%d", index),
				"requirement resolution mode is invalid",
			))
		}
		if _, exists := seenModes[mode]; exists {
			issues = append(issues, policyIssue(
				"policy.resolution_mode.duplicate",
				fmt.Sprintf("document.admissionRules.allowedResolutionModes.%d", index),
				"allowed requirement resolution modes must be unique",
			))
		}
		seenModes[mode] = struct{}{}
	}
	if rules.MinimumBakeSeconds < 0 ||
		rules.MinimumBakeSeconds > maxPolicyDurationSeconds {
		issues = append(issues, policyIssue(
			"policy.admission.minimum_bake_invalid",
			"document.admissionRules.minimumBakeSeconds",
			"minimum bake seconds must be between 0 and 31622400",
		))
	}
	if rules.MaximumWaitSeconds < 0 ||
		rules.MaximumWaitSeconds > maxPolicyDurationSeconds {
		issues = append(issues, policyIssue(
			"policy.admission.maximum_wait_invalid",
			"document.admissionRules.maximumWaitSeconds",
			"maximum wait seconds must be between 0 and 31622400",
		))
	}
	issues = append(issues, validateUniqueUUIDs(
		rules.MaintenanceWindowVersionIDs,
		"policy.window",
		"document.admissionRules.maintenanceWindowVersionIds",
	)...)
	issues = append(issues, validateUniqueUUIDs(
		rules.FreezeRuleVersionIDs,
		"policy.freeze",
		"document.admissionRules.freezeRuleVersionIds",
	)...)
	return issues
}

func validateCampaignRules(rules types.CampaignRules) []types.ValidationIssue {
	issues := make([]types.ValidationIssue, 0)
	if rules.MinimumWaveBakeSeconds < 0 ||
		rules.MinimumWaveBakeSeconds > maxPolicyDurationSeconds {
		issues = append(issues, policyIssue(
			"policy.campaign.minimum_bake_invalid",
			"document.campaignRules.minimumWaveBakeSeconds",
			"minimum wave bake seconds must be between 0 and 31622400",
		))
	}
	if rules.MaximumWaveSize < 1 || rules.MaximumWaveSize > 10000 {
		issues = append(issues, policyIssue(
			"policy.campaign.maximum_wave_size_invalid",
			"document.campaignRules.maximumWaveSize",
			"maximum wave size must be between 1 and 10000",
		))
	}
	if rules.MaximumConcurrency < 1 ||
		rules.MaximumConcurrency > rules.MaximumWaveSize {
		issues = append(issues, policyIssue(
			"policy.campaign.maximum_concurrency_invalid",
			"document.campaignRules.maximumConcurrency",
			"maximum concurrency must be between 1 and maximumWaveSize",
		))
	}
	if rules.FailureToleranceBasisPoints < 0 ||
		rules.FailureToleranceBasisPoints > 10000 {
		issues = append(issues, policyIssue(
			"policy.campaign.failure_tolerance_invalid",
			"document.campaignRules.failureToleranceBasisPoints",
			"failure tolerance basis points must be between 0 and 10000",
		))
	}
	if rules.MinimumHealthyBasisPoints < 0 ||
		rules.MinimumHealthyBasisPoints > 10000 {
		issues = append(issues, policyIssue(
			"policy.campaign.minimum_healthy_invalid",
			"document.campaignRules.minimumHealthyBasisPoints",
			"minimum healthy basis points must be between 0 and 10000",
		))
	}
	return issues
}

func validateOverrideRules(rules types.OverrideRules) []types.ValidationIssue {
	issues := make([]types.ValidationIssue, 0)
	if rules.PolicyVersionID != uuid.Nil ||
		rules.AuthorityKind != "" ||
		rules.AuthorityID != uuid.Nil {
		issues = append(issues, policyIssue(
			"policy.override.authority_forbidden",
			"document.overrideRules",
			"document override rules must not contain derived authority fields",
		))
	}
	if !rules.Allowed {
		if rules.AuthorityGroupID != nil ||
			len(rules.ShortenableGateKeys) != 0 ||
			rules.MinimumReasonLength != 0 {
			issues = append(issues, policyIssue(
				"policy.override.disabled_fields_forbidden",
				"document.overrideRules",
				"disabled overrides must not declare authority, shortened gates, or a reason length",
			))
		}
		return issues
	}
	if rules.AuthorityGroupID == nil || *rules.AuthorityGroupID == uuid.Nil {
		issues = append(issues, policyIssue(
			"policy.override.authority_required",
			"document.overrideRules.authorityGroupId",
			"override authority group ID is required",
		))
	}
	if rules.MinimumReasonLength < 1 || rules.MinimumReasonLength > 4096 {
		issues = append(issues, policyIssue(
			"policy.override.reason_length_invalid",
			"document.overrideRules.minimumReasonLength",
			"override minimum reason length must be between 1 and 4096",
		))
	}
	seen := map[string]struct{}{}
	for index, key := range rules.ShortenableGateKeys {
		if _, allowed := shortenableOverrideGateKeys[key]; !allowed {
			issues = append(issues, policyIssue(
				"policy.override.gate_not_shortenable",
				fmt.Sprintf("document.overrideRules.shortenableGateKeys.%d", index),
				"override gate is not eligible for emergency acceleration",
			))
		}
		if _, exists := seen[key]; exists {
			issues = append(issues, policyIssue(
				"policy.override.gate_duplicate",
				fmt.Sprintf("document.overrideRules.shortenableGateKeys.%d", index),
				"shortenable override gates must be unique",
			))
		}
		seen[key] = struct{}{}
	}
	return issues
}

func validateRequiredEvidence(evidence []string) []types.ValidationIssue {
	issues := make([]types.ValidationIssue, 0)
	if len(evidence) > maxPolicyCollectionSize {
		return []types.ValidationIssue{policyIssue(
			"policy.evidence.too_many",
			"document.requiredEvidence",
			"requiredEvidence must contain at most 256 items",
		)}
	}
	seen := map[string]struct{}{}
	for index, key := range evidence {
		if !isCanonicalPolicyKey(key) {
			issues = append(issues, policyIssue(
				"policy.evidence.key_invalid",
				fmt.Sprintf("document.requiredEvidence.%d", index),
				"required evidence key must be canonical lowercase text",
			))
		}
		if _, exists := seen[key]; exists {
			issues = append(issues, policyIssue(
				"policy.evidence.key_duplicate",
				fmt.Sprintf("document.requiredEvidence.%d", index),
				"required evidence keys must be unique",
			))
		}
		seen[key] = struct{}{}
	}
	return issues
}

func validateBootstrapRules(
	rules types.BootstrapRules,
	approvalRules []types.ApprovalRule,
	riskGates []types.PolicyRiskGate,
) []types.ValidationIssue {
	issues := make([]types.ValidationIssue, 0)
	if !rules.Mode.IsValid() {
		issues = append(issues, policyIssue(
			"policy.bootstrap.mode_invalid",
			"document.bootstrapRules.mode",
			"bootstrap mode must be block, require_approval, or allow_after_preflight",
		))
	}
	approvalKeys := make(map[string]struct{}, len(approvalRules))
	for _, rule := range approvalRules {
		approvalKeys[rule.Key] = struct{}{}
	}
	seenApprovalKeys := map[string]struct{}{}
	for index, key := range rules.ApprovalRuleKeys {
		if _, exists := approvalKeys[key]; !exists {
			issues = append(issues, policyIssue(
				"policy.bootstrap.approval_rule_not_found",
				fmt.Sprintf("document.bootstrapRules.approvalRuleKeys.%d", index),
				"bootstrap approval rule must reference a declared approval rule",
			))
		}
		if _, exists := seenApprovalKeys[key]; exists {
			issues = append(issues, policyIssue(
				"policy.bootstrap.approval_rule_duplicate",
				fmt.Sprintf("document.bootstrapRules.approvalRuleKeys.%d", index),
				"bootstrap approval rule keys must be unique",
			))
		}
		seenApprovalKeys[key] = struct{}{}
	}
	if rules.Mode == types.BootstrapModeRequireApproval &&
		len(rules.ApprovalRuleKeys) == 0 {
		issues = append(issues, policyIssue(
			"policy.bootstrap.approval_required",
			"document.bootstrapRules.approvalRuleKeys",
			"require_approval bootstrap mode needs at least one approval rule",
		))
	}
	if rules.Mode != types.BootstrapModeRequireApproval &&
		len(rules.ApprovalRuleKeys) != 0 {
		issues = append(issues, policyIssue(
			"policy.bootstrap.approval_forbidden",
			"document.bootstrapRules.approvalRuleKeys",
			"bootstrap approval rules are only allowed in require_approval mode",
		))
	}
	gateKeys := make(map[string]struct{}, len(riskGates))
	for _, gate := range riskGates {
		gateKeys[gate.Key] = struct{}{}
	}
	seenGateKeys := map[string]struct{}{}
	for index, key := range rules.RequiredGateKeys {
		if _, exists := gateKeys[key]; !exists {
			issues = append(issues, policyIssue(
				"policy.bootstrap.risk_gate_not_found",
				fmt.Sprintf("document.bootstrapRules.requiredGateKeys.%d", index),
				"bootstrap risk gate must reference a declared risk gate",
			))
		}
		if _, exists := seenGateKeys[key]; exists {
			issues = append(issues, policyIssue(
				"policy.bootstrap.risk_gate_duplicate",
				fmt.Sprintf("document.bootstrapRules.requiredGateKeys.%d", index),
				"bootstrap risk gate keys must be unique",
			))
		}
		seenGateKeys[key] = struct{}{}
	}
	return issues
}

func validateUniqueUUIDs(
	values []uuid.UUID,
	codePrefix string,
	field string,
) []types.ValidationIssue {
	issues := make([]types.ValidationIssue, 0)
	if len(values) > maxPolicyCollectionSize {
		return []types.ValidationIssue{policyIssue(
			codePrefix+".too_many",
			field,
			"version references must contain at most 256 items",
		)}
	}
	seen := map[uuid.UUID]struct{}{}
	for index, value := range values {
		if value == uuid.Nil {
			issues = append(issues, policyIssue(
				codePrefix+".id_required",
				fmt.Sprintf("%s.%d", field, index),
				"version reference ID must not be empty",
			))
		}
		if _, exists := seen[value]; exists {
			issues = append(issues, policyIssue(
				codePrefix+".id_duplicate",
				fmt.Sprintf("%s.%d", field, index),
				"version reference IDs must be unique",
			))
		}
		seen[value] = struct{}{}
	}
	return issues
}

func emptyEffectivePolicy() types.EffectivePolicy {
	return types.EffectivePolicy{
		VersionIDs:    []uuid.UUID{},
		ApprovalRules: []types.ApprovalRule{},
		RiskGates:     []types.PolicyRiskGate{},
		AdmissionRules: types.AdmissionRules{
			AllowedResolutionModes:      []types.RequirementResolutionMode{},
			MaintenanceWindowVersionIDs: []uuid.UUID{},
			FreezeRuleVersionIDs:        []uuid.UUID{},
		},
		OverrideRules:    []types.OverrideRules{},
		RequiredEvidence: []string{},
		BootstrapRules: types.EffectiveBootstrapRules{
			ApprovalRules: []types.EffectivePolicyReference{},
			RequiredGates: []types.EffectivePolicyReference{},
		},
	}
}

func composeCampaignRules(
	effective *types.CampaignRules,
	next types.CampaignRules,
	first bool,
) {
	effective.MinimumWaveBakeSeconds = max(
		effective.MinimumWaveBakeSeconds,
		next.MinimumWaveBakeSeconds,
	)
	effective.MinimumHealthyBasisPoints = max(
		effective.MinimumHealthyBasisPoints,
		next.MinimumHealthyBasisPoints,
	)
	if first {
		effective.MaximumWaveSize = next.MaximumWaveSize
		effective.MaximumConcurrency = next.MaximumConcurrency
		effective.FailureToleranceBasisPoints = next.FailureToleranceBasisPoints
		return
	}
	effective.MaximumWaveSize = min(effective.MaximumWaveSize, next.MaximumWaveSize)
	effective.MaximumConcurrency = min(effective.MaximumConcurrency, next.MaximumConcurrency)
	effective.FailureToleranceBasisPoints = min(
		effective.FailureToleranceBasisPoints,
		next.FailureToleranceBasisPoints,
	)
}

func intersectResolutionModes(
	current map[types.RequirementResolutionMode]struct{},
	next []types.RequirementResolutionMode,
) map[types.RequirementResolutionMode]struct{} {
	nextSet := make(map[types.RequirementResolutionMode]struct{}, len(next))
	for _, value := range next {
		nextSet[value] = struct{}{}
	}
	if current == nil {
		return nextSet
	}
	for value := range current {
		if _, exists := nextSet[value]; !exists {
			delete(current, value)
		}
	}
	return current
}

func intersectUUIDs(current map[uuid.UUID]struct{}, next []uuid.UUID) map[uuid.UUID]struct{} {
	nextSet := make(map[uuid.UUID]struct{}, len(next))
	for _, value := range next {
		nextSet[value] = struct{}{}
	}
	if current == nil {
		return nextSet
	}
	for value := range current {
		if _, exists := nextSet[value]; !exists {
			delete(current, value)
		}
	}
	return current
}

func sortedResolutionModeSet(
	values map[types.RequirementResolutionMode]struct{},
) []types.RequirementResolutionMode {
	result := make([]types.RequirementResolutionMode, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func sortedUUIDSet(values map[uuid.UUID]struct{}) []uuid.UUID {
	result := make([]uuid.UUID, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].String() < result[j].String()
	})
	return result
}

func sortedStringSet(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func stricterBootstrapMode(left, right types.BootstrapMode) types.BootstrapMode {
	rank := map[types.BootstrapMode]int{
		types.BootstrapModeAllowAfterPreflight: 0,
		types.BootstrapModeRequireApproval:     1,
		types.BootstrapModeBlock:               2,
	}
	if rank[right] > rank[left] {
		return right
	}
	return left
}

func sortEffectivePolicy(effective *types.EffectivePolicy) {
	sort.Slice(effective.ApprovalRules, func(i, j int) bool {
		left, right := effective.ApprovalRules[i], effective.ApprovalRules[j]
		if left.AuthorityKind != right.AuthorityKind {
			return left.AuthorityKind < right.AuthorityKind
		}
		if left.AuthorityID != right.AuthorityID {
			return left.AuthorityID.String() < right.AuthorityID.String()
		}
		if left.Key != right.Key {
			return left.Key < right.Key
		}
		return left.PolicyVersionID.String() < right.PolicyVersionID.String()
	})
	sort.Slice(effective.OverrideRules, func(i, j int) bool {
		left, right := effective.OverrideRules[i], effective.OverrideRules[j]
		if left.AuthorityKind != right.AuthorityKind {
			return left.AuthorityKind < right.AuthorityKind
		}
		if left.AuthorityID != right.AuthorityID {
			return left.AuthorityID.String() < right.AuthorityID.String()
		}
		return left.PolicyVersionID.String() < right.PolicyVersionID.String()
	})
	sort.Slice(effective.RiskGates, func(i, j int) bool {
		left, right := effective.RiskGates[i], effective.RiskGates[j]
		if left.AuthorityKind != right.AuthorityKind {
			return left.AuthorityKind < right.AuthorityKind
		}
		if left.AuthorityID != right.AuthorityID {
			return left.AuthorityID.String() < right.AuthorityID.String()
		}
		if left.Key != right.Key {
			return left.Key < right.Key
		}
		if left.PolicyVersionID != right.PolicyVersionID {
			return left.PolicyVersionID.String() < right.PolicyVersionID.String()
		}
		return left.Condition < right.Condition
	})
	sortEffectivePolicyReferences(effective.BootstrapRules.ApprovalRules)
	sortEffectivePolicyReferences(effective.BootstrapRules.RequiredGates)
}

func sortEffectivePolicyReferences(references []types.EffectivePolicyReference) {
	sort.Slice(references, func(i, j int) bool {
		left, right := references[i], references[j]
		if left.AuthorityKind != right.AuthorityKind {
			return left.AuthorityKind < right.AuthorityKind
		}
		if left.AuthorityID != right.AuthorityID {
			return left.AuthorityID.String() < right.AuthorityID.String()
		}
		if left.Key != right.Key {
			return left.Key < right.Key
		}
		return left.PolicyVersionID.String() < right.PolicyVersionID.String()
	})
}

func effectivePolicyChecksum(effective types.EffectivePolicy) (string, error) {
	input := struct {
		Domain                string                        `json:"domain"`
		VersionIDs            []uuid.UUID                   `json:"versionIds"`
		SubscriberSetChecksum string                        `json:"subscriberSetChecksum"`
		ApprovalRules         []types.ApprovalRule          `json:"approvalRules"`
		RiskGates             []types.PolicyRiskGate        `json:"riskGates"`
		AdmissionRules        types.AdmissionRules          `json:"admissionRules"`
		CampaignRules         types.CampaignRules           `json:"campaignRules"`
		OverrideRules         []types.OverrideRules         `json:"overrideRules"`
		RequiredEvidence      []string                      `json:"requiredEvidence"`
		BootstrapRules        types.EffectiveBootstrapRules `json:"bootstrapRules"`
	}{
		Domain:                "distr.effective-deployment-policy/v1",
		VersionIDs:            effective.VersionIDs,
		SubscriberSetChecksum: effective.SubscriberSetChecksum,
		ApprovalRules:         effective.ApprovalRules,
		RiskGates:             effective.RiskGates,
		AdmissionRules:        effective.AdmissionRules,
		CampaignRules:         effective.CampaignRules,
		OverrideRules:         effective.OverrideRules,
		RequiredEvidence:      effective.RequiredEvidence,
		BootstrapRules:        effective.BootstrapRules,
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("marshal effective policy: %w", err)
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func cloneApprovalRules(
	rules []types.ApprovalRule,
	keepDerivedAuthority bool,
) []types.ApprovalRule {
	result := make([]types.ApprovalRule, len(rules))
	for index, rule := range rules {
		rule.Key = strings.TrimSpace(rule.Key)
		rule.SeparationConstraints = slices.Clone(rule.SeparationConstraints)
		if rule.SeparationConstraints == nil {
			rule.SeparationConstraints = []types.SeparationConstraint{}
		}
		sort.Slice(rule.SeparationConstraints, func(i, j int) bool {
			return rule.SeparationConstraints[i] < rule.SeparationConstraints[j]
		})
		if !keepDerivedAuthority {
			rule.PolicyVersionID = uuid.Nil
			rule.AuthorityKind = ""
			rule.AuthorityID = uuid.Nil
		}
		result[index] = rule
	}
	return result
}

func cloneRiskGates(
	gates []types.PolicyRiskGate,
	keepDerivedAuthority bool,
) []types.PolicyRiskGate {
	result := make([]types.PolicyRiskGate, len(gates))
	for index, gate := range gates {
		gate.Key = strings.TrimSpace(gate.Key)
		gate.Condition = strings.TrimSpace(gate.Condition)
		if !keepDerivedAuthority {
			gate.PolicyVersionID = uuid.Nil
			gate.AuthorityKind = ""
			gate.AuthorityID = uuid.Nil
		}
		result[index] = gate
	}
	return result
}

func cloneAdmissionRules(rules types.AdmissionRules) types.AdmissionRules {
	result := rules
	result.AllowedResolutionModes = slices.Clone(rules.AllowedResolutionModes)
	if result.AllowedResolutionModes == nil {
		result.AllowedResolutionModes = []types.RequirementResolutionMode{}
	}
	sort.Slice(result.AllowedResolutionModes, func(i, j int) bool {
		return result.AllowedResolutionModes[i] < result.AllowedResolutionModes[j]
	})
	result.MaintenanceWindowVersionIDs = slices.Clone(rules.MaintenanceWindowVersionIDs)
	if result.MaintenanceWindowVersionIDs == nil {
		result.MaintenanceWindowVersionIDs = []uuid.UUID{}
	}
	sort.Slice(result.MaintenanceWindowVersionIDs, func(i, j int) bool {
		return result.MaintenanceWindowVersionIDs[i].String() <
			result.MaintenanceWindowVersionIDs[j].String()
	})
	result.FreezeRuleVersionIDs = slices.Clone(rules.FreezeRuleVersionIDs)
	if result.FreezeRuleVersionIDs == nil {
		result.FreezeRuleVersionIDs = []uuid.UUID{}
	}
	sort.Slice(result.FreezeRuleVersionIDs, func(i, j int) bool {
		return result.FreezeRuleVersionIDs[i].String() <
			result.FreezeRuleVersionIDs[j].String()
	})
	return result
}

func cloneOverrideRules(
	rules types.OverrideRules,
	keepDerivedAuthority bool,
) types.OverrideRules {
	result := rules
	result.ShortenableGateKeys = normalizedStrings(rules.ShortenableGateKeys)
	if !keepDerivedAuthority {
		result.PolicyVersionID = uuid.Nil
		result.AuthorityKind = ""
		result.AuthorityID = uuid.Nil
	}
	return result
}

func cloneBootstrapRules(rules types.BootstrapRules) types.BootstrapRules {
	result := rules
	result.ApprovalRuleKeys = normalizedStrings(rules.ApprovalRuleKeys)
	result.RequiredGateKeys = normalizedStrings(rules.RequiredGateKeys)
	return result
}

func normalizedStrings(values []string) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = strings.TrimSpace(value)
	}
	sort.Strings(result)
	return result
}

func isCanonicalPolicyKey(value string) bool {
	return canonicalPolicyKeyPattern.MatchString(value)
}

func policyIssue(code, field, message string) types.ValidationIssue {
	return types.ValidationIssue{Code: code, Field: field, Message: message}
}

func sortValidationIssues(issues []types.ValidationIssue) {
	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].Code != issues[j].Code {
			return issues[i].Code < issues[j].Code
		}
		if issues[i].Field != issues[j].Field {
			return issues[i].Field < issues[j].Field
		}
		return issues[i].Message < issues[j].Message
	})
}
