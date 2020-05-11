#!/usr/bin/env bash

readonly OK=0
readonly NONOK=1
readonly UNKNOWN=2

readonly FILE=/rootfs/home/core/watched_file

echo "$(date)" >> /rootfs/home/core/test.log

if [ -f "$FILE" ]; then
  echo "$FILE exists"
  exit $NONOK
else
  echo "$FILE does not exist"
  exit $OK
fi
