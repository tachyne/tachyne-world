package server

// Note blocks + jukeboxes, mirroring the vanilla models. A note block's
// right-click cycles its note (0-24) and plays; a punch just plays. The
// sound is resolved server-side (vanilla triggerEvent): instrument from the
// block below (mob heads above override), pitch 2^((note-12)/12), volume 3
// in the records category, plus the colored note particle. Jukeboxes hold
// one disc, start/stop music via level events 1010/1011 — the data value is
// the jukebox_song registry index, which the gateways sync to every client
// in our declared order, so one number works on every version.

import (
	"math"
	"strings"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// --- note blocks ---

// Note-block state layout at canonical 1.21.11: properties enumerate
// alphabetically — instrument(23) × note(25) × powered(2).
var noteBlockBase, noteBlockMax = worldgen.BlockRange("note_block")

func isNoteBlock(state uint32) bool { return state >= noteBlockBase && state <= noteBlockMax }

func noteOf(state uint32) int { return int(state-noteBlockBase) / 2 % 25 }

// withNote keeps the state's instrument/powered bits and swaps the note.
func withNote(state uint32, note int) uint32 {
	off := state - noteBlockBase
	return noteBlockBase + off - uint32(noteOf(state)*2) + uint32(note*2)
}

// notePowered reads the note block's powered bit (the low bit; booleans list
// true first, so an even offset is powered=true).
func notePowered(state uint32) bool { return (state-noteBlockBase)%2 == 0 }

// noteWithPowered sets the powered bit, preserving instrument + note.
func noteWithPowered(state uint32, p bool) uint32 {
	b := state - (state-noteBlockBase)%2
	if p {
		return b
	}
	return b + 1
}

// noteInstrumentSounds maps the generated instrument indexes (harp = 0) to
// their sound events. Mob-head instruments use the imitate set.
var noteInstrumentSounds = map[string]string{
	"harp": "minecraft:block.note_block.harp", "basedrum": "minecraft:block.note_block.basedrum",
	"snare": "minecraft:block.note_block.snare", "hat": "minecraft:block.note_block.hat",
	"bass": "minecraft:block.note_block.bass", "flute": "minecraft:block.note_block.flute",
	"bell": "minecraft:block.note_block.bell", "guitar": "minecraft:block.note_block.guitar",
	"chime": "minecraft:block.note_block.chime", "xylophone": "minecraft:block.note_block.xylophone",
	"iron_xylophone": "minecraft:block.note_block.iron_xylophone", "cow_bell": "minecraft:block.note_block.cow_bell",
	"didgeridoo": "minecraft:block.note_block.didgeridoo", "bit": "minecraft:block.note_block.bit",
	"banjo": "minecraft:block.note_block.banjo", "pling": "minecraft:block.note_block.pling",
	"zombie": "minecraft:block.note_block.imitate.zombie", "skeleton": "minecraft:block.note_block.imitate.skeleton",
	"creeper": "minecraft:block.note_block.imitate.creeper", "dragon": "minecraft:block.note_block.imitate.ender_dragon",
	"wither_skeleton": "minecraft:block.note_block.imitate.wither_skeleton", "piglin": "minecraft:block.note_block.imitate.piglin",
}

// noteHeadInstruments are the instruments sensed from the block ABOVE
// (vanilla worksAboveNoteBlock); they play at fixed pitch.
var noteHeadInstruments = map[string]bool{
	"zombie": true, "skeleton": true, "creeper": true, "dragon": true,
	"wither_skeleton": true, "piglin": true, "custom_head": true,
}

// noteInstrument resolves the live instrument for a note block (vanilla
// setInstrument): a head above wins; otherwise the block below, unless the
// below-block is itself a head (then harp).
func (h *hub) noteInstrument(dim, x, y, z int) string {
	w := h.worldFor(dim)
	above := noteInstrumentNames[noteInstrumentFor(w.At(x, y+1, z))]
	if noteHeadInstruments[above] {
		return above
	}
	below := noteInstrumentNames[noteInstrumentFor(w.At(x, y-1, z))]
	if noteHeadInstruments[below] {
		return "harp"
	}
	return below
}

type evNoteBlock struct {
	eid     int32
	x, y, z int
	tune    bool // right-click cycles the note before playing
}

func (evNoteBlock) isHubEvent() {}

// onNoteBlock tunes and/or plays a note block on the hub.
func (h *hub) onNoteBlock(players map[int32]*tracked, e evNoteBlock) {
	t := players[e.eid]
	if t == nil {
		return
	}
	w := h.worldFor(t.dim)
	state := w.At(e.x, e.y, e.z)
	if !isNoteBlock(state) {
		return
	}
	if e.tune {
		state = withNote(state, (noteOf(state)+1)%25)
		h.setBlockLive(players, t.dim, e.x, e.y, e.z, state)
		h.incCustom(t, "tune_noteblock", 1)
	} else {
		h.incCustom(t, "play_noteblock", 1)
	}
	h.playNoteBlock(players, t.dim, e.x, e.y, e.z, state)
}

// playNoteBlock resolves and plays the note (vanilla playNote+triggerEvent):
// muffled by a solid block above unless a head instrument is at work.
func (h *hub) playNoteBlock(players map[int32]*tracked, dim, x, y, z int, state uint32) {
	instr := h.noteInstrument(dim, x, y, z)
	if !noteHeadInstruments[instr] && h.worldFor(dim).At(x, y+1, z) != 0 {
		return // muffled
	}
	sound, ok := noteInstrumentSounds[instr]
	if !ok {
		return // custom_head: no skull sound source in v1
	}
	note := noteOf(state)
	pitch := float32(1)
	if !noteHeadInstruments[instr] {
		pitch = float32(math.Pow(2, float64(note-12)/12))
	}
	cx, cy, cz := float64(x)+0.5, float64(y)+0.5, float64(z)+0.5
	h.playSoundDim(players, dim, sound, sndRecord, cx, cy, cz, 3, pitch)
	if !noteHeadInstruments[instr] {
		// The note particle: with count 0 the offset is the hue (note/24).
		h.toNearbyEv(players, dim, cx, cz, attachproto.Particles{
			PID: particleNote, X: cx, Y: float64(y) + 1.2, Z: cz,
			Spread: float32(note) / 24, Speed: 1, Count: 0,
		})
	}
}

// --- jukeboxes ---

var jukeboxBase, jukeboxMax = worldgen.BlockRange("jukebox")

func isJukebox(state uint32) bool { return state >= jukeboxBase && state <= jukeboxMax }

// Jukebox states: has_record=true, then false (the default).
func jukeboxState(hasRecord bool) uint32 {
	if hasRecord {
		return jukeboxBase
	}
	return jukeboxBase + 1
}

// Jukebox level events (vanilla): data = jukebox_song registry index.
const (
	worldEventJukeboxPlay = 1010
	worldEventJukeboxStop = 1011
)

// jukeboxSongs lists the songs in the registry order the gateways declare
// to every client (the base set alphabetically, then the appended ones), so
// the level-event data value is version-independent. Lengths in seconds.
var jukeboxSongs = []struct {
	name string
	secs int
}{
	{"11", 71}, {"13", 178}, {"5", 178}, {"blocks", 345}, {"cat", 185},
	{"chirp", 185}, {"creator", 176}, {"creator_music_box", 73}, {"far", 174},
	{"mall", 197}, {"mellohi", 96}, {"otherside", 195}, {"pigstep", 149},
	{"precipice", 299}, {"relic", 218}, {"stal", 150}, {"strad", 188},
	{"wait", 238}, {"ward", 251},
	// Appended for 26.x clients (older clients lack these registry entries).
	{"bounce", 234}, {"lava_chicken", 134}, {"tears", 175},
}

// discNames reverse-maps music-disc item ids to their names once.
var discNames = func() map[int32]string {
	out := map[int32]string{}
	for name, id := range itemByName {
		if strings.HasPrefix(name, "music_disc_") {
			out[int32(id)] = name
		}
	}
	return out
}()

// jukeboxSongFor resolves a music-disc item to its song index and length
// in ticks (with vanilla's one-second end padding). ok=false for non-discs.
func jukeboxSongFor(item int32) (int32, uint64, bool) {
	name := discNames[item]
	if !strings.HasPrefix(name, "music_disc_") {
		return 0, 0, false
	}
	song := strings.TrimPrefix(name, "music_disc_")
	for i, s := range jukeboxSongs {
		if s.name == song {
			return int32(i), uint64(s.secs*20 + 20), true
		}
	}
	return 0, 0, false
}

// jukebox is one jukebox's held disc + playback clock.
type jukebox struct {
	disc    invStack
	started uint64 // world tick playback began (0 = not playing)
	length  uint64 // song length in ticks incl. end padding
}

type evUseJukebox struct {
	eid     int32
	x, y, z int
	slot    int32
}

func (evUseJukebox) isHubEvent() {}

// onUseJukebox inserts a disc or ejects the current one.
func (h *hub) onUseJukebox(players map[int32]*tracked, e evUseJukebox) {
	t := players[e.eid]
	if t == nil || t.inv == nil {
		return
	}
	pos := blockPos{e.x, e.y, e.z}
	jb := h.jukeboxes[pos]
	if jb != nil && jb.disc.count > 0 { // eject
		h.ejectJukebox(players, t.dim, pos, jb)
		return
	}
	st := t.inv.slots[e.slot]
	song, length, ok := jukeboxSongFor(st.item)
	if !ok {
		return
	}
	disc := st
	disc.count = 1
	if t.gamemode != gmCreative {
		if st.count--; st.count == 0 {
			st = invStack{}
		}
		t.inv.slots[e.slot] = st
		h.sendSlot(t, int(e.slot))
	}
	h.jukeboxes[pos] = &jukebox{disc: disc, started: h.tick.Load(), length: length}
	h.setBlockLive(players, t.dim, e.x, e.y, e.z, jukeboxState(true))
	h.toNearbyEv(players, t.dim, float64(e.x), float64(e.z), attachproto.WorldFX{
		Event: worldEventJukeboxPlay, X: e.x, Y: e.y, Z: e.z, Data: song})
	h.incCustom(t, "play_record", 1)
}

// ejectJukebox pops the disc and stops the music.
func (h *hub) ejectJukebox(players map[int32]*tracked, dim int, pos blockPos, jb *jukebox) {
	delete(h.jukeboxes, pos)
	if it := h.spawnItem(players, jb.disc.item, 1,
		float64(pos.x)+0.5, float64(pos.y)+1.01, float64(pos.z)+0.5); it != nil {
		it.mapID = jb.disc.mapID
	}
	h.setBlockLive(players, dim, pos.x, pos.y, pos.z, jukeboxState(false))
	h.toNearbyEv(players, dim, float64(pos.x), float64(pos.z), attachproto.WorldFX{
		Event: worldEventJukeboxStop, X: pos.x, Y: pos.y, Z: pos.z})
}

// jukeboxTick ends songs after their length (vanilla stops server-side and
// tells clients; the disc stays until ejected). 1 Hz is plenty.
func (h *hub) jukeboxTick(players map[int32]*tracked) {
	now := h.tick.Load()
	for pos, jb := range h.jukeboxes {
		if jb.started != 0 && now-jb.started >= jb.length {
			jb.started = 0
			// Dim isn't stored per jukebox: overworld-only placement (block
			// sim v1), matching containers.
			h.toNearbyEv(players, 0, float64(pos.x), float64(pos.z), attachproto.WorldFX{
				Event: worldEventJukeboxStop, X: pos.x, Y: pos.y, Z: pos.z})
		}
	}
}

// spillJukebox runs from spillContainer: a broken jukebox drops its disc.
func (h *hub) spillJukebox(players map[int32]*tracked, x, y, z int, newState uint32) {
	pos := blockPos{x, y, z}
	jb := h.jukeboxes[pos]
	if jb == nil || isJukebox(newState) {
		return
	}
	delete(h.jukeboxes, pos)
	if it := h.spawnItem(players, jb.disc.item, 1,
		float64(x)+0.5, float64(y)+0.5, float64(z)+0.5); it != nil {
		it.mapID = jb.disc.mapID
	}
	h.toNearbyEv(players, 0, float64(x), float64(z), attachproto.WorldFX{
		Event: worldEventJukeboxStop, X: x, Y: y, Z: z})
}
