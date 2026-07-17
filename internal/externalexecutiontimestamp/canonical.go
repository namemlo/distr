package externalexecutiontimestamp

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

const (
	RawWallLayout               = "2006-01-02T15:04:05.000000"
	InstantLayout               = "2006-01-02T15:04:05.000000Z"
	ConversionExpressionVersion = "external-execution-offset/v1"

	rawCellDomain          = "distr.external-execution-timestamp/raw-cell/v1"
	rawSetDomain           = "distr.external-execution-timestamp/raw-set/v1"
	databaseIdentityDomain = "distr.external-execution-timestamp/database-identity/v1"
	cellDecisionDomain     = "distr.external-execution-timestamp/cell-decision/v1"
	manifestDecisionDomain = "distr.external-execution-timestamp/manifest-decision/v1"
)

var (
	checksumPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	cellOrdinals    = map[string]map[string]uint8{
		"externalexecution": {
			"created_at":           1,
			"updated_at":           2,
			"started_at":           3,
			"completed_at":         4,
			"callback_deadline_at": 5,
		},
		"externalexecutionevent": {"created_at": 6},
	}
)

type rawCellKey struct {
	table   string
	rowID   string
	ordinal uint8
}

type canonicalRawCell struct {
	key      rawCellKey
	checksum string
}

func writeField(buffer *bytes.Buffer, value string) {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(value)))
	buffer.Write(length[:])
	buffer.WriteString(value)
}

func writeOptionalString(buffer *bytes.Buffer, value string) {
	if value == "" {
		writeField(buffer, "NULL")
		return
	}
	writeField(buffer, "VALUE")
	writeField(buffer, value)
}

func writeOptionalUUID(buffer *bytes.Buffer, value *uuid.UUID) error {
	if value == nil {
		writeField(buffer, "NULL")
		return nil
	}
	if *value == uuid.Nil {
		return fmt.Errorf("optional UUID cannot be nil UUID")
	}
	writeField(buffer, "VALUE")
	writeField(buffer, strings.ToLower(value.String()))
	return nil
}

func writeOptionalInt32(buffer *bytes.Buffer, value *int32) {
	if value == nil {
		writeField(buffer, "NULL")
		return
	}
	writeField(buffer, "VALUE")
	writeField(buffer, strconv.FormatInt(int64(*value), 10))
}

func writeOptionalPointerString(buffer *bytes.Buffer, value *string) {
	if value == nil {
		writeField(buffer, "NULL")
		return
	}
	writeField(buffer, "VALUE")
	writeField(buffer, *value)
}

func checksum(data []byte) string {
	digest := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func rawKey(cell types.ExternalExecutionTimestampRawCell) (rawCellKey, error) {
	table := strings.ToLower(cell.SourceTable)
	column := strings.ToLower(cell.SourceColumn)
	columns, ok := cellOrdinals[table]
	if !ok {
		return rawCellKey{}, fmt.Errorf("raw cell table %q is not in allowlist", cell.SourceTable)
	}
	expectedOrdinal, ok := columns[column]
	if !ok || expectedOrdinal != cell.ColumnOrdinal {
		return rawCellKey{}, fmt.Errorf(
			"raw cell %s.%s ordinal %d is not in allowlist",
			cell.SourceTable,
			cell.SourceColumn,
			cell.ColumnOrdinal,
		)
	}
	if cell.SourceRowID == uuid.Nil {
		return rawCellKey{}, fmt.Errorf("raw cell source row id cannot be nil UUID")
	}
	if cell.RawValue != nil {
		wall, err := time.Parse(RawWallLayout, *cell.RawValue)
		if err != nil || wall.Format(RawWallLayout) != *cell.RawValue {
			return rawCellKey{}, fmt.Errorf("raw wall value must use %s", RawWallLayout)
		}
	}
	return rawCellKey{
		table:   table,
		rowID:   strings.ToLower(cell.SourceRowID.String()),
		ordinal: cell.ColumnOrdinal,
	}, nil
}

func rawCellKeyLess(left, right rawCellKey) bool {
	if left.table != right.table {
		return left.table < right.table
	}
	if left.rowID != right.rowID {
		return left.rowID < right.rowID
	}
	return left.ordinal < right.ordinal
}

func CanonicalRawCell(cell types.ExternalExecutionTimestampRawCell) ([]byte, error) {
	key, err := rawKey(cell)
	if err != nil {
		return nil, err
	}

	var buffer bytes.Buffer
	writeField(&buffer, rawCellDomain)
	writeField(&buffer, key.table)
	writeField(&buffer, key.rowID)
	writeField(&buffer, strings.ToLower(cell.SourceColumn))
	writeField(&buffer, strconv.FormatUint(uint64(cell.ColumnOrdinal), 10))
	if cell.RawValue == nil {
		writeField(&buffer, "NULL")
	} else {
		writeField(&buffer, "VALUE")
		writeField(&buffer, *cell.RawValue)
	}
	return buffer.Bytes(), nil
}

func ComputeRawCellChecksum(cell types.ExternalExecutionTimestampRawCell) (string, error) {
	canonical, err := CanonicalRawCell(cell)
	if err != nil {
		return "", err
	}
	return checksum(canonical), nil
}

func ComputeRawSetChecksum(cells []types.ExternalExecutionTimestampRawCell) (string, error) {
	canonicalCells := make([]canonicalRawCell, 0, len(cells))
	for index, cell := range cells {
		key, err := rawKey(cell)
		if err != nil {
			return "", fmt.Errorf("raw cell %d: %w", index, err)
		}
		cellChecksum, err := ComputeRawCellChecksum(cell)
		if err != nil {
			return "", fmt.Errorf("raw cell %d: %w", index, err)
		}
		canonicalCells = append(canonicalCells, canonicalRawCell{
			key: key, checksum: cellChecksum,
		})
	}
	slices.SortFunc(canonicalCells, func(left, right canonicalRawCell) int {
		if rawCellKeyLess(left.key, right.key) {
			return -1
		}
		if rawCellKeyLess(right.key, left.key) {
			return 1
		}
		return 0
	})
	for index := 1; index < len(canonicalCells); index++ {
		if canonicalCells[index-1].key == canonicalCells[index].key {
			return "", fmt.Errorf(
				"duplicate raw cell %s/%s/%d",
				canonicalCells[index].key.table,
				canonicalCells[index].key.rowID,
				canonicalCells[index].key.ordinal,
			)
		}
	}

	var buffer bytes.Buffer
	writeField(&buffer, rawSetDomain)
	writeField(&buffer, strconv.Itoa(len(canonicalCells)))
	for _, cell := range canonicalCells {
		writeField(&buffer, cell.checksum)
	}
	return checksum(buffer.Bytes()), nil
}

func canonicalIDs(kind string, ids []uuid.UUID) ([]uuid.UUID, error) {
	canonical := slices.Clone(ids)
	for _, id := range canonical {
		if id == uuid.Nil {
			return nil, fmt.Errorf("nil %s id", kind)
		}
	}
	slices.SortFunc(canonical, func(left, right uuid.UUID) int {
		return bytes.Compare(left[:], right[:])
	})
	for index := 1; index < len(canonical); index++ {
		if canonical[index-1] == canonical[index] {
			return nil, fmt.Errorf("duplicate %s id %s", kind, canonical[index])
		}
	}
	return canonical, nil
}

func ComputeDatabaseIdentityChecksum(
	sourceVersion uint,
	executionIDs []uuid.UUID,
	eventIDs []uuid.UUID,
	rawCellCount uint64,
	rawSetChecksum string,
) (string, error) {
	if !checksumPattern.MatchString(rawSetChecksum) {
		return "", fmt.Errorf("raw set checksum must use lowercase sha256 format")
	}
	canonicalExecutionIDs, err := canonicalIDs("execution", executionIDs)
	if err != nil {
		return "", err
	}
	canonicalEventIDs, err := canonicalIDs("event", eventIDs)
	if err != nil {
		return "", err
	}

	var buffer bytes.Buffer
	writeField(&buffer, databaseIdentityDomain)
	writeField(&buffer, strconv.FormatUint(uint64(sourceVersion), 10))
	writeField(&buffer, strconv.Itoa(len(canonicalExecutionIDs)))
	for _, id := range canonicalExecutionIDs {
		writeField(&buffer, strings.ToLower(id.String()))
	}
	writeField(&buffer, strconv.Itoa(len(canonicalEventIDs)))
	for _, id := range canonicalEventIDs {
		writeField(&buffer, strings.ToLower(id.String()))
	}
	writeField(&buffer, strconv.FormatUint(rawCellCount, 10))
	writeField(&buffer, rawSetChecksum)
	return checksum(buffer.Bytes()), nil
}

func ParseInstant(value string) (time.Time, error) {
	instant, err := time.Parse(InstantLayout, value)
	if err != nil || instant.Format(InstantLayout) != value {
		return time.Time{}, fmt.Errorf("instant must use %s", InstantLayout)
	}
	return instant.UTC(), nil
}

func FormatInstant(value time.Time) string {
	return value.UTC().Format(InstantLayout)
}

func ConvertWallClock(raw string, offsetSeconds int32) (time.Time, error) {
	if offsetSeconds < -64800 || offsetSeconds > 64800 {
		return time.Time{}, fmt.Errorf("offset seconds must be between -64800 and 64800")
	}
	wall, err := time.Parse(RawWallLayout, raw)
	if err != nil || wall.Format(RawWallLayout) != raw {
		return time.Time{}, fmt.Errorf("raw wall value must use %s", RawWallLayout)
	}
	return wall.Add(-time.Duration(offsetSeconds) * time.Second).UTC(), nil
}
