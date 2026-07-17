package externalexecutiontimestamp

import (
	"bytes"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

type canonicalCellDecision struct {
	key      rawCellKey
	checksum string
}

type manifestProblemAdder func(string, ...any)

type manifestCellValidationState struct {
	rawCells           []types.ExternalExecutionTimestampRawCell
	seenCells          map[rawCellKey]struct{}
	executionOrdinals  map[string]uint8
	eventOrdinals      map[string]uint8
	executionIDs       map[string]uuid.UUID
	eventIDs           map[string]uuid.UUID
	populatedCellCount uint64
}

func ComputeCellDecisionChecksum(
	cell types.ExternalExecutionTimestampCellDecision,
) (string, error) {
	if !checksumPattern.MatchString(cell.RawCellChecksum) {
		return "", fmt.Errorf("raw cell checksum must use lowercase sha256 format")
	}
	if cell.ConvertedValue != nil {
		if _, err := ParseInstant(*cell.ConvertedValue); err != nil {
			return "", fmt.Errorf("converted value: %w", err)
		}
	}

	var buffer bytes.Buffer
	writeField(&buffer, cellDecisionDomain)
	writeField(&buffer, cell.RawCellChecksum)
	writeField(&buffer, string(cell.Decision))
	writeOptionalString(&buffer, cell.SourceZone)
	writeOptionalInt32(&buffer, cell.SourceOffsetSeconds)
	writeOptionalPointerString(&buffer, cell.ConvertedValue)
	writeOptionalString(&buffer, cell.EvidenceReference)
	writeOptionalString(&buffer, cell.EvidenceChecksum)
	writeOptionalString(&buffer, cell.ApprovingIdentity)
	writeField(&buffer, cell.ConversionExpressionVersion)
	return checksum(buffer.Bytes()), nil
}

func ComputeDecisionContentChecksum(
	manifest types.ExternalExecutionTimestampManifest,
) (string, error) {
	if manifest.ID == uuid.Nil {
		return "", fmt.Errorf("manifest id cannot be nil UUID")
	}
	if !checksumPattern.MatchString(manifest.DatabaseIdentityChecksum) {
		return "", fmt.Errorf("database identity checksum must use lowercase sha256 format")
	}
	if !checksumPattern.MatchString(manifest.RawCellChecksum) {
		return "", fmt.Errorf("raw cell checksum must use lowercase sha256 format")
	}
	if _, err := ParseInstant(manifest.SnapshotStartedAt); err != nil {
		return "", fmt.Errorf("snapshot start: %w", err)
	}
	if _, err := ParseInstant(manifest.SnapshotEndedAt); err != nil {
		return "", fmt.Errorf("snapshot end: %w", err)
	}

	decisions := make([]canonicalCellDecision, 0, len(manifest.Cells))
	for index, cell := range manifest.Cells {
		key, err := rawKey(cell.ExternalExecutionTimestampRawCell)
		if err != nil {
			return "", fmt.Errorf("cell %d: %w", index, err)
		}
		cellChecksum, err := ComputeCellDecisionChecksum(cell)
		if err != nil {
			return "", fmt.Errorf("cell %d: %w", index, err)
		}
		decisions = append(decisions, canonicalCellDecision{key: key, checksum: cellChecksum})
	}
	slices.SortFunc(decisions, func(left, right canonicalCellDecision) int {
		if rawCellKeyLess(left.key, right.key) {
			return -1
		}
		if rawCellKeyLess(right.key, left.key) {
			return 1
		}
		return 0
	})

	var buffer bytes.Buffer
	writeField(&buffer, manifestDecisionDomain)
	writeField(&buffer, strings.ToLower(manifest.ID.String()))
	if err := writeOptionalUUID(&buffer, manifest.SupersedesManifestID); err != nil {
		return "", fmt.Errorf("supersedes manifest id: %w", err)
	}
	writeField(&buffer, manifest.DatabaseIdentityChecksum)
	writeField(&buffer, strconv.FormatUint(uint64(manifest.SourceSchemaVersion), 10))
	writeField(&buffer, manifest.SnapshotStartedAt)
	writeField(&buffer, manifest.SnapshotEndedAt)
	writeField(&buffer, strconv.FormatUint(manifest.ExecutionCount, 10))
	writeField(&buffer, strconv.FormatUint(manifest.EventCount, 10))
	writeField(&buffer, strconv.FormatUint(manifest.RawCellCount, 10))
	writeField(&buffer, strconv.FormatUint(manifest.PopulatedCellCount, 10))
	writeField(&buffer, manifest.RawCellChecksum)
	writeOptionalString(&buffer, manifest.EvidenceBundleReference)
	writeOptionalString(&buffer, manifest.EvidenceBundleChecksum)
	writeField(&buffer, manifest.ToolVersion)
	writeField(&buffer, manifest.ConversionExpressionVersion)
	writeOptionalString(&buffer, manifest.AuthorIdentity)
	writeOptionalString(&buffer, manifest.ReviewerIdentity)
	writeOptionalString(&buffer, manifest.TargetReleaseCommit)
	writeOptionalString(&buffer, manifest.TargetImageDigest)
	writeField(&buffer, strconv.Itoa(len(decisions)))
	for _, decision := range decisions {
		writeField(&buffer, decision.checksum)
	}
	return checksum(buffer.Bytes()), nil
}

func ValidateManifestDocument(manifest types.ExternalExecutionTimestampManifest) []error {
	problems := make([]error, 0)
	add := func(format string, arguments ...any) {
		problems = append(problems, fmt.Errorf(format, arguments...))
	}

	validateManifestHeader(manifest, add)
	validateManifestSnapshotInterval(manifest, add)
	actualCellCount := uint64(len(manifest.Cells))
	validateManifestRawCellCount(manifest, actualCellCount, add)
	cellState := validateManifestCells(manifest.Cells, add)
	validateManifestCellSet(manifest, actualCellCount, cellState, add)

	validateManifestApprovalMetadata(manifest, add)
	recomputedDecisionChecksum, err := ComputeDecisionContentChecksum(manifest)
	if err != nil {
		add("decision content checksum: %v", err)
	} else if manifest.DecisionContentChecksum != recomputedDecisionChecksum {
		add(
			"decision content checksum = %q, want %q",
			manifest.DecisionContentChecksum,
			recomputedDecisionChecksum,
		)
	}

	return problems
}

func validateManifestHeader(
	manifest types.ExternalExecutionTimestampManifest,
	add manifestProblemAdder,
) {
	if manifest.ID == uuid.Nil {
		add("manifest id cannot be nil UUID")
	}
	if manifest.SupersedesManifestID != nil {
		switch *manifest.SupersedesManifestID {
		case uuid.Nil:
			add("supersedes manifest id cannot be nil UUID")
		case manifest.ID:
			add("manifest cannot supersede itself")
		}
	}
	if manifest.SourceSchemaVersion != 137 {
		add("manifest source schema version must be 137")
	}
	if !checksumPattern.MatchString(manifest.DatabaseIdentityChecksum) {
		add("database identity checksum must use lowercase sha256 format")
	}
	if !checksumPattern.MatchString(manifest.RawCellChecksum) {
		add("raw cell checksum must use lowercase sha256 format")
	}
	if !checksumPattern.MatchString(manifest.DecisionContentChecksum) {
		add("decision content checksum must use lowercase sha256 format")
	}
	if strings.TrimSpace(manifest.ToolVersion) == "" {
		add("tool version is required")
	}
	if manifest.ConversionExpressionVersion != ConversionExpressionVersion {
		add("manifest conversion expression version must equal %q", ConversionExpressionVersion)
	}
}

func validateManifestSnapshotInterval(
	manifest types.ExternalExecutionTimestampManifest,
	add manifestProblemAdder,
) {
	snapshotStart, startErr := ParseInstant(manifest.SnapshotStartedAt)
	if startErr != nil {
		add("snapshot start: %v", startErr)
	}
	snapshotEnd, endErr := ParseInstant(manifest.SnapshotEndedAt)
	if endErr != nil {
		add("snapshot end: %v", endErr)
	}
	if startErr == nil && endErr == nil && snapshotEnd.Before(snapshotStart) {
		add("snapshot end cannot precede snapshot start")
	}
}

func validateManifestRawCellCount(
	manifest types.ExternalExecutionTimestampManifest,
	actualCellCount uint64,
	add manifestProblemAdder,
) {
	maximumUint64 := ^uint64(0)
	if manifest.ExecutionCount > (maximumUint64-manifest.EventCount)/5 {
		add("raw cell count formula overflows")
	} else {
		expectedRawCellCount := 5*manifest.ExecutionCount + manifest.EventCount
		if manifest.RawCellCount != expectedRawCellCount {
			add(
				"raw cell count = %d, want 5*%d+%d = %d",
				manifest.RawCellCount,
				manifest.ExecutionCount,
				manifest.EventCount,
				expectedRawCellCount,
			)
		}
	}
	if manifest.RawCellCount != actualCellCount {
		add(
			"raw cell count = %d, but document has %d cells",
			manifest.RawCellCount,
			actualCellCount,
		)
	}
}

func validateManifestCells(
	cells []types.ExternalExecutionTimestampCellDecision,
	add manifestProblemAdder,
) manifestCellValidationState {
	state := manifestCellValidationState{
		rawCells:          make([]types.ExternalExecutionTimestampRawCell, 0, len(cells)),
		seenCells:         make(map[rawCellKey]struct{}, len(cells)),
		executionOrdinals: make(map[string]uint8),
		eventOrdinals:     make(map[string]uint8),
		executionIDs:      make(map[string]uuid.UUID),
		eventIDs:          make(map[string]uuid.UUID),
	}
	for index, cell := range cells {
		validateManifestCell(index, cell, &state, add)
	}
	return state
}

func validateManifestCell(
	index int,
	cell types.ExternalExecutionTimestampCellDecision,
	state *manifestCellValidationState,
	add manifestProblemAdder,
) {
	raw := cell.ExternalExecutionTimestampRawCell
	state.rawCells = append(state.rawCells, raw)
	if raw.RawValue != nil {
		state.populatedCellCount++
	}

	key, err := rawKey(raw)
	if err != nil {
		add("cell %d: %v", index, err)
	} else {
		validateManifestCellKey(raw, key, state, add)
	}

	expectedRawChecksum, err := ComputeRawCellChecksum(raw)
	if err != nil {
		add("cell %d raw checksum: %v", index, err)
	} else if raw.RawCellChecksum != expectedRawChecksum {
		add(
			"cell %d raw cell checksum = %q, want %q",
			index,
			raw.RawCellChecksum,
			expectedRawChecksum,
		)
	}
	if !checksumPattern.MatchString(raw.RawCellChecksum) {
		add("cell %d raw cell checksum must use lowercase sha256 format", index)
	}
	if cell.ConversionExpressionVersion != ConversionExpressionVersion {
		add("cell %d conversion expression version must equal %q", index, ConversionExpressionVersion)
	}
	validateManifestCellDecision(index, cell, add)
}

func validateManifestCellKey(
	raw types.ExternalExecutionTimestampRawCell,
	key rawCellKey,
	state *manifestCellValidationState,
	add manifestProblemAdder,
) {
	if _, exists := state.seenCells[key]; exists {
		add("duplicate cell %s/%s/%d", key.table, key.rowID, key.ordinal)
	} else {
		state.seenCells[key] = struct{}{}
	}
	switch key.table {
	case "externalexecution":
		state.executionOrdinals[key.rowID] |= uint8(1 << key.ordinal)
		state.executionIDs[key.rowID] = raw.SourceRowID
	case "externalexecutionevent":
		state.eventOrdinals[key.rowID] |= uint8(1 << key.ordinal)
		state.eventIDs[key.rowID] = raw.SourceRowID
	}
}

func validateManifestCellDecision(
	index int,
	cell types.ExternalExecutionTimestampCellDecision,
	add manifestProblemAdder,
) {
	noConversion := cell.SourceZone == "" &&
		cell.SourceOffsetSeconds == nil &&
		cell.ConvertedValue == nil &&
		cell.EvidenceReference == "" &&
		cell.EvidenceChecksum == "" &&
		cell.ApprovingIdentity == ""
	switch cell.Decision {
	case types.ExternalExecutionTimestampDecisionNull:
		if cell.RawValue != nil || !noConversion {
			add("cell %d NULL_VALUE requires null raw value and no conversion evidence", index)
		}
	case types.ExternalExecutionTimestampDecisionUnresolved:
		if cell.RawValue == nil || !noConversion {
			add("cell %d UNRESOLVED requires a raw value and no conversion evidence", index)
		}
	case types.ExternalExecutionTimestampDecisionProven,
		types.ExternalExecutionTimestampDecisionAttested:
		validateResolvedManifestCell(index, cell, add)
	default:
		add("cell %d has unsupported decision %q", index, cell.Decision)
	}
}

func validateResolvedManifestCell(
	index int,
	cell types.ExternalExecutionTimestampCellDecision,
	add manifestProblemAdder,
) {
	if cell.RawValue == nil ||
		cell.SourceOffsetSeconds == nil ||
		cell.ConvertedValue == nil ||
		strings.TrimSpace(cell.EvidenceReference) == "" ||
		!checksumPattern.MatchString(cell.EvidenceChecksum) ||
		strings.TrimSpace(cell.ApprovingIdentity) == "" {
		add(
			"cell %d %s requires raw value, explicit offset, converted value, evidence, and approver",
			index,
			cell.Decision,
		)
		return
	}
	expected, err := ConvertWallClock(*cell.RawValue, *cell.SourceOffsetSeconds)
	if err != nil {
		add("cell %d conversion: %v", index, err)
		return
	}
	converted, err := ParseInstant(*cell.ConvertedValue)
	if err != nil {
		add("cell %d converted value: %v", index, err)
	} else if !converted.Equal(expected) {
		add(
			"cell %d converted value does not reproduce raw wall minus explicit offset",
			index,
		)
	}
}

func validateManifestCellSet(
	manifest types.ExternalExecutionTimestampManifest,
	actualCellCount uint64,
	state manifestCellValidationState,
	add manifestProblemAdder,
) {
	const expectedExecutionOrdinals = uint8(
		1<<1 | 1<<2 | 1<<3 | 1<<4 | 1<<5,
	)
	for rowID, ordinals := range state.executionOrdinals {
		if ordinals != expectedExecutionOrdinals {
			add("execution %s must contain exactly ordinals 1 through 5", rowID)
		}
	}
	for rowID, ordinals := range state.eventOrdinals {
		if ordinals != uint8(1<<6) {
			add("event %s must contain exactly ordinal 6", rowID)
		}
	}
	if uint64(len(state.executionIDs)) != manifest.ExecutionCount {
		add(
			"execution count = %d, but cells contain %d execution ids",
			manifest.ExecutionCount,
			len(state.executionIDs),
		)
	}
	if uint64(len(state.eventIDs)) != manifest.EventCount {
		add(
			"event count = %d, but cells contain %d event ids",
			manifest.EventCount,
			len(state.eventIDs),
		)
	}
	if state.populatedCellCount != manifest.PopulatedCellCount {
		add(
			"populated cell count = %d, but document contains %d populated cells",
			manifest.PopulatedCellCount,
			state.populatedCellCount,
		)
	}
	validateManifestSnapshotIdentity(manifest, actualCellCount, state, add)
}

func validateManifestSnapshotIdentity(
	manifest types.ExternalExecutionTimestampManifest,
	actualCellCount uint64,
	state manifestCellValidationState,
	add manifestProblemAdder,
) {
	recomputedRawSet, rawSetErr := ComputeRawSetChecksum(state.rawCells)
	if rawSetErr != nil {
		add("raw cell checksum: %v", rawSetErr)
		return
	}
	if manifest.RawCellChecksum != recomputedRawSet {
		add("raw cell checksum = %q, want %q", manifest.RawCellChecksum, recomputedRawSet)
	}
	canonicalExecutionIDs := make([]uuid.UUID, 0, len(state.executionIDs))
	for _, id := range state.executionIDs {
		canonicalExecutionIDs = append(canonicalExecutionIDs, id)
	}
	canonicalEventIDs := make([]uuid.UUID, 0, len(state.eventIDs))
	for _, id := range state.eventIDs {
		canonicalEventIDs = append(canonicalEventIDs, id)
	}
	recomputedIdentity, err := ComputeDatabaseIdentityChecksum(
		manifest.SourceSchemaVersion,
		canonicalExecutionIDs,
		canonicalEventIDs,
		actualCellCount,
		recomputedRawSet,
	)
	if err != nil {
		add("database identity checksum: %v", err)
	} else if manifest.DatabaseIdentityChecksum != recomputedIdentity {
		add(
			"database identity checksum = %q, want %q",
			manifest.DatabaseIdentityChecksum,
			recomputedIdentity,
		)
	}
}

func validateManifestApprovalMetadata(
	manifest types.ExternalExecutionTimestampManifest,
	add func(string, ...any),
) {
	baseFieldsEmpty := manifest.EvidenceBundleReference == "" &&
		manifest.EvidenceBundleChecksum == "" &&
		manifest.AuthorIdentity == "" &&
		manifest.ReviewerIdentity == "" &&
		manifest.TargetReleaseCommit == "" &&
		manifest.TargetImageDigest == ""
	allFieldsEmpty := baseFieldsEmpty && manifest.ApprovedAt == ""

	validateComplete := func(requireApprovedAt bool) {
		if strings.TrimSpace(manifest.EvidenceBundleReference) == "" {
			add("evidence bundle reference is required")
		}
		if !checksumPattern.MatchString(manifest.EvidenceBundleChecksum) {
			add("evidence bundle checksum must use lowercase sha256 format")
		}
		author := strings.TrimSpace(manifest.AuthorIdentity)
		reviewer := strings.TrimSpace(manifest.ReviewerIdentity)
		if author == "" {
			add("author identity is required")
		}
		if reviewer == "" {
			add("reviewer identity is required")
		}
		if author != "" && reviewer != "" && author == reviewer {
			add("author and reviewer identities must differ")
		}
		if !commitPattern.MatchString(manifest.TargetReleaseCommit) {
			add("target release commit must be 40 lowercase hexadecimal characters")
		}
		if !checksumPattern.MatchString(manifest.TargetImageDigest) {
			add("target image digest must use lowercase sha256 format")
		}
		if requireApprovedAt {
			if _, err := ParseInstant(manifest.ApprovedAt); err != nil {
				add("approved at: %v", err)
			}
		} else if manifest.ApprovedAt != "" {
			add("DRAFT manifest must not have approved at")
		}
	}

	switch manifest.State {
	case types.ExternalExecutionTimestampManifestStateDraft:
		if allFieldsEmpty {
			return
		}
		validateComplete(false)
	case types.ExternalExecutionTimestampManifestStateApproved,
		types.ExternalExecutionTimestampManifestStateApplied,
		types.ExternalExecutionTimestampManifestStateVerified:
		validateComplete(true)
	case types.ExternalExecutionTimestampManifestStateRevokedBeforeApply:
		if allFieldsEmpty {
			return
		}
		validateComplete(true)
	default:
		add("unsupported manifest state %q", manifest.State)
	}
}

func ValidateSupersession(
	previous types.ExternalExecutionTimestampManifest,
	next types.ExternalExecutionTimestampManifest,
) []error {
	problems := ValidateManifestDocument(next)
	add := func(format string, arguments ...any) {
		problems = append(problems, fmt.Errorf(format, arguments...))
	}

	validateSupersessionMetadata(previous, next, add)
	previousCells := indexPreviousManifestCells(previous.Cells, add)
	nextCells := indexNextManifestCells(next.Cells)
	validateSupersessionCells(previousCells, nextCells, add)

	return problems
}

func validateSupersessionMetadata(
	previous types.ExternalExecutionTimestampManifest,
	next types.ExternalExecutionTimestampManifest,
	add manifestProblemAdder,
) {
	if next.ID == previous.ID {
		add("superseding manifest must have a new id")
	}
	if next.SupersedesManifestID == nil || *next.SupersedesManifestID != previous.ID {
		add("next manifest must supersede previous manifest %s", previous.ID)
	}
	if next.SourceSchemaVersion != previous.SourceSchemaVersion {
		add("superseding manifest must preserve original source schema version")
	}
	if next.SnapshotStartedAt != previous.SnapshotStartedAt ||
		next.SnapshotEndedAt != previous.SnapshotEndedAt {
		add("superseding manifest must preserve original snapshot interval")
	}
	if next.ExecutionCount != previous.ExecutionCount ||
		next.EventCount != previous.EventCount ||
		next.RawCellCount != previous.RawCellCount ||
		next.PopulatedCellCount != previous.PopulatedCellCount {
		add("superseding manifest must preserve original snapshot counts")
	}
	if next.DatabaseIdentityChecksum != previous.DatabaseIdentityChecksum {
		add("superseding manifest must preserve original database identity checksum")
	}
	if next.RawCellChecksum != previous.RawCellChecksum {
		add("superseding manifest must preserve original raw-set checksum")
	}
}

func indexPreviousManifestCells(
	cells []types.ExternalExecutionTimestampCellDecision,
	add manifestProblemAdder,
) map[rawCellKey]types.ExternalExecutionTimestampCellDecision {
	indexed := make(map[rawCellKey]types.ExternalExecutionTimestampCellDecision, len(cells))
	for index, cell := range cells {
		key, err := rawKey(cell.ExternalExecutionTimestampRawCell)
		if err != nil {
			add("previous cell %d: %v", index, err)
			continue
		}
		if _, exists := indexed[key]; exists {
			add(
				"previous manifest contains duplicate cell %s/%s/%d",
				key.table,
				key.rowID,
				key.ordinal,
			)
			continue
		}
		indexed[key] = cell
	}
	return indexed
}

func indexNextManifestCells(
	cells []types.ExternalExecutionTimestampCellDecision,
) map[rawCellKey]types.ExternalExecutionTimestampCellDecision {
	indexed := make(map[rawCellKey]types.ExternalExecutionTimestampCellDecision, len(cells))
	for _, cell := range cells {
		key, err := rawKey(cell.ExternalExecutionTimestampRawCell)
		if err == nil {
			indexed[key] = cell
		}
	}
	return indexed
}

func validateSupersessionCells(
	previousCells map[rawCellKey]types.ExternalExecutionTimestampCellDecision,
	nextCells map[rawCellKey]types.ExternalExecutionTimestampCellDecision,
	add manifestProblemAdder,
) {
	for key := range nextCells {
		if _, exists := previousCells[key]; !exists {
			add(
				"superseding manifest contains unexpected cell %s/%s/%d",
				key.table,
				key.rowID,
				key.ordinal,
			)
		}
	}
	for key, previousCell := range previousCells {
		nextCell, exists := nextCells[key]
		if !exists {
			add(
				"superseding manifest is missing prior cell %s/%s/%d",
				key.table,
				key.rowID,
				key.ordinal,
			)
			continue
		}
		validateSupersededCell(key, previousCell, nextCell, add)
	}
}

func validateSupersededCell(
	key rawCellKey,
	previous types.ExternalExecutionTimestampCellDecision,
	next types.ExternalExecutionTimestampCellDecision,
	add manifestProblemAdder,
) {
	if !equalOptionalString(previous.RawValue, next.RawValue) ||
		previous.RawCellChecksum != next.RawCellChecksum {
		add(
			"cell %s/%s/%d raw value and checksum must remain unchanged",
			key.table,
			key.rowID,
			key.ordinal,
		)
	}
	switch previous.Decision {
	case types.ExternalExecutionTimestampDecisionProven,
		types.ExternalExecutionTimestampDecisionAttested:
		validateSupersededResolvedCell(key, previous, next, add)
	case types.ExternalExecutionTimestampDecisionUnresolved:
		validateSupersededUnresolvedCell(key, next, add)
	case types.ExternalExecutionTimestampDecisionNull:
		if next.Decision != types.ExternalExecutionTimestampDecisionNull {
			add("NULL_VALUE decision cannot change for %s/%s/%d", key.table, key.rowID, key.ordinal)
		}
	}
}

func validateSupersededResolvedCell(
	key rawCellKey,
	previous types.ExternalExecutionTimestampCellDecision,
	next types.ExternalExecutionTimestampCellDecision,
	add manifestProblemAdder,
) {
	if next.Decision != previous.Decision {
		add("resolved decision cannot change for %s/%s/%d", key.table, key.rowID, key.ordinal)
	}
	if next.SourceZone != previous.SourceZone ||
		!equalOptionalInt32(next.SourceOffsetSeconds, previous.SourceOffsetSeconds) ||
		next.EvidenceReference != previous.EvidenceReference ||
		next.EvidenceChecksum != previous.EvidenceChecksum ||
		next.ApprovingIdentity != previous.ApprovingIdentity ||
		next.ConversionExpressionVersion != previous.ConversionExpressionVersion {
		add("resolved cell evidence cannot change for %s/%s/%d", key.table, key.rowID, key.ordinal)
	}
	if next.ConvertedValue == nil || previous.ConvertedValue == nil ||
		*next.ConvertedValue != *previous.ConvertedValue {
		add("resolved instant cannot change for %s/%s/%d", key.table, key.rowID, key.ordinal)
	}
}

func validateSupersededUnresolvedCell(
	key rawCellKey,
	next types.ExternalExecutionTimestampCellDecision,
	add manifestProblemAdder,
) {
	switch next.Decision {
	case types.ExternalExecutionTimestampDecisionUnresolved,
		types.ExternalExecutionTimestampDecisionProven,
		types.ExternalExecutionTimestampDecisionAttested:
		return
	default:
		add(
			"UNRESOLVED decision may change only to PROVEN or ATTESTED for %s/%s/%d",
			key.table,
			key.rowID,
			key.ordinal,
		)
	}
}

func equalOptionalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func equalOptionalInt32(left, right *int32) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
