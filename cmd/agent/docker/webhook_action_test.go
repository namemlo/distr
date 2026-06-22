package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestWebhookActionInputRejectsUnsafeTargetsAndPlaintextHeaders(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*testing.T)
		mutate  func(map[string]any)
		message string
	}{
		{
			name:    "missing trusted host policy",
			message: webhookAllowedHostsEnv + " is required",
		},
		{
			name: "non-https URL",
			setup: func(t *testing.T) {
				t.Setenv(webhookAllowedHostsEnv, "hooks.example.com")
			},
			mutate: func(inputs map[string]any) {
				inputs["url"] = "http://hooks.example.com/deployments"
			},
			message: "url must use https",
		},
		{
			name: "URL credentials",
			setup: func(t *testing.T) {
				t.Setenv(webhookAllowedHostsEnv, "hooks.example.com")
			},
			mutate: func(inputs map[string]any) {
				inputs["url"] = "https://user:pass@hooks.example.com/deployments"
			},
			message: "url must not include credentials",
		},
		{
			name: "host not allowlisted",
			setup: func(t *testing.T) {
				t.Setenv(webhookAllowedHostsEnv, "hooks.example.com")
			},
			mutate: func(inputs map[string]any) {
				inputs["url"] = "https://169.254.169.254/latest/meta-data"
			},
			message: "webhook host is not allowlisted",
		},
		{
			name: "unspecified IPv4 target without private allowlist",
			setup: func(t *testing.T) {
				t.Setenv(webhookAllowedHostsEnv, "0.0.0.0")
			},
			mutate: func(inputs map[string]any) {
				inputs["url"] = "https://0.0.0.0/deployments"
			},
			message: "webhook host resolves to unsafe address",
		},
		{
			name: "carrier grade NAT target without private allowlist",
			setup: func(t *testing.T) {
				t.Setenv(webhookAllowedHostsEnv, "100.64.0.1")
			},
			mutate: func(inputs map[string]any) {
				inputs["url"] = "https://100.64.0.1/deployments"
			},
			message: "webhook host resolves to unsafe address",
		},
		{
			name: "plaintext authorization header",
			setup: func(t *testing.T) {
				t.Setenv(webhookAllowedHostsEnv, "hooks.example.com")
			},
			mutate: func(inputs map[string]any) {
				inputs["headers"] = map[string]any{"Authorization": "Bearer plain-secret"}
			},
			message: "headers cannot include Authorization",
		},
		{
			name: "duplicate output names",
			setup: func(t *testing.T) {
				t.Setenv(webhookAllowedHostsEnv, "hooks.example.com")
			},
			mutate: func(inputs map[string]any) {
				inputs["outputs"] = []any{
					map[string]any{"name": "remoteId", "pointer": "/id", "type": "string"},
					map[string]any{"name": "remoteId", "pointer": "/accepted", "type": "boolean"},
				}
			},
			message: "outputs contains duplicate name",
		},
		{
			name: "reserved statusCode output name",
			setup: func(t *testing.T) {
				t.Setenv(webhookAllowedHostsEnv, "hooks.example.com")
			},
			mutate: func(inputs map[string]any) {
				inputs["outputs"] = []any{
					map[string]any{"name": "statusCode", "pointer": "/status", "type": "number"},
				}
			},
			message: "outputs name statusCode is reserved",
		},
		{
			name: "reserved attempts output name",
			setup: func(t *testing.T) {
				t.Setenv(webhookAllowedHostsEnv, "hooks.example.com")
			},
			mutate: func(inputs map[string]any) {
				inputs["outputs"] = []any{
					map[string]any{"name": "attempts", "pointer": "/attempts", "type": "number"},
				}
			},
			message: "outputs name attempts is reserved",
		},
		{
			name: "too many declared outputs",
			setup: func(t *testing.T) {
				t.Setenv(webhookAllowedHostsEnv, "hooks.example.com")
			},
			mutate: func(inputs map[string]any) {
				inputs["outputs"] = webhookOutputDeclarations(api.MaxStepRunEventOutputItemCount - 1)
			},
			message: "outputs contains too many entries",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			inputs := validWebhookInputs()
			if tt.setup != nil {
				tt.setup(t)
			}
			if tt.mutate != nil {
				tt.mutate(inputs)
			}

			_, err := decodeWebhookActionInput(inputs)

			g.Expect(err).To(MatchError(ContainSubstring(tt.message)))
		})
	}
}

func TestExecuteWebhookStepRejectsReservedOutputNamesBeforeHTTPRequest(t *testing.T) {
	g := NewWithT(t)
	requestSent := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestSent = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	setWebhookPolicyEnvForURL(t, server.URL)
	oldClient := webhookHTTPClientForTest
	webhookHTTPClientForTest = server.Client()
	t.Cleanup(func() { webhookHTTPClientForTest = oldClient })
	inputs := validWebhookInputs()
	inputs["url"] = server.URL + "/deployments"
	inputs["secretHeaders"] = map[string]any{"Authorization": "Bearer token"}
	inputs["signingSecret"] = "signing-secret-value"
	inputs["outputs"] = []any{
		map[string]any{"name": "statusCode", "pointer": "/status", "type": "number"},
	}
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:     uuid.New(),
		Key:           "notify",
		ActionType:    webhookActionType,
		ActionVersion: types.AgentActionVersionV1,
		Inputs:        inputs,
	}
	recorder := &recordingLeasedTaskClient{}

	err := executeWebhookStep(context.Background(), lease, step, recorder)

	g.Expect(err).To(MatchError(ContainSubstring("outputs name statusCode is reserved")))
	g.Expect(requestSent).To(BeFalse())
	g.Expect(eventTypes(recorder.events)).To(Equal([]types.StepRunEventType{
		types.StepRunEventTypeStarted,
		types.StepRunEventTypeFailed,
	}))
	g.Expect(recorder.events[1].Message).To(ContainSubstring("outputs name statusCode is reserved"))
}

func TestExecuteWebhookStepRejectsTooManyDeclaredOutputsBeforeHTTPRequest(t *testing.T) {
	g := NewWithT(t)
	requestSent := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestSent = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	setWebhookPolicyEnvForURL(t, server.URL)
	oldClient := webhookHTTPClientForTest
	webhookHTTPClientForTest = server.Client()
	t.Cleanup(func() { webhookHTTPClientForTest = oldClient })
	inputs := validWebhookInputs()
	inputs["url"] = server.URL + "/deployments"
	inputs["secretHeaders"] = map[string]any{"Authorization": "Bearer token"}
	inputs["signingSecret"] = "signing-secret-value"
	inputs["outputs"] = webhookOutputDeclarations(api.MaxStepRunEventOutputItemCount - 1)
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:     uuid.New(),
		Key:           "notify",
		ActionType:    webhookActionType,
		ActionVersion: types.AgentActionVersionV1,
		Inputs:        inputs,
	}
	recorder := &recordingLeasedTaskClient{}

	err := executeWebhookStep(context.Background(), lease, step, recorder)

	g.Expect(err).To(MatchError(ContainSubstring("outputs contains too many entries")))
	g.Expect(requestSent).To(BeFalse())
	g.Expect(eventTypes(recorder.events)).To(Equal([]types.StepRunEventType{
		types.StepRunEventTypeStarted,
		types.StepRunEventTypeFailed,
	}))
}

func TestExecuteWebhookStepFailsClosedOnEventPersistenceError(t *testing.T) {
	g := NewWithT(t)
	requestSent := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestSent = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	setWebhookPolicyEnvForURL(t, server.URL)
	oldClient := webhookHTTPClientForTest
	webhookHTTPClientForTest = server.Client()
	t.Cleanup(func() { webhookHTTPClientForTest = oldClient })
	inputs := validWebhookInputs()
	inputs["url"] = server.URL + "/deployments"
	inputs["secretHeaders"] = map[string]any{"Authorization": "Bearer token"}
	inputs["signingSecret"] = "signing-secret-value"
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:     uuid.New(),
		Key:           "notify",
		ActionType:    webhookActionType,
		ActionVersion: types.AgentActionVersionV1,
		Inputs:        inputs,
	}
	recorder := &recordingLeasedTaskClient{
		recordingStepEventClient: recordingStepEventClient{
			stepEventErr:   errors.New("event persistence failed"),
			stepEventErrOn: types.StepRunEventTypeStarted,
		},
	}

	err := executeWebhookStep(context.Background(), lease, step, recorder)

	g.Expect(err).To(MatchError(ContainSubstring("event persistence failed")))
	g.Expect(requestSent).To(BeFalse())
	g.Expect(recorder.events).To(BeEmpty())
}

func TestWebhookSignatureCanonicalDataIsDeterministic(t *testing.T) {
	g := NewWithT(t)
	endpoint, err := url.Parse("https://hooks.example.com/a/b?x=1")
	g.Expect(err).NotTo(HaveOccurred())

	digest := webhookBodyDigest([]byte(`{"ok":true}`))
	canonical := webhookCanonicalData("post", endpoint, "2026-06-22T12:34:56Z", "idem-123", digest)
	signature := webhookSignature("signing-secret", canonical)

	g.Expect(digest).To(Equal("sha256:4062edaf750fb8074e7e83e0c9028c94e32468a8b6f1614774328ef045150f93"))
	g.Expect(canonical).To(Equal(strings.Join([]string{
		"POST",
		"/a/b?x=1",
		"2026-06-22T12:34:56Z",
		"idem-123",
		"sha256:4062edaf750fb8074e7e83e0c9028c94e32468a8b6f1614774328ef045150f93",
	}, "\n")))
	g.Expect(signature).To(Equal("sha256=405ecfc654bc95cb07acaff530eb82ffdcfca4b8ec7cfd82b41ea50d98eebb74"))
}

func TestWebhookActionHMACSignatureIsDeterministicAcrossQueryOrder(t *testing.T) {
	g := NewWithT(t)
	first, err := url.Parse("https://hooks.example.com/deployments?b=2&a=1")
	g.Expect(err).NotTo(HaveOccurred())
	second, err := url.Parse("https://hooks.example.com/deployments?a=1&b=2")
	g.Expect(err).NotTo(HaveOccurred())
	digest := webhookBodyDigest([]byte(`{"ok":true}`))

	firstSignature := webhookSignature("signing-secret", webhookCanonicalData("POST", first, "2026-06-22T12:34:56Z", "idem-123", digest))
	secondSignature := webhookSignature("signing-secret", webhookCanonicalData("POST", second, "2026-06-22T12:34:56Z", "idem-123", digest))

	g.Expect(firstSignature).To(Equal(secondSignature))
}

func TestWebhookActionHMACSignatureIgnoresURLFragment(t *testing.T) {
	g := NewWithT(t)
	withoutFragment, err := url.Parse("https://hooks.example.com/deployments?a=1&b=2")
	g.Expect(err).NotTo(HaveOccurred())
	withFragment, err := url.Parse("https://hooks.example.com/deployments?b=2&a=1#section")
	g.Expect(err).NotTo(HaveOccurred())
	digest := webhookBodyDigest([]byte(`{"ok":true}`))

	withoutFragmentCanonical := webhookCanonicalData("POST", withoutFragment, "2026-06-22T12:34:56Z", "idem-123", digest)
	withFragmentCanonical := webhookCanonicalData("POST", withFragment, "2026-06-22T12:34:56Z", "idem-123", digest)
	withoutFragmentSignature := webhookSignature("signing-secret", withoutFragmentCanonical)
	withFragmentSignature := webhookSignature("signing-secret", withFragmentCanonical)

	g.Expect(withoutFragmentCanonical).To(Equal(withFragmentCanonical))
	g.Expect(withoutFragmentSignature).To(Equal(withFragmentSignature))
}

func TestWebhookRequestBodyNilAndEmptyObjectShareDigest(t *testing.T) {
	g := NewWithT(t)

	nilBody, err := webhookRequestBodyBytes(nil)
	g.Expect(err).NotTo(HaveOccurred())
	emptyObjectBody, err := webhookRequestBodyBytes(map[string]any{})
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(string(nilBody)).To(Equal(`{}`))
	g.Expect(webhookBodyDigest(nilBody)).To(Equal(webhookBodyDigest(emptyObjectBody)))
}

func TestNewWebhookHTTPClientRejectsUnsafeTargetThroughProxyEnvironment(t *testing.T) {
	g := NewWithT(t)
	var connectCount int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			atomic.AddInt32(&connectCount, 1)
		}
		http.Error(w, "proxy should not be reached", http.StatusBadGateway)
	}))
	defer proxy.Close()
	proxyURL, err := url.Parse(proxy.URL)
	g.Expect(err).NotTo(HaveOccurred())
	t.Setenv("HTTPS_PROXY", proxy.URL)
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("NO_PROXY", "")
	t.Setenv(webhookAllowedHostsEnv, "localhost")
	t.Setenv(webhookAllowedPrivateHostsEnv, proxyURL.Host)
	input := webhookActionInput{
		URL:                 "https://localhost./deployments",
		Method:              http.MethodPost,
		Body:                map[string]any{"deploymentId": "dep-123"},
		SigningSecret:       "signing-secret",
		IdempotencyKey:      "idem-123",
		TimeoutSeconds:      1,
		ExpectedStatusCodes: []int{http.StatusOK},
		Retry: webhookRetryPolicy{
			MaxAttempts: 1,
		},
	}

	_, err = runWebhookAction(context.Background(), input, func(string) error { return nil })

	g.Expect(err).To(MatchError(ContainSubstring("webhook host resolves to unsafe address")))
	g.Expect(atomic.LoadInt32(&connectCount)).To(Equal(int32(0)))
}

func TestRunWebhookActionRejectsUnsafeResolvedAddressBeforeAttempt(t *testing.T) {
	g := NewWithT(t)
	t.Setenv(webhookAllowedHostsEnv, "hooks.example.com")
	setWebhookLookupIPAddr(t, func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	})
	progress := []string{}
	input := webhookActionInput{
		URL:                 "https://hooks.example.com/deployments",
		Method:              http.MethodPost,
		Body:                map[string]any{"deploymentId": "dep-123"},
		SigningSecret:       "signing-secret",
		IdempotencyKey:      "idem-123",
		TimeoutSeconds:      1,
		ExpectedStatusCodes: []int{http.StatusOK},
		Retry: webhookRetryPolicy{
			MaxAttempts: 3,
		},
	}

	result, err := runWebhookAction(context.Background(), input, func(message string) error {
		progress = append(progress, message)
		return nil
	})

	g.Expect(err).To(MatchError(ContainSubstring("webhook host resolves to unsafe address")))
	g.Expect(result.Attempts).To(Equal(0))
	g.Expect(progress).To(BeEmpty())
}

func TestRunWebhookActionUsesPinnedResolvedAddressForDial(t *testing.T) {
	g := NewWithT(t)
	t.Setenv(webhookAllowedHostsEnv, "hooks.example.com")
	lookups := 0
	setWebhookLookupIPAddr(t, func(context.Context, string) ([]net.IPAddr, error) {
		lookups++
		if lookups == 1 {
			return []net.IPAddr{{IP: net.ParseIP("203.0.113.10")}}, nil
		}
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	})
	var dialAddress string
	setWebhookDialContext(t, func(_ context.Context, _ string, address string) (net.Conn, error) {
		dialAddress = address
		return nil, errors.New("stop after pinned dial")
	})
	input := webhookActionInput{
		URL:                 "https://hooks.example.com/deployments",
		Method:              http.MethodPost,
		Body:                map[string]any{"deploymentId": "dep-123"},
		SigningSecret:       "signing-secret",
		IdempotencyKey:      "idem-123",
		TimeoutSeconds:      1,
		ExpectedStatusCodes: []int{http.StatusOK},
		Retry: webhookRetryPolicy{
			MaxAttempts: 1,
		},
	}

	result, err := runWebhookAction(context.Background(), input, func(string) error { return nil })

	g.Expect(err).To(MatchError(ContainSubstring("stop after pinned dial")))
	g.Expect(result.Attempts).To(Equal(1))
	g.Expect(lookups).To(Equal(1))
	g.Expect(dialAddress).To(Equal("203.0.113.10:443"))
}

func TestWebhookActionInputRejectsRedirectToUnsafeTargets(t *testing.T) {
	g := NewWithT(t)
	var requestCount int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		http.Redirect(w, r, "https://100.64.0.1/latest/meta-data", http.StatusFound)
	}))
	defer server.Close()
	serverURL, err := url.Parse(server.URL)
	g.Expect(err).NotTo(HaveOccurred())
	t.Setenv(webhookAllowedHostsEnv, strings.Join([]string{serverURL.Host, "100.64.0.1"}, ","))
	t.Setenv(webhookAllowedPrivateHostsEnv, serverURL.Host)
	oldClient := webhookHTTPClientForTest
	webhookHTTPClientForTest = server.Client()
	t.Cleanup(func() { webhookHTTPClientForTest = oldClient })
	input := webhookActionInput{
		URL:                 server.URL + "/deployments",
		Method:              http.MethodPost,
		Body:                map[string]any{"deploymentId": "dep-123"},
		SigningSecret:       "signing-secret",
		IdempotencyKey:      "idem-123",
		TimeoutSeconds:      1,
		ExpectedStatusCodes: []int{http.StatusOK},
		Retry: webhookRetryPolicy{
			MaxAttempts: 1,
		},
	}

	_, err = runWebhookAction(context.Background(), input, func(string) error { return nil })

	g.Expect(err).To(MatchError(ContainSubstring("webhook host resolves to unsafe address")))
	g.Expect(atomic.LoadInt32(&requestCount)).To(Equal(int32(1)))
}

func TestWebhookActionInputRejectsRedirectToUnsafeResolvedHost(t *testing.T) {
	g := NewWithT(t)
	var requestCount int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		http.Redirect(w, r, "https://redirect.example.com/latest/meta-data", http.StatusFound)
	}))
	defer server.Close()
	serverURL, err := url.Parse(server.URL)
	g.Expect(err).NotTo(HaveOccurred())
	t.Setenv(webhookAllowedHostsEnv, strings.Join([]string{serverURL.Host, "redirect.example.com"}, ","))
	t.Setenv(webhookAllowedPrivateHostsEnv, serverURL.Host)
	setWebhookLookupIPAddr(t, func(_ context.Context, host string) ([]net.IPAddr, error) {
		g.Expect(host).To(Equal("redirect.example.com"))
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	})
	oldClient := webhookHTTPClientForTest
	webhookHTTPClientForTest = server.Client()
	t.Cleanup(func() { webhookHTTPClientForTest = oldClient })
	input := webhookActionInput{
		URL:                 server.URL + "/deployments",
		Method:              http.MethodPost,
		Body:                map[string]any{"deploymentId": "dep-123"},
		SigningSecret:       "signing-secret",
		IdempotencyKey:      "idem-123",
		TimeoutSeconds:      1,
		ExpectedStatusCodes: []int{http.StatusOK},
		Retry: webhookRetryPolicy{
			MaxAttempts: 1,
		},
	}

	_, err = runWebhookAction(context.Background(), input, func(string) error { return nil })

	g.Expect(err).To(MatchError(ContainSubstring("webhook host resolves to unsafe address")))
	g.Expect(atomic.LoadInt32(&requestCount)).To(Equal(int32(1)))
}

func TestRunWebhookActionRejectsUntrustedTLSCertificate(t *testing.T) {
	g := NewWithT(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	setWebhookPolicyEnvForURL(t, server.URL)
	input := webhookActionInput{
		URL:                 server.URL + "/deployments",
		Method:              http.MethodPost,
		Body:                map[string]any{"deploymentId": "dep-123"},
		SigningSecret:       "signing-secret",
		IdempotencyKey:      "idem-123",
		TimeoutSeconds:      1,
		ExpectedStatusCodes: []int{http.StatusOK},
		Retry: webhookRetryPolicy{
			MaxAttempts: 2,
		},
	}

	result, err := runWebhookAction(context.Background(), input, func(string) error { return nil })

	g.Expect(err).To(MatchError(ContainSubstring("certificate")))
	g.Expect(result.Attempts).To(Equal(1))
}

func TestWebhookActionSecurityContractSuite(t *testing.T) {
	t.Run("SSRF via DNS fails before attempts", func(t *testing.T) {
		g := NewWithT(t)
		t.Setenv(webhookAllowedHostsEnv, "hooks.example.com")
		setWebhookLookupIPAddr(t, func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
		})
		input := webhookActionInput{
			URL:                 "https://hooks.example.com/deployments",
			Method:              http.MethodPost,
			Body:                map[string]any{"deploymentId": "dep-123"},
			SigningSecret:       "signing-secret",
			IdempotencyKey:      "idem-123",
			TimeoutSeconds:      1,
			ExpectedStatusCodes: []int{http.StatusOK},
			Retry: webhookRetryPolicy{
				MaxAttempts: 2,
			},
		}

		result, err := runWebhookAction(context.Background(), input, func(string) error { return nil })

		g.Expect(err).To(MatchError(ContainSubstring("webhook host resolves to unsafe address")))
		g.Expect(result.Attempts).To(Equal(0))
	})
	t.Run("proxy environment is ignored", func(t *testing.T) {
		g := NewWithT(t)
		var proxyRequests int32
		proxy := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			atomic.AddInt32(&proxyRequests, 1)
		}))
		defer proxy.Close()
		t.Setenv("HTTP_PROXY", proxy.URL)
		t.Setenv("HTTPS_PROXY", proxy.URL)
		t.Setenv("NO_PROXY", "")
		t.Setenv(webhookAllowedHostsEnv, "localhost")
		input := webhookActionInput{
			URL:                 "https://localhost./deployments",
			Method:              http.MethodPost,
			Body:                map[string]any{"deploymentId": "dep-123"},
			SigningSecret:       "signing-secret",
			IdempotencyKey:      "idem-123",
			TimeoutSeconds:      1,
			ExpectedStatusCodes: []int{http.StatusOK},
			Retry: webhookRetryPolicy{
				MaxAttempts: 1,
			},
		}

		_, err := runWebhookAction(context.Background(), input, func(string) error { return nil })

		g.Expect(err).To(MatchError(ContainSubstring("webhook host resolves to unsafe address")))
		g.Expect(atomic.LoadInt32(&proxyRequests)).To(Equal(int32(0)))
	})
	t.Run("signing ignores query order and fragment", func(t *testing.T) {
		g := NewWithT(t)
		first, err := url.Parse("https://hooks.example.com/deployments?b=2&a=1#section")
		g.Expect(err).NotTo(HaveOccurred())
		second, err := url.Parse("https://hooks.example.com/deployments?a=1&b=2")
		g.Expect(err).NotTo(HaveOccurred())
		digest := webhookBodyDigest([]byte(`{"ok":true}`))

		firstSignature := webhookSignature("signing-secret", webhookCanonicalData("POST", first, "2026-06-22T12:34:56Z", "idem-123", digest))
		secondSignature := webhookSignature("signing-secret", webhookCanonicalData("POST", second, "2026-06-22T12:34:56Z", "idem-123", digest))

		g.Expect(firstSignature).To(Equal(secondSignature))
	})
	t.Run("DNS failures are non-retryable", func(t *testing.T) {
		g := NewWithT(t)
		err := &net.DNSError{Err: "temporary DNS failure", Name: "hooks.example.com", IsTemporary: true}

		g.Expect(isRetryableWebhookAttemptError(0, err)).To(BeFalse())
	})
}

func TestExecuteWebhookStepPreservesResolvedSigningSecretBytes(t *testing.T) {
	g := NewWithT(t)
	const signingSecret = "  signing-secret-value  "
	var signature string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		signature = r.Header.Get("X-Distr-Signature")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	setWebhookPolicyEnvForURL(t, server.URL)
	oldClient := webhookHTTPClientForTest
	webhookHTTPClientForTest = server.Client()
	t.Cleanup(func() { webhookHTTPClientForTest = oldClient })
	oldNow := webhookNow
	webhookNow = func() time.Time { return time.Date(2026, 6, 22, 12, 34, 56, 0, time.UTC) }
	t.Cleanup(func() { webhookNow = oldNow })
	inputs := validWebhookInputs()
	inputs["url"] = server.URL + "/deployments"
	inputs["secretHeaders"] = map[string]any{"Authorization": "Bearer token"}
	inputs["signingSecret"] = signingSecret
	inputs["outputs"] = []any{}
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:     uuid.New(),
		Key:           "notify",
		ActionType:    webhookActionType,
		ActionVersion: types.AgentActionVersionV1,
		Inputs:        inputs,
	}
	recorder := &recordingLeasedTaskClient{}
	endpoint, err := url.Parse(server.URL + "/deployments")
	g.Expect(err).NotTo(HaveOccurred())
	body, err := webhookRequestBodyBytes(map[string]any{"deploymentId": "dep-123"})
	g.Expect(err).NotTo(HaveOccurred())
	expectedSignature := webhookSignature(
		signingSecret,
		webhookCanonicalData(
			http.MethodPost,
			endpoint,
			"2026-06-22T12:34:56Z",
			"notify-demo",
			webhookBodyDigest(body),
		),
	)

	err = executeWebhookStep(context.Background(), lease, step, recorder)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(signature).To(Equal(expectedSignature))
}

func TestExecuteWebhookStepRedactsWireNormalizedSecretHeaderValue(t *testing.T) {
	g := NewWithT(t)
	const headerSecret = " secret-value "
	const normalizedHeaderSecret = "secret-value"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.Expect(r.Header.Get("X-Webhook-Token")).To(Equal(normalizedHeaderSecret))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"echo":"` + normalizedHeaderSecret + `"}`))
	}))
	defer server.Close()
	setWebhookPolicyEnvForURL(t, server.URL)
	oldClient := webhookHTTPClientForTest
	webhookHTTPClientForTest = server.Client()
	t.Cleanup(func() { webhookHTTPClientForTest = oldClient })
	inputs := validWebhookInputs()
	inputs["url"] = server.URL + "/deployments"
	inputs["secretHeaders"] = map[string]any{"X-Webhook-Token": headerSecret}
	inputs["signingSecret"] = "signing-secret-value"
	inputs["outputs"] = []any{
		map[string]any{"name": "echo", "pointer": "/echo", "type": "string", "required": true},
	}
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:     uuid.New(),
		Key:           "notify",
		ActionType:    webhookActionType,
		ActionVersion: types.AgentActionVersionV1,
		Inputs:        inputs,
	}
	recorder := &recordingLeasedTaskClient{}

	err := executeWebhookStep(context.Background(), lease, step, recorder)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(recorder.events).To(HaveLen(3))
	g.Expect(recorder.events[2].Outputs).To(ContainElement(api.AgentStepRunOutputRequest{Name: "echo", Value: "[REDACTED]"}))
	payload, err := json.Marshal(recorder.events)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(payload)).NotTo(ContainSubstring(normalizedHeaderSecret))
}

func TestExecuteWebhookStepSendsSignedRequestRetriesAndExtractsOutputs(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	const headerSecret = "Bearer super-secret-webhook-token"
	const signingSecret = "signing-secret-value"
	var idempotencyKeys []string
	var signatures []string
	var bodies []string
	attempts := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		body, err := io.ReadAll(r.Body)
		g.Expect(err).NotTo(HaveOccurred())
		bodies = append(bodies, string(body))
		idempotencyKeys = append(idempotencyKeys, r.Header.Get("Idempotency-Key"))
		signatures = append(signatures, r.Header.Get("X-Distr-Signature"))
		g.Expect(r.Method).To(Equal(http.MethodPost))
		g.Expect(r.Header.Get("Authorization")).To(Equal(headerSecret))
		g.Expect(r.Header.Get("X-Distr-Timestamp")).To(Equal("2026-06-22T12:34:56Z"))
		g.Expect(r.Header.Get("X-Distr-Body-Digest")).To(HavePrefix("sha256:"))
		if attempts == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"retry":true}`))
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"id":"remote-123","accepted":true,"echo":"` + headerSecret + `","signature":"` + r.Header.Get("X-Distr-Signature") + `"}`))
	}))
	defer server.Close()
	setWebhookPolicyEnvForURL(t, server.URL)
	oldClient := webhookHTTPClientForTest
	webhookHTTPClientForTest = server.Client()
	t.Cleanup(func() { webhookHTTPClientForTest = oldClient })
	oldNow := webhookNow
	webhookNow = func() time.Time { return time.Date(2026, 6, 22, 12, 34, 56, 0, time.UTC) }
	t.Cleanup(func() { webhookNow = oldNow })
	inputs := validWebhookInputs()
	inputs["url"] = server.URL + "/deployments"
	inputs["secretHeaders"] = map[string]any{"Authorization": headerSecret}
	inputs["signingSecret"] = signingSecret
	inputs["retry"] = map[string]any{
		"maxAttempts":          2,
		"backoffSeconds":       0,
		"retryableStatusCodes": []any{503},
	}
	inputs["expectedStatusCodes"] = []any{202}
	inputs["outputs"] = []any{
		map[string]any{"name": "remoteId", "pointer": "/id", "type": "string", "required": true},
		map[string]any{"name": "accepted", "pointer": "/accepted", "type": "boolean", "sensitive": true},
		map[string]any{"name": "echo", "pointer": "/echo", "type": "string"},
		map[string]any{"name": "signature", "pointer": "/signature", "type": "string"},
	}
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:        uuid.New(),
		Key:              "notify",
		ActionType:       webhookActionType,
		ActionVersion:    types.AgentActionVersionV1,
		Inputs:           inputs,
		SecretReferences: []string{"secret:webhook_auth_token", "secret:webhook_signing_key"},
		IdempotencyKey:   "sha256:lease-step-key",
	}
	recorder := &recordingLeasedTaskClient{}

	err := executeWebhookStep(ctx, lease, step, recorder)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(attempts).To(Equal(2))
	g.Expect(idempotencyKeys).To(Equal([]string{"notify-demo", "notify-demo"}))
	g.Expect(signatures).To(HaveLen(2))
	g.Expect(signatures[0]).To(Equal(signatures[1]))
	g.Expect(bodies).To(Equal([]string{`{"deploymentId":"dep-123"}`, `{"deploymentId":"dep-123"}`}))
	g.Expect(eventTypes(recorder.events)).To(Equal([]types.StepRunEventType{
		types.StepRunEventTypeStarted,
		types.StepRunEventTypeProgress,
		types.StepRunEventTypeProgress,
		types.StepRunEventTypeSucceeded,
	}))
	outputs := recorder.events[3].Outputs
	g.Expect(outputs).To(ContainElement(api.AgentStepRunOutputRequest{Name: "statusCode", Value: 202}))
	g.Expect(outputs).To(ContainElement(api.AgentStepRunOutputRequest{Name: "attempts", Value: 2}))
	g.Expect(outputs).To(ContainElement(api.AgentStepRunOutputRequest{Name: "remoteId", Value: "remote-123"}))
	g.Expect(outputs).To(ContainElement(api.AgentStepRunOutputRequest{Name: "accepted", Sensitive: true}))
	g.Expect(outputs).To(ContainElement(api.AgentStepRunOutputRequest{Name: "echo", Value: "[REDACTED]"}))
	g.Expect(outputs).To(ContainElement(api.AgentStepRunOutputRequest{Name: "signature", Value: "[REDACTED]"}))
	payload, err := json.Marshal(recorder.events)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(payload)).NotTo(ContainSubstring(headerSecret))
	g.Expect(string(payload)).NotTo(ContainSubstring(signingSecret))
}

func TestExecuteWebhookStepRetriesOnConfigured429ExpectedStatus(t *testing.T) {
	g := NewWithT(t)
	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		if requests == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	setWebhookPolicyEnvForURL(t, server.URL)
	oldClient := webhookHTTPClientForTest
	webhookHTTPClientForTest = server.Client()
	t.Cleanup(func() { webhookHTTPClientForTest = oldClient })
	inputs := validWebhookInputs()
	inputs["url"] = server.URL + "/deployments"
	inputs["secretHeaders"] = map[string]any{"Authorization": "Bearer token"}
	inputs["signingSecret"] = "signing-secret-value"
	inputs["retry"] = map[string]any{
		"maxAttempts":          2,
		"backoffSeconds":       0,
		"retryableStatusCodes": []any{http.StatusTooManyRequests},
	}
	inputs["expectedStatusCodes"] = []any{http.StatusOK, http.StatusTooManyRequests}
	inputs["outputs"] = []any{}
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:     uuid.New(),
		Key:           "notify",
		ActionType:    webhookActionType,
		ActionVersion: types.AgentActionVersionV1,
		Inputs:        inputs,
	}
	recorder := &recordingLeasedTaskClient{}

	err := executeWebhookStep(context.Background(), lease, step, recorder)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(requests).To(Equal(2))
	outputs := recorder.events[len(recorder.events)-1].Outputs
	g.Expect(outputs).To(ContainElement(api.AgentStepRunOutputRequest{Name: "statusCode", Value: http.StatusOK}))
	g.Expect(outputs).To(ContainElement(api.AgentStepRunOutputRequest{Name: "attempts", Value: 2}))
}

func TestWebhookActionRetriesPreservesIdempotencyKeyHeaderAcrossAttempts(t *testing.T) {
	g := NewWithT(t)
	var idempotencyKeys []string
	attempts := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		idempotencyKeys = append(idempotencyKeys, r.Header.Get("Idempotency-Key"))
		if attempts == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	setWebhookPolicyEnvForURL(t, server.URL)
	oldClient := webhookHTTPClientForTest
	webhookHTTPClientForTest = server.Client()
	t.Cleanup(func() { webhookHTTPClientForTest = oldClient })
	input := webhookActionInput{
		URL:                 server.URL + "/deployments",
		Method:              http.MethodPost,
		Body:                map[string]any{"deploymentId": "dep-123"},
		SigningSecret:       "signing-secret",
		IdempotencyKey:      "idem-123",
		TimeoutSeconds:      5,
		ExpectedStatusCodes: []int{http.StatusOK},
		Retry: webhookRetryPolicy{
			MaxAttempts:          2,
			BackoffSeconds:       0,
			RetryableStatusCodes: []int{http.StatusServiceUnavailable},
		},
	}

	_, err := runWebhookAction(context.Background(), input, func(string) error { return nil })

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(attempts).To(Equal(2))
	g.Expect(idempotencyKeys).To(Equal([]string{"idem-123", "idem-123"}))
}

func TestWebhookActionExtractsDeclaredJSONPointerOutputsFromArrays(t *testing.T) {
	g := NewWithT(t)
	responseBody := []byte(`{"items":[{"id":"remote-123"}]}`)

	outputs, err := extractWebhookDeclaredOutputs(responseBody, []webhookOutputDeclaration{
		{Name: "remoteId", Pointer: "/items/0/id", Type: "string", Required: true},
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(outputs).To(Equal([]api.AgentStepRunOutputRequest{
		{Name: "remoteId", Value: "remote-123"},
	}))
}

func TestExecuteWebhookStepDoesNotRetryOversizedResponseBody(t *testing.T) {
	g := NewWithT(t)
	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, strings.Repeat("x", webhookMaxResponseBodyBytes+1))
	}))
	defer server.Close()
	setWebhookPolicyEnvForURL(t, server.URL)
	oldClient := webhookHTTPClientForTest
	webhookHTTPClientForTest = server.Client()
	t.Cleanup(func() { webhookHTTPClientForTest = oldClient })
	inputs := validWebhookInputs()
	inputs["url"] = server.URL + "/deployments"
	inputs["secretHeaders"] = map[string]any{"Authorization": "Bearer token"}
	inputs["signingSecret"] = "signing-secret-value"
	inputs["retry"] = map[string]any{
		"maxAttempts":          3,
		"backoffSeconds":       0,
		"retryableStatusCodes": []any{503},
	}
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:     uuid.New(),
		Key:           "notify",
		ActionType:    webhookActionType,
		ActionVersion: types.AgentActionVersionV1,
		Inputs:        inputs,
	}
	recorder := &recordingLeasedTaskClient{}

	err := executeWebhookStep(context.Background(), lease, step, recorder)

	g.Expect(err).To(MatchError(ContainSubstring("webhook response body is too large")))
	g.Expect(requests).To(Equal(1))
	g.Expect(eventTypes(recorder.events)).To(Equal([]types.StepRunEventType{
		types.StepRunEventTypeStarted,
		types.StepRunEventTypeProgress,
		types.StepRunEventTypeFailed,
	}))
}

func TestExecuteWebhookStepRejectsOversizedResponseBody(t *testing.T) {
	g := NewWithT(t)
	t.Setenv(webhookAllowedHostsEnv, "hooks.example.com")
	setWebhookLookupIPAddrToPublicHost(t)
	declaredRemoteID := strings.Repeat("x", webhookMaxResponseBodyBytes)
	oversizedBody := []byte(`{"id":"` + declaredRemoteID + `"}`)
	responseBody := &countingReadCloser{data: oversizedBody}
	roundTrips := 0
	oldClient := webhookHTTPClientForTest
	webhookHTTPClientForTest = &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			roundTrips++
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       responseBody,
				Request:    request,
			}, nil
		}),
	}
	t.Cleanup(func() { webhookHTTPClientForTest = oldClient })
	inputs := validWebhookInputs()
	inputs["secretHeaders"] = map[string]any{"Authorization": "Bearer token"}
	inputs["signingSecret"] = "signing-secret-value"
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:     uuid.New(),
		Key:           "notify",
		ActionType:    webhookActionType,
		ActionVersion: types.AgentActionVersionV1,
		Inputs:        inputs,
	}
	recorder := &recordingLeasedTaskClient{}

	err := executeWebhookStep(context.Background(), lease, step, recorder)

	g.Expect(err).To(MatchError(ContainSubstring("webhook response body is too large")))
	g.Expect(roundTrips).To(Equal(1))
	g.Expect(responseBody.bytesRead).To(Equal(webhookMaxResponseBodyBytes + 1))
	g.Expect(eventTypes(recorder.events)).To(Equal([]types.StepRunEventType{
		types.StepRunEventTypeStarted,
		types.StepRunEventTypeProgress,
		types.StepRunEventTypeFailed,
	}))
	g.Expect(recorder.events[2].Outputs).To(BeEmpty())
	g.Expect(recorder.events[2].Message).To(ContainSubstring("webhook response body is too large"))
	for _, event := range recorder.events {
		g.Expect(event.Outputs).NotTo(ContainElement(api.AgentStepRunOutputRequest{Name: "remoteId", Value: declaredRemoteID}))
	}
}

func TestRunWebhookActionDoesNotRetryPermanentTransportError(t *testing.T) {
	g := NewWithT(t)
	t.Setenv(webhookAllowedHostsEnv, "hooks.example.com")
	setWebhookLookupIPAddrToPublicHost(t)
	inputs := validWebhookInputs()
	inputs["secretHeaders"] = map[string]any{"Authorization": "Bearer token"}
	inputs["signingSecret"] = "signing-secret-value"
	inputs["retry"] = map[string]any{
		"maxAttempts":          3,
		"backoffSeconds":       0,
		"retryableStatusCodes": []any{503},
	}
	input, err := decodeWebhookActionInput(inputs)
	g.Expect(err).NotTo(HaveOccurred())
	roundTrips := 0
	oldClient := webhookHTTPClientForTest
	webhookHTTPClientForTest = &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			roundTrips++
			return nil, fmt.Errorf("permanent transport failure")
		}),
	}
	t.Cleanup(func() { webhookHTTPClientForTest = oldClient })

	result, err := runWebhookAction(context.Background(), input, func(string) error { return nil })

	g.Expect(err).To(MatchError(ContainSubstring("permanent transport failure")))
	g.Expect(result.Attempts).To(Equal(1))
	g.Expect(roundTrips).To(Equal(1))
}

func TestWebhookAttemptRetryClassification(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		err       error
		wantRetry bool
	}{
		{
			name:      "EOF response body read",
			err:       io.ErrUnexpectedEOF,
			wantRetry: true,
		},
		{
			name:      "temporary body read timeout",
			err:       timeoutReadError{},
			wantRetry: true,
		},
		{
			name:      "temporary DNS failure",
			err:       &net.DNSError{Err: "temporary DNS failure", Name: "hooks.example.com", IsTemporary: true},
			wantRetry: false,
		},
		{
			name:      "unsafe IP validation failure",
			err:       fmt.Errorf("webhook host resolves to unsafe address"),
			wantRetry: false,
		},
		{
			name:      "oversized response body",
			status:    http.StatusOK,
			err:       fmt.Errorf("webhook response body is too large"),
			wantRetry: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			retry := isRetryableWebhookAttemptError(tt.status, tt.err)

			g.Expect(retry).To(Equal(tt.wantRetry))
		})
	}
}

func TestRunWebhookActionRetriesTransientResponseBodyReadError(t *testing.T) {
	g := NewWithT(t)
	t.Setenv(webhookAllowedHostsEnv, "hooks.example.com")
	setWebhookLookupIPAddrToPublicHost(t)
	inputs := validWebhookInputs()
	inputs["secretHeaders"] = map[string]any{"Authorization": "Bearer token"}
	inputs["signingSecret"] = "signing-secret-value"
	inputs["retry"] = map[string]any{
		"maxAttempts":          2,
		"backoffSeconds":       0,
		"retryableStatusCodes": []any{503},
	}
	inputs["expectedStatusCodes"] = []any{202}
	input, err := decodeWebhookActionInput(inputs)
	g.Expect(err).NotTo(HaveOccurred())
	roundTrips := 0
	oldClient := webhookHTTPClientForTest
	webhookHTTPClientForTest = &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			roundTrips++
			if roundTrips == 1 {
				return &http.Response{
					StatusCode: http.StatusAccepted,
					Body:       failingReadCloser{err: io.ErrUnexpectedEOF},
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusAccepted,
				Body:       io.NopCloser(strings.NewReader(`{"id":"remote-123"}`)),
			}, nil
		}),
	}
	t.Cleanup(func() { webhookHTTPClientForTest = oldClient })

	result, err := runWebhookAction(context.Background(), input, func(string) error { return nil })

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Attempts).To(Equal(2))
	g.Expect(result.Outputs).To(ContainElement(api.AgentStepRunOutputRequest{Name: "remoteId", Value: "remote-123"}))
	g.Expect(roundTrips).To(Equal(2))
}

func TestRunWebhookActionRetriesTransientResponseBodyNetworkError(t *testing.T) {
	g := NewWithT(t)
	t.Setenv(webhookAllowedHostsEnv, "hooks.example.com")
	setWebhookLookupIPAddrToPublicHost(t)
	inputs := validWebhookInputs()
	inputs["secretHeaders"] = map[string]any{"Authorization": "Bearer token"}
	inputs["signingSecret"] = "signing-secret-value"
	inputs["retry"] = map[string]any{
		"maxAttempts":          2,
		"backoffSeconds":       0,
		"retryableStatusCodes": []any{503},
	}
	inputs["expectedStatusCodes"] = []any{202}
	input, err := decodeWebhookActionInput(inputs)
	g.Expect(err).NotTo(HaveOccurred())
	roundTrips := 0
	oldClient := webhookHTTPClientForTest
	webhookHTTPClientForTest = &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			roundTrips++
			if roundTrips == 1 {
				return &http.Response{
					StatusCode: http.StatusAccepted,
					Body:       failingReadCloser{err: timeoutReadError{}},
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusAccepted,
				Body:       io.NopCloser(strings.NewReader(`{"id":"remote-123"}`)),
			}, nil
		}),
	}
	t.Cleanup(func() { webhookHTTPClientForTest = oldClient })

	result, err := runWebhookAction(context.Background(), input, func(string) error { return nil })

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Attempts).To(Equal(2))
	g.Expect(result.Outputs).To(ContainElement(api.AgentStepRunOutputRequest{Name: "remoteId", Value: "remote-123"}))
	g.Expect(roundTrips).To(Equal(2))
}

func TestExecuteTaskLeaseHeartbeatsAndRunsWebhookStep(t *testing.T) {
	g := NewWithT(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"remote-123"}`))
	}))
	defer server.Close()
	setWebhookPolicyEnvForURL(t, server.URL)
	oldClient := webhookHTTPClientForTest
	webhookHTTPClientForTest = server.Client()
	t.Cleanup(func() { webhookHTTPClientForTest = oldClient })
	inputs := validWebhookInputs()
	inputs["url"] = server.URL + "/deployments"
	inputs["secretHeaders"] = map[string]any{"Authorization": "Bearer token"}
	inputs["signingSecret"] = "signing-secret-value"
	lease := api.AgentTaskLease{
		TaskID:     uuid.New(),
		LeaseToken: "lease-token",
		Steps: []api.AgentTaskLeaseStep{
			{
				StepRunID:     uuid.New(),
				Key:           "notify",
				ActionType:    webhookActionType,
				ActionVersion: types.AgentActionVersionV1,
				Inputs:        inputs,
			},
		},
	}
	recorder := &recordingLeasedTaskClient{}
	apply := func(_ context.Context, _ api.AgentDeployment, _ composeDeployOptions, _ func(string)) (*AgentDeployment, string, error) {
		t.Fatal("compose apply should not run for webhook actions")
		return nil, "", nil
	}

	err := executeTaskLease(context.Background(), lease, recorder, apply)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(recorder.heartbeatTaskIDs).To(Equal([]uuid.UUID{lease.TaskID}))
	g.Expect(eventTypes(recorder.events)).To(Equal([]types.StepRunEventType{
		types.StepRunEventTypeStarted,
		types.StepRunEventTypeProgress,
		types.StepRunEventTypeSucceeded,
	}))
}

func validWebhookInputs() map[string]any {
	return map[string]any{
		"url":            "https://hooks.example.com/deployments",
		"method":         "POST",
		"headers":        map[string]any{"X-Deployment": "demo"},
		"secretHeaders":  map[string]any{"Authorization": "webhook_auth_token"},
		"body":           map[string]any{"deploymentId": "dep-123"},
		"sensitiveBody":  true,
		"signingSecret":  "webhook_signing_key",
		"timeoutSeconds": 30,
		"retry": map[string]any{
			"maxAttempts":          2,
			"backoffSeconds":       0,
			"retryableStatusCodes": []any{503},
		},
		"expectedStatusCodes": []any{200, 202},
		"idempotencyKey":      "notify-demo",
		"outputs": []any{
			map[string]any{"name": "remoteId", "pointer": "/id", "type": "string", "required": true},
		},
	}
}

func webhookOutputDeclarations(count int) []any {
	outputs := make([]any, 0, count)
	for i := 0; i < count; i++ {
		outputs = append(outputs, map[string]any{
			"name":    fmt.Sprintf("remoteId%d", i),
			"pointer": fmt.Sprintf("/items/%d/id", i),
			"type":    "string",
		})
	}
	return outputs
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type failingReadCloser struct {
	err error
}

func (r failingReadCloser) Read([]byte) (int, error) {
	return 0, r.err
}

func (r failingReadCloser) Close() error {
	return nil
}

type countingReadCloser struct {
	data      []byte
	bytesRead int
}

func (r *countingReadCloser) Read(p []byte) (int, error) {
	if r.bytesRead >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.bytesRead:])
	r.bytesRead += n
	return n, nil
}

func (r *countingReadCloser) Close() error {
	return nil
}

type timeoutReadError struct{}

func (timeoutReadError) Error() string {
	return "body read timeout"
}

func (timeoutReadError) Timeout() bool {
	return true
}

func (timeoutReadError) Temporary() bool {
	return true
}

func setWebhookPolicyEnvForURL(t *testing.T, rawURL string) {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse webhook test URL: %v", err)
	}
	t.Setenv(webhookAllowedHostsEnv, parsed.Host)
	t.Setenv(webhookAllowedPrivateHostsEnv, parsed.Host)
}

func setWebhookLookupIPAddr(t *testing.T, lookup func(context.Context, string) ([]net.IPAddr, error)) {
	t.Helper()
	oldLookup := webhookLookupIPAddr
	webhookLookupIPAddr = lookup
	t.Cleanup(func() { webhookLookupIPAddr = oldLookup })
}

func setWebhookLookupIPAddrToPublicHost(t *testing.T) {
	t.Helper()
	setWebhookLookupIPAddr(t, func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("203.0.113.10")}}, nil
	})
}

func setWebhookDialContext(t *testing.T, dial func(context.Context, string, string) (net.Conn, error)) {
	t.Helper()
	oldDial := webhookDialContext
	webhookDialContext = dial
	t.Cleanup(func() { webhookDialContext = oldDial })
}
