param([string]$OutputRoot = "out")
$ErrorActionPreference = 'Stop'
Push-Location (Join-Path $PSScriptRoot '..')
try {
    foreach ($rid in 'win-x64', 'win-arm64') {
        $destination = Join-Path $OutputRoot $rid
        if (Test-Path $destination) { Remove-Item $destination -Recurse -Force }
        dotnet publish src/AirReply.App/AirReply.App.csproj -c Release -r $rid --self-contained `
            -p:PublishSingleFile=true -p:IncludeNativeLibrariesForSelfExtract=true `
            -p:EnableCompressionInSingleFile=true -p:DebugType=none -o $destination
        if ($LASTEXITCODE -ne 0) { throw "Publish failed for $rid" }
        if (!(Test-Path (Join-Path $destination 'AIReply.exe'))) { throw "Missing AIReply.exe for $rid" }
        Copy-Item README-Windows.txt $destination
        $zip = Join-Path $OutputRoot "AIReply-$rid.zip"
        Compress-Archive -Path "$destination/*" -DestinationPath $zip -Force
        $hash = (Get-FileHash $zip -Algorithm SHA256).Hash.ToLowerInvariant()
        "$hash  AIReply-$rid.zip" | Set-Content "$zip.sha256" -Encoding ascii
    }
}
finally { Pop-Location }
