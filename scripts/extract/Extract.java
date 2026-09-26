// Extract derives per-block and per-item facts by asking the GAME, for any
// version, rather than taking them from a third-party dataset or parsing
// source text.
//
// Text cannot reach the facts that matter most: light emission is a per-state
// lambda and collision is a shape built in code. Running the game evaluates
// them exactly. The generators under scripts/ read the JSON this writes.
//
// The first version was validated field for field against minecraft-data at
// both 1.21.11 and 26.1 (1,166 and 1,168 blocks, zero mismatches) before any
// generator was pointed at it. Run it through extract.sh, which prepares the
// classpath for a given version.
import com.google.gson.GsonBuilder;
import com.google.gson.JsonArray;
import com.google.gson.JsonObject;
import net.minecraft.SharedConstants;
import net.minecraft.core.BlockPos;
import net.minecraft.core.Direction;
import net.minecraft.core.HolderLookup;
import net.minecraft.core.component.DataComponents;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.server.Bootstrap;
import net.minecraft.world.food.FoodProperties;
import net.minecraft.world.item.Item;
import net.minecraft.world.item.component.Tool;
import net.minecraft.world.level.EmptyBlockGetter;
import net.minecraft.world.level.block.Block;
import net.minecraft.world.level.block.Blocks;
import net.minecraft.world.level.block.FireBlock;
import net.minecraft.world.level.block.SoundType;
import net.minecraft.world.level.block.entity.BlockEntityType;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.phys.AABB;
import net.minecraft.world.phys.Vec3;
import net.minecraft.world.phys.shapes.VoxelShape;

import java.io.FileWriter;
import java.io.Writer;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.lang.reflect.Modifier;
import java.util.IdentityHashMap;
import java.util.Map;
import java.util.List;

public class Extract {
    // 1.21.11 calls it getLightBlock(); 26.x renamed it getLightDampening().
    // Resolved once, so one tool runs against every version -- which is what
    // makes a cross-version comparison trustworthy.
    private static final Method LIGHT_DAMPENING = resolve("getLightDampening", "getLightBlock");

    // The offset bounds are protected on BlockBehaviour (a pointed dripstone
    // overrides them); asked through reflection so the override answers.
    private static final Method MAX_H_OFFSET = declared("getMaxHorizontalOffset");
    private static final Method MAX_V_OFFSET = declared("getMaxVerticalOffset");

    private static Method declared(String name) {
        try {
            Method m = net.minecraft.world.level.block.state.BlockBehaviour.class.getDeclaredMethod(name);
            m.setAccessible(true);
            return m;
        } catch (NoSuchMethodException e) {
            throw new IllegalStateException(e);
        }
    }

    private static Method resolve(String... names) {
        for (String n : names) {
            try {
                return BlockState.class.getMethod(n);
            } catch (NoSuchMethodException ignored) {
            }
        }
        throw new IllegalStateException("no light-dampening accessor on BlockState");
    }

    private static int dampening(BlockState st) {
        try {
            return (Integer) LIGHT_DAMPENING.invoke(st);
        } catch (Exception e) {
            throw new RuntimeException(e);
        }
    }

    // 26.x keeps an item's stack size, durability and food in data components
    // that the server binds while loading resources, after the registry
    // bootstrap: BuiltInRegistries.DATA_COMPONENT_INITIALIZERS built against a
    // full registry lookup, then applied. A bare Bootstrap never does it, so
    // this does, against the data generator's lookup. 1.21.x has no such step —
    // its items carry their components already — so it is found by reflection
    // and skipped when absent.
    private static void bindItemComponents() throws Exception {
        java.lang.reflect.Field field;
        try {
            field = BuiltInRegistries.class.getField("DATA_COMPONENT_INITIALIZERS");
        } catch (NoSuchFieldException e) {
            return;
        }
        Class<?> vanilla = Class.forName("net.minecraft.data.registries.VanillaRegistries");
        Method lookup = null;
        for (String n : new String[]{"createWorldLookup", "createLookup"}) {
            try {
                lookup = vanilla.getMethod(n);
                break;
            } catch (NoSuchMethodException ignored) {
            }
        }
        if (lookup == null) {
            throw new IllegalStateException("no registry lookup on VanillaRegistries");
        }
        Object initializers = field.get(null);
        List<?> pending = (List<?>) initializers.getClass()
                .getMethod("build", HolderLookup.Provider.class)
                .invoke(initializers, lookup.invoke(null));
        Method apply = Class.forName("net.minecraft.core.component.DataComponentInitializers$PendingComponents")
                .getMethod("apply");
        for (Object p : pending) {
            apply.invoke(p);
        }
    }

    public static void main(String[] args) throws Exception {
        SharedConstants.tryDetectVersion();
        Bootstrap.bootStrap();
        bindItemComponents();

        // SoundType's constants by identity, so a block's sound can be named:
        // blocks share these instances (copied properties carry the same one).
        Map<SoundType, String> soundNames = new IdentityHashMap<>();
        JsonArray soundTypes = new JsonArray();
        for (Field f : SoundType.class.getFields()) {
            if (!Modifier.isStatic(f.getModifiers()) || f.getType() != SoundType.class) continue;
            SoundType t = (SoundType) f.get(null);
            if (soundNames.putIfAbsent(t, f.getName()) != null) continue;
            JsonObject so = new JsonObject();
            so.addProperty("name", f.getName());
            so.addProperty("volume", t.getVolume());
            so.addProperty("pitch", t.getPitch());
            so.addProperty("break", BuiltInRegistries.SOUND_EVENT.getKey(t.getBreakSound()).getPath());
            so.addProperty("step", BuiltInRegistries.SOUND_EVENT.getKey(t.getStepSound()).getPath());
            so.addProperty("place", BuiltInRegistries.SOUND_EVENT.getKey(t.getPlaceSound()).getPath());
            so.addProperty("hit", BuiltInRegistries.SOUND_EVENT.getKey(t.getHitSound()).getPath());
            so.addProperty("fall", BuiltInRegistries.SOUND_EVENT.getKey(t.getFallSound()).getPath());
            soundTypes.add(so);
        }

        // Fire's odds for a state, asked of the fire block itself (its
        // setFlammable table, and the rule that a waterlogged state never burns).
        java.lang.reflect.Method igniteOdds = FireBlock.class.getDeclaredMethod("getIgniteOdds", BlockState.class);
        java.lang.reflect.Method burnOdds = FireBlock.class.getDeclaredMethod("getBurnOdds", BlockState.class);
        igniteOdds.setAccessible(true);
        burnOdds.setAccessible(true);
        FireBlock fire = (FireBlock) Blocks.FIRE;

        JsonArray blocks = new JsonArray();
        for (Block block : BuiltInRegistries.BLOCK) {
            List<BlockState> states = block.getStateDefinition().getPossibleStates();
            JsonObject o = new JsonObject();
            o.addProperty("name", BuiltInRegistries.BLOCK.getKey(block).getPath());
            o.addProperty("hardness", block.defaultDestroyTime());
            o.addProperty("resistance", block.getExplosionResistance());
            o.addProperty("minStateId", Block.getId(states.get(0)));
            o.addProperty("maxStateId", Block.getId(states.get(states.size() - 1)));
            o.addProperty("defaultState", Block.getId(block.defaultBlockState()));
            // Which block entity type, if any, this block carries — asked of each
            // type's own valid-block set rather than guessed from the block's name.
            String beType = null;
            for (BlockEntityType<?> t : BuiltInRegistries.BLOCK_ENTITY_TYPE) {
                if (t.isValid(block.defaultBlockState())) {
                    beType = BuiltInRegistries.BLOCK_ENTITY_TYPE.getKey(t).getPath();
                    break;
                }
            }
            o.addProperty("blockEntityType", beType);
            // What PistonBaseBlock.isPushable asks: does the state carry a block
            // entity at all (such blocks never move).
            o.addProperty("hasBlockEntity", block.defaultBlockState().hasBlockEntity());
            // The loot table the block actually drops from. Usually its own name,
            // but a wall torch, wall sign, wall banner or wall head borrows the
            // standing block's (dropsLike), and some blocks have none at all.
            block.getLootTable().ifPresent(k -> o.addProperty("lootTable", k.identifier().getPath()));
            // The item this block is, as the game maps it: a wall sign is the sign
            // item, a wall torch the torch. And whether it needs the right tool
            // to drop anything at all (Player.hasCorrectToolForDrops).
            o.addProperty("item", BuiltInRegistries.ITEM.getKey(block.asItem()).getPath());
            o.addProperty("requiresTool", block.defaultBlockState().requiresCorrectToolForDrops());
            o.addProperty("isAir", block.defaultBlockState().isAir());
            // The block's sound type (its default state's), by SoundType constant.
            o.addProperty("sound", soundNames.getOrDefault(block.defaultBlockState().getSoundType(), null));
            // The note-block instrument this block gives a note block above it.
            o.addProperty("instrument", block.defaultBlockState().instrument().getSerializedName());
            // Whether lava sets it alight (Properties.ignitedByLava: a block
            // flag every state shares — LavaFluid.randomTick's fire test, and
            // not the same set as the fire odds below).
            o.addProperty("ignitedByLava", block.defaultBlockState().ignitedByLava());

            // One entry per STATE, in state-id order. These are the facts a
            // per-block dataset flattens: light, light filtering, collision,
            // solidity and the full-cube test all vary with a block's state.
            JsonArray emit = new JsonArray(), filter = new JsonArray(), box = new JsonArray();
            JsonArray solid = new JsonArray(), fullCube = new JsonArray(), solidRender = new JsonArray();
            JsonArray bounds = new JsonArray(), mapColor = new JsonArray(), fluid = new JsonArray();
            JsonArray push = new JsonArray(), ignite = new JsonArray(), burn = new JsonArray();
            JsonArray faces = new JsonArray(), outline = new JsonArray();
            // A plant's random nudge (BlockBehaviour.Properties.offsetType): the
            // outline below is recorded un-nudged, and the consumer re-applies
            // the nudge for the position it asks about. The type is told apart
            // by the Y component, which only an XYZ nudge ever sets.
            if (block.defaultBlockState().hasOffsetFunction()) {
                JsonObject off = new JsonObject();
                boolean xyz = false;
                for (int i = 0; i < 16 && !xyz; i++) {
                    xyz = block.defaultBlockState().getOffset(new BlockPos(i, 0, i * 7)).y != 0;
                }
                off.addProperty("type", xyz ? "xyz" : "xz");
                off.addProperty("maxHorizontal", (float) MAX_H_OFFSET.invoke(block));
                off.addProperty("maxVertical", (float) MAX_V_OFFSET.invoke(block));
                o.add("offset", off);
            }
            for (BlockState st : states) {
                // The outline shape (getShape under an empty context: what a
                // ClipContext.Block.OUTLINE ray and the crosshair hit), as its
                // boxes, with the offset nudge at the origin taken back out.
                // null for the full cube, [] for no outline at all.
                VoxelShape ol = st.getShape(EmptyBlockGetter.INSTANCE, BlockPos.ZERO);
                if (ol.isEmpty()) {
                    outline.add(new JsonArray());
                } else if (Block.isShapeFullBlock(ol)) {
                    outline.add(com.google.gson.JsonNull.INSTANCE);
                } else {
                    Vec3 nudge = st.getOffset(BlockPos.ZERO);
                    JsonArray boxes = new JsonArray();
                    for (AABB r : ol.move(nudge.reverse()).toAabbs()) {
                        JsonArray rr = new JsonArray();
                        for (double v : new double[]{r.minX, r.minY, r.minZ, r.maxX, r.maxY, r.maxZ}) {
                            rr.add(Math.round(v * 1e6) / 1e6);
                        }
                        boxes.add(rr);
                    }
                    outline.add(boxes);
                }
                // The state's map colour id (a bed's halves differ).
                mapColor.add(st.getMapColor(EmptyBlockGetter.INSTANCE, BlockPos.ZERO).id);
                // Whether the state holds a fluid — water and lava, and every
                // waterlogged block, kelp and seagrass (LevelChunkSection's fluidCount).
                fluid.add(!st.getFluidState().isEmpty());
                // How a piston treats it: NORMAL, DESTROY, BLOCK, IGNORE, PUSH_ONLY.
                push.add(st.getPistonPushReaction().name());
                ignite.add((int) igniteOdds.invoke(fire, st));
                burn.add((int) burnOdds.invoke(fire, st));
                emit.add(st.getLightEmission());
                filter.add(dampening(st));
                VoxelShape shape = st.getCollisionShape(EmptyBlockGetter.INSTANCE, BlockPos.ZERO);
                box.add(shape.isEmpty() ? "empty" : "block");
                // Asked of the game rather than reimplemented: isSolid applies
                // the forceSolidOn/Off flags before it measures anything, and a
                // hand-kept copy of those flags is how the solid table went wrong.
                solid.add(st.isSolid());
                fullCube.add(st.isCollisionShapeFullBlock(EmptyBlockGetter.INSTANCE, BlockPos.ZERO));
                solidRender.add(st.isSolidRender());
                JsonArray aabb = new JsonArray();
                if (!shape.isEmpty()) {
                    AABB b = shape.bounds();
                    for (double v : new double[]{b.minX, b.minY, b.minZ, b.maxX, b.maxY, b.maxZ}) {
                        aabb.add(v);
                    }
                }
                bounds.add(aabb);
                // The collision shape's cross-section on each face, for the
                // fluid face test (FlowingFluid.canPassThroughWall →
                // Shapes.mergedFaceOccludes): per direction in Direction order
                // (down, up, north, south, west, east), the face shape's boxes
                // flattened onto the face's two axes as [a0, b0, a1, b1].
                // Empty and full-block shapes, the common cases, record null.
                if (shape.isEmpty() || st.isCollisionShapeFullBlock(EmptyBlockGetter.INSTANCE, BlockPos.ZERO)) {
                    faces.add(com.google.gson.JsonNull.INSTANCE);
                } else {
                    JsonArray perDir = new JsonArray();
                    for (Direction d : Direction.values()) {
                        JsonArray rects = new JsonArray();
                        VoxelShape face = shape.getFaceShape(d);
                        if (!face.isEmpty()) {
                            for (AABB r : face.toAabbs()) {
                                double[] q;
                                switch (d.getAxis()) {
                                    case X: q = new double[]{r.minY, r.minZ, r.maxY, r.maxZ}; break;
                                    case Y: q = new double[]{r.minX, r.minZ, r.maxX, r.maxZ}; break;
                                    default: q = new double[]{r.minX, r.minY, r.maxX, r.maxY}; break;
                                }
                                JsonArray rr = new JsonArray();
                                for (double v : q) {
                                    rr.add(v);
                                }
                                rects.add(rr);
                            }
                        }
                        perDir.add(rects);
                    }
                    faces.add(perDir);
                }
            }
            o.add("emitLight", emit);
            o.add("filterLight", filter);
            o.add("boundingBox", box);
            o.add("isSolid", solid);
            o.add("isFullCube", fullCube);
            o.add("isSolidRender", solidRender);
            o.add("collisionBounds", bounds);
            o.add("mapColor", mapColor);
            o.add("fluid", fluid);
            o.add("pushReaction", push);
            o.add("igniteOdds", ignite);
            o.add("burnOdds", burn);
            o.add("collisionFaces", faces);
            o.add("outlineBoxes", outline);
            blocks.add(o);
        }

        JsonArray items = new JsonArray();
        for (Item item : BuiltInRegistries.ITEM) {
            JsonObject o = new JsonObject();
            o.addProperty("name", BuiltInRegistries.ITEM.getKey(item).getPath());
            o.addProperty("id", BuiltInRegistries.ITEM.getId(item));
            o.addProperty("stackSize", item.getDefaultMaxStackSize());
            Integer maxDamage = item.components().get(DataComponents.MAX_DAMAGE);
            if (maxDamage != null) {
                o.addProperty("maxDurability", maxDamage);
            }
            // A tool's rules, as Tool.isCorrectForDrops reads them: the first rule
            // that states a verdict and holds the block decides. Blocks are named
            // by tag where the rule uses one, so the caller can resolve them from
            // the jar's tag files — the tags need not be bound here.
            Tool tool = item.components().get(DataComponents.TOOL);
            if (tool != null) {
                JsonArray rules = new JsonArray();
                for (Tool.Rule r : tool.rules()) {
                    if (r.correctForDrops().isEmpty()) {
                        continue;
                    }
                    JsonObject jr = new JsonObject();
                    r.blocks().unwrap()
                            .ifLeft(tag -> jr.addProperty("tag", tag.location().getPath()))
                            .ifRight(list -> {
                                JsonArray names = new JsonArray();
                                list.forEach(h -> names.add(BuiltInRegistries.BLOCK.getKey(h.value()).getPath()));
                                jr.add("blocks", names);
                            });
                    jr.addProperty("correct", r.correctForDrops().get());
                    rules.add(jr);
                }
                o.add("toolRules", rules);
            }
            FoodProperties food = item.components().get(DataComponents.FOOD);
            if (food != null) {
                o.addProperty("nutrition", food.nutrition());
                o.addProperty("saturation", food.saturation());
            }
            items.add(o);
        }

        JsonObject root = new JsonObject();
        root.add("blocks", blocks);
        root.add("items", items);
        root.add("soundTypes", soundTypes);
        try (Writer w = new FileWriter(args[0])) {
            new GsonBuilder().create().toJson(root, w);
        }
        System.out.println("blocks=" + blocks.size() + " items=" + items.size() + " -> " + args[0]);
    }
}
