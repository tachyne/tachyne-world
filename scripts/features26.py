"""Normalise a 26.x tree feature into the 1.21.x configured_feature shape.

26.x rewrote the worldgen feature format: features live in worldgen/feature/
(not configured_feature/), the "config" wrapper is gone, block states are
{"id", "properties"} or a bare name, state providers dropped their
"*_state_provider" type names (and simple_state_provider is just the state),
some providers are references into the new worldgen/block_state_provider
registry, fields at their codec default are omitted, and the trunk's
dirt_provider + force_dirt became one below_trunk_provider. The tree
generator reads the 1.21.x shape; this maps one onto the other, so the same
parser reads both. A shape it does not know stops it rather than guessing.

Stdlib only.
"""
import json

SIZE_DEFAULTS = {
    "minecraft:two_layers_feature_size": {"limit": 1, "lower_size": 0, "upper_size": 1},
    "minecraft:three_layers_feature_size": {"limit": 1, "upper_limit": 1, "lower_size": 0,
                                            "middle_size": 1, "upper_size": 1},
}


# Decorator fields 26.x omits at their codec default.
DECORATOR_DEFAULTS = {
    "minecraft:place_on_ground": {"tries": 128, "radius": 2, "height": 1},
}

# Single-rule providers whose condition the 1.21.x decorator code applies
# itself (setDirtAt's and AlterGroundDecorator's "is it dirt" tests): the
# 1.21.x shape is just the rule's result.
IMPLIED_CONDITIONS = {
    json.dumps({"type": "minecraft:matching_block_tag", "tag": "minecraft:beneath_tree_podzol_replaceable"}),
}


class Reader:
    def __init__(self, zipfile, blocks_report):
        """blocks_report: the 26.x blocks.json (a bare block name means its
        default state, which 1.21.x wrote out property by property)."""
        self.z = zipfile
        self.defaults = {}
        for name, b in blocks_report.items():
            for st in b["states"]:
                if st.get("default") and st.get("properties"):
                    self.defaults[name] = st["properties"]

    def data(self, kind, key):
        ns, _, path = key.partition(":")
        return json.loads(self.z.read(f"data/{ns}/worldgen/{kind}/{path}.json"))

    def feature(self, name):
        return json.loads(self.z.read(f"data/minecraft/worldgen/feature/{name}.json"))

    def state(self, s):
        """A 26.x block state -> the 1.21.x {"Name", "Properties"} form."""
        if isinstance(s, str):
            out = {"Name": s}
            if s in self.defaults:
                out["Properties"] = dict(self.defaults[s])
            return out
        out = {"Name": s["id"]}
        if s.get("properties"):
            out["Properties"] = s["properties"]
        return out

    def provider(self, p):
        """A 26.x block state provider -> the 1.21.x typed provider."""
        if isinstance(p, str):
            ns, _, path = p.partition(":")
            try:
                return self.provider(self.data("block_state_provider", p))
            except KeyError:
                return {"type": "minecraft:simple_state_provider", "state": self.state(p)}
        t = p.get("type")
        if t is None:  # a bare state
            return {"type": "minecraft:simple_state_provider", "state": self.state(p)}
        if t == "minecraft:weighted":
            return {"type": "minecraft:weighted_state_provider",
                    "entries": [{"data": self.state(e["data"]), "weight": e["weight"]} for e in p["entries"]]}
        if t == "minecraft:rule_based" and len(p["rules"]) == 1 and \
                json.dumps(p["rules"][0]["if_true"]) in IMPLIED_CONDITIONS:
            return self.provider(p["rules"][0]["then"])
        if t == "minecraft:randomized_int":
            return {"type": "minecraft:randomized_int_state_provider", "property": p["property"],
                    "source": self.provider(p["source"]), "values": p["values"]}
        raise SystemExit(f"features26: unhandled state provider {p}")

    def below_trunk(self, p):
        """below_trunk_provider -> (dirt_provider, force_dirt). The rule
        "dirt unless the ground is #cannot_replace_below_tree_trunk" is 1.21.x's
        dirt + force_dirt false (setDirtAt leaves #dirt ground alone); a plain
        state replaces whatever is there, which is force_dirt."""
        if p == "minecraft:soil_beneath_tree":
            return {"type": "minecraft:simple_state_provider", "state": {"Name": "minecraft:dirt"}}, False
        if isinstance(p, dict) and "id" in p:
            return self.provider(p), True
        raise SystemExit(f"features26: unhandled below_trunk_provider {p}")

    def decorator(self, d):
        d = dict(d)
        for k, v in DECORATOR_DEFAULTS.get(d["type"], {}).items():
            d.setdefault(k, v)
        for key in ("block_provider", "provider"):
            if key in d:
                d[key] = self.provider(d[key])
        if "block_state_provider" in d:
            d["block_state_provider"] = self.provider(d["block_state_provider"])
        return d

    def tree(self, f):
        """A 26.x tree feature -> the 1.21.x {"type", "config"} object."""
        if f.get("type") != "minecraft:tree":
            raise SystemExit(f"features26: not a tree: {f.get('type')}")
        c = {k: v for k, v in f.items() if k not in ("type", "below_trunk_provider")}
        c["trunk_provider"] = self.provider(f["trunk_provider"])
        c["foliage_provider"] = self.provider(f["foliage_provider"])
        c["dirt_provider"], c["force_dirt"] = self.below_trunk(f["below_trunk_provider"])
        ms = dict(f["minimum_size"])
        for k, v in SIZE_DEFAULTS.get(ms["type"], {}).items():
            ms.setdefault(k, v)
        c["minimum_size"] = ms
        c["decorators"] = [self.decorator(d) for d in f.get("decorators", [])]
        rp = c.get("root_placer")
        if rp is not None:
            rp = json.loads(json.dumps(rp))
            rp["root_provider"] = self.provider(rp["root_provider"])
            arp = rp["above_root_placement"]
            arp["above_root_provider"] = self.provider(arp["above_root_provider"])
            mrp = rp["mangrove_root_placement"]
            mrp["muddy_roots_provider"] = self.provider(mrp["muddy_roots_provider"])
            c["root_placer"] = rp
        return {"type": "minecraft:tree", "config": c}

    def fallen(self, f):
        """A 26.x fallen_tree feature -> the 1.21.x {"type", "config"} object."""
        if f.get("type") != "minecraft:fallen_tree":
            raise SystemExit(f"features26: not a fallen tree: {f.get('type')}")
        c = {k: v for k, v in f.items() if k != "type"}
        c["trunk_provider"] = self.provider(f["trunk_provider"])
        c["log_decorators"] = [self.decorator(d) for d in f.get("log_decorators", [])]
        c["stump_decorators"] = [self.decorator(d) for d in f.get("stump_decorators", [])]
        return {"type": "minecraft:fallen_tree", "config": c}

    def any(self, name):
        """A tree or fallen-tree feature by name, normalised; None for any
        other feature type."""
        f = self.feature(name)
        t = f.get("type")
        if t == "minecraft:tree":
            return self.tree(f)
        if t == "minecraft:fallen_tree":
            return self.fallen(f)
        return None
