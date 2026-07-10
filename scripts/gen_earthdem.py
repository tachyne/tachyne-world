#!/usr/bin/env python3
"""gen_earthdem.py — build an embedded Earth DEM grid for tachyne's EarthGenerator.

Crops real elevation data (Copernicus GLO-30, 30 m global DSM — free, open,
ESA) to a named region and writes a compact binary grid the Go engine embeds:

    OUT: internal/worldgen/earthdata/<name>.bin.gz
    format (gzipped): "TDEM1\n" | u32le json-len | json header | int16le grid
    grid: metres above sea level, row-major, rows north->south, cols west->east

Needs: pip install tifffile numpy imagecodecs; network (or pre-downloaded
tiles in --cache). Run OUTSIDE the sandbox. Tiles are fetched from the public
AWS bucket (no auth):
  https://copernicus-dem-30m.s3.amazonaws.com/Copernicus_DSM_COG_10_<T>_DEM/...

Region: the Cape Peninsula (Cape Town) — lon [18.25, 18.55], lat [-34.40,
-33.80], block-space origin (ref) at the city centre so spawnable landmarks
have small coordinates. Add regions by adding entries to REGIONS.
"""

import gzip
import io
import json
import os
import struct
import sys
import urllib.request

import numpy as np
import tifffile

OUT_DIR = os.path.join(os.path.dirname(__file__), "..", "internal", "worldgen", "earthdata")

REGIONS = {
    # Greater Cape Town: Bloubergstrand + Robben Island to Cape Point, and east
    # across the Cape Flats to Stellenbosch/Somerset West (the Hottentots-
    # Holland main ridge sits just past 19°E — its shoulder makes the horizon).
    # v1 was the bare peninsula (lon<=18.55); a real phone flight found the
    # east edge 12 km from the city, so the metro moved in.
    "capetown": {
        "lon_w": 18.25, "lon_e": 19.00,   # west/east bounds (deg)
        "lat_n": -33.55, "lat_s": -34.40,  # north/south bounds (deg)
        "ref_lat": -33.92, "ref_lon": 18.42,  # block (0,0) — Cape Town city centre
        "tiles": ["S34_00_E018_00", "S35_00_E018_00"],
    },
}

PX = 1.0 / 3600.0  # GLO-30 pixel size in degrees (both axes at this latitude)


def tile_path(cache: str, t: str) -> str:
    name = f"Copernicus_DSM_COG_10_{t}_DEM.tif"
    p = os.path.join(cache, name)
    if not os.path.exists(p):
        url = f"https://copernicus-dem-30m.s3.amazonaws.com/Copernicus_DSM_COG_10_{t}_DEM/{name}"
        print(f"fetch {url}")
        urllib.request.urlretrieve(url, p)
    return p


def tile_origin(t: str) -> tuple[float, float]:
    """North-west corner (lat, lon) of a 1x1 degree tile from its name."""
    # S34_00_E018_00 covers lat [-34,-33], lon [18,19]: raster origin (-33, 18).
    lat = int(t[1:3])
    if t[0] == "S":
        lat = -lat
    lon = int(t[8:11]) * (1 if t[7] == "E" else -1)
    return (lat + 1 if t[0] == "S" else lat, lon)  # north edge


def build(name: str, cache: str) -> None:
    r = REGIONS[name]
    cols = round((r["lon_e"] - r["lon_w"]) / PX)
    rows = round((r["lat_n"] - r["lat_s"]) / PX)
    grid = np.full((rows, cols), -32768, dtype=np.int16)  # -32768 = nodata

    for t in r["tiles"]:
        top_lat, west_lon = tile_origin(t)
        data = tifffile.imread(tile_path(cache, t))  # (3600, 3600) float32 metres
        th, tw = data.shape
        for row in range(rows):
            lat = r["lat_n"] - (row + 0.5) * PX
            trow = int((top_lat - lat) / PX)
            if trow < 0 or trow >= th:
                continue
            c0 = int(round((r["lon_w"] - west_lon) / PX))
            if c0 < 0 or c0 + cols > tw:
                raise SystemExit(f"{t}: lon range leaves the tile (c0={c0})")
            vals = data[trow, c0:c0 + cols]
            grid[row, :] = np.round(vals).astype(np.int16)

    if (grid == -32768).any():
        raise SystemExit(f"{name}: {int((grid == -32768).sum())} nodata cells — tile coverage gap")

    header = {
        "name": name, "lat_n": r["lat_n"], "lon_w": r["lon_w"], "d": PX,
        "w": cols, "h": rows, "ref_lat": r["ref_lat"], "ref_lon": r["ref_lon"],
    }
    hj = json.dumps(header).encode()
    buf = io.BytesIO()
    buf.write(b"TDEM1\n")
    buf.write(struct.pack("<I", len(hj)))
    buf.write(hj)
    buf.write(grid.tobytes())  # int16 little-endian on every platform we build on

    os.makedirs(OUT_DIR, exist_ok=True)
    out = os.path.join(OUT_DIR, f"{name}.bin.gz")
    with gzip.open(out, "wb", compresslevel=9) as f:
        f.write(buf.getvalue())
    print(f"{out}: {cols}x{rows} samples, {os.path.getsize(out)/1e6:.1f} MB gzipped, "
          f"elev [{grid.min()}, {grid.max()}] m")


if __name__ == "__main__":
    cache = sys.argv[2] if len(sys.argv) > 2 else "."
    build(sys.argv[1] if len(sys.argv) > 1 else "capetown", cache)
