#!/usr/bin/env python3
"""Regenerate internal/server/advancements_gen.go — the vanilla advancement
tree (canonical 1.21.11), distilled for the engine's criteria tracker.

Sources (all local, no network):
  - data/minecraft/advancement/**.json   from the vanilla 1.21.11 server jar
    (recipe-unlock advancements under advancement/recipes/ are excluded —
    they belong to the recipe book)
  - data/minecraft/tags/item/*.json      same jar (predicate tag expansion)
  - assets/minecraft/lang/en_us.json     same jar (English titles for chat
    announce + Bedrock fallback; Java clients render the translate keys)
  - internal/server/itemnames_gen.go     icon/predicate item ids (the engine's
    one item-id space, minecraft-data 1.21.11)

Tree layout (each display's x,y) is computed here the way the vanilla
server lays out its advancement screen (a Buchheim tidy tree over the
visible nodes, x = depth, y = tidy row) so the client's tree matches vanilla.

Run: python3 scripts/gen_advancements.py [path-to-server.jar]
"""
import io
import json
import os
import re
import sys
import zipfile

JAR = sys.argv[1] if len(sys.argv) > 1 else os.path.expanduser(
    "~/vanilla/server-1.21.11.jar")
OUT = "internal/server/advancements_gen.go"

DIMS = {"minecraft:overworld": 0, "minecraft:the_nether": 1, "minecraft:the_end": 2}
FRAMES = {"task": 0, "challenge": 1, "goal": 2}


def load_jar(path):
    z = zipfile.ZipFile(path)
    inner = [n for n in z.namelist()
             if n.startswith("META-INF/versions/") and n.endswith(".jar")]
    if inner:
        z = zipfile.ZipFile(io.BytesIO(z.read(inner[0])))
    return z


def load_item_ids():
    src = open("internal/server/itemnames_gen.go").read()
    return {m.group(1): int(m.group(2))
            for m in re.finditer(r'"([a-z0-9_]+)":\s+(\d+),', src)}


def strip_ns(name):
    return name.split(":", 1)[1] if ":" in name else name


class Tags:
    """Tag expansion for item, block and entity_type tags (recursive)."""
    def __init__(self, z):
        self.z, self.cache = z, {}

    def expand(self, tag, kind="item"):  # "#minecraft:beehives" -> block names
        tag = strip_ns(tag.lstrip("#"))
        key = (kind, tag)
        if key in self.cache:
            return self.cache[key]
        out = []
        try:
            d = json.loads(self.z.read(f"data/minecraft/tags/{kind}/{tag}.json"))
        except KeyError:
            self.cache[key] = out
            return out
        for v in d["values"]:
            v = v["id"] if isinstance(v, dict) else v
            if v.startswith("#"):
                out += self.expand(v, kind)
            else:
                out.append(strip_ns(v))
        self.cache[key] = out
        return out


def names_of(val, tags, kind):
    """A string / list of ids-or-tags -> concrete name list ('' -> [])."""
    if val is None:
        return []
    vals = val if isinstance(val, list) else [val]
    out = []
    for v in vals:
        if v.startswith("#"):
            out += tags.expand(v, kind)
        else:
            out.append(strip_ns(v))
    return out


def items_pred(pred, tags):
    """One ItemPredicate's `items` field -> concrete item-name list."""
    it = pred.get("items")
    if it is None:
        return None  # match-any predicate (counts/components only) — treat as any
    vals = it if isinstance(it, list) else [it]
    names = []
    for v in vals:
        if v.startswith("#"):
            names += tags.expand(v)
        else:
            names.append(strip_ns(v))
    return names


def entity_type(cond_list):
    """Contextual entity-predicate list -> plain type name ('' = any)."""
    for c in cond_list or []:
        t = (c.get("predicate") or {}).get("type")
        if t and not t.startswith("#"):
            return strip_ns(t)
    return ""


def ent_pred(cond_list):
    """Contextual entity-predicate list -> (type, baby, variant). type '' = any,
    baby: None/True/False."""
    for c in cond_list or []:
        p = c.get("predicate") or {}
        t = p.get("type")
        t = strip_ns(t) if t and not t.startswith("#") else ""
        baby = (p.get("flags") or {}).get("is_baby")
        var = ""
        ts = p.get("type_specific") or {}
        if isinstance(ts, dict) and "variant" in ts:
            var = strip_ns(str(ts["variant"]))
        return t, baby, var
    return "", None, ""


def loc_of(pred):
    """A location predicate -> dict(blocks, props, biome, structure, smokey)."""
    out = {}
    b = pred.get("block") or {}
    if b.get("blocks") is not None:
        out["blocks"] = b["blocks"]
    if b.get("state"):
        out["props"] = {k: str(v) for k, v in b["state"].items()}
    if isinstance(pred.get("biomes"), str) and not pred["biomes"].startswith("#"):
        out["biome"] = strip_ns(pred["biomes"])
    if isinstance(pred.get("structures"), str):
        out["structure"] = strip_ns(pred["structures"])
    if pred.get("smokey"):
        out["smokey"] = True
    return out


def loc_checks(terms, tags):
    """placed_block's location list: any_of(all_of(location_check...)) ->
    OR-groups of (offset, blocks, props). A bare location_check is one group;
    a block_state_property term checks the placed block itself."""
    def one(term):
        c = term.get("condition", "")
        if c == "minecraft:location_check":
            p = term.get("predicate") or {}
            l = loc_of(p)
            return [{"dx": term.get("offsetX", 0), "dy": term.get("offsetY", 0),
                     "dz": term.get("offsetZ", 0),
                     "blocks": names_of(l.get("blocks"), tags, "block"),
                     "props": l.get("props", {})}]
        if c == "minecraft:block_state_property":
            return [{"dx": 0, "dy": 0, "dz": 0,
                     "blocks": [strip_ns(term["block"])],
                     "props": {k: str(v) for k, v in (term.get("properties") or {}).items()}}]
        if c == "minecraft:all_of":
            out = []
            for t in term.get("terms", []):
                out += one(t)
            return out
        return []
    groups = []
    for term in terms or []:
        if term.get("condition") == "minecraft:any_of":
            for alt in term.get("terms", []):
                groups.append(one(alt))
        else:
            g = one(term)
            if g:
                groups.append(g)
    return groups


def rng_min(v, key=None):
    """A number-or-{min,max} range -> its min (0 if absent)."""
    if v is None:
        return 0
    if isinstance(v, dict):
        if key is not None:
            return rng_min(v.get(key))
        return float(v.get("min", 0))
    return float(v)


def distill(trigger, cond, tags):
    """Reduce a criterion to the engine-matchable schema: a dict of non-default
    fields. `unmatchable` is set only where the engine has no way to observe
    the shape at all; the engine sites decide the rest at runtime."""
    t = strip_ns(trigger)
    c = cond or {}
    d = {"trigger": t}
    if t == "inventory_changed":
        preds = [items_pred(p, tags) for p in c.get("items", [])]
        preds = [p for p in preds if p is not None]
        if not preds:
            d["unmatchable"] = True  # slot-count/any-item shapes: not yet
        d["items"] = preds
    elif t in ("consume_item", "fishing_rod_hooked", "filled_bucket", "used_totem",
               "shot_crossbow", "item_durability_changed"):
        p = items_pred(c.get("item", {}), tags)
        d["items"] = [p] if p else []
        if t == "item_durability_changed":
            for pl in c.get("player", []) or []:
                v = ((pl.get("predicate") or {}).get("vehicle") or {}).get("type")
                if v: d["vehicle"] = strip_ns(v)
    elif t == "placed_block":
        groups = loc_checks(c.get("location", []), tags)
        simple = [g for g in groups if len(g) == 1 and g[0]["dx"] == g[0]["dy"] == g[0]["dz"] == 0
                  and not g[0]["props"] and len(g[0]["blocks"]) == 1]
        if len(groups) == 1 and simple:
            d["block"] = simple[0][0]["blocks"][0]
        elif groups:
            d["locChecks"] = groups
        else:
            d["unmatchable"] = True
    elif t == "item_used_on_block":
        for term in c.get("location", []) or []:
            cc = term.get("condition")
            if cc == "minecraft:location_check":
                l = loc_of(term.get("predicate") or {})
                if "blocks" in l: d["blocks"] = names_of(l["blocks"], tags, "block")
                for k in ("props", "biome", "structure", "smokey"):
                    if k in l: d[k] = l[k]
            elif cc == "minecraft:match_tool":
                p = items_pred(term.get("predicate") or {}, tags)
                if p is not None: d["items"] = [p]
                if "predicates" in (term.get("predicate") or {}):
                    d["toolPred"] = list((term["predicate"]["predicates"]).keys())[0].split(":")[-1]
    elif t in ("player_killed_entity", "entity_killed_player", "tame_animal", "summoned_entity",
               "thrown_item_picked_up_by_player"):
        d["entity"], _, _ = ent_pred(c.get("entity"))
    elif t == "bred_animals":
        d["entity"], _, _ = ent_pred(c.get("child"))
    elif t in ("player_interacted_with_entity", "player_sheared_equipment", "thrown_item_picked_up_by_entity"):
        d["entity"], baby, var = ent_pred(c.get("entity"))
        if baby is not None: d["baby"] = 1 if baby else 0
        if var: d["variant"] = var
        p = items_pred(c.get("item", {}), tags)
        if p: d["items"] = [p]
    elif t == "changed_dimension":
        d["dim"] = DIMS.get(c.get("to"), -1)
        if "from" in c:
            d["unmatchable"] = True
    elif t == "location":
        for pl in c.get("player", []) or []:
            p = pl.get("predicate") or {}
            l = loc_of(p.get("location") or {})
            for k in ("biome", "structure"):
                if k in l: d[k] = l[k]
            so = (p.get("stepping_on") or {}).get("block") or {}
            if so.get("blocks") is not None:
                d["blocks"] = names_of(so["blocks"], tags, "block")
            feet = ((p.get("equipment") or {}).get("feet") or {})
            fp = items_pred(feet, tags) if feet else None
            if fp: d["equipFeet"] = fp
        if not any(k in d for k in ("biome", "structure", "blocks")):
            d["unmatchable"] = True
    elif t == "construct_beacon":
        lv = c.get("level", {})
        d["minLevel"] = int(lv.get("min", 0)) if isinstance(lv, dict) else int(lv)
    elif t == "effects_changed":
        eff = c.get("effects")
        if eff:
            d["effects"] = [strip_ns(k) for k in eff.keys()]
        src, _, _ = ent_pred(c.get("source"))
        if src: d["sourceEntity"] = src
    elif t in ("recipe_crafted", "crafter_recipe_crafted"):
        d["recipe"] = strip_ns(c.get("recipe_id", ""))
        ings = [items_pred(i, tags) for i in c.get("ingredients", [])]
        ings = [i for i in ings if i]
        if ings: d["ingredients"] = ings
    elif t == "player_generates_container_loot":
        d["lootTable"] = strip_ns(c.get("loot_table", ""))
    elif t in ("player_hurt_entity", "entity_hurt_player"):
        dm = c.get("damage") or {}
        ty = dm.get("type") or {}
        de = ty.get("direct_entity") or {}
        if de.get("type"):
            d["damageDirect"] = names_of(de["type"], tags, "entity_type")
        mh = ((de.get("equipment") or {}).get("mainhand") or {})
        mp = items_pred(mh, tags) if mh else None
        if mp: d["mainhand"] = mp
        for tg in ty.get("tags", []) or []:
            if tg.get("expected", True): d["damageTag"] = strip_ns(tg["id"])
        if dm.get("dealt") is not None: d["minDealt"] = rng_min(dm["dealt"])
        if dm.get("blocked"): d["blocked"] = True
    elif t == "killed_by_arrow":
        p = items_pred(c.get("fired_from_weapon", {}), tags)
        if p: d["items"] = [p]
        if c.get("unique_entity_types") is not None:
            d["minUnique"] = int(rng_min(c["unique_entity_types"]))
        vs = []
        for v in c.get("victims", []) or []:
            et, _, _ = ent_pred(v)
            vs.append(et)
        if vs: d["victims"] = vs
    elif t == "using_item":
        p = items_pred(c.get("item", {}), tags)
        if p: d["items"] = [p]
        for pl in c.get("player", []) or []:
            la = (((pl.get("predicate") or {}).get("type_specific") or {}).get("looking_at") or {}).get("type")
            if la: d["lookingAt"] = strip_ns(la)
    elif t in ("enter_block", "slide_down_block", "bee_nest_destroyed"):
        if c.get("block"): d["blocks"] = names_of(c["block"], tags, "block")
        if t == "bee_nest_destroyed":
            if c.get("num_bees_inside") is not None: d["minCount"] = int(rng_min(c["num_bees_inside"]))
            ip = c.get("item") or {}
            ench = (ip.get("predicates") or {}).get("minecraft:enchantments")
            if ench: d["enchant"] = strip_ns(ench[0]["enchantments"])
    elif t == "target_hit":
        d["signal"] = int(rng_min(c.get("signal_strength")))
        for pr in c.get("projectile", []) or []:
            dist = (pr.get("predicate") or {}).get("distance") or {}
            if dist.get("horizontal"): d["minDistH"] = rng_min(dist["horizontal"])
    elif t in ("levitation", "nether_travel", "ride_entity_in_lava", "fall_from_height", "fall_after_explosion"):
        dist = c.get("distance") or {}
        if dist.get("y"): d["minDistY"] = rng_min(dist["y"])
        if dist.get("horizontal"): d["minDistH"] = rng_min(dist["horizontal"])
        if dist.get("absolute"): d["minDistAbs"] = rng_min(dist["absolute"])
        sp = (c.get("start_position") or {}).get("position") or {}
        if sp.get("y"): d["startYMin"] = rng_min(sp["y"])
        for pl in c.get("player", []) or []:
            p = pl.get("predicate") or {}
            pos = ((p.get("location") or {}).get("position") or {})
            if isinstance(pos.get("y"), dict) and "max" in pos["y"]: d["endYMax"] = float(pos["y"]["max"])
            v = (p.get("vehicle") or {}).get("type")
            if v: d["vehicle"] = strip_ns(v)
            dm = (p.get("location") or {}).get("dimension")
            if dm: d["dim"] = DIMS.get(dm, -1)
        cause, _, _ = ent_pred(c.get("cause"))
        if cause: d["cause"] = cause
    elif t == "lightning_strike":
        by, _, _ = ent_pred(c.get("bystander"))
        if by: d["bystander"] = by
        for l in c.get("lightning", []) or []:
            ts = (l.get("predicate") or {}).get("type_specific") or {}
            if ts.get("blocks_set_on_fire") == 0: d["noFire"] = True
            dist = (l.get("predicate") or {}).get("distance") or {}
            if dist.get("absolute"): d["maxDistAbs"] = float(dist["absolute"].get("max", 0))
    elif t == "started_riding":
        for pl in c.get("player", []) or []:
            v = (pl.get("predicate") or {}).get("vehicle") or {}
            if v.get("type"): d["vehicle"] = names_of(v["type"], tags, "entity_type")[0] if names_of(v["type"], tags, "entity_type") else ""
            if (v.get("passenger") or {}).get("type"): d["passenger"] = strip_ns(v["passenger"]["type"])
    elif t == "spear_mobs":
        d["minCount"] = int(rng_min(c.get("count")))
    elif t == "channeled_lightning":
        vs = []
        for v in c.get("victims", []) or []:
            et, _, _ = ent_pred(v)
            vs.append(et)
        if vs: d["victims"] = vs
    elif t == "allay_drop_item_on_block":
        for term in c.get("location", []) or []:
            if term.get("condition") == "minecraft:location_check":
                l = loc_of(term.get("predicate") or {})
                if "blocks" in l: d["blocks"] = names_of(l["blocks"], tags, "block")
            elif term.get("condition") == "minecraft:match_tool":
                p = items_pred(term.get("predicate") or {}, tags)
                if p: d["items"] = [p]
    elif t in ("slept_in_bed", "villager_trade", "enchanted_item", "brewed_potion",
               "cured_zombie_villager", "avoid_vibration", "hero_of_the_village",
               "kill_mob_near_sculk_catalyst"):
        pass  # condition-free
    else:
        d["unmatchable"] = True  # trigger not yet observable engine-side
    # Triggers whose mechanics the engine does not have at all stay flagged, so
    # the tree keeps telling the truth about what is obtainable.
    if t in NOT_OBSERVABLE:
        d["unmatchable"] = True
    return d


# Triggers with NO engine mechanic behind them (yet). Listed here rather than
# silently unmatched so the count in the header stays honest.
NOT_OBSERVABLE = {
    "channeled_lightning",          # no channeling trident / lightning entity
    "slide_down_block",             # no honey-block slide
    "player_sheared_equipment",     # no wolf armour
    "thrown_item_picked_up_by_entity",  # no piglin bartering
    "thrown_item_picked_up_by_player",  # no allay behaviour
    "allay_drop_item_on_block",
    "started_riding",               # no mob-in-boat (goat) riding
    "spear_mobs",                   # no spear
    "avoid_vibration",              # sneaking does not yet suppress vibrations
}


# ---- vanilla TreeNodePosition (Buchheim tidy tree), x = depth, y = row ----
class TNP:
    def __init__(self, node, parent, prev_sib, child_index, depth, children_of):
        self.node, self.parent, self.prev = node, parent, prev_sib
        self.child_index = child_index
        self.children = []
        self.ancestor, self.thread = self, None
        self.x, self.y = depth, -1.0
        self.mod = self.change = self.shift = 0.0
        prev = None
        for ch in children_of(node):
            prev = self.add_child(ch, prev, children_of)

    def add_child(self, node, prev, children_of):
        if node["display"] is not None:
            prev = TNP(node, self, prev, len(self.children) + 1,
                       self.x + 1, children_of)
            self.children.append(prev)
        else:
            for gc in children_of(node):
                prev = self.add_child(gc, prev, children_of)
        return prev

    def first_walk(self):
        if not self.children:
            self.y = self.prev.y + 1.0 if self.prev else 0.0
            return
        default_ancestor = None
        for ch in self.children:
            ch.first_walk()
            default_ancestor = ch.apportion(
                ch if default_ancestor is None else default_ancestor)
        self.execute_shifts()
        mid = (self.children[0].y + self.children[-1].y) / 2.0
        if self.prev:
            self.y = self.prev.y + 1.0
            self.mod = self.y - mid
        else:
            self.y = mid

    def second_walk(self, mod_sum, depth, mn):
        self.y += mod_sum
        self.x = depth
        mn = min(mn, self.y)
        for ch in self.children:
            mn = ch.second_walk(mod_sum + self.mod, depth + 1, mn)
        return mn

    def third_walk(self, off):
        self.y += off
        for ch in self.children:
            ch.third_walk(off)

    def execute_shifts(self):
        shift = change = 0.0
        for ch in reversed(self.children):
            ch.y += shift
            ch.mod += shift
            change += ch.change
            shift += ch.shift + change

    def prev_or_thread(self):
        return self.thread or (self.children[0] if self.children else None)

    def next_or_thread(self):
        return self.thread or (self.children[-1] if self.children else None)

    def apportion(self, default_ancestor):
        if self.prev is None:
            return default_ancestor
        vir = vor = self
        vil, vol = self.prev, self.parent.children[0]
        sir = sor = self.mod
        sil, sol = vil.mod, vol.mod
        while vil.next_or_thread() and vir.prev_or_thread():
            vil = vil.next_or_thread()
            vir = vir.prev_or_thread()
            vol = vol.prev_or_thread()
            vor = vor.next_or_thread()
            vor.ancestor = self
            shift = vil.y + sil - (vir.y + sir) + 1.0
            if shift > 0.0:
                vil.get_ancestor(self, default_ancestor).move_subtree(self, shift)
                sir += shift
                sor += shift
            sil += vil.mod
            sir += vir.mod
            sol += vol.mod
            sor += vor.mod
        if vil.next_or_thread() and not vor.next_or_thread():
            vor.thread = vil.next_or_thread()
            vor.mod += sil - sor
        else:
            if vir.prev_or_thread() and not vol.prev_or_thread():
                vol.thread = vir.prev_or_thread()
                vol.mod += sir - sol
            default_ancestor = self
        return default_ancestor

    def move_subtree(self, right, shift):
        subtrees = float(right.child_index - self.child_index)
        if subtrees != 0.0:
            right.change -= shift / subtrees
            self.change += shift / subtrees
        right.shift += shift
        right.y += shift
        right.mod += shift

    def get_ancestor(self, other, default_ancestor):
        if self.ancestor is not None and self.ancestor in other.parent.children:
            return self.ancestor
        return default_ancestor

    def finalize(self, out):
        if self.node["display"] is not None:
            out[self.node["id"]] = (float(self.x), self.y)
        for ch in self.children:
            ch.finalize(out)

    @staticmethod
    def run(root, children_of):
        tp = TNP(root, None, None, 1, 0, children_of)
        tp.first_walk()
        mn = tp.second_walk(0.0, 0, tp.y)
        if mn < 0.0:
            tp.third_walk(-mn)
        out = {}
        tp.finalize(out)
        return out


def gstr(s):
    return json.dumps(s)  # Go string literal via JSON escaping


def main():
    z = load_jar(JAR)
    tags = Tags(z)
    item_ids = load_item_ids()
    lang = json.loads(z.read("assets/minecraft/lang/en_us.json"))

    nodes = {}
    for n in sorted(z.namelist()):
        if (not n.startswith("data/minecraft/advancement/") or
                "/recipes/" in n or not n.endswith(".json")):
            continue
        d = json.loads(z.read(n))
        aid = "minecraft:" + n[len("data/minecraft/advancement/"):-len(".json")]
        crits = []
        for cname, c in d["criteria"].items():
            crits.append({"name": cname,
                          **distill(c["trigger"], c.get("conditions"), tags)})
        reqs = d.get("requirements") or [[k] for k in d["criteria"]]
        disp = None
        if "display" in d:
            dd = d["display"]
            title_key = dd["title"].get("translate", "") or dd["title"].get("text", "")
            desc_key = dd["description"].get("translate", "") or dd["description"].get("text", "")
            icon = strip_ns(dd["icon"]["id"])
            if icon not in item_ids:
                raise SystemExit(f"{aid}: unknown icon item {icon}")
            disp = {
                "title": title_key, "desc": desc_key,
                "titleEN": lang.get(title_key, title_key),
                "descEN": lang.get(desc_key, desc_key),
                "icon": item_ids[icon],
                "frame": FRAMES[dd.get("frame", "task")],
                "background": strip_ns(dd["background"]) if "background" in dd else "",
                "showToast": dd.get("show_toast", True),
                "announceChat": dd.get("announce_to_chat", True),
                "hidden": dd.get("hidden", False),
            }
        nodes[aid] = {"id": aid, "parent": d.get("parent", ""),
                      "criteria": crits, "reqs": reqs, "display": disp,
                      "xp": (d.get("rewards") or {}).get("experience", 0)}

    # layout per root (vanilla runs TreeNodePosition per tree)
    kids = {}
    for nd in nodes.values():
        kids.setdefault(nd["parent"], []).append(nd)
    for v in kids.values():
        v.sort(key=lambda nd: nd["id"])  # jar order is alphabetical already

    def children_of(nd):
        return kids.get(nd["id"], [])

    pos = {}
    for nd in nodes.values():
        if nd["parent"] == "":
            pos.update(TNP.run(nd, children_of))

    unmatch = sum(1 for nd in nodes.values()
                  for c in nd["criteria"] if c.get("unmatchable"))
    total = sum(len(nd["criteria"]) for nd in nodes.values())

    w = []
    w.append("// Code generated by scripts/gen_advancements.py. DO NOT EDIT.")
    w.append("// Source: 1.21.11 server jar data/minecraft/advancement (recipe")
    w.append("// unlocks excluded) + tags/item + en_us.json; layout matches the")
    w.append("// vanilla tidy tree. %d advancements, %d criteria (%d not yet" % (
        len(nodes), total, unmatch))
    w.append("// observable engine-side — their advancements stay unobtainable).")
    w.append("")
    w.append("package server")
    w.append("")
    w.append("var advTable = []advNode{")
    for aid in sorted(nodes):
        nd = nodes[aid]
        w.append("\t{")
        w.append(f"\t\tid: {gstr(nd['id'])}, parent: {gstr(nd['parent'])}, xp: {nd['xp']},")
        w.append("\t\tcriteria: []advCriterion{")
        for c in nd["criteria"]:
            f = [f"name: {gstr(c['name'])}", f"trigger: {gstr(c['trigger'])}"]
            if c.get("unmatchable"):
                f.append("unmatchable: true")
            if c.get("entity"):
                f.append(f"entity: {gstr(c['entity'])}")
            if c.get("block"):
                f.append(f"block: {gstr(c['block'])}")
            if c.get("biome"):
                f.append(f"biome: {gstr(c['biome'])}")
            if "dim" in c:
                f.append(f"dim: {c['dim']}, hasDim: true")
            if c.get("minLevel"):
                f.append(f"minLevel: {c['minLevel']}")
            if c.get("items"):
                sets = []
                for p in c["items"]:
                    ids = sorted(set(item_ids[x] for x in p if x in item_ids))
                    sets.append("{%s}" % ", ".join(map(str, ids)))
                f.append("items: [][]int32{%s}" % ", ".join(sets))
            if c.get("ingredients"):
                sets = []
                for p in c["ingredients"]:
                    ids = sorted(set(item_ids[x] for x in p if x in item_ids))
                    sets.append("{%s}" % ", ".join(map(str, ids)))
                f.append("ingredients: [][]int32{%s}" % ", ".join(sets))
            if c.get("equipFeet"):
                ids = sorted(set(item_ids[x] for x in c["equipFeet"] if x in item_ids))
                f.append("equipFeet: []int32{%s}" % ", ".join(map(str, ids)))
            if c.get("mainhand"):
                ids = sorted(set(item_ids[x] for x in c["mainhand"] if x in item_ids))
                f.append("mainhand: []int32{%s}" % ", ".join(map(str, ids)))
            if c.get("blocks"):
                f.append("blocks: []string{%s}" % ", ".join(gstr(b) for b in c["blocks"]))
            if c.get("props"):
                f.append("props: map[string]string{%s}" % ", ".join(
                    f"{gstr(k)}: {gstr(v)}" for k, v in sorted(c["props"].items())))
            if c.get("locChecks"):
                groups = []
                for g in c["locChecks"]:
                    checks = []
                    for chk in g:
                        props = ", ".join(f"{gstr(k)}: {gstr(v)}" for k, v in sorted(chk["props"].items()))
                        checks.append("{dx: %d, dy: %d, dz: %d, blocks: []string{%s}, props: map[string]string{%s}}" % (
                            chk["dx"], chk["dy"], chk["dz"], ", ".join(gstr(b) for b in chk["blocks"]), props))
                    groups.append("{%s}" % ", ".join(checks))
                f.append("locChecks: [][]advLocCheck{%s}" % ", ".join(groups))
            for key in ("structure", "recipe", "lootTable", "sourceEntity", "damageTag",
                        "lookingAt", "vehicle", "passenger", "bystander", "cause", "variant",
                        "enchant", "toolPred"):
                if c.get(key):
                    f.append(f"{key}: {gstr(c[key])}")
            if c.get("damageDirect"):
                f.append("damageDirect: []string{%s}" % ", ".join(gstr(x) for x in c["damageDirect"]))
            if c.get("victims"):
                f.append("victims: []string{%s}" % ", ".join(gstr(x) for x in c["victims"]))
            if c.get("effects"):
                f.append("effects: []string{%s}" % ", ".join(gstr(x) for x in c["effects"]))
            for key in ("minUnique", "minCount", "signal"):
                if key in c:
                    f.append(f"{key}: {c[key]}")
            if "baby" in c:
                f.append(f"baby: {c['baby']}, hasBaby: true")
            for key in ("minDealt", "minDistH", "minDistY", "minDistAbs", "maxDistAbs", "startYMin", "endYMax"):
                if key in c:
                    f.append(f"{key}: {c[key]}")
            for key in ("smokey", "blocked", "noFire"):
                if c.get(key):
                    f.append(f"{key}: true")
            w.append("\t\t\t{%s}," % ", ".join(f))
        w.append("\t\t},")
        w.append("\t\treqs: [][]string{")
        for r in nd["reqs"]:
            w.append("\t\t\t{%s}," % ", ".join(gstr(x) for x in r))
        w.append("\t\t},")
        if nd["display"] is not None:
            dd = nd["display"]
            x, y = pos.get(aid, (0.0, 0.0))
            w.append("\t\tdisplay: &advDisplay{")
            w.append(f"\t\t\ttitle: {gstr(dd['title'])}, desc: {gstr(dd['desc'])},")
            w.append(f"\t\t\ttitleEN: {gstr(dd['titleEN'])}, descEN: {gstr(dd['descEN'])},")
            w.append(f"\t\t\ticon: {dd['icon']}, frame: {dd['frame']}, background: {gstr(dd['background'])},")
            w.append(f"\t\t\tshowToast: {str(dd['showToast']).lower()}, announceChat: {str(dd['announceChat']).lower()}, hidden: {str(dd['hidden']).lower()},")
            w.append(f"\t\t\tx: {x}, y: {y},")
            w.append("\t\t},")
        w.append("\t},")
    w.append("}")
    w.append("")
    with open(OUT, "w") as fo:
        fo.write("\n".join(w))
    print(f"wrote {OUT}: {len(nodes)} advancements, {total} criteria "
          f"({unmatch} unmatchable), {len(pos)} positioned")


if __name__ == "__main__":
    main()
