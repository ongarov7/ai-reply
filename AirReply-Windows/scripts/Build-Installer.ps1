[CmdletBinding()]
param(
    [string]$OutputRoot = "out",
    [string]$IsccPath,
    [string]$AppVersion
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$sourceRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
if (![System.IO.Path]::IsPathRooted($OutputRoot)) {
    $OutputRoot = Join-Path $sourceRoot $OutputRoot
}
$payloadRoot = [System.IO.Path]::GetFullPath($OutputRoot)

if (!$AppVersion) {
    [xml]$properties = Get-Content (Join-Path $sourceRoot 'Directory.Build.props') -Raw
    $AppVersion = [string]$properties.Project.PropertyGroup[0].Version
}
if ($AppVersion -notmatch '^\d{1,5}\.\d{1,5}\.\d{1,5}(\.\d{1,5})?$' -or
    @($AppVersion.Split('.') | Where-Object { [int]$_ -gt 65535 }).Count -gt 0) {
    throw "Installer version must contain three or four numeric components in the range 0..65535."
}

if (!$IsccPath) {
    $compilerCommand = Get-Command ISCC.exe -ErrorAction SilentlyContinue
    if ($compilerCommand) { $IsccPath = $compilerCommand.Source }
    else {
        foreach ($programDirectory in @(${env:ProgramFiles(x86)}, $env:ProgramFiles)) {
            if ($programDirectory) {
                $candidate = Join-Path $programDirectory 'Inno Setup 6\ISCC.exe'
                if (Test-Path -LiteralPath $candidate -PathType Leaf) { $IsccPath = $candidate; break }
            }
        }
    }
}
if (!$IsccPath -or !(Test-Path -LiteralPath $IsccPath -PathType Leaf)) {
    throw "Inno Setup 6.7.4 compiler is required. Provide -IsccPath or run scripts/Get-InnoSetup.ps1."
}

# Verify actual native payloads, rather than trusting folder names or a stale previous publish.
function Assert-PeArchitecture([string]$Path, [uint16]$ExpectedMachine) {
    if (!(Test-Path -LiteralPath $Path -PathType Leaf)) { throw "Missing published payload: $Path" }
    $stream = [System.IO.File]::OpenRead($Path)
    $reader = [System.IO.BinaryReader]::new($stream)
    try {
        if ($reader.ReadUInt16() -ne 0x5A4D) { throw "Payload is not a Windows executable: $Path" }
        $stream.Position = 0x3C
        $peOffset = $reader.ReadUInt32()
        if ($peOffset -gt $stream.Length - 6) { throw "Invalid PE header: $Path" }
        $stream.Position = $peOffset
        if ($reader.ReadUInt32() -ne 0x00004550 -or $reader.ReadUInt16() -ne $ExpectedMachine) {
            throw "Published payload has the wrong CPU architecture: $Path"
        }
    }
    finally { $reader.Dispose(); $stream.Dispose() }
}
Assert-PeArchitecture (Join-Path $payloadRoot 'win-x64\AIReply.exe') 0x8664
Assert-PeArchitecture (Join-Path $payloadRoot 'win-arm64\AIReply.exe') 0xAA64

$installer = Join-Path $payloadRoot 'AIReply-Setup.exe'
$checksum = "$installer.sha256"
Remove-Item -LiteralPath $installer, $checksum -Force -ErrorAction SilentlyContinue
& $IsccPath "/DSourceRoot=$sourceRoot" "/DPayloadRoot=$payloadRoot" "/DBuildVersion=$AppVersion" `
    (Join-Path $sourceRoot 'installer\AIReply.iss')
if ($LASTEXITCODE -ne 0) { throw "Inno Setup compilation failed with exit code $LASTEXITCODE" }
if (!(Test-Path -LiteralPath $installer -PathType Leaf) -or (Get-Item -LiteralPath $installer).Length -eq 0) {
    throw "Inno Setup did not produce AIReply-Setup.exe."
}
$hash = (Get-FileHash -LiteralPath $installer -Algorithm SHA256).Hash.ToLowerInvariant()
"$hash  AIReply-Setup.exe" | Set-Content -LiteralPath $checksum -Encoding ascii
Write-Output "Built universal installer: $installer"
