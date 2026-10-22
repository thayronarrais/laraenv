# Builds pgvector for Windows against a PostgreSQL tree and packages the
# release asset consumed by the lara4ever catalog (KindPgvector):
#
#   pgvector-<version>-pg<major>-win-x64.zip
#     lib\vector.dll, share\extension\*, share\doc\pgvector\LICENSE
#   pgvector-<version>-pg<major>-win-x64.zip.sha256
#
# Upload both to the thayronarrais/laraenv release tagged pgvector-<version>.
#
# Requirements: Visual Studio Build Tools (C++ x64), git, and a PostgreSQL
# binaries tree that includes include\ and lib\ (the EnterpriseDB zip does).
#
# Usage (Windows PowerShell 5.1):
#   .\build-pgvector.ps1                                  # against C:\laraenv\postgres
#   .\build-pgvector.ps1 -PgRoot C:\pg\17 -Version 0.8.6  # against another tree

param(
    [string]$PgRoot  = "C:\laraenv\postgres",
    [ValidatePattern('^\d+\.\d+\.\d+$')]
    [string]$Version = "0.8.6",
    [Alias("OutDir")]
    [string]$OutputDir
)

$ErrorActionPreference = "Stop"

$pgConfig = Join-Path $PgRoot "bin\pg_config.exe"
if (-not (Test-Path $pgConfig)) { throw "pg_config not found at $pgConfig - point -PgRoot at a PostgreSQL binaries tree" }
$pgVersionLine = (& $pgConfig --version)                      # "PostgreSQL 18.3"
if ($LASTEXITCODE -ne 0) { throw "pg_config --version failed" }
$pgMajor = ($pgVersionLine -replace '[^\d]*(\d+).*', '$1')
if ($pgMajor -notmatch '^\d+$') { throw "Cannot determine PostgreSQL major from '$pgVersionLine'" }
if (-not $OutputDir) { $OutputDir = Join-Path $PSScriptRoot "dist\$Version-pg$pgMajor" }
$OutputDir = [System.IO.Path]::GetFullPath($OutputDir)
if (Test-Path -LiteralPath $OutputDir) {
    if (-not (Test-Path -LiteralPath $OutputDir -PathType Container) -or
        (Get-ChildItem -LiteralPath $OutputDir -Force | Select-Object -First 1)) {
        throw "OutputDir must be empty or nonexistent; existing pgvector artifacts are preserved: $OutputDir"
    }
}
Write-Host "Building pgvector $Version against $pgVersionLine (major $pgMajor)"

$vswhere = "${env:ProgramFiles(x86)}\Microsoft Visual Studio\Installer\vswhere.exe"
if (-not (Test-Path $vswhere)) { throw "vswhere.exe not found - install Visual Studio Build Tools" }
$vsPath = & $vswhere -products * -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath | Select-Object -First 1
if (-not $vsPath) { throw "No Visual Studio installation with C++ x64 tools found" }
$vcvars = Join-Path $vsPath "VC\Auxiliary\Build\vcvars64.bat"

$work = Join-Path ([System.IO.Path]::GetTempPath()) "pgvector-build-$Version-pg$pgMajor-$([guid]::NewGuid().ToString('N'))"
$work = [System.IO.Path]::GetFullPath($work)
if ($work.Equals($OutputDir, [System.StringComparison]::OrdinalIgnoreCase) -or
    $work.StartsWith($OutputDir.TrimEnd('\', '/') + [System.IO.Path]::DirectorySeparatorChar, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw "Temporary build staging must be outside OutputDir: $work"
}
if (Test-Path -LiteralPath $work) { throw "Temporary build path already exists: $work" }
Write-Host "Isolated build staging (retained for inspection): $work"
git clone --quiet --depth 1 --branch "v$Version" https://github.com/pgvector/pgvector.git $work
if ($LASTEXITCODE -ne 0) { throw "git clone of pgvector v$Version failed" }

# vcvars64 is a batch file, so the compile runs inside one cmd session.
$previousPgRoot = $env:PGROOT
try {
    $env:PGROOT = $PgRoot
    cmd /c "`"$vcvars`" >nul 2>&1 && cd /d `"$work`" && nmake /NOLOGO /F Makefile.win"
    if ($LASTEXITCODE -ne 0) { throw "nmake build failed" }
} finally { $env:PGROOT = $previousPgRoot }
if (-not (Test-Path (Join-Path $work "vector.dll"))) { throw "build produced no vector.dll" }
if (-not (Test-Path -LiteralPath (Join-Path $work "LICENSE") -PathType Leaf)) {
    throw "Upstream pgvector LICENSE is missing; refusing to create a redistributable ZIP."
}

# Stage the exact layout the installer extracts over C:\laraenv\postgres.
$stage = Join-Path $work "stage"
New-Item -ItemType Directory -Path (Join-Path $stage "lib") | Out-Null
New-Item -ItemType Directory -Path (Join-Path $stage "share\extension") | Out-Null
New-Item -ItemType Directory -Path (Join-Path $stage "share\doc\pgvector") | Out-Null
Copy-Item (Join-Path $work "vector.dll") (Join-Path $stage "lib")
Copy-Item (Join-Path $work "vector.control") (Join-Path $stage "share\extension")
Copy-Item (Join-Path $work "sql\vector--*.sql") (Join-Path $stage "share\extension")
Copy-Item -LiteralPath (Join-Path $work "LICENSE") -Destination (Join-Path $stage "share\doc\pgvector\LICENSE")

if (Test-Path -LiteralPath $OutputDir) {
    if (Get-ChildItem -LiteralPath $OutputDir -Force | Select-Object -First 1) {
        throw "OutputDir changed during the build; refusing to overwrite artifacts: $OutputDir"
    }
} else { New-Item -ItemType Directory -Path $OutputDir | Out-Null }
$zipName = "pgvector-$Version-pg$pgMajor-win-x64.zip"
$zipPath = Join-Path $OutputDir $zipName
Compress-Archive -Path (Join-Path $stage "*") -DestinationPath $zipPath

$hash = (Get-FileHash $zipPath -Algorithm SHA256).Hash.ToLower()
$checksumStream = [System.IO.File]::Open("$zipPath.sha256", [System.IO.FileMode]::CreateNew)
$checksumWriter = [System.IO.StreamWriter]::new($checksumStream, [System.Text.Encoding]::ASCII)
try { $checksumWriter.WriteLine("$hash  $zipName") } finally { $checksumWriter.Dispose() }

Write-Host "OK: $zipPath"
Write-Host "OK: $zipPath.sha256"
Write-Host "Upload both to: https://github.com/thayronarrais/laraenv/releases/tag/pgvector-$Version"
