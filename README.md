# RestoreSafe
A simple and effective command-line tool for creating encrypted archives from directories, suitable for synchronization with cloud services.

## Configuration

### Windows
1. Download RestoreSafe and move it to your desired folder.
2. Copy "config\Windows\config_SAMPLE.json" to the same folder and rename it to "config.json".
3. Amend "config.json" to your needs.
4. "RestoreSafe.exe" cannot be executed via double-click. If you wish to execute RestoreSafe directly via double-click, please also copy "config\Windows\RestoreSafe.bat" to the same folder and double-click "RestoreSafe.bat".

### macOS
1. Download RestoreSafe and move it to your desired folder.
2. Copy "config\macOS\config_SAMPLE.json" to the same folder and rename it to "config.json".
3. Amend "config.json" to your needs.
4. On macOS you need to code sign the application and remove the malware check. Copy "config\macOS\mac.entitlements" to the same folder.
    1. Open the terminal and navigate to the folder where you moved RestoreSafe.
    '''
    cd "/path/to/RestoreSafe"
    '''
    2. Code sign RestoreSafe.
    '''
    codesign --entitlements mac.entitlements -s - "RestoreSafe"
    '''
    3. Remove malware check (quarantine attribute)
    '''
    xattr -d com.apple.quarantine $appPath "RestoreSafe"
    '''

## Usage
tbd