package protectedhistory

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

const (
	SchemaV1 = "distr.protected-history/v1"

	recordDomain   = "distr.protected-history/record/v1"
	recordsDomain  = "distr.protected-history/record-set/v1"
	artifactDomain = "distr.protected-history/artifact/v1"
)

var (
	checksumPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	allowedKinds    = map[string]struct{}{
		"application":                                 {},
		"applicationversion":                          {},
		"approvaldecision":                            {},
		"approvalrequest":                             {},
		"baselineadoptioncomponent":                   {},
		"customerorganization":                        {},
		"deployment":                                  {},
		"deploymentlogrecord":                         {},
		"deploymentplan":                              {},
		"deploymentplanissue":                         {},
		"deploymentplanresolvedrequirement":           {},
		"deploymentplanstep":                          {},
		"deploymentplantarget":                        {},
		"deploymentplantargetcomponent":               {},
		"deploymentplanvariable":                      {},
		"deploymentpreflightcheck":                    {},
		"deploymentpreflightrun":                      {},
		"deploymentrevision":                          {},
		"deploymentrevisionstatus":                    {},
		"deploymenttarget":                            {},
		"deploymenttargetlogrecord":                   {},
		"deploymenttargetstatus":                      {},
		"executionattempt":                            {},
		"executionevent":                              {},
		"executionintent":                             {},
		"executionruntimeevidence":                    {},
		"externalexecution":                           {},
		"externalexecutionevent":                      {},
		"externalexecutiontimestampcellprovenance":    {},
		"externalexecutiontimestampcontractgate":      {},
		"externalexecutiontimestampdeletiontombstone": {},
		"externalexecutiontimestampexpandstate":       {},
		"externalexecutiontimestampmanifest":          {},
		"processsnapshot":                             {},
		"releasebundle":                               {},
		"releasebundleauditevent":                     {},
		"releasebundlecomponent":                      {},
		"releasebundleidempotencykey":                 {},
		"sampleretirementitem":                        {},
		"sampleretirementjob":                         {},
		"sampleretirementownershipevidence":           {},
		"steprun":                                     {},
		"steprunevent":                                {},
		"steprunlogchunk":                             {},
		"steprunoutput":                               {},
		"targetcomponentobservation":                  {},
		"targetcomponentstate":                        {},
		"task":                                        {},
		"tasklease":                                   {},
		"taskresourcelock":                            {},
		"variablesnapshot":                            {},
		"variablesnapshotvalue":                       {},
	}
)

type Scope struct {
	OrganizationID          string   `json:"organizationId"`
	CustomerOrganizationIDs []string `json:"customerOrganizationIds"`
	DeploymentTargetIDs     []string `json:"deploymentTargetIds"`
}

type RawRecord struct {
	Kind    string
	ID      string
	Payload json.RawMessage
}

type Record struct {
	Kind    string          `json:"kind"`
	ID      string          `json:"id"`
	Payload json.RawMessage `json:"payload"`
	Hash    string          `json:"hash"`
}

type KindCount struct {
	Kind  string `json:"kind"`
	Count uint64 `json:"count"`
}

type Artifact struct {
	Schema              string      `json:"schema"`
	SourceSchemaVersion uint64      `json:"sourceSchemaVersion"`
	Scope               Scope       `json:"scope"`
	Counts              []KindCount `json:"counts"`
	RecordCount         uint64      `json:"recordCount"`
	Records             []Record    `json:"records"`
	RecordsRoot         string      `json:"recordsRoot"`
	ArtifactID          string      `json:"artifactId"`
}

func AllowedKinds() []string {
	kinds := make([]string, 0, len(allowedKinds))
	for kind := range allowedKinds {
		kinds = append(kinds, kind)
	}
	slices.Sort(kinds)
	return kinds
}

func CanonicalScope(scope Scope) (Scope, error) {
	organizationID, err := canonicalUUID("organization", scope.OrganizationID)
	if err != nil {
		return Scope{}, err
	}
	customerOrganizationIDs, err := canonicalUUIDs(
		"customer organization", scope.CustomerOrganizationIDs,
	)
	if err != nil {
		return Scope{}, err
	}
	deploymentTargetIDs, err := canonicalUUIDs("deployment target", scope.DeploymentTargetIDs)
	if err != nil {
		return Scope{}, err
	}
	if len(customerOrganizationIDs) == 0 && len(deploymentTargetIDs) == 0 {
		return Scope{}, errors.New("scope requires at least one customer organization or deployment target")
	}
	return Scope{
		OrganizationID:          organizationID,
		CustomerOrganizationIDs: customerOrganizationIDs,
		DeploymentTargetIDs:     deploymentTargetIDs,
	}, nil
}

func Build(scope Scope, sourceSchemaVersion uint64, rawRecords []RawRecord) (*Artifact, error) {
	if sourceSchemaVersion < 138 {
		return nil, fmt.Errorf("source schema version %d is unsupported; minimum is 138", sourceSchemaVersion)
	}
	canonicalScope, err := CanonicalScope(scope)
	if err != nil {
		return nil, fmt.Errorf("canonicalize scope: %w", err)
	}
	records := make([]Record, 0, len(rawRecords))
	for index, rawRecord := range rawRecords {
		record, err := buildRecord(rawRecord)
		if err != nil {
			return nil, fmt.Errorf("record %d: %w", index, err)
		}
		records = append(records, record)
	}
	slices.SortFunc(records, compareRecordKey)
	if err := rejectDuplicateRecords(records); err != nil {
		return nil, err
	}
	counts := countKinds(records)
	recordsRoot := computeRecordsRoot(records)
	artifact := &Artifact{
		Schema:              SchemaV1,
		SourceSchemaVersion: sourceSchemaVersion,
		Scope:               canonicalScope,
		Counts:              counts,
		RecordCount:         uint64(len(records)),
		Records:             records,
		RecordsRoot:         recordsRoot,
	}
	artifact.ArtifactID = computeArtifactID(*artifact)
	return artifact, nil
}

func Marshal(artifact Artifact) ([]byte, error) {
	if err := Validate(artifact); err != nil {
		return nil, err
	}
	payload, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal protected history artifact: %w", err)
	}
	return append(payload, '\n'), nil
}

func Parse(payload []byte) (*Artifact, error) {
	if _, err := canonicalJSON(payload, true); err != nil {
		return nil, fmt.Errorf("parse protected history artifact: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	var artifact Artifact
	if err := decoder.Decode(&artifact); err != nil {
		return nil, fmt.Errorf("parse protected history artifact: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return nil, fmt.Errorf("parse protected history artifact: %w", err)
	}
	if err := Validate(artifact); err != nil {
		return nil, err
	}
	return &artifact, nil
}

func Validate(artifact Artifact) error {
	if artifact.Schema != SchemaV1 {
		return fmt.Errorf("unsupported protected history schema %q", artifact.Schema)
	}
	if artifact.SourceSchemaVersion < 138 {
		return fmt.Errorf(
			"source schema version %d is unsupported; minimum is 138",
			artifact.SourceSchemaVersion,
		)
	}
	canonicalScope, err := CanonicalScope(artifact.Scope)
	if err != nil {
		return fmt.Errorf("validate scope: %w", err)
	}
	if !scopeEqual(canonicalScope, artifact.Scope) {
		return errors.New("scope is not in canonical order")
	}
	if artifact.RecordCount != uint64(len(artifact.Records)) {
		return fmt.Errorf(
			"record count %d does not match %d records",
			artifact.RecordCount,
			len(artifact.Records),
		)
	}
	for index := range artifact.Records {
		record := artifact.Records[index]
		canonicalRecord, err := buildRecord(RawRecord{
			Kind: record.Kind, ID: record.ID, Payload: record.Payload,
		})
		if err != nil {
			return fmt.Errorf("validate record %d: %w", index, err)
		}
		if canonicalRecord.Hash != record.Hash {
			return fmt.Errorf("record %s/%s hash mismatch", record.Kind, record.ID)
		}
		if index > 0 && compareRecordKey(artifact.Records[index-1], record) >= 0 {
			return fmt.Errorf("records are not in strict canonical order at %s/%s", record.Kind, record.ID)
		}
	}
	if err := rejectDuplicateRecords(artifact.Records); err != nil {
		return err
	}
	expectedCounts := countKinds(artifact.Records)
	if !slices.Equal(expectedCounts, artifact.Counts) {
		return errors.New("record counts do not match records")
	}
	expectedRoot := computeRecordsRoot(artifact.Records)
	if artifact.RecordsRoot != expectedRoot {
		return errors.New("records root mismatch")
	}
	if !checksumPattern.MatchString(artifact.ArtifactID) {
		return errors.New("artifact id must use lowercase sha256 format")
	}
	if artifact.ArtifactID != computeArtifactID(artifact) {
		return errors.New("artifact id mismatch")
	}
	return nil
}

func buildRecord(rawRecord RawRecord) (Record, error) {
	kind := strings.ToLower(strings.TrimSpace(rawRecord.Kind))
	if _, ok := allowedKinds[kind]; !ok {
		return Record{}, fmt.Errorf("record kind %q is not in the protected-history allowlist", rawRecord.Kind)
	}
	id, err := canonicalUUID(kind, rawRecord.ID)
	if err != nil {
		return Record{}, err
	}
	payload, err := canonicalJSON(rawRecord.Payload, true)
	if err != nil {
		return Record{}, fmt.Errorf("%s/%s payload: %w", kind, id, err)
	}
	var buffer bytes.Buffer
	writeField(&buffer, recordDomain)
	writeField(&buffer, kind)
	writeField(&buffer, id)
	writeFieldBytes(&buffer, payload)
	return Record{
		Kind: kind, ID: id, Payload: payload, Hash: checksum(buffer.Bytes()),
	}, nil
}

func canonicalUUID(kind, value string) (string, error) {
	id, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil || id == uuid.Nil {
		return "", fmt.Errorf("%s id %q is not a nonzero UUID", kind, value)
	}
	return strings.ToLower(id.String()), nil
}

func canonicalUUIDs(kind string, values []string) ([]string, error) {
	canonical := make([]string, 0, len(values))
	for _, value := range values {
		id, err := canonicalUUID(kind, value)
		if err != nil {
			return nil, err
		}
		canonical = append(canonical, id)
	}
	slices.Sort(canonical)
	for index := 1; index < len(canonical); index++ {
		if canonical[index-1] == canonical[index] {
			return nil, fmt.Errorf("duplicate %s id %s", kind, canonical[index])
		}
	}
	return canonical, nil
}

func compareRecordKey(left, right Record) int {
	if comparison := strings.Compare(left.Kind, right.Kind); comparison != 0 {
		return comparison
	}
	return strings.Compare(left.ID, right.ID)
}

func rejectDuplicateRecords(records []Record) error {
	for index := 1; index < len(records); index++ {
		if records[index-1].Kind == records[index].Kind && records[index-1].ID == records[index].ID {
			return fmt.Errorf("duplicate protected record %s/%s", records[index].Kind, records[index].ID)
		}
	}
	return nil
}

func countKinds(records []Record) []KindCount {
	counts := make([]KindCount, 0)
	for _, record := range records {
		if len(counts) == 0 || counts[len(counts)-1].Kind != record.Kind {
			counts = append(counts, KindCount{Kind: record.Kind, Count: 1})
		} else {
			counts[len(counts)-1].Count++
		}
	}
	return counts
}

func computeRecordsRoot(records []Record) string {
	var buffer bytes.Buffer
	writeField(&buffer, recordsDomain)
	writeField(&buffer, strconv.Itoa(len(records)))
	for _, record := range records {
		writeField(&buffer, record.Kind)
		writeField(&buffer, record.ID)
		writeField(&buffer, record.Hash)
	}
	return checksum(buffer.Bytes())
}

func computeArtifactID(artifact Artifact) string {
	var buffer bytes.Buffer
	writeField(&buffer, artifactDomain)
	writeField(&buffer, artifact.Schema)
	writeField(&buffer, strconv.FormatUint(artifact.SourceSchemaVersion, 10))
	writeField(&buffer, artifact.Scope.OrganizationID)
	writeStringSet(&buffer, artifact.Scope.CustomerOrganizationIDs)
	writeStringSet(&buffer, artifact.Scope.DeploymentTargetIDs)
	writeField(&buffer, strconv.FormatUint(artifact.RecordCount, 10))
	for _, count := range artifact.Counts {
		writeField(&buffer, count.Kind)
		writeField(&buffer, strconv.FormatUint(count.Count, 10))
	}
	writeField(&buffer, artifact.RecordsRoot)
	return checksum(buffer.Bytes())
}

func scopeEqual(left, right Scope) bool {
	return left.OrganizationID == right.OrganizationID &&
		slices.Equal(left.CustomerOrganizationIDs, right.CustomerOrganizationIDs) &&
		slices.Equal(left.DeploymentTargetIDs, right.DeploymentTargetIDs)
}

func writeStringSet(buffer *bytes.Buffer, values []string) {
	writeField(buffer, strconv.Itoa(len(values)))
	for _, value := range values {
		writeField(buffer, value)
	}
}

func writeField(buffer *bytes.Buffer, value string) {
	writeFieldBytes(buffer, []byte(value))
}

func writeFieldBytes(buffer *bytes.Buffer, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	buffer.Write(length[:])
	buffer.Write(value)
}

func checksum(payload []byte) string {
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func canonicalJSON(payload []byte, requireObject bool) ([]byte, error) {
	if len(payload) == 0 {
		return nil, errors.New("JSON is empty")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	value, err := decodeJSONValue(decoder)
	if err != nil {
		return nil, err
	}
	if err := requireJSONEOF(decoder); err != nil {
		return nil, err
	}
	if requireObject {
		if _, ok := value.(map[string]any); !ok {
			return nil, errors.New("JSON value must be an object")
		}
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal canonical JSON: %w", err)
	}
	return canonical, nil
}

func decodeJSONValue(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return token, nil
	}
	switch delimiter {
	case '{':
		object := map[string]any{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, errors.New("JSON object key must be a string")
			}
			if _, duplicate := object[key]; duplicate {
				return nil, fmt.Errorf("duplicate JSON object key %q", key)
			}
			value, err := decodeJSONValue(decoder)
			if err != nil {
				return nil, err
			}
			object[key] = value
		}
		if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
			return nil, errors.New("unterminated JSON object")
		}
		return object, nil
	case '[':
		array := make([]any, 0)
		for decoder.More() {
			value, err := decodeJSONValue(decoder)
			if err != nil {
				return nil, err
			}
			array = append(array, value)
		}
		if token, err = decoder.Token(); err != nil || token != json.Delim(']') {
			return nil, errors.New("unterminated JSON array")
		}
		return array, nil
	default:
		return nil, fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
}

func requireJSONEOF(decoder *json.Decoder) error {
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("JSON contains trailing content")
		}
		return err
	}
	return nil
}
