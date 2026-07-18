package svc

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/distr-sh/distr/internal/env"
	"github.com/distr-sh/distr/internal/targetconfig"
	"go.opentelemetry.io/contrib/instrumentation/github.com/aws/aws-sdk-go-v2/otelaws"
)

func newS3Client(ctx context.Context) *s3.Client {
	return newS3ClientWithConfig(ctx, env.RegistryS3Config())
}

func newS3ClientWithConfig(ctx context.Context, s3Config env.S3Config) *s3.Client {
	opts := []func(*s3.Options){s3ClientOptions(s3Config)}
	if s3Config.ResignForGCP {
		opts = append(opts, resignForGCP)
	}

	if config, err := awsconfig.LoadDefaultConfig(ctx); err != nil {
		return s3.New(s3.Options{}, opts...)
	} else {
		otelaws.AppendMiddlewares(&config.APIOptions)
		return s3.NewFromConfig(config, opts...)
	}
}

func newTargetConfigObjectVerifier(ctx context.Context) targetconfig.ObjectVerifier {
	return newTargetConfigObjectVerifierWithConfig(ctx, env.TargetConfigObjectStore())
}

func newTargetConfigObjectVerifierWithConfig(
	ctx context.Context,
	config env.TargetConfigObjectStoreConfig,
) targetconfig.ObjectVerifier {
	if !config.Configured() {
		return targetconfig.NewUnavailableObjectVerifier()
	}
	return targetconfig.NewS3ObjectVerifierForBucket(
		newS3ClientWithConfig(ctx, config.S3),
		config.S3.Bucket,
	)
}

func s3ClientOptions(s3Config env.S3Config) func(o *s3.Options) {
	return func(o *s3.Options) {
		o.Region = s3Config.Region
		if s3Config.Endpoint != nil && *s3Config.Endpoint != "" {
			o.BaseEndpoint = s3Config.Endpoint
		}
		o.UsePathStyle = s3Config.UsePathStyle
		if s3Config.RequestChecksumCalculationWhenRequired {
			o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		}
		if s3Config.ResponseChecksumValidationWhenRequired {
			o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
		}
		if s3Config.AccessKeyID != nil && *s3Config.AccessKeyID != "" &&
			s3Config.SecretAccessKey != nil && *s3Config.SecretAccessKey != "" {
			o.Credentials = aws.NewCredentialsCache(
				credentials.NewStaticCredentialsProvider(*s3Config.AccessKeyID, *s3Config.SecretAccessKey, ""),
			)
		}
	}
}
