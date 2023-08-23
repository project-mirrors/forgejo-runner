FROM --platform=$BUILDPLATFORM tonistiigi/xx AS xx

FROM --platform=$BUILDPLATFORM docker.io/library/golang:1.21-alpine3.18 as build-env

#
# Transparently cross compile for the target platform
#
COPY --from=xx / /
ARG TARGETPLATFORM
RUN apk --no-cache add clang lld
RUN xx-apk --no-cache add gcc musl-dev
RUN xx-go --wrap

# Do not remove `git` here, it is required for getting runner version when executing `make build`
RUN apk add --no-cache build-base git

COPY . /srv
WORKDIR /srv

RUN make clean && make build

FROM docker.io/library/alpine:3.18
LABEL maintainer="contact@forgejo.org"

RUN apk add --no-cache git bash

COPY --from=build-env /srv/forgejo-runner /bin/forgejo-runner

ENV HOME=/data

USER 1000:1000

WORKDIR /data

VOLUME ["/data"]

CMD ["/bin/forgejo-runner"]
