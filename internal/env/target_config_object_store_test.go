package env

import (
	"testing"

	. "github.com/onsi/gomega"
)

func TestLoadTargetConfigObjectStoreConfigIsIndependentFromRegistry(t *testing.T) {
	t.Setenv("REGISTRY_ENABLED", "false")
	t.Setenv("TARGET_CONFIG_OBJECT_STORE_ENABLED", "true")
	t.Setenv("TARGET_CONFIG_S3_REGION", "ap-southeast-1")
	t.Setenv("TARGET_CONFIG_S3_ENDPOINT", "https://objects.example.invalid")
	t.Setenv("TARGET_CONFIG_S3_BUCKET", "target-config")
	t.Setenv("TARGET_CONFIG_S3_ACCESS_KEY_ID", "access-key")
	t.Setenv("TARGET_CONFIG_S3_SECRET_ACCESS_KEY", "generated-secret")
	t.Setenv("TARGET_CONFIG_S3_USE_PATH_STYLE", "true")

	config := loadTargetConfigObjectStoreConfig()

	g := NewWithT(t)
	g.Expect(config.Configured()).To(BeTrue())
	g.Expect(config.S3.Region).To(Equal("ap-southeast-1"))
	g.Expect(config.S3.Endpoint).NotTo(BeNil())
	g.Expect(*config.S3.Endpoint).To(Equal("https://objects.example.invalid"))
	g.Expect(config.S3.Bucket).To(Equal("target-config"))
	g.Expect(config.S3.UsePathStyle).To(BeTrue())
}

func TestTargetConfigObjectStoreConfigTreatsMissingOrPartialCredentialsAsUnavailable(t *testing.T) {
	g := NewWithT(t)
	t.Setenv("TARGET_CONFIG_OBJECT_STORE_ENABLED", "true")
	t.Setenv("TARGET_CONFIG_S3_REGION", "")

	missingRegion := loadTargetConfigObjectStoreConfig()
	g.Expect(missingRegion.Configured()).To(BeFalse())

	t.Setenv("TARGET_CONFIG_S3_REGION", "ap-southeast-1")
	t.Setenv("TARGET_CONFIG_S3_ENDPOINT", "https://objects.example.invalid")
	t.Setenv("TARGET_CONFIG_S3_BUCKET", "target-config")
	t.Setenv("TARGET_CONFIG_S3_ACCESS_KEY_ID", "configured-without-secret")
	t.Setenv("TARGET_CONFIG_S3_SECRET_ACCESS_KEY", "")

	partialCredentials := loadTargetConfigObjectStoreConfig()
	g.Expect(partialCredentials.Configured()).To(BeFalse())
}

func TestTargetConfigObjectStoreConfigSupportsAWSDefaultsAndIAMRole(t *testing.T) {
	g := NewWithT(t)
	t.Setenv("TARGET_CONFIG_OBJECT_STORE_ENABLED", "true")
	t.Setenv("TARGET_CONFIG_S3_REGION", "ap-southeast-1")
	t.Setenv("TARGET_CONFIG_S3_BUCKET", "target-config")
	t.Setenv("TARGET_CONFIG_S3_ENDPOINT", "")
	t.Setenv("TARGET_CONFIG_S3_ACCESS_KEY_ID", "")
	t.Setenv("TARGET_CONFIG_S3_SECRET_ACCESS_KEY", "")

	config := loadTargetConfigObjectStoreConfig()

	g.Expect(config.Configured()).To(BeTrue())
	g.Expect(config.S3.Endpoint).To(BeNil())
	g.Expect(config.S3.AccessKeyID).To(BeNil())
	g.Expect(config.S3.SecretAccessKey).To(BeNil())
}

func TestTargetConfigObjectStoreConfigRejectsInvalidOptionalConfiguration(t *testing.T) {
	endpoint := "https://objects.example.invalid"
	invalidEndpoint := "https://user:password@objects.example.invalid"
	accessKey := "access-key"
	secretKey := "generated-secret"
	empty := ""

	tests := []struct {
		name      string
		endpoint  *string
		accessKey *string
		secretKey *string
		valid     bool
	}{
		{name: "AWS defaults and IAM role", valid: true},
		{name: "custom endpoint and static pair", endpoint: &endpoint, accessKey: &accessKey, secretKey: &secretKey, valid: true},
		{name: "invalid endpoint", endpoint: &invalidEndpoint},
		{name: "access key only", accessKey: &accessKey},
		{name: "secret key only", secretKey: &secretKey},
		{name: "empty optional values", endpoint: &empty, accessKey: &empty, secretKey: &empty, valid: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			config := TargetConfigObjectStoreConfig{
				Enabled: true,
				S3: S3Config{
					Region:          "ap-southeast-1",
					Bucket:          "target-config",
					Endpoint:        tt.endpoint,
					AccessKeyID:     tt.accessKey,
					SecretAccessKey: tt.secretKey,
				},
			}

			g.Expect(config.Configured()).To(Equal(tt.valid))
		})
	}
}

func TestTargetConfigObjectStoreConfigRejectsPlaceholderOrIncompleteEndpointAndBucket(t *testing.T) {
	g := NewWithT(t)
	t.Setenv("TARGET_CONFIG_OBJECT_STORE_ENABLED", "true")
	t.Setenv("TARGET_CONFIG_S3_REGION", "ap-southeast-1")
	t.Setenv("TARGET_CONFIG_S3_ENDPOINT", "https://objects.example.invalid")
	t.Setenv("TARGET_CONFIG_S3_BUCKET", "target-config")
	t.Setenv("TARGET_CONFIG_S3_ACCESS_KEY_ID", "access-key")
	t.Setenv("TARGET_CONFIG_S3_SECRET_ACCESS_KEY", "generated-secret")

	for _, key := range []string{
		"TARGET_CONFIG_S3_REGION",
		"TARGET_CONFIG_S3_ENDPOINT",
		"TARGET_CONFIG_S3_BUCKET",
		"TARGET_CONFIG_S3_ACCESS_KEY_ID",
		"TARGET_CONFIG_S3_SECRET_ACCESS_KEY",
	} {
		t.Run(key, func(t *testing.T) {
			g := NewWithT(t)
			t.Setenv(key, "CHANGE_ME_UNSAFE")
			g.Expect(loadTargetConfigObjectStoreConfig().Configured()).To(BeFalse())
		})
	}

	t.Setenv("TARGET_CONFIG_S3_ENDPOINT", "")
	g.Expect(loadTargetConfigObjectStoreConfig().Configured()).To(BeTrue())
	t.Setenv("TARGET_CONFIG_S3_ENDPOINT", "https://objects.example.invalid")
	t.Setenv("TARGET_CONFIG_S3_BUCKET", "")
	g.Expect(loadTargetConfigObjectStoreConfig().Configured()).To(BeFalse())
}
