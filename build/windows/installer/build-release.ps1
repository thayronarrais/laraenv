# Builds the MSI, portable ZIP and updater executable with their sidecars.
# Requires Node >=24, the pinned Go toolchain, and WiX CLI/UI/Util 5.0.2.
# Existing release artifacts are never cleaned or overwritten.
param(
    [ValidatePattern('^\d+\.\d+\.\d+$')]
    [string]$Version = "0.1.0",
    [switch]$SkipBuild,
    [switch]$SkipLangSync,
    [string]$OutputDir,
    # Package an existing executable without building or syncing translations.
    # This is also the entry point for isolated packaging fixtures.
    [string]$SourceExe
)

$ErrorActionPreference = "Stop"
$scriptDir = $PSScriptRoot
$projectRoot = [System.IO.Path]::GetFullPath((Join-Path $scriptDir "..\..\.."))
$binDir = Join-Path $projectRoot "build\bin"
$portableSrc = Join-Path $projectRoot "build\windows\portable"
$wxsPath = Join-Path $scriptDir "laraenv.wxs"
if (-not $OutputDir) { $OutputDir = Join-Path $projectRoot "build\release\$Version" }
$releaseDir = [System.IO.Path]::GetFullPath($OutputDir)

function Assert-EmptyOutputDirectory {
    param([Parameter(Mandatory)][string]$Path)
    if (Test-Path -LiteralPath $Path) {
        if (-not (Test-Path -LiteralPath $Path -PathType Container)) {
            throw "OutputDir is not a directory: $Path"
        }
        if (Get-ChildItem -LiteralPath $Path -Force | Select-Object -First 1) {
            throw "OutputDir must be empty or nonexistent; existing artifacts are preserved: $Path"
        }
    }
}

function Copy-NewFile {
    param([Parameter(Mandatory)][string]$Source, [Parameter(Mandatory)][string]$Destination)
    # File.Copy(..., false) also refuses overwrites if another process wins a race.
    [System.IO.File]::Copy($Source, $Destination, $false)
}

function New-Sha256Sidecar {
    param([Parameter(Mandatory)][string]$Path)
    $sidecar = "$Path.sha256"
    $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $Path).Hash.ToLowerInvariant()
    $stream = [System.IO.File]::Open($sidecar, [System.IO.FileMode]::CreateNew)
    $writer = [System.IO.StreamWriter]::new($stream, [System.Text.Encoding]::ASCII)
    try { $writer.Write("$hash  $(Split-Path -Leaf $Path)") } finally { $writer.Dispose() }
    return $sidecar
}

Assert-EmptyOutputDirectory -Path $releaseDir
if (-not (Get-Command node -ErrorAction SilentlyContinue)) { throw "Node >=24 is required for release notice checks." }

# Check the authoritative notices before any build, translation sync or staging.
Push-Location $projectRoot
try {
    & node tools/licenses/generate.mjs --check
    if ($LASTEXITCODE -ne 0) { throw "Dependency notices are missing or stale. Run node tools/licenses/generate.mjs and review the result." }
    & node tools/licenses/installer-rtf.mjs --check
    if ($LASTEXITCODE -ne 0) { throw "Installer license is stale. Run node tools/licenses/installer-rtf.mjs and review the result." }
} finally { Pop-Location }

$licensePayload = @(
    "LICENSE", "NOTICE", "THIRD_PARTY_NOTICES.txt", "third_party\dependencies.json",
    "third_party\sources\wix-5.0.2-source.zip"
)
foreach ($relative in $licensePayload) {
    if (-not (Test-Path -LiteralPath (Join-Path $projectRoot $relative) -PathType Leaf)) {
        throw "Missing required release license payload: $relative"
    }
}

if (-not (Get-Command wix -ErrorAction SilentlyContinue)) {
    throw "WiX 5.0.2 CLI is required. Provision it separately with: dotnet tool install --global wix --version 5.0.2"
}
$wixVersion = (& wix --version 2>&1 | Out-String).Trim()
if ($LASTEXITCODE -ne 0 -or $wixVersion -notmatch '^5\.0\.2(?:\+|$)') {
    throw "WiX CLI 5.0.2 is required; found '$wixVersion'. Release preparation does not modify tool installations."
}
$globalExtensions = (& wix extension list --global 2>&1 | Out-String)
if ($LASTEXITCODE -ne 0) { throw "Cannot inspect the WiX global extension cache." }
foreach ($extension in @("WixToolset.UI.wixext", "WixToolset.Util.wixext")) {
    if ($globalExtensions -notmatch "(?m)^$([regex]::Escape($extension))\s+5\.0\.2\s*$") {
        throw "Missing prerequisite $extension/5.0.2. Provision it separately with: wix extension add --global $extension/5.0.2"
    }
}

# Match the actual compiled resources to the audited official packages, not
# only the version labels reported by the extension cache.
$noticePolicy = Get-Content -LiteralPath (Join-Path $projectRoot "tools\licenses\policy.json") -Raw | ConvertFrom-Json
foreach ($package in $noticePolicy.installer.packages) {
    $extensionDll = Join-Path $env:USERPROFILE ".wix\extensions\$($package.name)\$($noticePolicy.installer.version)\wixext5\$($package.name).dll"
    if (-not (Test-Path -LiteralPath $extensionDll -PathType Leaf) -or
        (Get-FileHash -LiteralPath $extensionDll -Algorithm SHA256).Hash.ToLowerInvariant() -ne $package.dllSha256) {
        throw "WiX extension bytes differ from the reviewed license/source policy: $($package.name)"
    }
}

if ($SourceExe) {
    $exePath = [System.IO.Path]::GetFullPath($SourceExe)
    $SkipBuild = $true
    $SkipLangSync = $true
} elseif ($SkipBuild) {
    $exePath = Join-Path $binDir "LaraEnv.exe"
} else {
    # A unique Wails output name preserves any previous build\bin\LaraEnv.exe.
    $buildName = "LaraEnv-$Version-$([guid]::NewGuid().ToString('N')).exe"
    $exePath = Join-Path $binDir $buildName
    if (Test-Path -LiteralPath $exePath) { throw "Build output already exists: $exePath" }
}
if ($SkipBuild -and -not (Test-Path -LiteralPath $exePath -PathType Leaf)) {
    throw "Source executable not found: $exePath"
}

if (-not $SkipLangSync) {
    Push-Location $projectRoot
    try {
        & node tools/langs/pull.mjs
        if ($LASTEXITCODE -ne 0) { throw "translation pull failed" }
        & node tools/langs/check.mjs langs
        if ($LASTEXITCODE -ne 0) { throw "translation catalogues failed validation" }
    } finally { Pop-Location }
}
if (-not $SkipBuild) {
    Push-Location $projectRoot
    try {
        # Build/run the CLI against this checkout's fixed tool graph rather
        # than an older standalone global Wails installation.
        & go run -mod=readonly github.com/wailsapp/wails/v2/cmd/wails build -nosyncgomod -platform windows/amd64 -webview2 browser -o $buildName -ldflags "-X lara4ever/internal/version.Version=$Version"
        if ($LASTEXITCODE -ne 0) { throw "wails build failed" }
    } finally { Pop-Location }
}
if (-not (Test-Path -LiteralPath $exePath -PathType Leaf)) { throw "Built executable not found: $exePath" }

function New-InstallerAssets {
    param([Parameter(Mandatory)][string]$ProjectRoot, [Parameter(Mandatory)][string]$Destination)
    Add-Type -AssemblyName System.Drawing
    $logo = [System.Drawing.Image]::FromFile((Join-Path $ProjectRoot "frontend\logo_laraenv.png"))
    $icon = [System.Drawing.Image]::FromFile((Join-Path $ProjectRoot "build\appicon.png"))
    try {
        foreach ($spec in @(
            @{ Name = "WixDialog.bmp"; Width = 493; Height = 312; Image = $logo; Padding = 36 },
            @{ Name = "WixBanner.bmp"; Width = 493; Height = 58; Image = $icon; Padding = 4 }
        )) {
            $bitmap = [System.Drawing.Bitmap]::new($spec.Width, $spec.Height, [System.Drawing.Imaging.PixelFormat]::Format24bppRgb)
            $graphics = [System.Drawing.Graphics]::FromImage($bitmap)
            try {
                $graphics.Clear([System.Drawing.Color]::White)
                $graphics.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
                $scale = [Math]::Min(($spec.Width - 2 * $spec.Padding) / $spec.Image.Width, ($spec.Height - 2 * $spec.Padding) / $spec.Image.Height)
                $width = [int][Math]::Round($spec.Image.Width * $scale)
                $height = [int][Math]::Round($spec.Image.Height * $scale)
                $left = if ($spec.Name -eq "WixBanner.bmp") { $spec.Width - $width - 12 } else { [int](($spec.Width - $width) / 2) }
                $graphics.DrawImage($spec.Image, $left, [int](($spec.Height - $height) / 2), $width, $height)
                $bitmap.Save((Join-Path $Destination $spec.Name), [System.Drawing.Imaging.ImageFormat]::Bmp)
            } finally { $graphics.Dispose(); $bitmap.Dispose() }
        }
        # Windows supports a PNG image inside an ICO directory. Generate this
        # from the same owned appicon source instead of reusing legacy artwork.
        $iconBitmap = [System.Drawing.Bitmap]::new(256, 256)
        $graphics = [System.Drawing.Graphics]::FromImage($iconBitmap)
        $png = [System.IO.MemoryStream]::new()
        try {
            $graphics.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
            $graphics.DrawImage($icon, 0, 0, 256, 256)
            $iconBitmap.Save($png, [System.Drawing.Imaging.ImageFormat]::Png)
            $bytes = $png.ToArray()
            $stream = [System.IO.File]::Open((Join-Path $Destination "LaraEnv.ico"), [System.IO.FileMode]::CreateNew)
            $writer = [System.IO.BinaryWriter]::new($stream)
            try {
                $writer.Write([uint16]0); $writer.Write([uint16]1); $writer.Write([uint16]1)
                $writer.Write([byte]0); $writer.Write([byte]0); $writer.Write([byte]0); $writer.Write([byte]0)
                $writer.Write([uint16]1); $writer.Write([uint16]32)
                $writer.Write([uint32]$bytes.Length); $writer.Write([uint32]22); $writer.Write($bytes)
            } finally { $writer.Dispose() }
        } finally { $graphics.Dispose(); $iconBitmap.Dispose(); $png.Dispose() }
    } finally { $logo.Dispose(); $icon.Dispose() }
}

$stageRoot = Join-Path ([System.IO.Path]::GetTempPath()) "laraenv-release-$Version-$([guid]::NewGuid().ToString('N'))"
$stageRoot = [System.IO.Path]::GetFullPath($stageRoot)
if ($stageRoot.Equals($releaseDir, [System.StringComparison]::OrdinalIgnoreCase) -or
    $stageRoot.StartsWith($releaseDir.TrimEnd('\', '/') + [System.IO.Path]::DirectorySeparatorChar, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw "Temporary staging must be outside OutputDir: $stageRoot"
}
New-Item -ItemType Directory -Path $stageRoot | Out-Null
Write-Host "Isolated staging (retained for inspection): $stageRoot"
New-InstallerAssets -ProjectRoot $projectRoot -Destination $stageRoot
$stagedMsi = Join-Path $stageRoot "LaraEnv-$Version.msi"
& wix build -arch x64 -pdbtype none `
    -ext WixToolset.UI.wixext/5.0.2 -ext WixToolset.Util.wixext/5.0.2 `
    -d "ProductVersion=$Version" -d "SourceExe=$exePath" `
    -d "ProjectRoot=$projectRoot" -d "InstallerAssets=$stageRoot" `
    -o $stagedMsi $wxsPath
if ($LASTEXITCODE -ne 0) { throw "wix build failed; staging is retained at $stageRoot" }

$portableDir = Join-Path $stageRoot "LaraEnv"
New-Item -ItemType Directory -Path $portableDir | Out-Null
New-Item -ItemType Directory -Path (Join-Path $portableDir "third_party") | Out-Null
New-Item -ItemType Directory -Path (Join-Path $portableDir "third_party\sources") | Out-Null
Copy-NewFile -Source $exePath -Destination (Join-Path $portableDir "LaraEnv.exe")
foreach ($file in @(".laraenv-portable", "README.txt")) {
    Copy-NewFile -Source (Join-Path $portableSrc $file) -Destination (Join-Path $portableDir $file)
}
foreach ($relative in $licensePayload) {
    Copy-NewFile -Source (Join-Path $projectRoot $relative) -Destination (Join-Path $portableDir $relative)
}
$stagedZip = Join-Path $stageRoot "LaraEnv-$Version-portable-win64.zip"
Compress-Archive -LiteralPath $portableDir -DestinationPath $stagedZip

# Publishable outputs are copied only after both packages have succeeded.
Assert-EmptyOutputDirectory -Path $releaseDir
if (-not (Test-Path -LiteralPath $releaseDir)) { New-Item -ItemType Directory -Path $releaseDir | Out-Null }
$artifacts = @()
foreach ($item in @(
    @{ Source = $stagedMsi; Name = "LaraEnv-$Version.msi" },
    @{ Source = $stagedZip; Name = "LaraEnv-$Version-portable-win64.zip" },
    @{ Source = $exePath; Name = "LaraEnv.exe" }
)) {
    $destination = Join-Path $releaseDir $item.Name
    Copy-NewFile -Source $item.Source -Destination $destination
    $artifacts += $destination
    $artifacts += New-Sha256Sidecar -Path $destination
}
Write-Host "Release artifacts in $releaseDir" -ForegroundColor Green
$artifacts | ForEach-Object { Write-Host "  $(Split-Path -Leaf $_)" -ForegroundColor Green }
