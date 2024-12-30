#!/bin/bash

# https://unix.stackexchange.com/questions/30091/fix-or-alternative-for-mktemp-in-os-x
MYTEMPDIR=$(mktemp -d 2>/dev/null || mktemp -d -t 'mytmpdir')

cd server
go install github.com/a-h/templ/cmd/templ@latest
go generate
go build -o "$MYTEMPDIR/server"
cd ..

cp -r static "$MYTEMPDIR"

tar -czvf bundle.tar.gz --directory="$MYTEMPDIR" .

rm -r "$MYTEMPDIR"
