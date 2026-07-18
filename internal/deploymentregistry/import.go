package deploymentregistry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

const (
	MaximumImportDiagnostics = 100
	MaximumImportPlacements  = 1000
)

var (
	importEvidencePattern = regexp.MustCompile(`^evidence://sha256/([0-9a-f]{64})$`)
	importChecksumPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	importCommitPattern   = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)
	importKeyPattern      = regexp.MustCompile(`^[a-z0-9]+([._-][a-z0-9]+)*$`)
	secretKeyPattern      = regexp.MustCompile(`(?i)(secret|password|passwd|token|credential|private.?key|api.?key)`)
	absolutePathPattern   = regexp.MustCompile(`(?i)^(?:[a-z]:[\\/]|\\\\|//|/)`)
	hostnamePattern       = regexp.MustCompile(`(?i)^(?:[a-z0-9-]+\.)+[a-z]{2,}(?::[0-9]+)?$`)
)

func PreviewImport(ctx context.Context, request types.RegistryImportRequest) (*types.RegistryImportPreview, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateImportNormalization(request); err != nil {
		return nil, err
	}
	request = NormalizeImportRequest(request)
	if err := validateExistingImportRoots(request.ExistingRoots); err != nil {
		return nil, fmt.Errorf("existing registry baseline contains unsafe data")
	}
	if err := validateImportRequest(request); err != nil {
		return nil, err
	}
	roots := request.Roots
	existing := request.ExistingRoots
	omissions, discoveredPlacements := sourcePlacementOmissions(
		request.SourcePlacements, roots,
	)
	diff := importDiff(existing, roots)
	diagnostics := importDiagnostics(roots)
	truncated := len(diagnostics) > MaximumImportDiagnostics
	if truncated {
		diagnostics = diagnostics[:MaximumImportDiagnostics]
	}
	counts := types.RegistryImportCounts{
		DiscoveredRoots:      len(roots),
		ClassifiedRoots:      RegistryCoverage(roots).ClassifiedRoots,
		DiscoveredPlacements: discoveredPlacements,
		OmittedPlacements:    len(omissions),
		Creates:              len(diff.Creates),
		Updates:              len(diff.Updates),
		Retirements:          len(diff.Retirements),
		Conflicts:            len(diff.Conflicts),
	}
	canonical := struct {
		SourceKind        string                                `json:"sourceKind"`
		ToolName          string                                `json:"toolName"`
		ToolVersion       string                                `json:"toolVersion"`
		SourceCommit      string                                `json:"sourceCommit,omitempty"`
		Parameters        [][2]string                           `json:"parameters"`
		EvidenceReference string                                `json:"evidenceReference"`
		EvidenceChecksum  string                                `json:"evidenceChecksum"`
		SourcePlacements  []types.RegistryImportSourcePlacement `json:"sourcePlacements"`
		Roots             []types.RegistryImportCandidateRoot   `json:"roots"`
		Diff              types.RegistryImportDiff              `json:"diff"`
		Omissions         []string                              `json:"omissions"`
	}{
		request.SourceKind, request.ToolName, request.ToolVersion, request.SourceCommit,
		sortedParameters(request.Parameters), request.EvidenceReference, request.EvidenceChecksum,
		request.SourcePlacements, roots, diff, omissions,
	}
	payload, err := json.Marshal(canonical)
	if err != nil {
		return nil, fmt.Errorf("canonicalize registry import: %w", err)
	}
	sum := sha256.Sum256(payload)
	return &types.RegistryImportPreview{
		PreviewChecksum:      "sha256:" + hex.EncodeToString(sum[:]),
		Counts:               counts,
		Diff:                 diff,
		Omissions:            omissions,
		Diagnostics:          diagnostics,
		DiagnosticsTruncated: truncated,
		Roots:                roots,
	}, nil
}

func validateExistingImportRoots(roots []types.RegistryImportCandidateRoot) error {
	for _, root := range roots {
		if len(root.Key) > 64 || !importKeyPattern.MatchString(root.Key) ||
			len(root.Name) == 0 || len(root.Name) > 256 ||
			len(root.PhysicalIdentity) == 0 || len(root.PhysicalIdentity) > 512 ||
			root.SourcePath != "" {
			return fmt.Errorf("unsafe existing root")
		}
		for field, value := range map[string]string{
			"root key": root.Key, "root name": root.Name, "physical identity": root.PhysicalIdentity,
		} {
			if err := validateSafeImportText(field, value); err != nil {
				return err
			}
		}
		for _, placement := range root.Placements {
			if len(placement.ComponentKey) > 64 ||
				!importKeyPattern.MatchString(placement.ComponentKey) ||
				len(placement.PhysicalName) == 0 || len(placement.PhysicalName) > 512 ||
				len(placement.ConfigNamespace) > 512 || len(placement.DatabaseBoundary) > 512 ||
				len(placement.HealthAdapter) > 256 || len(placement.RenamedFrom) > 512 {
				return fmt.Errorf("unsafe existing placement")
			}
			for field, value := range map[string]string{
				"component key": placement.ComponentKey, "physical name": placement.PhysicalName,
				"config namespace": placement.ConfigNamespace, "database boundary": placement.DatabaseBoundary,
				"health adapter": placement.HealthAdapter, "renamed from": placement.RenamedFrom,
			} {
				if err := validateSafeImportText(field, value); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func ClassificationResult(
	classification types.ImportClassification,
	candidateModel types.DeliveryModel,
) (types.DeliveryModel, types.RegistryManagementState, bool, error) {
	switch classification {
	case types.ImportClassificationStandard:
		return types.DeliveryModelDedicated, types.RegistryManagementStateManaged, true, nil
	case types.ImportClassificationShared:
		return types.DeliveryModelShared, types.RegistryManagementStateManaged, true, nil
	case types.ImportClassificationExternal:
		return types.DeliveryModelExternal, types.RegistryManagementStateExternal, true, nil
	case types.ImportClassificationObserveOnly:
		if !candidateModel.IsValid() {
			return "", "", false, fmt.Errorf("observe_only requires a valid candidate delivery model")
		}
		return candidateModel, types.RegistryManagementStateObserveOnly, true, nil
	case types.ImportClassificationIgnored:
		return "", "", false, nil
	case types.ImportClassificationNeedsDecision:
		return "", types.RegistryManagementStateUnclassified, false, nil
	default:
		return "", "", false, fmt.Errorf("invalid import classification %q", classification)
	}
}

func RegistryCoverage(roots []types.RegistryImportCandidateRoot) types.RegistryCoverageReport {
	report := types.RegistryCoverageReport{
		DiscoveredRoots:      len(roots),
		DiscoveredPlacements: placementCount(roots),
		Omissions:            []string{},
	}
	services := make(map[string]struct{})
	for _, root := range roots {
		for _, placement := range root.Placements {
			services[placement.ComponentKey] = struct{}{}
		}
		topologyIssues := registryImportTopologyIssues(root)
		topologyValid := len(topologyIssues) == 0
		switch root.Classification {
		case types.ImportClassificationStandard, types.ImportClassificationShared:
			report.ClassifiedRoots++
			if topologyValid {
				report.ActionableManagedRoots++
			}
		case types.ImportClassificationObserveOnly:
			report.ClassifiedRoots++
			report.ObserveOnlyRoots++
		case types.ImportClassificationExternal:
			report.ClassifiedRoots++
			report.ExternalRoots++
		case types.ImportClassificationIgnored:
			report.ClassifiedRoots++
			report.IgnoredRoots++
		default:
			report.UnresolvedRoots++
			report.Omissions = append(report.Omissions, root.Key)
		}
		if root.Classification.IsValid() &&
			root.Classification != types.ImportClassificationNeedsDecision &&
			len(topologyIssues) != 0 {
			report.UnresolvedRoots++
			for _, issue := range topologyIssues {
				report.Omissions = append(report.Omissions, root.Key+":"+issue.Field)
			}
		}
	}
	report.Services = len(services)
	sort.Strings(report.Omissions)
	report.Complete = report.UnresolvedRoots == 0 && report.OmittedPlacements == 0
	return report
}

func RegistryCoverageWithOmissions(
	roots []types.RegistryImportCandidateRoot,
	omissions []string,
) types.RegistryCoverageReport {
	report := RegistryCoverage(roots)
	report.OmittedPlacements = len(omissions)
	report.Omissions = append(report.Omissions, omissions...)
	sort.Strings(report.Omissions)
	report.Complete = report.UnresolvedRoots == 0 && report.OmittedPlacements == 0
	return report
}

func validateImportRequest(request types.RegistryImportRequest) error {
	if request.OrganizationID == [16]byte{} {
		return fmt.Errorf("organizationId is required")
	}
	if request.ActorID == [16]byte{} {
		return fmt.Errorf("actor is required")
	}
	for field, value := range map[string]string{
		"sourceKind": request.SourceKind, "toolName": request.ToolName, "toolVersion": request.ToolVersion,
	} {
		if strings.TrimSpace(value) == "" || utf8.RuneCountInString(value) > 128 {
			return fmt.Errorf("%s is required and limited to 128 characters", field)
		}
		if err := validateSafeImportText(field, value); err != nil {
			return err
		}
	}
	if request.SourceCommit != "" && !importCommitPattern.MatchString(request.SourceCommit) {
		return fmt.Errorf("sourceCommit must be an exact lowercase git checksum")
	}
	match := importEvidencePattern.FindStringSubmatch(request.EvidenceReference)
	if len(match) != 2 || !importChecksumPattern.MatchString(request.EvidenceChecksum) ||
		match[1] != request.EvidenceChecksum {
		return fmt.Errorf("evidenceReference must be evidence://sha256/<checksum> matching evidenceChecksum")
	}
	if len(request.Parameters) > 64 {
		return fmt.Errorf("parameters exceed the maximum of 64")
	}
	for key, value := range request.Parameters {
		if len(key) == 0 || len(key) > 128 || len(value) > 1024 {
			return fmt.Errorf("parameter %q contains an unsafe or unbounded value", key)
		}
		if err := validateSafeImportText("parameter key", key); err != nil {
			return err
		}
		if err := validateSafeImportText("parameter value", value); err != nil {
			return err
		}
	}
	if len(request.SourcePlacements) > MaximumImportPlacements {
		return fmt.Errorf("source placements exceed the maximum of %d", MaximumImportPlacements)
	}
	seenSourcePlacements := make(map[string]struct{}, len(request.SourcePlacements))
	for _, placement := range request.SourcePlacements {
		if len(placement.RootKey) > 64 || !importKeyPattern.MatchString(placement.RootKey) ||
			len(placement.PhysicalName) == 0 || len(placement.PhysicalName) > 512 {
			return fmt.Errorf("source placement contains an unsafe or unbounded identity")
		}
		if err := validateSafeImportText("root key", placement.RootKey); err != nil {
			return err
		}
		if err := validateSafeImportText("physical name", placement.PhysicalName); err != nil {
			return err
		}
		identity := sourcePlacementIdentity(placement)
		if _, exists := seenSourcePlacements[identity]; exists {
			return fmt.Errorf("source placements contain duplicate identities")
		}
		seenSourcePlacements[identity] = struct{}{}
	}
	seenRoots := make(map[string]struct{}, len(request.Roots))
	totalPlacements := 0
	for _, root := range request.Roots {
		if _, exists := seenRoots[root.Key]; exists {
			return fmt.Errorf("duplicate root %q", root.Key)
		}
		seenRoots[root.Key] = struct{}{}
		if root.SourcePath != "" {
			return fmt.Errorf("sourcePath is not accepted; use content-addressed evidence")
		}
		if len(root.Key) > 64 || !importKeyPattern.MatchString(root.Key) {
			return fmt.Errorf("root key %q must be canonical lowercase text", root.Key)
		}
		if !root.DeliveryModel.IsValid() {
			return fmt.Errorf("root %q deliveryModel is invalid", root.Key)
		}
		if !root.Classification.IsValid() {
			return fmt.Errorf("root %q classification is invalid", root.Key)
		}
		if len(root.Name) == 0 || len(root.Name) > 256 ||
			len(root.PhysicalIdentity) == 0 || len(root.PhysicalIdentity) > 512 {
			return fmt.Errorf("root %q contains unsafe or unbounded text", root.Key)
		}
		for field, value := range map[string]string{
			"root key": root.Key, "root name": root.Name, "physical identity": root.PhysicalIdentity,
		} {
			if err := validateSafeImportText(field, value); err != nil {
				return fmt.Errorf("root %q: %w", root.Key, err)
			}
		}
		for _, subscriberID := range root.SubscriberCustomerOrganizationIDs {
			if subscriberID == [16]byte{} {
				return fmt.Errorf("root %q contains an empty subscriber ID", root.Key)
			}
		}
		seenPlacements := make(map[string]struct{}, len(root.Placements))
		totalPlacements += len(root.Placements)
		if totalPlacements > MaximumImportPlacements {
			return fmt.Errorf("candidate placements exceed the maximum of %d", MaximumImportPlacements)
		}
		seenComponents := make(map[string]struct{}, len(root.Placements))
		seenPhysicalNames := make(map[string]struct{}, len(root.Placements))
		for _, placement := range root.Placements {
			if len(placement.ComponentKey) > 64 ||
				!importKeyPattern.MatchString(placement.ComponentKey) ||
				len(placement.PhysicalName) == 0 || len(placement.PhysicalName) > 512 ||
				len(placement.ConfigNamespace) > 512 || len(placement.DatabaseBoundary) > 512 ||
				len(placement.HealthAdapter) > 256 || len(placement.RenamedFrom) > 512 {
				return fmt.Errorf("root %q contains an unsafe or unbounded placement", root.Key)
			}
			for field, value := range map[string]string{
				"component key": placement.ComponentKey, "physical name": placement.PhysicalName,
				"config namespace": placement.ConfigNamespace, "database boundary": placement.DatabaseBoundary,
				"health adapter": placement.HealthAdapter, "renamed from": placement.RenamedFrom,
			} {
				if err := validateSafeImportText(field, value); err != nil {
					return fmt.Errorf("root %q: %w", root.Key, err)
				}
			}
			if len(request.SourcePlacements) != 0 {
				sourceIdentity := sourcePlacementIdentity(types.RegistryImportSourcePlacement{
					RootKey: root.Key, PhysicalName: placement.PhysicalName,
				})
				if _, exists := seenSourcePlacements[sourceIdentity]; !exists {
					return fmt.Errorf(
						"candidate placement is absent from the source placement baseline",
					)
				}
			}
			key := placementIdentity(placement)
			if _, exists := seenPlacements[key]; exists {
				return fmt.Errorf("duplicate placement in root %q", root.Key)
			}
			seenPlacements[key] = struct{}{}
			if _, exists := seenComponents[placement.ComponentKey]; exists {
				return fmt.Errorf("duplicate component placement in root %q", root.Key)
			}
			seenComponents[placement.ComponentKey] = struct{}{}
			physicalIdentity := strings.ToLower(placement.PhysicalName)
			if _, exists := seenPhysicalNames[physicalIdentity]; exists {
				return fmt.Errorf("duplicate physical placement in root %q", root.Key)
			}
			seenPhysicalNames[physicalIdentity] = struct{}{}
		}
	}
	return nil
}

// NormalizeImportRequest canonicalizes every accepted string and subscriber set
// before checksums, persistence, or API projection.
func NormalizeImportRequest(request types.RegistryImportRequest) types.RegistryImportRequest {
	request.SourceKind = strings.TrimSpace(request.SourceKind)
	request.ToolName = strings.TrimSpace(request.ToolName)
	request.ToolVersion = strings.TrimSpace(request.ToolVersion)
	request.SourceCommit = strings.ToLower(strings.TrimSpace(request.SourceCommit))
	request.EvidenceReference = strings.ToLower(strings.TrimSpace(request.EvidenceReference))
	request.EvidenceChecksum = strings.ToLower(strings.TrimSpace(request.EvidenceChecksum))
	parameters := make(map[string]string, len(request.Parameters))
	for key, value := range request.Parameters {
		parameters[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	request.Parameters = parameters
	request.SourcePlacements = canonicalSourcePlacements(request.SourcePlacements)
	request.Roots = canonicalRoots(request.Roots)
	request.ExistingRoots = canonicalRoots(request.ExistingRoots)
	return request
}

func canonicalSourcePlacements(
	input []types.RegistryImportSourcePlacement,
) []types.RegistryImportSourcePlacement {
	result := append([]types.RegistryImportSourcePlacement(nil), input...)
	for index := range result {
		result[index].RootKey = strings.ToLower(strings.TrimSpace(result[index].RootKey))
		result[index].PhysicalName = strings.TrimSpace(result[index].PhysicalName)
	}
	sort.Slice(result, func(i, j int) bool {
		return sourcePlacementIdentity(result[i]) < sourcePlacementIdentity(result[j])
	})
	if result == nil {
		return []types.RegistryImportSourcePlacement{}
	}
	return result
}

func canonicalRoots(input []types.RegistryImportCandidateRoot) []types.RegistryImportCandidateRoot {
	result := append([]types.RegistryImportCandidateRoot(nil), input...)
	for index := range result {
		result[index].Key = strings.ToLower(strings.TrimSpace(result[index].Key))
		result[index].Name = strings.TrimSpace(result[index].Name)
		result[index].DeliveryModel = types.DeliveryModel(
			strings.ToLower(strings.TrimSpace(string(result[index].DeliveryModel))),
		)
		result[index].Classification = types.ImportClassification(
			strings.ToLower(strings.TrimSpace(string(result[index].Classification))),
		)
		result[index].PhysicalIdentity = strings.TrimSpace(result[index].PhysicalIdentity)
		result[index].SourcePath = strings.TrimSpace(result[index].SourcePath)
		result[index].SubscriberCustomerOrganizationIDs = canonicalSubscriberIDs(
			result[index].SubscriberCustomerOrganizationIDs,
		)
		result[index].Placements = append([]types.RegistryImportCandidatePlacement(nil), result[index].Placements...)
		for placementIndex := range result[index].Placements {
			placement := &result[index].Placements[placementIndex]
			placement.ComponentKey = strings.ToLower(strings.TrimSpace(placement.ComponentKey))
			placement.PhysicalName = strings.TrimSpace(placement.PhysicalName)
			placement.ConfigNamespace = strings.TrimSpace(placement.ConfigNamespace)
			placement.DatabaseBoundary = strings.TrimSpace(placement.DatabaseBoundary)
			placement.HealthAdapter = strings.TrimSpace(placement.HealthAdapter)
			placement.RenamedFrom = strings.TrimSpace(placement.RenamedFrom)
		}
		sort.Slice(result[index].Placements, func(i, j int) bool {
			return placementIdentity(result[index].Placements[i]) < placementIdentity(result[index].Placements[j])
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	return result
}

func importDiff(existing, incoming []types.RegistryImportCandidateRoot) types.RegistryImportDiff {
	diff := types.RegistryImportDiff{}
	oldRoots := rootMap(existing)
	newRoots := rootMap(incoming)
	for _, root := range incoming {
		old, exists := oldRoots[root.Key]
		if !exists {
			diff.Creates = append(diff.Creates, change("create_root", root.Key, "", "", "new discovered root"))
			continue
		}
		if !registryImportRootTopologyEqual(old, root) {
			diff.Conflicts = append(diff.Conflicts, change(
				"root_topology_changed", root.Key, "", "",
				"existing root topology differs from discovered evidence",
			))
			continue
		}
		diffPlacements(&diff, root.Key, old.Placements, root.Placements)
	}
	for _, root := range existing {
		if _, exists := newRoots[root.Key]; !exists {
			diff.Retirements = append(diff.Retirements, change("retire_root", root.Key, "", "", "root absent from current discovery"))
		}
	}
	sortChanges(&diff)
	return diff
}

func importDiagnostics(roots []types.RegistryImportCandidateRoot) []types.ValidationIssue {
	result := make([]types.ValidationIssue, 0)
	for _, root := range roots {
		result = append(result, registryImportTopologyIssues(root)...)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Field != result[j].Field {
			return result[i].Field < result[j].Field
		}
		return result[i].Code < result[j].Code
	})
	return result
}

func placementCount(roots []types.RegistryImportCandidateRoot) int {
	total := 0
	for _, root := range roots {
		total += len(root.Placements)
	}
	return total
}
func sourcePlacementOmissions(
	source []types.RegistryImportSourcePlacement,
	roots []types.RegistryImportCandidateRoot,
) ([]string, int) {
	if len(source) == 0 {
		return []string{}, placementCount(roots)
	}
	mapped := make(map[string]struct{}, placementCount(roots))
	for _, root := range roots {
		for _, placement := range root.Placements {
			mapped[sourcePlacementIdentity(types.RegistryImportSourcePlacement{
				RootKey: root.Key, PhysicalName: placement.PhysicalName,
			})] = struct{}{}
		}
	}
	omissions := make([]string, 0)
	for _, placement := range source {
		if _, exists := mapped[sourcePlacementIdentity(placement)]; exists {
			continue
		}
		omissions = append(omissions, placement.RootKey+":"+placement.PhysicalName)
	}
	sort.Strings(omissions)
	return omissions, len(source)
}
func sortedParameters(parameters map[string]string) [][2]string {
	result := make([][2]string, 0, len(parameters))
	for key, value := range parameters {
		result = append(result, [2]string{key, value})
	}
	sort.Slice(result, func(i, j int) bool { return result[i][0] < result[j][0] })
	return result
}
func rootMap(roots []types.RegistryImportCandidateRoot) map[string]types.RegistryImportCandidateRoot {
	result := make(map[string]types.RegistryImportCandidateRoot, len(roots))
	for _, root := range roots {
		result[root.Key] = root
	}
	return result
}
func placementIdentity(placement types.RegistryImportCandidatePlacement) string {
	return placement.ComponentKey + "\x00" + strings.ToLower(strings.TrimSpace(placement.PhysicalName))
}
func sourcePlacementIdentity(placement types.RegistryImportSourcePlacement) string {
	return placement.RootKey + "\x00" + strings.ToLower(strings.TrimSpace(placement.PhysicalName))
}
func change(kind, root, placement, physicalName, message string) types.RegistryImportChange {
	return types.RegistryImportChange{
		Kind: kind, RootKey: root, PlacementKey: placement,
		PhysicalName: physicalName, Message: message,
	}
}
func sortChanges(diff *types.RegistryImportDiff) {
	for _, list := range []*[]types.RegistryImportChange{&diff.Creates, &diff.Updates, &diff.Retirements, &diff.Conflicts} {
		sort.Slice(*list, func(i, j int) bool {
			left, right := (*list)[i], (*list)[j]
			return left.RootKey+"\x00"+left.PlacementKey+"\x00"+left.PhysicalName+"\x00"+left.Kind <
				right.RootKey+"\x00"+right.PlacementKey+"\x00"+right.PhysicalName+"\x00"+right.Kind
		})
	}
}

func validateImportNormalization(request types.RegistryImportRequest) error {
	normalizedParameters := make(map[string]struct{}, len(request.Parameters))
	for key := range request.Parameters {
		normalizedKey := strings.TrimSpace(key)
		if _, exists := normalizedParameters[normalizedKey]; exists {
			return fmt.Errorf("parameters contain duplicate keys after normalization")
		}
		normalizedParameters[normalizedKey] = struct{}{}
	}
	return nil
}

func validateSafeImportText(field, value string) error {
	if value == "" {
		return nil
	}
	if secretKeyPattern.MatchString(value) {
		return fmt.Errorf("%s contains secret-looking text", field)
	}
	trimmed := strings.TrimSpace(value)
	lower := strings.ToLower(trimmed)
	identifierField := field == "root key" || field == "component key" ||
		field == "sourceKind" || field == "toolName" || field == "toolVersion" ||
		field == "parameter key"
	if absolutePathPattern.MatchString(trimmed) ||
		strings.Contains(lower, "://") ||
		strings.HasPrefix(lower, "host=") ||
		strings.HasPrefix(lower, "hostname=") ||
		strings.Contains(lower, ".internal") ||
		strings.Contains(lower, ".local") ||
		lower == "localhost" ||
		(!identifierField && hostnamePattern.MatchString(trimmed)) ||
		net.ParseIP(trimmed) != nil {
		return fmt.Errorf("%s contains path or hostname data", field)
	}
	return nil
}

func canonicalSubscriberIDs(input []uuid.UUID) []uuid.UUID {
	if len(input) == 0 {
		return []uuid.UUID{}
	}
	seen := make(map[uuid.UUID]struct{}, len(input))
	result := make([]uuid.UUID, 0, len(input))
	for _, id := range input {
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].String() < result[j].String()
	})
	return result
}

func registryImportTopologyIssues(root types.RegistryImportCandidateRoot) []types.ValidationIssue {
	issues := make([]types.ValidationIssue, 0)
	add := func(code, field, message string) {
		issues = append(issues, types.ValidationIssue{
			Code:    "registry.import." + code,
			Field:   "roots." + root.Key + "." + field,
			Message: message,
		})
	}
	model, _, create, err := ClassificationResult(root.Classification, root.DeliveryModel)
	if err != nil {
		add("delivery_invalid", "deliveryModel", "classification and delivery model are incompatible")
		return issues
	}
	if !create {
		return issues
	}
	if root.DeploymentTargetID == uuid.Nil {
		add("deployment_target_required", "deploymentTargetId", "actionable root requires a deployment target")
	}
	if root.EnvironmentID == uuid.Nil {
		add("environment_required", "environmentId", "actionable root requires an environment")
	}
	switch model {
	case types.DeliveryModelDedicated:
		if root.CustomerOrganizationID == nil {
			add("customer_required", "customerOrganizationId", "dedicated root requires a customer")
		}
		if len(root.SubscriberCustomerOrganizationIDs) != 0 {
			add("subscribers_forbidden", "subscriberCustomerOrganizationIds", "dedicated root cannot declare subscribers")
		}
	case types.DeliveryModelShared:
		if root.CustomerOrganizationID != nil {
			add("customer_forbidden", "customerOrganizationId", "shared root cannot declare a single customer")
		}
		if len(root.SubscriberCustomerOrganizationIDs) == 0 {
			add("subscribers_required", "subscriberCustomerOrganizationIds", "shared root requires subscribers")
		}
	case types.DeliveryModelExternal:
		if root.CustomerOrganizationID != nil {
			add("customer_forbidden", "customerOrganizationId", "external root cannot declare a customer")
		}
		if len(root.SubscriberCustomerOrganizationIDs) != 0 {
			add("subscribers_forbidden", "subscriberCustomerOrganizationIds", "external root cannot declare subscribers")
		}
	}
	componentCounts := make(map[string]int, len(root.Placements))
	for _, placement := range root.Placements {
		componentCounts[placement.ComponentKey]++
	}
	for componentKey, count := range componentCounts {
		if count > 1 {
			add(
				"component_placement_ambiguous",
				"placements."+componentKey,
				"an actionable root supports one active placement per component",
			)
		}
	}
	sort.Slice(issues, func(i, j int) bool {
		if issues[i].Field != issues[j].Field {
			return issues[i].Field < issues[j].Field
		}
		return issues[i].Code < issues[j].Code
	})
	return issues
}

func diffPlacements(
	diff *types.RegistryImportDiff,
	rootKey string,
	existing []types.RegistryImportCandidatePlacement,
	incoming []types.RegistryImportCandidatePlacement,
) {
	oldByIdentity := make(map[string]types.RegistryImportCandidatePlacement, len(existing))
	for _, placement := range existing {
		oldByIdentity[placementIdentity(placement)] = placement
	}
	usedOld := make(map[string]struct{}, len(existing))
	unmatchedIncoming := make([]types.RegistryImportCandidatePlacement, 0)
	for _, placement := range incoming {
		identity := placementIdentity(placement)
		previous, found := oldByIdentity[identity]
		if !found {
			unmatchedIncoming = append(unmatchedIncoming, placement)
			continue
		}
		usedOld[identity] = struct{}{}
		if !placementMetadataEqual(previous, placement) {
			diff.Updates = append(diff.Updates, change(
				"update_placement", rootKey, placement.ComponentKey, placement.PhysicalName,
				"component placement metadata changed",
			))
		}
	}

	incomingByComponent := make(map[string]int, len(unmatchedIncoming))
	for _, placement := range unmatchedIncoming {
		incomingByComponent[placement.ComponentKey]++
	}
	for _, placement := range unmatchedIncoming {
		oldCandidates := make([]types.RegistryImportCandidatePlacement, 0, 1)
		for _, previous := range existing {
			if previous.ComponentKey != placement.ComponentKey {
				continue
			}
			if _, used := usedOld[placementIdentity(previous)]; used {
				continue
			}
			oldCandidates = append(oldCandidates, previous)
		}
		if len(oldCandidates) == 1 && incomingByComponent[placement.ComponentKey] == 1 {
			previous := oldCandidates[0]
			usedOld[placementIdentity(previous)] = struct{}{}
			if strings.EqualFold(placement.RenamedFrom, previous.PhysicalName) {
				diff.Updates = append(diff.Updates, change(
					"rename_placement", rootKey, placement.ComponentKey, placement.PhysicalName,
					"aliased physical rename",
				))
			} else {
				diff.Conflicts = append(diff.Conflicts, change(
					"rename_requires_decision", rootKey, placement.ComponentKey, placement.PhysicalName,
					"physical rename requires an alias or retire/new identity decision",
				))
			}
			continue
		}
		diff.Creates = append(diff.Creates, change(
			"create_placement", rootKey, placement.ComponentKey, placement.PhysicalName,
			"new component placement",
		))
	}
	for _, previous := range existing {
		if _, used := usedOld[placementIdentity(previous)]; used {
			continue
		}
		diff.Retirements = append(diff.Retirements, change(
			"retire_placement", rootKey, previous.ComponentKey, previous.PhysicalName,
			"placement absent from current discovery",
		))
	}
}

func placementMetadataEqual(
	left, right types.RegistryImportCandidatePlacement,
) bool {
	return left.ConfigNamespace == right.ConfigNamespace &&
		left.DatabaseBoundary == right.DatabaseBoundary &&
		left.HealthAdapter == right.HealthAdapter
}

func registryImportRootTopologyEqual(
	left, right types.RegistryImportCandidateRoot,
) bool {
	if left.Name != right.Name ||
		left.DeliveryModel != right.DeliveryModel ||
		left.Classification != right.Classification ||
		left.DeploymentTargetID != right.DeploymentTargetID ||
		left.EnvironmentID != right.EnvironmentID ||
		left.PhysicalIdentity != right.PhysicalIdentity ||
		!registryImportOptionalUUIDEqual(
			left.CustomerOrganizationID, right.CustomerOrganizationID,
		) ||
		len(left.SubscriberCustomerOrganizationIDs) != len(right.SubscriberCustomerOrganizationIDs) {
		return false
	}
	for index := range left.SubscriberCustomerOrganizationIDs {
		if left.SubscriberCustomerOrganizationIDs[index] !=
			right.SubscriberCustomerOrganizationIDs[index] {
			return false
		}
	}
	return true
}

func registryImportOptionalUUIDEqual(left, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
