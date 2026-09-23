"""The engine's canonical content version, for every generator that reads it.

The engine's content — blocks and their states, items, entity types,
recipes, loot, advancements, structures — is one Minecraft version's, and
every id it persists or emits is that version's. Each content generator
reads its facts for that version from here, so moving the engine to a new
version is one setting, not forty scripts each with the old number written
in:

    TACHYNE_CANON=26.3 python3 scripts/gen_items.py

What is NOT the canonical content version, and must not read this: the wire
layout (the 770 renderer and its protomaps, the registries and tags a
client is sent), and particle ids, which the engine emits in 770's
numbering (gen_particleremap.py).

Stdlib only.
"""
import io
import os
import zipfile

VERSION = os.environ.get("TACHYNE_CANON", "1.21.11")

# The protocol whose clients number things exactly as the canonical content
# does — the translation tables' identity.
PROTOCOLS = {"1.21.5": 770, "1.21.11": 774, "26.1": 775, "26.2": 776, "26.3": 777}
if VERSION not in PROTOCOLS:
    raise SystemExit(f"canon.py: no protocol known for {VERSION}; add it to PROTOCOLS")
PROTOCOL = PROTOCOLS[VERSION]


def jar(ver=None):
    """The vanilla server jar (the bundler; see inner_jar)."""
    return os.path.expanduser(f"~/vanilla/server-{ver or VERSION}.jar")


def inner_jar(ver=None):
    """The server jar's own classes and data, out of the bundler, as a ZipFile."""
    outer = zipfile.ZipFile(jar(ver))
    inner = [n for n in outer.namelist() if n.startswith("META-INF/versions/") and n.endswith(".jar")]
    if not inner:
        return outer  # an unbundled jar
    return zipfile.ZipFile(io.BytesIO(outer.read(inner[0])))


def report(name, ver=None):
    """A file of the server's --reports output (blocks.json, registries.json)."""
    root = os.environ.get("VANILLA_REPORTS", os.path.expanduser("~/vanilla/reports"))
    return os.path.join(root, ver or VERSION, name)


def extract(ver=None):
    """The runtime extractor's output (scripts/extract)."""
    return os.path.expanduser(f"~/vanilla/extract/{ver or VERSION}.json")


def src(ver=None):
    """The server's source tree, for the facts only its code holds."""
    return os.path.expanduser(f"~/DecompilerMC/src/{ver or VERSION}/server")
