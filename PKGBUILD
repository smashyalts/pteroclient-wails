# Maintainer: Your Name <your.email@example.com>
pkgname=pteroclient-wails
pkgver=1.0.0
pkgrel=1
pkgdesc="A modern Pterodactyl Panel client with integrated file editor and console"
arch=('x86_64')
url="https://github.com/yourusername/pteroclient-wails"
license=('MIT')
depends=('webkit2gtk' 'gtk3')
makedepends=('go' 'git')
source=("$pkgname-$pkgver.tar.gz::$url/archive/v$pkgver.tar.gz")
sha256sums=('SKIP')

prepare() {
    cd "$srcdir/$pkgname-$pkgver"
    
    # Install Wails if not present
    if ! command -v wails &> /dev/null; then
        export GOPATH="$srcdir/go"
        export PATH="$PATH:$GOPATH/bin"
        go install github.com/wailsapp/wails/v2/cmd/wails@latest
    fi
}

build() {
    cd "$srcdir/$pkgname-$pkgver"
    
    export GOPATH="$srcdir/go"
    export PATH="$PATH:$GOPATH/bin"
    export CGO_CPPFLAGS="${CPPFLAGS}"
    export CGO_CFLAGS="${CFLAGS}"
    export CGO_CXXFLAGS="${CXXFLAGS}"
    export CGO_LDFLAGS="${LDFLAGS}"
    export GOFLAGS="-buildmode=pie -trimpath -ldflags=-linkmode=external -mod=readonly -modcacherw"
    
    # Build with Wails
    wails build -s -platform linux/amd64
}

package() {
    cd "$srcdir/$pkgname-$pkgver"
    
    # Install binary
    install -Dm755 "build/bin/$pkgname" "$pkgdir/usr/bin/$pkgname"
    
    # Install desktop file
    install -Dm644 "build/linux/$pkgname.desktop" "$pkgdir/usr/share/applications/$pkgname.desktop"
    
    # Install icon (if exists)
    if [ -f "build/appicon.png" ]; then
        install -Dm644 "build/appicon.png" "$pkgdir/usr/share/pixmaps/$pkgname.png"
    fi
    
    # Install license
    install -Dm644 LICENSE "$pkgdir/usr/share/licenses/$pkgname/LICENSE"
}
