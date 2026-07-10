# Vanilla 1.21.5 reference data (Mojang datagen)

Authoritative dumps produced by **Mojang's own data generator** — these outrank
minecraft-data, mcmeta and the wiki whenever they disagree.

Generated on the LAN VM from the official server jar (SHA-1
`e6ec2f64e6080b9b5d9b471b291c33cc7f509733`, verified against the piston-meta
version manifest):

```bash
~/java/bin/java -DbundlerMainClass=net.minecraft.data.Main \
    -jar server-1.21.5.jar --reports --server --output ./generated
```

| File | Contents |
|---|---|
| `reports/registries.json` | every registry + protocol IDs (83 registries) |
| `reports/blocks.json` | all 1104 blocks, 27 914 block states + properties |
| `reports/items.json` | items + default components |
| `reports/packets.json` | packet name → protocol ID, all states/directions |
| `reports/commands.json` | the vanilla command tree |
| `reports/biome_parameters/` | multi-noise climate points (seed-parity ref) |

Used by `cmd/diffprobe` (packet naming) and for crosschecking our `*_gen.go`
tables. The live vanilla oracle server runs on the VM at `:25566` — see
"Vanilla oracle" in `docs/MECHANICS.md`.
