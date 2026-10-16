# Installs Gentry on Windows: downloads the program of the platform from a
# release on GitHub, checks its SHA-256 against checksums.txt of the release,
# puts it into the folder bin of the data root and adds the folder to the
# variable Path of the user.
#
#   irm https://github.com/t8nax/gentry/releases/latest/download/install.ps1 | iex
#   & ([scriptblock]::Create((irm …/install.ps1))) -Version 0.1.0 -NoModifyPath
#
# The data root is GENTRY_HOME, or %USERPROFILE%\.gentry. GENTRY_RELEASES_URL
# replaces the address the files are downloaded from, such as for a mock
# release in the tests; the hints name the releases on GitHub whatever it is.
#
# The script runs in Windows PowerShell 5.1 and in PowerShell 7, in the
# session of the user: it never closes the window. A refusal writes its lines
# to the error stream, leaves 1 in $LASTEXITCODE and returns; a success leaves
# 0 there. Its work runs in a script block of its own, so nothing of it stays
# in the session. It downloads and checks the program in a temporary folder
# and only then puts it in its place, so an error at any step leaves the
# installed program as it was.

# CmdletBinding makes PowerShell refuse an unknown parameter, such as a typo
# of -Version, before the script runs: without it the script would take the
# rest of the arguments silently and install the latest release.
[CmdletBinding()]
param(
    [string]$Version = '',
    [switch]$NoModifyPath
)

& {
    param([string]$Version, [bool]$NoModifyPath)

    Set-StrictMode -Version 3.0
    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue' # the bar of Windows PowerShell slows the download down

    $githubReleases = 'https://github.com/t8nax/gentry/releases'

    # The texts of the script, a line «Key=Text» each, with the probe first.
    # Windows PowerShell 5.1 reads a script that irm downloads with no charset
    # as ISO-8859-1, and one from a file with no BOM in the ANSI code page of
    # the system: a letter of UTF-8 comes as two characters then, some of them
    # quotes. In a here-string they end nothing, and the texts are turned back
    # into UTF-8 by the code page that gives the probe back as it was written.
    $catalog = @'
Probe=АИНПЁё
Installed=Gentry {0} установлен.
Program=Программа: {0}
Previous=Прежняя версия: {0}
PreviousUnknown=не определена
Renamed=Прежняя программа занята и переименована: {0}
RenamedRunning=Запущенные процессы Gentry работают на прежней версии до перезапуска.
PathAdded=Папка добавлена в переменную Path пользователя.
PathPresent=Папка уже есть в PATH.
PathKept=PATH не изменён.
PathFailed=Не удалось добавить папку в PATH.
Error=Ошибка: {0}
HintNewWindow=Открыть новое окно терминала и подключить ИИ-инструмент: gentry setup <ИИ-инструмент>
HintConnect=Подключить ИИ-инструмент: gentry setup <ИИ-инструмент>
HintTools=Посмотреть доступные ИИ-инструменты: gentry setup --help
HintAddPath=Добавить папку в PATH: {0}
HintConnectAt=Подключить ИИ-инструмент: {0} setup <ИИ-инструмент>
HintToolsAt=Посмотреть доступные ИИ-инструменты: {0} setup --help
VersionInvalid=Версия указана неверно.
VersionLine=Версия: {0}
NotFound=Выпуск не найден.
Latest=последняя
SeeReleases=Посмотреть выпуски: {0}
DownloadFailed=Не удалось скачать файл.
Address=Адрес: {0}
ChecksumMismatch=Контрольная сумма скачанного файла не совпала.
File=Файл: {0}
Expected=Ожидалась: {0}
Got=Получена: {0}
WriteFailed=Не удалось записать программу.
Unsupported=Платформа не поддерживается.
Platform=Платформа: {0}
Supported=Поддерживаемые платформы:
'@
    $want = -join @([char]0x0410, [char]0x0418, [char]0x041D, [char]0x041F, [char]0x0401, [char]0x0451)
    $probe = ($catalog -split "`r?`n")[0].Substring('Probe='.Length)
    if ($probe -cne $want) {
        foreach ($codePage in @(28591, 1252, 0)) {
            try {
                $read = [Text.Encoding]::GetEncoding($codePage)
            } catch {
                continue
            }
            if ([Text.Encoding]::UTF8.GetString($read.GetBytes($probe)) -ceq $want) {
                $catalog = [Text.Encoding]::UTF8.GetString($read.GetBytes($catalog))
                break
            }
        }
    }
    $texts = @{}
    foreach ($line in $catalog -split "`r?`n") {
        $key, $text = $line.Split('=', 2)
        $texts[$key] = $text
    }

    # The platforms of a release, in the order of the message on an
    # unsupported one.
    $platforms = @('linux/amd64', 'linux/arm64', 'darwin/amd64', 'darwin/arm64', 'windows/amd64', 'windows/arm64')

    # The marker of a refusal: Refuse throws it after it has written the lines,
    # and the end of the script turns it into the code 1.
    $refused = 'gentry-install-refused'

    function Say([string[]]$Lines) {
        foreach ($line in $Lines) { $Host.UI.WriteLine($line) }
    }

    # Refuse writes the lines to the error stream and stops the script. The
    # lines go at once: the host writes no empty line of its own.
    function Refuse([string[]]$Lines) {
        $Host.UI.WriteErrorLine($Lines -join [Environment]::NewLine)
        throw $refused
    }

    # A fetch error is a file that has not downloaded: its reason, and
    # whether the server answered that there is no such file.
    function Get-FetchError($ErrorRecord) {
        $exception = $ErrorRecord.Exception
        $missing = $false
        $response = $null
        if ($exception.PSObject.Properties['Response']) { $response = $exception.Response }
        if ($null -ne $response -and $response.PSObject.Properties['StatusCode']) {
            $missing = [int]$response.StatusCode -eq 404
        }
        [pscustomobject]@{ Reason = $exception.Message; Missing = $missing }
    }

    # Fetch downloads the address into the file, or into memory when the file
    # is empty, follows the redirects and returns the address it came from at
    # last.
    function Fetch([string]$Url, [string]$File) {
        $request = @{ Uri = $Url; UseBasicParsing = $true }
        if ($File) { $request.OutFile = $File; $request.PassThru = $true }
        $response = Invoke-WebRequest @request
        $base = $response.BaseResponse
        if ($base.PSObject.Properties['ResponseUri']) { return $base.ResponseUri.AbsoluteUri } # Windows PowerShell
        return $base.RequestMessage.RequestUri.AbsoluteUri # PowerShell 7
    }

    function Test-Version([string]$Value) {
        return $Value -match '\A(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\z'
    }

    function NotFound([string]$Name) {
        Refuse @($texts.NotFound, ($texts.VersionLine -f $Name), '', ($texts.SeeReleases -f $githubReleases))
    }

    function DownloadFailed([string]$Url, $Failure) {
        Refuse @($texts.DownloadFailed, ($texts.Address -f $Url), ($texts.Error -f $Failure.Reason))
    }

    # Get-Platform returns the platform of the machine, os/arch.
    function Get-Platform {
        $arch = $env:PROCESSOR_ARCHITECTURE
        # A 32-bit PowerShell on a 64-bit Windows names the machine here.
        if ($env:PROCESSOR_ARCHITEW6432) { $arch = $env:PROCESSOR_ARCHITEW6432 }
        switch ($arch) {
            'AMD64' { return 'windows/amd64' }
            'ARM64' { return 'windows/arm64' }
            'x86' { return 'windows/386' }
            default { return 'windows/' + $arch.ToLowerInvariant() }
        }
    }

    # Get-DataRoot returns the data root: GENTRY_HOME, a relative one from the
    # current folder, as gentry takes it, or %USERPROFILE%\.gentry.
    function Get-DataRoot {
        if ($env:GENTRY_HOME) {
            $path = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($env:GENTRY_HOME)
            return [IO.Path]::GetFullPath($path).TrimEnd('\')
        }
        return Join-Path $env:USERPROFILE '.gentry'
    }

    # Get-Previous returns the version the installed program names on
    # version, «не определена» when it names none, or $null when there is no
    # program.
    function Get-Previous([string]$Program) {
        if (-not (Test-Path -LiteralPath $Program)) { return $null }
        try {
            foreach ($line in @(& $Program version 2>$null)) {
                if ("$line" -match 'Gentry: (\S+)') { return $Matches[1] }
            }
        } catch {
        }
        return $texts.PreviousUnknown
    }

    # Install-Program puts the downloaded program in its place and returns
    # whether a running process held the program before, which is renamed to
    # gentry.exe.old then. A gentry.exe.old left from an earlier installation
    # is removed, unless a process still holds it.
    function Install-Program([string]$Downloaded, [string]$Bin, [string]$Program) {
        $old = $Program + '.old'
        $staged = $Program + '.new'
        try {
            [void](New-Item -ItemType Directory -Force -Path $Bin)
            if (Test-Path -LiteralPath $old) {
                try { [IO.File]::Delete($old) } catch { }
            }
            [IO.File]::Copy($Downloaded, $staged, $true)
            if (-not (Test-Path -LiteralPath $Program)) {
                [IO.File]::Move($staged, $Program)
                return $false
            }
            if (Test-Path -LiteralPath $old) {
                # A process holds gentry.exe.old: gentry.exe is replaced in
                # place, which works when no process holds it.
                [IO.File]::Delete($Program)
                [IO.File]::Move($staged, $Program)
                return $false
            }
            # A running program cannot be removed or overwritten but can be
            # renamed: the program goes to gentry.exe.old, the new one takes
            # its place, and gentry.exe.old stays only while a process holds
            # it.
            [IO.File]::Move($Program, $old)
            try {
                [IO.File]::Move($staged, $Program)
            } catch {
                [IO.File]::Move($old, $Program)
                throw
            }
            try {
                [IO.File]::Delete($old)
                return $false
            } catch {
                return $true
            }
        } catch {
            if (Test-Path -LiteralPath $staged) {
                try { [IO.File]::Delete($staged) } catch { }
            }
            Refuse @($texts.WriteFailed, ($texts.Program -f $Program), ($texts.Error -f $_.Exception.Message))
        }
    }

    # Send-EnvironmentChange tells the windows the environment of the user has
    # changed, so new processes of Explorer get the new Path.
    function Send-EnvironmentChange {
        try {
            if (-not ('GentryInstall.User32' -as [type])) {
                Add-Type -Namespace GentryInstall -Name User32 -MemberDefinition @'
[DllImport("user32.dll", SetLastError = true, CharSet = CharSet.Unicode)]
public static extern IntPtr SendMessageTimeout(IntPtr hWnd, uint msg, UIntPtr wParam, string lParam, uint flags, uint timeout, out UIntPtr result);
'@
            }
            $result = [UIntPtr]::Zero
            # HWND_BROADCAST, WM_SETTINGCHANGE, SMTO_ABORTIFHUNG, 5 seconds.
            [void][GentryInstall.User32]::SendMessageTimeout([IntPtr]0xffff, 0x1a, [UIntPtr]::Zero, 'Environment', 2, 5000, [ref]$result)
        } catch {
            # The new Path is written: a window that has not heard of it gets
            # it at the next sign-in.
        }
    }

    # Add-UserPath appends the folder to the variable Path of the user and
    # returns 'added', or 'present' when the variable has it already, whatever
    # the case of its letters and a trailing backslash. The rest of the value
    # stays as it is, references %VAR% and the type of the variable included.
    function Add-UserPath([string]$Folder) {
        $key = [Microsoft.Win32.Registry]::CurrentUser.CreateSubKey('Environment')
        try {
            $exists = @($key.GetValueNames()) -contains 'Path'
            $value = [string]$key.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
            $kind = [Microsoft.Win32.RegistryValueKind]::ExpandString
            if ($exists -and $key.GetValueKind('Path') -eq [Microsoft.Win32.RegistryValueKind]::String) {
                $kind = [Microsoft.Win32.RegistryValueKind]::String
            }
            $folderKey = $Folder.TrimEnd('\')
            foreach ($entry in $value.Split(';')) {
                $expanded = [Environment]::ExpandEnvironmentVariables($entry.Trim()).TrimEnd('\')
                if ($expanded -and [string]::Equals($expanded, $folderKey, [StringComparison]::OrdinalIgnoreCase)) {
                    return 'present'
                }
            }
            if ($value -eq '') {
                $value = $Folder
            } elseif ($value.EndsWith(';')) {
                $value = $value + $Folder
            } else {
                $value = $value + ';' + $Folder
            }
            $key.SetValue('Path', $value, $kind)
        } finally {
            $key.Close()
        }
        Send-EnvironmentChange
        return 'added'
    }

    $tmp = $null
    try {
        if ($Version -and -not (Test-Version $Version)) {
            Refuse @($texts.VersionInvalid, ($texts.VersionLine -f $Version))
        }
        $platform = Get-Platform
        if ($platforms -notcontains $platform) {
            Refuse (@($texts.Unsupported, ($texts.Platform -f $platform), $texts.Supported) + @($platforms | ForEach-Object { '  ' + $_ }))
        }
        $os, $arch = $platform.Split('/')
        $asset = "gentry-$os-$arch.exe"

        if ($PSVersionTable.PSVersion.Major -lt 6) {
            [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
        }
        $releases = $githubReleases
        if ($env:GENTRY_RELEASES_URL) { $releases = $env:GENTRY_RELEASES_URL }
        $releases = $releases.TrimEnd('/')

        $data = Get-DataRoot
        $bin = Join-Path $data 'bin'
        $program = Join-Path $bin 'gentry.exe'

        $tmp = Join-Path ([IO.Path]::GetTempPath()) ('gentry-' + [Guid]::NewGuid().ToString('N'))
        try {
            [void](New-Item -ItemType Directory -Path $tmp)
        } catch {
            Refuse @($texts.WriteFailed, ($texts.Program -f $program), ($texts.Error -f $_.Exception.Message))
        }

        if (-not $Version) {
            $url = "$releases/latest"
            try {
                $final = Fetch $url ''
            } catch {
                $failure = Get-FetchError $_
                if ($failure.Missing) { NotFound $texts.Latest }
                DownloadFailed $url $failure
            }
            if ($final -match '/tag/v([^/?#]+)\z' -and (Test-Version $Matches[1])) {
                $Version = $Matches[1]
            } else {
                NotFound $texts.Latest
            }
        }

        $base = "$releases/download/v$Version"
        $checksums = Join-Path $tmp 'checksums.txt'
        try {
            [void](Fetch "$base/checksums.txt" $checksums)
        } catch {
            $failure = Get-FetchError $_
            if ($failure.Missing) { NotFound $Version }
            DownloadFailed "$base/checksums.txt" $failure
        }
        $downloaded = Join-Path $tmp $asset
        try {
            [void](Fetch "$base/$asset" $downloaded)
        } catch {
            DownloadFailed "$base/$asset" (Get-FetchError $_)
        }
        $expected = ''
        foreach ($line in [IO.File]::ReadAllLines($checksums)) {
            if ($line -match '\A([0-9a-fA-F]{64}) [ *](.+)\z' -and $Matches[2] -eq $asset) {
                $expected = $Matches[1].ToLowerInvariant()
                break
            }
        }
        $actual = (Get-FileHash -LiteralPath $downloaded -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($expected -ne $actual) {
            Refuse @($texts.ChecksumMismatch, ($texts.File -f $asset), ($texts.Expected -f $expected), ($texts.Got -f $actual))
        }

        $previous = Get-Previous $program
        $busy = Install-Program $downloaded $bin $program

        $lines = @(($texts.Installed -f $Version), ($texts.Program -f $program))
        if ($null -ne $previous) { $lines += $texts.Previous -f $previous }
        if ($busy) { $lines += @(($texts.Renamed -f ($program + '.old')), $texts.RenamedRunning) }
        $hint = 'manual'
        if ($NoModifyPath) {
            $lines += $texts.PathKept
        } else {
            try {
                if ((Add-UserPath $bin) -eq 'added') {
                    $lines += $texts.PathAdded
                    $hint = 'new'
                } else {
                    $lines += $texts.PathPresent
                    $hint = 'present'
                }
            } catch {
                $lines += @($texts.PathFailed, ($texts.Error -f $_.Exception.Message))
            }
        }
        $lines += ''
        switch ($hint) {
            'new' { $lines += @($texts.HintNewWindow, $texts.HintTools) }
            'present' { $lines += @($texts.HintConnect, $texts.HintTools) }
            default { $lines += @(($texts.HintAddPath -f $bin), ($texts.HintConnectAt -f $program), ($texts.HintToolsAt -f $program)) }
        }
        Say $lines
        $global:LASTEXITCODE = 0
    } catch {
        if ("$_" -ne $refused) {
            $Host.UI.WriteErrorLine($_.Exception.Message)
        }
        $global:LASTEXITCODE = 1
    } finally {
        if ($tmp -and (Test-Path -LiteralPath $tmp)) {
            Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
        }
    }
} $Version $NoModifyPath.IsPresent
