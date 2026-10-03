package server

// dialog.go is /dialog (DialogCommand) and the world's end of dialogs: a
// player's dialog opened or cleared (ServerPlayer.openDialog, the clear
// packet) and the custom click actions clients send back
// (MinecraftServer.handleCustomClickAction, which vanilla only logs — here
// plugins and the bus hear them).

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"unicode"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/plugin"
)

// registryDialogs are the dialogs the dialog registry holds (the core pack's,
// as the gateways declare it), and the tags naming them; data packs add
// their own (packDialogs).
var registryDialogs = map[string]bool{
	"minecraft:custom_options": true,
	"minecraft:quick_actions":  true,
	"minecraft:server_links":   true,
}

var dialogTags = map[string]bool{
	"#minecraft:pause_screen_additions": true,
	"#minecraft:quick_actions":          true,
}

// cmdDialog is the command: LEVEL_GAMEMASTERS, like vanilla's.
func (s *Server) cmdDialog(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission.")
		return
	}
	ev, complaint := parseDialog(p, args)
	if complaint != "" {
		p.tell(complaint)
		return
	}
	s.hub.post(ev)
}

// parseDialog is the argument handling: `show <targets> <dialog>` or
// `clear <targets>`. A non-empty complaint is what the player is told.
func parseDialog(p *player, args []string) (evDialog, string) {
	const usage = "Usage: /dialog show <targets> <dialog> | /dialog clear <targets>"
	if len(args) < 2 {
		return evDialog{}, usage
	}
	switch strings.ToLower(args[0]) {
	case "clear":
		if len(args) != 2 {
			return evDialog{}, usage
		}
		return evDialog{by: p, target: args[1]}, ""
	case "show":
		if len(args) < 3 {
			return evDialog{}, usage
		}
		d, complaint := parseDialogArg(strings.TrimSpace(strings.Join(args[2:], " ")))
		if complaint != "" {
			return evDialog{}, complaint
		}
		return evDialog{by: p, target: args[1], show: &d}, ""
	}
	return evDialog{}, usage
}

// parseDialogArg is ResourceOrIdArgument.dialog: a registry dialog by id, or
// an inline one as SNBT (JSON is SNBT too), checked against the dialog codec
// so a malformed dialog is refused here instead of failing to decode on the
// target's client.
func parseDialogArg(arg string) (attachproto.ShowDialog, string) {
	if strings.HasPrefix(arg, "{") {
		v, err := parseSNBT(arg)
		if err != nil {
			return attachproto.ShowDialog{}, "Failed to parse structure: " + err.Error()
		}
		m, ok := v.(map[string]any)
		if !ok {
			return attachproto.ShowDialog{}, "Failed to parse structure: not a dialog"
		}
		if why := checkDialog(m); why != "" {
			return attachproto.ShowDialog{}, "Failed to parse structure: " + why
		}
		raw, err := json.Marshal(m)
		if err != nil {
			return attachproto.ShowDialog{}, "Failed to parse structure: " + err.Error()
		}
		return attachproto.ShowDialog{Dialog: raw}, ""
	}
	id := qualifyID(arg)
	if !dialogExists(id) {
		return attachproto.ShowDialog{}, fmt.Sprintf("Can't find element '%s' in registry 'minecraft:dialog'", id)
	}
	return attachproto.ShowDialog{Ref: id}, ""
}

func qualifyID(id string) string {
	if strings.Contains(id, ":") {
		return id
	}
	return "minecraft:" + id
}

// typeOf reads a dispatch "type" field with its default namespace.
func typeOf(m map[string]any) string {
	t, _ := m["type"].(string)
	if t == "" {
		return ""
	}
	return qualifyID(t)
}

// checkDialog is Dialog.DIRECT_CODEC's shape: the type, CommonDialogData
// (title, body, inputs, after_action) and each type's own required fields.
func checkDialog(m map[string]any) string { return (*dialogScope)(nil).check(m) }

// dialogScope is the dialogs and dialog tags a dialog's references may name
// while a pack load is checked (its own entries beside the built-in ones);
// nil is the installed registry.
type dialogScope struct {
	ids  map[string]bool
	tags map[string]bool
}

func (sc *dialogScope) hasID(id string) bool {
	if sc != nil && sc.ids[id] {
		return true
	}
	return dialogExists(id)
}

func (sc *dialogScope) hasTag(id string) bool {
	if sc != nil {
		return sc.tags[id] || dialogTags["#"+id]
	}
	_, ok := tagMembers("dialog", id)
	return ok
}

func (sc *dialogScope) check(m map[string]any) string {
	if _, ok := m["title"]; !ok {
		return "No key title in MapLike"
	}
	if a, ok := m["after_action"].(string); ok && a != "close" && a != "none" && a != "wait_for_response" {
		return "Unknown after_action " + a
	}
	if body, ok := m["body"]; ok {
		for _, b := range listOf(body) {
			if why := checkBody(b); why != "" {
				return why
			}
		}
	}
	if inputs, ok := m["inputs"]; ok {
		list, ok := inputs.([]any)
		if !ok {
			return "inputs is not a list"
		}
		for _, in := range list {
			if why := checkInput(in); why != "" {
				return why
			}
		}
	}
	switch t := typeOf(m); t {
	case "minecraft:notice":
		if a, ok := m["action"]; ok {
			return sc.checkButton(a)
		}
	case "minecraft:confirmation":
		for _, k := range []string{"yes", "no"} {
			b, ok := m[k]
			if !ok {
				return "No key " + k + " in MapLike"
			}
			if why := sc.checkButton(b); why != "" {
				return why
			}
		}
	case "minecraft:multi_action":
		acts, _ := m["actions"].([]any)
		if len(acts) == 0 {
			return "actions must be a non-empty list"
		}
		for _, a := range acts {
			if why := sc.checkButton(a); why != "" {
				return why
			}
		}
	case "minecraft:dialog_list":
		d, ok := m["dialogs"]
		if !ok {
			return "No key dialogs in MapLike"
		}
		for _, e := range listOf(d) {
			if why := sc.checkDialogRef(e); why != "" {
				return why
			}
		}
	case "minecraft:server_links":
	case "":
		return "No key type in MapLike"
	default:
		return "Unknown dialog type " + t
	}
	if e, ok := m["exit_action"]; ok {
		return sc.checkButton(e)
	}
	return ""
}

// listOf is a compact list: one element or a list of them.
func listOf(v any) []any {
	if l, ok := v.([]any); ok {
		return l
	}
	return []any{v}
}

// checkDialogRef is a Holder<Dialog> inside a dialog: a registry id, a tag
// (in a list field) or an inline dialog.
func (sc *dialogScope) checkDialogRef(v any) string {
	switch x := v.(type) {
	case string:
		if strings.HasPrefix(x, "#") {
			if !sc.hasTag(qualifyID(x[1:])) {
				return "Unknown dialog tag " + x
			}
			return ""
		}
		if !sc.hasID(qualifyID(x)) {
			return "Unknown dialog " + x
		}
		return ""
	case map[string]any:
		return sc.check(x)
	}
	return "Not a dialog"
}

func checkBody(v any) string {
	switch x := v.(type) {
	case string: // PlainMessage's short form
		return ""
	case map[string]any:
		switch typeOf(x) {
		case "minecraft:plain_message":
			if _, ok := x["contents"]; !ok {
				return "No key contents in MapLike"
			}
		case "minecraft:item":
			if _, ok := x["item"]; !ok {
				return "No key item in MapLike"
			}
		default:
			return "Unknown dialog body type"
		}
		return ""
	}
	return "Not a dialog body"
}

// checkInput is Input: a key (a macro variable name) and a control.
func checkInput(v any) string {
	x, ok := v.(map[string]any)
	if !ok {
		return "Not an input"
	}
	key, ok := x["key"].(string)
	if !ok {
		return "No key key in MapLike"
	}
	for _, r := range key {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			return key + " is not a valid input name"
		}
	}
	if _, ok := x["label"]; !ok {
		return "No key label in MapLike"
	}
	switch typeOf(x) {
	case "minecraft:text", "minecraft:boolean":
	case "minecraft:single_option":
		if opts, _ := x["options"].([]any); len(opts) == 0 {
			return "options must be a non-empty list"
		}
	case "minecraft:number_range":
		for _, k := range []string{"start", "end"} {
			if _, ok := x[k]; !ok {
				return "No key " + k + " in MapLike"
			}
		}
	default:
		return "Unknown input type"
	}
	return ""
}

// dialogActions are the click-event actions a dialog button may carry,
// with the two dynamic ones.
var dialogActions = map[string]bool{
	"minecraft:open_url": true, "minecraft:run_command": true, "minecraft:suggest_command": true,
	"minecraft:show_dialog": true, "minecraft:change_page": true, "minecraft:copy_to_clipboard": true,
	"minecraft:custom": true, "minecraft:dynamic/run_command": true, "minecraft:dynamic/custom": true,
}

// checkButton is ActionButton: CommonButtonData's label and the optional
// action.
func (sc *dialogScope) checkButton(v any) string {
	x, ok := v.(map[string]any)
	if !ok {
		return "Not a button"
	}
	if _, ok := x["label"]; !ok {
		return "No key label in MapLike"
	}
	a, ok := x["action"]
	if !ok {
		return ""
	}
	am, ok := a.(map[string]any)
	if !ok {
		return "Not an action"
	}
	t := typeOf(am)
	if !dialogActions[t] {
		return "Unknown action type " + t
	}
	if t == "minecraft:show_dialog" {
		d, ok := am["dialog"]
		if !ok {
			return "No key dialog in MapLike"
		}
		if s, isStr := d.(string); isStr && strings.HasPrefix(s, "#") {
			return "Not a dialog" // a single holder, not a set
		}
		return sc.checkDialogRef(d)
	}
	return ""
}

// evDialog is /dialog show (show set) or clear for the named targets.
type evDialog struct {
	by     *player
	target string
	show   *attachproto.ShowDialog
}

func (evDialog) isHubEvent() {}

// onDialog opens or clears the targets' dialog and answers as DialogCommand
// does (sendFeedback, broadcast to operators).
func (h *hub) onDialog(players map[int32]*tracked, e evDialog) {
	targets := h.commandTargets(players, e.by.eid, e.target)
	if len(targets) == 0 {
		e.by.trySendEv(chatEv("No player was found"))
		return
	}
	for _, t := range targets {
		if !t.p.dialogs {
			continue // a gateway without dialogs (Bedrock) has nothing to show it with
		}
		if e.show != nil {
			t.p.trySendEv(*e.show)
		} else {
			t.p.trySendEv(attachproto.ClearDialog{})
		}
	}
	var msg string
	switch {
	case e.show != nil && len(targets) == 1:
		msg = "Displayed dialog to " + targets[0].p.name
	case e.show != nil:
		msg = fmt.Sprintf("Displayed dialog to %d players", len(targets))
	case len(targets) == 1:
		msg = "Cleared dialog for " + targets[0].p.name
	default:
		msg = fmt.Sprintf("Cleared dialog for %d players", len(targets))
	}
	h.cmdSuccess(players, e.by, msg, true)
}

// evCustomClick is a player's custom click action: from play, or from a
// configuration phase (ServerCommonPacketListenerImpl handles it in both),
// when the player is out of the level.
type evCustomClick struct {
	p *player
	e attachproto.CustomClickAction
}

func (evCustomClick) isHubEvent() {}

// onCustomClick is handleCustomClickAction: vanilla logs it; here plugins
// hear it (PlayerCustomClickEvent), and through them the bus. A dialog
// shown by a configuration task (or still open across a reconfiguration)
// answers while its player is between levels; that action counts too.
func (h *hub) onCustomClick(players map[int32]*tracked, e evCustomClick) {
	p := e.p
	if p == nil {
		return
	}
	t := players[p.eid]
	phase := "play"
	if t == nil || t.p != p {
		if !p.configuring.Load() {
			return // gone
		}
		phase = "configuration"
	}
	log.Printf("custom click action %s from %s in %s (payload %s)", e.e.ID, p.name, phase, e.e.Payload)
	if plugin.Has[*plugin.PlayerCustomClickEvent](h.plugins) {
		h.plugins.Fire(&plugin.PlayerCustomClickEvent{EID: p.eid, Name: p.name, ID: e.e.ID, Payload: e.e.Payload})
	}
}

// dialogExists is whether the dialog registry holds id: a built-in dialog
// or one the installed packs add.
func dialogExists(id string) bool {
	if registryDialogs[id] {
		return true
	}
	if pc := currentPack(); pc != nil {
		_, ok := pc.dialogs[id]
		return ok
	}
	return false
}
