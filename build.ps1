# Construit Chili Lab : l'application, puis (avec -Installer) son installateur.
#
#   powershell -ExecutionPolicy Bypass -File build.ps1
#   powershell -ExecutionPolicy Bypass -File build.ps1 -Installer   # + dist\ChiliLabSetup-x.y.z.exe
#
# Chili Lab réutilise le code du langage (compilateur, carte, firmware) :
# le dépôt chili doit être cloné à côté (..\chili), cf. go.mod.
# Pour embarquer un firmware à jour, lance d'abord ..\chili\build.ps1.

param([switch]$Installer)
$ErrorActionPreference = 'Stop'
$root = $PSScriptRoot
$version = (Select-String -Path (Join-Path $root 'main.go') -Pattern 'const version = "([^"]+)"').Matches[0].Groups[1].Value

Write-Host "== Chili Lab $version"
Push-Location $root
try {
    go build -ldflags '-H=windowsgui -s -w' -o 'Chili Lab.exe' .
    if ($LASTEXITCODE -ne 0) { throw 'échec de la compilation' }
} finally {
    Pop-Location
}
Write-Host "OK : $(Join-Path $root 'Chili Lab.exe')"

if ($Installer) {
    Write-Host '== Installateur'
    $iscc = Join-Path $env:LOCALAPPDATA 'Programs\Inno Setup 6\ISCC.exe'
    if (-not (Test-Path $iscc)) { $iscc = 'C:\Program Files (x86)\Inno Setup 6\ISCC.exe' }
    & $iscc /Q "/DAppVersion=$version" (Join-Path $root 'installer\chili-lab.iss')
    if ($LASTEXITCODE -ne 0) { throw "échec de la compilation de l'installateur" }
    Write-Host "OK : $(Join-Path $root "dist\ChiliLabSetup-$version.exe")"
}
