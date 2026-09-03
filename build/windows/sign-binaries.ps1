# Signs Windows binaries with Authenticode (SHA256 + RFC3161 timestamp).
# CI order: wails build -> -ExeOnly -> makensis -> -InstallerOnly

param(
    [Parameter(Mandatory = $true)]
    [string]$PfxPath,
    [Parameter(Mandatory = $true)]
    [string]$Password,
    [string]$BinDir = "",
    [string]$CerOut = "",
    [string]$TimestampUrl = "http://timestamp.digicert.com",
    [switch]$ExportOnly,
    [switch]$ExeOnly,
    [switch]$InstallerOnly
)

$ErrorActionPreference = "Stop"

if ($ExeOnly -and $InstallerOnly) {
    throw "Use only one of -ExeOnly or -InstallerOnly"
}

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

function Get-SignTargets([string]$Dir, [bool]$OnlyExe, [bool]$OnlyInstaller) {
    $targets = [System.Collections.Generic.List[string]]::new()

    if (-not $OnlyInstaller) {
        $primary = Join-Path $Dir "presentation-timer.exe"
        if (Test-Path -LiteralPath $primary) {
            $targets.Add((Resolve-Path -LiteralPath $primary).Path)
        } elseif (-not $OnlyExe) {
            Get-ChildItem -Path $Dir -Filter "presentation-timer*.exe" -ErrorAction SilentlyContinue |
                Where-Object { $_.Name -notmatch '-installer\.exe$' } |
                ForEach-Object { $targets.Add($_.FullName) }
            $wailsapp = Join-Path $Dir "wailsapp.exe"
            if ((Test-Path -LiteralPath $wailsapp) -and $targets.Count -eq 0) {
                $targets.Add((Resolve-Path -LiteralPath $wailsapp).Path)
            }
        }
    }

    if (-not $OnlyExe) {
        Get-ChildItem -Path $Dir -Filter "presentation-timer-*-installer.exe" -ErrorAction SilentlyContinue |
            ForEach-Object { $targets.Add($_.FullName) }
    }

    return @($targets | Select-Object -Unique)
}

$signTool = Find-SignTool
$targets = Get-SignTargets -Dir $BinDir -OnlyExe:$ExeOnly -OnlyInstaller:$InstallerOnly

if ($targets.Count -eq 0) {
    $mode = if ($ExeOnly) { "executable" } elseif ($InstallerOnly) { "installer" } else { "binaries" }
    $hint = @(
        "No $mode to sign in $BinDir.",
        "Run: wails build (and makensis for installer).",
        "Expected presentation-timer.exe (set `"name`": `"presentation-timer`" in wails.json)."
    ) -join "`n"
    throw $hint
}

foreach ($file in $targets) {
    Write-Host "Signing $file"
    & $signTool sign /fd SHA256 /f $PfxPath /p $Password /tr $TimestampUrl /td SHA256 $file
    if ($LASTEXITCODE -ne 0) {
        throw "signtool failed for $file (exit $LASTEXITCODE)"
    }
}

if (-not $ExeOnly -and -not $InstallerOnly) {
    Write-PublicCer $CerOut
    Sync-WindowsCer $CerOut
}
