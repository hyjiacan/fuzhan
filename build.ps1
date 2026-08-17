$ErrorActionPreference = "Stop"

$PROJECT_NAME = "fuzhan"
$PLATFORMS = @(
    "windows/amd64",
    "linux/amd64"
)

$rootDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$uiDir = Join-Path $rootDir "ui"
$binDir = Join-Path $rootDir "bin"

# 默认值
$VERSION = Get-Date -Format "yyyy.MM.dd"
$target = $null
$platformFilter = $null
$pack = $false

# 平台别名映射
$PLATFORM_ALIASES = @{
    "w" = @("windows/amd64")
    "l" = @("linux/amd64")
}

# 解析参数
$i = 0
while ($i -lt $args.Length) {
    $arg = $args[$i]
    if ($arg -eq "-v" -or $arg -eq "--version") {
        if ($i + 1 -lt $args.Length) {
            $VERSION = $args[$i + 1]
            $i += 2
        } else {
            Write-Host "Error: -v requires a version argument" -ForegroundColor Red
            exit 1
        }
    } elseif ($arg -eq "-v*") {
        # 支持 -v2025.12.10 格式
        $VERSION = $arg.Substring(2)
        $i += 1
    } elseif ($arg -eq "-p" -or $arg -eq "--pack") {
        $pack = $true
        $i += 1
    } elseif ($arg -eq "server") {
        $target = "server"
        # 检查下一个参数是否是平台
        if ($i + 1 -lt $args.Length) {
            $next = $args[$i + 1]
            if ($PLATFORM_ALIASES.ContainsKey($next)) {
                $platformFilter = $PLATFORM_ALIASES[$next]
                $i += 2
            } elseif ($next -eq "-v" -or $next -eq "--version" -or $next -eq "ui") {
                # 这些不是平台，跳过
                $i += 1
            } elseif ($next -match "^windows|^linux|^darwin|^macos|^mac|^win") {
                # 平台名称但没有匹配别名，尝试直接匹配
                $platformFilter = @($next)
                $i += 2
            } else {
                $i += 1
            }
        } else {
            $i += 1
        }
    } else {
        $target = $arg
        $i += 1
    }
}

function Build-Platforms {
    param(
        [string[]]$Platforms,
        [switch]$Pack
    )

    foreach ($p in $Platforms) {
        $parts = $p -split "/"
        $OS = $parts[0]
        $ARCH = $parts[1]
        Write-Host "Building $OS $ARCH..." -NoNewline

        # 统一的可执行文件名
        $EXE_NAME = "fuzhan"
        if ($OS -eq "windows") {
            $EXE_NAME = "$EXE_NAME.exe"
        }

        Push-Location (Join-Path $rootDir "server")
        try {
            $exitCode = -1
            $env:GOOS = $OS
            $env:GOARCH = $ARCH
            go build -ldflags "-s -w" -o "$binDir/$EXE_NAME" .
            $exitCode = $LASTEXITCODE
        } finally {
            Remove-Item Env:GOOS, Env:GOARCH -ErrorAction SilentlyContinue
            Pop-Location
        }

        if ($exitCode -eq 0) {
            if ($Pack) {
                # 创建 zip 包
                $ZIP_NAME = "$PROJECT_NAME-$VERSION-$OS-$ARCH.zip"
                Compress-Archive -Path "$binDir/$EXE_NAME" -DestinationPath "$binDir/$ZIP_NAME" -Force
                Remove-Item "$binDir/$EXE_NAME" -Force
                Write-Host " OK ($ZIP_NAME)" -ForegroundColor Green
            } else {
                Write-Host " OK ($EXE_NAME)" -ForegroundColor Green
            }
        } else {
            Write-Host " FAIL" -ForegroundColor Red
            exit 1
        }
    }
}
if ($target -eq "ui") {
    Write-Host "=== Build Frontend ===" -ForegroundColor Cyan
    Push-Location $uiDir
    yarn build
    Pop-Location
    Write-Host "Frontend built to: $uiDir\..\server\web" -ForegroundColor Green
    exit 0
}
elseif ($target -eq "server") {
    Write-Host "=== Build Backend ($VERSION) ===" -ForegroundColor Cyan
    if (Test-Path $binDir) { Remove-Item "$binDir\*" -Recurse -Force }
    New-Item -ItemType Directory -Force -Path $binDir | Out-Null

    $platformsToBuild = if ($platformFilter) { $platformFilter } else { $PLATFORMS }
    Build-Platforms -Platforms $platformsToBuild -Pack:$pack

    Write-Host ""
    Write-Host "=== Done ===" -ForegroundColor Cyan
    if ($pack) {
        Get-ChildItem $binDir -Filter "*.zip" | ForEach-Object { Write-Host "  $($_.Name) ($([math]::Round($_.Length/1MB, 1)) MB)" }
    } else {
        Get-ChildItem $binDir -File | ForEach-Object { Write-Host "  $($_.Name) ($([math]::Round($_.Length/1MB, 1)) MB)" }
    }
    exit 0
}
elseif ($target -ne $null -and $target -ne "") {
    Write-Host "Usage: .\build.ps1 [-v version] [-p] [ui|server [w|l|m]]" -ForegroundColor Yellow
    Write-Host "  -v, --version <version>: Specify version (default: current date)" -ForegroundColor Gray
    Write-Host "  -p, --pack: Create zip packages (default: only build binary)" -ForegroundColor Gray
    Write-Host "  ui: build frontend only" -ForegroundColor Gray
    Write-Host "  server [w|l|m]: build backend (default: all platforms)" -ForegroundColor Gray
    Write-Host "    w: Windows x64" -ForegroundColor Gray
    Write-Host "    l: Linux x64" -ForegroundColor Gray
    Write-Host "    m: macOS (已移除)" -ForegroundColor Gray
    exit 1
}

# 默认：先编译 ui 再编译 server
Write-Host "=== Step 1: Build Frontend ===" -ForegroundColor Cyan
Push-Location $uiDir
yarn build
Pop-Location

Write-Host ""
Write-Host "=== Step 2: Build Backend ($VERSION) ===" -ForegroundColor Cyan
if (Test-Path $binDir) { Remove-Item "$binDir\*" -Recurse -Force }
New-Item -ItemType Directory -Force -Path $binDir | Out-Null
Build-Platforms -Platforms $PLATFORMS -Pack:$pack

Write-Host ""
Write-Host "=== Build Complete ===" -ForegroundColor Cyan
if ($pack) {
    Get-ChildItem $binDir -Filter "*.zip" | ForEach-Object { Write-Host "  $($_.Name) ($([math]::Round($_.Length/1MB, 1)) MB)" }
} else {
    Get-ChildItem $binDir -File | ForEach-Object { Write-Host "  $($_.Name) ($([math]::Round($_.Length/1MB, 1)) MB)" }
}