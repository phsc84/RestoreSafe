# build.ps1 - Cross-platform build script for Windows and macOS

# Example usage:
#.\build.ps1
#.\build.ps1 -prod 1.0.0
#.\build.ps1 -prod -version 1.0.0

# Parse command-line arguments
param (
    [switch]$prod,      # -prod switch for production build
    [string]$version    # -version parameter to set the version
)

# Default values
$appName = "RestoreSafe"
$prodBuild = $false
$outputDir = "test"
$buildName = "development"
$buildVersion = "0.0.1-dev"

# Check for production build flag
if ($prod) {
    $prodBuild = $true
    $outputDir = "dist"
    $buildName = "production"

    # Override the default version if -version parameter was provided
    if ($version) {
        $buildVersion = $version
    } else {
        Write-Host "Using default version $buildVersion for production build."  -ForegroundColor Yellow
    }
}

# Create the output directory if it doesn't exist
if (!(Test-Path -Path $outputDir)) {
    New-Item -ItemType Directory -Path $outputDir | Out-Null
}

# Clean the output directory (only old versions of the app)
if (Test-Path -Path $outputDir) {
    Write-Host "Cleaning old versions from output directory..."

    # Create a regex pattern to match old versions
    $searchPattern = ""
    if ($prodBuild) {
        # Matches "AppName-Version_"
        $searchPattern = "^$appName-(.+)_"
    } else {
        # Matches "AppName.exe"
        $searchPattern = "^$appName\.exe$"
    }

    Get-ChildItem -Path $outputDir | Where-Object { $_.Name -match $searchPattern } | Remove-Item -Force
}

# Define the build targets based on build configuration
$targets = @()
if ($prodBuild) {
    # Production builds
    $targets += @{ GOOS = "windows"; GOARCH = "amd64"; Output = "$outputDir/$appName-$buildVersion" + "_win-x64.exe" }
    $targets += @{ GOOS = "darwin"; GOARCH = "arm64"; Output = "$outputDir/$appName-$buildVersion" + "_mac-arm64.app" }
} else {
    # Dev build (only Windows for faster local development)
    $targets += @{ GOOS = "windows"; GOARCH = "amd64"; Output = "$outputDir/$appName" + ".exe" }
}

# Build the application for each target
foreach ($target in $targets) {
    # Set environment variables
    $env:GOOS = $target.GOOS
    $env:GOARCH = $target.GOARCH

    # Build the application
    Write-Host "Building $buildName build for $($target.GOOS)/$($target.GOARCH)..."

    # Define the build command as an array
    $cmd = @("go", "build", "-o", $target.Output)

    # Add ldflags for version embedding (for production builds)
    if ($prodBuild) {
        $ldflags = "-ldflags ""-X main.version=$buildVersion -X main.buildTime=$(Get-Date -UFormat %s)"""
        $cmd += $ldflags
    }
    
    # Add the package or .go file AFTER the ldflags
    $cmd += "main.go"

    # Invoke the build command and capture output (same as before)
    $processInfo = New-Object System.Diagnostics.ProcessStartInfo
    $processInfo.FileName = $cmd[0]
    $processInfo.Arguments = ($cmd[1..($cmd.Length - 1)]) -join " " # Corrected join
    $processInfo.RedirectStandardOutput = $true
    $processInfo.RedirectStandardError = $true
    $processInfo.UseShellExecute = $false

    $process = New-Object System.Diagnostics.Process
    $process.StartInfo = $processInfo

    $process.Start() | Out-Null
    $stdout = $process.StandardOutput.ReadToEnd()
    $stderr = $process.StandardError.ReadToEnd()
    $process.WaitForExit()

    if ($process.ExitCode -ne 0) {
        Write-Host "Build failed for $($target.GOOS)/$($target.GOARCH)." -ForegroundColor Red
        if ($stderr) {
            Write-Host "Error details (stderr):" -ForegroundColor Red
            Write-Host $stderr -ForegroundColor DarkGray
        }
        if ($stdout) {
            Write-Host "Error details (stdout):" -ForegroundColor Red
            Write-Host $stdout -ForegroundColor DarkGray
        }
        exit 1
    }

    Write-Host "Build successful: $($target.Output)" -ForegroundColor Green
    
}

Write-Host "All builds completed!" -ForegroundColor Cyan