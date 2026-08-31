[CmdletBinding()]
param(
    [ValidateRange(0, [int]::MaxValue)]
    [int]$FromMigration = 138,

    [ValidateRange(0, [int]::MaxValue)]
    [int]$ToMigration = 167,

    [string]$DatabaseUrl,

    [string]$OutputPath = 'work/control-plane-migration-matrix-report.json',

    [ValidateSet('16.14', '18.4')]
    [string]$ExpectedPostgresVersion = '18.4',

    [switch]$PlanOnly
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

$ReportSchema = 'distr.control-plane-migration-matrix-report/v1'
$RepositoryRoot = Split-Path -Parent $PSScriptRoot
$MigrationDirectory = Join-Path $RepositoryRoot 'internal/migrations/sql'
$MaximumDiagnosticCharacters = 4096
$SafeDatabaseNamePattern = '(?i)(test|testing|ci|fixture|sandbox|control[-_]?plane)'
$AllowedQueryKeys = @('application_name', 'connect_timeout', 'sslmode')
$MatrixJwtSecret = ''
$ScenarioIds = @(
    'postgres-runtime-version',
    'migration-138-to-167-upgrade',
    'clean-install',
    'single-step-down-and-refusal-contracts',
    'checkpoint-idempotency-and-cursor-resume',
    'v1-flags-off',
    'mixed-v1-v2',
    'v2-history-flags-off',
    'upstream-compatibility'
)
$ScenarioTests = [ordered]@{
    'single-step-down-and-refusal-contracts' = @(
        'TestRunnerRefusesHistoricalMigrationBeforeEngineConstruction',
        'TestRunnerRefusesZeroHistoryDownCrossingWithActiveTimestampRows',
        'TestExecutionV2DowngradeRefusesRetainedV2Tasks',
        'TestAdapterAssignmentDowngradeRefusesAnyAdapterData',
        'TestMigration166CreatesImmutableNativeBaselineAdoption',
        'TestExecutionRuntimeTrustMigrationPreservesLegacyRowsAndRequiresCompleteV3Shape',
        'TestExecutionRuntimeTrustMigrationCreatesBoundedAppendOnlyAttemptEvidence',
        'TestExecutionRuntimeTrustDowngradeLocksAndRefusesRetainedV3Evidence'
    )
    'checkpoint-idempotency-and-cursor-resume' = @(
        'TestTargetConfigV1ExtractionRepositoryDryRunApplyRestartAndNoOp',
        'TestBackfillLegacyDeploymentCompatibilityProcessesMultipleBatchesAndCanResume',
        'TestReleaseBackfillInvocationProcessesOneBatchAndAdvancesNextCursor'
    )
    'v1-flags-off' = @(
        'TestV1TaskCreationFlagsOffRemainsUngatedAndEventCompatible',
        'TestParseReleaseContractPreservesV1Shape',
        'TestValidateProtocolV1RequiresCompatibleSteps',
        'TestExistingLegacyTasksRemainIdempotentAfterExecution'
    )
    'mixed-v1-v2' = @(
        'TestTargetConfigV1ExtractionIgnoresV2SourcesInMixedHistory',
        'TestParseReleaseContractV2StrictlyDispatchesBySchema',
        'TestBuildTargetPlanGraphHasReachableProtocolV1Projection',
        'TestValidatePlanDraftRejectsV1IncompatibleResolution'
    )
    'v2-history-flags-off' = @(
        'TestExistingTargetPlanV2TasksReplayAfterExecution',
        'TestDeploymentPlansFeatureFlagMiddlewareRejectsDisabledAPI',
        'TestDeploymentCampaignSchedulerRegistrationRequiresBothControlPlaneFlags',
        'TestExecutionV2ReadyStepRecoveryRegistrationRequiresBothControlPlaneFlags',
        'TestV1TaskCreationFlagsOffRemainsUngatedAndEventCompatible'
    )
    'upstream-compatibility' = @(
        'TestCanonicalizeOmitsEmptySourceMetadataForCompatibility',
        'TestParseReleaseContractPreservesV1Shape',
        'TestV1TaskCreationFlagsOffRemainsUngatedAndEventCompatible',
        'TestDeploymentRegistryRoutedResourceFamiliesAuthorizationAndCompatibility'
    )
}

function Get-Sha256Text {
    param([Parameter(Mandatory)][AllowEmptyString()][string]$Text)

    $bytes = [System.Text.Encoding]::UTF8.GetBytes($Text)
    $hash = [System.Security.Cryptography.SHA256]::HashData($bytes)
    return 'sha256:' + [Convert]::ToHexString($hash).ToLowerInvariant()
}

function Get-Sha256File {
    param([Parameter(Mandatory)][string]$Path)

    return 'sha256:' + (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
}

function Get-RedactedText {
    param(
        [AllowNull()][AllowEmptyString()][string]$Text,
        [AllowNull()][string]$Password
    )

    if ([string]::IsNullOrEmpty($Text)) {
        return ''
    }

    $redacted = $Text -replace '(?i)\bpostgres(?:ql)?://[^\s''"]+', '[REDACTED_DATABASE_URL]'
    $redacted = $redacted -replace '(?i)\b(password|secret|token|api[-_]?key)\s*[:=]\s*[^\s;,]+', '$1=[REDACTED]'
    if (-not [string]::IsNullOrEmpty($Password)) {
        $redacted = $redacted -replace [Regex]::Escape($Password), '[REDACTED_SECRET]'
    }
    if (-not [string]::IsNullOrEmpty($script:MatrixJwtSecret)) {
        $redacted = $redacted -replace [Regex]::Escape($script:MatrixJwtSecret), '[REDACTED_SECRET]'
    }
    return $redacted
}

function Get-BoundedDiagnostic {
    param([AllowNull()][AllowEmptyString()][string]$Text)

    if ([string]::IsNullOrEmpty($Text)) {
        return ''
    }
    $trimmed = $Text.Trim()
    if ($trimmed.Length -gt $MaximumDiagnosticCharacters) {
        return $trimmed.Substring(0, $MaximumDiagnosticCharacters) + '[TRUNCATED]'
    }
    return $trimmed
}

function ConvertFrom-QueryString {
    param([AllowNull()][string]$Query)

    $result = [ordered]@{}
    $trimmed = if ($null -eq $Query) { '' } else { $Query.TrimStart('?') }
    if ([string]::IsNullOrWhiteSpace($trimmed)) {
        return $result
    }

    foreach ($pair in $trimmed.Split('&', [StringSplitOptions]::RemoveEmptyEntries)) {
        $parts = $pair.Split('=', 2)
        $key = [Uri]::UnescapeDataString($parts[0]).ToLowerInvariant()
        $value = if ($parts.Count -eq 2) { [Uri]::UnescapeDataString($parts[1]) } else { '' }
        if ([string]::IsNullOrWhiteSpace($key) -or $result.Contains($key)) {
            throw 'database URL query keys must be non-empty and unique'
        }
        if ($AllowedQueryKeys -notcontains $key) {
            throw "database URL query key '$key' is not allowed"
        }
        $result[$key] = $value
    }
    return $result
}

function Test-LoopbackHost {
    param([Parameter(Mandatory)][string]$HostName)

    if ($HostName.Equals('localhost', [StringComparison]::OrdinalIgnoreCase)) {
        return $true
    }

    $address = $null
    if ([System.Net.IPAddress]::TryParse($HostName, [ref]$address)) {
        return [System.Net.IPAddress]::IsLoopback($address)
    }
    return $false
}

function Get-SafeDatabaseIdentity {
    param([Parameter(Mandatory)][string]$Url)

    if ([string]::IsNullOrWhiteSpace($Url)) {
        throw 'DatabaseUrl is required and must name an isolated test database'
    }

    $uri = $null
    if (-not [Uri]::TryCreate($Url, [UriKind]::Absolute, [ref]$uri)) {
        throw 'DatabaseUrl must be an absolute PostgreSQL URL'
    }
    if ($uri.Scheme -notin @('postgres', 'postgresql')) {
        throw 'DatabaseUrl must use postgres or postgresql'
    }
    if (-not [string]::IsNullOrEmpty($uri.Fragment)) {
        throw 'DatabaseUrl must not contain a fragment'
    }
    if (-not (Test-LoopbackHost -HostName $uri.Host)) {
        throw 'DatabaseUrl must use localhost or a loopback IP address'
    }
    if ($uri.Port -lt 1 -or $uri.Port -gt 65535) {
        throw 'DatabaseUrl must contain a valid port'
    }

    $databaseName = [Uri]::UnescapeDataString($uri.AbsolutePath.TrimStart('/'))
    if (
        [string]::IsNullOrWhiteSpace($databaseName) -or
        $databaseName.Contains('/') -or
        $databaseName -notmatch '^[A-Za-z0-9_-]+$' -or
        $databaseName -notmatch $SafeDatabaseNamePattern
    ) {
        throw 'DatabaseUrl database name must be an explicit test, CI, fixture, sandbox, or control-plane database'
    }

    $userInfo = $uri.UserInfo.Split(':', 2)
    $userName = if ($userInfo.Count -ge 1) { [Uri]::UnescapeDataString($userInfo[0]) } else { '' }
    $password = if ($userInfo.Count -eq 2) { [Uri]::UnescapeDataString($userInfo[1]) } else { '' }
    if ([string]::IsNullOrWhiteSpace($userName)) {
        throw 'DatabaseUrl must include an explicit test database user'
    }

    $query = ConvertFrom-QueryString -Query $uri.Query
    return [ordered]@{
        scheme         = $uri.Scheme.ToLowerInvariant()
        host           = $uri.Host.ToLowerInvariant()
        port           = $uri.Port
        database       = $databaseName
        user           = $userName
        password       = $password
        passwordPresent = -not [string]::IsNullOrEmpty($password)
        sslMode        = if ($query.Contains('sslmode')) { $query['sslmode'] } else { '' }
        connectTimeout = if ($query.Contains('connect_timeout')) { $query['connect_timeout'] } else { '' }
        applicationName = if ($query.Contains('application_name')) { $query['application_name'] } else { '' }
    }
}

function Get-MigrationInventory {
    if (-not (Test-Path -LiteralPath $MigrationDirectory -PathType Container)) {
        throw 'migration directory is missing'
    }
    if ($FromMigration -gt $ToMigration) {
        throw 'FromMigration must be less than or equal to ToMigration'
    }

    $rows = [System.Collections.Generic.List[object]]::new()
    for ($version = $FromMigration; $version -le $ToMigration; $version++) {
        $up = @(Get-ChildItem -LiteralPath $MigrationDirectory -File -Filter "${version}_*.up.sql")
        $down = @(Get-ChildItem -LiteralPath $MigrationDirectory -File -Filter "${version}_*.down.sql")
        if ($up.Count -ne 1 -or $down.Count -ne 1) {
            throw "migration $version must have exactly one up and one down file"
        }
        $rows.Add([ordered]@{
            version    = $version
            upFile     = $up[0].Name
            upSha256   = Get-Sha256File -Path $up[0].FullName
            downFile   = $down[0].Name
            downSha256 = Get-Sha256File -Path $down[0].FullName
        })
    }
    return $rows
}

function Assert-RequiredTestInventory {
    $testFiles = @(Get-ChildItem -LiteralPath (Join-Path $RepositoryRoot 'internal') -Recurse -File -Filter '*_test.go')
    if ($testFiles.Count -eq 0) {
        throw 'Go test inventory is empty'
    }
    $source = ($testFiles | ForEach-Object { Get-Content -LiteralPath $_.FullName -Raw }) -join "`n"
    foreach ($scenarioId in $ScenarioTests.Keys) {
        foreach ($testName in $ScenarioTests[$scenarioId]) {
            if ($source -notmatch ('(?m)^func\s+' + [Regex]::Escape($testName) + '\s*\(')) {
                throw "required $scenarioId test '$testName' is missing"
            }
        }
    }
}

function Get-ScenarioTestPattern {
    param([Parameter(Mandatory)][string]$ScenarioId)

    if (-not $ScenarioTests.Contains($ScenarioId)) {
        throw "scenario '$ScenarioId' has no pinned test inventory"
    }
    return '^(' + (($ScenarioTests[$ScenarioId] | ForEach-Object { [Regex]::Escape($_) }) -join '|') + ')$'
}

function Get-SourceMetadata {
    $commit = 'unknown'
    $dirty = $true
    try {
        $commit = (& git -C $RepositoryRoot rev-parse HEAD 2>$null).Trim()
        $status = & git -C $RepositoryRoot status --porcelain --untracked-files=no 2>$null
        $dirty = -not [string]::IsNullOrWhiteSpace(($status -join "`n"))
    } catch {
        $commit = 'unknown'
        $dirty = $true
    }
    return [ordered]@{ commit = $commit; workingTreeDirty = $dirty }
}

function Get-ExternalCommand {
    param([Parameter(Mandatory)][string]$Name)

    $commands = @(Get-Command $Name -CommandType Application -ErrorAction SilentlyContinue)
    if ($commands.Count -eq 0) {
        throw "required command '$Name' is unavailable"
    }
    return $commands[0].Source
}

function Invoke-ExternalCommand {
    param(
        [Parameter(Mandatory)][string]$FilePath,
        [Parameter(Mandatory)][string[]]$Arguments,
        [Parameter(Mandatory)][string]$Description,
        [AllowNull()][string]$Password
    )

    $started = [DateTimeOffset]::UtcNow
    $output = ''
    $exitCode = -1
    try {
        $output = (& $FilePath @Arguments 2>&1 | Out-String)
        $exitCode = $LASTEXITCODE
    } catch {
        $output = $_.Exception.Message
        $exitCode = -1
    }
    $duration = [Math]::Round(([DateTimeOffset]::UtcNow - $started).TotalMilliseconds, 3)
    $redacted = Get-RedactedText -Text $output -Password $Password
    $result = [ordered]@{
        description  = $Description
        exitCode     = $exitCode
        durationMs   = $duration
        outputSha256 = Get-Sha256Text -Text $redacted
        output       = $redacted
        diagnostic   = Get-BoundedDiagnostic -Text $redacted
    }
    return $result
}

function Invoke-CheckedExternalCommand {
    param(
        [Parameter(Mandatory)][AllowEmptyCollection()][System.Collections.Generic.List[object]]$Checks,
        [Parameter(Mandatory)][string]$FilePath,
        [Parameter(Mandatory)][string[]]$Arguments,
        [Parameter(Mandatory)][string]$Description,
        [AllowNull()][string]$Password
    )

    $result = Invoke-ExternalCommand -FilePath $FilePath -Arguments $Arguments `
        -Description $Description -Password $Password
    $Checks.Add($result)
    if ($result.exitCode -ne 0) {
        throw [System.InvalidOperationException]::new(
            "$Description failed with exit code $($result.exitCode)`n$($result.diagnostic)"
        )
    }
    return $result
}

function Invoke-MatrixScenario {
    param(
        [Parameter(Mandatory)][string]$Id,
        [Parameter(Mandatory)][scriptblock]$Operation,
        [Parameter(Mandatory)][AllowEmptyString()][string]$Password
    )

    $started = [DateTimeOffset]::UtcNow
    $checks = [System.Collections.Generic.List[object]]::new()
    $status = 'PASS'
    $diagnostic = ''
    try {
        & $Operation $checks
    } catch {
        $status = 'FAIL'
        $diagnostic = Get-BoundedDiagnostic -Text (
            Get-RedactedText -Text $_.Exception.Message -Password $Password
        )
    }
    return [ordered]@{
        id         = $Id
        status     = $status
        startedAt  = $started.ToString('o')
        durationMs = [Math]::Round(([DateTimeOffset]::UtcNow - $started).TotalMilliseconds, 3)
        checks     = $checks
        diagnostic = $diagnostic
    }
}

function Set-ProcessEnvironment {
    param(
        [Parameter(Mandatory)][string]$Name,
        [AllowNull()][string]$Value,
        [Parameter(Mandatory)][hashtable]$Original
    )

    if (-not $Original.ContainsKey($Name)) {
        $Original[$Name] = [Environment]::GetEnvironmentVariable($Name, 'Process')
    }
    [Environment]::SetEnvironmentVariable($Name, $Value, 'Process')
}

function Add-SchemaToDatabaseUrl {
    param(
        [Parameter(Mandatory)][string]$Url,
        [Parameter(Mandatory)][string]$Schema
    )

    $separator = if ($Url.Contains('?')) { '&' } else { '?' }
    return $Url + $separator + 'search_path=' + [Uri]::EscapeDataString($Schema)
}

function Write-ReportAtomic {
    param(
        [Parameter(Mandatory)][System.Collections.IDictionary]$Report,
        [Parameter(Mandatory)][string]$Path
    )

    $resolved = [IO.Path]::GetFullPath((Join-Path $RepositoryRoot $Path))
    $workRoot = [IO.Path]::GetFullPath((Join-Path $RepositoryRoot 'work'))
    if (-not $resolved.StartsWith($workRoot + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
        throw 'OutputPath must stay below the repository work directory'
    }
    if (Test-Path -LiteralPath $resolved) {
        throw 'OutputPath already exists; evidence reports are never overwritten'
    }

    $directory = Split-Path -Parent $resolved
    New-Item -ItemType Directory -Path $directory -Force | Out-Null
    $withoutChecksum = $Report | ConvertTo-Json -Depth 20 -Compress
    $Report['reportChecksum'] = Get-Sha256Text -Text $withoutChecksum
    $json = $Report | ConvertTo-Json -Depth 20
    $temporary = Join-Path $directory ('.control-plane-migration-matrix-' + [Guid]::NewGuid().ToString('N') + '.tmp')
    try {
        [IO.File]::WriteAllText($temporary, $json + [Environment]::NewLine, [Text.UTF8Encoding]::new($false))
        Move-Item -LiteralPath $temporary -Destination $resolved
    } finally {
        if (Test-Path -LiteralPath $temporary) {
            Remove-Item -LiteralPath $temporary -Force
        }
    }
    return $resolved
}

$database = Get-SafeDatabaseIdentity -Url $DatabaseUrl
$migrationInventory = Get-MigrationInventory
Assert-RequiredTestInventory
$source = Get-SourceMetadata
$startedAt = [DateTimeOffset]::UtcNow
$scenarioResults = [System.Collections.Generic.List[object]]::new()
$createdSchemas = [System.Collections.Generic.List[string]]::new()
$droppedSchemas = [System.Collections.Generic.List[string]]::new()
$cleanupResults = [System.Collections.Generic.List[object]]::new()
$environmentOriginals = @{}

$staticScenario = [ordered]@{
    id         = 'migration-file-integrity'
    status     = 'PASS'
    startedAt  = $startedAt.ToString('o')
    durationMs = 0
    checks     = @([ordered]@{
        description = "exact migration pairs $FromMigration through $ToMigration"
        count       = $migrationInventory.Count
        checksum    = Get-Sha256Text -Text ($migrationInventory | ConvertTo-Json -Depth 5 -Compress)
    })
    diagnostic = ''
}
$scenarioResults.Add($staticScenario)

if ($PlanOnly) {
    foreach ($scenarioId in $ScenarioIds) {
        $scenarioResults.Add([ordered]@{
            id         = $scenarioId
            status     = 'PLANNED'
            startedAt  = $startedAt.ToString('o')
            durationMs = 0
            checks     = @()
            diagnostic = 'PlanOnly: no go, psql, database, or network operation was attempted'
        })
    }
} else {
    $go = $null
    $psql = $null
    $prerequisiteError = ''
    try {
        $go = Get-ExternalCommand -Name 'go'
        $psql = Get-ExternalCommand -Name 'psql'
    } catch {
        $prerequisiteError = Get-BoundedDiagnostic -Text (
            Get-RedactedText -Text $_.Exception.Message -Password $database.password
        )
    }

    if (-not [string]::IsNullOrEmpty($prerequisiteError)) {
        foreach ($scenarioId in $ScenarioIds) {
            $scenarioResults.Add([ordered]@{
                id         = $scenarioId
                status     = 'BLOCKED'
                startedAt  = [DateTimeOffset]::UtcNow.ToString('o')
                durationMs = 0
                checks     = @()
                diagnostic = $prerequisiteError
            })
        }
    } else {
        Set-ProcessEnvironment -Name 'PGHOST' -Value $database.host -Original $environmentOriginals
        Set-ProcessEnvironment -Name 'PGPORT' -Value ([string]$database.port) -Original $environmentOriginals
        Set-ProcessEnvironment -Name 'PGDATABASE' -Value $database.database -Original $environmentOriginals
        Set-ProcessEnvironment -Name 'PGUSER' -Value $database.user -Original $environmentOriginals
        Set-ProcessEnvironment -Name 'PGPASSWORD' -Value $database.password -Original $environmentOriginals
        Set-ProcessEnvironment -Name 'PGSSLMODE' -Value $database.sslMode -Original $environmentOriginals
        Set-ProcessEnvironment -Name 'PGCONNECT_TIMEOUT' -Value $database.connectTimeout -Original $environmentOriginals
        Set-ProcessEnvironment -Name 'PGAPPNAME' -Value $database.applicationName -Original $environmentOriginals
        Set-ProcessEnvironment -Name 'DISTR_TEST_DATABASE_URL' -Value $DatabaseUrl -Original $environmentOriginals

        Push-Location $RepositoryRoot
        try {
        $randomJwtBytes = [byte[]]::new(48)
        [System.Security.Cryptography.RandomNumberGenerator]::Fill($randomJwtBytes)
        $script:MatrixJwtSecret = [Convert]::ToBase64String($randomJwtBytes)
        Set-ProcessEnvironment -Name 'JWT_SECRET' -Value $script:MatrixJwtSecret `
            -Original $environmentOriginals
        Set-ProcessEnvironment -Name 'DISTR_HOST' -Value 'http://127.0.0.1' `
            -Original $environmentOriginals
        $newSchema = {
            return 'cp_matrix_' + [Guid]::NewGuid().ToString('N')
        }
        $createSchema = {
            param($checks, $schema)
            $check = Invoke-CheckedExternalCommand -Checks $checks -FilePath $psql -Arguments @(
                '--no-psqlrc', '--set', 'ON_ERROR_STOP=1', '--command', "CREATE SCHEMA `"$schema`""
            ) -Description "create isolated schema $schema" -Password $database.password
            $createdSchemas.Add($schema)
        }
        $runMigration = {
            param($checks, $schema, [string[]]$arguments, $description)
            Set-ProcessEnvironment -Name 'DATABASE_URL' -Value (
                Add-SchemaToDatabaseUrl -Url $DatabaseUrl -Schema $schema
            ) -Original $environmentOriginals
            $null = Invoke-CheckedExternalCommand -Checks $checks -FilePath $go -Arguments (
                @('run', './cmd/hub', 'migrate') + $arguments
            ) -Description $description -Password $database.password
        }
        $runGoTest = {
            param($checks, [string[]]$packages, $pattern, $description, $flags)
            Set-ProcessEnvironment -Name 'DISTR_EXPERIMENTAL_FEATURE_FLAGS' -Value $flags -Original $environmentOriginals
            $null = Invoke-CheckedExternalCommand -Checks $checks -FilePath $go -Arguments (
                @('test') + $packages + @('-count=1', '-run', $pattern)
            ) -Description $description -Password $database.password
        }

        $scenarioResults.Add((Invoke-MatrixScenario -Id 'postgres-runtime-version' -Password $database.password -Operation {
            param($checks)
            $check = Invoke-CheckedExternalCommand -Checks $checks -FilePath $psql -Arguments @(
                '--no-psqlrc', '--tuples-only', '--no-align', '--command', 'SHOW server_version'
            ) -Description "verify PostgreSQL runtime version $ExpectedPostgresVersion" -Password $database.password
            $observed = $check.output.Trim()
            if ($observed -ne $ExpectedPostgresVersion) {
                throw "expected PostgreSQL $ExpectedPostgresVersion but found $observed"
            }
        }))

        $scenarioResults.Add((Invoke-MatrixScenario -Id 'migration-138-to-167-upgrade' -Password $database.password -Operation {
            param($checks)
            $schema = & $newSchema
            & $createSchema $checks $schema
            & $runMigration $checks $schema @('--to', "$FromMigration") "migrate empty isolated schema to $FromMigration"
            & $runMigration $checks $schema @('--to', "$ToMigration") "upgrade isolated schema from $FromMigration to $ToMigration"
            & $runMigration $checks $schema @('--check') 'verify upgraded schema preflight'
        }))

        $scenarioResults.Add((Invoke-MatrixScenario -Id 'clean-install' -Password $database.password -Operation {
            param($checks)
            $schema = & $newSchema
            & $createSchema $checks $schema
            & $runMigration $checks $schema @('--to', "$ToMigration") "clean install through migration $ToMigration"
            & $runMigration $checks $schema @('--check') 'verify clean-install schema preflight'
        }))

        $scenarioResults.Add((Invoke-MatrixScenario -Id 'single-step-down-and-refusal-contracts' -Password $database.password -Operation {
            param($checks)
            $schema = & $newSchema
            & $createSchema $checks $schema
            & $runMigration $checks $schema @('--to', "$ToMigration") "prepare schema at migration $ToMigration"
            & $runMigration $checks $schema @('--to', "$($ToMigration - 1)") 'exercise one safe down migration'
            & $runMigration $checks $schema @('--to', "$ToMigration") 'restore the safe-down schema'
            & $runGoTest $checks @('./internal/migrations', './internal/db') (
                Get-ScenarioTestPattern -ScenarioId 'single-step-down-and-refusal-contracts'
            ) 'exercise pinned downgrade refusal contract tests' ''
        }))

        $scenarioResults.Add((Invoke-MatrixScenario -Id 'checkpoint-idempotency-and-cursor-resume' -Password $database.password -Operation {
            param($checks)
            & $runGoTest $checks @('./internal/db', './internal/targetconfig') (
                Get-ScenarioTestPattern -ScenarioId 'checkpoint-idempotency-and-cursor-resume'
            ) 'exercise checkpoint idempotency and cursor resume tests' 'operator_control_plane_v2'
        }))

        $scenarioResults.Add((Invoke-MatrixScenario -Id 'v1-flags-off' -Password $database.password -Operation {
            param($checks)
            & $runGoTest $checks @('./internal/db', './internal/releasebundles', './internal/planning') (
                Get-ScenarioTestPattern -ScenarioId 'v1-flags-off'
            ) 'exercise v1 behavior with control-plane flags off' ''
        }))

        $scenarioResults.Add((Invoke-MatrixScenario -Id 'mixed-v1-v2' -Password $database.password -Operation {
            param($checks)
            & $runGoTest $checks @('./internal/db', './internal/releasebundles', './internal/planning') (
                Get-ScenarioTestPattern -ScenarioId 'mixed-v1-v2'
            ) 'exercise exact mixed v1/v2 dispatch and policy' 'operator_control_plane_v2,executor_protocol_v2'
        }))

        $scenarioResults.Add((Invoke-MatrixScenario -Id 'v2-history-flags-off' -Password $database.password -Operation {
            param($checks)
            & $runGoTest $checks @('./internal/db', './internal/handlers', './internal/svc') (
                Get-ScenarioTestPattern -ScenarioId 'v2-history-flags-off'
            ) 'exercise retained v2 history with flags off and unchanged v1 behavior' ''
        }))

        $scenarioResults.Add((Invoke-MatrixScenario -Id 'upstream-compatibility' -Password $database.password -Operation {
            param($checks)
            & $runGoTest $checks @('./internal/releasebundles', './internal/planning', './internal/db', './internal/handlers') (
                Get-ScenarioTestPattern -ScenarioId 'upstream-compatibility'
            ) 'exercise current upstream-compatible v1 contracts' ''
        }))
        } finally {
            foreach ($schema in $createdSchemas) {
                try {
                    $null = Invoke-CheckedExternalCommand -Checks $cleanupResults -FilePath $psql -Arguments @(
                        '--no-psqlrc', '--set', 'ON_ERROR_STOP=1', '--command', "DROP SCHEMA IF EXISTS `"$schema`" CASCADE"
                    ) -Description "drop isolated schema $schema" -Password $database.password
                    $droppedSchemas.Add($schema)
                } catch {
                    Write-Warning (Get-BoundedDiagnostic -Text (
                        Get-RedactedText -Text $_.Exception.Message -Password $database.password
                    ))
                }
            }
            Pop-Location
            foreach ($name in $environmentOriginals.Keys) {
                [Environment]::SetEnvironmentVariable($name, $environmentOriginals[$name], 'Process')
            }
            $script:MatrixJwtSecret = ''
        }
    }
}

$status = if ($PlanOnly) {
    'PLANNED'
} elseif (
    @($scenarioResults | Where-Object { $_.status -ne 'PASS' }).Count -eq 0 -and
    $createdSchemas.Count -eq $droppedSchemas.Count
) {
    'PASS'
} else {
    'FAIL'
}

$versionScenario = $scenarioResults | Where-Object { $_.id -eq 'postgres-runtime-version' } | Select-Object -First 1
$observedPostgresVersion = if (
    $null -ne $versionScenario -and
    $versionScenario.status -eq 'PASS' -and
    $versionScenario.checks.Count -eq 1
) {
    $versionScenario.checks[0].output.Trim()
} else {
    ''
}

$report = [ordered]@{
    schemaVersion = $ReportSchema
    status        = $status
    planOnly      = [bool]$PlanOnly
    startedAt     = $startedAt.ToString('o')
    completedAt   = [DateTimeOffset]::UtcNow.ToString('o')
    source        = $source
    range         = [ordered]@{
        from = $FromMigration
        to   = $ToMigration
    }
    database      = [ordered]@{
        scheme          = $database.scheme
        host            = $database.host
        port            = $database.port
        name            = $database.database
        user            = $database.user
        passwordPresent = $database.passwordPresent
        sslMode         = $database.sslMode
        expectedServerVersion = $ExpectedPostgresVersion
        observedServerVersion = $observedPostgresVersion
    }
    migrationFiles = $migrationInventory
    scenarios      = $scenarioResults
    coverage       = [ordered]@{
        schemaUpgrade = [ordered]@{
            from = $FromMigration
            to   = $ToMigration
        }
        schemaDown = [ordered]@{
            mode = 'single-step'
            from = $ToMigration
            to   = $ToMigration - 1
        }
        checkpoint = 'idempotency-and-cursor-resume-tests'
        notExecuted = @(
            'process-interruption-and-restart',
            'binary-rollback'
        )
    }
    integrity      = [ordered]@{
        algorithm       = 'sha256'
        encoding        = 'utf8'
        serialization   = 'compact-json-preserving-property-order'
        scope           = 'complete-report-excluding-reportChecksum'
        commandEvidence = 'complete-redacted-output'
    }
    cleanup        = [ordered]@{
        attemptedSchemas = $createdSchemas.Count
        droppedSchemas   = $droppedSchemas.Count
        complete         = $createdSchemas.Count -eq $droppedSchemas.Count
        checks           = $cleanupResults
    }
}

$written = Write-ReportAtomic -Report $report -Path $OutputPath
Write-Output $written
if ($status -eq 'FAIL') {
    exit 1
}
