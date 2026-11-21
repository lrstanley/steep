# Zig 0.17 toolchain used to compile lib-vt to wasm32-freestanding.
# The compiler stays in this image; the host only needs Docker and Go.
# The module is built into the image. docker run writes it to stdout.
FROM debian:bookworm-slim

RUN apt-get update \
	&& apt-get install -y --no-install-recommends ca-certificates curl xz-utils git \
	&& rm -rf /var/lib/apt/lists/*

ARG ZIG_VERSION=0.17.0
ARG ZIG_SHA256=1cbe9df9f27e6b78d14ccbca43b6703a404ef79ef1c463de901d7f088d4e2026

RUN curl -fsSL -o /tmp/zig.tar.xz \
		"https://ziglang.org/download/${ZIG_VERSION}/zig-x86_64-linux-${ZIG_VERSION}.tar.xz" \
	&& echo "${ZIG_SHA256}  /tmp/zig.tar.xz" | sha256sum -c - \
	&& tar -C /opt -xJf /tmp/zig.tar.xz \
	&& ln -s "/opt/zig-x86_64-linux-${ZIG_VERSION}/zig" /usr/local/bin/zig \
	&& rm /tmp/zig.tar.xz

# Pinned libghostty-vt release. The build fetches the tag and checks that it
# still points at GHOSTTY_REV. generic keeps simd128 off so the translator does
# not require GOEXPERIMENT=simd.
ARG GHOSTTY_TAG=vt-1.0.1
ARG GHOSTTY_REV=108cacb17ed8990840cd14abfce43d8ed8503ca1

# Ghostty turns Kitty graphics off for freestanding targets. The patch turns
# it back on, and refuses the file, temporary-file and shared-memory media,
# which need a filesystem. Direct payloads work as on any other target.
COPY kitty-freestanding.patch /kitty-freestanding.patch

# libghostty-vt keeps Glyph Protocol registrations but has no C API to read
# them. The patch keeps each entry's register command and exports the
# glossary, with a generation that changes whenever it does.
COPY glyph-glossary.patch /glyph-glossary.patch

# The C API reads a cell's hyperlink URI, but not its OSC 8 id. The patch
# exports it.
COPY hyperlink-id.patch /hyperlink-id.patch

# Ghostty treats SGR 6 as SGR 5. The patch keeps which one a style asked for,
# with SGR 6 also setting blink, so everything that reads blink stays as it was.
COPY rapid-blink.patch /rapid-blink.patch

RUN mkdir -p /src \
	&& cd /src \
	&& git init -q \
	&& git remote add origin https://github.com/ghostty-org/ghostty.git \
	&& git fetch --depth 1 origin tag "${GHOSTTY_TAG}" \
	&& test "$(git rev-parse "${GHOSTTY_TAG}^{commit}")" = "${GHOSTTY_REV}" \
	&& git checkout --detach --quiet "${GHOSTTY_REV}" \
	&& git apply /kitty-freestanding.patch \
	&& git apply /glyph-glossary.patch \
	&& git apply /hyperlink-id.patch \
	&& git apply /rapid-blink.patch \
	&& zig build -Demit-lib-vt -Dtarget=wasm32-freestanding -Dcpu=generic -Doptimize=ReleaseSmall \
	&& mv zig-out/bin/ghostty-vt.wasm /ghostty-vt.wasm \
	&& rm -rf /src

CMD ["cat", "/ghostty-vt.wasm"]
