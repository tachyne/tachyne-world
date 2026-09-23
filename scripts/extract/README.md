# scripts/extract — facts from the running game

Several generators under `scripts/` read `~/vanilla/extract/<version>.json`
instead of a third-party dataset. This directory produces it: `Extract.java`
bootstraps the server's registries and asks each block state and item what
it is — light emission and filtering, collision, solidity, the full-cube
test, hardness, resistance, loot table, block entity, stack size,
durability and food.

It exists because the dataset we used before records one value per *block*
for facts vanilla computes per *state*, and stops at 26.1. Building tables
from it put lit furnaces in the dark, let light through double slabs, kept
open fence gates solid and got 2,809 states wrong on the farmland lid test.

## Running it

    JAVA=/path/to/jdk25/bin/java JAVAC=/path/to/jdk25/bin/javac \
        scripts/extract/extract.sh 26.3

Needs `server-<version>.jar` (the official bundler download) in `$VANILLA`,
default `~/vanilla`. 26.x servers ship unobfuscated. Earlier versions need
remapping first: set `REMAP_MAPPINGS` to Mojang's published server mappings
for that version (TSRG) and `SPECIALSOURCE` to a SpecialSource jar.

26.3 class files need JDK 25.

## Trusting it

The extractor was checked field for field against the previous dataset at
two versions that dataset covers — 1.21.11 and 26.1 — with zero mismatches
across every block, before any generator was pointed at it. When extending
it, repeat that: add the field, compare against an independent source where
one exists, and regenerate the existing tables to confirm nothing moved that
was not meant to.

## Item components on 26.x

26.x keeps an item's stack size, durability and food in data components that
the server binds while loading resources, which a bare registry bootstrap never
does. Extract.java binds them itself, against the data generator's registry
lookup, before reading any item. Checked at 26.1 against the old dataset:
stack size and durability exact for all 1,506 items, food points exact for all
44 foods — and saturation equal to nutrition × modifier × 2 for every one,
which is also exactly what the engine ships at 1.21.11.
