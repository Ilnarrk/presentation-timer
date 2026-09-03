# Signs Windows binaries with Authenticode (SHA256 + RFC3161 timestamp).
# Prefer calling this after `wails build --nsis` instead of NSIS !finalize.

param(
    [Parameter(Mandatory = $true)]
    [string]$PfxPath,
    [Parameter(Mandatory = $true)]
    [string]$Password,
    [string]$BinDir = "",
    [string]$CerOut = "",
    [string]$TimestampUrl = "http://timestamp.digicert.com",
    [switch]$ExportOnly
)

$ErrorActionPreference = "Stop"

if (-not $BinDir) {
    $BinDir = Join-Path (Split-Path -Parent $PSScriptRoot) "bin"
}

if (-not (Test-Path -LiteralPath $PfxPath)) {
    throw "PFX not found: $PfxPath"
}

function Find-SignTool {
    $cmd = Get-Command signtool.exe -ErrorAction SilentlyContinue
    if ($cmd) {
        return $cmd.Source
    }

    $kitRoots = @(
        "${env:ProgramFiles(x86)}\Windows Kits\10\bin",
        "${env:ProgramFiles}\Windows Kits\10\bin"
    )
    foreach ($root in $kitRoots) {
        if (-not (Test-Path $root)) {
            continue
        }
        $found = Get-ChildItem -Path $root -Recurse -Filter signtool.exe -ErrorAction SilentlyContinue |
            Where-Object { $_.FullName -match '\\x64\\signtool\.exe$' } |
            Sort-Object FullName -Descending |
            Select-Object -First 1
        if ($found) {
            return $found.FullName
        }
    }

    throw "signtool.exe not found. Install Windows 10/11 SDK."
}

function Export-PublicCer([string]$Pfx, [string]$Pass, [string]$OutPath) {
    $secure = ConvertTo-SecureString -String $Pass -AsPlainText -Force
    $cert = [System.Security.Cryptography.X509Certificates.X509Certificate2]::new(
        (Resolve-Path $Pfx),
        $secure,
        [System.Security.Cryptography.X509Certificates.X509KeyStorageFlags]::EphemeralKeySet
    )
    try {
        $bytes = $cert.Export([System.Security.Cryptography.X509Certificates.X509ContentType]::Cert)
        [System.IO.File]::WriteAllBytes($OutPath, $bytes)
    }
    finally {
        $cert.Dispose()
    }
}

function Write-PublicCer([string]$OutPath) {
    $cerDir = Split-Path -Parent $OutPath
    if ($cerDir -and -not (Test-Path $cerDir)) {
        New-Item -ItemType Directory -Force -Path $cerDir | Out-Null
    }
    Export-PublicCer -Pfx $PfxPath -Pass $Password -OutPath $OutPath
    Write-Host "Public certificate: $OutPath"
}

$windowsCer = Join-Path $PSScriptRoot "codesign.cer"
if (-not $CerOut) {
    if ($ExportOnly) {
        $CerOut = $windowsCer
    } else {
        $CerOut = Join-Path $BinDir "codesign.cer"
    }
}

function Sync-WindowsCer([string]$FromPath) {
    $fromFull = [IO.Path]::GetFullPath($FromPath)
    $winFull = [IO.Path]::GetFullPath($windowsCer)
    if ($fromFull -ne $winFull) {
        Copy-Item -LiteralPath $FromPath -Destination $windowsCer -Force
    }
}

if ($ExportOnly) {
    Write-PublicCer $CerOut
    Sync-WindowsCer $CerOut
    return
}

function Get-SignTargets([string]$Dir) {
    $targets = [System.Collections.Generic.List[string]]::new()

    $primary = Join-Path $Dir "presentation-timer.exe"
    if (Test-Path -LiteralPath $primary) {
        $targets.Add((Resolve-Path -LiteralPath $primary).Path)
    }

    Get-ChildItem -Path $Dir -Filter "*.exe" -ErrorAction SilentlyContinue | ForEach-Object {
        if ($_.Name -match '-installer\.exe$') {
            $targets.Add($_.FullName)
            return
        }
        if ($_.Name -like 'presentation-timer*.exe' -and $_.Name -notmatch '-installer\.exe$') {
            $targets.Add($_.FullName)
            return
        }
        if ($_.Name -eq 'wailsapp.exe') {
            $targets.Add($_.FullName)
        }
    }

    return @($targets | Select-Object -Unique)
}

$signTool = Find-SignTool
$targets = Get-SignTargets $BinDir

if ($targets.Count -eq 0) {
    $hint = @(
        "No binaries to sign in $BinDir.",
        "Run: wails build --nsis",
        "Expected presentation-timer.exe (set `"name`": `"presentation-timer`" in wails.json).",
        "For the NSIS installer, install NSIS and ensure makensis is on PATH."
    ) -join "`n"
    throw $hint
}

$wailsFallback = $targets | Where-Object { $_ -match '\\wailsapp\.exe$' }
if ($wailsFallback -and -not (Test-Path -LiteralPath (Join-Path $BinDir "presentation-timer.exe"))) {
    Write-Warning "Signing wailsapp.exe because presentation-timer.exe was not found. Rebuild after adding name to wails.json."
}

foreach ($file in $targets | Select-Object -Unique) {
    Write-Host "Signing $file"
    & $signTool sign /fd SHA256 /f $PfxPath /p $Password /tr $TimestampUrl /td SHA256 $file
    if ($LASTEXITCODE -ne 0) {
        throw "signtool failed for $file (exit $LASTEXITCODE)"
    }
}

Write-PublicCer $CerOut
Sync-WindowsCer $CerOut
