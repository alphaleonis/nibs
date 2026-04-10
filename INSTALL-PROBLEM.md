# install.ps1 fails under `irm | iex` in pwsh 7.6.0

## Symptom

On a Windows 10 26200 machine running pwsh 7.6.0, invoking the install script via
the documented one-liner fails at the extraction step:

```
❯ $PSVersionTable

Name                           Value
----                           -----
PSVersion                      7.6.0
PSEdition                      Core
GitCommitId                    7.6.0
OS                             Microsoft Windows 10.0.26200
Platform                       Win32NT
PSCompatibleVersions           {1.0, 2.0, 3.0, 4.0…}
...

❯ irm https://raw.githubusercontent.com/alphaleonis/nibs/main/install.ps1 | iex
Installing nibs  (windows/amd64)
Version: v0.1.0
Downloading https://github.com/alphaleonis/nibs/releases/download/v0.1.0/nibs_0.1.0_windows_amd64.zip
Checksum verified
Invoke-Expression: A parameter cannot be found that matches parameter name 'DestinationPath'
```

The script runs fine up to "Checksum verified", then fails at the next step
(`Expand-Archive`). The error is attributed to `Invoke-Expression`, **not** to
`Expand-Archive` — which is the weird part.

## What already doesn't reproduce the bug

The same failing machine is needed — these all succeed in our local testing:

- `pwsh.exe -File install.ps1 ...` on pwsh 7.5.5 (Windows 10 latest) — OK
- `powershell.exe -File install.ps1 ...` (Windows PowerShell 5.1) — OK
- `irm URL | iex` on pwsh 7.5.5 (Windows 10 latest) — OK
- `irm URL | iex` on pwsh 7.6.0 **on a different machine** (Windows 10 latest) — OK

So the bug requires **pwsh 7.6.0 on the specific failing machine**. Something
in that environment — profile, modules, `$PSDefaultParameterValues`, installed
module version of `Microsoft.PowerShell.Archive`, etc. — is contributing.

## What we've already tried in install.ps1

Current `install.ps1` (this branch) already has the "obvious" robustness
improvements:

1. Archive and destination paths stored in explicit `$ArchivePath` / `$ExtractDir`
   variables.
2. Extract target is a dedicated `extracted/` subdirectory of the temp dir, so
   the archive file is no longer inside the destination folder.
3. `Expand-Archive -LiteralPath $ArchivePath -DestinationPath $ExtractDir -Force`
   (switched from `-Path` with an inline `Join-Path` subexpression).

These fixed the related `-File` invocation issues on 7.5.x, but did **not**
fix the 7.6.0 `irm | iex` scenario described above.

## Leading hypotheses (to validate on the failing machine)

1. **Error-attribution change in pwsh 7.6.** The *actual* error is still a
   parameter binding failure on `Expand-Archive -DestinationPath`, but 7.6
   now blames the outer `Invoke-Expression` in the displayed error record.
   The real culprit would be something interfering with `Expand-Archive`'s
   parameter set — e.g., an old shim module loaded before
   `Microsoft.PowerShell.Archive`, or `$PSDefaultParameterValues` with a
   wildcard like `*:DestinationPath`.

2. **Profile / environment difference.** The failing machine may load a
   profile that injects a `$PSDefaultParameterValues` entry, shadows the
   `Expand-Archive` command, or aliases `iex`/`Invoke-Expression` to something
   that accepts fewer parameters.

3. **Archive module version mismatch.** An older `Microsoft.PowerShell.Archive`
   module may be in `$env:PSModulePath` ahead of the built-in one, exposing a
   different parameter set for `Expand-Archive`.

4. **Parser/scope quirk specific to 7.6.** Less likely, but possible: a 7.6
   parser change in how `[CmdletBinding()] param(...)`-bearing scripts are
   evaluated via `iex` when piped from `irm`.

## What to investigate on the failing machine

Run each of the following on **the machine where the bug reproduces**,
ideally *before* any profile customization, to pinpoint the real culprit:

```powershell
# 0. Confirm versions
$PSVersionTable.PSVersion
(Get-Module Microsoft.PowerShell.Archive -ListAvailable | Sort-Object Version -Desc) |
    Select-Object Version, ModuleBase

# 1. Is the cmdlet shadowed?
Get-Command Expand-Archive -All
Get-Command Invoke-Expression -All
Get-Command iex -All

# 2. Any relevant default parameter values set?
$PSDefaultParameterValues

# 3. Does the fix work with no profile loaded?
#    (expect: success — if yes, a profile is the culprit)
pwsh -NoProfile -Command "irm https://raw.githubusercontent.com/alphaleonis/nibs/fix/install-ps1-pwsh-iex/install.ps1 | iex"

# 4. Does bypassing the cmdlet entirely work?
#    (temporary sanity check — run from the affected shell with profile loaded)
$tmp = Join-Path $env:TEMP "nibs-manual-$([guid]::NewGuid().ToString('N').Substring(0,8))"
New-Item -ItemType Directory $tmp -Force | Out-Null
$zip = Join-Path $tmp "nibs.zip"
Invoke-WebRequest https://github.com/alphaleonis/nibs/releases/download/v0.1.0/nibs_0.1.0_windows_amd64.zip -OutFile $zip -UseBasicParsing
Add-Type -AssemblyName System.IO.Compression.FileSystem
[System.IO.Compression.ZipFile]::ExtractToDirectory($zip, $tmp)
Get-ChildItem $tmp

# 5. Reproduce the exact failure with maximum context:
$ErrorActionPreference = 'Stop'
try {
    irm https://raw.githubusercontent.com/alphaleonis/nibs/fix/install-ps1-pwsh-iex/install.ps1 | iex
} catch {
    $_ | Format-List * -Force
    $_.InvocationInfo | Format-List * -Force
    $_.ScriptStackTrace
    $_.Exception | Format-List * -Force
}
```

The output of step 5 is the most useful single piece of information — it will
show whether the error truly originates from `Invoke-Expression` or whether
`Expand-Archive` is the real source and pwsh 7.6 is just misreporting.

## Candidate fix if the cmdlet route stays broken

If the root cause turns out to be environmental and can't be reasonably
expected to be clean on every user's machine, the fallback is to stop using
`Expand-Archive` altogether and use the .NET API directly:

```powershell
Add-Type -AssemblyName System.IO.Compression.FileSystem
[System.IO.Compression.ZipFile]::ExtractToDirectory($ArchivePath, $ExtractDir)
```

This bypasses cmdlet parameter binding, `$PSDefaultParameterValues`, and any
module-version differences — at the cost of not supporting overwrite semantics
(we don't need that, since `$ExtractDir` is freshly created per run).

## How to reproduce on the affected machine

1. Clone this branch:
   ```sh
   git clone -b fix/install-ps1-pwsh-iex https://github.com/alphaleonis/nibs.git
   cd nibs
   ```
2. Open pwsh 7.6.0 on that machine and run the investigation script block
   above (section "What to investigate on the failing machine"). Capture the
   output of step 5 in particular.
3. Report back so the root cause can be pinpointed and a targeted fix made.
