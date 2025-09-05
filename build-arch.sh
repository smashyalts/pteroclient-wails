#!/bin/bash

# Pterodactyl Manager - Arch Linux Build Script
# This script builds the application for Arch Linux

echo "Building Pterodactyl Manager for Arch Linux..."

# Check if Go is installed
if ! command -v go &> /dev/null; then
    echo "Go is not installed. Please install Go first:"
    echo "sudo pacman -S go"
    exit 1
fi

# Check if Wails is installed
if ! command -v wails &> /dev/null; then
    echo "Wails is not installed. Installing Wails..."
    go install github.com/wailsapp/wails/v2/cmd/wails@latest
    export PATH=$PATH:~/go/bin
fi

# Install required system dependencies for Wails on Arch
echo "Installing system dependencies..."
sudo pacman -S --needed webkit2gtk gtk3 libappindicator-gtk3

# Clean previous builds
echo "Cleaning previous builds..."
rm -rf build/bin

# Build the application
echo "Building application..."
wails build -s -platform linux/amd64

# Check if build was successful
if [ -f "build/bin/pteroclient-wails" ]; then
    echo "Build successful!"
    echo "Binary location: build/bin/pteroclient-wails"
    
    # Make the binary executable
    chmod +x build/bin/pteroclient-wails
    
    # Optional: Create AppImage for better portability
    read -p "Do you want to create an AppImage? (y/n) " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        echo "Creating AppImage..."
        wails build -s -platform linux/amd64 -package
    fi
else
    echo "Build failed!"
    exit 1
fi

echo "Build complete!"
