#!/bin/bash

set -e

if [[ ! -f "machine-controller" ]]; then
  echo "Building userdata"
  make build-machine-controller
fi

if [[ ! -f ".kubeconfig" ]]; then
  echo "Unable to find $(pwd)/.kubeconfig"
  echo "Make sure to put your .kubeconfig file into the root directory of the project"
  exit 1
fi

exec "$@"