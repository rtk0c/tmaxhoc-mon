#!/bin/bash

# https://unix.stackexchange.com/questions/30091/fix-or-alternative-for-mktemp-in-os-x
MYTEMPDIR=$(mktemp -d 2>/dev/null || mktemp -d -t 'mytmpdir')

cd server
go build -o "$MYTEMPDIR/server"
cd ..

cp -r panel "$MYTEMPDIR"

tar -czvf bundle.tar.gz --directory="$MYTEMPDIR" .

rm -r "$MYTEMPDIR"
