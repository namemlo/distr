package webhookaction

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestWebhookTransportFailureMatrix(t *testing.T) {
	t.Run("retryable 5xx exhausts the frozen attempt budget", func(t *testing.T) {
		g := NewWithT(t)
		var deliveries atomic.Int32
		var idempotencyKeys []string
		var keysMu sync.Mutex
		client := &http.Client{Transport: webhookFailureRoundTripper(func(request *http.Request) (*http.Response, error) {
			deliveries.Add(1)
			keysMu.Lock()
			idempotencyKeys = append(idempotencyKeys, request.Header.Get("Idempotency-Key"))
			keysMu.Unlock()
			return webhookFailureResponse(http.StatusServiceUnavailable, `{"retry":true}`), nil
		})}
		input := webhookFailureMatrixInput(t, 3)

		result, err := Run(
			context.Background(), input, func(string) error { return nil },
			webhookFailureMatrixRuntimeOptions(client),
		)

		g.Expect(err).To(MatchError("webhook returned unexpected status 503"))
		g.Expect(result.StatusCode).To(Equal(http.StatusServiceUnavailable))
		g.Expect(result.Attempts).To(Equal(3))
		g.Expect(deliveries.Load()).To(Equal(int32(3)))
		g.Expect(idempotencyKeys).To(Equal([]string{
			input.IdempotencyKey, input.IdempotencyKey, input.IdempotencyKey,
		}))
		g.Expect(webhookRetryReasons(result.AuditTrail)).To(Equal([]string{
			"retryable status 503", "retryable status 503",
		}))
	})

	t.Run("transient transport timeout exhausts without exceeding the budget", func(t *testing.T) {
		g := NewWithT(t)
		var deliveries atomic.Int32
		client := &http.Client{Transport: webhookFailureRoundTripper(func(*http.Request) (*http.Response, error) {
			deliveries.Add(1)
			return nil, webhookRetryableTimeoutError{}
		})}
		input := webhookFailureMatrixInput(t, 2)

		result, err := Run(
			context.Background(), input, func(string) error { return nil },
			webhookFailureMatrixRuntimeOptions(client),
		)

		g.Expect(err).To(MatchError(ContainSubstring("webhook transport timeout")))
		g.Expect(result.Attempts).To(Equal(2))
		g.Expect(deliveries.Load()).To(Equal(int32(2)))
		g.Expect(webhookRetryReasons(result.AuditTrail)).To(Equal([]string{
			"retryable transport error",
		}))
	})

	t.Run("caller timeout stops retries immediately", func(t *testing.T) {
		g := NewWithT(t)
		var deliveries atomic.Int32
		client := &http.Client{Transport: webhookFailureRoundTripper(func(request *http.Request) (*http.Response, error) {
			deliveries.Add(1)
			<-request.Context().Done()
			return nil, request.Context().Err()
		})}
		input := webhookFailureMatrixInput(t, 5)
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
		defer cancel()

		result, err := Run(
			ctx, input, func(string) error { return nil },
			webhookFailureMatrixRuntimeOptions(client),
		)

		g.Expect(err).To(MatchError("webhook timed out"))
		g.Expect(result.Attempts).To(Equal(1))
		g.Expect(deliveries.Load()).To(Equal(int32(1)))
	})

	t.Run("response loss replays one immutable operation identity", func(t *testing.T) {
		g := NewWithT(t)
		var deliveries atomic.Int32
		mutations := map[string]int{}
		var mutationsMu sync.Mutex
		client := &http.Client{Transport: webhookFailureRoundTripper(func(request *http.Request) (*http.Response, error) {
			attempt := deliveries.Add(1)
			key := request.Header.Get("Idempotency-Key")
			mutationsMu.Lock()
			if mutations[key] == 0 {
				mutations[key] = 1
			}
			mutationsMu.Unlock()
			if attempt == 1 {
				return &http.Response{
					StatusCode: http.StatusAccepted,
					Header:     make(http.Header),
					Body:       webhookLostResponseBody{},
				}, nil
			}
			return webhookFailureResponse(http.StatusAccepted, `{"queueId":"jenkins-42"}`), nil
		})}
		input := webhookFailureMatrixInput(t, 3)
		input.Outputs = []OutputDeclaration{{
			Name: "queueId", Pointer: "/queueId", Type: "string", Required: true,
		}}

		result, err := Run(
			context.Background(), input, func(string) error { return nil },
			webhookFailureMatrixRuntimeOptions(client),
		)

		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(result.Attempts).To(Equal(2))
		g.Expect(deliveries.Load()).To(Equal(int32(2)))
		g.Expect(mutations).To(Equal(map[string]int{input.IdempotencyKey: 1}))
		g.Expect(result.Outputs).To(HaveLen(1))
		g.Expect(result.Outputs[0].Value).To(Equal("jenkins-42"))
		g.Expect(webhookRetryReasons(result.AuditTrail)).To(Equal([]string{
			"retryable transport error",
		}))
	})
}

func webhookFailureMatrixInput(t *testing.T, maxAttempts int) Input {
	t.Helper()
	t.Setenv(webhookAllowedHostsEnv, "hooks.example.com")
	input, err := DecodeInput(map[string]any{
		"url":            "https://hooks.example.com/deploy",
		"method":         "POST",
		"body":           map[string]any{"component": "transaction-api"},
		"signingSecret":  "failure-matrix-signing-secret",
		"idempotencyKey": "choice-tp-dev-transaction-1.0.0",
		"timeoutSeconds": 30,
		"expectedStatusCodes": []any{
			http.StatusAccepted,
		},
		"retry": map[string]any{
			"maxAttempts": maxAttempts, "backoffSeconds": 0,
			"retryableStatusCodes": []any{http.StatusServiceUnavailable},
		},
	})
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	input.TenantID = uuid.New()
	input.LeaseID = uuid.New()
	input.TaskID = uuid.New()
	input.StepRunID = uuid.New()
	return input
}

func webhookFailureMatrixRuntimeOptions(client *http.Client) RuntimeOptions {
	return RuntimeOptions{
		HTTPClient: client,
		LookupIPAddr: func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("192.0.2.10")}}, nil
		},
	}
}

func webhookFailureResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func webhookRetryReasons(audit AuditExport) []string {
	reasons := make([]string, 0)
	for _, event := range audit.Events {
		if event.RetryReason != "" {
			reasons = append(reasons, event.RetryReason)
		}
	}
	return reasons
}

type webhookFailureRoundTripper func(*http.Request) (*http.Response, error)

func (roundTrip webhookFailureRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

type webhookRetryableTimeoutError struct{}

func (webhookRetryableTimeoutError) Error() string   { return "webhook transport timeout" }
func (webhookRetryableTimeoutError) Timeout() bool   { return true }
func (webhookRetryableTimeoutError) Temporary() bool { return true }

type webhookLostResponseBody struct{}

func (webhookLostResponseBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (webhookLostResponseBody) Close() error             { return nil }
