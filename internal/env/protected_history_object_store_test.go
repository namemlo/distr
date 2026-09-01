package env

import (
	"testing"

	. "github.com/onsi/gomega"
)

func TestLoadProtectedHistoryObjectStoreConfigIsDedicated(t *testing.T) {
	t.Setenv("PROTECTED_HISTORY_OBJECT_STORE_ENABLED", "true")
	t.Setenv("PROTECTED_HISTORY_S3_REGION", "ap-southeast-1")
	t.Setenv("PROTECTED_HISTORY_S3_ENDPOINT", "https://history.example.com")
	t.Setenv("PROTECTED_HISTORY_S3_BUCKET", "protected-history")
	t.Setenv("PROTECTED_HISTORY_S3_ACCESS_KEY_ID", "history-access")
	t.Setenv("PROTECTED_HISTORY_S3_SECRET_ACCESS_KEY", "history-secret")
	t.Setenv("PROTECTED_HISTORY_S3_USE_PATH_STYLE", "true")
	t.Setenv("REGISTRY_S3_BUCKET", "registry")
	t.Setenv("TARGET_CONFIG_S3_BUCKET", "target-config")

	config := loadProtectedHistoryObjectStoreConfig()

	g := NewWithT(t)
	g.Expect(config.Configured()).To(BeTrue())
	g.Expect(config.S3.Region).To(Equal("ap-southeast-1"))
	g.Expect(config.S3.Bucket).To(Equal("protected-history"))
	g.Expect(config.S3.Endpoint).NotTo(BeNil())
	g.Expect(*config.S3.Endpoint).To(Equal("https://history.example.com"))
	g.Expect(config.S3.AccessKeyID).NotTo(BeNil())
	g.Expect(*config.S3.AccessKeyID).To(Equal("history-access"))
	g.Expect(config.S3.SecretAccessKey).NotTo(BeNil())
	g.Expect(*config.S3.SecretAccessKey).To(Equal("history-secret"))
	g.Expect(config.S3.UsePathStyle).To(BeTrue())
}

func TestProtectedHistoryObjectStoreConfigFailsClosed(t *testing.T) {
	t.Setenv("PROTECTED_HISTORY_OBJECT_STORE_ENABLED", "true")
	t.Setenv("PROTECTED_HISTORY_S3_REGION", "ap-southeast-1")
	t.Setenv("PROTECTED_HISTORY_S3_BUCKET", "protected-history")
	t.Setenv("PROTECTED_HISTORY_S3_ACCESS_KEY_ID", "history-access")

	g := NewWithT(t)
	g.Expect(loadProtectedHistoryObjectStoreConfig().Configured()).To(BeFalse())

	t.Setenv("PROTECTED_HISTORY_S3_SECRET_ACCESS_KEY", "history-secret")
	g.Expect(loadProtectedHistoryObjectStoreConfig().Configured()).To(BeTrue())

	t.Setenv("PROTECTED_HISTORY_S3_BUCKET", "${BUCKET}")
	g.Expect(loadProtectedHistoryObjectStoreConfig().Configured()).To(BeFalse())
}
