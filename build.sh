#!/bin/bash

source .github/env.sh

rm -rf build/android \
  build/java \
  build/javac-output \
  build/srcd

gomobile bind -v -cache $(realpath build) -trimpath -ldflags='-s -w' . || exit 1
rm -r libcore-sources.jar

proj=../SagerNetV5/app/libs
if [ -d $proj ]; then
  cp -f libcore.aar $proj
  echo ">> install $(realpath $proj)/libcore.aar"
fi