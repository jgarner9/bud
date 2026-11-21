#!/bin/sh
set -eu

# Reproduction only. The deployment package should verify its final artifact hash.
work=$1
mkdir -p "$work"
cd "$work"

fetch() {
    url=$1
    name=$2
    expected=$3
    if [ ! -f "$name" ]; then
        curl --fail --silent --show-error --location --output "$name.part" "$url"
        mv "$name.part" "$name"
    fi
    actual=$(git hash-object "$name")
    if [ "$actual" != "$expected" ]; then
        printf 'Unexpected blob for %s. Expected %s, got %s\n' "$name" "$expected" "$actual" >&2
        return 1
    fi
}

vec=https://raw.githubusercontent.com/asg017/sqlite-vec/v0.1.6
sqlite=https://raw.githubusercontent.com/sqlite/sqlite/version-3.46.1/src
fetch "$vec/sqlite-vec.c" sqlite-vec.c d8490cd02954dc529d8d282c9650ad8973b56f4f
fetch "$vec/sqlite-vec.h.tmpl" sqlite-vec.h.tmpl f49f62f6552b45ac612d236af96979aaba5bac8c
fetch "$sqlite/sqlite3ext.h" sqlite3ext.h ae0949baf75ae83bb4406ccef85f8d05d2542c07
fetch "$sqlite/sqlite.h.in" sqlite.h.in 549b52a1536e1035792a0b9d94097d9989fa90d3

python3 - <<'PY'
from pathlib import Path

vec = Path("sqlite-vec.h.tmpl").read_text()
for key, value in {
    "VERSION": "0.1.6",
    "VERSION_MAJOR": "0",
    "VERSION_MINOR": "1",
    "VERSION_PATCH": "6",
    "DATE": "jga-367-spike",
    "SOURCE": "d8490cd02954dc529d8d282c9650ad8973b56f4f",
}.items():
    vec = vec.replace("${" + key + "}", value)
Path("sqlite-vec.h").write_text(vec)

sqlite = Path("sqlite.h.in").read_text()
for key, value in {
    "VERS": "3.46.1",
    "VERSION-NUMBER": "3046001",
    "SOURCE-ID": "JGA-367 spike",
}.items():
    sqlite = sqlite.replace("--" + key + "--", value)
Path("sqlite3.h").write_text(sqlite)
PY

cc -O2 -DSQLITE_VEC_OMIT_FS -fPIC -shared -o vec0.so sqlite-vec.c -lm
sha256sum vec0.so
