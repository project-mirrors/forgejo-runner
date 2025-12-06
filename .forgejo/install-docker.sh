#!/usr/bin/env bash

set -eu -o pipefail

DOCKER_VERSION="$1"

if [[ "$DOCKER_VERSION" == "stable" ]];
then
  echo "Installing Docker from Debian's stable apt packages."
  apt-get install -y -qq docker.io

elif [[ "$DOCKER_VERSION" == "latest" ]];
then
  echo "Installing Docker from Docker's latest apt packages."

  # Derived from Docker's official debian installation documentation: https://docs.docker.com/engine/install/debian/#install-using-the-repository
  # Add Docker's official GPG key:
  apt-get update
  apt-get install -y -qq ca-certificates curl
  install -m 0755 -d /etc/apt/keyrings
  curl -fsSL https://download.docker.com/linux/debian/gpg -o /etc/apt/keyrings/docker.asc
  chmod a+r /etc/apt/keyrings/docker.asc

  # Add the repository to Apt sources:
  tee /etc/apt/sources.list.d/docker.sources <<EOF
Types: deb
URIs: https://download.docker.com/linux/debian
Suites: $(. /etc/os-release && echo "$VERSION_CODENAME")
Components: stable
Signed-By: /etc/apt/keyrings/docker.asc
EOF

  apt-get update
  apt-get install -y -qq docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

else
  echo "unknown docker"
  exit 1

fi

docker info
