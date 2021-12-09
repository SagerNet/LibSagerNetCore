#!/bin/bash

source .github/env.sh

BUILD="../libcore_build"

rm -rf $BUILD/android \
  $BUILD/java \
  $BUILD/javac-output \
  $BUILD/src

gomobile bind -v -cache $(realpath $BUILD) -trimpath -ldflags='-s -w' . || exit 1
rm -r libcore-sources.jar

proj=../SagerNet/app/libs
if [ -d $proj ]; then
  cp -f libcore.aar $proj
  echo ">> install $(realpath $proj)/libcore.aar"
fi