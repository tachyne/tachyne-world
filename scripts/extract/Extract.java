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
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.server.Bootstrap;
import net.minecraft.world.item.Item;
import net.minecraft.world.level.EmptyBlockGetter;
import net.minecraft.world.level.block.Block;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.phys.AABB;
import net.minecraft.world.phys.shapes.VoxelShape;

import java.io.FileWriter;
import java.io.Writer;
import java.lang.reflect.Method;
import java.util.List;

public class Extract {
    // 1.21.11 calls it getLightBlock(); 26.x renamed it getLightDampening().
    // Resolved once, so one tool runs against every version -- which is what
    // makes a cross-version comparison trustworthy.
    private static final Method LIGHT_DAMPENING = resolve("getLightDampening", "getLightBlock");

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

    public static void main(String[] args) throws Exception {
        SharedConstants.tryDetectVersion();
        Bootstrap.bootStrap();

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

            // One entry per STATE, in state-id order. These are the facts a
            // per-block dataset flattens: light, light filtering, collision,
            // solidity and the full-cube test all vary with a block's state.
            JsonArray emit = new JsonArray(), filter = new JsonArray(), box = new JsonArray();
            JsonArray solid = new JsonArray(), fullCube = new JsonArray(), solidRender = new JsonArray();
            JsonArray bounds = new JsonArray();
            for (BlockState st : states) {
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
            }
            o.add("emitLight", emit);
            o.add("filterLight", filter);
            o.add("boundingBox", box);
            o.add("isSolid", solid);
            o.add("isFullCube", fullCube);
            o.add("isSolidRender", solidRender);
            o.add("collisionBounds", bounds);
            blocks.add(o);
        }

        JsonArray items = new JsonArray();
        for (Item item : BuiltInRegistries.ITEM) {
            JsonObject o = new JsonObject();
            o.addProperty("name", BuiltInRegistries.ITEM.getKey(item).getPath());
            o.addProperty("id", BuiltInRegistries.ITEM.getId(item));
            // 26.x keeps stack size in data components, which a bare bootstrap
            // does not bind; -1 marks that rather than failing the whole run.
            int stack = -1;
            try {
                stack = item.getDefaultMaxStackSize();
            } catch (Throwable ignored) {
            }
            o.addProperty("stackSize", stack);
            items.add(o);
        }

        JsonObject root = new JsonObject();
        root.add("blocks", blocks);
        root.add("items", items);
        try (Writer w = new FileWriter(args[0])) {
            new GsonBuilder().create().toJson(root, w);
        }
        System.out.println("blocks=" + blocks.size() + " items=" + items.size() + " -> " + args[0]);
    }
}
