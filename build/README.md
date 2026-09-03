# Build Directory

The build directory is used to house all the build files and assets for your application. 

The structure is:

* bin - Output directory
* darwin - macOS specific files
* windows - Windows specific files

## Mac

The `darwin` directory holds files specific to Mac builds.
These may be customised and used as part of the build. To return these files to the default state, simply delete them
and
build with `wails build`.

The directory contains the following files:

- `Info.plist` - the main plist file used for Mac builds. It is used when building using `wails build`.
- `Info.dev.plist` - same as the main plist file but used when building using `wails dev`.

## Windows

The `windows` directory contains the manifest and rc files used when building with `wails build`.
These may be customised for your application. To return these files to the default state, simply delete them and
build with `wails build`.

- `icon.ico` - The icon used for the application. This is used when building using `wails build`. If you wish to
  use a different icon, simply replace this file with your own. If it is missing, a new `icon.ico` file
  will be created using the `appicon.png` file in the build directory.
- `installer/*` - The files used to create the Windows installer. These are used when building using `wails build`.
- `info.json` - Application details used for Windows builds. The data here will be used by the Windows installer,
  as well as the application itself (right click the exe -> properties -> details)
- `app.json` - Single source of app metadata (UI version label, about dialog, Windows exe properties).
  Before each build `sync_app_config.go` embeds it into the binary and updates `wails.json` to match.
- `wails.exe.manifest` - The main application manifest file.
- `create-codesign-cert.ps1` - Creates a self-signed code signing certificate (`codesign.pfx` private, `codesign.cer` public).
- `sign-binaries.ps1` - Signs `build/bin` executables with Authenticode after `wails build --nsis`.
- `installer/project.nsi` - NSIS installer; requires admin to import `codesign.cer` into LocalMachine TrustedPeople and TrustedPublisher.