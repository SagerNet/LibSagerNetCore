#!/bin/bash

source .github/env.sh

go mod download -x
go get -v github.com/sagernet/gomobile/cmd/gomobile@v0.0.0-20210822074701-68a55075c7d2
go get -v github.com/sagernet/gomobile/cmd/gobind@v0.0.0-20210822074701-68a55075c7d2
gomobile init
