FROM php:8.3-cli

# Install dependencies and PHP extensions
RUN apt-get update && apt-get install -y \
    libzip-dev \
    zip \
    unzip \
    git \
    wget \
    && docker-php-ext-install sockets \
    && rm -rf /var/lib/apt/lists/*

# Define Go version and install
ARG GO_VERSION=1.22.4

RUN wget https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz -O go.tar.gz \
    && tar -C /usr/local -xzf go.tar.gz \
    && rm go.tar.gz

# Set environment variables for Go
ENV PATH="/usr/local/go/bin:/root/go/bin:${PATH}"
ENV GOFLAGS="-mod=mod"

# Copy Composer from the official Composer image
COPY --from=composer:2 /usr/bin/composer /usr/bin/composer


COPY --from=golangci/golangci-lint:v1.59.1-alpine /usr/bin/golangci-lint /usr/local/bin/golangci-lint

RUN go install github.com/cespare/reflex@v0.3
