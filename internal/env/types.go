package env

import (
	"fmt"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
)

type RegistrationMode string

const (
	RegistrationEnabled  RegistrationMode = "enabled"
	RegistrationHidden   RegistrationMode = "hidden"
	RegistrationDisabled RegistrationMode = "disabled"
)

func parseRegistrationMode(value string) (RegistrationMode, error) {
	switch value {
	case string(RegistrationEnabled), string(RegistrationHidden), string(RegistrationDisabled):
		return RegistrationMode(value), nil
	default:
		return "", fmt.Errorf("invalid RegistrationMode: %v", value)
	}
}

type StripeWebhookVersionMismatchBehaviorType string

const (
	StripeWebhookVersionMismatchBehaviorIgnore StripeWebhookVersionMismatchBehaviorType = "ignore"
	StripeWebhookVersionMismatchBehaviorError  StripeWebhookVersionMismatchBehaviorType = "error"
)

func parseStripeWebhookVersionMismatchBehavior(value string) (StripeWebhookVersionMismatchBehaviorType, error) {
	switch value {
	case string(StripeWebhookVersionMismatchBehaviorIgnore),
		string(StripeWebhookVersionMismatchBehaviorError):
		return StripeWebhookVersionMismatchBehaviorType(value), nil
	default:
		return "", fmt.Errorf("invalid StripeWebhookVersionMismatchBehavior: %v", value)
	}
}

type MailerTypeString string

const (
	MailerTypeSMTP        MailerTypeString = "smtp"
	MailerTypeSES         MailerTypeString = "ses"
	MailerTypeUnspecified MailerTypeString = ""
)

func parseMailerType(value string) (MailerTypeString, error) {
	switch value {
	case string(MailerTypeSES), string(MailerTypeSMTP), string(MailerTypeUnspecified):
		return MailerTypeString(value), nil
	default:
		return "", fmt.Errorf("invalid MailerTypeString: %v", value)
	}
}

type MailerConfig struct {
	Type        MailerTypeString
	FromAddress mail.Address
	SmtpConfig  *MailerSMTPConfig
}

type MailerSMTPConfig struct {
	Host        string
	Port        int
	Username    string
	Password    string
	ImplicitTLS bool
}

type S3Config struct {
	Bucket                                 string
	Region                                 string
	Endpoint                               *string
	AccessKeyID                            *string
	SecretAccessKey                        *string
	UsePathStyle                           bool
	AllowRedirect                          bool
	CreateBucket                           bool
	RequestChecksumCalculationWhenRequired bool
	ResponseChecksumValidationWhenRequired bool
	ResignForGCP                           bool
}

type TargetConfigObjectStoreConfig struct {
	Enabled bool
	S3      S3Config
}

var (
	targetConfigRegionPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	targetConfigBucketPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
	targetConfigCredentialPattern = regexp.MustCompile(`^[A-Za-z0-9._~+/=-]+$`)
)

func (config TargetConfigObjectStoreConfig) Configured() bool {
	if !config.Enabled ||
		!validTargetConfigRegion(config.S3.Region) ||
		!validTargetConfigEndpoint(config.S3.Endpoint) ||
		!validTargetConfigBucket(config.S3.Bucket) ||
		!validTargetConfigCredentials(config.S3.AccessKeyID, config.S3.SecretAccessKey) {
		return false
	}
	return true
}

func validTargetConfigRegion(value string) bool {
	return value == strings.TrimSpace(value) &&
		!targetConfigPlaceholder(value) &&
		targetConfigRegionPattern.MatchString(value)
}

func validTargetConfigEndpoint(value *string) bool {
	if value == nil || *value == "" {
		return true
	}
	if *value != strings.TrimSpace(*value) || targetConfigPlaceholder(*value) {
		return false
	}
	parsed, err := url.Parse(*value)
	return err == nil &&
		(parsed.Scheme == "http" || parsed.Scheme == "https") &&
		parsed.Host != "" &&
		parsed.User == nil &&
		parsed.RawQuery == "" &&
		parsed.Fragment == ""
}

func validTargetConfigCredentials(accessKey, secretKey *string) bool {
	accessConfigured := accessKey != nil && *accessKey != ""
	secretConfigured := secretKey != nil && *secretKey != ""
	if accessConfigured != secretConfigured {
		return false
	}
	if !accessConfigured {
		return true
	}
	return validTargetConfigCredential(accessKey, 3, 256) &&
		validTargetConfigCredential(secretKey, 8, 512)
}

func validTargetConfigBucket(value string) bool {
	return value == strings.TrimSpace(value) &&
		!targetConfigPlaceholder(value) &&
		!strings.Contains(value, "..") &&
		targetConfigBucketPattern.MatchString(value)
}

func validTargetConfigCredential(value *string, minLength, maxLength int) bool {
	if value == nil ||
		*value != strings.TrimSpace(*value) ||
		targetConfigPlaceholder(*value) ||
		len(*value) < minLength ||
		len(*value) > maxLength {
		return false
	}
	return targetConfigCredentialPattern.MatchString(*value)
}

func targetConfigPlaceholder(value string) bool {
	return strings.Contains(strings.ToUpper(value), "CHANGE_ME")
}

type SamplerType string

const (
	SamplerAlwaysOn                SamplerType = "always_on"
	SamplerAlwaysOff               SamplerType = "always_off"
	SamplerTraceIDRatio            SamplerType = "traceidratio"
	SamplerParentBasedAlwaysOn     SamplerType = "parentbased_always_on"
	SamplerParsedBasedAlwaysOff    SamplerType = "parentbased_always_off"
	SamplerParentBasedTraceIDRatio SamplerType = "parentbased_traceidratio"
)

func parseSamplerType(value string) (SamplerType, error) {
	switch value {
	case string(SamplerAlwaysOn),
		string(SamplerAlwaysOff),
		string(SamplerTraceIDRatio),
		string(SamplerParentBasedAlwaysOn),
		string(SamplerParsedBasedAlwaysOff),
		string(SamplerParentBasedTraceIDRatio):

		return SamplerType(value), nil
	default:
		return "", fmt.Errorf("invalid SamplerType: %v", value)
	}
}

type SamplerConfig struct {
	Sampler SamplerType
	Arg     float64
}
