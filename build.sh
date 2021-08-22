#!/bin/bash

source .github/env.sh

cache=$(realpath build)
gomobile bind -v -cache $cache -trimpath -ldflags='-s -w' . || exit 1
rm -r libcore-sources.jar
