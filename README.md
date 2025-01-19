# RestoreSafe
A simple and effective command-line tool for creating encrypted archives from directories, suitable for synchronization with cloud services.

## Installation and configuration

### Windows
tbd

### macOS
On macOS you need to code sign the application and remove the malware check.
1. Navigate to the path where you place RestoreSafe
    '''
    cd "/path/to/RestoreSafe"
    '''
2. Code sign RestoreSafe
    '''
    codesign --entitlements mac.entitlements -s - "RestoreSafe"
    '''
3. Remove malware check (quarantine attribute)
    '''
    xattr -d com.apple.quarantine $appPath "RestoreSafe"
    '''
