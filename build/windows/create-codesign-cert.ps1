# Creates a self-signed Authenticode certificate for local/CI signing.
# The .pfx stays private; ship only codesign.cer to users.
#
# Example:
#   powershell -ExecutionPolicy Bypass -File build/windows/create-codesign-cert.ps1
#
# Then encode the PFX for GitHub Actions:
#   [Convert]::ToBase64String([IO.File]::ReadAllBytes("build\windows\codesign.pfx"))

param(
    [string]$Subject = "CN=Presentation Timer",
    [string]$FriendlyName = "Presentation Timer Code Signing",
    [string]$OutDir = $PSScriptRoot,
    [int]$ValidYears = 5,
    [SecureString]$Password
)

$ErrorActionPreference = "Stop"

if (-not $Password) {
    $Password = Read-Host -AsSecureString -Prompt "Password for codesign.pfx"
}

if ($ValidYears -lt 1) {
    throw "ValidYears must be at least 1"
}

New-Item -ItemType Directory -Force -Path $OutDir | Out-Null

$cert = New-SelfSignedCertificate `
    -Type Custom `
    -KeyUsage DigitalSignature `
    -CertStoreLocation "Cert:\CurrentUser\My" `
    -TextExtension @("2.5.29.37={text}1.3.6.1.5.5.7.3.3", "2.5.29.19={text}") `
    -Subject $Subject `
    -FriendlyName $FriendlyName `
    -NotAfter (Get-Date).AddYears($ValidYears)

$pfxPath = Join-Path $OutDir "codesign.pfx"
$cerPath = Join-Path $OutDir "codesign.cer"

Export-PfxCertificate -Cert $cert -FilePath $pfxPath -Password $Password | Out-Null
Export-Certificate -Cert $cert -FilePath $cerPath -Type CERT | Out-Null

Write-Host "Created:"
Write-Host "  PFX (keep secret): $pfxPath"
Write-Host "  CER (public):      $cerPath"
Write-Host "  Thumbprint:        $($cert.Thumbprint)"
Write-Host "Add CODE_SIGN_PFX_BASE64 and CODE_SIGN_PFX_PASSWORD to GitHub Actions secrets."
Write-Host "Do not commit codesign.pfx."
