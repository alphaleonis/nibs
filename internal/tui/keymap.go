package tui

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/alphaleonis/nibs/internal/safetext"
	"gopkg.in/yaml.v3"
)

// keymap is the action table in effect: the defaults with the user config's
// tui.keys applied.
type keymap struct {
	actions []action
}

// firstKey returns the first key bound to name, or "" when it has none.
func (km keymap) firstKey(name actionName) string {
	if a := findAction(km.actions, name); a != nil && len(a.Keys) > 0 {
		return a.Keys[0]
	}
	return ""
}

// userBinding is one entry of tui.keys, and whether buildKeymap ignored it.
type userBinding struct {
	name    actionName
	keys    []string
	entry   string
	line    int
	ignored string // why the entry was ignored, or "" while it is not
}

// buildKeymap applies keys, the keys setting of the user config's tui block, to
// defaults, which must have no key conflicts. It returns the keymap and the
// entries it ignored, whose actions keep their defaults. An entry is ignored
// when it names no action, a key is malformed, a key would take typing from a
// text box, or a key collides with another action in a context they share. Both
// sides of a collision between two entries are ignored. A keys that is not a
// mapping is one ignored entry, on keysLine. A nil or empty keys gives the
// defaults.
func buildKeymap(defaults []action, keys *yaml.Node, keysLine int) (keymap, []ignoredEntry) {
	km := keymap{actions: defaults}
	node, bad := keysNode(keys, keysLine)
	if bad != nil {
		return km, []ignoredEntry{*bad}
	}
	if node == nil {
		return km, nil
	}

	var bound []*userBinding
	for i := 0; i+1 < len(node.Content); i += 2 {
		name, value := node.Content[i], node.Content[i+1]
		b := &userBinding{
			name:  actionName(name.Value),
			entry: safetext.StripBounded(name.Value + ": " + nodeText(value)),
			line:  name.Line,
		}
		b.keys, b.ignored = checkEntry(defaults, b.name, value)
		if b.ignored == "" && slices.ContainsFunc(bound, func(o *userBinding) bool { return o.ignored == "" && o.name == b.name }) {
			b.ignored = "binds an action an earlier entry binds"
		}
		bound = append(bound, b)
	}

	// Each pass ignores at least one entry, and its action takes its defaults
	// again, which can collide with an entry that a later pass then ignores.
	for {
		table := applyBindings(defaults, bound)
		if !ignoreCollisions(table, bound) {
			km.actions = table
			break
		}
	}
	var ignored []ignoredEntry
	for _, b := range bound {
		if b.ignored != "" {
			ignored = append(ignored, ignoredEntry{entry: b.entry, line: b.line, reason: b.ignored})
		}
	}
	return km, ignored
}

// keysNode returns keys, resolved when it is an alias, or nil when it is
// absent. It also returns nil, and the ignored entry on line, when keys is not
// a mapping.
func keysNode(keys *yaml.Node, line int) (*yaml.Node, *ignoredEntry) {
	keys = resolveAlias(keys)
	switch {
	case absentNode(keys):
		return nil, nil
	case keys.Kind != yaml.MappingNode:
		return nil, &ignoredEntry{entry: safetext.StripBounded("tui.keys: " + nodeText(keys)), line: line, reason: "is not a map from action names to keys"}
	}
	return keys, nil
}

// decodeError returns the messages of a yaml.v3 decode error, without the
// prefix yaml.TypeError adds.
func decodeError(err error) string {
	var typeErr *yaml.TypeError
	if errors.As(err, &typeErr) {
		return strings.Join(typeErr.Errors, "; ")
	}
	return err.Error()
}

// applyBindings returns a copy of defaults with each entry not ignored in bound
// replacing its action's keys.
func applyBindings(defaults []action, bound []*userBinding) []action {
	table := slices.Clone(defaults)
	for _, b := range bound {
		if b.ignored == "" {
			findAction(table, b.name).Keys = b.keys
		}
	}
	return table
}

// ignoreCollisions marks every entry in bound whose keys collide in table as
// ignored. It reports whether it marked any.
func ignoreCollisions(table []action, bound []*userBinding) bool {
	byName := map[actionName]*userBinding{}
	for _, b := range bound {
		if b.ignored == "" {
			byName[b.name] = b
		}
	}
	reasons := map[actionName]string{}
	for _, c := range keyConflicts(table) {
		if _, ok := byName[c.holder]; ok && reasons[c.holder] == "" {
			reasons[c.holder] = c.reasonForHolder()
		}
		if _, ok := byName[c.other]; ok && reasons[c.other] == "" {
			reasons[c.other] = c.reasonForOther()
		}
	}
	for name, why := range reasons {
		byName[name].ignored = why
	}
	return len(reasons) > 0
}

// checkEntry returns the keys an entry binds name to, or why the entry is
// ignored.
func checkEntry(defaults []action, name actionName, value *yaml.Node) ([]string, string) {
	a := findAction(defaults, name)
	if a == nil {
		return nil, "no action has this name"
	}
	keys, why := entryKeys(value)
	if why != "" {
		return nil, why
	}
	for i, binding := range keys {
		seq := strings.Fields(binding)
		if len(seq) == 0 {
			return nil, "has an empty key"
		}
		for _, k := range seq {
			if why := checkKey(k); why != "" {
				return nil, fmt.Sprintf("%q: %s", k, why)
			}
		}
		keys[i] = strings.Join(seq, " ")
		if slices.Contains(keys[:i], keys[i]) {
			return nil, fmt.Sprintf("lists %q twice", keys[i])
		}
		if ctx, ok := typingContext(a); ok && typesText(seq[0]) {
			return nil, fmt.Sprintf("%q types text in the %s", seq[0], ctx)
		}
	}
	return keys, ""
}

// entryKeys reads an entry's value, one key or a list of keys, or says why it
// is neither.
func entryKeys(value *yaml.Node) ([]string, string) {
	isKey := func(n *yaml.Node) bool { return n.Kind == yaml.ScalarNode && n.Tag != "!!null" }
	switch {
	case isKey(value):
		return []string{value.Value}, ""
	case value.Kind == yaml.SequenceNode && len(value.Content) > 0:
		keys := make([]string, len(value.Content))
		for i, n := range value.Content {
			if !isKey(n) {
				return nil, "lists something that is not a key"
			}
			keys[i] = n.Value
		}
		return keys, ""
	case value.Kind == yaml.SequenceNode, value.Kind == yaml.ScalarNode:
		return nil, "names no key"
	}
	return nil, "is not a key or a list of keys"
}

// nodeText renders the value of a tui setting the way the palette shows it.
func nodeText(n *yaml.Node) string {
	switch n.Kind {
	case yaml.ScalarNode:
		return n.Value
	case yaml.SequenceNode:
		parts := make([]string, len(n.Content))
		for i, c := range n.Content {
			parts[i] = nodeText(c)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	}
	return "…"
}

// typingContext returns a text-box context a is active in. A text box takes
// every key that types, so an action there may be bound only to keys that do
// not.
func typingContext(a *action) (keyContext, bool) {
	for _, ctx := range []keyContext{ctxFilterInput, ctxTextInput, ctxPalette} {
		if slices.Contains(a.Contexts, ctx) {
			return ctx, true
		}
	}
	return 0, false
}

// typesText reports whether a key written as checkKey accepts it types text
// into a text box.
func typesText(k string) bool {
	return k == "space" || utf8.RuneCountInString(k) == 1
}

// keyModifiers are the modifiers a key can carry, in the order Bubbletea writes
// them in a key's String.
var keyModifiers = []string{"ctrl", "alt", "shift", "meta", "hyper", "super"}

// namedKeys are the names Bubbletea writes for the keys that type no
// character and that a legacy or kitty-protocol terminal sends without a
// modifier. TestKeysNamedKeysArriveAsWritten decodes each from the bytes a
// terminal sends.
var namedKeys = func() map[string]bool {
	names := map[string]bool{}
	for _, n := range strings.Fields("enter tab backspace esc space up down left right begin home end insert delete pgup pgdown printscreen pause menu") {
		names[n] = true
	}
	for i := 1; i <= 35; i++ {
		names[fmt.Sprintf("f%d", i)] = true
	}
	return names
}()

// legacyAliases are keys a legacy terminal sends as the same byte as another
// key, so the decoder writes them as that key.
var legacyAliases = map[string]string{"ctrl+[": "esc", "ctrl+m": "enter", "ctrl+i": "tab"}

// checkKey returns why k is not a key the way a Bubbletea key message's String
// writes it, or "" when it is. A valid key is a character, or a name from
// namedKeys, after any modifiers joined by "+" in keyModifiers order. Shift
// with a character is refused, because that key arrives as the character
// shift types. An uppercase ASCII letter with modifiers but no shift is
// refused, because the legacy and kitty encodings both decode that key with a
// lowercase letter. A key in legacyAliases is refused too.
func checkKey(k string) string {
	if alias, ok := legacyAliases[k]; ok {
		return fmt.Sprintf("a legacy terminal sends it as %q", alias)
	}
	rest := k
	var mods []string
	for utf8.RuneCountInString(rest) > 1 {
		mod, after, found := strings.Cut(rest, "+")
		if !found {
			break
		}
		i := slices.Index(keyModifiers, mod)
		if i < 0 {
			return fmt.Sprintf("%q is not a modifier", mod)
		}
		if len(mods) > 0 && i <= slices.Index(keyModifiers, mods[len(mods)-1]) {
			return "modifiers go in the order " + strings.Join(keyModifiers, ", ")
		}
		mods = append(mods, mod)
		rest = after
	}
	switch {
	case rest == "":
		return "no key follows the modifier"
	case utf8.RuneCountInString(rest) == 1:
		r, _ := utf8.DecodeRuneInString(rest)
		if !unicode.IsPrint(r) {
			return "is not a printable character"
		}
		if slices.Equal(mods, []string{"shift"}) {
			return "write the character shift types instead"
		}
		if len(mods) > 0 && !slices.Contains(mods, "shift") && r < utf8.RuneSelf && unicode.IsUpper(r) {
			withShift := slices.DeleteFunc(slices.Clone(keyModifiers), func(m string) bool {
				return m != "shift" && !slices.Contains(mods, m)
			})
			return fmt.Sprintf("write %q", strings.Join(withShift, "+")+"+"+string(unicode.ToLower(r)))
		}
		return ""
	case namedKeys[rest]:
		return ""
	case namedKeys[strings.ToLower(rest)]:
		return fmt.Sprintf("key names are lowercase: %q", strings.ToLower(rest))
	}
	return fmt.Sprintf("%q is not a key name", rest)
}
