FROM ghcr.io/roadrunner-server/roadrunner:3.0.1

ARG TARGETARCH

COPY --chmod=755 --chown=0:0 ./rr-${TARGETARCH} /usr/bin/rr
