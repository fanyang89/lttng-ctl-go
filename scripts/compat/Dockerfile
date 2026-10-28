ARG BASE_IMAGE=debian:12
FROM ${BASE_IMAGE} AS base
ARG DEBIAN_MIRROR=deb.debian.org
RUN find /etc/apt -type f \( -name '*.list' -o -name '*.sources' \) \
    -exec sed -i "s/deb.debian.org/${DEBIAN_MIRROR}/g" {} + \
    && apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    build-essential autoconf automake libtool flex bison git ca-certificates \
    pkg-config libpopt-dev liburcu-dev libxml2-dev uuid-dev libnuma-dev \
    babeltrace2 libbabeltrace2-dev python3 clang binutils procps util-linux \
    && rm -rf /var/lib/apt/lists/*
ARG TEST_UID=1000
ARG TEST_GID=1000
RUN groupadd -g ${TEST_GID} tester && useradd -m -u ${TEST_UID} -g ${TEST_GID} tester
USER tester
ENV PATH=/opt/go/bin:/usr/bin:/bin GOTOOLCHAIN=local
WORKDIR /sdk

FROM base AS native
USER root
COPY fetch.sh run.sh versions.tsv urcu.tsv /sdk/scripts/compat/
ENV LTTNG_COMPAT_NATIVE=/opt/lttng
ARG LTTNG_BUILD_JOBS=8
RUN sh /sdk/scripts/compat/fetch.sh \
    && bash /sdk/scripts/compat/run.sh build \
    && cp /sdk/scripts/compat/*.tsv /opt/lttng/ \
    && cp -r /work/logs /opt/lttng/build-logs

FROM base AS runner
USER root
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    curl libicu72 libssl3 zlib1g unzip \
    && rm -rf /var/lib/apt/lists/*
ARG GO_VERSION=1.27.1
# Official https://go.dev/dl/?mode=json&include=all checksum.
ARG GO_SHA256=63d339f0da5ab53635a56f2490a7984dfe12dfcff22ad749f63edaf590168445
RUN set -eu; \
    for source in https://golang.google.cn/dl https://go.dev/dl; do \
      if curl -fL --retry 3 --connect-timeout 15 --max-time 300 \
        "$source/go$GO_VERSION.linux-amd64.tar.gz" -o /tmp/go.tar.gz \
        && echo "$GO_SHA256  /tmp/go.tar.gz" | sha256sum -c -; then break; fi; \
    done; \
    echo "$GO_SHA256  /tmp/go.tar.gz" | sha256sum -c -; \
    tar -xzf /tmp/go.tar.gz -C /opt; rm /tmp/go.tar.gz
ARG RUNNER_VERSION=2.338.0
# Official repository Actions runner downloads API checksum.
ARG RUNNER_SHA256=af4b794c1bc41d73d40535e3fe092a39f9679cd8d965954c2aca25a05ca41d32
RUN curl -fL --retry 3 --connect-timeout 15 --max-time 600 \
    "https://github.com/actions/runner/releases/download/v$RUNNER_VERSION/actions-runner-linux-x64-$RUNNER_VERSION.tar.gz" \
    -o /tmp/runner.tar.gz \
    && echo "$RUNNER_SHA256  /tmp/runner.tar.gz" | sha256sum -c - \
    && mkdir /opt/actions-runner \
    && tar -xzf /tmp/runner.tar.gz -C /opt/actions-runner \
    && rm /tmp/runner.tar.gz
COPY --from=native /opt/lttng /opt/lttng
ENV GOPROXY=https://goproxy.cn \
    LTTNG_COMPAT_NATIVE=/opt/lttng \
    HOME=/home/tester
USER tester
WORKDIR /home/tester
RUN go version && cp -a /opt/actions-runner /tmp/runner-check \
    && /tmp/runner-check/bin/Runner.Listener --version \
    && rm -rf /tmp/runner-check

# Preserve the lightweight local harness as the default build target.
FROM base AS tester
