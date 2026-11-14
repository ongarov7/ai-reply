param(
    [string]$DestinationRoot = (Join-Path ([IO.Path]::GetTempPath()) 'AIReply-InnoSetup-6.7.4')
)
$ErrorActionPreference = 'Stop'
if (!$IsWindows) { throw 'The installer compiler runs on Windows.' }

# Pinned official release and GitHub release asset digest. No Chocolatey or machine-wide tool installation.
# https://github.com/jrsoftware/issrc/releases/tag/is-6_7_4
$source = 'https://github.com/jrsoftware/issrc/releases/download/is-6_7_4/innosetup-6.7.4.exe'
$expectedHash = 'a549929319f9120716677c685535ca4671e4b937b5fdc87e2585f32d8eb30e8c'
$DestinationRoot = [IO.Path]::GetFullPath($DestinationRoot)
New-Item $DestinationRoot -ItemType Directory -Force | Out-Null
$download = Join-Path $DestinationRoot 'innosetup-6.7.4.exe'
if (!(Test-Path $download) -or (Get-FileHash $download -Algorithm SHA256).Hash.ToLowerInvariant() -ne $expectedHash) {
    Invoke-WebRequest $source -OutFile $download
}
if ((Get-FileHash $download -Algorithm SHA256).Hash.ToLowerInvariant() -ne $expectedHash) {
    throw 'Inno Setup download SHA-256 mismatch.'
}
$signature = Get-AuthenticodeSignature $download
if ($signature.Status -ne 'Valid' -or $signature.SignerCertificate.Subject -notmatch 'Pyrsys') {
    throw "Unexpected Inno Setup publisher/signature: $($signature.Status)"
}
$compilerDirectory = Join-Path $DestinationRoot 'compiler'
$process = Start-Process $download -ArgumentList @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', '/SP-', '/NOICONS', '/CURRENTUSER', '/PORTABLE=1', "/DIR=`"$compilerDirectory`"") -Wait -PassThru
if ($process.ExitCode -ne 0) { throw "Inno Setup installation exited with $($process.ExitCode)." }
$compiler = Join-Path $compilerDirectory 'ISCC.exe'
if (!(Test-Path $compiler)) { throw 'Inno Setup compiler was not installed.' }
Write-Output $compiler
