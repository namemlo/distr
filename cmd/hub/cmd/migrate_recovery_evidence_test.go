package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestPublishMigrateRecoveryEvidenceAtomicNoOverwriteAnd0600(t *testing.T) {
	g := NewWithT(t)
	directory := t.TempDir()
	output := filepath.Join(directory, "recovery-result.json")
	recoveryID := uuid.MustParse("11111111-2222-4333-8444-555555555555")
	value := struct {
		Result string `json:"result"`
	}{Result: "SUCCEEDED"}
	payload := migrateRecoverDirtyTestJSON(t, value)
	reservation, err := reserveMigrateRecoveryEvidence(output, recoveryID)
	g.Expect(err).NotTo(HaveOccurred())

	checksum, err := reservation.Publish(value)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(checksum).To(Equal(migrateRecoverDirtyTestChecksum(payload)))
	actual, err := os.ReadFile(output)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(actual).To(Equal(payload))
	info, err := os.Stat(output)
	g.Expect(err).NotTo(HaveOccurred())
	if runtime.GOOS != "windows" {
		g.Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o600)))
	}
	_, err = os.Lstat(migrateRecoveryEvidenceTempPath(output, recoveryID))
	g.Expect(errors.Is(err, os.ErrNotExist)).To(BeTrue())

	second, err := reserveMigrateRecoveryEvidence(output, recoveryID)
	g.Expect(errors.Is(err, os.ErrExist)).To(BeTrue())
	g.Expect(second).To(BeNil())
	actual, err = os.ReadFile(output)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(actual).To(Equal(payload))
}

func TestMigrateRecoveryEvidenceTempCollisionIsFailClosed(t *testing.T) {
	g := NewWithT(t)
	directory := t.TempDir()
	output := filepath.Join(directory, "recovery-result.json")
	recoveryID := uuid.MustParse("11111111-2222-4333-8444-555555555555")
	tempPath := migrateRecoveryEvidenceTempPath(output, recoveryID)
	g.Expect(os.WriteFile(tempPath, []byte("RESERVED"), 0o600)).To(Succeed())

	reservation, err := reserveMigrateRecoveryEvidence(output, recoveryID)

	g.Expect(errors.Is(err, os.ErrExist)).To(BeTrue())
	g.Expect(reservation).To(BeNil())
	actual, readErr := os.ReadFile(tempPath)
	g.Expect(readErr).NotTo(HaveOccurred())
	g.Expect(actual).To(Equal([]byte("RESERVED")))
}

func TestMigrateRecoveryEvidenceCancelRemovesOnlyReservation(t *testing.T) {
	g := NewWithT(t)
	directory := t.TempDir()
	output := filepath.Join(directory, "recovery-plan.json")
	recoveryID := uuid.MustParse("11111111-2222-4333-8444-555555555555")
	reservation, err := reserveMigrateRecoveryEvidence(output, recoveryID)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(reservation.Cancel()).To(Succeed())

	_, err = os.Lstat(output)
	g.Expect(errors.Is(err, os.ErrNotExist)).To(BeTrue())
	_, err = os.Lstat(migrateRecoveryEvidenceTempPath(output, recoveryID))
	g.Expect(errors.Is(err, os.ErrNotExist)).To(BeTrue())
}

func TestReserveMigrateRecoveryEvidenceSyncsFileThenDirectoryBeforeReturn(
	t *testing.T,
) {
	g := NewWithT(t)
	directory := t.TempDir()
	output := filepath.Join(directory, "recovery-result.json")
	recoveryID := uuid.MustParse("11111111-2222-4333-8444-555555555555")
	tempPath := migrateRecoveryEvidenceTempPath(output, recoveryID)
	var events []string

	reservation, err := reserveMigrateRecoveryEvidenceWithDurability(
		output,
		recoveryID,
		migrateRecoveryEvidenceDurabilityHooks{
			SyncFile: func(file *os.File) error {
				events = append(events, "file-sync")
				g.Expect(file.Name()).To(Equal(tempPath))
				_, statErr := os.Stat(tempPath)
				g.Expect(statErr).NotTo(HaveOccurred())
				return nil
			},
			SyncDirectory: func(path string) error {
				events = append(events, "directory-sync")
				g.Expect(path).To(Equal(directory))
				return nil
			},
		},
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(events).To(Equal([]string{"file-sync", "directory-sync"}))
	g.Expect(reservation.Cancel()).To(Succeed())
}

func TestReserveMigrateRecoveryEvidenceSyncFailureCancelsReservation(t *testing.T) {
	g := NewWithT(t)
	directory := t.TempDir()
	output := filepath.Join(directory, "recovery-result.json")
	recoveryID := uuid.MustParse("11111111-2222-4333-8444-555555555555")
	var directorySyncCalls uint64

	reservation, err := reserveMigrateRecoveryEvidenceWithDurability(
		output,
		recoveryID,
		migrateRecoveryEvidenceDurabilityHooks{
			SyncFile: func(*os.File) error {
				return errors.New("injected file sync failure")
			},
			SyncDirectory: func(string) error {
				directorySyncCalls++
				return nil
			},
		},
	)

	g.Expect(err).To(MatchError(ContainSubstring(
		"injected file sync failure",
	)))
	g.Expect(reservation).To(BeNil())
	g.Expect(directorySyncCalls).To(BeZero())
	_, err = os.Lstat(output)
	g.Expect(errors.Is(err, os.ErrNotExist)).To(BeTrue())
	_, err = os.Lstat(migrateRecoveryEvidenceTempPath(output, recoveryID))
	g.Expect(errors.Is(err, os.ErrNotExist)).To(BeTrue())
}
