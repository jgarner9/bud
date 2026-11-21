#!/bin/sh
# Build the home server's embedding server (JGA-294, Suite D52): llama.cpp's
# llama-server at a pinned tag, CPU only, plus the pinned Qwen3-Embedding
# GGUF, each checked before it's used. Usage: build-embed-server.sh <dir>.
# It leaves <dir>/llama-server and <dir>/qwen3-embedding-0.6b-q8_0.gguf, and
# prints both SHA-256s for the unit's env file. Needs git, cmake, a C++
# compiler and curl. Re-running reuses a download whose digest matches.
set -eu

# llama.cpp release tag b12200 (2026-11-07) and the commit it must point at
llama_tag=b12200
llama_commit=c19043f67e52f1f1e00d797246e19643bb2f168e
# Qwen/Qwen3-Embedding-0.6B-GGUF at a pinned revision, and the file's SHA-256
model_rev=370f27d7550e0def9b39c1f16d3fbaa13aa67728
model_file=Qwen3-Embedding-0.6B-Q8_0.gguf
model_sha256=06507c7b42688469c4e7298b0a1e16deff06caf291cf0a5b278c308249c3e439
model_out=qwen3-embedding-0.6b-q8_0.gguf

if [ $# -ne 1 ]; then
	echo "usage: build-embed-server.sh <dir>" >&2
	exit 2
fi
for tool in git cmake curl; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		echo "build-embed-server: $tool isn't on PATH; install it and run this again" >&2
		exit 1
	fi
done

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d' ' -f1
	else
		shasum -a 256 "$1" | cut -d' ' -f1
	fi
}

mkdir -p "$1"
out=$(cd "$1" && pwd)
src=$out/llama.cpp-$llama_tag

if [ ! -d "$src/.git" ]; then
	git init -q "$src"
fi
git -C "$src" fetch -q --depth 1 https://github.com/ggml-org/llama.cpp "refs/tags/$llama_tag:refs/tags/$llama_tag"
got=$(git -C "$src" rev-parse "refs/tags/$llama_tag^{commit}")
if [ "$got" != "$llama_commit" ]; then
	echo "build-embed-server: llama.cpp tag $llama_tag points at $got, not the pinned $llama_commit; nothing built" >&2
	exit 1
fi
git -C "$src" -c advice.detachedHead=false checkout -q --force "$llama_commit"

# CPU only and static, with no web UI (it would download prebuilt assets)
# and no curl (the server never fetches models itself)
cmake -S "$src" -B "$src/build" -DCMAKE_BUILD_TYPE=Release \
	-DBUILD_SHARED_LIBS=OFF -DGGML_NATIVE=ON -DLLAMA_CURL=OFF \
	-DLLAMA_BUILD_TESTS=OFF -DLLAMA_BUILD_EXAMPLES=OFF \
	-DLLAMA_BUILD_UI=OFF -DLLAMA_USE_PREBUILT_UI=OFF >"$src/cmake.log"
cmake --build "$src/build" --target llama-server -j >"$src/build.log"
cp "$src/build/bin/llama-server" "$out/llama-server.part"
mv "$out/llama-server.part" "$out/llama-server"

if [ ! -f "$out/$model_out" ] || [ "$(sha256 "$out/$model_out")" != "$model_sha256" ]; then
	curl --fail --silent --show-error --location --output "$out/$model_out.part" \
		"https://huggingface.co/Qwen/Qwen3-Embedding-0.6B-GGUF/resolve/$model_rev/$model_file"
	got=$(sha256 "$out/$model_out.part")
	if [ "$got" != "$model_sha256" ]; then
		rm -f "$out/$model_out.part"
		echo "build-embed-server: $model_file has SHA-256 $got, not the pinned $model_sha256; download removed" >&2
		exit 1
	fi
	mv "$out/$model_out.part" "$out/$model_out"
fi

echo "BUD_EMBED_SERVER_SHA256=$(sha256 "$out/llama-server")"
echo "BUD_EMBED_MODEL_SHA256=$model_sha256"
