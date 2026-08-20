# Start from the Go 1.26 devcontainer image on Debian Bookworm.
FROM mcr.microsoft.com/devcontainers/go@sha256:1a3df7e2f24378acfb7c1048b7b018813112f280f011bad4aa9b02fff600481e

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        libffi-dev \
        libyaml-dev \
        xxd \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*
