#!/usr/bin/env python3
"""Regenerate internal/server/advancements_gen.go — the vanilla advancement
tree (the canonical version's), distilled for the engine's criteria tracker.

Sources (all local, no network):
  - data/minecraft/advancement/**.json   from the canonical version's server jar
    (recipe-unlock advancements under advancement/recipes/ are excluded —
    they belong to the recipe book)
  - data/minecraft/tags/item/*.json      same jar (predicate tag expansion)
  - assets/minecraft/lang/en_us.json     same jar (English titles for chat
    announce + Bedrock fallback; Java clients render the translate keys)
  - internal/server/itemnames_gen.go     icon/predicate item ids (the engine's
    one item-id space, from vanilla's registries report)

Tree layout (each display's x,y) is computed here the way the vanilla
server lays out its advancement screen (a Buchheim tidy tree over the
visible nodes, x = depth, y = tidy row) so the client's tree matches vanilla.

Run: python3 scripts/gen_advancements.py [path-to-server.jar]
"""
import canon
import io
import json
import os
import re
import sys
import zipfile

JAR = sys.argv[1] if len(sys.argv) > 1 else canon.jar(canon.VERSION)  # reads 26.x shapes: normalize()
OUT = "internal/server/advancements_gen.go"
# The version named in the generated header: the jar's, from its file name.
_m = re.search(r"server-(.+)\.jar$", os.path.basename(JAR))
SOURCE = _m.group(1) if _m else "vanilla"

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
        # 1.21.5 moved the variant onto the item-component map: a cat's is
        # "minecraft:cat/variant", a wolf's "minecraft:wolf/variant", a frog's
        # "minecraft:frog/variant". Without reading these, every criterion of
        # complete_catalogue / whole_pack / leash_all_frog_variants matched any
        # animal at all and the advancement fell out on the first tame.
        comps = p.get("components") or {}
        if not var and isinstance(comps, dict):
            for k, v in comps.items():
                if str(k).endswith("/variant") and isinstance(v, str):
                    var = strip_ns(v)
                    if not t:
                        t = strip_ns(str(k)).split("/")[0]
                    break
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
    """placed_block's location list -> OR-groups of AND-ed (offset, blocks,
    props) checks. The list itself is an AND (a context predicate), any_of
    and all_of nest freely, so the result is the disjunctive normal form: a
    location_check is one offset check, a block_state_property checks the
    placed block itself. Other condition kinds are ignored (they constrain
    nothing the engine can see)."""
    def dnf(term):  # -> list of alternatives, each a list of checks
        c = term.get("condition", "")
        if c == "minecraft:location_check":
            l = loc_of(term.get("predicate") or {})
            return [[{"dx": term.get("offsetX", 0), "dy": term.get("offsetY", 0),
                      "dz": term.get("offsetZ", 0),
                      "blocks": names_of(l.get("blocks"), tags, "block"),
                      "props": l.get("props", {})}]]
        if c == "minecraft:block_state_property":
            return [[{"dx": 0, "dy": 0, "dz": 0,
                      "blocks": names_of(term["block"], tags, "block"),
                      "props": {k: str(v) for k, v in (term.get("properties") or {}).items()}}]]
        if c == "minecraft:any_of":
            return [g for t in term.get("terms", []) for g in dnf(t)]
        if c == "minecraft:all_of":
            return conj(term.get("terms", []))
        return [[]]

    def conj(ts):  # AND of terms: every combination of their alternatives
        out = [[]]
        for t in ts:
            out = [g + h for g in out for h in dnf(t)]
        return out

    return [g for g in conj(terms or []) if g]


# ---- 26.x condition shapes -> the shape distill() reads ----
#
# 26.x rewrote the predicate formats: a context predicate (player, entity,
# child, location, ...) is one condition object rather than a list of them
# (a top-level all_of stands for the old implicit AND), a condition names
# itself with "type" rather than "condition", block_state_property became
# match_block {blocks, state}, entity-predicate fields are namespaced
# ("minecraft:entity_type", "minecraft:flags", "minecraft:type_specific/
# player"), damage-type tag ids carry a '#', and a few trigger fields were
# renamed (recipe_id -> recipes, loot_table -> loot_tables, block -> blocks).
# normalize() rewrites a criterion's conditions into the older shape, so
# distill() reads either version's data and gives the same answer wherever
# the two versions mean the same thing. Older data passes through unchanged.

LOOT_CONDITIONS = {"minecraft:entity_properties", "minecraft:inverted", "minecraft:any_of",
                   "minecraft:all_of", "minecraft:location_check", "minecraft:match_block",
                   "minecraft:match_tool", "minecraft:block_state_property"}

# Entity-predicate fields whose values are themselves entity predicates.
NESTED_ENTITY = {"vehicle", "passenger", "targeted_entity", "looking_at",
                 "direct_entity", "source_entity"}


def is_condition(v):
    """A 26.x condition object (the older shape names itself "condition")."""
    return isinstance(v, dict) and v.get("type") in LOOT_CONDITIONS


def norm_entity(p):
    """An entity predicate (26.x namespaced fields) -> the older field names."""
    if not isinstance(p, dict):
        return p
    out = {}
    for k, v in p.items():
        if k == "minecraft:entity_type":
            out["type"] = v
            continue
        if k.startswith("minecraft:type_specific/"):
            ts = {"type": "minecraft:" + k.split("/", 1)[1]}
            for kk, vv in (v or {}).items():
                ts[kk] = norm_entity(vv) if kk in NESTED_ENTITY else vv
            out["type_specific"] = ts
            continue
        k = strip_ns(k) if k.startswith("minecraft:") else k
        out[k] = norm_entity(v) if k in NESTED_ENTITY else v
    return out


def norm_damage(d):
    """A damage-source predicate: nested entity predicates + '#'-less tag ids."""
    if not isinstance(d, dict):
        return d
    out = {}
    for k, v in d.items():
        if k in ("direct_entity", "source_entity"):
            v = norm_entity(v)
        elif k == "tags":
            v = [{**t, "id": t["id"].lstrip("#")} for t in v]
        out[k] = v
    return out


def norm_cond(c):
    """One loot condition -> the older {"condition": ...} shape."""
    if "condition" in c:  # already the older shape
        return c
    t = c["type"]
    out = {"condition": t}
    if t == "minecraft:match_block":
        out["condition"] = "minecraft:block_state_property"
        out["block"] = c["blocks"]
        if c.get("state"):
            out["properties"] = c["state"]
        return out
    for k, v in c.items():
        if k == "type":
            continue
        if k == "predicate" and t == "minecraft:entity_properties":
            v = norm_entity(v)
        elif k == "terms":
            v = [norm_cond(x) for x in v]
        elif k == "term":
            v = norm_cond(v)
        out[k] = v
    return out


def norm_context(c):
    """A context predicate -> the older list-of-conditions (an implicit AND).
    not(any_of(a, b)) is the older not(a), not(b)."""
    if isinstance(c, list):
        return [norm_cond(x) for x in c]
    if c.get("type") == "minecraft:all_of":
        return [norm_cond(x) for x in c["terms"]]
    if c.get("type") == "minecraft:inverted" and (c.get("term") or {}).get("type") == "minecraft:any_of":
        return [{"condition": "minecraft:inverted", "term": norm_cond(x)} for x in c["term"]["terms"]]
    return [norm_cond(c)]


def single(v, what):
    """A 26.x one-or-list id field -> the one id the engine matches on."""
    if isinstance(v, list):
        if len(v) != 1:
            raise SystemExit(f"gen_advancements: {what} lists {len(v)} ids; the engine matches one")
        return v[0]
    return v


def normalize(trigger, cond):
    """A criterion's conditions, either version's shape, in the older shape."""
    if not cond:
        return cond
    t = strip_ns(trigger)
    out = {}
    for k, v in cond.items():
        if k == "recipes" and t in ("recipe_crafted", "crafter_recipe_crafted"):
            k, v = "recipe_id", single(v, k)
        elif k == "loot_tables" and t == "player_generates_container_loot":
            k, v = "loot_table", single(v, k)
        elif k == "blocks" and t in ("enter_block", "slide_down_block", "bee_nest_destroyed"):
            k = "block"
        elif k == "killing_blow":
            v = norm_damage(v)
        elif k == "damage" and isinstance(v, dict) and "type" in v:
            v = {**v, "type": norm_damage(v["type"])}
        elif is_condition(v):
            v = norm_context(v)
        elif isinstance(v, list) and v and all(is_condition(x) for x in v):
            v = [norm_context(x) for x in v]  # victims: a list of context predicates
        out[k] = v
    return out


def rng_min(v, key=None):
    """A number-or-{min,max} range -> its min (0 if absent)."""
    if v is None:
        return 0
    if isinstance(v, dict):
        if key is not None:
            return rng_min(v.get(key))
        return float(v.get("min", 0))
    return float(v)


OMINOUS_BANNER = "block.minecraft.ominous_banner"


def body_pred(d, c, tags):
    """An interacted entity's body armour after the interaction: its item
    and exact damage (repair_wolf_armor: wolf_armor at damage 0, i.e. the
    scute finished the repair). Other equipment shapes stop the generator."""
    for pc in c.get("entity") or []:
        eq = (pc.get("predicate") or {}).get("equipment")
        if eq is None:
            continue
        body = eq.get("body") or {}
        comps = body.get("components") or {}
        if set(eq) != {"body"} or not set(body) <= {"items", "components"} or not set(comps) <= {"minecraft:damage"}:
            raise SystemExit(f"gen_advancements: interacted entity equipment {eq}")
        if "items" in body:
            d["bodyItems"] = names_of(body["items"], tags, "item")
        if "minecraft:damage" in comps:
            d["bodyDamage"] = int(comps["minecraft:damage"])
ARMOR_SLOTS = {"head", "chest", "legs", "feet"}


def player_pred(d, c, tags):
    """The criterion's player predicate, in the two shapes 26.3 gives these
    triggers: standing at or above a height (trade_at_world_height), and
    wearing none of an item set in any armour slot (distract_piglin's
    not-any_of over the four slots, which the normaliser splits into four
    inverted terms). Anything else stops the generator."""
    avoid = {}
    for pc in c.get("player") or []:
        cond = strip_ns(pc.get("condition", ""))
        if cond == "entity_properties":
            p = pc.get("predicate") or {}
            pos = (p.get("location") or {}).get("position") or {}
            if set(p) != {"location"} or set(p["location"]) != {"position"} or set(pos) != {"y"} \
                    or set(pos["y"]) != {"min"}:
                raise SystemExit(f"gen_advancements: player predicate {p}")
            d["playerMinY"] = rng_min(pos["y"])
        elif cond == "inverted":
            term = pc.get("term") or {}
            eq = (term.get("predicate") or {}).get("equipment") or {}
            if strip_ns(term.get("condition", "")) != "entity_properties" or set(term["predicate"]) != {"equipment"} \
                    or len(eq) != 1 or not set(eq) <= ARMOR_SLOTS:
                raise SystemExit(f"gen_advancements: inverted player predicate {term}")
            (slot, want), = eq.items()
            if set(want) != {"items"}:
                raise SystemExit(f"gen_advancements: inverted player equipment {want}")
            avoid[slot] = tuple(names_of(want["items"], tags, "item"))
        else:
            raise SystemExit(f"gen_advancements: player condition {cond}")
    if avoid:
        if set(avoid) != ARMOR_SLOTS or len(set(avoid.values())) != 1:
            raise SystemExit(f"gen_advancements: player armour predicate {avoid}")
        d["playerNotWearing"] = list(next(iter(avoid.values())))


def kill_pred(d, c, tags):
    """The parts of a kill criterion (Player/EntityKilled... TriggerInstance:
    entity + killing_blow) beyond the entity's type: a type tag, the entity's
    horizontal distance from the player and its dimension, an ominous banner
    on its head (a raid captain), and the killing blow's direct entity and
    damage-type tag. A shape outside these stops the generator rather than
    matching loosely."""
    for pc in c.get("entity") or []:
        p = pc.get("predicate") or {}
        for k, v in p.items():
            if k in ("type", "flags", "type_specific", "components"):
                continue  # ent_pred reads these
            if k == "distance":
                if set(v) != {"horizontal"} or set(v["horizontal"]) != {"min"}:
                    raise SystemExit(f"gen_advancements: kill distance {v}")
                d["minDistH"] = rng_min(v["horizontal"])
            elif k == "location":
                if set(v) != {"dimension"}:
                    raise SystemExit(f"gen_advancements: kill location {v}")
                d["dim"] = DIMS[v["dimension"]]
            elif k == "equipment":
                head = v.get("head") or {}
                name = ((head.get("components") or {}).get("minecraft:item_name") or {}).get("translate")
                if set(v) != {"head"} or strip_ns(str(head.get("items"))) != "white_banner" or name != OMINOUS_BANNER:
                    raise SystemExit(f"gen_advancements: kill equipment {v}")
                d["ominousBanner"] = True
            else:
                raise SystemExit(f"gen_advancements: kill entity predicate field {k}")
        ty = p.get("type")
        if isinstance(ty, str) and ty.startswith("#"):
            d["entities"] = names_of(ty, tags, "entity_type")
    kb = c.get("killing_blow") or {}
    for k, v in kb.items():
        if k == "direct_entity":
            if set(v) != {"type"}:
                raise SystemExit(f"gen_advancements: killing_blow direct_entity {v}")
            d["damageDirect"] = names_of(v["type"], tags, "entity_type")
        elif k == "tags":
            want = [tg for tg in v if tg.get("expected", True)]
            if len(want) != 1 or len(want) != len(v):
                raise SystemExit(f"gen_advancements: killing_blow tags {v}")
            d["damageTag"] = strip_ns(want[0]["id"])
        else:
            raise SystemExit(f"gen_advancements: killing_blow field {k}")


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
        # The variant matters here too: complete_catalogue is eleven tame_animal
        # criteria that differ only by the cat's coat, whole_pack nine by the
        # wolf's.
        d["entity"], baby, var = ent_pred(c.get("entity"))
        if baby is not None: d["baby"] = 1 if baby else 0
        if var: d["variant"] = var
        if t in ("player_killed_entity", "entity_killed_player"):
            kill_pred(d, c, tags)
    elif t == "bred_animals":
        d["entity"], baby, var = ent_pred(c.get("child"))
        if baby is not None: d["baby"] = 1 if baby else 0
        if var: d["variant"] = var
    elif t in ("player_interacted_with_entity", "player_sheared_equipment", "thrown_item_picked_up_by_entity"):
        d["entity"], baby, var = ent_pred(c.get("entity"))
        if baby is not None: d["baby"] = 1 if baby else 0
        if var: d["variant"] = var
        if t == "player_interacted_with_entity":
            body_pred(d, c, tags)
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
            if v.get("type"): d["vehicles"] = names_of(v["type"], tags, "entity_type")  # a tag keeps every member (#boat)
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
    # So do criteria naming content the engine does not have yet: a mob it
    # never spawns, a biome its generator never places.
    if d.get("entity") in ABSENT_ENTITIES or d.get("biome") in ABSENT_BIOMES:
        d["unmatchable"] = True
    if t in ("villager_trade", "thrown_item_picked_up_by_entity", "player_interacted_with_entity"):
        player_pred(d, c, tags)
    return d


# Triggers with NO engine mechanic behind them (yet). Listed here rather than
# silently unmatched so the count in the header stays honest.
NOT_OBSERVABLE = {
    # Six of these had live fire sites by 2026-09-20 and were only held back
    # by this list: channeled_lightning (trident channeling), slide_down_block
    # (honey), thrown_item_picked_up_by_player (tosses), allay_drop_item_on_
    # block, started_riding, avoid_vibration (sneak-suppressed vibrations) —
    # and thrown_item_picked_up_by_entity now that piglins pick gold up.
    # spear_mobs left it with the spear's charge (spear.go).
}

# 26.3 content the engine does not simulate yet. Criteria naming it stay
# unmatchable, so the advancements needing them stay unobtainable until it
# lands. Empty since 2026-09-24: the sulfur cube fires uh_oh's two criteria
# (sulfurcube.go) and sulfur_caves generates.
ABSENT_ENTITIES = set()
ABSENT_BIOMES = set()


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
                          **distill(c["trigger"], normalize(c["trigger"], c.get("conditions")), tags)})
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
    w.append(f"// Source: {SOURCE} server jar data/minecraft/advancement (recipe")
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
            if c.get("vehicles"):
                f.append("vehicles: []string{%s}" % ", ".join(gstr(x) for x in c["vehicles"]))
            if c.get("bodyItems"):
                ids = sorted(set(item_ids[x] for x in c["bodyItems"] if x in item_ids))
                f.append("bodyItems: []int32{%s}" % ", ".join(map(str, ids)))
            if "bodyDamage" in c:
                f.append(f"bodyDamage: {c['bodyDamage']}, hasBodyDamage: true")
            if c.get("playerNotWearing"):
                ids = sorted(set(item_ids[x] for x in c["playerNotWearing"] if x in item_ids))
                f.append("playerNotWearing: []int32{%s}" % ", ".join(map(str, ids)))
            if c.get("entities"):
                f.append("entities: []string{%s}" % ", ".join(gstr(x) for x in c["entities"]))
            if c.get("effects"):
                f.append("effects: []string{%s}" % ", ".join(gstr(x) for x in c["effects"]))
            for key in ("minUnique", "minCount", "signal"):
                if key in c:
                    f.append(f"{key}: {c[key]}")
            if "baby" in c:
                f.append(f"baby: {c['baby']}, hasBaby: true")
            for key in ("minDealt", "minDistH", "minDistY", "minDistAbs", "maxDistAbs", "startYMin", "endYMax",
                        "playerMinY"):
                if key in c:
                    f.append(f"{key}: {c[key]}")
            for key in ("smokey", "blocked", "noFire", "ominousBanner"):
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
