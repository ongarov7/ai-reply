param([string]$OutputRoot = 'out')
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
if (!$IsWindows -or $env:GITHUB_ACTIONS -ne 'true') {
    throw 'Run installer lifecycle checks only on a disposable Windows GitHub Actions runner.'
}
$sourceRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
if (![IO.Path]::IsPathRooted($OutputRoot)) { $OutputRoot = Join-Path $sourceRoot $OutputRoot }
$OutputRoot = [IO.Path]::GetFullPath($OutputRoot)
$installer = Join-Path $OutputRoot 'AIReply-Setup.exe'
$testDirectory = Join-Path $env:RUNNER_TEMP ('AIReply-install-test-' + [Guid]::NewGuid().ToString('N'))
$installDirectory = Join-Path $testDirectory 'Installed AI Reply'
$executable = Join-Path $installDirectory 'AIReply.exe'
$uninstaller = Join-Path $installDirectory 'unins000.exe'
$uninstallKey = 'Software\Microsoft\Windows\CurrentVersion\Uninstall\{7AEBA055-83B4-4B99-A30B-92E5C9C0C5D0}_is1'
$runKey = 'Software\Microsoft\Windows\CurrentVersion\Run'
$approvedKey = 'Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run'
$registry = [Microsoft.Win32.RegistryKey]::OpenBaseKey([Microsoft.Win32.RegistryHive]::CurrentUser, [Microsoft.Win32.RegistryView]::Registry64)
$startShortcut = Join-Path ([Environment]::GetFolderPath('Programs', 'DoNotVerify')) 'AI Reply.lnk'
$desktopShortcut = Join-Path ([Environment]::GetFolderPath('DesktopDirectory', 'DoNotVerify')) 'AI Reply.lnk'
$assertions = 0
$ownsTestState = $false
$appProcess = $null
$preservedFiles = @()
$outcome = @{ success = $false; architecture = [Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString() }
$brandIcon = Join-Path $sourceRoot 'src\AirReply.App\Assets\AIReply.ico'
if (!('AirReply.InstallerValidation.EmbeddedIconVerifier' -as [type])) {
    Add-Type -Path (Join-Path $sourceRoot 'installer\EmbeddedIconVerifier.cs')
}

function Assert-Installer([bool]$Condition, [string]$Message) {
    if (!$Condition) { throw $Message }
    $script:assertions++
}
function Invoke-InstallerProcess([string]$File, [string[]]$InstallerArguments) {
    $process = Start-Process -FilePath $File -ArgumentList $InstallerArguments -PassThru
    try {
        if (!$process.WaitForExit(60000)) { $process.Kill($true); throw "Installer timed out: $File" }
        return $process.ExitCode
    }
    finally { $process.Dispose() }
}
function Read-RegistryValue([string]$Path, [string]$Name) {
    $key = $registry.OpenSubKey($Path)
    try { if ($key) { return $key.GetValue($Name) }; return $null }
    finally { if ($key) { $key.Dispose() } }
}
function Write-RegistryValue([string]$Path, [string]$Name, $Value, [Microsoft.Win32.RegistryValueKind]$Kind) {
    $key = $registry.CreateSubKey($Path)
    try { $key.SetValue($Name, $Value, $Kind) }
    finally { $key.Dispose() }
}
function Wait-InstallerCondition([scriptblock]$Condition) {
    for ($attempt = 0; $attempt -lt 150; $attempt++) {
        if (& $Condition) { return $true }
        Start-Sleep -Milliseconds 100
    }
    return $false
}
function Test-AppMutex {
    try { $mutex = [Threading.Mutex]::OpenExisting('Local\AIReply-production'); $mutex.Dispose(); return $true }
    catch [Threading.WaitHandleCannotBeOpenedException] { return $false }
}
function Assert-BrandIcon([string]$Path) {
    $description = [AirReply.InstallerValidation.EmbeddedIconVerifier]::AssertBrandIcon($Path, $brandIcon)
    $script:assertions++
    return $description
}
function Assert-ShortcutIcon($Shortcut, [string]$ExpectedExecutable) {
    $iconLocation = [string]$Shortcut.IconLocation
    if ([string]::IsNullOrWhiteSpace($iconLocation)) {
        # An empty IconLocation tells Windows to use the target's primary embedded icon.
        $iconPath = $Shortcut.TargetPath
        $iconIndex = 0
    }
    else {
        $separator = $iconLocation.LastIndexOf(',')
        $iconPath = if ($separator -ge 0) { $iconLocation.Substring(0, $separator) } else { $iconLocation }
        $iconIndex = if ($separator -ge 0) { [int]$iconLocation.Substring($separator + 1) } else { 0 }
        $iconPath = [Environment]::ExpandEnvironmentVariables($iconPath.Trim().Trim([char]34))
        if ([string]::IsNullOrWhiteSpace($iconPath)) { $iconPath = $Shortcut.TargetPath }
    }
    Assert-Installer ($iconPath -eq $ExpectedExecutable -and $iconIndex -eq 0) 'Shortcut does not resolve the installed AI Reply brand icon.'
}
function Install-App([string]$LogName) {
    Invoke-InstallerProcess $installer @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', '/SP-', '/TASKS=desktopicon', "/DIR=`"$installDirectory`"", "/LOG=`"$(Join-Path $OutputRoot $LogName)`"")
}
function Uninstall-App([string]$LogName) {
    Invoke-InstallerProcess $uninstaller @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', "/LOG=`"$(Join-Path $OutputRoot $LogName)`"")
}

try {
    Assert-Installer (Test-Path $installer) 'AIReply-Setup.exe is missing.'
    # Verify actual encoded frame bytes in the primary PE icon group, including 16px and 256px frames.
    # This also checks ARM64 on x64 without loading it as executable code.
    $outcome.brand_icons = @{
        installer = (Assert-BrandIcon $installer)
        x64 = (Assert-BrandIcon (Join-Path $OutputRoot 'win-x64\AIReply.exe'))
        arm64 = (Assert-BrandIcon (Join-Path $OutputRoot 'win-arm64\AIReply.exe'))
    }
    $changedReference = Join-Path $env:RUNNER_TEMP ('AIReply-changed-icon-' + [Guid]::NewGuid().ToString('N') + '.ico')
    try {
        $changedBytes = [IO.File]::ReadAllBytes($brandIcon)
        $changedBytes[$changedBytes.Length - 1] = $changedBytes[$changedBytes.Length - 1] -bxor 0x10
        [IO.File]::WriteAllBytes($changedReference, $changedBytes)
        $rejected = $false
        try { [AirReply.InstallerValidation.EmbeddedIconVerifier]::AssertBrandIcon($installer, $changedReference) | Out-Null }
        catch {
            if ($_.Exception -is [IO.InvalidDataException] -or $_.Exception.InnerException -is [IO.InvalidDataException]) { $rejected = $true }
            else { throw }
        }
        Assert-Installer $rejected 'The icon verifier accepted changed logo image bytes.'
    }
    finally { Remove-Item $changedReference -Force -ErrorAction SilentlyContinue }
    Assert-Installer (!(Test-AppMutex)) 'An existing AI Reply instance would contaminate installer checks.'
    $existing = $registry.OpenSubKey($uninstallKey)
    try { Assert-Installer ($null -eq $existing) 'An existing AI Reply installation would contaminate installer checks.' }
    finally { if ($existing) { $existing.Dispose() } }
    Assert-Installer ($null -eq (Read-RegistryValue $runKey 'AI Reply')) 'An existing autostart setting would contaminate installer checks.'
    Assert-Installer ($null -eq (Read-RegistryValue $approvedKey 'AI Reply')) 'An existing startup approval would contaminate installer checks.'
    Assert-Installer (!(Test-Path $startShortcut) -and !(Test-Path $desktopShortcut)) 'Existing shortcuts would contaminate installer checks.'
    $ownsTestState = $true
    New-Item $testDirectory -ItemType Directory -Force | Out-Null
    $nativeRid = if ($outcome.architecture -eq 'Arm64') { 'win-arm64' } else { 'win-x64' }
    $expectedHash = (Get-FileHash (Join-Path $OutputRoot "$nativeRid/AIReply.exe") -Algorithm SHA256).Hash

    Assert-Installer ((Install-App 'installer-install.log') -eq 0) 'Silent install failed.'
    Assert-Installer ((Get-FileHash $executable -Algorithm SHA256).Hash -eq $expectedHash) 'Installer selected the wrong native payload.'
    $outcome.brand_icons.installed = Assert-BrandIcon $executable
    Assert-Installer (Test-Path $uninstaller) 'Automatic uninstaller is missing.'
    Assert-Installer (Test-Path (Join-Path $installDirectory 'README-Windows.txt')) 'Installed instructions are missing.'
    [xml]$properties = Get-Content (Join-Path $sourceRoot 'Directory.Build.props') -Raw
    Assert-Installer ((Read-RegistryValue $uninstallKey 'DisplayVersion') -eq [string]$properties.Project.PropertyGroup[0].Version) 'Installed Apps version is incorrect.'
    Assert-Installer ((Read-RegistryValue $uninstallKey 'DisplayName') -eq 'AI Reply') 'Installed Apps display name is incorrect.'
    Assert-Installer ((Read-RegistryValue $uninstallKey 'InstallLocation').TrimEnd('\') -eq $installDirectory) 'Installed Apps path is incorrect.'
    Assert-Installer ((Read-RegistryValue $uninstallKey 'DisplayIcon') -eq $executable) 'Installed Apps icon is incorrect.'
    Assert-Installer ((Read-RegistryValue $uninstallKey 'UninstallString') -like '*unins000.exe*') 'Installed Apps uninstall command is missing.'
    Assert-Installer (Test-Path $startShortcut) 'Start menu shortcut is missing.'
    Assert-Installer (Test-Path $desktopShortcut) 'Optional desktop shortcut was not installed.'
    $shell = New-Object -ComObject WScript.Shell
    $shortcut = $shell.CreateShortcut($startShortcut)
    Assert-Installer ($shortcut.TargetPath -eq $executable) 'Start menu shortcut targets the wrong file.'
    Assert-Installer ($shortcut.WorkingDirectory -eq $installDirectory) 'Start menu shortcut working directory is incorrect.'
    Assert-ShortcutIcon $shortcut $executable
    $desktopLink = $shell.CreateShortcut($desktopShortcut)
    Assert-Installer ($desktopLink.TargetPath -eq $executable) 'Desktop shortcut targets the wrong file.'
    Assert-ShortcutIcon $desktopLink $executable

    # Preview starts the real installed Avalonia executable and single-instance guard without any production HTTP.
    $appProcess = Start-Process $executable -ArgumentList '--preview', '--background' -PassThru
    Assert-Installer (Wait-InstallerCondition { !( $appProcess.HasExited) -and (Test-AppMutex) }) 'Installed application did not start.'
    Assert-Installer ((Invoke-InstallerProcess $executable @('--preview', '--background')) -eq 0) 'Second background launch did not exit normally.'
    Assert-Installer ((Install-App 'installer-running-install.log') -ne 0) 'Setup replaced a running application.'
    Assert-Installer ((Uninstall-App 'installer-running-uninstall.log') -ne 0) 'Uninstaller removed a running application.'
    Assert-Installer (!$appProcess.HasExited) 'Installer killed the running application.'
    Assert-Installer ((Get-FileHash $executable -Algorithm SHA256).Hash -eq $expectedHash) 'Running-app guard modified installed files.'
    $appProcess.Kill($true); $appProcess.WaitForExit(); $appProcess.Dispose(); $appProcess = $null
    Assert-Installer (Wait-InstallerCondition { !(Test-AppMutex) }) 'The application mutex survived process exit.'

    # Reinstall/upgrade uses one stable registration and restores the payload.
    [IO.File]::WriteAllText($executable, 'Reinstall must replace a damaged payload.')
    Assert-Installer ((Install-App 'installer-upgrade.log') -eq 0) 'Reinstall/upgrade failed.'
    Assert-Installer ((Get-FileHash $executable -Algorithm SHA256).Hash -eq $expectedHash) 'Reinstall did not replace the payload.'
    $uninstallEntries = $registry.OpenSubKey('Software\Microsoft\Windows\CurrentVersion\Uninstall')
    try { Assert-Installer (@($uninstallEntries.GetSubKeyNames() | Where-Object { $_ -eq '{7AEBA055-83B4-4B99-A30B-92E5C9C0C5D0}_is1' }).Count -eq 1) 'Upgrade created duplicate uninstall entries.' }
    finally { $uninstallEntries.Dispose() }

    $dataRoot = Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) 'AI Reply'
    foreach ($subdirectory in @('context', 'production\secure')) {
        $folder = Join-Path $dataRoot $subdirectory
        New-Item $folder -ItemType Directory -Force | Out-Null
        $marker = Join-Path $folder ('installer-smoke-' + [Guid]::NewGuid().ToString('N') + '.txt')
        [IO.File]::WriteAllText($marker, 'Synthetic retained account data; no credentials.')
        $preservedFiles += $marker
    }
    $startupCommand = '"' + $executable + '" --background'
    Write-RegistryValue $runKey 'AI Reply' $startupCommand String
    Write-RegistryValue $approvedKey 'AI Reply' ([byte[]](2,0,0,0,0,0,0,0,0,0,0,0)) Binary
    Assert-Installer ((Uninstall-App 'installer-uninstall.log') -eq 0) 'Silent uninstall failed.'
    Assert-Installer (Wait-InstallerCondition { !(Test-Path $executable) -and !(Test-Path $uninstaller) }) 'Uninstall left application binaries.'
    Assert-Installer ($null -eq (Read-RegistryValue $uninstallKey 'DisplayName')) 'Uninstall registration remains.'
    Assert-Installer (!(Test-Path $startShortcut) -and !(Test-Path $desktopShortcut)) 'Uninstall left shortcuts.'
    Assert-Installer ($null -eq (Read-RegistryValue $runKey 'AI Reply')) 'Uninstall left its own startup command.'
    Assert-Installer ($null -eq (Read-RegistryValue $approvedKey 'AI Reply')) 'Uninstall left its own startup approval.'
    foreach ($marker in $preservedFiles) { Assert-Installer ([IO.File]::ReadAllText($marker) -eq 'Synthetic retained account data; no credentials.') 'Uninstall removed account/profile data.' }

    # A portable copy may own autostart; uninstalling the installed copy must preserve that setting.
    Assert-Installer ((Install-App 'installer-reinstall.log') -eq 0) 'Second clean install failed.'
    $portableCommand = '"' + (Join-Path $testDirectory 'Portable\AIReply.exe') + '" --background'
    Write-RegistryValue $runKey 'AI Reply' $portableCommand String
    Write-RegistryValue $approvedKey 'AI Reply' ([byte[]](3,0,0,0,0,0,0,0,0,0,0,0)) Binary
    Assert-Installer ((Uninstall-App 'installer-portable-preservation.log') -eq 0) 'Final uninstall failed.'
    Assert-Installer (Wait-InstallerCondition { !(Test-Path $executable) -and !(Test-Path $uninstaller) -and $null -eq (Read-RegistryValue $uninstallKey 'DisplayName') -and !(Test-Path $startShortcut) -and !(Test-Path $desktopShortcut) }) 'Final uninstall left binaries, shortcuts or registration.'
    Assert-Installer ((Read-RegistryValue $runKey 'AI Reply') -eq $portableCommand) 'Uninstall removed a different portable copy startup setting.'
    Assert-Installer ($null -ne (Read-RegistryValue $approvedKey 'AI Reply')) 'Uninstall removed a different portable copy startup approval.'
    $outcome.success = $true
    Write-Output "Installer lifecycle passed: $assertions assertions; native $nativeRid."
}
catch { $outcome.error = $_.Exception.Message; throw }
finally {
    if ($appProcess) { if (!$appProcess.HasExited) { $appProcess.Kill($true); $appProcess.WaitForExit() }; $appProcess.Dispose() }
    foreach ($path in $(if ($ownsTestState) { @($runKey, $approvedKey) } else { @() })) {
        $key = $registry.OpenSubKey($path, $true)
        try { if ($key) { $key.DeleteValue('AI Reply', $false) } }
        finally { if ($key) { $key.Dispose() } }
    }
    foreach ($marker in $preservedFiles) { Remove-Item $marker -Force -ErrorAction SilentlyContinue }
    $registry.Dispose()
    $outcome.assertions = $assertions
    $outcome | ConvertTo-Json | Set-Content (Join-Path $OutputRoot 'installer-test-results.json') -Encoding utf8
}
