package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/google/uuid"
)

type migrateRecoveryEvidenceReservation interface {
	Publish(any) (string, error)
	Cancel() error
	FailClosed() error
}

type migrateRecoveryEvidenceFileReservation struct {
	file      *os.File
	tempPath  string
	finalPath string
	directory string
}

type migrateRecoveryEvidenceDurabilityHooks struct {
	SyncFile      func(*os.File) error
	SyncDirectory func(string) error
}

func migrateRecoveryEvidenceTempPath(
	finalPath string,
	recoveryID uuid.UUID,
) string {
	directory := filepath.Dir(finalPath)
	base := filepath.Base(finalPath)
	return filepath.Join(
		directory,
		"."+base+"."+recoveryID.String()+".tmp",
	)
}

func reserveMigrateRecoveryEvidence(
	finalPath string,
	recoveryID uuid.UUID,
) (migrateRecoveryEvidenceReservation, error) {
	return reserveMigrateRecoveryEvidenceWithDurability(
		finalPath,
		recoveryID,
		migrateRecoveryEvidenceDurabilityHooks{
			SyncFile: func(file *os.File) error {
				return file.Sync()
			},
			SyncDirectory: syncMigrateRecoveryEvidenceDirectory,
		},
	)
}

func reserveMigrateRecoveryEvidenceWithDurability(
	finalPath string,
	recoveryID uuid.UUID,
	durability migrateRecoveryEvidenceDurabilityHooks,
) (migrateRecoveryEvidenceReservation, error) {
	if recoveryID == uuid.Nil {
		return nil, errors.New("recovery id is required for evidence reservation")
	}
	if finalPath == "" {
		return nil, errors.New("recovery evidence output path is required")
	}
	if err := requireMigrateRecoveryEvidencePathAbsent(finalPath); err != nil {
		return nil, err
	}
	tempPath := migrateRecoveryEvidenceTempPath(finalPath, recoveryID)
	file, err := os.OpenFile(
		tempPath,
		os.O_CREATE|os.O_EXCL|os.O_WRONLY,
		0o600,
	)
	if err != nil {
		return nil, fmt.Errorf("reserve recovery evidence: %w", err)
	}
	reservation := &migrateRecoveryEvidenceFileReservation{
		file:      file,
		tempPath:  tempPath,
		finalPath: finalPath,
		directory: filepath.Dir(finalPath),
	}
	if err := file.Chmod(0o600); err != nil {
		return nil, errors.Join(
			fmt.Errorf("set recovery evidence reservation mode: %w", err),
			reservation.Cancel(),
		)
	}
	if durability.SyncFile == nil || durability.SyncDirectory == nil {
		return nil, errors.Join(
			errors.New("recovery evidence durability hooks are required"),
			reservation.Cancel(),
		)
	}
	if err := durability.SyncFile(file); err != nil {
		return nil, errors.Join(
			fmt.Errorf("sync recovery evidence reservation: %w", err),
			reservation.Cancel(),
		)
	}
	if err := durability.SyncDirectory(reservation.directory); err != nil {
		return nil, errors.Join(err, reservation.Cancel())
	}
	// Close the final-name race as far as possible before the mutation
	// boundary. os.Link below remains the authoritative no-replace operation.
	if err := requireMigrateRecoveryEvidencePathAbsent(finalPath); err != nil {
		return nil, errors.Join(err, reservation.Cancel())
	}
	return reservation, nil
}

func requireMigrateRecoveryEvidencePathAbsent(path string) error {
	_, err := os.Lstat(path)
	switch {
	case err == nil:
		return fmt.Errorf("recovery evidence output already exists: %w", os.ErrExist)
	case errors.Is(err, os.ErrNotExist):
		return nil
	default:
		return fmt.Errorf("inspect recovery evidence output: %w", err)
	}
}

func (reservation *migrateRecoveryEvidenceFileReservation) Publish(
	value any,
) (string, error) {
	if reservation == nil || reservation.file == nil {
		return "", errors.New("recovery evidence reservation is not open")
	}
	data, checksum, err := encodeMigrateRecoveryEvidence(value)
	if err != nil {
		_ = reservation.closeFailClosed()
		return "", err
	}
	if err := writeMigrateRecoveryEvidenceFile(reservation.file, data); err != nil {
		return "", errors.Join(err, reservation.closeFailClosed())
	}
	if err := reservation.file.Sync(); err != nil {
		return "", errors.Join(
			fmt.Errorf("sync recovery evidence reservation: %w", err),
			reservation.closeFailClosed(),
		)
	}
	if err := reservation.file.Close(); err != nil {
		reservation.file = nil
		return "", fmt.Errorf("close recovery evidence reservation: %w", err)
	}
	reservation.file = nil
	if err := os.Link(reservation.tempPath, reservation.finalPath); err != nil {
		return "", fmt.Errorf("publish recovery evidence without replacement: %w", err)
	}
	if err := syncMigrateRecoveryEvidenceDirectory(reservation.directory); err != nil {
		return "", err
	}
	if err := os.Remove(reservation.tempPath); err != nil {
		return "", fmt.Errorf("remove recovery evidence reservation: %w", err)
	}
	if err := syncMigrateRecoveryEvidenceDirectory(reservation.directory); err != nil {
		return "", err
	}
	return checksum, nil
}

func (reservation *migrateRecoveryEvidenceFileReservation) Cancel() error {
	if reservation == nil {
		return nil
	}
	closeErr := reservation.closeFailClosed()
	removeErr := os.Remove(reservation.tempPath)
	if errors.Is(removeErr, os.ErrNotExist) {
		removeErr = nil
	}
	if removeErr != nil {
		removeErr = fmt.Errorf(
			"remove recovery evidence reservation: %w",
			removeErr,
		)
	}
	var syncErr error
	if removeErr == nil {
		syncErr = syncMigrateRecoveryEvidenceDirectory(reservation.directory)
	}
	return errors.Join(closeErr, removeErr, syncErr)
}

func (reservation *migrateRecoveryEvidenceFileReservation) FailClosed() error {
	return reservation.closeFailClosed()
}

func (reservation *migrateRecoveryEvidenceFileReservation) closeFailClosed() error {
	if reservation == nil || reservation.file == nil {
		return nil
	}
	err := reservation.file.Close()
	reservation.file = nil
	if err != nil {
		return fmt.Errorf("close recovery evidence reservation: %w", err)
	}
	return nil
}

func encodeMigrateRecoveryEvidence(value any) ([]byte, string, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, "", errors.New("encode recovery evidence JSON failed")
	}
	data = append(data, '\n')
	sum := sha256.Sum256(data)
	return data, "sha256:" + hex.EncodeToString(sum[:]), nil
}

func writeMigrateRecoveryEvidenceFile(file *os.File, data []byte) error {
	for len(data) > 0 {
		written, err := file.Write(data)
		if err != nil {
			return fmt.Errorf("write recovery evidence reservation: %w", err)
		}
		if written == 0 {
			return errors.New("write recovery evidence reservation made no progress")
		}
		data = data[written:]
	}
	return nil
}

func syncMigrateRecoveryEvidenceDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open recovery evidence directory: %w", err)
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	// Windows does not expose durable directory fsync through os.File.Sync.
	// The call is still attempted; Linux production treats any failure as fatal.
	if runtime.GOOS == "windows" {
		syncErr = nil
	}
	if syncErr != nil {
		syncErr = fmt.Errorf("sync recovery evidence directory: %w", syncErr)
	}
	if closeErr != nil {
		closeErr = fmt.Errorf("close recovery evidence directory: %w", closeErr)
	}
	return errors.Join(syncErr, closeErr)
}
