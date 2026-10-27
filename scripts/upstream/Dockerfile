ARG BASE_IMAGE=registry.fedoraproject.org/fedora:44
FROM ${BASE_IMAGE}

RUN dnf install -y gcc gcc-c++ make autoconf automake libtool flex bison \
    git-core ca-certificates pkgconf-pkg-config popt-devel userspace-rcu-devel \
    libxml2-devel lttng-ust-devel libbabeltrace2-devel babeltrace2 python3-bt2 \
    python3-devel python3-lxml swig perl-Test-Harness libabigail \
    procps-ng util-linux findutils diffutils hostname which shadow-utils \
    && dnf clean all

ARG TEST_UID=1000
ARG TEST_GID=1000
RUN groupadd -g ${TEST_GID} tester && useradd -m -u ${TEST_UID} -g ${TEST_GID} tester
USER tester
ENV PATH=/opt/go/bin:/usr/bin:/bin
ENV PYTHON=/usr/bin/python3 PYTHON3=/usr/bin/python3 GOTOOLCHAIN=local
ENV LTTNG_TOOLS_DISABLE_KERNEL_TESTS=1
WORKDIR /work
