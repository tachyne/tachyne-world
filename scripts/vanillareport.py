"""Vanilla's own data reports, in the shape the generators were written for.

Several generators were built on minecraft-data, which stops at 26.1. The
server's data generator writes the same registry and block-state facts for any
version (run the server jar with --reports), so this reads those and presents
them the way the generators already expect — each keeps its logic and only
changes where it loads from.

Reports live in ~/vanilla/reports/<version>/ unless $VANILLA_REPORTS says
otherwise.
"""
import json
import os

ROOT = os.environ.get("VANILLA_REPORTS", os.path.expanduser("~/vanilla/reports"))


def _load(ver, name):
    with open(os.path.join(ROOT, ver, name)) as f:
        return json.load(f)


def registry(ver, key):
    """[{name, id}] for one registry, in protocol-id order. key like "item"."""
    entries = _load(ver, "registries.json")["minecraft:" + key]["entries"]
    return sorted(({"name": k.removeprefix("minecraft:"), "id": v["protocol_id"]}
                   for k, v in entries.items()), key=lambda e: e["id"])


def _prop_type(values):
    if values == ["true", "false"]:
        return "bool"
    if all(v.isdigit() for v in values):
        return "int"
    return "enum"


def _layout_order(states, props):
    """Most-significant-first property order, read off the ids: walking from
    the first state, the property that changes one step later is the fastest
    varying, the one that changes one full cycle of it later is next, and so
    on."""
    if not props:
        return []
    by_id = sorted(states, key=lambda s: s["id"])
    first = by_id[0]["properties"]
    order, stride = [], 1
    while len(order) < len(props):
        nxt = by_id[stride]["properties"]
        changed = [p for p in props if p not in order and nxt[p] != first[p]]
        if len(changed) != 1:
            raise ValueError("cannot read property layout off the state ids")
        order.append(changed[0])
        stride *= len(props[changed[0]])
    order.reverse()
    return order


def blocks(ver):
    """Blocks in state-id order, each {name, id, minStateId, maxStateId,
    defaultState, states:[{name, type, num_values, values}]}.

    Property order is the mixed-radix order state ids are laid out in — the
    thing SetProperty arithmetic depends on. It is DERIVED from the state ids,
    not read from the report's property listing: that listing follows
    declaration order, which differs from the layout for chests, copper chests
    and piston heads (12 blocks in 1.21.11), and taking it at face value would
    compute the wrong state for every one of them.
    """
    report = _load(ver, "blocks.json")
    ids = {e["name"]: e["id"] for e in registry(ver, "block")}
    out = []
    for key, b in report.items():
        name = key.removeprefix("minecraft:")
        sids = [s["id"] for s in b["states"]]
        default = next((s["id"] for s in b["states"] if s.get("default")), sids[0])
        props = b.get("properties", {})
        states = []
        for pname in _layout_order(b["states"], props):
            vals = props[pname]
            states.append({"name": pname, "type": _prop_type(vals),
                           "num_values": len(vals), "values": vals})
        out.append({"name": name, "id": ids[name], "minStateId": min(sids),
                    "maxStateId": max(sids), "defaultState": default, "states": states})
    out.sort(key=lambda b: b["minStateId"])
    return out
