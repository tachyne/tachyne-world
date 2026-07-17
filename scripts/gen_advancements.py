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
    def __init__(self, z):
        self.z, self.cache = z, {}

    def expand(self, tag):  # "#minecraft:stone_tool_materials" -> item names
        tag = strip_ns(tag.lstrip("#"))
        if tag in self.cache:
            return self.cache[tag]
        out = []
        d = json.loads(self.z.read(f"data/minecraft/tags/item/{tag}.json"))
        for v in d["values"]:
            if v.startswith("#"):
                out += self.expand(v)
            else:
                out.append(strip_ns(v))
        self.cache[tag] = out
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


def distill(trigger, cond, tags):
    """Reduce a criterion to the engine-matchable schema. Returns a dict of
    non-default fields, or {'unmatchable': True} when the engine cannot
    observe this trigger/condition shape yet."""
    t = strip_ns(trigger)
    c = cond or {}
    d = {"trigger": t}
    if t == "inventory_changed":
        preds = [items_pred(p, tags) for p in c.get("items", [])]
        preds = [p for p in preds if p is not None]
        if not preds:
            d["unmatchable"] = True  # slot-count/any-item shapes: not yet
        d["items"] = preds
    elif t == "consume_item":
        p = items_pred(c.get("item", {}), tags)
        d["items"] = [p] if p else []
    elif t == "fishing_rod_hooked":
        p = items_pred(c.get("item", {}), tags)  # the caught item
        d["items"] = [p] if p else []
    elif t == "placed_block":
        blocks = [strip_ns(l["block"]) for l in c.get("location", [])
                  if isinstance(l, dict) and "block" in l]
        if blocks:
            d["block"] = blocks[0]
        else:
            d["unmatchable"] = True
    elif t == "player_killed_entity":
        d["entity"] = entity_type(c.get("entity"))
    elif t == "entity_killed_player":
        d["entity"] = entity_type(c.get("entity"))
    elif t == "bred_animals":
        d["entity"] = entity_type(c.get("child"))
    elif t == "tame_animal":
        d["entity"] = entity_type(c.get("entity"))
    elif t == "changed_dimension":
        d["dim"] = DIMS.get(c.get("to"), -1)
        if "from" in c:  # nether_travel-style round trips: not yet
            d["unmatchable"] = True
    elif t == "location":
        biome = ""
        for p in c.get("player", []) or []:
            loc = (p.get("predicate") or {}).get("location") or {}
            b = loc.get("biomes")
            if isinstance(b, str) and not b.startswith("#"):
                biome = strip_ns(b)
        if biome:
            d["biome"] = biome
        else:
            d["unmatchable"] = True  # structure visits etc.
    elif t == "construct_beacon":
        lv = c.get("level", {})
        if isinstance(lv, dict):
            d["minLevel"] = int(lv.get("min", 0))
        else:
            d["minLevel"] = int(lv)
    elif t in ("slept_in_bed", "villager_trade", "enchanted_item",
               "brewed_potion", "cured_zombie_villager"):
        pass  # condition-free (or engine matches unconditionally)
    else:
        d["unmatchable"] = True  # trigger not yet observable engine-side
    return d


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
