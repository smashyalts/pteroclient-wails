# Building Pterodactyl Manager for Arch Linux

## Prerequisites

### 1. Install Required Packages

```bash
# Install Go and build dependencies
sudo pacman -S go git base-devel

# Install Wails dependencies
sudo pacman -S webkit2gtk gtk3
```

### 2. Install Wails CLI

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest

# Add Go bin to PATH (add to ~/.bashrc or ~/.zshrc for persistence)
export PATH=$PATH:~/go/bin
```

## Building from Source

### Method 1: Quick Build Script

```bash
# Clone the repository
git clone https://github.com/yourusername/pteroclient-wails.git
cd pteroclient-wails

# Make build script executable
chmod +x build-arch.sh

# Run the build script
./build-arch.sh
```

### Method 2: Manual Build

```bash
# Clone the repository
git clone https://github.com/yourusername/pteroclient-wails.git
cd pteroclient-wails

# Build the application
wails build -s

# The binary will be in build/bin/pteroclient-wails
```

### Method 3: Using PKGBUILD (AUR-style)

```bash
# Clone the repository
git clone https://github.com/yourusername/pteroclient-wails.git
cd pteroclient-wails

# Build and install package
makepkg -si
```

## Creating an AppImage (Optional)

For better portability across Linux distributions:

```bash
# Build with packaging
wails build -s -platform linux/amd64 -package

# The AppImage will be in build/bin/
```

## Running the Application

### From Build Directory
```bash
./build/bin/pteroclient-wails
```

### After Installation (PKGBUILD)
```bash
pteroclient-wails
```

## Troubleshooting

### Missing Dependencies

If you encounter errors about missing libraries:

```bash
# Install additional GTK dependencies
sudo pacman -S libappindicator-gtk3 libnotify

# For WebKit issues
sudo pacman -S webkit2gtk-4.1
```

### Permission Issues

If the binary doesn't execute:

```bash
chmod +x build/bin/pteroclient-wails
```

### Display Issues

For Wayland users:

```bash
# Run with X11 backend
GDK_BACKEND=x11 ./build/bin/pteroclient-wails
```

## Creating a Package for Distribution

### For AUR Submission

1. Update the PKGBUILD with:
   - Your maintainer information
   - Correct source URL
   - Proper version number
   - Calculate sha256sums: `makepkg -g`

2. Test the package:
```bash
makepkg -si
namcap PKGBUILD
namcap pteroclient-wails-*.pkg.tar.zst
```

3. Create .SRCINFO:
```bash
makepkg --printsrcinfo > .SRCINFO
```

### Building for Multiple Architectures

```bash
# For ARM64 (aarch64)
GOARCH=arm64 wails build -s -platform linux/arm64

# For ARMv7
GOARCH=arm GOARM=7 wails build -s -platform linux/arm
```

## Dependencies Summary

### Runtime Dependencies
- `webkit2gtk` - WebKit rendering engine
- `gtk3` - GTK3 toolkit

### Build Dependencies
- `go` (>= 1.18)
- `git`
- `base-devel` (for makepkg)

### Optional Dependencies
- `libappindicator-gtk3` - System tray support
- `libnotify` - Desktop notifications

## File Locations

After installation:
- Binary: `/usr/bin/pteroclient-wails`
- Desktop file: `/usr/share/applications/pteroclient-wails.desktop`
- Icon: `/usr/share/pixmaps/pteroclient-wails.png`
- Config: `~/.config/pteroclient-wails/config.json`

## Notes

- The application requires an active internet connection to connect to Pterodactyl panels
- Configuration is stored in the user's home directory
- The application uses WebView2 on Windows but WebKitGTK on Linux
- Some features may behave slightly differently between platforms
