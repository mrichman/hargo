cd ..

mkdir -p binaries
mkdir -p hargo-$1

cp LICENSE hargo-$1
cp README.md hargo-$1

HASH="$(git rev-parse --short HEAD)"
VERSION="$(go run tools/build-version.go)"
DATE="$(go run tools/build-date.go)"

# Mac
echo "OSX 64"
GOOS=darwin GOARCH=amd64 go build -ldflags "-s -w -X main.Version=$1 -X main.CommitHash=$HASH -X 'main.CompileDate=$DATE'" -o hargo-$1/hargo ./cmd/hargo
tar -czf hargo-$1-osx.tar.gz hargo-$1
mv hargo-$1-osx.tar.gz binaries

# Linux
echo "Linux 64"
GOOS=linux GOARCH=amd64 go build -ldflags "-s -w -X main.Version=$1 -X main.CommitHash=$HASH -X 'main.CompileDate=$DATE'" -o hargo-$1/hargo ./cmd/hargo
tar -czf hargo-$1-linux64.tar.gz hargo-$1
mv hargo-$1-linux64.tar.gz binaries
echo "Linux 32"
GOOS=linux GOARCH=386 go build -ldflags "-s -w -X main.Version=$1 -X main.CommitHash=$HASH -X 'main.CompileDate=$DATE'" -o hargo-$1/hargo ./cmd/hargo
tar -czf hargo-$1-linux32.tar.gz hargo-$1
mv hargo-$1-linux32.tar.gz binaries
echo "Linux arm"
GOOS=linux GOARCH=arm go build -ldflags "-s -w -X main.Version=$1 -X main.CommitHash=$HASH -X 'main.CompileDate=$DATE'" -o hargo-$1/hargo ./cmd/hargo
tar -czf hargo-$1-linux-arm.tar.gz hargo-$1
mv hargo-$1-linux-arm.tar.gz binaries

# NetBSD
echo "NetBSD 64"
GOOS=netbsd GOARCH=amd64 go build -ldflags "-s -w -X main.Version=$1 -X main.CommitHash=$HASH -X 'main.CompileDate=$DATE'" -o hargo-$1/hargo ./cmd/hargo
tar -czf hargo-$1-netbsd64.tar.gz hargo-$1
mv hargo-$1-netbsd64.tar.gz binaries
echo "NetBSD 32"
GOOS=netbsd GOARCH=386 go build -ldflags "-s -w -X main.Version=$1 -X main.CommitHash=$HASH -X 'main.CompileDate=$DATE'" -o hargo-$1/hargo ./cmd/hargo
tar -czf hargo-$1-netbsd32.tar.gz hargo-$1
mv hargo-$1-netbsd32.tar.gz binaries

# OpenBSD
echo "OpenBSD 64"
GOOS=openbsd GOARCH=amd64 go build -ldflags "-s -w -X main.Version=$1 -X main.CommitHash=$HASH -X 'main.CompileDate=$DATE'" -o hargo-$1/hargo ./cmd/hargo
tar -czf hargo-$1-openbsd64.tar.gz hargo-$1
mv hargo-$1-openbsd64.tar.gz binaries
echo "OpenBSD 32"
GOOS=openbsd GOARCH=386 go build -ldflags "-s -w -X main.Version=$1 -X main.CommitHash=$HASH -X 'main.CompileDate=$DATE'" -o hargo-$1/hargo ./cmd/hargo
tar -czf hargo-$1-openbsd32.tar.gz hargo-$1
mv hargo-$1-openbsd32.tar.gz binaries

# FreeBSD
echo "FreeBSD 64"
GOOS=freebsd GOARCH=amd64 go build -ldflags "-s -w -X main.Version=$1 -X main.CommitHash=$HASH -X 'main.CompileDate=$DATE'" -o hargo-$1/hargo ./cmd/hargo
tar -czf hargo-$1-freebsd64.tar.gz hargo-$1
mv hargo-$1-freebsd64.tar.gz binaries
echo "FreeBSD 32"
GOOS=freebsd GOARCH=386 go build -ldflags "-s -w -X main.Version=$1 -X main.CommitHash=$HASH -X 'main.CompileDate=$DATE'" -o hargo-$1/hargo ./cmd/hargo
tar -czf hargo-$1-freebsd32.tar.gz hargo-$1
mv hargo-$1-freebsd32.tar.gz binaries

rm hargo-$1/hargo

# Windows
echo "Windows 64"
GOOS=windows GOARCH=amd64 go build -ldflags "-s -w -X main.Version=$1 -X main.CommitHash=$HASH -X 'main.CompileDate=$DATE'" -o hargo-$1/hargo.exe ./cmd/hargo
zip -r -q -T hargo-$1-win64.zip hargo-$1
mv hargo-$1-win64.zip binaries
echo "Windows 32"
GOOS=windows GOARCH=386 go build -ldflags "-s -w -X main.Version=$1 -X main.CommitHash=$HASH -X 'main.CompileDate=$DATE'" -o hargo-$1/hargo.exe ./cmd/hargo
zip -r -q -T hargo-$1-win32.zip hargo-$1
mv hargo-$1-win32.zip binaries

rm -rf hargo-$1