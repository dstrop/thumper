FROM ghcr.io/roadrunner-server/roadrunner:2025.1.15

ARG TARGETARCH

COPY --chmod=755 --chown=0:0 ./rr-${TARGETARCH} /usr/bin/rr
