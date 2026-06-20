package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/distr-sh/distr/api"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

const (
	releaseExitSuccess    = 0
	releaseExitUsage      = 2
	releaseExitValidation = 3
	releaseExitAuth       = 4
	releaseExitAPI        = 5
)

type releaseCommandRuntime struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Client *http.Client
	Getenv func(string) string
}

type releaseCommandOptions struct {
	Server string
	Token  string
	Output string
}

type releaseExitError struct {
	code int
	err  error
}

func (e *releaseExitError) Error() string {
	return e.err.Error()
}

func (e *releaseExitError) Unwrap() error {
	return e.err
}

func (e *releaseExitError) ExitCode() int {
	return e.code
}

func CommandExitCode(err error) int {
	if err == nil {
		return releaseExitSuccess
	}
	var exitErr interface{ ExitCode() int }
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 1
}

func NewReleaseCommand() *cobra.Command {
	return newReleaseCommand(releaseCommandRuntime{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Client: http.DefaultClient,
		Getenv: os.Getenv,
	})
}

func newReleaseCommand(runtime releaseCommandRuntime) *cobra.Command {
	runtime = runtime.withDefaults()
	opts := releaseCommandOptions{Output: "text"}
	cmd := &cobra.Command{
		Use:   "release",
		Short: "manage release bundles through the public API",
	}
	configureReleaseCommandErrors(cmd)
	cmd.PersistentFlags().StringVar(&opts.Server, "server", "", "Distr server URL")
	cmd.PersistentFlags().StringVar(&opts.Token, "token", "", "Distr API token")
	cmd.PersistentFlags().StringVar(&opts.Output, "output", "text", "output format: text or json")
	cmd.AddCommand(
		newReleaseCreateCommand(runtime, &opts),
		newReleaseValidateCommand(runtime, &opts),
		newReleasePublishCommand(runtime, &opts),
	)
	return cmd
}

func (r releaseCommandRuntime) withDefaults() releaseCommandRuntime {
	if r.Stdin == nil {
		r.Stdin = os.Stdin
	}
	if r.Stdout == nil {
		r.Stdout = os.Stdout
	}
	if r.Stderr == nil {
		r.Stderr = os.Stderr
	}
	if r.Client == nil {
		r.Client = http.DefaultClient
	}
	if r.Getenv == nil {
		r.Getenv = os.Getenv
	}
	return r
}

func configureReleaseCommandErrors(cmd *cobra.Command) {
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return newReleaseExitError(releaseExitUsage, err.Error())
	})
}

func releaseNoArgs(cmd *cobra.Command, args []string) error {
	if len(args) != 0 {
		return newReleaseExitError(releaseExitUsage, "command does not accept arguments")
	}
	return nil
}

func releaseExactArgs(count int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) != count {
			return newReleaseExitError(releaseExitUsage, fmt.Sprintf("command requires %d argument(s)", count))
		}
		return nil
	}
}

type releaseCreateOptions struct {
	File           string
	IdempotencyKey string
}

func newReleaseCreateCommand(runtime releaseCommandRuntime, opts *releaseCommandOptions) *cobra.Command {
	createOpts := releaseCreateOptions{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "create a draft release bundle",
		Args:  releaseNoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			config, err := resolveReleaseCommandConfig(*opts, runtime)
			if err != nil {
				return err
			}
			body, err := readReleaseCreateBody(createOpts.File, runtime.Stdin)
			if err != nil {
				return err
			}
			response, err := doReleaseAPIRequest(
				cmd.Context(),
				runtime,
				config,
				http.MethodPost,
				"/api/v1/release-bundles",
				body,
				strings.TrimSpace(createOpts.IdempotencyKey),
				false,
			)
			if err != nil {
				return err
			}
			return writeReleaseBundleOutput(runtime.Stdout, config.Output, response)
		},
	}
	configureReleaseCommandErrors(cmd)
	cmd.Flags().StringVar(&createOpts.File, "file", "", "release bundle request JSON file, or - for stdin")
	cmd.Flags().StringVar(&createOpts.IdempotencyKey, "idempotency-key", "", "optional idempotency key")
	return cmd
}

func newReleaseValidateCommand(runtime releaseCommandRuntime, opts *releaseCommandOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate RELEASE_BUNDLE_ID",
		Short: "validate a release bundle",
		Args:  releaseExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			config, err := resolveReleaseCommandConfig(*opts, runtime)
			if err != nil {
				return err
			}
			if _, err := uuid.Parse(args[0]); err != nil {
				return newReleaseExitError(releaseExitUsage, "release bundle ID must be a UUID")
			}
			response, err := doReleaseAPIRequest(
				cmd.Context(),
				runtime,
				config,
				http.MethodPost,
				"/api/v1/release-bundles/"+args[0]+"/validate",
				[]byte(`{}`),
				"",
				false,
			)
			if err != nil {
				return err
			}
			return writeReleaseValidationOutput(runtime.Stdout, config.Output, response)
		},
	}
	configureReleaseCommandErrors(cmd)
	return cmd
}

func newReleasePublishCommand(runtime releaseCommandRuntime, opts *releaseCommandOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "publish RELEASE_BUNDLE_ID",
		Short: "publish a release bundle",
		Args:  releaseExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			config, err := resolveReleaseCommandConfig(*opts, runtime)
			if err != nil {
				return err
			}
			if _, err := uuid.Parse(args[0]); err != nil {
				return newReleaseExitError(releaseExitUsage, "release bundle ID must be a UUID")
			}
			response, err := doReleaseAPIRequest(
				cmd.Context(),
				runtime,
				config,
				http.MethodPost,
				"/api/v1/release-bundles/"+args[0]+"/publish",
				[]byte(`{}`),
				"",
				true,
			)
			if err != nil {
				return err
			}
			if isReleaseValidationResponse(response) {
				return writeReleaseValidationOutput(runtime.Stdout, config.Output, response)
			}
			return writeReleaseBundleOutput(runtime.Stdout, config.Output, response)
		},
	}
	configureReleaseCommandErrors(cmd)
	return cmd
}

type releaseCommandConfig struct {
	Server string
	Token  string
	Output string
}

func resolveReleaseCommandConfig(
	opts releaseCommandOptions,
	runtime releaseCommandRuntime,
) (releaseCommandConfig, error) {
	config := releaseCommandConfig{
		Server: strings.TrimSpace(opts.Server),
		Token:  strings.TrimSpace(opts.Token),
		Output: strings.TrimSpace(opts.Output),
	}
	if config.Server == "" {
		config.Server = strings.TrimSpace(runtime.Getenv("DISTR_SERVER_URL"))
	}
	if config.Token == "" {
		config.Token = strings.TrimSpace(runtime.Getenv("DISTR_API_TOKEN"))
	}
	if config.Output == "" {
		config.Output = "text"
	}
	if config.Server == "" {
		return config, newReleaseExitError(releaseExitUsage, "--server or DISTR_SERVER_URL is required")
	}
	if !strings.HasPrefix(config.Server, "http://") && !strings.HasPrefix(config.Server, "https://") {
		return config, newReleaseExitError(releaseExitUsage, "--server must include http:// or https://")
	}
	if config.Token == "" {
		return config, newReleaseExitError(releaseExitUsage, "--token or DISTR_API_TOKEN is required")
	}
	if config.Output != "text" && config.Output != "json" {
		return config, newReleaseExitError(releaseExitUsage, "--output must be json or text")
	}
	return config, nil
}

func readReleaseCreateBody(file string, stdin io.Reader) ([]byte, error) {
	file = strings.TrimSpace(file)
	if file == "" {
		return nil, newReleaseExitError(releaseExitUsage, "--file is required")
	}
	var data []byte
	var err error
	if file == "-" {
		data, err = io.ReadAll(stdin)
	} else {
		data, err = os.ReadFile(file)
	}
	if err != nil {
		return nil, newReleaseExitError(releaseExitUsage, fmt.Sprintf("failed to read release request: %v", err))
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, newReleaseExitError(releaseExitUsage, "release request file is empty")
	}
	if !json.Valid(data) {
		return nil, newReleaseExitError(releaseExitUsage, "release request must be valid JSON")
	}
	return data, nil
}

func doReleaseAPIRequest(
	ctx context.Context,
	runtime releaseCommandRuntime,
	config releaseCommandConfig,
	method string,
	path string,
	body []byte,
	idempotencyKey string,
	allowValidationFailure bool,
) ([]byte, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		method,
		strings.TrimRight(config.Server, "/")+path,
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, newReleaseExitError(releaseExitUsage, fmt.Sprintf("failed to construct request: %v", err))
	}
	req.Header.Set("Authorization", releaseAuthorizationHeader(config.Token))
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}

	resp, err := runtime.Client.Do(req)
	if err != nil {
		return nil, newReleaseExitError(
			releaseExitAPI,
			redactReleaseSecrets(fmt.Sprintf("request failed: %v", err), config.Token),
		)
	}
	defer resp.Body.Close()

	responseBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, newReleaseExitError(
			releaseExitAPI,
			redactReleaseSecrets(fmt.Sprintf("failed to read response: %v", readErr), config.Token),
		)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return bytes.TrimSpace(responseBody), nil
	}
	if allowValidationFailure && resp.StatusCode == http.StatusBadRequest && isReleaseValidationResponse(responseBody) {
		return bytes.TrimSpace(responseBody), nil
	}

	code := releaseExitAPI
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		code = releaseExitAuth
	} else if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		code = releaseExitUsage
	}
	message := strings.TrimSpace(string(responseBody))
	if message == "" {
		message = http.StatusText(resp.StatusCode)
	}
	message = redactReleaseSecrets(message, config.Token)
	_, _ = fmt.Fprintf(runtime.Stderr, "API request failed with status %d: %s\n", resp.StatusCode, message)
	return nil, newReleaseExitError(code, fmt.Sprintf("API request failed with status %d: %s", resp.StatusCode, message))
}

func releaseAuthorizationHeader(token string) string {
	scheme, _ := releaseCredentialParts(token)
	if scheme != "" {
		return token
	}
	return "AccessToken " + token
}

func redactReleaseSecrets(value string, token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return value
	}
	scheme, credential := releaseCredentialParts(token)
	candidates := []struct {
		value       string
		replacement string
	}{
		{value: token, replacement: "[REDACTED]"},
		{value: credential, replacement: "[REDACTED]"},
		{value: "AccessToken " + credential, replacement: "AccessToken [REDACTED]"},
		{value: "Bearer " + credential, replacement: "Bearer [REDACTED]"},
	}
	if scheme != "" {
		candidates = append(candidates, struct {
			value       string
			replacement string
		}{value: scheme + " " + credential, replacement: scheme + " [REDACTED]"})
	}
	redacted := value
	seen := map[string]struct{}{}
	for _, candidate := range candidates {
		if candidate.value == "" {
			continue
		}
		key := strings.ToLower(candidate.value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		redacted = replaceCaseInsensitive(redacted, candidate.value, candidate.replacement)
	}
	return redacted
}

func releaseCredentialParts(token string) (string, string) {
	fields := strings.Fields(strings.TrimSpace(token))
	if len(fields) >= 2 {
		scheme := strings.ToLower(fields[0])
		if scheme == "bearer" || scheme == "accesstoken" {
			return fields[0], strings.Join(fields[1:], " ")
		}
	}
	return "", strings.TrimSpace(token)
}

func replaceCaseInsensitive(value string, old string, replacement string) string {
	if old == "" {
		return value
	}
	var builder strings.Builder
	remaining := value
	lowerOld := strings.ToLower(old)
	for {
		index := strings.Index(strings.ToLower(remaining), lowerOld)
		if index < 0 {
			builder.WriteString(remaining)
			return builder.String()
		}
		builder.WriteString(remaining[:index])
		builder.WriteString(replacement)
		remaining = remaining[index+len(old):]
	}
}

func isReleaseValidationResponse(response []byte) bool {
	var probe struct {
		Valid *bool `json:"valid"`
	}
	return json.Unmarshal(response, &probe) == nil && probe.Valid != nil
}

func writeReleaseBundleOutput(stdout io.Writer, output string, response []byte) error {
	if output == "json" {
		_, err := fmt.Fprintln(stdout, string(response))
		return err
	}
	var bundle struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(response, &bundle); err != nil {
		return newReleaseExitError(releaseExitAPI, fmt.Sprintf("failed to parse release bundle response: %v", err))
	}
	_, err := fmt.Fprintf(stdout, "Release bundle %s %s\n", bundle.ID, bundle.Status)
	return err
}

func writeReleaseValidationOutput(stdout io.Writer, output string, response []byte) error {
	var result api.ReleaseBundleValidationResponse
	if err := json.Unmarshal(response, &result); err != nil {
		return newReleaseExitError(releaseExitAPI, fmt.Sprintf("failed to parse validation response: %v", err))
	}
	if output == "json" {
		if _, err := fmt.Fprintln(stdout, string(response)); err != nil {
			return err
		}
	} else if result.Valid {
		if _, err := fmt.Fprintln(stdout, "Release bundle validation valid"); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintln(stdout, "Release bundle validation invalid"); err != nil {
			return err
		}
		for _, issue := range result.Errors {
			if _, err := fmt.Fprintf(stdout, "- %s: %s\n", issue.Field, issue.Message); err != nil {
				return err
			}
		}
	}
	if !result.Valid {
		return newReleaseExitError(releaseExitValidation, "release bundle validation failed")
	}
	return nil
}

func newReleaseExitError(code int, message string) error {
	return &releaseExitError{code: code, err: errors.New(message)}
}

func init() {
	RootCommand.AddCommand(NewReleaseCommand())
}
