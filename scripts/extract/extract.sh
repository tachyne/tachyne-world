#!/bin/bash
# extract.sh <version> — write $EXTRACT_OUT/<version>.json by running the game.
#
# Unpacks the server bundler jar (the real server plus its bundled libraries),
# compiles Extract.java against it and runs it. The generators under scripts/
# read the result.
#
# 26.x servers ship unobfuscated and need nothing else. Earlier versions are
# obfuscated: point REMAP_MAPPINGS at Mojang's published server mappings in
# TSRG form and SPECIALSOURCE at a SpecialSource jar, and the server is
# remapped before compiling against it.
#
#   VANILLA        dir holding server-<version>.jar      (default ~/vanilla)
#   EXTRACT_OUT    where <version>.json is written       (default ~/vanilla/extract)
#   EXTRACT_WORK   scratch space, reused between runs    (default ~/.cache/tachyne-extract)
#   JAVA, JAVAC    a JDK new enough for the version — 26.3 class files need 25
set -euo pipefail
VER="${1:?usage: extract.sh <version>}"
VANILLA="${VANILLA:-$HOME/vanilla}"
OUT="${EXTRACT_OUT:-$HOME/vanilla/extract}"
WORK="${EXTRACT_WORK:-$HOME/.cache/tachyne-extract}/$VER"
JAVA="${JAVA:-java}"; JAVAC="${JAVAC:-javac}"
HERE="$(cd "$(dirname "$0")" && pwd)"

mkdir -p "$WORK/libs" "$WORK/classes" "$OUT"
python3 - "$VANILLA/server-$VER.jar" "$WORK" <<'PY'
import sys, zipfile, os
jar, work = sys.argv[1], sys.argv[2]
z = zipfile.ZipFile(jar)
inner = [n for n in z.namelist() if n.startswith("META-INF/versions/") and n.endswith(".jar")]
if not inner:
    sys.exit("%s is not a bundler jar" % jar)
open(os.path.join(work, "server.jar"), "wb").write(z.read(inner[0]))
for n in z.namelist():
    if n.startswith("META-INF/libraries/") and n.endswith(".jar"):
        open(os.path.join(work, "libs", os.path.basename(n)), "wb").write(z.read(n))
PY

SERVER="$WORK/server.jar"
if ! python3 -c "import sys,zipfile; sys.exit(0 if 'net/minecraft/world/level/block/Blocks.class' in zipfile.ZipFile(sys.argv[1]).namelist() else 1)" "$SERVER"; then
    : "${REMAP_MAPPINGS:?$VER is obfuscated: set REMAP_MAPPINGS to its server mappings (TSRG)}"
    : "${SPECIALSOURCE:?$VER is obfuscated: set SPECIALSOURCE to a SpecialSource jar}"
    if [ ! -s "$WORK/server-remapped.jar" ]; then
        "$JAVA" -jar "$SPECIALSOURCE" --in-jar "$SERVER" --out-jar "$WORK/server-remapped.jar" \
            --srg-in "$REMAP_MAPPINGS" --kill-lvt >/dev/null
    fi
    SERVER="$WORK/server-remapped.jar"
fi

CP="$SERVER:$(ls "$WORK"/libs/*.jar | tr '\n' ':')"
"$JAVAC" -nowarn -Xlint:-deprecation -cp "$CP" -d "$WORK/classes" "$HERE/Extract.java"
# Run from the scratch dir: the server bootstrap writes a logs/ directory into
# the working directory, and those logs carry local paths.
(cd "$WORK" && "$JAVA" -Xmx900m -cp "$WORK/classes:$CP" Extract "$OUT/$VER.json" 2>&1 | grep -E "blocks=|Exception|Error" || true)
