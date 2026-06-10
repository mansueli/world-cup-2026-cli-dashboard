#!/bin/bash
wget -qO- https://github.com/goreleaser/goreleaser/releases/download/v2.16.0/goreleaser_Linux_x86_64.tar.gz | tar xz
./goreleaser check
