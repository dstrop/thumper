FROM ghcr.io/roadrunner-server/roadrunner:2024.3.4

ARG TARGETARCH

COPY ./rr-${TARGETARCH} /usr/bin/rr
RUN  chmod a+x /usr/bin/rr
