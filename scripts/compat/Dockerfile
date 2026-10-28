ARG BASE_IMAGE=debian:12
FROM ${BASE_IMAGE}
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
