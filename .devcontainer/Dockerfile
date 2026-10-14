# The dev container (DEV-03): the toolchain of mise.toml, baked in so a new clone needs no
# installs. Docker comes from the docker-in-docker feature in devcontainer.json.
FROM mcr.microsoft.com/devcontainers/base:ubuntu-24.04@sha256:d94c97dd9cacf183d0a6fd12a8e87b526e9e928307674ae9c94139139c0c6eae

# mise, pinned and checked against the release's SHASUMS256.txt.
ARG MISE_VERSION=v2026.10.9
ARG MISE_SHA256=6ab6646b52b83f6bbd51c9d0d3ae54264ffa90efd1ac6d7c0ec5a86bbc6a0030
RUN curl -fsSLo /usr/local/bin/mise "https://github.com/jdx/mise/releases/download/${MISE_VERSION}/mise-${MISE_VERSION}-linux-x64" \
 && echo "${MISE_SHA256}  /usr/local/bin/mise" | sha256sum -c - \
 && chmod 0755 /usr/local/bin/mise

USER vscode
ENV MISE_DATA_DIR=/home/vscode/.local/share/mise PATH=/home/vscode/.local/share/mise/shims:$PATH
# Install the pinned tools at image build time; the workspace's mise.toml then finds them.
COPY --chown=vscode:vscode mise.toml /tmp/toolchain/mise.toml
RUN cd /tmp/toolchain && mise trust --yes mise.toml && mise install --yes \
 && echo 'eval "$(mise activate bash)"' >> /home/vscode/.bashrc
