"""Read 26.x loot tables as the 1.21.x shape the loot bakers were written for.

26.x rewrote the loot format: a pool's or entry's `conditions` list became one
`condition`; a condition's `condition` key and a function's `function` key
became `type`; `functions` became `modifier`; `block_state_property` became
`match_block` (`blocks`, `state`); common conditions became named predicate
files (`"condition": "minecraft:tool/can_silk_touch"`); and defaults (a pool's
`bonus_rolls`, set_count's `add`) are left out. normalize() undoes all of that,
so one baker reads both versions; gen_blockloot's oracle is that every table
the two versions share comes out equal.
"""
import json

_predicates = {}


def _predicate(jar, ref):
    if ref not in _predicates:
        ns, path = ref.split(":", 1) if ":" in ref else ("minecraft", ref)
        _predicates[ref] = json.loads(jar.read("data/%s/predicate/%s.json" % (ns, path)))
    return _predicates[ref]


def condition(jar, c):
    if isinstance(c, str):
        c = _predicate(jar, c)
    if isinstance(c, list):
        return [condition(jar, x) for x in c]
    out = {}
    for k, v in c.items():
        if k == "type":
            out["condition"] = v
        elif k == "terms":
            out["terms"] = [condition(jar, t) for t in v]
        elif k == "term":
            out["term"] = condition(jar, v)
        else:
            out[k] = v
    if out.get("condition") in ("minecraft:entity_properties", "minecraft:damage_source_properties") and "predicate" in out:
        out["predicate"] = entity_predicate(out["predicate"])
    if out.get("condition") == "minecraft:match_block":
        out["condition"] = "minecraft:block_state_property"
        if "blocks" in out:
            out["block"] = out.pop("blocks")
        if "state" in out:
            out["properties"] = out.pop("state")
    return out


# Maps keyed by registry ids, whose keys are data rather than field names.
_ID_MAPS = {"effects", "enchantments", "stored_enchantments", "statistics", "recipes",
            "predicates"}  # item sub-predicates are keyed by component type


def entity_predicate(p, key=None):
    """26.x entity predicates namespace their fields (minecraft:location) and
    call the entity's type entity_type; 1.21.x did neither."""
    if isinstance(p, list):
        return [entity_predicate(x, key) for x in p]
    if not isinstance(p, dict):
        return p
    out = {}
    for k, v in p.items():
        nk = k[len("minecraft:"):] if k.startswith("minecraft:") and key not in _ID_MAPS else k
        if nk == "entity_type":
            nk = "type"
        if nk.startswith("type_specific/"):  # type_specific/sheep: {…} -> type_specific: {type: sheep, …}
            kind = nk.split("/", 1)[1]
            kind = {"cube_mob": "slime"}.get(kind, kind)  # 1.21.x called the slime/magma sub-predicate slime
            v = dict({"type": "minecraft:" + kind}, **entity_predicate(v, nk))
            out["type_specific"] = v
            continue
        if nk == "components" and isinstance(v, dict):  # component keys may omit the namespace
            out[nk] = {(ck if ":" in ck else "minecraft:" + ck): cv for ck, cv in v.items()}
            continue
        if nk == "id" and key == "tags" and isinstance(v, str) and v.startswith("#"):
            v = v[1:]  # damage tags: 26.x writes the tag with its #
        out[nk] = entity_predicate(v, nk)
    return out


def conditions(jar, c):
    """A 26.x `condition` (one, or a list) as a 1.21.x `conditions` list."""
    if isinstance(c, list):
        return [condition(jar, x) for x in c]
    one = condition(jar, c)
    if one.get("condition") == "minecraft:all_of":
        return one["terms"]  # a list of conditions is all_of
    return [one]


def function(jar, f):
    out = {}
    for k, v in f.items():
        if k == "type":
            out["function"] = v
        elif k == "condition":
            out["conditions"] = conditions(jar, v)
        else:
            out[k] = _walk(jar, v)
    if out.get("function") in ("minecraft:set_count", "minecraft:set_damage", "minecraft:set_enchantments"):
        out.setdefault("add", False)
    if out.get("function") == "minecraft:set_components" and isinstance(out.get("components"), dict):
        out["components"] = {(k if ":" in k else "minecraft:" + k): v for k, v in out["components"].items()}
    return out


def _walk(jar, x):
    if isinstance(x, list):
        return [_walk(jar, v) for v in x]
    if not isinstance(x, dict):
        return x
    out = {}
    if x.get("type") == "minecraft:tag" and isinstance(x.get("items"), str) and "name" not in x:
        x = dict(x)
        x["name"] = x.pop("items").lstrip("#")  # a tag entry named its tag without the #
    for k, v in x.items():
        if k == "condition":
            out["conditions"] = conditions(jar, v)
        elif k == "modifier":
            out["functions"] = [function(jar, f) for f in (v if isinstance(v, list) else [v])]
        elif k == "pools" and isinstance(v, list):  # top-level or an inline table's
            out[k] = [dict({"bonus_rolls": 0.0}, **_walk(jar, pool)) if isinstance(pool, dict) else pool for pool in v]
        else:
            out[k] = _walk(jar, v)
    return out


def normalize(jar, table):
    return _walk(jar, table)


def is26(version):
    """Whether a version writes loot in the 26.x format."""
    return int(version.split(".")[0]) >= 26


def read(jar, path, version):
    """A loot table from the jar, in the 1.21.x shape whichever version wrote it."""
    t = json.loads(jar.read(path))
    return normalize(jar, t) if is26(version) else t
