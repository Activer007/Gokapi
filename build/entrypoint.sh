#!/bin/bash

set -e

targets=${@-"darwin/amd64 darwin/arm64 linux/amd64 linux/386 linux/arm linux/arm64 linux/riscv64 windows/amd64 windows/arm64"}

cd /src/supashare
go generate ./...


VERSION_GOKAPI=$(grep -oP 'const versionGokapi = "\K[^"]+' cmd/supashare/Main.go)
VERSION_CLI=$(grep -oP 'const version = "v\K[^"]+' cmd/cli-uploader/cliflags/cliflags.go)
 
echo
echo "SupaShare version : ${VERSION_GOKAPI}"
echo "CLI version    : ${VERSION_CLI}"
echo


echo
echo "Building CLI Binary"
echo

for target in $targets; do
	os="$(echo $target | cut -d '/' -f1)"
	arch="$(echo $target | cut -d '/' -f2)"
	output="build/supashare-cli"
	zipname="$output-${VERSION_CLI}_${os}-${arch}.zip"
	if [ $os = "windows" ]; then
		output+='.exe'
	fi

	echo "----> Building SupaShare CLI for $target"
	GOOS=$os GOARCH=$arch CGO_ENABLED=0 go build -ldflags="-s -w -X 'github.com/Activer007/SupaShare/internal/environment.Builder=Github Release Builder' -X 'github.com/Activer007/SupaShare/internal/environment.BuildTime=$(date)'" -o $output github.com/Activer007/SupaShare/cmd/cli-uploader
	zip -j $zipname $output >/dev/null
	rm $output
done

echo
echo "Building Main Binary"
echo

for target in $targets; do
	os="$(echo $target | cut -d '/' -f1)"
	arch="$(echo $target | cut -d '/' -f2)"
	output="build/supashare"
	zipname="$output-${VERSION_GOKAPI}_${os}-${arch}.zip"
	if [ $os = "windows" ]; then
		output+='.exe'
	fi

	echo "----> Building SupaShare for $target"
	GOOS=$os GOARCH=$arch CGO_ENABLED=0 go build -ldflags="-s -w -X 'github.com/Activer007/SupaShare/internal/environment.Builder=Github Release Builder' -X 'github.com/Activer007/SupaShare/internal/environment.BuildTime=$(date)'" -o $output github.com/Activer007/SupaShare/cmd/supashare
	zip -j $zipname $output >/dev/null
	rm $output
done


echo "----> Build is complete. List of files at build/:"
cd build/
ls -l supashare-*
