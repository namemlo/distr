package migrations

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/distr-sh/distr/internal/types"
)

const timestampRecoveryCatalogChecksumDomain = "distr.external-execution-timestamp/dirty-recovery-catalog/v1"

var timestampRecoveryCatalogIdentifierPattern = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

func ClassifyTimestampDirtyRecovery(
	status SchemaStatus,
	catalogShape types.TimestampRecoveryCatalogShape,
) (uint, error) {
	if status.Version < 0 {
		return 0, errors.New("timestamp dirty recovery marker is malformed or absent")
	}
	if !status.Dirty {
		return 0, errors.New("timestamp dirty recovery requires a dirty schema marker")
	}
	if status.Version != 137 && status.Version != 138 {
		return 0, errors.New("timestamp dirty recovery marker version must be 137 or 138")
	}
	switch catalogShape {
	case types.TimestampRecoveryCatalogShapePredecessor137:
		return 137, nil
	case types.TimestampRecoveryCatalogShapeExpand138:
		return 138, nil
	default:
		return 0, fmt.Errorf(
			"timestamp dirty recovery catalog shape %q is not an exact supported shape",
			catalogShape,
		)
	}
}

func ComputeTimestampRecoveryCatalogChecksum(
	records []types.TimestampRecoveryCatalogRecord,
) (string, error) {
	if len(records) == 0 {
		return "", errors.New("timestamp recovery catalog must contain at least one record")
	}
	canonical := slices.Clone(records)
	for index, record := range canonical {
		if err := validateTimestampRecoveryCatalogRecord(record); err != nil {
			return "", fmt.Errorf("timestamp recovery catalog record %d: %w", index, err)
		}
	}
	slices.SortFunc(canonical, func(left, right types.TimestampRecoveryCatalogRecord) int {
		for _, comparison := range []int{
			strings.Compare(string(left.Category), string(right.Category)),
			strings.Compare(left.RelationName, right.RelationName),
			strings.Compare(left.ObjectName, right.ObjectName),
			strings.Compare(left.Definition, right.Definition),
		} {
			if comparison != 0 {
				return comparison
			}
		}
		return 0
	})
	for index := 1; index < len(canonical); index++ {
		previous, current := canonical[index-1], canonical[index]
		if previous.Category == current.Category &&
			previous.RelationName == current.RelationName &&
			previous.ObjectName == current.ObjectName {
			return "", fmt.Errorf(
				"duplicate timestamp recovery catalog identity %s/%s/%s",
				current.Category,
				current.RelationName,
				current.ObjectName,
			)
		}
	}

	var buffer bytes.Buffer
	if err := writeTimestampRecoveryCatalogField(
		&buffer,
		timestampRecoveryCatalogChecksumDomain,
	); err != nil {
		return "", err
	}
	if err := writeTimestampRecoveryCatalogField(
		&buffer,
		strconv.Itoa(len(canonical)),
	); err != nil {
		return "", err
	}
	for _, record := range canonical {
		for _, value := range []string{
			string(record.Category),
			record.RelationName,
			record.ObjectName,
			record.Definition,
		} {
			if err := writeTimestampRecoveryCatalogField(&buffer, value); err != nil {
				return "", err
			}
		}
	}
	digest := sha256.Sum256(buffer.Bytes())
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func validateTimestampRecoveryCatalogRecord(
	record types.TimestampRecoveryCatalogRecord,
) error {
	switch record.Category {
	case types.TimestampRecoveryCatalogCategoryRelation,
		types.TimestampRecoveryCatalogCategoryFunction:
		if record.RelationName != "" {
			return fmt.Errorf("%s record relation name must be empty", record.Category)
		}
	case types.TimestampRecoveryCatalogCategoryColumn,
		types.TimestampRecoveryCatalogCategoryConstraint,
		types.TimestampRecoveryCatalogCategoryIndex,
		types.TimestampRecoveryCatalogCategoryTrigger:
		if !timestampRecoveryCatalogIdentifierPattern.MatchString(record.RelationName) {
			return fmt.Errorf(
				"%s record relation name must be a safe PostgreSQL identifier",
				record.Category,
			)
		}
	default:
		return fmt.Errorf("unsupported catalog category %q", record.Category)
	}
	if !timestampRecoveryCatalogIdentifierPattern.MatchString(record.ObjectName) {
		return fmt.Errorf(
			"%s record object name must be a safe PostgreSQL identifier",
			record.Category,
		)
	}
	if record.Definition == "" ||
		len(record.Definition) > 1<<20 ||
		!utf8.ValidString(record.Definition) ||
		strings.ContainsRune(record.Definition, '\x00') {
		return fmt.Errorf(
			"%s record definition must be non-empty valid UTF-8 without NUL and at most 1 MiB",
			record.Category,
		)
	}
	return nil
}

func writeTimestampRecoveryCatalogField(buffer *bytes.Buffer, value string) error {
	if uint64(len(value)) > math.MaxUint32 {
		return errors.New("timestamp recovery catalog field exceeds framing limit")
	}
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(value)))
	buffer.Write(length[:])
	buffer.WriteString(value)
	return nil
}
