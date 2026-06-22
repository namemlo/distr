package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

const (
	fileRenderAllowedRootsEnv = "DISTR_FILE_RENDER_ALLOWED_ROOTS"
	fileRenderTempPrefix      = ".distr-file-render-"
	fileRenderPrivateTempMode = 0o600
)

var fileRenderVariableNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var fileRenderBeforeAtomicWriteForTest func(fileRenderDestination)
var fileRenderBeforeBackupForTest func(fileRenderDestination)
var fileRenderBeforeExistingMetadataForTest func(fileRenderDestination)
var fileRenderBeforeTempRenameForTest func(fileRenderDestination, string)
var fileRenderApplyMetadataForTest func(*os.File, os.FileInfo, os.FileMode, string, string) error

type fileRenderActionInput struct {
	DestinationPath string            `json:"destinationPath"`
	Template        string            `json:"template"`
	Variables       map[string]string `json:"variables"`
	SecretVariables map[string]string `json:"secretVariables"`
	Mode            string            `json:"mode"`
	Owner           string            `json:"owner"`
	Group           string            `json:"group"`
	Backup          bool              `json:"backup"`
	IdempotencyKey  string            `json:"idempotencyKey"`
	TimeoutSeconds  int               `json:"timeoutSeconds"`
}

type fileRenderResult struct {
	DestinationPath string
	BackupPath      string
	Changed         bool
}

type fileRenderDestination struct {
	rootPath     string
	relativePath string
	absolutePath string
	root         *os.Root
}

func (d fileRenderDestination) close() {
	if d.root != nil {
		_ = d.root.Close()
	}
}

func decodeFileRenderActionInput(inputs map[string]any) (fileRenderActionInput, error) {
	var input fileRenderActionInput
	data, err := json.Marshal(inputs)
	if err != nil {
		return input, fmt.Errorf("encode file render inputs: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return input, fmt.Errorf("decode file render inputs: %w", err)
	}
	normalizeFileRenderActionInput(&input)
	if err := validateFileRenderActionInput(input); err != nil {
		return input, err
	}
	return input, nil
}

func normalizeFileRenderActionInput(input *fileRenderActionInput) {
	input.DestinationPath = strings.TrimSpace(input.DestinationPath)
	input.Mode = strings.TrimSpace(input.Mode)
	input.Owner = strings.TrimSpace(input.Owner)
	input.Group = strings.TrimSpace(input.Group)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.Variables == nil {
		input.Variables = map[string]string{}
	}
	if input.SecretVariables == nil {
		input.SecretVariables = map[string]string{}
	}
}

func validateFileRenderActionInput(input fileRenderActionInput) error {
	if _, err := fileRenderAllowedRoots(); err != nil {
		return err
	}
	if err := validateFileRenderDestinationPath(input.DestinationPath); err != nil {
		return err
	}
	if err := validateFileRenderVariables("variables", input.Variables); err != nil {
		return err
	}
	if err := validateFileRenderVariables("secretVariables", input.SecretVariables); err != nil {
		return err
	}
	for name := range input.SecretVariables {
		if _, exists := input.Variables[name]; exists {
			return fmt.Errorf("variables conflict with secretVariables")
		}
	}
	if _, err := parseFileRenderMode(input.Mode, len(input.SecretVariables) > 0); err != nil {
		return err
	}
	if _, _, err := parseFileRenderOwnerGroup(input.Owner, input.Group); err != nil {
		return err
	}
	if input.TimeoutSeconds < 0 {
		return fmt.Errorf("timeoutSeconds must be greater than or equal to 0")
	}
	if _, err := renderFileTemplate(input.Template, input.Variables, input.SecretVariables); err != nil {
		return err
	}
	return nil
}

func validateFileRenderVariables(field string, values map[string]string) error {
	for name, value := range values {
		name = strings.TrimSpace(name)
		if name == "" {
			return fmt.Errorf("%s name is required", field)
		}
		if !fileRenderVariableNamePattern.MatchString(name) {
			return fmt.Errorf("%s name %q is invalid", field, name)
		}
		if strings.ContainsRune(value, 0) {
			return fmt.Errorf("%s %q contains unsupported characters", field, name)
		}
	}
	return nil
}

func validateFileRenderDestinationPath(destinationPath string) error {
	if strings.TrimSpace(destinationPath) == "" {
		return fmt.Errorf("destinationPath is required")
	}
	if filepath.IsAbs(destinationPath) || strings.HasPrefix(destinationPath, "/") || strings.HasPrefix(destinationPath, `\`) {
		return fmt.Errorf("destinationPath must be relative")
	}
	parts := strings.FieldsFunc(destinationPath, func(r rune) bool { return r == '/' || r == '\\' })
	for _, part := range parts {
		if part == ".." {
			return fmt.Errorf("destinationPath cannot contain traversal")
		}
	}
	cleaned := filepath.Clean(destinationPath)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return fmt.Errorf("destinationPath cannot contain traversal")
	}
	return nil
}

func executeFileRenderStep(
	ctx context.Context,
	lease api.AgentTaskLease,
	step api.AgentTaskLeaseStep,
	client leasedTaskClient,
) error {
	sequence := int64(1)
	if err := recordStepEvent(ctx, client, step.StepRunID, lease.LeaseToken, sequence, types.StepRunEventTypeStarted, "starting file render", nil, nil); err != nil {
		return err
	}
	secretValues := []string(nil)
	recordFailure := func(err error) error {
		sequence++
		redactedErr := redactErrorWithSecretValues(err, secretValues)
		if recordErr := recordStepEvent(ctx, client, step.StepRunID, lease.LeaseToken, sequence, types.StepRunEventTypeFailed, redactedErr.Error(), nil, nil, secretValues...); recordErr != nil {
			return redactErrorWithSecretValues(recordErr, secretValues)
		}
		return redactedErr
	}
	if step.ActionType != fileRenderActionType {
		return recordFailure(fmt.Errorf("unsupported actionType %q", step.ActionType))
	}
	if step.ActionVersion != types.AgentActionVersionV1 {
		return recordFailure(fmt.Errorf("unsupported actionVersion %q", step.ActionVersion))
	}
	input, err := decodeFileRenderActionInput(step.Inputs)
	if err != nil {
		return recordFailure(err)
	}
	if input.IdempotencyKey == "" {
		input.IdempotencyKey = strings.TrimSpace(step.IdempotencyKey)
	}
	secretValues = fileRenderSecretValues(input)
	renderCtx, renderCancel := context.WithCancel(ctx)
	if input.TimeoutSeconds > 0 {
		renderCtx, renderCancel = context.WithTimeout(ctx, time.Duration(input.TimeoutSeconds)*time.Second)
	}
	defer renderCancel()
	heartbeatErrCh, stopHeartbeat := startTaskLeaseHeartbeat(renderCtx, lease, client, renderCancel)

	var progressErr error
	var progressErrMu sync.Mutex
	updateStatus := func(status string) {
		status = strings.TrimSpace(status)
		if status == "" {
			return
		}
		progressErrMu.Lock()
		defer progressErrMu.Unlock()
		if progressErr != nil {
			return
		}
		sequence++
		progressErr = recordStepEvent(renderCtx, client, step.StepRunID, lease.LeaseToken, sequence, types.StepRunEventTypeProgress, status, nil, nil, secretValues...)
		if progressErr != nil {
			renderCancel()
		}
	}
	result, err := runFileRender(renderCtx, input, updateStatus)
	stopHeartbeat()
	progressErrMu.Lock()
	callbackErr := progressErr
	progressErrMu.Unlock()
	if callbackErr != nil {
		return redactErrorWithSecretValues(callbackErr, secretValues)
	}
	if heartbeatErr := taskLeaseHeartbeatError(heartbeatErrCh); heartbeatErr != nil {
		return recordFailure(heartbeatErr)
	}
	if err != nil {
		return recordFailure(err)
	}
	sequence++
	outputs := []api.AgentStepRunOutputRequest{
		{Name: "destinationPath", Value: result.DestinationPath},
		{Name: "changed", Value: result.Changed},
	}
	if result.BackupPath != "" {
		outputs = append(outputs, api.AgentStepRunOutputRequest{Name: "backupPath", Value: result.BackupPath})
	}
	return recordStepEvent(ctx, client, step.StepRunID, lease.LeaseToken, sequence, types.StepRunEventTypeSucceeded, "File render succeeded", nil, outputs, secretValues...)
}

func runFileRender(ctx context.Context, input fileRenderActionInput, updateStatus func(string)) (fileRenderResult, error) {
	if updateStatus != nil {
		updateStatus("rendering file")
	}
	if err := fileRenderContextError(ctx); err != nil {
		return fileRenderResult{}, err
	}
	rendered, err := renderFileTemplate(input.Template, input.Variables, input.SecretVariables)
	if err != nil {
		return fileRenderResult{}, err
	}
	return writeRenderedFile(ctx, input, []byte(rendered))
}

func renderFileTemplate(template string, variables, secretVariables map[string]string) (string, error) {
	if strings.Contains(template, "{{") || strings.Contains(template, "}}") || strings.Contains(template, "`") {
		return "", fmt.Errorf("unsupported template syntax")
	}
	var builder strings.Builder
	cursor := 0
	for {
		startRel := strings.Index(template[cursor:], "${")
		if startRel < 0 {
			builder.WriteString(template[cursor:])
			break
		}
		start := cursor + startRel
		endRel := strings.Index(template[start+2:], "}")
		if endRel < 0 {
			return "", fmt.Errorf("unsupported template syntax")
		}
		end := start + 2 + endRel
		builder.WriteString(template[cursor:start])
		expression := template[start+2 : end]
		value, err := fileRenderTemplateValue(expression, variables, secretVariables)
		if err != nil {
			return "", err
		}
		builder.WriteString(value)
		cursor = end + 1
	}
	return builder.String(), nil
}

func fileRenderTemplateValue(expression string, variables, secretVariables map[string]string) (string, error) {
	if strings.TrimSpace(expression) != expression || expression == "" {
		return "", fmt.Errorf("unsupported template syntax")
	}
	source := variables
	name := expression
	secret := false
	if strings.HasPrefix(expression, "secrets.") {
		secret = true
		name = strings.TrimPrefix(expression, "secrets.")
		source = secretVariables
	} else if strings.Contains(expression, ".") {
		return "", fmt.Errorf("unsupported template syntax")
	}
	if !fileRenderVariableNamePattern.MatchString(name) {
		return "", fmt.Errorf("unsupported template syntax")
	}
	value, ok := source[name]
	if !ok {
		if secret {
			return "", fmt.Errorf("template secret variable %q is not provided", name)
		}
		return "", fmt.Errorf("template variable %q is not provided", name)
	}
	return value, nil
}

func writeRenderedFile(ctx context.Context, input fileRenderActionInput, data []byte) (fileRenderResult, error) {
	destination, err := prepareFileRenderDestination(input.DestinationPath)
	if err != nil {
		return fileRenderResult{}, err
	}
	defer destination.close()
	result := fileRenderResult{DestinationPath: destination.absolutePath}
	if err := fileRenderContextError(ctx); err != nil {
		return result, err
	}
	mode, err := parseFileRenderMode(input.Mode, len(input.SecretVariables) > 0)
	if err != nil {
		return result, err
	}
	if existingFile, existingInfo, err := openExistingFileNoSwap(destination); err == nil {
		existing, readErr := io.ReadAll(existingFile)
		if readErr != nil {
			_ = existingFile.Close()
			return result, fmt.Errorf("read existing destination: %w", readErr)
		}
		if bytes.Equal(existing, data) {
			if fileRenderBeforeExistingMetadataForTest != nil {
				fileRenderBeforeExistingMetadataForTest(destination)
			}
			if err := verifyExistingFileNoSwap(destination, existingInfo); err != nil {
				_ = existingFile.Close()
				return result, err
			}
			if fileRenderMetadataAlreadyMatches(existingInfo, mode, input.Owner, input.Group) {
				_ = existingFile.Close()
				return result, nil
			}
			_ = existingFile.Close()
			if err := atomicWriteFileRenderDestination(ctx, destination, data, mode, input.Owner, input.Group); err != nil {
				return result, err
			}
			result.Changed = true
			return result, nil
		}
		_ = existingFile.Close()
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return result, fmt.Errorf("read existing destination: %w", err)
	}
	if input.Backup {
		backupPath, err := backupFileRenderDestination(ctx, destination, mode)
		if err != nil {
			return result, err
		}
		result.BackupPath = backupPath
	}
	if err := atomicWriteFileRenderDestination(ctx, destination, data, mode, input.Owner, input.Group); err != nil {
		return result, err
	}
	result.Changed = true
	return result, nil
}

func prepareFileRenderDestination(destinationPath string) (fileRenderDestination, error) {
	if err := validateFileRenderDestinationPath(destinationPath); err != nil {
		return fileRenderDestination{}, err
	}
	roots, err := fileRenderAllowedRoots()
	if err != nil {
		return fileRenderDestination{}, err
	}
	root := roots[0]
	relativePath := filepath.Clean(destinationPath)
	rootHandle, err := os.OpenRoot(root)
	if err != nil {
		return fileRenderDestination{}, fmt.Errorf("open file render root: %w", err)
	}
	destination := fileRenderDestination{
		rootPath:     root,
		relativePath: relativePath,
		absolutePath: filepath.Clean(filepath.Join(root, relativePath)),
		root:         rootHandle,
	}
	parent := filepath.Dir(relativePath)
	if parent != "." {
		if err := rootHandle.MkdirAll(parent, 0o700); err != nil {
			destination.close()
			return fileRenderDestination{}, fmt.Errorf("destinationPath escapes allowlisted root: %w", err)
		}
	}
	info, err := rootHandle.Lstat(relativePath)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			destination.close()
			return fileRenderDestination{}, fmt.Errorf("destinationPath cannot be a symlink")
		}
		if info.IsDir() {
			destination.close()
			return fileRenderDestination{}, fmt.Errorf("destinationPath cannot be a directory")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		destination.close()
		return fileRenderDestination{}, fmt.Errorf("inspect destinationPath: %w", err)
	}
	return destination, nil
}

func fileRenderAllowedRoots() ([]string, error) {
	raw := strings.TrimSpace(os.Getenv(fileRenderAllowedRootsEnv))
	if raw == "" {
		return nil, fmt.Errorf("%s is required", fileRenderAllowedRootsEnv)
	}
	roots := []string{}
	for _, root := range strings.Split(raw, ",") {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		if !filepath.IsAbs(root) {
			return nil, fmt.Errorf("%s entries must be absolute paths", fileRenderAllowedRootsEnv)
		}
		resolved, err := filepath.EvalSymlinks(root)
		if err != nil {
			return nil, fmt.Errorf("resolve %s entry symlinks: %w", fileRenderAllowedRootsEnv, err)
		}
		roots = append(roots, filepath.Clean(resolved))
	}
	if len(roots) == 0 {
		return nil, fmt.Errorf("%s is required", fileRenderAllowedRootsEnv)
	}
	return roots, nil
}

func backupFileRenderDestination(ctx context.Context, destination fileRenderDestination, mode os.FileMode) (string, error) {
	if fileRenderBeforeBackupForTest != nil {
		fileRenderBeforeBackupForTest(destination)
	}
	source, info, err := openExistingFileNoSwap(destination)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("inspect existing destination for backup: %w", err)
	}
	defer source.Close()
	backupMode := moreRestrictiveFileRenderMode(info.Mode().Perm(), mode)
	backupRelativePath := destination.relativePath + ".bak"
	if err := copyFileAtomic(ctx, destination, source, backupRelativePath, backupMode); err != nil {
		return "", fmt.Errorf("back up existing destination: %w", err)
	}
	return destination.absolutePath + ".bak", nil
}

func moreRestrictiveFileRenderMode(existingMode, targetMode os.FileMode) os.FileMode {
	return existingMode.Perm() & targetMode.Perm()
}

func copyFileAtomic(ctx context.Context, destination fileRenderDestination, source *os.File, backupRelativePath string, mode os.FileMode) error {
	if err := fileRenderContextError(ctx); err != nil {
		return err
	}
	tempRelativePath := fileRenderTempPath(filepath.Dir(backupRelativePath), "backup")
	temp, err := destination.root.OpenFile(tempRelativePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, fileRenderPrivateTempMode)
	if err != nil {
		return err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = destination.root.Remove(tempRelativePath)
		}
	}()
	if err := temp.Chmod(fileRenderPrivateTempMode); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := io.Copy(temp, source); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	tempInfo, err := temp.Stat()
	if err != nil {
		_ = temp.Close()
		return err
	}
	if fileRenderBeforeTempRenameForTest != nil {
		fileRenderBeforeTempRenameForTest(destination, tempRelativePath)
	}
	if err := temp.Chmod(mode); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := fileRenderContextError(ctx); err != nil {
		_ = temp.Close()
		return err
	}
	if err := verifyRootRegularFileNoSwap(destination.root, tempRelativePath, "temporary destination", tempInfo); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := destination.root.Rename(tempRelativePath, backupRelativePath); err != nil {
		return err
	}
	cleanup = false
	return nil
}
func atomicWriteFileRenderDestination(ctx context.Context, destination fileRenderDestination, data []byte, mode os.FileMode, owner, group string) error {
	if err := fileRenderContextError(ctx); err != nil {
		return err
	}
	if fileRenderBeforeAtomicWriteForTest != nil {
		fileRenderBeforeAtomicWriteForTest(destination)
	}
	tempRelativePath := fileRenderTempPath(filepath.Dir(destination.relativePath), "write")
	temp, err := destination.root.OpenFile(tempRelativePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, fileRenderPrivateTempMode)
	if err != nil {
		return fmt.Errorf("destinationPath escapes allowlisted root: create temporary destination file: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = destination.root.Remove(tempRelativePath)
		}
	}()
	if err := temp.Chmod(fileRenderPrivateTempMode); err != nil {
		_ = temp.Close()
		return fmt.Errorf("chmod temporary destination file: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write temporary destination file: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("sync temporary destination file: %w", err)
	}
	tempInfo, err := temp.Stat()
	if err != nil {
		_ = temp.Close()
		return fmt.Errorf("stat temporary destination file: %w", err)
	}
	if fileRenderBeforeTempRenameForTest != nil {
		fileRenderBeforeTempRenameForTest(destination, tempRelativePath)
	}
	if err := applyFileRenderMetadataToFile(temp, tempInfo, mode, owner, group); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("sync temporary destination metadata: %w", err)
	}
	if err := fileRenderContextError(ctx); err != nil {
		_ = temp.Close()
		return err
	}
	if err := verifyRootRegularFileNoSwap(destination.root, tempRelativePath, "temporary destination", tempInfo); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary destination file: %w", err)
	}
	if err := destination.root.Rename(tempRelativePath, destination.relativePath); err != nil {
		return fmt.Errorf("rename temporary destination file: %w", err)
	}
	cleanup = false
	renderedFile, _, err := openRootRegularFileMatching(destination.root, destination.relativePath, "destinationPath", tempInfo)
	if err != nil {
		return err
	}
	_ = renderedFile.Close()
	return nil
}
func fileRenderTempPath(parent, purpose string) string {
	name := fileRenderTempPrefix + purpose + "-" + uuid.NewString()
	if parent == "." {
		return name
	}
	return filepath.Join(parent, name)
}

func openExistingFileNoSwap(destination fileRenderDestination) (*os.File, os.FileInfo, error) {
	return openRootRegularFileNoSwap(destination.root, destination.relativePath, "destinationPath")
}

func openRootRegularFileNoSwap(root *os.Root, relativePath, label string) (*os.File, os.FileInfo, error) {
	info, err := root.Lstat(relativePath)
	if err != nil {
		return nil, nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, fmt.Errorf("%s cannot be a symlink", label)
	}
	if !info.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("%s must be a regular file", label)
	}
	file, err := root.Open(relativePath)
	if err != nil {
		return nil, nil, err
	}
	openedInfo, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	if !os.SameFile(info, openedInfo) {
		_ = file.Close()
		return nil, nil, fmt.Errorf("%s changed during file render", label)
	}
	return file, info, nil
}

func verifyExistingFileNoSwap(destination fileRenderDestination, originalInfo os.FileInfo) error {
	return verifyRootRegularFileNoSwap(destination.root, destination.relativePath, "destinationPath", originalInfo)
}

func verifyRootRegularFileNoSwap(root *os.Root, relativePath, label string, originalInfo os.FileInfo) error {
	info, err := root.Lstat(relativePath)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s cannot be a symlink", label)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s must be a regular file", label)
	}
	if !os.SameFile(originalInfo, info) {
		return fmt.Errorf("%s changed during file render", label)
	}
	return nil
}

func openRootRegularFileMatching(root *os.Root, relativePath, label string, expectedInfo os.FileInfo) (*os.File, os.FileInfo, error) {
	file, info, err := openRootRegularFileNoSwap(root, relativePath, label)
	if err != nil {
		return nil, nil, err
	}
	if !os.SameFile(expectedInfo, info) {
		_ = file.Close()
		return nil, nil, fmt.Errorf("%s changed during file render", label)
	}
	return file, info, nil
}

func fileRenderMetadataAlreadyMatches(info os.FileInfo, mode os.FileMode, owner, group string) bool {
	if !fileRenderModeAlreadyMatches(info.Mode().Perm(), mode.Perm()) {
		return false
	}
	return fileRenderOwnerGroupAlreadyMatches(info, owner, group)
}

func fileRenderModeAlreadyMatches(current, desired os.FileMode) bool {
	if runtime.GOOS == "windows" {
		return current&0o200 == desired&0o200
	}
	return current == desired
}

func fileRenderOwnerGroupAlreadyMatches(info os.FileInfo, owner, group string) bool {
	owner = strings.TrimSpace(owner)
	group = strings.TrimSpace(group)
	if owner == "" && group == "" {
		return true
	}
	if runtime.GOOS == "windows" {
		return true
	}
	uid, gid, err := parseFileRenderOwnerGroup(owner, group)
	if err != nil {
		return false
	}
	stat := reflect.ValueOf(info.Sys())
	if stat.Kind() == reflect.Pointer {
		if stat.IsNil() {
			return false
		}
		stat = stat.Elem()
	}
	if stat.Kind() != reflect.Struct {
		return false
	}
	if uid >= 0 && !fileRenderMetadataIDMatches(stat.FieldByName("Uid"), uid) {
		return false
	}
	if gid >= 0 && !fileRenderMetadataIDMatches(stat.FieldByName("Gid"), gid) {
		return false
	}
	return true
}

func fileRenderMetadataIDMatches(value reflect.Value, expected int) bool {
	if !value.IsValid() {
		return false
	}
	switch value.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return value.Int() == int64(expected)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return value.Uint() == uint64(expected)
	default:
		return false
	}
}

func applyFileRenderMetadataToFile(file *os.File, info os.FileInfo, mode os.FileMode, owner, group string) error {
	if fileRenderApplyMetadataForTest != nil {
		return fileRenderApplyMetadataForTest(file, info, mode, owner, group)
	}
	if err := file.Chmod(mode); err != nil {
		return fmt.Errorf("chmod rendered file: %w", err)
	}
	return applyFileRenderOwnerGroupToFile(file, owner, group)
}

func applyFileRenderOwnerGroupToFile(file *os.File, owner, group string) error {
	uid, gid, err := parseFileRenderOwnerGroup(owner, group)
	if err != nil {
		return err
	}
	if uid < 0 && gid < 0 {
		return nil
	}
	if runtime.GOOS == "windows" {
		return nil
	}
	if err := file.Chown(uid, gid); err != nil {
		return fmt.Errorf("chown rendered file: %w", err)
	}
	return nil
}

func parseFileRenderMode(value string, hasSecretVariables bool) (os.FileMode, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		if hasSecretVariables {
			return 0o600, nil
		}
		return 0o644, nil
	}
	if !regexp.MustCompile(`^0?[0-7]{3}$`).MatchString(value) {
		return 0, fmt.Errorf("mode must be an octal file mode")
	}
	parsed, err := strconv.ParseUint(value, 8, 32)
	if err != nil {
		return 0, fmt.Errorf("mode must be an octal file mode")
	}
	if parsed > 0o777 {
		return 0, fmt.Errorf("mode must be an octal file mode")
	}
	return os.FileMode(parsed), nil
}

func parseFileRenderOwnerGroup(owner, group string) (int, int, error) {
	uid := -1
	gid := -1
	if strings.TrimSpace(owner) != "" {
		parsed, err := strconv.Atoi(strings.TrimSpace(owner))
		if err != nil || parsed < 0 {
			return -1, -1, fmt.Errorf("owner must be a numeric user id")
		}
		uid = parsed
	}
	if strings.TrimSpace(group) != "" {
		parsed, err := strconv.Atoi(strings.TrimSpace(group))
		if err != nil || parsed < 0 {
			return -1, -1, fmt.Errorf("group must be a numeric group id")
		}
		gid = parsed
	}
	return uid, gid, nil
}

func fileRenderContextError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("file render timed out")
		}
		return fmt.Errorf("file render canceled")
	}
	return nil
}

func fileRenderSecretValues(input fileRenderActionInput) []string {
	values := make([]string, 0, len(input.SecretVariables))
	for _, value := range input.SecretVariables {
		if value != "" {
			values = append(values, value)
		}
	}
	return values
}
