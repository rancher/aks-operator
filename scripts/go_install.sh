#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

if [ -z "${1}" ]; then
  echo "must provide module as first parameter"
  exit 1
fi

if [ -z "${2}" ]; then
  echo "must provide binary name as second parameter"
  exit 1
fi

if [ -z "${3}" ]; then
  echo "must provide version as third parameter"
  exit 1
fi

if [ -z "${GOBIN}" ]; then
  echo "GOBIN is not set. Must set GOBIN to install the bin in a specified directory."
  exit 1
fi

rm "${GOBIN}/${2}"* 2> /dev/null || true

# install the golang module specified as the first argument
go install -tags tools "${1}@${3}"
mv "${GOBIN}/${2}" "${GOBIN}/${2}-${3}"
ln -sf "${GOBIN}/${2}-${3}" "${GOBIN}/${2}"
